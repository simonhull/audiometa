package types_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/simonhull/audiometa/internal/testbudget"
	"github.com/simonhull/audiometa/internal/types"
)

func FuzzDetectFormat(f *testing.F) {
	for _, name := range []string{"sample.mp3", "sample.flac", "sample.m4a", "sample.m4b", "sample.ogg", "sample.opus", "sample.wav", "sample_rf64.wav", "sample.aiff", "sample.aifc"} {
		if data, err := os.ReadFile(filepath.Join("..", "..", "testdata", name)); err == nil {
			f.Add(data[:min(len(data), 256)]) // detection reads only the head
		}
	}
	f.Add([]byte("ID3"))
	f.Fuzz(func(t *testing.T, data []byte) {
		var format types.Format
		var err error
		testbudget.Check(t, testbudget.For(testbudget.Read, types.FormatUnknown), len(data), "DetectFormat", func() {
			format, err = types.DetectFormat(bytes.NewReader(data), int64(len(data)), "fuzz")
		})
		if (err == nil) == (format == types.FormatUnknown) {
			t.Fatalf("DetectFormat = %v, %v: a format comes with nil error and only then", format, err)
		}
		var ue *types.UnsupportedFormatError
		if err != nil && !errors.As(err, &ue) {
			t.Fatalf("error %T %v, want *UnsupportedFormatError", err, err)
		}
	})
}
