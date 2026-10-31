package mp3

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func FuzzParseMP3(f *testing.F) {
	for _, name := range []string{"sample.mp3"} {
		if data, err := os.ReadFile(filepath.Join("..", "..", "testdata", name)); err == nil {
			f.Add(data)
		}
	}
	f.Add([]byte("ID3\x03\x00\x00\x00\x00\x00\x00"))
	f.Add([]byte{})

	p := &parser{}
	f.Fuzz(func(t *testing.T, data []byte) {
		// Must not panic or hang on any input; errors are fine.
		_, _ = p.Parse(context.Background(), bytes.NewReader(data), int64(len(data)), "fuzz.mp3")
		_, _ = p.ExtractArtwork(context.Background(), bytes.NewReader(data), int64(len(data)), "fuzz.mp3")
	})
}
