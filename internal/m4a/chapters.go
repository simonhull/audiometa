package m4a

import (
	"time"

	"github.com/simonhull/audiometa/internal/binary"
	"github.com/simonhull/audiometa/internal/types"
)

// Tries in order: QuickTime chapter tracks (tref) -> Nero chapters (chpl).
func parseChapters(sr *binary.SafeReader, moovAtom *Atom, fileDuration time.Duration) ([]types.Chapter, error) {
	// Try QuickTime chapter tracks first (most common in professional audiobooks)
	qtChapters, qtErr := parseQuickTimeChapters(sr, moovAtom, fileDuration)
	if qtErr == nil && len(qtChapters) > 0 {
		return qtChapters, nil
	}

	// Fall back to Nero chpl format
	chplChapters, chplErr := parseChplChapters(sr, moovAtom, fileDuration)
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
	udtaAtom, err := findAtom(sr, moovAtom.DataOffset(), moovAtom.DataOffset()+int64(moovAtom.DataSize()), "udta")
	if err != nil {
		// No udta - no chapters (not an error, just means no chapters)
		return nil, nil //nolint:nilerr // Not an error, just means no chapters found
	}

	// Find chpl atom (chapter list)
	// Path: udta -> chpl
	chplAtom, err := findAtom(sr, udtaAtom.DataOffset(), udtaAtom.DataOffset()+int64(udtaAtom.DataSize()), "chpl")
	if err != nil {
		// No chpl - no chapters (not an error, just means no chapters)
		return nil, nil //nolint:nilerr // Not an error, just means no chapters found
	}

	// Parse chpl atom
	offset := chplAtom.DataOffset()

	// Read version (1 byte)
	_, err = binary.Read[uint8](sr, offset, "chpl version")
	if err != nil {
		return nil, err
	}
	offset++

	// Skip flags (3 bytes)
	offset += 3

	// Skip reserved (4 bytes)
	offset += 4

	// Read chapter count (1 byte)
	chapterCount, err := binary.Read[uint8](sr, offset, "chapter count")
	if err != nil {
		return nil, err
	}
	offset++

	if chapterCount == 0 {
		return nil, nil
	}

	chapters := make([]types.Chapter, 0, chapterCount)

	// Read each chapter
	for i := uint8(0); i < chapterCount; i++ {
		// Read start time (8 bytes, in 100-nanosecond units)
		startTime100ns, err := binary.Read[uint64](sr, offset, "chapter start time")
		if err != nil {
			return nil, err
		}
		offset += 8

		// Convert to time.Duration (100-nanosecond units -> nanoseconds)
		startTime := time.Duration(startTime100ns * 100)

		// Read title length (1 byte)
		titleLen, err := binary.Read[uint8](sr, offset, "chapter title length")
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
		}

		chapters = append(chapters, chapter)
	}

	// Calculate end times
	// Each chapter ends where the next one starts
	// Last chapter ends at file duration
	for i := 0; i < len(chapters); i++ {
		if i < len(chapters)-1 {
			// End time is the start of the next chapter
			chapters[i].EndTime = chapters[i+1].StartTime
		} else {
			// Last chapter ends at file duration

			chapters[i].EndTime = fileDuration
		}
	}

	return chapters, nil
}

// Format: trak -> tref -> chap references a text track with chapter names.
func parseQuickTimeChapters(sr *binary.SafeReader, moovAtom *Atom, fileDuration time.Duration) ([]types.Chapter, error) {
	// Step 1: Find the chapter track reference
	chapterTrackID := findChapterTrackReference(sr, moovAtom)
	if chapterTrackID == 0 {
		return nil, nil
	}

	// Step 2: Find the chapter track by ID
	chapterTrak := findTrackByID(sr, moovAtom, chapterTrackID)
	if chapterTrak == nil {
		return nil, nil
	}

	// Step 3: Parse the text track
	return parseTextTrackChapters(sr, chapterTrak, fileDuration)
}

// findChapterTrackReference finds the tref->chap atom and returns the chapter track ID.
func findChapterTrackReference(sr *binary.SafeReader, moovAtom *Atom) uint32 {
	offset := moovAtom.DataOffset()
	end := offset + int64(moovAtom.DataSize())

	for offset < end {
		trakAtom, err := readAtomHeader(sr, offset)
		if err != nil {
			break
		}

		if trakAtom.Type == "trak" {
			trackID := extractChapterTrackID(sr, trakAtom)
			if trackID != 0 {
				return trackID
			}
		}

		offset += int64(trakAtom.Size)
	}

	return 0
}

// extractChapterTrackID reads the chapter track ID from tref->chap if present.
func extractChapterTrackID(sr *binary.SafeReader, trakAtom *Atom) uint32 {
	trefAtom, err := findAtom(sr, trakAtom.DataOffset(), trakAtom.DataOffset()+int64(trakAtom.DataSize()), "tref")
	if err != nil {
		return 0
	}

	chapAtom, err := findAtom(sr, trefAtom.DataOffset(), trefAtom.DataOffset()+int64(trefAtom.DataSize()), "chap")
	if err != nil || chapAtom.DataSize() < 4 {
		return 0
	}

	trackID, err := binary.Read[uint32](sr, chapAtom.DataOffset(), "chapter track ID")
	if err != nil {
		return 0
	}

	return trackID
}

