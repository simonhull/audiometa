package m4a

import (
	"context"
	stdbinary "encoding/binary"
	"errors"
	"fmt"
	"math"
	"slices"
	"time"
	"unicode/utf16"

	"github.com/simonhull/audiometa/internal/binary"
	"github.com/simonhull/audiometa/internal/types"
)

// errTruncatedTable reports a sample table or list that holds fewer entries
// than it declares. Its readers return the entries they did read with it.
var errTruncatedTable = errors.New("sample table cut short")

// truncatedTable reports that the atom holds have of the want entries it declares.
func truncatedTable(atom string, have, want int64) error {
	return fmt.Errorf("%s holds %d of the %d entries it declares: %w", atom, have, want, errTruncatedTable)
}

// parseChapters tries QuickTime chapter tracks (tref), then Nero chapters
// (chpl). Chapters read before damage are returned together with its error, so
// a caller keeps them and reports the damage. An absent source is not an error.
func parseChapters(ctx context.Context, sr *binary.SafeReader, moovAtom *Atom, fileDuration time.Duration) ([]types.Chapter, error) {
	// Try QuickTime chapter tracks first (most common in professional audiobooks)
	qtChapters, qtErr := parseQuickTimeChapters(ctx, sr, moovAtom, fileDuration)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(qtChapters) > 0 {
		return qtChapters, qtErr
	}

	// Fall back to Nero chpl format
	chplChapters, chplErr := parseChplChapters(ctx, sr, moovAtom, fileDuration)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(chplChapters) > 0 {
		return chplChapters, errors.Join(qtErr, chplErr)
	}

	return nil, errors.Join(qtErr, chplErr)
}

