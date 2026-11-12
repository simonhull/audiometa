package audiometa

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

// ioFixture is a realistic file for the I/O benchmarks.
type ioFixture struct {
	name string
	path string
}

// ioCover is a 256 KiB stand-in JPEG: big enough to span several read windows.
func ioCover() []byte {
	return append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, bytes.Repeat([]byte{0x5A}, 256<<10)...)
}

// ioFixtures returns testdata/bench files as they are, plus FLAC, MP3 and M4B
// copies with a 256 KiB front cover added through Write. When
// AUDIOMETA_IO_CORPUS names a directory, its regular files are added too (for
// measuring real libraries; never required).
func ioFixtures(tb testing.TB) []ioFixture {
	tb.Helper()
	var out []ioFixture
	for _, name := range benchFixtures {
		out = append(out, ioFixture{name: name, path: filepath.Join("testdata", "bench", name)})
	}
	dir := tb.TempDir()
	for _, name := range []string{"book.flac", "book.mp3", "book.m4b"} {
		data, err := os.ReadFile(filepath.Join("testdata", "bench", name))
		if err != nil {
			tb.Fatal(err)
		}
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, data, 0o600); err != nil {
			tb.Fatal(err)
		}
		err = Write(path, func(f *File) error {
			return f.AddArtwork(Artwork{Data: ioCover(), MIMEType: "image/jpeg", Type: ArtworkFrontCover})
		})
		if err != nil {
			tb.Fatalf("add cover to %s: %v", name, err)
		}
		out = append(out, ioFixture{name: name + "+cover", path: path})
	}
	if corpus := os.Getenv("AUDIOMETA_IO_CORPUS"); corpus != "" {
		entries, err := os.ReadDir(corpus)
		if err != nil {
			tb.Fatal(err)
		}
		for _, e := range entries {
			if !e.Type().IsRegular() {
				continue
			}
			path := filepath.Join(corpus, e.Name())
			if _, err := openForBench(path); err != nil {
				continue
			}
			out = append(out, ioFixture{name: e.Name(), path: path})
		}
	}
	return out
}

// openForBench opens path through openReader and returns the parse result.
func openForBench(path string) (*parsedFile, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err //nolint:wrapcheck // test helper returns the error for the test to report
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err //nolint:wrapcheck // test helper returns the error for the test to report
	}
	return openReader(context.Background(), f, st.Size(), path)
}

// ioRun opens fx once and, when step is not nil, calls it with the parsed file
// reading through a counter that was reset after the open. It returns the
// reads and bytes requested by the open, or by step.
func ioRun(b *testing.B, fx ioFixture, step func(f *File)) (calls int, bytesRead int64) { //nolint:nonamedreturns // the names document the results
	b.Helper()
	osf, err := os.Open(fx.path)
	if err != nil {
		b.Fatal(err)
	}
	defer osf.Close()
	st, err := osf.Stat()
	if err != nil {
		b.Fatal(err)
	}
	c := &countingReaderAt{r: osf}
	pf, err := openReader(context.Background(), c, st.Size(), fx.name)
	if err != nil {
		b.Fatal(err)
	}
	if step == nil {
		return c.calls, c.bytes
	}
	*c = countingReaderAt{r: osf}
	f := &File{File: *pf.file, reader: c, parser: pf.parser, src: pf.source()}
	step(f)
	return c.calls, c.bytes
}

func benchIO(b *testing.B, step func(b *testing.B, f *File)) {
	b.Helper()
	for _, fx := range ioFixtures(b) {
		b.Run(fx.name, func(b *testing.B) {
			b.ReportAllocs()
			var calls int
			var n int64
			var s func(f *File)
			if step != nil {
				s = func(f *File) { step(b, f) }
			}
			for b.Loop() {
				calls, n = ioRun(b, fx, s)
			}
			b.ReportMetric(float64(calls), "reads/op")
			b.ReportMetric(float64(n), "bytes/op")
		})
	}
}

func TestOpenReader_ReadsHeadOnce(t *testing.T) {
	for _, name := range []string{"sample.flac", "sample.m4a", "sample.mp3", "sample.opus", "sample.wav"} {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", name))
			if err != nil {
				t.Fatal(err)
			}
			c := &countingReaderAt{r: bytes.NewReader(data)}
			if _, err := openReader(context.Background(), c, int64(len(data)), name); err != nil {
				t.Fatal(err)
			}
			zero := 0
			for _, off := range c.offs {
				if off == 0 {
					zero++
				}
			}
			if zero > 1 {
				t.Errorf("%d reads at offset 0 (offsets %v), want at most 1", zero, c.offs)
			}
		})
	}
}

// BenchmarkIOOpen reports the reads and bytes requested by one open.
func BenchmarkIOOpen(b *testing.B) { benchIO(b, nil) }

// BenchmarkIOArtwork reports the reads and bytes requested by ExtractArtwork
// after an open.
func BenchmarkIOArtwork(b *testing.B) {
	benchIO(b, func(b *testing.B, f *File) {
		b.Helper()
		if _, err := f.ExtractArtwork(); err != nil {
			b.Fatal(err)
		}
	})
}

// BenchmarkIODigest reports the reads and bytes requested by AudioDigest after
// an open.
func BenchmarkIODigest(b *testing.B) {
	benchIO(b, func(b *testing.B, f *File) {
		b.Helper()
		if _, err := f.AudioDigest(); err != nil {
			b.Fatal(err)
		}
	})
}
