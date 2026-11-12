package ogg

import (
	"context"
	"encoding/binary"
	"errors"
	"testing"

	"github.com/simonhull/audiometa/internal/types"
	"github.com/simonhull/audiometa/internal/vorbis"
)

// TestCommentHeaderHugeLength feeds both comment headers a length close to 4 GiB. On 32-bit
// platforms such a length used to wrap negative and panic.
func TestCommentHeaderHugeLength(t *testing.T) {
	t.Parallel()
	huge := binary.LittleEndian.AppendUint32(nil, 0xFFFFFFF0)
	vendorThenHuge := binary.LittleEndian.AppendUint32(nil, 1)
	vendorThenHuge = append(vendorThenHuge, 'v')
	vendorThenHuge = binary.LittleEndian.AppendUint32(vendorThenHuge, 1)
	vendorThenHuge = append(vendorThenHuge, huge...)
	vendorThenHuge = append(vendorThenHuge, "xx"...)

	codecs := []struct {
		name  string
		magic string
		parse func(ctx context.Context, data []byte, file *types.File) error
	}{
		{"vorbis", "\x03vorbis", parseVorbisComment},
		{"opus", "OpusTags", parseOpusTags},
	}
	bodies := []struct {
		name string
		body []byte
	}{
		{"vendor", append(append([]byte{}, huge...), "xx"...)},
		{"comment", vendorThenHuge},
	}
	for _, c := range codecs {
		for _, b := range bodies {
			t.Run(c.name+"/"+b.name, func(t *testing.T) {
				t.Parallel()
				var file types.File
				if err := c.parse(t.Context(), append([]byte(c.magic), b.body...), &file); err != nil {
					t.Fatalf("err = %v, want a warning only", err)
				}
				if len(file.Warnings) != 1 {
					t.Fatalf("warnings = %v, want exactly one", file.Warnings)
				}
				if w := file.Warnings[0]; w.Stage != types.StageMetadata || !errors.Is(w.Err, vorbis.ErrTruncated) {
					t.Errorf("warning = %+v, want StageMetadata wrapping vorbis.ErrTruncated", w)
				}
			})
		}
	}
}
