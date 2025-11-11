package flac

import (
	"fmt"
	"strings"
	"time"

	"github.com/simonhull/audiometa"
	"github.com/simonhull/audiometa/internal/binary"
)

// CueSheet represents a FLAC CUESHEET metadata block
type CueSheet struct {
	MediaCatalogNumber string
	LeadIn             uint64
	IsCD               bool
	Tracks             []CueTrack
}

// CueTrack represents a track in a cue sheet
type CueTrack struct {
	Offset      uint64 // samples from start of audio
	Number      byte   // track number (1-99, 170=lead-out)
	ISRC        string
	IsAudio     bool
	PreEmphasis bool
	Indices     []CueIndex
}

// CueIndex represents an index point within a track
type CueIndex struct {
	Offset uint64 // samples from start of track
	Number byte   // index number
}

// parseCueSheet parses a FLAC CUESHEET metadata block
func parseCueSheet(sr *binary.SafeReader, offset int64, length uint32, file *audiometa.File) error {
	if length < 396 { // Minimum size: 128+8+1+259+1 = 397 bytes (but some fields can be smaller)
		return fmt.Errorf("CUESHEET block too short: %d bytes (need at least 396)", length)
	}

	startOffset := offset

	// Read media catalog number (128 bytes, ASCII, null-padded)
	mcnBytes := make([]byte, 128)
	if err := sr.ReadAt(mcnBytes, offset, "media catalog number"); err != nil {
		return fmt.Errorf("read MCN: %w", err)
	}
	mcn := strings.TrimRight(string(mcnBytes), "\x00")
	offset += 128

	// Read lead-in samples (8 bytes, 64-bit big-endian)
	leadIn, err := binary.Read[uint64](sr, offset, "lead-in samples")
	if err != nil {
		return fmt.Errorf("read lead-in: %w", err)
	}
	offset += 8

	// Read flags (1 byte)
	flags, err := binary.Read[uint8](sr, offset, "cuesheet flags")
	if err != nil {
		return fmt.Errorf("read flags: %w", err)
	}
	isCD := (flags & 0x80) != 0
	offset += 1

	// Skip reserved (259 bytes)
	offset += 259

	// Read track count (1 byte)
	trackCount, err := binary.Read[uint8](sr, offset, "track count")
	if err != nil {
		return fmt.Errorf("read track count: %w", err)
	}
	offset += 1

	// Verify we have enough data for tracks
	bytesRead := offset - startOffset
	if int64(length) < bytesRead {
		return fmt.Errorf("CUESHEET block truncated")
	}

	// Parse tracks
	tracks := make([]CueTrack, 0, trackCount)
	for i := byte(0); i < trackCount; i++ {
		track, nextOffset, err := parseCueTrack(sr, offset, startOffset+int64(length))
		if err != nil {
			return fmt.Errorf("parse track %d: %w", i, err)
		}
		tracks = append(tracks, *track)
		offset = nextOffset
	}

	cuesheet := &CueSheet{
		MediaCatalogNumber: mcn,
		LeadIn:             leadIn,
		IsCD:               isCD,
		Tracks:             tracks,
	}

	// Convert cuesheet to chapters
	file.Chapters = cuesheetToChapters(cuesheet, file.Audio.SampleRate)

	return nil
}

