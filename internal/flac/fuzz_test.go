package flac

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func FuzzParseFLAC(f *testing.F) {
	for _, name := range []string{"sample.flac", "sample_upper.flac"} {
		if data, err := os.ReadFile(filepath.Join("..", "..", "testdata", name)); err == nil {
			f.Add(data)
		}
	}
	f.Add([]byte("fLaC"))
	f.Add([]byte{})

	p := &parser{}
	f.Fuzz(func(t *testing.T, data []byte) {
		// Must not panic or hang on any input; errors are fine.
		_, _ = p.Parse(context.Background(), bytes.NewReader(data), int64(len(data)), "fuzz.flac")
		_, _ = p.ExtractArtwork(context.Background(), bytes.NewReader(data), int64(len(data)), "fuzz.flac")
	})
}