// findTrackByID finds a trak atom with the specified track ID.
func findTrackByID(sr *binary.SafeReader, moovAtom *Atom, targetID uint32) *Atom {
	offset := moovAtom.DataOffset()
	end := offset + int64(moovAtom.DataSize())

	for offset < end {
		trakAtom, err := readAtomHeader(sr, offset)
		if err != nil {
			break
		}

		if trakAtom.Type == "trak" {
			trackID := readTrackID(sr, trakAtom)
			if trackID == targetID {
				return trakAtom
			}
		}

		offset += int64(trakAtom.Size)
	}

	return nil
}

// readTrackID reads the track ID from a trak atom's tkhd.
func readTrackID(sr *binary.SafeReader, trakAtom *Atom) uint32 {
	tkhdAtom, err := findAtom(sr, trakAtom.DataOffset(), trakAtom.DataOffset()+int64(trakAtom.DataSize()), "tkhd")
	if err != nil {
		return 0
	}

	tkhdOffset := tkhdAtom.DataOffset()
	version, err := binary.Read[uint8](sr, tkhdOffset, "tkhd version")
	if err != nil {
		return 0
	}

	var trackIDOffset int64
	if version == 1 {
		trackIDOffset = tkhdOffset + 20 // 64-bit version
	} else {
		trackIDOffset = tkhdOffset + 12 // 32-bit version
	}

	trackID, err := binary.Read[uint32](sr, trackIDOffset, "track ID")
	if err != nil {
		return 0
	}

	return trackID
}

// parseTextTrackChapters extracts chapter information from a text track.
func parseTextTrackChapters(sr *binary.SafeReader, trakAtom *Atom, fileDuration time.Duration) ([]types.Chapter, error) {
	// Find mdia -> minf -> stbl (sample table)
	mdiaAtom, err := findAtom(sr, trakAtom.DataOffset(), trakAtom.DataOffset()+int64(trakAtom.DataSize()), "mdia")
	if err != nil {
		return nil, err
	}

	minfAtom, err := findAtom(sr, mdiaAtom.DataOffset(), mdiaAtom.DataOffset()+int64(mdiaAtom.DataSize()), "minf")
	if err != nil {
		return nil, err
	}

	stblAtom, err := findAtom(sr, minfAtom.DataOffset(), minfAtom.DataOffset()+int64(minfAtom.DataSize()), "stbl")
	if err != nil {
		return nil, err
	}

	// Extract timescale
	timescale, err := parseTrackTimescale(sr, mdiaAtom)
	if err != nil {
		return nil, err
	}

	// Parse chapter timings
	chapterTimes, err := parseChapterTimings(sr, stblAtom, timescale)
	if err != nil {
		return nil, err
	}

	// Parse sample sizes
	sampleSizes, err := parseSampleSizes(sr, stblAtom)
	if err != nil {
		return nil, err
	}

	// Parse chunk offsets
	chunkOffsets, err := parseChunkOffsets(sr, stblAtom)
	if err != nil {
		return nil, err
	}

	// Build chapters from text samples
	chapters := buildChaptersFromText(sr, chapterTimes, sampleSizes, chunkOffsets)

	// Calculate end times
	calculateChapterEndTimes(chapters, fileDuration)

	return chapters, nil
}

// parseTrackTimescale extracts the timescale from the mdhd atom.
func parseTrackTimescale(sr *binary.SafeReader, mdiaAtom *Atom) (uint32, error) {
	mdhdAtom, err := findAtom(sr, mdiaAtom.DataOffset(), mdiaAtom.DataOffset()+int64(mdiaAtom.DataSize()), "mdhd")
	if err != nil {
		return 0, err
	}

	mdhdOffset := mdhdAtom.DataOffset()
	version, err := binary.Read[uint8](sr, mdhdOffset, "mdhd version")
	if err != nil {
		return 1000, nil //nolint:nilerr // Default fallback on read failure
	}

	var timescale uint32
	if version == 1 {
		timescale, err = binary.Read[uint32](sr, mdhdOffset+20, "timescale")
	} else {
		timescale, err = binary.Read[uint32](sr, mdhdOffset+12, "timescale")
	}

	if err != nil || timescale == 0 {
		return 1000, nil //nolint:nilerr // Default fallback on read failure or zero timescale
	}

	return timescale, nil
}

