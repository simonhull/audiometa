package audiometa_test

import (
	"testing"
	"time"

	"github.com/simonhull/audiometa"
)

// Expected values match ffprobe (stream bit_rate, format duration) for each
// fixture. Durations are exact: a LAME tag's encoder delay and padding are
// trimmed, as ffmpeg does.
func TestOpen_MP3TechnicalInfo(t *testing.T) {
	tests := []struct {
		path       string
		vbr        bool
		bitrate    int
		sampleRate int
		channels   int
		duration   time.Duration
	}{
		{"testdata/sample.mp3", false, 128000, 44100, 2, 2 * time.Second},        // CBR with an Info header
		{"testdata/sample_v24.mp3", false, 128000, 44100, 2, 2 * time.Second},    // same, ID3v2.4 + ID3v1
		{"testdata/sample_mono_vbr.mp3", true, 33292, 44100, 1, 4 * time.Second}, // Xing: bytes over duration
		{"testdata/sample_mpeg2.mp3", false, 32000, 22050, 1, 2 * time.Second},   // MPEG-2
		{"testdata/sample_mpeg25.mp3", false, 8000, 8000, 1, 2 * time.Second},    // MPEG-2.5
		{"testdata/bench/book.mp3", false, 32000, 44100, 1, 10 * time.Second},    // Info frame at 56 kbps, audio at 32
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			f, err := audiometa.Open(tt.path)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			a := f.Audio
			if a.VBR != tt.vbr || a.Bitrate != tt.bitrate || a.SampleRate != tt.sampleRate || a.Channels != tt.channels || a.Duration != tt.duration {
				t.Errorf("got VBR=%v %d bps %d Hz %d ch %v, want VBR=%v %d bps %d Hz %d ch %v",
					a.VBR, a.Bitrate, a.SampleRate, a.Channels, a.Duration,
					tt.vbr, tt.bitrate, tt.sampleRate, tt.channels, tt.duration)
			}
			if len(f.Warnings) != 0 {
				t.Errorf("warnings = %v, want none", f.Warnings)
			}
		})
	}
}
