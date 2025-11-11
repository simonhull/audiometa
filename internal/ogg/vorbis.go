package ogg

import (
	"encoding/binary"
	"fmt"

	"github.com/simonhull/audiometa"
	"github.com/simonhull/audiometa/internal/vorbis"
)

// parseVorbisIdentification parses the Vorbis identification header (packet type 0x01).
//
// The identification header contains audio properties:
//   - Sample rate
//   - Number of channels
//   - Bitrate (nominal, maximum, minimum)
//
// Returns an error if the header is invalid or too short.
func parseVorbisIdentification(data []byte, file *audiometa.File) error {
	if len(data) < 30 {
		return fmt.Errorf("identification header too short: %d bytes", len(data))
	}

	// Verify packet type (0x01 = identification)
	if data[0] != 0x01 {
		return fmt.Errorf("not an identification header (type 0x%02x)", data[0])
	}

	// Verify "vorbis" magic marker
	if string(data[1:7]) != "vorbis" {
		return fmt.Errorf("invalid vorbis magic: %q", string(data[1:7]))
	}

	// Parse Vorbis version (should be 0)
	vorbisVersion := binary.LittleEndian.Uint32(data[7:11])
	if vorbisVersion != 0 {
		return fmt.Errorf("unsupported Vorbis version: %d", vorbisVersion)
	}

	// Parse audio properties (all little-endian)
	channels := data[11]
	sampleRate := binary.LittleEndian.Uint32(data[12:16])
	// bitrateMaximum := binary.LittleEndian.Uint32(data[16:20]) // Optional, can be 0
	bitrateNominal := binary.LittleEndian.Uint32(data[20:24])
	// bitrateMinimum := binary.LittleEndian.Uint32(data[24:28]) // Optional, can be 0

	// Populate file.Audio
	file.Audio.Codec = "Vorbis"
	file.Audio.Container = "Ogg"
	file.Audio.SampleRate = int(sampleRate)
	file.Audio.Channels = int(channels)
	file.Audio.Bitrate = int(bitrateNominal)
	file.Audio.Lossless = false
	file.Audio.VBR = true // Vorbis is typically VBR

	return nil
}

// parseVorbisComment parses the Vorbis comment header (packet type 0x03).
//
// The comment header contains Vorbis comments (tags) in the same format
// as FLAC Vorbis comments: UTF-8 strings in "KEY=VALUE" format.
//
// Structure:
//   - Vendor string (length + UTF-8 string)
//   - User comment list (count + comments)
//   - Each comment: length + UTF-8 string
//
// Returns an error if the header is invalid or truncated.
func parseVorbisComment(data []byte, file *audiometa.File) error {
	if len(data) < 8 {
		return fmt.Errorf("comment header too short: %d bytes", len(data))
	}

	// Verify packet type (0x03 = comment)
	if data[0] != 0x03 {
		return fmt.Errorf("not a comment header (type 0x%02x)", data[0])
	}

	// Verify "vorbis" magic marker
	if string(data[1:7]) != "vorbis" {
		return fmt.Errorf("invalid vorbis magic: %q", string(data[1:7]))
	}

	offset := 7

	// Read vendor string length (32-bit little-endian)
	if offset+4 > len(data) {
		return fmt.Errorf("truncated vendor length")
	}
	vendorLen := binary.LittleEndian.Uint32(data[offset : offset+4])
	offset += 4

	// Skip vendor string (we don't use it)
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
			file.Warnings = append(file.Warnings, audiometa.Warning{
				Stage:   "metadata",
				Message: fmt.Sprintf("truncated comment %d", i),
			})
			break
		}

		commentLen := binary.LittleEndian.Uint32(data[offset : offset+4])
		offset += 4

		if offset+int(commentLen) > len(data) {
			// Truncated comment
			file.Warnings = append(file.Warnings, audiometa.Warning{
				Stage:   "metadata",
				Message: fmt.Sprintf("truncated comment %d data", i),
			})
			break
		}

		comment := string(data[offset : offset+int(commentLen)])
		offset += int(commentLen)

		// Collect for chapter parsing
		allComments = append(allComments, comment)

		// Parse comment using shared Vorbis comment parser
		if err := vorbis.ParseComment(comment, &file.Tags); err != nil {
			// Non-fatal - add warning and continue
			file.Warnings = append(file.Warnings, audiometa.Warning{
				Stage:   "metadata",
				Message: fmt.Sprintf("invalid Vorbis comment: %s", err),
			})
		}
	}

	// Parse chapters from CHAPTER comments
	if len(allComments) > 0 {
		file.Chapters = vorbis.ParseChapters(allComments, file.Audio.Duration)
	}

	return nil
}
