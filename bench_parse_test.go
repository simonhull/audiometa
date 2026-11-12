package audiometa

import (
	"bytes"
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"flag"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// goldenOptions makes encoding/json/v2 write what the golden files recorded: two-space
// indentation, nil slices and maps as null, and durations as integer nanoseconds.
var goldenOptions = json.JoinOptions(
	jsontext.WithIndent("  "),
	json.FormatNilSliceAsNull(true),
	json.FormatNilMapAsNull(true),
	json.WithMarshalers(json.MarshalToFunc(func(enc *jsontext.Encoder, d time.Duration) error {
		return enc.WriteToken(jsontext.Int(int64(d)))
	})),
)

var update = flag.Bool("update", false, "rewrite golden files")

// countingReaderAt counts ReadAt calls and bytes requested on the wrapped
// reader. It records each call's offset in offs.
type countingReaderAt struct {
	r     io.ReaderAt
	calls int
	bytes int64
	offs  []int64
}

func (c *countingReaderAt) ReadAt(p []byte, off int64) (int, error) {
	c.calls++
	c.bytes += int64(len(p))
	c.offs = append(c.offs, off)
	return c.r.ReadAt(p, off)
}

var benchFixtures = []string{"book.m4b", "book.mp3", "book.flac", "book.opus"}

// BenchmarkParse measures in-memory parsing (no syscalls) and reports how many
// ReadAt calls one open issues, the proxy for pread syscalls on a real file.
func BenchmarkParse(b *testing.B) {
	for _, name := range benchFixtures {
		b.Run(name, func(b *testing.B) {
			data, err := os.ReadFile(filepath.Join("testdata", "bench", name))
			if err != nil {
				b.Fatal(err)
			}
			br := bytes.NewReader(data)
			b.ReportAllocs()
			var calls int
			for b.Loop() {
				c := &countingReaderAt{r: br}
				if _, err := openReader(context.Background(), c, int64(len(data)), name); err != nil {
					b.Fatal(err)
				}
				calls = c.calls
			}
			b.ReportMetric(float64(calls), "readat/op")
		})
	}
}

// BenchmarkOpenFile measures the public Open on a real file, syscalls included.
func BenchmarkOpenFile(b *testing.B) {
	for _, name := range benchFixtures {
		b.Run(name, func(b *testing.B) {
			path := filepath.Join("testdata", "bench", name)
			b.ReportAllocs()
			for b.Loop() {
				f, err := Open(path)
				if err != nil {
					b.Fatal(err)
				}
				_ = f.Close()
			}
		})
	}
}

func TestBenchFixturesParse(t *testing.T) {
	wantChapters := map[string]int{"book.m4b": 50, "book.mp3": 50, "book.opus": 50, "book.flac": 0}
	for _, name := range benchFixtures {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("testdata", "bench", name))
			if err != nil {
				t.Fatal(err)
			}
			pf, err := openReader(context.Background(), bytes.NewReader(data), int64(len(data)), name)
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			if got := len(pf.file.Chapters); got != wantChapters[name] {
				t.Errorf("chapters = %d, want %d", got, wantChapters[name])
			}
			got, err := json.Marshal(pf.file, goldenOptions) //nolint:musttag // the golden files record the untagged Go field names
			if err != nil {
				t.Fatal(err)
			}
			golden := filepath.Join("testdata", "bench", name+".golden")
			if *update {
				if err := os.WriteFile(golden, got, 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("parse result differs from %s (run with -update only if the change is intended)", golden)
			}
		})
	}
}
