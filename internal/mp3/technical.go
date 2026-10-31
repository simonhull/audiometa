package mp3

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	binutil "github.com/simonhull/audiometa/internal/binary"
	"github.com/simonhull/audiometa/internal/types"
)

// MPEG audio version bits, (header >> 19) & 3. MPEG-2.5 (0) and reserved (1)
// are rejected by validFrameHeader; supporting 2.5 would need 11025/12000/8000 Hz
// sample rates and 576 samples per frame.
const (
	versionMPEG2 = 2
	versionMPEG1 = 3
)

// Layer III bitrate tables in kbps, indexed by the header's bitrate index.
var (
	bitrateTableV1 = []int{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 0}
	bitrateTableV2 = []int{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160, 0}
)

// Sample rate tables in Hz, indexed by the header's sample rate index.
var (
	sampleRateTableV1 = []int{44100, 48000, 32000, 0}
	sampleRateTableV2 = []int{22050, 24000, 16000, 0}
)

// mpegVersion extracts the version bits: 3 = MPEG-1, 2 = MPEG-2.
func mpegVersion(header uint32) uint32 { return (header >> 19) & 0x3 }

// samplesPerFrame returns the Layer III samples per frame for the header's version.
func samplesPerFrame(header uint32) int {
	if mpegVersion(header) == versionMPEG1 {
		return 1152
	}
	return 576
}

// maxFrameSearch bounds how far past the ID3 tag the first frame is searched
// for. Real frame data begins within a few bytes of the tag; the margin covers
// padding and slightly wrong tag sizes without letting a crafted file force a
// scan of the whole input.
const maxFrameSearch = 1 << 16 // 64 KiB

var errNoMP3Frame = errors.New("no valid MP3 frame found")

// parseTechnicalInfo extracts bitrate, sample rate, codec, and duration from MP3 frames.
//
// It reads one bounded window starting at the end of the ID3 tag and scans it
// in memory for the first valid frame header.
func parseTechnicalInfo(ctx context.Context, sr *binutil.SafeReader, tagSize int64, fileSize int64, file *types.File) error {
	start := max(tagSize, 0)
	if start >= fileSize {
		return errNoMP3Frame
	}

	window := make([]byte, min(fileSize-start, maxFrameSearch))
	if err := sr.ReadAt(window, start, "MP3 frame search window"); err != nil {
		return fmt.Errorf("read frame search window: %w", err)
	}

	// Search for MP3 frame sync (11 bits set)
	for i := 0; i+4 <= len(window); i++ {
		if err := binutil.Canceled(ctx, i); err != nil {
			return err
		}

		header := binary.BigEndian.Uint32(window[i : i+4])
		if !validFrameHeader(header) {
			continue
		}

		bitrate, sampleRate, channels := parseMP3FrameHeader(header)
		if bitrate <= 0 || sampleRate <= 0 {
			continue
		}

		frameOffset := start + int64(i)
		file.Audio.Bitrate = bitrate
		file.Audio.SampleRate = sampleRate
		file.Audio.Channels = channels
		file.Audio.Codec = "MP3"

		// Check for VBR header
		duration, vbr := parseVBRHeader(sr, frameOffset, header, sampleRate)
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

	return errNoMP3Frame
}

// validFrameHeader reports whether header has the 11-bit frame sync and
// names an MPEG-1/2 Layer III frame.
func validFrameHeader(header uint32) bool {
	// Check frame sync (11 bits set: 0xFFE00000)
	if header&0xFFE00000 != 0xFFE00000 {
		return false
	}

	// MPEG1 (version 11) or MPEG2 (version 10)
	version := mpegVersion(header)
	if version != versionMPEG1 && version != versionMPEG2 {
		return false
	}

	// Layer III (01)
	return (header>>17)&0x3 == 1
}

// parseMP3FrameHeader extracts bitrate, sample rate, and channels from frame header.
func parseMP3FrameHeader(header uint32) (bitrate, sampleRate, channels int) {
	bitrates, sampleRates := bitrateTableV1, sampleRateTableV1
	if mpegVersion(header) != versionMPEG1 {
		bitrates, sampleRates = bitrateTableV2, sampleRateTableV2
	}

	// Bitrate index (4 bits)
	bitrateIdx := (header >> 12) & 0xF
	if bitrateIdx < uint32(len(bitrates)) { //nolint:gosec // G115: len of a small constant table
		bitrate = bitrates[bitrateIdx] * 1000 // Convert to bps
	}

	// Sample rate index (2 bits)
	sampleRateIdx := (header >> 10) & 0x3
	if sampleRateIdx < uint32(len(sampleRates)) { //nolint:gosec // G115: len of a small constant table
		sampleRate = sampleRates[sampleRateIdx]
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

// sideInfoSize returns the Layer III side-information size in bytes for the
// given frame header: MPEG-1: 32 (stereo modes) / 17 (mono); MPEG-2: 17 / 9.
func sideInfoSize(header uint32) int64 {
	mono := (header>>6)&0x3 == 3
	if mpegVersion(header) == versionMPEG1 {
		if mono {
			return 17
		}
		return 32
	}
	if mono {
		return 9
	}
	return 17
}

// parseVBRHeader checks for Xing/VBRI VBR headers and calculates accurate duration.
func parseVBRHeader(sr *binutil.SafeReader, frameOffset int64, header uint32, sampleRate int) (time.Duration, bool) {
	// Xing/Info follows the frame header and the side information, whose size
	// depends on MPEG version and channel mode.
	xingOffset := frameOffset + 4 + sideInfoSize(header)
	buf := make([]byte, 120) // Read enough for Xing header
	if err := sr.ReadAt(buf, xingOffset, "VBR header"); err != nil {
		return 0, false
	}

	// Check for Xing or Info marker
	isXing := string(buf[0:4]) == "Xing" || string(buf[0:4]) == "Info"
	if !isXing {
		// VBRI is always 32 bytes after the 4-byte header, regardless of channel mode.
		vbriOffset := frameOffset + 36
		vbriBuf := make([]byte, 32)
		if err := sr.ReadAt(vbriBuf, vbriOffset, "VBRI header"); err == nil {
			if string(vbriBuf[0:4]) == "VBRI" {
				// VBRI header found
				// Frames are at offset 14 (4 bytes)
				if len(vbriBuf) >= 18 {
					numFrames := binary.BigEndian.Uint32(vbriBuf[14:18])
					return calculateDurationFromFrames(numFrames, sampleRate, samplesPerFrame(header)), true
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
		return calculateDurationFromFrames(numFrames, sampleRate, samplesPerFrame(header)), true
	}

	return 0, false
}

// calculateDurationFromFrames calculates duration from number of frames.
func calculateDurationFromFrames(numFrames uint32, sampleRate, samplesPerFrame int) time.Duration {
	totalSamples := float64(numFrames) * float64(samplesPerFrame)
	durationSeconds := totalSamples / float64(sampleRate)
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
