package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/simonhull/audiometa/internal/binary"
	"github.com/simonhull/audiometa/internal/testbudget"
	"github.com/simonhull/audiometa/internal/types"
)

func FuzzWalkAtoms(f *testing.F) {
	for _, name := range []string{"sample.m4a", "sample_faststart.m4b", "bench/book.m4b"} {
		if data, err := os.ReadFile(filepath.Join("..", "..", "testdata", name)); err == nil {
			f.Add(data)
		}
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		testbudget.Check(t, testbudget.For(testbudget.Read, types.FormatM4A), len(data), "walkAtoms", func() {
			walkAtoms(binary.NewSafeReader(bytes.NewReader(data), int64(len(data)), "fuzz"), 0, int64(len(data)), 0, &printer{w: io.Discard})
		})
	})
}
