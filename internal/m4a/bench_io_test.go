package m4a

import (
	"bytes"
	"context"
	"io"
	"testing"
)

// countingReaderAt counts ReadAt calls and bytes requested.
type countingReaderAt struct {
	r     io.ReaderAt
	calls int
	bytes int64
}

func (c *countingReaderAt) ReadAt(p []byte, off int64) (int, error) {
	c.calls++
	c.bytes += int64(len(p))
	return c.r.ReadAt(p, off)
}

// BenchmarkIOParse reports the reads and bytes requested by Parse on files
// whose ilst has a 256 KiB cover in the middle, so items on both sides of it
// sit in different windows.
func BenchmarkIOParse(b *testing.B) {
	cover := append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, bytes.Repeat([]byte{0x5A}, 256<<10)...)
	items := [][]byte{
		ilstText("\xA9nam", "T"),
		covrItem(covrImage{13, cover}),
		ilstText("\xA9ART", "A"),
		ilstFreeform("REPLAYGAIN_TRACK_GAIN", "-1 dB"),
	}
	inputs := []struct {
		name string
		data []byte
	}{
		{"moov-at-end", buildMP4With(mp4Opts{items: items})},
		{"faststart", buildMP4With(mp4Opts{faststart: true, items: items})},
	}
	for _, in := range inputs {
		b.Run(in.name, func(b *testing.B) {
			br := bytes.NewReader(in.data)
			b.ReportAllocs()
			var c *countingReaderAt
			for b.Loop() {
				c = &countingReaderAt{r: br}
				if _, err := (&parser{}).Parse(context.Background(), c, int64(len(in.data)), "x.m4a"); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportMetric(float64(c.calls), "reads/op")
			b.ReportMetric(float64(c.bytes), "bytes/op")
		})
	}
}
