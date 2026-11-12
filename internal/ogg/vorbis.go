package ogg

import (
	"context"
	"encoding/binary"
	"fmt"

	"github.com/simonhull/audiometa/internal/types"
)

// parseVorbisIdentification parses the Vorbis identification header (packet type 0x01).
//
// The identification header contains audio properties:
//   - Sample rate
//   - Number of channels
//   - Bitrate (nominal, maximum, minimum)
//
// Returns an error if the header is invalid or too short.
func parseVorbisIdentification(data []byte, file *types.File) error {
	if len(data) < 30 {
		return fmt.Errorf("identification header too short: %d bytes", len(data))
	}

	// Verify packet type (0x01 = identification)
	if data[0] != 0x01 {
		return fmt.Errorf("not an identification header (type 0x%02x)", data[0])
	}

	// Verify "vorbis" magic marker
	if string(data[1:7]) != vorbisMagic {
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
	// bitrateMaximum is at data[16:20] but is optional and often 0, so we skip it
	// The nominal bitrate is signed (Vorbis I §4.2.2): a value <= 0 means unset.
	bitrateNominal := int32(binary.LittleEndian.Uint32(data[20:24])) //nolint:gosec // G115: the Vorbis nominal bitrate is a signed 32-bit field
	// bitrateMinimum is at data[24:28] but is optional and often 0, so we skip it

	// Populate file.Audio
	file.Audio.Codec = "Vorbis"
	file.Audio.Container = containerOgg
	file.Audio.SampleRate = int(sampleRate)
	file.Audio.Channels = int(channels)
	if bitrateNominal > 0 {
		file.Audio.Bitrate = int(bitrateNominal)
	}
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
func parseVorbisComment(ctx context.Context, data []byte, file *types.File) error {
	if len(data) < 8 {
		return fmt.Errorf("comment header too short: %d bytes", len(data))
	}

	// Verify packet type (0x03 = comment)
	if data[0] != 0x03 {
		return fmt.Errorf("not a comment header (type 0x%02x)", data[0])
	}

	// Verify "vorbis" magic marker
	if string(data[1:7]) != vorbisMagic {
		return fmt.Errorf("invalid vorbis magic: %q", string(data[1:7]))
	}

	return applyCommentHeader(ctx, data[7:], file)
}
