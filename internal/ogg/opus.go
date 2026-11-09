package ogg

import (
	"context"
	"encoding/binary"
	"fmt"

	"github.com/simonhull/audiometa/internal/types"
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
	outputGain := int16(binary.LittleEndian.Uint16(data[16:18])) //nolint:gosec // G115: output gain is a signed Q7.8 field (RFC 7845); reinterpreting the bits is the decoding
	mappingFamily := data[18]

	// Populate file.Audio
	file.Audio.Codec = "Opus"
	file.Audio.Container = containerOgg
	file.Audio.SampleRate = 48000 // Opus always outputs at 48kHz
	file.Audio.Channels = int(channels)
	file.Audio.Lossless = false
	file.Audio.VBR = true // Opus is VBR

	// inputSampleRate and outputGain are informational (normal encoder metadata,
	// not defects) - do not emit warnings for them; warnings are reserved for
	// parse degradation and would trip WithStrictParsing.
	_ = inputSampleRate
	_ = outputGain

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
func parseOpusTags(ctx context.Context, data []byte, file *types.File) error {
	if len(data) < 12 {
		return fmt.Errorf("OpusTags packet too short: %d bytes (need at least 12)", len(data))
	}

	// Verify "OpusTags" magic marker
	if string(data[0:8]) != "OpusTags" {
		return fmt.Errorf("invalid OpusTags magic: %q (expected \"OpusTags\")", string(data[0:8]))
	}

	return applyCommentHeader(ctx, data[8:], file)
}
