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
	t.Parallel()
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

func TestParseTechnicalInfo_OneRead(t *testing.T) {
	t.Parallel()
	// An MPEG-1 Layer III 128 kbps 44.1 kHz stereo frame (417 bytes) carrying a
	// Xing header with a frame count, then two more frames.
	frame := func() []byte { return append([]byte{0xFF, 0xFB, 0x90, 0x00}, make([]byte, 413)...) }
	first := frame()
	copy(first[4+32:], []byte{'X', 'i', 'n', 'g', 0, 0, 0, 1, 0, 0, 0, 100})
	data := bytes.Join([][]byte{make([]byte, 6000), first, frame(), frame(), make([]byte, 200)}, nil)
	cr := &countingReaderAt{r: bytes.NewReader(data)}
	sr := binutil.NewSafeReader(cr, int64(len(data)), "xing.mp3")
	file := &types.File{}

	if err := parseTechnicalInfo(context.Background(), sr, 0, int64(len(data)), file); err != nil {
		t.Fatal(err)
	}
	if !file.Audio.VBR || file.Audio.Duration == 0 {
		t.Errorf("audio = %+v, want the Xing frame count used", file.Audio)
	}
	if cr.calls != 1 {
		t.Errorf("ReadAt calls = %d, want 1 (the frame window)", cr.calls)
	}
}

func TestParseTechnical_FindsFrameAfterPadding(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
	data := make([]byte, maxFrameSearch+100)
	copy(data[maxFrameSearch+10:], []byte{0xFF, 0xFB, 0x90, 0x00})
	sr := binutil.NewSafeReader(bytes.NewReader(data), int64(len(data)), "far.mp3")

	err := parseTechnicalInfo(context.Background(), sr, 0, int64(len(data)), &types.File{})
	if !errors.Is(err, errNoMP3Frame) {
		t.Fatalf("err = %v, want errNoMP3Frame", err)
	}
}

func TestParseTechnical_MPEG25(t *testing.T) {
	t.Parallel()
	// MPEG-2.5 Layer III, 8 kbps, 8 kHz, mono: 72 * 8000 / 8000 = 72-byte frames.
	frame := append([]byte{0xFF, 0xE3, 0x18, 0xC4}, make([]byte, 72-4)...)
	f := parseBytes(t, bytes.Repeat(frame, 10))
	if a := f.Audio; a.SampleRate != 8000 || a.Channels != 1 || a.Bitrate != 8000 || len(f.Warnings) != 0 {
		t.Errorf("audio = %+v, warnings %v; want 8000 Hz mono 8 kbps", a, f.Warnings)
	}
}

func TestParseTechnical_SkipsUnconfirmedSync(t *testing.T) {
	t.Parallel()
	// A 64 kbps header with nothing at its next-frame offset (+208), then real 128 kbps frames.
	data := append([]byte{0xFF, 0xFB, 0x50, 0x00}, make([]byte, 96)...)
	data = append(data, audioBytes(16000)...)
	f := parseBytes(t, data)
	if f.Audio.Bitrate != 128000 {
		t.Errorf("bitrate = %d, want 128000 (the false sync at 0 must be skipped)", f.Audio.Bitrate)
	}
}
