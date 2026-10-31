package ogg

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func FuzzParseOgg(f *testing.F) {
	for _, name := range []string{"sample.ogg", "sample.opus"} {
		if data, err := os.ReadFile(filepath.Join("..", "..", "testdata", name)); err == nil {
			f.Add(data)
		}
	}
	f.Add([]byte("OggS\x00\x02"))
	f.Add([]byte{})

	p := &parser{}
	f.Fuzz(func(t *testing.T, data []byte) {
		// Must not panic or hang on any input; errors are fine.
		_, _ = p.Parse(context.Background(), bytes.NewReader(data), int64(len(data)), "fuzz.ogg")
	})
}
