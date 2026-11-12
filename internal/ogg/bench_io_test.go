package ogg

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"slices"
	"testing"
)

// countingReaderAt counts ReadAt calls and bytes requested, and records each
// call's offset and length.
type countingReaderAt struct {
	r     io.ReaderAt
	calls int
	bytes int64
	offs  []int64
	lens  []int
}

func (c *countingReaderAt) ReadAt(p []byte, off int64) (int, error) {
	c.calls++
	c.bytes += int64(len(p))
	c.offs = append(c.offs, off)
	c.lens = append(c.lens, len(p))
	return c.r.ReadAt(p, off)
}

// ioCover is a 256 KiB stand-in JPEG.
func ioCover() []byte {
	return append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, bytes.Repeat([]byte{0x5A}, 256<<10)...)
}

// ioOpus builds an Opus file of about 4 MB: OpusHead, OpusTags (carrying a
// 256 KiB picture when cover is set) and 240 audio pages of about 17 KB each,
// the last one ending the stream at one minute.
func ioOpus(cover bool) []byte {
	comments := []string{"TITLE=Bench", "ARTIST=Bench"}
	if cover {
		block := createTestPictureBlock(3, "image/jpeg", "", 1, 1, ioCover())
		comments = append(comments, "METADATA_BLOCK_PICTURE="+base64.StdEncoding.EncodeToString(block))
	}
	head := opusHeadPacket()
	pages := slices.Concat(
		[]pageSpec{{headerType: 0x02, serial: 1, lacing: lacingFor(len(head)), data: head}},
		splitAcrossPages(commentPacket("OpusTags", false, comments...), 0, 1),
	)
	const audioPages, audioSize = 240, 17_000
	payload := bytes.Repeat([]byte{0x7E}, audioSize)
	for i := range audioPages {
		spec := pageSpec{serial: 1, granule: uint64(48000 * 60 * (i + 1) / audioPages), lacing: lacingFor(audioSize), data: payload}
		if i == audioPages-1 {
			spec.headerType = pageEOS
		}
		pages = append(pages, spec)
	}
	return buildOgg(pages...)
}

type ioInput struct {
	name string
	data []byte
}

func ioInputs() []ioInput {
	return []ioInput{{"opus", ioOpus(false)}, {"opus+cover", ioOpus(true)}}
}

func benchIO(b *testing.B, run func(b *testing.B, r io.ReaderAt, size int64)) {
	for _, in := range ioInputs() {
		b.Run(in.name, func(b *testing.B) {
			br := bytes.NewReader(in.data)
			b.ReportAllocs()
			var c *countingReaderAt
			for b.Loop() {
				c = &countingReaderAt{r: br}
				run(b, c, int64(len(in.data)))
			}
			b.ReportMetric(float64(c.calls), "reads/op")
			b.ReportMetric(float64(c.bytes), "bytes/op")
		})
	}
}

// BenchmarkIOParse reports the reads and bytes requested by Parse.
func BenchmarkIOParse(b *testing.B) {
	benchIO(b, func(b *testing.B, r io.ReaderAt, size int64) {
		if _, err := (&parser{}).Parse(context.Background(), r, size, "x.opus"); err != nil {
			b.Fatal(err)
		}
	})
}

// BenchmarkIOExtractArtwork reports the reads and bytes requested by ExtractArtwork.
func BenchmarkIOExtractArtwork(b *testing.B) {
	benchIO(b, func(b *testing.B, r io.ReaderAt, size int64) {
		if _, err := (&parser{}).ExtractArtwork(context.Background(), r, size, "x.opus"); err != nil {
			b.Fatal(err)
		}
	})
}

// BenchmarkIODigest reports the reads and bytes requested by AudioDigest.
func BenchmarkIODigest(b *testing.B) {
	benchIO(b, func(b *testing.B, r io.ReaderAt, size int64) {
		if _, err := (digester{}).AudioDigest(context.Background(), r, size); err != nil {
			b.Fatal(err)
		}
	})
}
