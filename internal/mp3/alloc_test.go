package mp3

import (
	"bytes"
	"context"
	"strings"
	"testing"

	binutil "github.com/simonhull/audiometa/internal/binary"
	"github.com/simonhull/audiometa/internal/types"
)

func TestParseID3v2_OversizedFrameWarns(t *testing.T) {
	t.Parallel()
	// ID3v2.3 TIT2 frame header declaring a ~2 GB non-synchsafe size in a tiny tag.
	data := tagBytes(3, 0, []byte{'T', 'I', 'T', '2', 0x7F, 0xFF, 0xFF, 0xFF, 0, 0, 0, 'a', 'b'})
	sr := binutil.NewSafeReader(bytes.NewReader(data), int64(len(data)), "t.mp3")
	file := &types.File{}
	if _, err := parseID3v2(context.Background(), sr, file); err != nil {
		t.Fatal(err)
	}
	if len(file.Warnings) != 1 || !strings.Contains(file.Warnings[0].Message, "failed to read frame TIT2") {
		t.Fatalf("warnings = %v, want one 'failed to read frame TIT2'", file.Warnings)
	}
}