// parseCueTrack parses a single track from CUESHEET
func parseCueTrack(sr *binary.SafeReader, offset, maxOffset int64) (*CueTrack, int64, error) {
	if offset+36 > maxOffset {
		return nil, 0, fmt.Errorf("track data exceeds block bounds")
	}

	// Read track offset (8 bytes, 64-bit big-endian, samples from start of audio)
	trackOffset, err := binary.Read[uint64](sr, offset, "track offset")
	if err != nil {
		return nil, 0, fmt.Errorf("read track offset: %w", err)
	}
	offset += 8

	// Read track number (1 byte, 1-99 for normal tracks, 170 for lead-out)
	trackNumber, err := binary.Read[uint8](sr, offset, "track number")
	if err != nil {
		return nil, 0, fmt.Errorf("read track number: %w", err)
	}
	offset += 1

	// Read ISRC (12 bytes, ASCII)
	isrcBytes := make([]byte, 12)
	if err := sr.ReadAt(isrcBytes, offset, "ISRC"); err != nil {
		return nil, 0, fmt.Errorf("read ISRC: %w", err)
	}
	isrc := strings.TrimRight(string(isrcBytes), "\x00")
	offset += 12

	// Read flags (1 byte)
	flags, err := binary.Read[uint8](sr, offset, "track flags")
	if err != nil {
		return nil, 0, fmt.Errorf("read track flags: %w", err)
	}
	isAudio := (flags & 0x80) == 0       // Audio if bit 7 is NOT set
	preEmphasis := (flags & 0x40) != 0   // Pre-emphasis if bit 6 is set
	offset += 1

	// Skip reserved (13 bytes)
	offset += 13

	// Read index count (1 byte)
	indexCount, err := binary.Read[uint8](sr, offset, "index count")
	if err != nil {
		return nil, 0, fmt.Errorf("read index count: %w", err)
	}
	offset += 1

	// Parse indices
	indices := make([]CueIndex, 0, indexCount)
	for j := byte(0); j < indexCount; j++ {
		if offset+12 > maxOffset {
			return nil, 0, fmt.Errorf("index data exceeds block bounds")
		}

		index, nextOffset, err := parseCueIndex(sr, offset)
		if err != nil {
			return nil, 0, fmt.Errorf("parse index %d: %w", j, err)
		}
		indices = append(indices, *index)
		offset = nextOffset
	}

	track := &CueTrack{
		Offset:      trackOffset,
		Number:      trackNumber,
		ISRC:        isrc,
		IsAudio:     isAudio,
		PreEmphasis: preEmphasis,
		Indices:     indices,
	}

	return track, offset, nil
}

// parseCueIndex parses a single index point from CUESHEET
func parseCueIndex(sr *binary.SafeReader, offset int64) (*CueIndex, int64, error) {
	// Read index offset (8 bytes, 64-bit big-endian, samples relative to track start)
	indexOffset, err := binary.Read[uint64](sr, offset, "index offset")
	if err != nil {
		return nil, 0, fmt.Errorf("read index offset: %w", err)
	}
	offset += 8

	// Read index number (1 byte)
	indexNumber, err := binary.Read[uint8](sr, offset, "index number")
	if err != nil {
		return nil, 0, fmt.Errorf("read index number: %w", err)
	}
	offset += 1

	// Skip reserved (3 bytes)
	offset += 3

	return &CueIndex{
		Offset: indexOffset,
		Number: indexNumber,
	}, offset, nil
}

// cuesheetToChapters converts a CUESHEET to audiometa.Chapter slice
func cuesheetToChapters(cuesheet *CueSheet, sampleRate int) []audiometa.Chapter {
	if len(cuesheet.Tracks) == 0 {
		return nil
	}

	if sampleRate <= 0 {
		return nil
	}

	// Filter out non-audio tracks and lead-out (track 170)
	var audioTracks []CueTrack
	for _, track := range cuesheet.Tracks {
		if track.IsAudio && track.Number != 170 {
			audioTracks = append(audioTracks, track)
		}
	}

	if len(audioTracks) == 0 {
		return nil
	}

	// Find lead-out track for calculating last chapter end time
	var leadOutOffset uint64
	for _, track := range cuesheet.Tracks {
		if track.Number == 170 {
			leadOutOffset = track.Offset
			break
		}
	}

	// Convert tracks to chapters
	chapters := make([]audiometa.Chapter, len(audioTracks))

	for i, track := range audioTracks {
		// Calculate start time (samples / sample_rate = seconds)
		seconds := float64(track.Offset) / float64(sampleRate)
		startTime := time.Duration(seconds * float64(time.Second))

		// Calculate end time
		var endTime time.Duration
		if i < len(audioTracks)-1 {
			// End at start of next track
			nextOffset := audioTracks[i+1].Offset
			seconds := float64(nextOffset) / float64(sampleRate)
			endTime = time.Duration(seconds * float64(time.Second))
		} else if leadOutOffset > 0 {
			// Last track: use lead-out if available
			seconds := float64(leadOutOffset) / float64(sampleRate)
			endTime = time.Duration(seconds * float64(time.Second))
		}
		// If no lead-out and last track, endTime stays 0 (will be set by duration)

		// Generate title (Track number, or use ISRC if present)
		title := fmt.Sprintf("Track %02d", track.Number)
		if track.ISRC != "" {
			title = fmt.Sprintf("Track %02d (%s)", track.Number, track.ISRC)
		}

		chapters[i] = audiometa.Chapter{
			Index:     i + 1,
			Title:     title,
			StartTime: startTime,
			EndTime:   endTime,
		}
	}

	return chapters
}
