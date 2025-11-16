package mp3

import (
	"encoding/binary"
	"fmt"
	"time"

	binutil "github.com/simonhull/audiometa/internal/binary"
	"github.com/simonhull/audiometa/internal/types"
)

// MP3 bitrate table (MPEG1 Layer III) in kbps.
var bitrateTable = []int{
	0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 0,
}

// MP3 sample rate table (MPEG1) in Hz.
var sampleRateTable = []int{
	44100, 48000, 32000, 0,
}

// parseTechnicalInfo extracts bitrate, sample rate, codec, and duration from MP3 frames.
func parseTechnicalInfo(sr *binutil.SafeReader, tagSize int64, fileSize int64, file *types.File) error {
	// Find first MP3 frame (after ID3 tag)
	frameOffset := tagSize

	// Search for MP3 frame sync (11 bits set)
	for frameOffset < fileSize-4 {
		header, err := findMP3FrameAt(sr, frameOffset)
		if err == nil {
			// Found valid frame
			bitrate, sampleRate, channels := parseMP3FrameHeader(header)
			if bitrate > 0 && sampleRate > 0 {
				file.Audio.Bitrate = bitrate
				file.Audio.SampleRate = sampleRate
				file.Audio.Channels = channels
				file.Audio.Codec = "MP3"

				// Check for VBR header
				duration, vbr := parseVBRHeader(sr, frameOffset, sampleRate, fileSize, tagSize)
				if vbr {
					file.Audio.Duration = duration
					file.Audio.VBR = true
				} else {
					// CBR - estimate from bitrate and file size
					file.Audio.Duration = estimateCBRDuration(bitrate, fileSize, tagSize)
					file.Audio.VBR = false
				}

				return nil
			}
		}

		frameOffset++
	}

	return fmt.Errorf("no valid MP3 frame found")
}

// findMP3FrameAt attempts to read an MP3 frame header at the given offset.
func findMP3FrameAt(sr *binutil.SafeReader, offset int64) (uint32, error) {
	buf := make([]byte, 4)
	if err := sr.ReadAt(buf, offset, "MP3 frame header"); err != nil {
		return 0, err
	}

	header := binary.BigEndian.Uint32(buf)

	// Check frame sync (11 bits set: 0xFFE00000)
	if header&0xFFE00000 != 0xFFE00000 {
		return 0, fmt.Errorf("invalid frame sync")
	}

	// Validate MPEG version and layer
	version := (header >> 19) & 0x3
	layer := (header >> 17) & 0x3

	// MPEG1 (version 11) or MPEG2 (version 10)
	if version != 3 && version != 2 {
		return 0, fmt.Errorf("unsupported MPEG version")
	}

	// Layer III (01)
	if layer != 1 {
		return 0, fmt.Errorf("unsupported layer")
	}

	return header, nil
}

// parseMP3FrameHeader extracts bitrate, sample rate, and channels from frame header.
func parseMP3FrameHeader(header uint32) (bitrate, sampleRate, channels int) {
	// Bitrate index (4 bits)
	bitrateIdx := (header >> 12) & 0xF
	if bitrateIdx < uint32(len(bitrateTable)) {
		bitrate = bitrateTable[bitrateIdx] * 1000 // Convert to bps
	}

	// Sample rate index (2 bits)
	sampleRateIdx := (header >> 10) & 0x3
	if sampleRateIdx < uint32(len(sampleRateTable)) {
		sampleRate = sampleRateTable[sampleRateIdx]
	}

	// Channel mode (2 bits)
	channelMode := (header >> 6) & 0x3
	if channelMode == 3 {
		channels = 1 // Mono
	} else {
		channels = 2 // Stereo, Joint Stereo, Dual Channel
	}

	return
}

// parseVBRHeader checks for Xing/VBRI VBR headers and calculates accurate duration.
func parseVBRHeader(sr *binutil.SafeReader, frameOffset int64, sampleRate int, fileSize int64, tagSize int64) (time.Duration, bool) {
	// Xing/Info header is 36 bytes after frame header for MPEG1
	// Check for "Xing" or "Info" marker
	xingOffset := frameOffset + 36
	buf := make([]byte, 120) // Read enough for Xing header
	if err := sr.ReadAt(buf, xingOffset, "VBR header"); err != nil {
		return 0, false
	}

	// Check for Xing or Info marker
	isXing := string(buf[0:4]) == "Xing" || string(buf[0:4]) == "Info"
	if !isXing {
		// Try VBRI header (different location)
		vbriOffset := frameOffset + 36
		vbriBuf := make([]byte, 32)
		if err := sr.ReadAt(vbriBuf, vbriOffset, "VBRI header"); err == nil {
			if string(vbriBuf[0:4]) == "VBRI" {
				// VBRI header found
				// Frames are at offset 14 (4 bytes)
				if len(vbriBuf) >= 18 {
					numFrames := binary.BigEndian.Uint32(vbriBuf[14:18])
					return calculateDurationFromFrames(numFrames, sampleRate), true
				}
			}
		}
		return 0, false
	}

	// Parse Xing header
	flags := binary.BigEndian.Uint32(buf[4:8])

	// Frames field is present if bit 0 is set
	if flags&0x0001 != 0 {
		numFrames := binary.BigEndian.Uint32(buf[8:12])
		return calculateDurationFromFrames(numFrames, sampleRate), true
	}

	return 0, false
}

// calculateDurationFromFrames calculates duration from number of frames.
func calculateDurationFromFrames(numFrames uint32, sampleRate int) time.Duration {
	// Each MPEG1 Layer III frame = 1152 samples
	samplesPerFrame := 1152
	totalSamples := uint64(numFrames) * uint64(samplesPerFrame)
	durationSeconds := float64(totalSamples) / float64(sampleRate)
	return time.Duration(durationSeconds * float64(time.Second))
}

// estimateCBRDuration estimates duration for constant bitrate files.
func estimateCBRDuration(bitrate int, fileSize int64, tagSize int64) time.Duration {
	if bitrate == 0 {
		return 0
	}

	// Audio data size (excluding ID3 tag)
	audioSize := fileSize - tagSize

	// Duration = (audio size in bytes * 8 bits/byte) / bitrate
	durationSeconds := float64(audioSize*8) / float64(bitrate)
	return time.Duration(durationSeconds * float64(time.Second))
}