// parseChplChapters extracts chapter markers from the chpl atom (Nero format).
// An absent udta or chpl means no chapters; a corrupt one is an error, and a
// list cut short returns the chapters before the cut with its error.
func parseChplChapters(ctx context.Context, sr *binary.SafeReader, moovAtom *Atom, fileDuration time.Duration) ([]types.Chapter, error) {
	// Path: moov -> udta -> chpl
	chplAtom, err := moovAtom.path(ctx, sr, atomUdta, "chpl")
	if errors.Is(err, errAtomNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	// Parse chpl atom
	offset := chplAtom.DataOffset()

	// Read version (1 byte)
	version, err := sr.Read[uint8](offset, "chpl version")
	if err != nil {
		return nil, err
	}
	offset++

	// Skip flags (3 bytes)
	offset += 3

	// The 4-byte reserved field exists only when version != 0
	// (ffmpeg libavformat/mov.c mov_read_chpl).
	if version != 0 {
		offset += 4
	}

	// Read chapter count (1 byte)
	chapterCount, err := sr.Read[uint8](offset, "chapter count")
	if err != nil {
		return nil, err
	}
	offset++

	if chapterCount == 0 {
		return nil, nil
	}

	chapters := make([]types.Chapter, 0, chapterCount)

	// Read each chapter
	// cut returns what was read so far, with err, when the list ends early.
	cut := func(i uint8, err error) ([]types.Chapter, error) {
		types.FillChapterEndTimes(chapters, fileDuration)
		return chapters, fmt.Errorf("chpl chapter %d: %w: %w", i+1, errTruncatedTable, err)
	}

	for i := range chapterCount {
		// Read start time (8 bytes, in 100-nanosecond units)
		startTime100ns, err := sr.Read[uint64](offset, "chapter start time")
		if err != nil {
			return cut(i, err)
		}
		offset += 8

		// Convert to time.Duration (100-nanosecond units -> nanoseconds)
		if startTime100ns > math.MaxInt64/100 {
			break // Start time overflows time.Duration: keep the chapters read so far.
		}
		startTime := time.Duration(startTime100ns * 100)

		// Read title length (1 byte)
		titleLen, err := sr.Read[uint8](offset, "chapter title length")
		if err != nil {
			return cut(i, err)
		}
		offset++

		// Read title (N bytes)
		var title string
		if titleLen > 0 {
			titleBytes := make([]byte, titleLen)
			if err := sr.ReadAt(titleBytes, offset, "chapter title"); err != nil {
				return cut(i, err)
			}
			offset += int64(titleLen)
			title = string(titleBytes)
		}

		chapter := types.Chapter{
			Index:     int(i + 1),
			Title:     title,
			StartTime: startTime,
			Source:    types.ChapterSourceNero,
		}

		chapters = append(chapters, chapter)
	}

	// Each chapter ends where the next starts; the last at the file duration.
	types.FillChapterEndTimes(chapters, fileDuration)

	return chapters, nil
}

// Format: trak -> tref -> chap references a text track with chapter names.
func parseQuickTimeChapters(ctx context.Context, sr *binary.SafeReader, moovAtom *Atom, fileDuration time.Duration) ([]types.Chapter, error) {
	// Step 1: Find the chapter track reference
	chapterTrackID := findChapterTrackReference(ctx, sr, moovAtom)
	if chapterTrackID == 0 {
		return nil, nil
	}

	// Step 2: Find the chapter track by ID
	chapterTrak := findTrackByID(ctx, sr, moovAtom, chapterTrackID)
	if chapterTrak == nil {
		return nil, nil
	}

	// Step 3: Parse the text track
	return parseTextTrackChapters(ctx, sr, chapterTrak, fileDuration)
}

// findChapterTrackReference finds the tref->chap atom and returns the chapter track ID.
func findChapterTrackReference(ctx context.Context, sr *binary.SafeReader, moovAtom *Atom) uint32 {
	offset := moovAtom.DataOffset()
	end := offset + moovAtom.DataSize()

	for i := 0; offset < end; i++ {
		if err := binary.Canceled(ctx, i); err != nil {
			return 0
		}
		trakAtom, err := readAtomHeader(sr, offset, end)
		if err != nil {
			break
		}

		if trakAtom.Type == atomTrak {
			trackID := extractChapterTrackID(ctx, sr, trakAtom)
			if trackID != 0 {
				return trackID
			}
		}

		offset += trakAtom.Size
	}

	return 0
}

// extractChapterTrackID reads the chapter track ID from tref->chap if present.
func extractChapterTrackID(ctx context.Context, sr *binary.SafeReader, trakAtom *Atom) uint32 {
	trefAtom, err := trakAtom.child(ctx, sr, "tref")
	if err != nil {
		return 0
	}

	chapAtom, err := trefAtom.child(ctx, sr, "chap")
	if err != nil || chapAtom.DataSize() < 4 {
		return 0
	}

	trackID, err := sr.Read[uint32](chapAtom.DataOffset(), "chapter track ID")
	if err != nil {
		return 0
	}

	return trackID
}

// findTrackByID finds a trak atom with the specified track ID.
func findTrackByID(ctx context.Context, sr *binary.SafeReader, moovAtom *Atom, targetID uint32) *Atom {
	offset := moovAtom.DataOffset()
	end := offset + moovAtom.DataSize()

	for i := 0; offset < end; i++ {
		if err := binary.Canceled(ctx, i); err != nil {
			return nil
		}
		trakAtom, err := readAtomHeader(sr, offset, end)
		if err != nil {
			break
		}

		if trakAtom.Type == atomTrak {
			trackID := readTrackID(ctx, sr, trakAtom)
			if trackID == targetID {
				return trakAtom
			}
		}

		offset += trakAtom.Size
	}

	return nil
}

// readTrackID reads the track ID from a trak atom's tkhd.
func readTrackID(ctx context.Context, sr *binary.SafeReader, trakAtom *Atom) uint32 {
	tkhdAtom, err := trakAtom.child(ctx, sr, "tkhd")
	if err != nil {
		return 0
	}

	tkhdOffset := tkhdAtom.DataOffset()
	version, err := sr.Read[uint8](tkhdOffset, "tkhd version")
	if err != nil {
		return 0
	}

	var trackIDOffset int64
	if version == 1 {
		trackIDOffset = tkhdOffset + 20 // 64-bit version
	} else {
		trackIDOffset = tkhdOffset + 12 // 32-bit version
	}

	trackID, err := sr.Read[uint32](trackIDOffset, "track ID")
	if err != nil {
		return 0
	}

	return trackID
}

// parseTextTrackChapters extracts chapter information from a text track. A
// missing table means no chapters. A table cut short still yields the chapters
// it describes, and the returned error says which tables were cut.
func parseTextTrackChapters(ctx context.Context, sr *binary.SafeReader, trakAtom *Atom, fileDuration time.Duration) ([]types.Chapter, error) {
	// Find mdia -> minf -> stbl (sample table)
	mdiaAtom, err := trakAtom.child(ctx, sr, "mdia")
	if errors.Is(err, errAtomNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	stblAtom, err := mdiaAtom.path(ctx, sr, "minf", "stbl")
	if errors.Is(err, errAtomNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	tables, cuts, err := readChapterTables(ctx, sr, stblAtom, parseTrackTimescale(ctx, sr, mdiaAtom))
	if errors.Is(err, errAtomNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(tables.chunkOffsets) == 0 || len(tables.sampleSizes) == 0 {
		return nil, errors.Join(cuts...)
	}
	sampleOffsets := computeSampleOffsets(tables.sampleSizes, tables.chunkOffsets, tables.stsc)
	chapterTimes, sampleSizes := tables.times, tables.sampleSizes

	// Build chapters from text samples
	chapters := buildChaptersFromText(ctx, sr, chapterTimes, sampleSizes, sampleOffsets)
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Calculate end times
	types.FillChapterEndTimes(chapters, fileDuration)

	return chapters, errors.Join(cuts...)
}

// chapterTables holds the sample tables of a chapter text track.
type chapterTables struct {
	times        []time.Duration
	sampleSizes  []uint32
	chunkOffsets []uint64
	stsc         []stscEntry
}

// readChapterTables reads the tables of a chapter track's stbl. A table cut
// short still contributes the entries it holds, and the damage is returned in
// cuts. A missing stts, stsz or chunk table is an error wrapping errAtomNotFound.
// Without a usable stsc the legacy layout is used, and the damage is reported.
func readChapterTables(ctx context.Context, sr *binary.SafeReader, stblAtom *Atom, timescale uint32) (chapterTables, []error, error) {
	var t chapterTables
	var cuts []error
	// cut records a table cut short, which the chapters are built from anyway,
	// and returns any other error.
	cut := func(err error) error {
		if errors.Is(err, errTruncatedTable) {
			cuts = append(cuts, err)
			return nil
		}
		return err
	}

	var err error
	t.times, err = parseChapterTimings(ctx, sr, stblAtom, timescale, chapterSampleBound(ctx, sr, stblAtom))
	if err := cut(err); err != nil {
		return t, cuts, err
	}
	t.sampleSizes, err = parseSampleSizes(ctx, sr, stblAtom, len(t.times))
	if err := cut(err); err != nil {
		return t, cuts, err
	}
	t.chunkOffsets, err = parseChunkOffsets(ctx, sr, stblAtom, len(t.sampleSizes))
	if err := cut(err); err != nil {
		return t, cuts, err
	}

	t.stsc, err = parseSampleToChunk(ctx, sr, stblAtom)
	if err != nil {
		cuts = append(cuts, err)
		if !errors.Is(err, errTruncatedTable) {
			t.stsc = nil
		}
	}
	return t, cuts, nil
}

// defaultTimescale is the media timescale assumed for a chapter track whose
// mdhd is absent or unusable.
const defaultTimescale = 1000

// parseTrackTimescale returns the timescale of mdiaAtom's media header, or
// defaultTimescale when there is none or it is 0.
func parseTrackTimescale(ctx context.Context, sr *binary.SafeReader, mdiaAtom *Atom) uint32 {
	if timescale, ok := mdhdTimescale(ctx, sr, mdiaAtom); ok && timescale != 0 {
		return timescale
	}
	return defaultTimescale
}

// maxChapterSamples bounds the timings accumulated from stts; real audiobooks
// have thousands of chapters, never a million.
const maxChapterSamples = 1 << 20

// chapterSampleBound returns how many samples the chapter track's stsz can
// describe in this file, at most maxChapterSamples: the entries a per-sample
// table holds, or, for a uniform sample size, as many samples as the file has
// room for. Every chapter sample takes at least 2 bytes, its title length, so
// a missing or unreadable stsz gives as many samples as the file has room for;
// its readers report it.
func chapterSampleBound(ctx context.Context, sr *binary.SafeReader, stblAtom *Atom) int {
	fileRoom := int(min(sr.Size()/2, maxChapterSamples))
	stszAtom, err := stblAtom.child(ctx, sr, "stsz")
	if err != nil || stszAtom.DataSize() < 12 {
		return fileRoom
	}
	defaultSize, err := sr.Read[uint32](stszAtom.DataOffset()+4, "default sample size")
	if err != nil {
		return fileRoom
	}
	count, err := sr.Read[uint32](stszAtom.DataOffset()+8, "sample count")
	if err != nil {
		return fileRoom
	}

	room := (stszAtom.DataSize() - 12) / 4 // entries of a per-sample table
	if defaultSize != 0 {
		room = sr.Size() / int64(max(defaultSize, 2))
	}
	return int(min(int64(count), room, maxChapterSamples))
}

// parseChapterTimings extracts chapter start times from the stts atom, at most
// maxSamples of them. A table that holds fewer entries than it declares returns
// the times it does hold, with errTruncatedTable.
func parseChapterTimings(ctx context.Context, sr *binary.SafeReader, stblAtom *Atom, timescale uint32, maxSamples int) ([]time.Duration, error) {
	sttsAtom, err := stblAtom.child(ctx, sr, "stts")
	if err != nil {
		return nil, err
	}

	if timescale == 0 {
		return nil, nil
	}

	// version and flags (4), entry count (4), then entries of 8 bytes.
	if sttsAtom.DataSize() < 8 {
		return nil, corrupted(sr, sttsAtom.Offset, "stts atom too short")
	}
	offset := sttsAtom.DataOffset() + 4 // Skip version + flags
	entryCount, err := sr.Read[uint32](offset, "stts entry count")
	if err != nil {
		return nil, err
	}
	offset += 4

	var currentTime uint64
	chapterTimes := []time.Duration{}

	held := (sttsAtom.DataSize() - 8) / 8
	for i := range min(int64(entryCount), held) {
		if err := binary.Canceled(ctx, int(i)); err != nil {
			return nil, err
		}

		sampleCount, err := sr.Read[uint32](offset, "sample count")
		if err != nil {
			return chapterTimes, fmt.Errorf("stts entry %d: %w: %w", i, errTruncatedTable, err)
		}
		sampleDuration, err := sr.Read[uint32](offset+4, "sample duration")
		if err != nil {
			return chapterTimes, fmt.Errorf("stts entry %d: %w: %w", i, errTruncatedTable, err)
		}
		offset += 8

		for range sampleCount {
			if len(chapterTimes) >= maxSamples {
				return chapterTimes, nil // Bounded: stop accumulating on pathological input.
			}
			chapterTimes = append(chapterTimes, mvhdDuration(currentTime, timescale))
			currentTime += uint64(sampleDuration)
		}
	}

	if int64(entryCount) > held {
		return chapterTimes, truncatedTable("stts", held, int64(entryCount))
	}
	return chapterTimes, nil
}

// parseSampleSizes extracts sample sizes from the stsz atom, at most maxSamples
// of them. A table that holds fewer than that returns the sizes it does hold,
// with errTruncatedTable.
func parseSampleSizes(ctx context.Context, sr *binary.SafeReader, stblAtom *Atom, maxSamples int) ([]uint32, error) {
	stszAtom, err := stblAtom.child(ctx, sr, "stsz")
	if err != nil {
		return nil, err
	}

	// version and flags (4), default size (4), count (4), then entries of 4 bytes.
	if stszAtom.DataSize() < 12 {
		return nil, corrupted(sr, stszAtom.Offset, "stsz atom too short")
	}
	offset := stszAtom.DataOffset() + 4 // Skip version + flags

	// Default sample size: if non-zero every sample has this size and no table follows
	// (ISO/IEC 14496-12 section 8.7.3).
	defaultSize, err := sr.Read[uint32](offset, "default sample size")
	if err != nil {
		return nil, err
	}
	offset += 4

	sampleCount, err := sr.Read[uint32](offset, "sample count")
	if err != nil {
		return nil, err
	}
	offset += 4

	// Only samples that have a timing can become chapters.
	n := max(maxSamples, 0)
	if uint64(sampleCount) < uint64(n) {
		n = int(sampleCount)
	}
	if defaultSize != 0 {
		return slices.Repeat([]uint32{defaultSize}, n), nil
	}

	held := int((stszAtom.DataSize() - 12) / 4)
	sampleSizes := make([]uint32, min(n, held))
	for i := range sampleSizes {
		if err := binary.Canceled(ctx, i); err != nil {
			return nil, err
		}

		size, err := sr.Read[uint32](offset, "sample size")
		if err != nil {
			return sampleSizes[:i], fmt.Errorf("stsz entry %d: %w: %w", i, errTruncatedTable, err)
		}
		sampleSizes[i] = size
		offset += 4
	}

	if n > held {
		return sampleSizes, truncatedTable("stsz", int64(held), int64(n))
	}
	return sampleSizes, nil
}

// stscEntry is one sample-to-chunk run (ISO/IEC 14496-12 section 8.7.4).
type stscEntry struct {
	firstChunk      uint32 // 1-based
	samplesPerChunk uint32
}

// parseSampleToChunk reads stsc; returns nil, nil when the atom is absent. A
// table that holds fewer entries than it declares returns the entries it does
// hold, with errTruncatedTable.
func parseSampleToChunk(ctx context.Context, sr *binary.SafeReader, stblAtom *Atom) ([]stscEntry, error) {
	stscAtom, err := stblAtom.child(ctx, sr, "stsc")
	if errors.Is(err, errAtomNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	// version and flags (4), entry count (4), then entries of 12 bytes.
	if stscAtom.DataSize() < 8 {
		return nil, corrupted(sr, stscAtom.Offset, "stsc atom too short")
	}
	offset := stscAtom.DataOffset() + 4 // Skip version + flags
	entryCount, err := sr.Read[uint32](offset, "stsc entry count")
	if err != nil {
		return nil, err
	}
	offset += 4

	var entries []stscEntry
	held := (stscAtom.DataSize() - 8) / 12
	for i := range min(int64(entryCount), held) {
		if err := binary.Canceled(ctx, int(i)); err != nil {
			return nil, err
		}

		firstChunk, err := sr.Read[uint32](offset, "stsc first chunk")
		if err != nil {
			return entries, fmt.Errorf("stsc entry %d: %w: %w", i, errTruncatedTable, err)
		}
		perChunk, err := sr.Read[uint32](offset+4, "stsc samples per chunk")
		if err != nil {
			return entries, fmt.Errorf("stsc entry %d: %w: %w", i, errTruncatedTable, err)
		}
		entries = append(entries, stscEntry{firstChunk: firstChunk, samplesPerChunk: perChunk})
		offset += 12 // skip sample_description_index
	}

	if int64(entryCount) > held {
		return entries, truncatedTable("stsc", held, int64(entryCount))
	}
	return entries, nil
}

// computeSampleOffsets lays samples out in chunks per stsc. Without stsc it falls back to the
// legacy layout: one chunk => all samples contiguous; otherwise one sample per chunk.
// The result may be shorter than sampleSizes when the tables describe fewer samples.
func computeSampleOffsets(sampleSizes []uint32, chunkOffsets []uint64, stsc []stscEntry) []uint64 {
	if len(chunkOffsets) == 0 || len(sampleSizes) == 0 {
		return nil
	}

	if len(stsc) == 0 {
		if len(chunkOffsets) == 1 {
			offsets := make([]uint64, len(sampleSizes))
			pos := chunkOffsets[0]
			for i, size := range sampleSizes {
				offsets[i] = pos
				pos += uint64(size)
			}
			return offsets
		}
		return slices.Clone(chunkOffsets[:min(len(chunkOffsets), len(sampleSizes))])
	}

	offsets := make([]uint64, 0, len(sampleSizes))
	e := 0
	for c, chunkStart := range chunkOffsets {
		chunkNum := uint64(c) + 1
		for e+1 < len(stsc) && uint64(stsc[e+1].firstChunk) <= chunkNum {
			e++
		}
		pos := chunkStart
		for range stsc[e].samplesPerChunk {
			if len(offsets) == len(sampleSizes) {
				return offsets
			}
			offsets = append(offsets, pos)
			pos += uint64(sampleSizes[len(offsets)-1])
		}
	}

	return offsets
}

// parseChunkOffsets extracts chunk offsets from stco or co64 atom. It reads at
// most maxChunks entries, and no more than the atom holds, whatever count the
// atom declares: the caller needs one chunk per sample at most. An atom that
// holds fewer entries than it declares (up to maxChunks) returns the offsets it
// does hold, with errTruncatedTable.
func parseChunkOffsets(ctx context.Context, sr *binary.SafeReader, stblAtom *Atom, maxChunks int) ([]uint64, error) {
	stcoAtom, err := stblAtom.child(ctx, sr, "stco")
	if errors.Is(err, errAtomNotFound) {
		// Try co64 for 64-bit offsets
		stcoAtom, err = stblAtom.child(ctx, sr, "co64")
	}
	if err != nil {
		return nil, err
	}

	// version and flags (4), entry count (4), then the entries.
	if stcoAtom.DataSize() < 8 {
		return nil, corrupted(sr, stcoAtom.Offset, stcoAtom.Type+" atom too short")
	}
	offset := stcoAtom.DataOffset() + 4 // Skip version + flags
	chunkCount, err := sr.Read[uint32](offset, "chunk count")
	if err != nil {
		return nil, err
	}
	offset += 4

	elemSize := int64(4)
	if stcoAtom.Type == "co64" {
		elemSize = 8
	}
	// The table cannot hold more entries than its atom, and nothing past the
	// samples needed is read, so the slice is sized by neither a hostile count
	// nor a table that merely fits in the file.
	held := (stcoAtom.DataSize() - 8) / elemSize
	want := min(int64(chunkCount), int64(max(maxChunks, 0)))
	n := min(want, held)
	chunkOffsets := make([]uint64, n)
	for i := range n {
		if err := binary.Canceled(ctx, int(i)); err != nil {
			return nil, err
		}

		var readErr error
		if elemSize == 8 {
			chunkOffsets[i], readErr = sr.Read[uint64](offset, "chunk offset")
		} else {
			var offset32 uint32
			offset32, readErr = sr.Read[uint32](offset, "chunk offset")
			chunkOffsets[i] = uint64(offset32)
		}
		if readErr != nil {
			return chunkOffsets[:i], fmt.Errorf("%s entry %d: %w: %w", stcoAtom.Type, i, errTruncatedTable, readErr)
		}
		offset += elemSize
	}

	if n < want {
		return chunkOffsets, truncatedTable(stcoAtom.Type, held, int64(chunkCount))
	}
	return chunkOffsets, nil
}

// buildChaptersFromText reads text samples and builds chapter list.
// sampleOffsets[i] is the file offset of sample i (see computeSampleOffsets); samples
// without an offset yield a chapter with an empty title.
func buildChaptersFromText(ctx context.Context, sr *binary.SafeReader, chapterTimes []time.Duration, sampleSizes []uint32, sampleOffsets []uint64) []types.Chapter {
	n := min(len(sampleSizes), len(chapterTimes))
	chapters := make([]types.Chapter, 0, n)

	for i := range n {
		if binary.Canceled(ctx, i) != nil {
			return chapters // Partial; the caller observes ctx.Err().
		}

		sampleSize := sampleSizes[i]
		if sampleSize == 0 || sampleSize >= 10000 {
			continue // Skip invalid sizes
		}

		var title string
		if i < len(sampleOffsets) && sampleOffsets[i] <= math.MaxInt64 {
			title = extractChapterTitle(sr, int64(sampleOffsets[i]), sampleSize)
		}

		chapters = append(chapters, types.Chapter{
			Index:     len(chapters) + 1,
			Title:     title,
			StartTime: chapterTimes[i],
			Source:    types.ChapterSourceQuickTime,
		})
	}

	return chapters
}

// extractChapterTitle reads and decodes a chapter title from a text sample.
func extractChapterTitle(sr *binary.SafeReader, chunkOffset int64, sampleSize uint32) string {
	textBuf := make([]byte, sampleSize)
	if err := sr.ReadAt(textBuf, chunkOffset, "chapter text"); err != nil {
		return ""
	}

	// Text samples have a 2-byte length prefix
	if sampleSize < 2 {
		return ""
	}

	textLen := int(textBuf[0])<<8 | int(textBuf[1])
	if textLen <= 0 || textLen > len(textBuf)-2 {
		return ""
	}

	return decodeChapterText(textBuf[2 : 2+textLen])
}

// decodeChapterText returns a QuickTime text sample's text: UTF-16 in the byte
// order its BOM gives when it starts with one, otherwise the bytes as stored.
func decodeChapterText(text []byte) string {
	if len(text) >= 2 && len(text)%2 == 0 {
		var order stdbinary.ByteOrder
		switch {
		case text[0] == 0xFE && text[1] == 0xFF:
			order = stdbinary.BigEndian
		case text[0] == 0xFF && text[1] == 0xFE:
			order = stdbinary.LittleEndian
		}
		if order != nil {
			units := make([]uint16, 0, len(text)/2-1)
			for i := 2; i < len(text); i += 2 {
				units = append(units, order.Uint16(text[i:]))
			}
			return string(utf16.Decode(units))
		}
	}
	return string(text)
}
