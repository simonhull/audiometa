// Package flac provides FLAC audio file parsing and metadata extraction.
package flac

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/simonhull/audiometa/internal/binary"
	"github.com/simonhull/audiometa/internal/types"
)

// CueSheet represents a FLAC CUESHEET metadata block.
type CueSheet struct {
	MediaCatalogNumber string
	LeadIn             uint64
	IsCD               bool
	Tracks             []CueTrack
}

// CueTrack represents a track in a cue sheet.
type CueTrack struct {
	Offset      uint64 // samples from start of audio
	Number      byte   // track number (1-99, 170=lead-out)
	ISRC        string
	IsAudio     bool
	PreEmphasis bool
	Indices     []CueIndex
}

// CueIndex represents an index point within a track.
type CueIndex struct {
	Offset uint64 // samples from start of track
	Number byte   // index number
}

// parseCueSheet parses a FLAC CUESHEET metadata block.
func parseCueSheet(sr *binary.SafeReader, offset int64, length uint32, file *types.File) error {
	if length < 396 { // Minimum size: 128+8+1+258+1 = 396 bytes
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
	offset++

	// Skip reserved (7 bits after the is-CD flag were in the flags byte; 258 more bytes)
	offset += 258

	// Read track count (1 byte)
	trackCount, err := binary.Read[uint8](sr, offset, "track count")
	if err != nil {
		return fmt.Errorf("read track count: %w", err)
	}
	offset++

	// Verify we have enough data for tracks
	bytesRead := offset - startOffset
	if int64(length) < bytesRead {
		return errors.New("CUESHEET block truncated")
	}

	// Parse tracks
	tracks := make([]CueTrack, 0, trackCount)
	for i := range trackCount {
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
	file.Chapters = cuesheetToChapters(cuesheet, file.Audio.SampleRate, file.Audio.Duration)

	return nil
}

// parseCueTrack parses a single track from CUESHEET.
func parseCueTrack(sr *binary.SafeReader, offset, maxOffset int64) (*CueTrack, int64, error) {
	if offset+36 > maxOffset {
		return nil, 0, errors.New("track data exceeds block bounds")
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
	offset++

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
	isAudio := (flags & 0x80) == 0     // Audio if bit 7 is NOT set
	preEmphasis := (flags & 0x40) != 0 // Pre-emphasis if bit 6 is set
	offset++

	// Skip reserved (13 bytes)
	offset += 13

	// Read index count (1 byte)
	indexCount, err := binary.Read[uint8](sr, offset, "index count")
	if err != nil {
		return nil, 0, fmt.Errorf("read index count: %w", err)
	}
	offset++

	// Parse indices
	indices := make([]CueIndex, 0, indexCount)
	for j := range indexCount {
		if offset+12 > maxOffset {
			return nil, 0, errors.New("index data exceeds block bounds")
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

// parseCueIndex parses a single index point from CUESHEET.
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
	offset++

	// Skip reserved (3 bytes)
	offset += 3

	return &CueIndex{
		Offset: indexOffset,
		Number: indexNumber,
	}, offset, nil
}

// Lead-out track numbers (RFC 9639 section 8.7): 170 for CD-DA cuesheets, 255 otherwise.
const (
	leadOutTrackCD    = 170
	leadOutTrackNonCD = 255
)

func isLeadOut(number byte) bool { return number == leadOutTrackCD || number == leadOutTrackNonCD }

// trackStartSample returns the sample where the track's audible content starts: the track
// offset plus the INDEX 01 offset (INDEX 00, if present, is the pregap). Falls back to the
// track offset when there is no INDEX 01.
func trackStartSample(t CueTrack) uint64 {
	for _, idx := range t.Indices {
		if idx.Number == 1 {
			return t.Offset + idx.Offset
		}
	}
	return t.Offset
}

// samplesToDuration converts a sample count to a Duration without float rounding.
// sampleRate is at most 2^20-1 (20-bit STREAMINFO field), so rem*1e9 cannot overflow uint64.
func samplesToDuration(samples uint64, sampleRate int) time.Duration {
	rate := uint64(sampleRate)
	return time.Duration(samples/rate)*time.Second + time.Duration(samples%rate*uint64(time.Second)/rate)
}

// cuesheetToChapters converts a CUESHEET to types.Chapter slice.
func cuesheetToChapters(cuesheet *CueSheet, sampleRate int, fileDuration time.Duration) []types.Chapter {
	if len(cuesheet.Tracks) == 0 || sampleRate <= 0 {
		return nil
	}

	// The lead-out is always the last track.
	var leadOut *CueTrack
	if last := cuesheet.Tracks[len(cuesheet.Tracks)-1]; isLeadOut(last.Number) {
		leadOut = &last
	}

	chapters := make([]types.Chapter, 0, len(cuesheet.Tracks))
	for _, track := range cuesheet.Tracks {
		if !track.IsAudio || isLeadOut(track.Number) {
			continue
		}

		// Title is the track number, or the ISRC if present.
		title := fmt.Sprintf("Track %02d", track.Number)
		if track.ISRC != "" {
			title = fmt.Sprintf("Track %02d (%s)", track.Number, track.ISRC)
		}

		chapters = append(chapters, types.Chapter{
			Index:     len(chapters) + 1,
			Title:     title,
			StartTime: samplesToDuration(trackStartSample(track), sampleRate),
			Source:    types.ChapterSourceCueSheet,
		})
	}

	if len(chapters) == 0 {
		return nil
	}

	end := fileDuration
	if leadOut != nil {
		end = samplesToDuration(leadOut.Offset, sampleRate)
	}
	types.FillChapterEndTimes(chapters, end)

	return chapters
}
