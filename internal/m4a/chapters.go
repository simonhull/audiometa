package m4a

import (
	"context"
	"errors"
	"math"
	"slices"
	"time"

	"github.com/simonhull/audiometa/internal/binary"
	"github.com/simonhull/audiometa/internal/types"
)

// Tries in order: QuickTime chapter tracks (tref) -> Nero chapters (chpl).
func parseChapters(ctx context.Context, sr *binary.SafeReader, moovAtom *Atom, fileDuration time.Duration) ([]types.Chapter, error) {
	// Try QuickTime chapter tracks first (most common in professional audiobooks)
	qtChapters, qtErr := parseQuickTimeChapters(ctx, sr, moovAtom, fileDuration)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if qtErr == nil && len(qtChapters) > 0 {
		return qtChapters, nil
	}

	// Fall back to Nero chpl format
	chplChapters, chplErr := parseChplChapters(sr, moovAtom, fileDuration)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if chplErr == nil && len(chplChapters) > 0 {
		return chplChapters, nil
	}

	// If we got partial results, return them
	if len(chplChapters) > 0 {
		return chplChapters, nil
	}
	if len(qtChapters) > 0 {
		return qtChapters, nil
	}

	// If both formats failed with errors (not just "not found"), return the error
	if qtErr != nil && chplErr != nil {
		return nil, qtErr // Return first error as representative
	}

	// No chapters found (not an error)
	return nil, nil
}

// parseChplChapters extracts chapter markers from the chpl atom (Nero format).
func parseChplChapters(sr *binary.SafeReader, moovAtom *Atom, fileDuration time.Duration) ([]types.Chapter, error) {
	// Find udta atom (user data)
	// Path: moov -> udta
	udtaAtom, err := findAtom(sr, moovAtom.DataOffset(), moovAtom.DataOffset()+moovAtom.DataSize(), "udta")
	if err != nil {
		// No udta - no chapters (not an error, just means no chapters)
		return nil, nil
	}

	// Find chpl atom (chapter list)
	// Path: udta -> chpl
	chplAtom, err := findAtom(sr, udtaAtom.DataOffset(), udtaAtom.DataOffset()+udtaAtom.DataSize(), "chpl")
	if err != nil {
		// No chpl - no chapters (not an error, just means no chapters)
		return nil, nil
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
	for i := range chapterCount {
		// Read start time (8 bytes, in 100-nanosecond units)
		startTime100ns, err := sr.Read[uint64](offset, "chapter start time")
		if err != nil {
			return nil, err
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
			return nil, err
		}
		offset++

		// Read title (N bytes)
		var title string
		if titleLen > 0 {
			titleBytes := make([]byte, titleLen)
			if err := sr.ReadAt(titleBytes, offset, "chapter title"); err != nil {
				return nil, err
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
			trackID := extractChapterTrackID(sr, trakAtom)
			if trackID != 0 {
				return trackID
			}
		}

		offset += trakAtom.Size
	}

	return 0
}

// extractChapterTrackID reads the chapter track ID from tref->chap if present.
func extractChapterTrackID(sr *binary.SafeReader, trakAtom *Atom) uint32 {
	trefAtom, err := findAtom(sr, trakAtom.DataOffset(), trakAtom.DataOffset()+trakAtom.DataSize(), "tref")
	if err != nil {
		return 0
	}

	chapAtom, err := findAtom(sr, trefAtom.DataOffset(), trefAtom.DataOffset()+trefAtom.DataSize(), "chap")
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
			trackID := readTrackID(sr, trakAtom)
			if trackID == targetID {
				return trakAtom
			}
		}

		offset += trakAtom.Size
	}

	return nil
}

