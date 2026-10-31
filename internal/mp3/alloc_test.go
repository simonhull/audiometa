package mp3

import (
	"bytes"
	"testing"

	binutil "github.com/simonhull/audiometa/internal/binary"
	"github.com/simonhull/audiometa/internal/types"
)

func TestReadSingleFrame_OversizedFrameWarns(t *testing.T) {
	// ID3v2.3 frame header declaring a ~2 GB non-synchsafe size in a tiny file.
	data := []byte{'T', 'I', 'T', '2', 0x7F, 0xFF, 0xFF, 0xFF, 0, 0, 0, 'a', 'b'}
	sr := binutil.NewSafeReader(bytes.NewReader(data), int64(len(data)), "t.mp3")
	file := &types.File{}

	frame, _, done := readSingleFrame(sr, file, ID3v2Header{Version: 3}, 0)
	if frame != nil || done {
		t.Fatalf("frame=%v done=%v, want nil/false", frame, done)
	}
	if len(file.Warnings) != 1 {
		t.Fatalf("warnings = %d, want 1", len(file.Warnings))
	}
}
