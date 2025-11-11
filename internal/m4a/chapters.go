package m4a

import (
	"time"

	"github.com/simonhull/audiometa"
	"github.com/simonhull/audiometa/internal/binary"
)

// parseChapters extracts chapter markers using multiple formats
// Tries in order: QuickTime chapter tracks (tref) -> Nero chapters (chpl)
func parseChapters(sr *binary.SafeReader, moovAtom *Atom, fileDuration time.Duration) ([]audiometa.Chapter, error) {
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

	// Return whichever we got, even if empty
	if len(chplChapters) > 0 {
		return chplChapters, nil
	}
	if len(qtChapters) > 0 {
		return qtChapters, nil
	}

	// No chapters found
	return nil, nil
}

// parseChplChapters extracts chapter markers from the chpl atom (Nero format)
func parseChplChapters(sr *binary.SafeReader, moovAtom *Atom, fileDuration time.Duration) ([]audiometa.Chapter, error) {
	// Find udta atom (user data)
	// Path: moov -> udta
	udtaAtom, err := findAtom(sr, moovAtom.DataOffset(), moovAtom.DataOffset()+int64(moovAtom.DataSize()), "udta")
	if err != nil {
		// No udta - no chapters
		return nil, nil
	}

	// Find chpl atom (chapter list)
	// Path: udta -> chpl
	chplAtom, err := findAtom(sr, udtaAtom.DataOffset(), udtaAtom.DataOffset()+int64(udtaAtom.DataSize()), "chpl")
	if err != nil {
		// No chpl - no chapters
		return nil, nil
	}

	// Parse chpl atom
	offset := chplAtom.DataOffset()

	// Read version (1 byte)
	_, err = binary.Read[uint8](sr, offset, "chpl version")
	if err != nil {
		return nil, err
	}
	offset += 1

	// Skip flags (3 bytes)
	offset += 3

	// Skip reserved (4 bytes)
	offset += 4

	// Read chapter count (1 byte)
	chapterCount, err := binary.Read[uint8](sr, offset, "chapter count")
	if err != nil {
		return nil, err
	}
	offset += 1

	if chapterCount == 0 {
		return nil, nil
	}

	chapters := make([]audiometa.Chapter, 0, chapterCount)

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
		offset += 1

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

		chapter := audiometa.Chapter{
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

// parseQuickTimeChapters extracts chapters from QuickTime chapter tracks
// Format: trak -> tref -> chap references a text track with chapter names
func parseQuickTimeChapters(sr *binary.SafeReader, moovAtom *Atom, fileDuration time.Duration) ([]audiometa.Chapter, error) {
	// Find all trak atoms
	offset := moovAtom.DataOffset()
	end := offset + int64(moovAtom.DataSize())

	var chapterTrackID uint32

	// Step 1: Find the track with a tref -> chap reference
	for offset < end {
		trakAtom, err := readAtomHeader(sr, offset)
		if err != nil {
			break
		}

		if trakAtom.Type == "trak" {
			// Look for tref inside this trak
			trefAtom, err := findAtom(sr, trakAtom.DataOffset(), trakAtom.DataOffset()+int64(trakAtom.DataSize()), "tref")
			if err == nil {
				// Found tref, look for chap inside it
				chapAtom, err := findAtom(sr, trefAtom.DataOffset(), trefAtom.DataOffset()+int64(trefAtom.DataSize()), "chap")
				if err == nil && chapAtom.DataSize() >= 4 {
					// Read the chapter track ID (4 bytes)
					trackID, err := binary.Read[uint32](sr, chapAtom.DataOffset(), "chapter track ID")
					if err == nil {
						chapterTrackID = trackID
						break
					}
				}
			}
		}

		offset += int64(trakAtom.Size)
	}

	if chapterTrackID == 0 {
		// No chapter track reference found
		return nil, nil
	}

	// Step 2: Find the chapter track (text track with matching ID)
	offset = moovAtom.DataOffset()
	for offset < end {
		trakAtom, err := readAtomHeader(sr, offset)
		if err != nil {
			break
		}

		if trakAtom.Type == "trak" {
			// Check if this track's ID matches
			tkhdAtom, err := findAtom(sr, trakAtom.DataOffset(), trakAtom.DataOffset()+int64(trakAtom.DataSize()), "tkhd")
			if err == nil {
				// tkhd format: [version][flags][...][track_id at offset 12 or 20]
				tkhdOffset := tkhdAtom.DataOffset()
				version, _ := binary.Read[uint8](sr, tkhdOffset, "tkhd version")

				var trackIDOffset int64
				if version == 1 {
					trackIDOffset = tkhdOffset + 20 // 64-bit version
				} else {
					trackIDOffset = tkhdOffset + 12 // 32-bit version
				}

				trackID, err := binary.Read[uint32](sr, trackIDOffset, "track ID")
				if err == nil && trackID == chapterTrackID {
					// Found the chapter track! Parse its samples
					return parseTextTrackChapters(sr, trakAtom, fileDuration)
				}
			}
		}

		offset += int64(trakAtom.Size)
	}

	return nil, nil
}

// parseTextTrackChapters extracts chapter information from a text track
func parseTextTrackChapters(sr *binary.SafeReader, trakAtom *Atom, fileDuration time.Duration) ([]audiometa.Chapter, error) {
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

	// Get time scale from mdhd
	mdhdAtom, err := findAtom(sr, mdiaAtom.DataOffset(), mdiaAtom.DataOffset()+int64(mdiaAtom.DataSize()), "mdhd")
	if err != nil {
		return nil, err
	}

	mdhdOffset := mdhdAtom.DataOffset()
	version, _ := binary.Read[uint8](sr, mdhdOffset, "mdhd version")

	var timescale uint32
	if version == 1 {
		timescale, _ = binary.Read[uint32](sr, mdhdOffset+20, "timescale")
	} else {
		timescale, _ = binary.Read[uint32](sr, mdhdOffset+12, "timescale")
	}

	if timescale == 0 {
		timescale = 1000 // Default fallback
	}

	// Parse stts (time-to-sample) for chapter timings
	sttsAtom, err := findAtom(sr, stblAtom.DataOffset(), stblAtom.DataOffset()+int64(stblAtom.DataSize()), "stts")
	if err != nil {
		return nil, err
	}

	sttsOffset := sttsAtom.DataOffset() + 4 // Skip version + flags
	entryCount, err := binary.Read[uint32](sr, sttsOffset, "stts entry count")
	if err != nil {
		return nil, err
	}
	sttsOffset += 4

	// Read time-to-sample entries
	var currentTime uint64
	chapterTimes := []time.Duration{}

	for i := uint32(0); i < entryCount; i++ {
		sampleCount, _ := binary.Read[uint32](sr, sttsOffset, "sample count")
		sampleDuration, _ := binary.Read[uint32](sr, sttsOffset+4, "sample duration")
		sttsOffset += 8

		for j := uint32(0); j < sampleCount; j++ {
			// Convert to time.Duration
			timeNs := (currentTime * 1_000_000_000) / uint64(timescale)
			chapterTimes = append(chapterTimes, time.Duration(timeNs))
			currentTime += uint64(sampleDuration)
		}
	}

	// Parse stco/co64 (chunk offsets) and stsz (sample sizes) to read text
	stszAtom, err := findAtom(sr, stblAtom.DataOffset(), stblAtom.DataOffset()+int64(stblAtom.DataSize()), "stsz")
	if err != nil {
		return nil, err
	}

	stszOffset := stszAtom.DataOffset() + 4                   // Skip version + flags
	_, _ = binary.Read[uint32](sr, stszOffset, "sample size") // If 0, sizes are in the table
	stszOffset += 4
	sampleCount, err := binary.Read[uint32](sr, stszOffset, "sample count")
	if err != nil {
		return nil, err
	}
	stszOffset += 4

	// Read sample sizes
	sampleSizes := make([]uint32, sampleCount)
	for i := uint32(0); i < sampleCount; i++ {
		size, _ := binary.Read[uint32](sr, stszOffset, "sample size")
		sampleSizes[i] = size
		stszOffset += 4
	}

	// Find stco (chunk offsets)
	stcoAtom, err := findAtom(sr, stblAtom.DataOffset(), stblAtom.DataOffset()+int64(stblAtom.DataSize()), "stco")
	if err != nil {
		// Try co64 for 64-bit offsets
		stcoAtom, err = findAtom(sr, stblAtom.DataOffset(), stblAtom.DataOffset()+int64(stblAtom.DataSize()), "co64")
		if err != nil {
			return nil, err
		}
	}

	stcoOffset := stcoAtom.DataOffset() + 4 // Skip version + flags
	chunkCount, err := binary.Read[uint32](sr, stcoOffset, "chunk count")
	if err != nil {
		return nil, err
	}
	stcoOffset += 4

	// For simplicity, assume 1 sample per chunk (common for text tracks)
	chapters := make([]audiometa.Chapter, 0, sampleCount)

	for i := uint32(0); i < chunkCount && i < sampleCount; i++ {
		var chunkOffset uint64
		if stcoAtom.Type == "co64" {
			chunkOffset, _ = binary.Read[uint64](sr, stcoOffset, "chunk offset")
			stcoOffset += 8
		} else {
			offset32, _ := binary.Read[uint32](sr, stcoOffset, "chunk offset")
			chunkOffset = uint64(offset32)
			stcoOffset += 4
		}

		// Read text sample
		sampleSize := sampleSizes[i]
		if sampleSize > 0 && sampleSize < 10000 { // Sanity check
			// Text samples have a 2-byte length prefix
			textBuf := make([]byte, sampleSize)
			if err := sr.ReadAt(textBuf, int64(chunkOffset), "chapter text"); err == nil {
				// Skip first 2 bytes (length prefix) and read UTF-8/UTF-16 text
				var title string
				if sampleSize >= 2 {
					textLen := int(textBuf[0])<<8 | int(textBuf[1])
					if textLen > 0 && textLen <= len(textBuf)-2 {
						title = string(textBuf[2 : 2+textLen])
					}
				}

				chapter := audiometa.Chapter{
					Index:     int(i + 1),
					Title:     title,
					StartTime: chapterTimes[i],
				}
				chapters = append(chapters, chapter)
			}
		}
	}

	// Calculate end times
	for i := 0; i < len(chapters); i++ {
		if i < len(chapters)-1 {
			chapters[i].EndTime = chapters[i+1].StartTime
		} else {
			chapters[i].EndTime = fileDuration
		}
	}

	return chapters, nil
}