// parseChapterTimings extracts chapter start times from the stts atom.
func parseChapterTimings(sr *binary.SafeReader, stblAtom *Atom, timescale uint32) ([]time.Duration, error) {
	sttsAtom, err := findAtom(sr, stblAtom.DataOffset(), stblAtom.DataOffset()+int64(stblAtom.DataSize()), "stts")
	if err != nil {
		return nil, err
	}

	offset := sttsAtom.DataOffset() + 4 // Skip version + flags
	entryCount, err := binary.Read[uint32](sr, offset, "stts entry count")
	if err != nil {
		return nil, err
	}
	offset += 4

	var currentTime uint64
	chapterTimes := []time.Duration{}

	for i := uint32(0); i < entryCount; i++ {
		sampleCount, err := binary.Read[uint32](sr, offset, "sample count")
		if err != nil {
			break // Return partial results
		}
		sampleDuration, err := binary.Read[uint32](sr, offset+4, "sample duration")
		if err != nil {
			break // Return partial results
		}
		offset += 8

		for j := uint32(0); j < sampleCount; j++ {
			timeNs := (currentTime * 1_000_000_000) / uint64(timescale)
			chapterTimes = append(chapterTimes, time.Duration(timeNs))
			currentTime += uint64(sampleDuration)
		}
	}

	return chapterTimes, nil //nolint:nilerr // Partial results are acceptable
}

// parseSampleSizes extracts sample sizes from the stsz atom.
func parseSampleSizes(sr *binary.SafeReader, stblAtom *Atom) ([]uint32, error) {
	stszAtom, err := findAtom(sr, stblAtom.DataOffset(), stblAtom.DataOffset()+int64(stblAtom.DataSize()), "stsz")
	if err != nil {
		return nil, err
	}

	offset := stszAtom.DataOffset() + 4 // Skip version + flags

	// Read default sample size (if 0, sizes are in table)
	_, err = binary.Read[uint32](sr, offset, "default sample size")
	if err != nil {
		return nil, err
	}
	offset += 4

	sampleCount, err := binary.Read[uint32](sr, offset, "sample count")
	if err != nil {
		return nil, err
	}
	offset += 4

	sampleSizes := make([]uint32, sampleCount)
	for i := uint32(0); i < sampleCount; i++ {
		size, err := binary.Read[uint32](sr, offset, "sample size")
		if err != nil {
			break // Return partial results
		}
		sampleSizes[i] = size
		offset += 4
	}

	return sampleSizes, nil //nolint:nilerr // Partial results are acceptable
}

// parseChunkOffsets extracts chunk offsets from stco or co64 atom.
func parseChunkOffsets(sr *binary.SafeReader, stblAtom *Atom) ([]uint64, error) {
	stcoAtom, err := findAtom(sr, stblAtom.DataOffset(), stblAtom.DataOffset()+int64(stblAtom.DataSize()), "stco")
	if err != nil {
		// Try co64 for 64-bit offsets
		stcoAtom, err = findAtom(sr, stblAtom.DataOffset(), stblAtom.DataOffset()+int64(stblAtom.DataSize()), "co64")
		if err != nil {
			return nil, err
		}
	}

	offset := stcoAtom.DataOffset() + 4 // Skip version + flags
	chunkCount, err := binary.Read[uint32](sr, offset, "chunk count")
	if err != nil {
		return nil, err
	}
	offset += 4

	chunkOffsets := make([]uint64, chunkCount)
	for i := uint32(0); i < chunkCount; i++ {
		var readErr error
		if stcoAtom.Type == "co64" {
			chunkOffsets[i], readErr = binary.Read[uint64](sr, offset, "chunk offset")
			offset += 8
		} else {
			offset32, readErr := binary.Read[uint32](sr, offset, "chunk offset")
			if readErr != nil {
				break // Return partial results
			}
			chunkOffsets[i] = uint64(offset32)
			offset += 4
		}
		if readErr != nil {
			break // Return partial results
		}
	}

	return chunkOffsets, nil //nolint:nilerr // Partial results are acceptable
}

// buildChaptersFromText reads text samples and builds chapter list.
func buildChaptersFromText(sr *binary.SafeReader, chapterTimes []time.Duration, sampleSizes []uint32, chunkOffsets []uint64) []types.Chapter {
	chapters := make([]types.Chapter, 0, len(chapterTimes))
	maxSamples := min(len(chunkOffsets), len(sampleSizes), len(chapterTimes))

	for i := 0; i < maxSamples; i++ {
		sampleSize := sampleSizes[i]
		if sampleSize == 0 || sampleSize >= 10000 {
			continue // Skip invalid sizes
		}

		title := extractChapterTitle(sr, int64(chunkOffsets[i]), sampleSize)

		chapter := types.Chapter{
			Index:     i + 1,
			Title:     title,
			StartTime: chapterTimes[i],
		}
		chapters = append(chapters, chapter)
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

// calculateChapterEndTimes sets the EndTime for each chapter.
func calculateChapterEndTimes(chapters []types.Chapter, fileDuration time.Duration) {
	for i := 0; i < len(chapters); i++ {
		if i < len(chapters)-1 {
			chapters[i].EndTime = chapters[i+1].StartTime
		} else {
			chapters[i].EndTime = fileDuration
		}
	}
}
