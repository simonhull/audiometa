package mp3

import (
	"testing"
	"time"
)

// makeHeader builds a Layer III frame header word (no CRC).
func makeHeader(version, bitrateIdx, sampleRateIdx, channelMode uint32) uint32 {
	return 0xFFE00000 | version<<19 | 1<<17 | 1<<16 | bitrateIdx<<12 | sampleRateIdx<<10 | channelMode<<6
}

func TestParseMP3FrameHeader(t *testing.T) {
	cases := []struct {
		name                          string
		header                        uint32
		bitrate, sampleRate, channels int
	}{
		{"MPEG-1 128k 44.1k stereo", makeHeader(3, 9, 0, 0), 128000, 44100, 2},
		{"MPEG-1 320k 32k joint", makeHeader(3, 14, 2, 1), 320000, 32000, 2},
		{"MPEG-2 32k 22.05k mono", makeHeader(2, 4, 0, 3), 32000, 22050, 1},
		{"MPEG-2 64k 24k stereo", makeHeader(2, 8, 1, 0), 64000, 24000, 2},
		{"MPEG-2 160k 16k", makeHeader(2, 14, 2, 0), 160000, 16000, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			br, sr, ch := parseMP3FrameHeader(tc.header)
			if br != tc.bitrate || sr != tc.sampleRate || ch != tc.channels {
				t.Errorf("got (%d, %d, %d), want (%d, %d, %d)", br, sr, ch, tc.bitrate, tc.sampleRate, tc.channels)
			}
		})
	}
}

func TestSamplesPerFrameAndDuration(t *testing.T) {
	if got := samplesPerFrame(makeHeader(3, 9, 0, 0)); got != 1152 {
		t.Errorf("MPEG-1 samples/frame = %d", got)
	}
	v2 := makeHeader(2, 4, 0, 3)
	if got := samplesPerFrame(v2); got != 576 {
		t.Errorf("MPEG-2 samples/frame = %d", got)
	}
	// 1000 MPEG-2 frames at 22050 Hz = 1000*576/22050 s.
	got := calculateDurationFromFrames(1000, 22050, samplesPerFrame(v2))
	const want = 26122449 * time.Microsecond // 576000 / 22050 s
	if diff := got - want; diff < -time.Millisecond || diff > time.Millisecond {
		t.Errorf("duration = %v, want %v", got, want)
	}
}
