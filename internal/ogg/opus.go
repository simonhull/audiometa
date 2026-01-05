package ogg

import (
	"encoding/binary"
	"fmt"

	"github.com/simonhull/audiometa/internal/types"
	"github.com/simonhull/audiometa/internal/vorbis"
)

// parseOpusHead parses the OpusHead identification header.
//
// The OpusHead header contains audio properties:
//   - Version (must be 1)
//   - Number of channels
//   - Pre-skip (samples to skip at start)
//   - Input sample rate (original recording rate, informational)
//   - Output gain (playback volume adjustment)
//   - Channel mapping family
//
// Note: Opus always outputs at 48kHz regardless of input sample rate.
//
// Returns an error if the header is invalid or unsupported.
func parseOpusHead(data []byte, file *types.File) error {
	if len(data) < 19 {
		return fmt.Errorf("OpusHead packet too short: %d bytes (need at least 19)", len(data))
	}

	// Verify "OpusHead" magic marker
	if string(data[0:8]) != "OpusHead" {
		return fmt.Errorf("invalid OpusHead magic: %q (expected \"OpusHead\")", string(data[0:8]))
	}

	// Verify version (must be 1)
	version := data[8]
	if version != 1 {
		return fmt.Errorf("unsupported Opus version: %d (only version 1 is supported)", version)
	}

	// Parse audio properties (all little-endian)
	channels := data[9]
	preSkip := binary.LittleEndian.Uint16(data[10:12])
	inputSampleRate := binary.LittleEndian.Uint32(data[12:16])
	outputGain := int16(binary.LittleEndian.Uint16(data[16:18]))
	mappingFamily := data[18]

	// Populate file.Audio
	file.Audio.Codec = "Opus"
	file.Audio.Container = containerOgg
	file.Audio.SampleRate = 48000 // Opus always outputs at 48kHz
	file.Audio.Channels = int(channels)
	file.Audio.Lossless = false
	file.Audio.VBR = true // Opus is VBR

	// Add informational warnings for non-default values
	if inputSampleRate != 48000 && inputSampleRate > 0 {
		file.Warnings = append(file.Warnings, types.Warning{
			Stage:   "technical",
			Message: fmt.Sprintf("original sample rate was %d Hz (Opus outputs at 48 kHz)", inputSampleRate),
		})
	}

	if outputGain != 0 {
		gainDB := float64(outputGain) / 256.0
		file.Warnings = append(file.Warnings, types.Warning{
			Stage:   "technical",
			Message: fmt.Sprintf("output gain: %.2f dB", gainDB),
		})
	}

	// Pre-skip and mapping family are informational, not needed for metadata
	_ = preSkip
	_ = mappingFamily

	return nil
}

// parseOpusTags parses the OpusTags comment header.
//
// The OpusTags header uses the same format as Vorbis comments:
//   - Vendor string (length + UTF-8 string)
//   - User comment list (count + comments)
//   - Each comment: length + UTF-8 string in "KEY=VALUE" format
//
// The only difference from Vorbis comments is the "OpusTags" magic marker.
//
// Returns an error if the header is invalid or truncated.
func parseOpusTags(data []byte, file *types.File) error {
	if len(data) < 12 {
		return fmt.Errorf("OpusTags packet too short: %d bytes (need at least 12)", len(data))
	}

	// Verify "OpusTags" magic marker
	if string(data[0:8]) != "OpusTags" {
		return fmt.Errorf("invalid OpusTags magic: %q (expected \"OpusTags\")", string(data[0:8]))
	}

	offset := 8

	// Read vendor string length (32-bit little-endian)
	if offset+4 > len(data) {
		return fmt.Errorf("truncated vendor length")
	}
	vendorLen := binary.LittleEndian.Uint32(data[offset : offset+4])
	offset += 4

	// Skip vendor string (we don't use it for metadata)
	if offset+int(vendorLen) > len(data) {
		return fmt.Errorf("truncated vendor string")
	}
	offset += int(vendorLen)

	// Read comment count (32-bit little-endian)
	if offset+4 > len(data) {
		return fmt.Errorf("truncated comment count")
	}
	commentCount := binary.LittleEndian.Uint32(data[offset : offset+4])
	offset += 4

	// Collect all comments for later chapter parsing
	var allComments []string

	// Read each comment
	for i := uint32(0); i < commentCount; i++ {
		if offset+4 > len(data) {
			// Truncated, but don't fail - just stop reading
			file.Warnings = append(file.Warnings, types.Warning{
				Stage:   "metadata",
				Message: fmt.Sprintf("truncated comment %d (missing length field)", i),
			})
			break
		}

		commentLen := binary.LittleEndian.Uint32(data[offset : offset+4])
		offset += 4

		if offset+int(commentLen) > len(data) {
			// Truncated comment data
			file.Warnings = append(file.Warnings, types.Warning{
				Stage:   "metadata",
				Message: fmt.Sprintf("truncated comment %d data (expected %d bytes)", i, commentLen),
			})
			break
		}

		comment := string(data[offset : offset+int(commentLen)])
		offset += int(commentLen)

		// Collect for chapter parsing
		allComments = append(allComments, comment)

		// Parse comment using shared Vorbis comment parser
		// (OpusTags uses identical format to Vorbis comments)
		if err := vorbis.ParseComment(comment, file); err != nil {
			// Non-fatal - add warning and continue
			file.Warnings = append(file.Warnings, types.Warning{
				Stage:   "metadata",
				Message: fmt.Sprintf("invalid Opus tag: %s", err),
			})
		}
	}

	// Parse chapters from CHAPTER comments
	if len(allComments) > 0 {
		file.Chapters = vorbis.ParseChapters(allComments, file.Audio.Duration)
	}

	return nil
}
