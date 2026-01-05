package types

import (
	"math"
	"testing"
	"time"
)

func TestPow10(t *testing.T) {
	tests := []struct {
		input    float64
		expected float64
	}{
		{0, 1.0},
		{1, 10.0},
		{-1, 0.1},
		{2, 100.0},
		{-2, 0.01},
		{0.5, math.Sqrt(10)}, // sqrt(10) ≈ 3.162...
		{3, 1000.0},
		{-3, 0.001},
	}

	for _, tc := range tests {
		got := pow10(tc.input)
		if math.Abs(got-tc.expected) > 1e-9 {
			t.Errorf("pow10(%v) = %v, want %v", tc.input, got, tc.expected)
		}
	}
}

func TestAudioInfo_String(t *testing.T) {
	tests := []struct {
		name  string
		audio AudioInfo
		want  string
	}{
		{
			name: "full info",
			audio: AudioInfo{
				Codec:      "FLAC",
				SampleRate: 44100,
				BitDepth:   16,
				Channels:   2,
				Lossless:   true,
			},
			want: "FLAC 44.1kHz 16-bit stereo lossless",
		},
		{
			name: "lossy with bitrate",
			audio: AudioInfo{
				Codec:      "AAC",
				SampleRate: 44100,
				Channels:   2,
				Bitrate:    256000,
			},
			want: "AAC 44.1kHz stereo 256kbps",
		},
		{
			name: "VBR",
			audio: AudioInfo{
				Codec:      "MP3",
				SampleRate: 44100,
				Channels:   2,
				Bitrate:    320000,
				VBR:        true,
			},
			want: "MP3 44.1kHz stereo 320kbps VBR",
		},
		{
			name: "mono",
			audio: AudioInfo{
				Codec:      "AAC",
				SampleRate: 48000,
				Channels:   1,
			},
			want: "AAC 48.0kHz mono",
		},
		{
			name: "5.1 surround",
			audio: AudioInfo{
				Codec:    "AC3",
				Channels: 6,
			},
			want: "AC3 0.0kHz 5.1",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.audio.String()
			if got != tc.want {
				t.Errorf("AudioInfo.String() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestChannelDescription(t *testing.T) {
	tests := []struct {
		channels int
		want     string
	}{
		{0, ""},
		{1, "mono"},
		{2, "stereo"},
		{4, "quad"},
		{6, "5.1"},
		{8, "7.1"},
		{3, "3ch"},
		{10, "10ch"},
	}

	for _, tc := range tests {
		got := channelDescription(tc.channels)
		if got != tc.want {
			t.Errorf("channelDescription(%d) = %q, want %q", tc.channels, got, tc.want)
		}
	}
}

func TestAudioInfo_IsHighRes(t *testing.T) {
	tests := []struct {
		name  string
		audio AudioInfo
		want  bool
	}{
		{
			name:  "CD quality - not high res",
			audio: AudioInfo{SampleRate: 44100, BitDepth: 16},
			want:  false,
		},
		{
			name:  "48kHz 16-bit - not high res",
			audio: AudioInfo{SampleRate: 48000, BitDepth: 16},
			want:  false,
		},
		{
			name:  "96kHz - high res",
			audio: AudioInfo{SampleRate: 96000, BitDepth: 16},
			want:  true,
		},
		{
			name:  "44.1kHz 24-bit - high res",
			audio: AudioInfo{SampleRate: 44100, BitDepth: 24},
			want:  true,
		},
		{
			name:  "192kHz 24-bit - high res",
			audio: AudioInfo{SampleRate: 192000, BitDepth: 24},
			want:  true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.audio.IsHighRes()
			if got != tc.want {
				t.Errorf("AudioInfo.IsHighRes() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestAudioInfo_ApplyReplayGain(t *testing.T) {
	tests := []struct {
		name      string
		audio     AudioInfo
		amplitude float64
		mode      string
		want      float64
		tolerance float64
	}{
		{
			name:      "no replay gain",
			audio:     AudioInfo{},
			amplitude: 0.8,
			mode:      "track",
			want:      0.8,
			tolerance: 0.0,
		},
		{
			name: "track gain positive",
			audio: AudioInfo{
				ReplayGain: &ReplayGainInfo{
					TrackGain: 6.0,
					TrackPeak: 0.5,
				},
			},
			amplitude: 0.5,
			mode:      "track",
			want:      0.5 * math.Pow(10, 6.0/20.0), // ~0.997
			tolerance: 0.001,
		},
		{
			name: "track gain negative",
			audio: AudioInfo{
				ReplayGain: &ReplayGainInfo{
					TrackGain: -6.0,
					TrackPeak: 1.0,
				},
			},
			amplitude: 1.0,
			mode:      "track",
			want:      math.Pow(10, -6.0/20.0), // ~0.501
			tolerance: 0.001,
		},
		{
			name: "album mode",
			audio: AudioInfo{
				ReplayGain: &ReplayGainInfo{
					TrackGain: -3.0,
					TrackPeak: 1.0,
					AlbumGain: -6.0,
					AlbumPeak: 0.9,
				},
			},
			amplitude: 1.0,
			mode:      "album",
			want:      math.Pow(10, -6.0/20.0),
			tolerance: 0.001,
		},
		{
			name: "zero peak returns original",
			audio: AudioInfo{
				ReplayGain: &ReplayGainInfo{
					TrackGain: 6.0,
					TrackPeak: 0.0,
				},
			},
			amplitude: 0.8,
			mode:      "track",
			want:      0.8,
			tolerance: 0.0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.audio.ApplyReplayGain(tc.amplitude, tc.mode)
			if math.Abs(got-tc.want) > tc.tolerance {
				t.Errorf("ApplyReplayGain(%v, %q) = %v, want %v (±%v)", tc.amplitude, tc.mode, got, tc.want, tc.tolerance)
			}
		})
	}
}

func TestAudioInfo_ShortCodecName(t *testing.T) {
	tests := []struct {
		name  string
		audio AudioInfo
		want  string
	}{
		{
			name:  "codec only",
			audio: AudioInfo{Codec: "mp4a"},
			want:  "mp4a",
		},
		{
			name:  "with description",
			audio: AudioInfo{Codec: "mp4a", CodecDescription: "AAC-LC"},
			want:  "AAC-LC",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.audio.ShortCodecName()
			if got != tc.want {
				t.Errorf("ShortCodecName() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestAudioInfo_FullCodecName(t *testing.T) {
	tests := []struct {
		name  string
		audio AudioInfo
		want  string
	}{
		{
			name:  "codec only",
			audio: AudioInfo{Codec: "FLAC"},
			want:  "FLAC",
		},
		{
			name:  "with description",
			audio: AudioInfo{Codec: "mp4a", CodecDescription: "AAC"},
			want:  "AAC",
		},
		{
			name:  "with profile",
			audio: AudioInfo{Codec: "mp4a", CodecDescription: "AAC", CodecProfile: "HE-AAC v2"},
			want:  "AAC (HE-AAC v2)",
		},
		{
			name:  "profile same as codec",
			audio: AudioInfo{Codec: "FLAC", CodecProfile: "FLAC"},
			want:  "FLAC",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.audio.FullCodecName()
			if got != tc.want {
				t.Errorf("FullCodecName() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestAudioInfo_IsModernAudiobookCodec(t *testing.T) {
	tests := []struct {
		name  string
		audio AudioInfo
		want  bool
	}{
		{name: "xHE-AAC mhm1", audio: AudioInfo{Codec: "mhm1"}, want: true},
		{name: "xHE-AAC mhm2", audio: AudioInfo{Codec: "mhm2"}, want: true},
		{name: "E-AC-3", audio: AudioInfo{Codec: "ec-3"}, want: true},
		{name: "AC-4", audio: AudioInfo{Codec: "ac-4"}, want: true},
		{name: "HE-AAC", audio: AudioInfo{Codec: "mp4a", CodecProfile: "HE-AAC"}, want: true},
		{name: "HE-AAC v2", audio: AudioInfo{Codec: "mp4a", CodecProfile: "HE-AAC v2"}, want: true},
		{name: "xHE-AAC profile", audio: AudioInfo{Codec: "mp4a", CodecProfile: "xHE-AAC"}, want: true},
		{name: "regular AAC", audio: AudioInfo{Codec: "mp4a", CodecProfile: "AAC-LC"}, want: false},
		{name: "MP3", audio: AudioInfo{Codec: "mp3"}, want: false},
		{name: "FLAC", audio: AudioInfo{Codec: "FLAC"}, want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.audio.IsModernAudiobookCodec()
			if got != tc.want {
				t.Errorf("IsModernAudiobookCodec() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestAudioInfo_Duration(t *testing.T) {
	audio := AudioInfo{Duration: 3*time.Minute + 45*time.Second}
	if audio.Duration != 225*time.Second {
		t.Errorf("Duration = %v, want %v", audio.Duration, 225*time.Second)
	}
}