// readTrackID reads the track ID from a trak atom's tkhd.
func readTrackID(sr *binary.SafeReader, trakAtom *Atom) uint32 {
	tkhdAtom, err := findAtom(sr, trakAtom.DataOffset(), trakAtom.DataOffset()+trakAtom.DataSize(), "tkhd")
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

// parseTextTrackChapters extracts chapter information from a text track.
func parseTextTrackChapters(ctx context.Context, sr *binary.SafeReader, trakAtom *Atom, fileDuration time.Duration) ([]types.Chapter, error) {
	// Find mdia -> minf -> stbl (sample table)
	mdiaAtom, err := findAtom(sr, trakAtom.DataOffset(), trakAtom.DataOffset()+trakAtom.DataSize(), "mdia")
	if err != nil {
		return nil, err
	}

	minfAtom, err := findAtom(sr, mdiaAtom.DataOffset(), mdiaAtom.DataOffset()+mdiaAtom.DataSize(), "minf")
	if err != nil {
		return nil, err
	}

	stblAtom, err := findAtom(sr, minfAtom.DataOffset(), minfAtom.DataOffset()+minfAtom.DataSize(), "stbl")
	if err != nil {
		return nil, err
	}

	// Extract timescale
	timescale, err := parseTrackTimescale(sr, mdiaAtom)
	if err != nil {
		return nil, err
	}

	// Parse chapter timings
	chapterTimes, err := parseChapterTimings(ctx, sr, stblAtom, timescale)
	if err != nil {
		return nil, err
	}

	// Parse sample sizes
	sampleSizes, err := parseSampleSizes(ctx, sr, stblAtom, len(chapterTimes))
	if err != nil {
		return nil, err
	}

	// Parse chunk offsets
	chunkOffsets, err := parseChunkOffsets(ctx, sr, stblAtom)
	if err != nil {
		return nil, err
	}

	if len(chunkOffsets) == 0 || len(sampleSizes) == 0 {
		return nil, nil
	}

	// Map samples to chunks (non-fatal if stsc is unreadable: legacy layout is used)
	stsc, err := parseSampleToChunk(ctx, sr, stblAtom)
	if err != nil {
		stsc = nil
	}
	sampleOffsets := computeSampleOffsets(sampleSizes, chunkOffsets, stsc)

	// Build chapters from text samples
	chapters := buildChaptersFromText(ctx, sr, chapterTimes, sampleSizes, sampleOffsets)
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Calculate end times
	types.FillChapterEndTimes(chapters, fileDuration)

	return chapters, nil
}

// parseTrackTimescale extracts the timescale from the mdhd atom.
func parseTrackTimescale(sr *binary.SafeReader, mdiaAtom *Atom) (uint32, error) {
	mdhdAtom, err := findAtom(sr, mdiaAtom.DataOffset(), mdiaAtom.DataOffset()+mdiaAtom.DataSize(), "mdhd")
	if err != nil {
		return 0, err
	}

	mdhdOffset := mdhdAtom.DataOffset()
	version, err := sr.Read[uint8](mdhdOffset, "mdhd version")
	if err != nil {
		return 1000, nil
	}

	var timescale uint32
	if version == 1 {
		timescale, err = sr.Read[uint32](mdhdOffset+20, "timescale")
	} else {
		timescale, err = sr.Read[uint32](mdhdOffset+12, "timescale")
	}

	if err != nil || timescale == 0 {
		return 1000, nil
	}

	return timescale, nil
}

// maxChapterSamples bounds the timings accumulated from stts; real audiobooks
// have thousands of chapters, never a million.
const maxChapterSamples = 1 << 20

// parseChapterTimings extracts chapter start times from the stts atom.
func parseChapterTimings(ctx context.Context, sr *binary.SafeReader, stblAtom *Atom, timescale uint32) ([]time.Duration, error) {
	sttsAtom, err := findAtom(sr, stblAtom.DataOffset(), stblAtom.DataOffset()+stblAtom.DataSize(), "stts")
	if err != nil {
		return nil, err
	}

	if timescale == 0 {
		return nil, nil
	}

	offset := sttsAtom.DataOffset() + 4 // Skip version + flags
	entryCount, err := sr.Read[uint32](offset, "stts entry count")
	if err != nil {
		return nil, err
	}
	offset += 4

	var currentTime uint64
	chapterTimes := []time.Duration{}

	for i := range entryCount {
		if err := binary.Canceled(ctx, int(i)); err != nil {
			return nil, err
		}

		sampleCount, err := sr.Read[uint32](offset, "sample count")
		if err != nil {
			break // Return partial results
		}
		sampleDuration, err := sr.Read[uint32](offset+4, "sample duration")
		if err != nil {
			break // Return partial results
		}
		offset += 8

		for range sampleCount {
			if len(chapterTimes) >= maxChapterSamples {
				return chapterTimes, nil // Bounded: stop accumulating on pathological input.
			}
			chapterTimes = append(chapterTimes, mvhdDuration(currentTime, timescale))
			currentTime += uint64(sampleDuration)
		}
	}

	return chapterTimes, nil
}

// parseSampleSizes extracts sample sizes from the stsz atom.
func parseSampleSizes(ctx context.Context, sr *binary.SafeReader, stblAtom *Atom, maxSamples int) ([]uint32, error) {
	stszAtom, err := findAtom(sr, stblAtom.DataOffset(), stblAtom.DataOffset()+stblAtom.DataSize(), "stsz")
	if err != nil {
		return nil, err
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

	sampleSizes := make([]uint32, n)
	for i := range sampleSizes {
		if err := binary.Canceled(ctx, i); err != nil {
			return nil, err
		}

		size, err := sr.Read[uint32](offset, "sample size")
		if err != nil {
			break // Return partial results
		}
		sampleSizes[i] = size
		offset += 4
	}

	return sampleSizes, nil
}

// stscEntry is one sample-to-chunk run (ISO/IEC 14496-12 section 8.7.4).
type stscEntry struct {
	firstChunk      uint32 // 1-based
	samplesPerChunk uint32
}

// parseSampleToChunk reads stsc; returns nil, nil when the atom is absent.
func parseSampleToChunk(ctx context.Context, sr *binary.SafeReader, stblAtom *Atom) ([]stscEntry, error) {
	stscAtom, err := findAtom(sr, stblAtom.DataOffset(), stblAtom.DataOffset()+stblAtom.DataSize(), "stsc")
	if err != nil {
		if errors.Is(err, errAtomNotFound) {
			return nil, nil
		}
		return nil, err
	}

	offset := stscAtom.DataOffset() + 4 // Skip version + flags
	entryCount, err := sr.Read[uint32](offset, "stsc entry count")
	if err != nil {
		return nil, err
	}
	offset += 4

	var entries []stscEntry
	for i := range entryCount {
		if err := binary.Canceled(ctx, int(i)); err != nil {
			return nil, err
		}

		firstChunk, err := sr.Read[uint32](offset, "stsc first chunk")
		if err != nil {
			break // Return partial results
		}
		perChunk, err := sr.Read[uint32](offset+4, "stsc samples per chunk")
		if err != nil {
			break
		}
		entries = append(entries, stscEntry{firstChunk: firstChunk, samplesPerChunk: perChunk})
		offset += 12 // skip sample_description_index
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

// parseChunkOffsets extracts chunk offsets from stco or co64 atom.
func parseChunkOffsets(ctx context.Context, sr *binary.SafeReader, stblAtom *Atom) ([]uint64, error) {
	stcoAtom, err := findAtom(sr, stblAtom.DataOffset(), stblAtom.DataOffset()+stblAtom.DataSize(), "stco")
	if err != nil {
		// Try co64 for 64-bit offsets
		stcoAtom, err = findAtom(sr, stblAtom.DataOffset(), stblAtom.DataOffset()+stblAtom.DataSize(), "co64")
		if err != nil {
			return nil, err
		}
	}

	offset := stcoAtom.DataOffset() + 4 // Skip version + flags
	chunkCount, err := sr.Read[uint32](offset, "chunk count")
	if err != nil {
		return nil, err
	}
	offset += 4

	elemSize := 4
	if stcoAtom.Type == "co64" {
		elemSize = 8
	}
	if err := sr.CheckLen(offset, int(chunkCount), elemSize, "chunk table"); err != nil {
		return nil, err
	}
	chunkOffsets := make([]uint64, chunkCount)
	for i := range chunkCount {
		if err := binary.Canceled(ctx, int(i)); err != nil {
			return nil, err
		}

		var readErr error
		if stcoAtom.Type == "co64" {
			chunkOffsets[i], readErr = sr.Read[uint64](offset, "chunk offset")
			offset += 8
		} else {
			var offset32 uint32
			offset32, readErr = sr.Read[uint32](offset, "chunk offset")
			chunkOffsets[i] = uint64(offset32)
			offset += 4
		}
		if readErr != nil {
			break // Return partial results
		}
	}

	return chunkOffsets, nil
}

// buildChaptersFromText reads text samples and builds chapter list.
// sampleOffsets[i] is the file offset of sample i (see computeSampleOffsets); samples
// without an offset yield a chapter with an empty title.
func buildChaptersFromText(ctx context.Context, sr *binary.SafeReader, chapterTimes []time.Duration, sampleSizes []uint32, sampleOffsets []uint64) []types.Chapter {
	chapters := make([]types.Chapter, 0, len(chapterTimes))

	for i := range min(len(sampleSizes), len(chapterTimes)) {
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
			Index:     i + 1,
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

	return string(textBuf[2 : 2+textLen])
}
