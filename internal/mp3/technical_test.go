package mp3

import (
	"bytes"
	"encoding/binary"
	"testing"
	"time"

	binutil "github.com/simonhull/audiometa/internal/binary"
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

func TestSideInfoSize(t *testing.T) {
	cases := []struct {
		name   string
		header uint32
		want   int64
	}{
		{"MPEG-1 stereo", makeHeader(3, 9, 0, 0), 32},
		{"MPEG-1 joint", makeHeader(3, 9, 0, 1), 32},
		{"MPEG-1 mono", makeHeader(3, 9, 0, 3), 17},
		{"MPEG-2 stereo", makeHeader(2, 4, 0, 0), 17},
		{"MPEG-2 mono", makeHeader(2, 4, 0, 3), 9},
	}
	for _, tc := range cases {
		if got := sideInfoSize(tc.header); got != tc.want {
			t.Errorf("%s: got %d, want %d", tc.name, got, tc.want)
		}
	}
}

func TestParseVBRHeader_XingPlacement(t *testing.T) {
	const frames = 100
	cases := []struct {
		name           string
		header         uint32
		sampleRate     int
		samplesPerFrm  int
		wantXingOffset int
	}{
		{"MPEG-1 stereo", makeHeader(3, 9, 0, 0), 44100, 1152, 36},
		{"MPEG-1 mono", makeHeader(3, 9, 0, 3), 44100, 1152, 21},
		{"MPEG-2 stereo", makeHeader(2, 4, 0, 0), 22050, 576, 21},
		{"MPEG-2 mono", makeHeader(2, 4, 0, 3), 22050, 576, 13},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			buf := make([]byte, 400)
			binary.BigEndian.PutUint32(buf, tc.header)
			x := buf[tc.wantXingOffset:]
			copy(x, "Xing")
			binary.BigEndian.PutUint32(x[4:], 1) // frames flag
			binary.BigEndian.PutUint32(x[8:], frames)

			sr := binutil.NewSafeReader(bytes.NewReader(buf), int64(len(buf)), "test")
			got, ok := parseVBRHeader(sr, 0, tc.header, tc.sampleRate)
			if !ok {
				t.Fatal("Xing header not found")
			}
			want := calculateDurationFromFrames(frames, tc.sampleRate, tc.samplesPerFrm)
			if got != want {
				t.Errorf("duration = %v, want %v", got, want)
			}
		})
	}
}
