package m4a

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func FuzzParseM4A(f *testing.F) {
	for _, name := range []string{"sample.m4a", "sample.m4b"} {
		if data, err := os.ReadFile(filepath.Join("..", "..", "testdata", name)); err == nil {
			f.Add(data)
		}
	}
	f.Add([]byte("\x00\x00\x00\x14ftypM4B \x00\x00\x00\x00M4B \x00\x00\x00\x08moov"))
	f.Add([]byte{})

	p := &parser{}
	f.Fuzz(func(t *testing.T, data []byte) {
		// Must not panic or hang on any input; errors are fine.
		_, _ = p.Parse(context.Background(), bytes.NewReader(data), int64(len(data)), "fuzz.m4a")
		_, _ = p.ExtractArtwork(context.Background(), bytes.NewReader(data), int64(len(data)), "fuzz.m4a")
	})
}
