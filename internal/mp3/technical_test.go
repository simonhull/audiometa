package mp3

import (
	"bytes"
	"context"
	"errors"
	"testing"

	binutil "github.com/simonhull/audiometa/internal/binary"
	"github.com/simonhull/audiometa/internal/types"
)

// countingReaderAt counts ReadAt calls and the bytes they request.
type countingReaderAt struct {
	r     *bytes.Reader
	calls int
}

func (c *countingReaderAt) ReadAt(p []byte, off int64) (int, error) {
	c.calls++
	return c.r.ReadAt(p, off)
}

func TestParseTechnical_JunkAfterTag_Bounded(t *testing.T) {
	data := make([]byte, 200<<10) // 200 KiB with no frame sync anywhere
	cr := &countingReaderAt{r: bytes.NewReader(data)}
	sr := binutil.NewSafeReader(cr, int64(len(data)), "junk.mp3")

	err := parseTechnicalInfo(context.Background(), sr, 0, int64(len(data)), &types.File{})
	if !errors.Is(err, errNoMP3Frame) {
		t.Fatalf("err = %v, want errNoMP3Frame", err)
	}
	if cr.calls != 1 {
		t.Errorf("ReadAt calls = %d, want 1 (one buffered window, no per-byte reads)", cr.calls)
	}
}

func TestParseTechnical_FindsFrameAfterPadding(t *testing.T) {
	// 1000 bytes of padding, then an MPEG-1 Layer III 128 kbps 44.1 kHz stereo header.
	data := make([]byte, 1000, 1100)
	data = append(data, 0xFF, 0xFB, 0x90, 0x00)
	data = append(data, make([]byte, 96)...)
	sr := binutil.NewSafeReader(bytes.NewReader(data), int64(len(data)), "pad.mp3")
	file := &types.File{}

	if err := parseTechnicalInfo(context.Background(), sr, 0, int64(len(data)), file); err != nil {
		t.Fatal(err)
	}
	if file.Audio.Bitrate != 128000 || file.Audio.SampleRate != 44100 || file.Audio.Codec != "MP3" {
		t.Errorf("audio = %+v", file.Audio)
	}
}

func TestParseTechnical_FrameBeyondWindowNotFound(t *testing.T) {
	data := make([]byte, maxFrameSearch+100)
	copy(data[maxFrameSearch+10:], []byte{0xFF, 0xFB, 0x90, 0x00})
	sr := binutil.NewSafeReader(bytes.NewReader(data), int64(len(data)), "far.mp3")

	err := parseTechnicalInfo(context.Background(), sr, 0, int64(len(data)), &types.File{})
	if !errors.Is(err, errNoMP3Frame) {
		t.Fatalf("err = %v, want errNoMP3Frame", err)
	}
}
