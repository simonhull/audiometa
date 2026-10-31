package mp3

import (
	"bytes"
	"context"
	"sync/atomic"
	"testing"
)

// byteCountingReaderAt tallies the bytes requested through ReadAt.
type byteCountingReaderAt struct {
	r     *bytes.Reader
	bytes atomic.Int64
}

func (c *byteCountingReaderAt) ReadAt(p []byte, off int64) (int, error) {
	c.bytes.Add(int64(len(p)))
	return c.r.ReadAt(p, off)
}

func TestParse_SkipsArtworkPayload(t *testing.T) {
	const imageSize = 1 << 20
	apic := append([]byte{0x00}, "image/jpeg\x00"...)
	apic = append(apic, 0x03, 0x00)
	apic = append(apic, bytes.Repeat([]byte{0xFF}, imageSize)...)

	for _, v := range []byte{3, 4} {
		data := append(id3Tag(v,
			id3Frame(v, "TIT2", latin1Text("Title")),
			id3Frame(v, "APIC", apic),
			id3Frame(v, "TALB", latin1Text("Album")),
		), mpegFrame...)

		cr := &byteCountingReaderAt{r: bytes.NewReader(data)}
		file, err := (&parser{}).Parse(context.Background(), cr, int64(len(data)), "t.mp3")
		if err != nil {
			t.Fatal(err)
		}
		if file.Tags.Title != "Title" || file.Tags.Album != "Album" {
			t.Errorf("v%d: title=%q album=%q; frames after the APIC must still parse", v, file.Tags.Title, file.Tags.Album)
		}
		if got := cr.bytes.Load(); got > imageSize/4 {
			t.Errorf("v%d: Parse read %d bytes; the APIC payload should be skipped", v, got)
		}

		art, err := (&parser{}).ExtractArtwork(context.Background(), bytes.NewReader(data), int64(len(data)), "t.mp3")
		if err != nil {
			t.Fatal(err)
		}
		if len(art) != 1 || len(art[0].Data) != imageSize {
			t.Errorf("v%d: ExtractArtwork returned %d images, want 1 of %d bytes", v, len(art), imageSize)
		}
	}
}
