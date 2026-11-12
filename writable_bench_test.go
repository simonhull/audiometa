package audiometa

import (
	"path/filepath"
	"testing"
)

// BenchmarkWritable measures the dry run on the bench fixtures. Its cost
// follows the metadata and structure, not the audio, which the dry run never
// reads.
func BenchmarkWritable(b *testing.B) {
	for _, name := range benchFixtures {
		if name == "book.opus" {
			continue // no writer: refused before any read
		}
		b.Run(name, func(b *testing.B) {
			f, err := Open(filepath.Join("testdata", "bench", name))
			if err != nil {
				b.Fatal(err)
			}
			defer func() { _ = f.Close() }()
			b.ReportAllocs()
			for b.Loop() {
				if err := f.Writable(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
