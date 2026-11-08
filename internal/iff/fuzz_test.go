package iff

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/simonhull/audiometa/internal/types"
)

func FuzzParseIFF(f *testing.F) {
	for _, name := range []string{"sample.wav", "sample_id3.wav", "sample_rf64.wav", "sample.aiff", "sample.aifc"} {
		if data, err := os.ReadFile(filepath.Join("..", "..", "testdata", name)); err == nil {
			f.Add(data)
		}
	}
	f.Add([]byte("RIFF\x04\x00\x00\x00WAVE"))
	f.Add([]byte("FORM\x00\x00\x00\x04AIFC"))
	f.Add([]byte("RF64\xff\xff\xff\xffWAVEds64"))
	f.Fuzz(func(t *testing.T, data []byte) {
		// Must not panic or hang on any input; errors are fine.
		for _, format := range []types.Format{types.FormatWAV, types.FormatAIFF} {
			ctx, r, n := context.Background(), bytes.NewReader(data), int64(len(data))
			p, w := &parser{format: format}, &writer{format: format}
			_, _ = p.Parse(ctx, r, n, "fuzz")
			_, _ = p.ExtractArtwork(ctx, r, n, "fuzz")
			_, _ = w.AudioDigest(ctx, r, n)
			_, _ = w.PreservedDigest(ctx, r, n)
			_, _ = w.WriteFile(ctx, io.Discard, r, n, &types.EditSet{Orig: &types.Tags{}, Final: &types.Tags{}, Fields: []types.FieldEdit{{Field: types.FieldTitle, Values: []string{"x"}}}})
		}
	})
}
