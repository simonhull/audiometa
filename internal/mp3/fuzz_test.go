package mp3

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/simonhull/audiometa/internal/testbudget"
	"github.com/simonhull/audiometa/internal/types"
)

func FuzzParseMP3(f *testing.F) {
	for _, name := range []string{"sample.mp3", "sample_v24.mp3", "sample_mono_vbr.mp3", "sample_mpeg2.mp3", "bench/book.mp3"} {
		if data, err := os.ReadFile(filepath.Join("..", "..", "testdata", name)); err == nil {
			f.Add(data)
		}
	}
	f.Add([]byte("ID3\x03\x00\x00\x00\x00\x00\x00"))
	f.Add([]byte{})

	p := &parser{}
	f.Fuzz(func(t *testing.T, data []byte) {
		// Must not panic or hang on any input; errors are fine.
		budget := testbudget.For(testbudget.Read, types.FormatMP3)
		testbudget.Check(t, budget, len(data), "Parse", func() {
			_, _ = p.Parse(context.Background(), bytes.NewReader(data), int64(len(data)), "fuzz.mp3")
		})
		testbudget.Check(t, budget, len(data), "ExtractArtwork", func() {
			_, _ = p.ExtractArtwork(context.Background(), bytes.NewReader(data), int64(len(data)), "fuzz.mp3")
		})
	})
}
