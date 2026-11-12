package ogg

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"testing"

	"github.com/simonhull/audiometa/internal/picture"
	"github.com/simonhull/audiometa/internal/types"
)

// TestWriteOpusPictureNumbersConsistent checks the rule opusinfo enforces on a written
// METADATA_BLOCK_PICTURE: width, height, color depth and color count are all valid or all 0.
func TestWriteOpusPictureNumbersConsistent(t *testing.T) {
	t.Parallel()
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.Black)
	var onePixel bytes.Buffer
	if err := png.Encode(&onePixel, img); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		art  types.Artwork
		want [4]uint32
	}{
		{"1x1 PNG", types.Artwork{MIMEType: "image/png", Data: onePixel.Bytes(), Width: 1, Height: 1}, [4]uint32{1, 1, 24, 0}},
		{"unreadable image", types.Artwork{MIMEType: "image/jpeg", Data: bytes.Repeat([]byte{7}, 300), Width: 1, Height: 1}, [4]uint32{}},
	}
	src := readCorpus(t, "sample.opus")
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tc.art.Type = types.ArtworkFrontCover
			out := writeBytes(t, src, editsFor(t, src, nil, types.ArtworkOp{Kind: types.ArtworkOpAdd, Art: tc.art}))
			raw, err := base64.StdEncoding.DecodeString(parseOggBytes(t, out).Tags.GetFirst("METADATA_BLOCK_PICTURE"))
			if err != nil {
				t.Fatal(err)
			}
			got, err := picture.Decode(raw)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got.Data, tc.art.Data) {
				t.Fatalf("decoded picture data differs: %d bytes", len(got.Data))
			}
			off := 4 // picture type
			for range 2 {
				off += 4 + int(binary.BigEndian.Uint32(raw[off:])) // MIME type, then description
			}
			var n [4]uint32
			for i := range n {
				n[i] = binary.BigEndian.Uint32(raw[off+4*i:])
			}
			if n != tc.want {
				t.Errorf("width, height, depth, colors = %v, want %v", n, tc.want)
			}
		})
	}
}
