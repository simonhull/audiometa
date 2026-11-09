package audiometa

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// benchAudioBytes is how much audio the in-place write benchmarks add to each
// sample, so the cost of reading the audio dominates as it does for real
// albums and audiobooks.
const benchAudioBytes = 64 << 20

// bigSample copies testdata file src into a temp dir with benchAudioBytes of
// zero audio added: appended for FLAC and MP3, and inside the final mdat atom
// for M4A/M4B (the faststart samples end with an mdat that has a 32-bit size).
func bigSample(b *testing.B, src string) string {
	b.Helper()
	data, err := os.ReadFile(src)
	if err != nil {
		b.Fatal(err)
	}
	if strings.HasSuffix(src, ".m4b") || strings.HasSuffix(src, ".m4a") {
		off := bytes.LastIndex(data, []byte("mdat")) - 4
		if off < 0 || int(binary.BigEndian.Uint32(data[off:])) != len(data)-off {
			b.Fatalf("%s: mdat is not the last atom", src)
		}
		binary.BigEndian.PutUint32(data[off:], uint32(len(data)-off+benchAudioBytes))
	}
	data = append(data, make([]byte, benchAudioBytes)...)
	path := filepath.Join(b.TempDir(), filepath.Base(src))
	if err := os.WriteFile(path, data, 0o600); err != nil {
		b.Fatal(err)
	}
	return path
}

// BenchmarkWriteInPlace measures Write with WithInPlace changing one title on
// a 64 MiB file per format. It fails if Write stops patching in place.
func BenchmarkWriteInPlace(b *testing.B) {
	for _, src := range []string{"testdata/sample.flac", "testdata/sample.mp3", "testdata/sample_faststart.m4b"} {
		b.Run(filepath.Base(src), func(b *testing.B) {
			path := bigSample(b, src)
			before, err := os.Stat(path)
			if err != nil {
				b.Fatal(err)
			}
			b.SetBytes(before.Size())
			titles := [2]string{"A", "B"}
			i := 0
			for b.Loop() {
				title := titles[i%2]
				i++
				if err := Write(path, func(f *File) error { f.Tags.Title = title; return nil }, WithInPlace()); err != nil {
					b.Fatal(err)
				}
			}
			after, err := os.Stat(path)
			if err != nil {
				b.Fatal(err)
			}
			if !os.SameFile(before, after) {
				b.Fatal("Write did not patch in place")
			}
		})
	}
}
