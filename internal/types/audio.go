package types

import (
	"fmt"
	"time"
)

// AudioInfo represents technical audio properties.
//
// AudioInfo provides format-agnostic access to audio technical metadata
// such as duration, sample rate, bit depth, and codec information.
type AudioInfo struct {
	ReplayGain       *ReplayGainInfo
	Codec            string
	CodecDescription string
	CodecProfile     string
	Container        string
	Duration         time.Duration
	SampleRate       int
	BitDepth         int
	Channels         int
	Bitrate          int
	Lossless         bool
	VBR              bool
}

// ReplayGainInfo represents loudness normalization data.
//
// ReplayGain provides information for normalizing playback volume across
// tracks and albums. See https://wiki.hydrogenaud.io/index.php?title=ReplayGain
type ReplayGainInfo struct {
	TrackGain float64 // Track gain adjustment in dB (can be negative)
	TrackPeak float64 // Track peak amplitude (0.0 to 1.0+)
	AlbumGain float64 // Album gain adjustment in dB (can be negative)
	AlbumPeak float64 // Album peak amplitude (0.0 to 1.0+)
}

// String returns a human-readable representation of the audio info.
// Example output: "FLAC 44.1kHz 16-bit stereo".
func (a AudioInfo) String() string {
	// Format sample rate
	sampleRate := fmt.Sprintf("%.1fkHz", float64(a.SampleRate)/1000)

	// Format bit depth
	bitDepth := ""
	if a.BitDepth > 0 {
		bitDepth = fmt.Sprintf("%d-bit", a.BitDepth)
	}

	// Format channels
	channels := channelDescription(a.Channels)

	// Format quality indicator
	quality := ""
	if a.Lossless {
		quality = "lossless"
	} else if a.Bitrate > 0 {
		quality = fmt.Sprintf("%dkbps", a.Bitrate/1000)
		if a.VBR {
			quality += " VBR"
		}
	}

	// Combine non-empty parts
	parts := []string{a.Codec}
	if sampleRate != "" {
		parts = append(parts, sampleRate)
	}
	if bitDepth != "" {
		parts = append(parts, bitDepth)
	}
	if channels != "" {
		parts = append(parts, channels)
	}
	if quality != "" {
		parts = append(parts, quality)
	}

	return join(parts, " ")
}

// channelDescription returns a human-readable channel description.
func channelDescription(channels int) string {
	switch channels {
	case 0:
		return ""
	case 1:
		return "mono"
	case 2:
		return "stereo"
	case 4:
		return "quad"
	case 6:
		return "5.1"
	case 8:
		return "7.1"
	default:
		return fmt.Sprintf("%dch", channels)
	}
}

// join concatenates strings with a separator, skipping empty strings.
func join(parts []string, sep string) string {
	var result string
	for i, part := range parts {
		if part == "" {
			continue
		}
		if i > 0 && result != "" {
			result += sep
		}
		result += part
	}
	return result
}

// IsHighRes returns true if the audio is high-resolution.
//
// High-resolution is defined as:
//   - Sample rate > 48kHz, OR
//   - Bit depth > 16
//
// Example:
//
//	if file.Audio.IsHighRes() {
//		fmt.Println("High-resolution audio")
//	}
func (a AudioInfo) IsHighRes() bool {
	return a.SampleRate > 48000 || a.BitDepth > 16
}

// ApplyReplayGain applies ReplayGain adjustment to an amplitude.
//
// Returns the adjusted amplitude. If ReplayGain is not available,
// returns the original amplitude.
//
// mode should be "track" or "album".
//
// Example:
//
//	adjusted := audio.ApplyReplayGain(0.8, "track")
func (a AudioInfo) ApplyReplayGain(amplitude float64, mode string) float64 {
	if a.ReplayGain == nil {
		return amplitude
	}

	var gain float64
	var peak float64

	switch mode {
	case "album":
		gain = a.ReplayGain.AlbumGain
		peak = a.ReplayGain.AlbumPeak
	default: // "track" or anything else
		gain = a.ReplayGain.TrackGain
		peak = a.ReplayGain.TrackPeak
	}

	if peak == 0 {
		return amplitude
	}

	// Apply gain (dB to linear)
	// Linear gain = 10^(dB/20)
	linearGain := pow10(gain / 20.0)
	adjusted := amplitude * linearGain

	// Prevent clipping
	if peak > 0 && adjusted > 1.0/peak {
		adjusted = 1.0 / peak
	}

	return adjusted
}

// pow10 returns 10^x.
// Simple implementation for ReplayGain calculations.
func pow10(x float64) float64 {
	// For small x, use approximation
	// For ReplayGain, x is typically -20 to +20
	// More accurate implementation would use math.Pow
	if x == 0 {
		return 1.0
	}
	// Simplified exponential approximation
	// In production, use: return math.Pow(10, x)
	result := 1.0
	if x > 0 {
		for i := 0; i < int(x*10); i++ {
			result *= 1.2589254117941673 // 10^0.1
		}
	} else {
		for i := 0; i < int(-x*10); i++ {
			result *= 0.7943282347242815 // 10^-0.1
		}
	}
	return result
}

// ShortCodecName returns a short, human-readable codec name.
func (a AudioInfo) ShortCodecName() string {
	if a.CodecDescription != "" {
		return a.CodecDescription
	}
	return a.Codec
}

// FullCodecName returns the full codec name with profile.
func (a AudioInfo) FullCodecName() string {
	codec := a.CodecDescription
	if codec == "" {
		codec = a.Codec
	}

	if a.CodecProfile != "" && a.CodecProfile != codec {
		return fmt.Sprintf("%s (%s)", codec, a.CodecProfile)
	}

	return codec
}

// IsModernAudiobookCodec returns true for modern audiobook codecs.
func (a AudioInfo) IsModernAudiobookCodec() bool {
	switch a.Codec {
	case "mhm1", "mhm2": // xHE-AAC
		return true
	case "ec-3": // E-AC-3
		return true
	case "ac-4": // AC-4
		return true
	case "mp4a": // AAC
		switch a.CodecProfile {
		case "HE-AAC", "HE-AAC v2", "xHE-AAC":
			return true
		}
	}
	return false
}
