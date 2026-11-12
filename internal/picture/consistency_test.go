package picture

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"testing"

	"github.com/simonhull/audiometa/internal/imagemeta"
	"github.com/simonhull/audiometa/internal/types"
)

// rgbPNG returns a 1x1 opaque 24-bit PNG.
func rgbPNG() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.Black)
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic(err) // encoding a 1x1 image to memory cannot fail
	}
	return buf.Bytes()
}

// blockFields returns a block's width, height, color depth and color count.
func blockFields(t *testing.T, block []byte) [4]uint32 {
	t.Helper()
	off := 4 // picture type
	for range 2 {
		off += 4 + int(binary.BigEndian.Uint32(block[off:])) // MIME type, then description
	}
	var f [4]uint32
	for i := range f {
		f[i] = binary.BigEndian.Uint32(block[off+4*i:])
	}
	return f
}

// TestEncodeNumericFieldsConsistent checks the rule opusinfo and the FLAC format enforce: the
// width, height, color depth and color count are either all valid or all zero.
func TestEncodeNumericFieldsConsistent(t *testing.T) {
	t.Parallel()
	bounds := image.Rect(0, 0, 3, 2)
	encode := func(enc func(*bytes.Buffer) error) []byte {
		t.Helper()
		var buf bytes.Buffer
		if err := enc(&buf); err != nil {
			t.Fatal(err)
		}
		return buf.Bytes()
	}
	rgba := image.NewNRGBA(bounds)
	rgba.Set(0, 0, color.NRGBA{R: 1, A: 10})
	pal := image.NewPaletted(bounds, color.Palette{color.Black, color.White, color.RGBA{R: 255, A: 255}})
	pngOf := func(img image.Image) []byte {
		return encode(func(b *bytes.Buffer) error { return png.Encode(b, img) })
	}
	indexed := pngOf(pal)
	opaque := image.NewRGBA(bounds)
	draw.Draw(opaque, bounds, image.NewUniform(color.RGBA{R: 1, G: 2, B: 3, A: 255}), image.Point{}, draw.Src)

	tests := []struct {
		name string
		mime string
		data []byte
		// want is width, height, depth and colors; all zero when the depth cannot be read.
		want [4]uint32
	}{
		{"png RGBA", imagemeta.MIMEPNG, pngOf(rgba), [4]uint32{3, 2, 32, 0}},
		{"png RGB", imagemeta.MIMEPNG, pngOf(opaque), [4]uint32{3, 2, 24, 0}},
		{"png gray", imagemeta.MIMEPNG, pngOf(image.NewGray(bounds)), [4]uint32{3, 2, 8, 0}},
		{"png indexed", imagemeta.MIMEPNG, indexed, [4]uint32{3, 2, 2, 3}},
		{"png indexed without PLTE", imagemeta.MIMEPNG, bytes.Replace(indexed, []byte("PLTE"), []byte("plTE"), 1), [4]uint32{}},
		{"jpeg", imagemeta.MIMEJPEG, encode(func(b *bytes.Buffer) error { return jpeg.Encode(b, image.NewRGBA(bounds), nil) }), [4]uint32{3, 2, 24, 0}},
		{"unknown bytes", "image/png", []byte("not an image"), [4]uint32{}},
		{"no MIME type", "", []byte("not an image"), [4]uint32{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// The artwork carries the true sizes; the depth is read from the image data.
			art := types.Artwork{Type: types.ArtworkFrontCover, MIMEType: tc.mime, Data: tc.data, Width: 3, Height: 2}
			enc, err := Encode(art)
			if err != nil {
				t.Fatal(err)
			}
			got := blockFields(t, enc)
			if got != tc.want {
				t.Errorf("width, height, depth, colors = %v, want %v", got, tc.want)
			}
			zeros := 0
			for _, f := range got {
				if f == 0 {
					zeros++
				}
			}
			if got[3] == 0 && got[2] != 0 {
				zeros-- // colors may be 0 for an image that is not indexed
			}
			if zeros != 0 && zeros != len(got) {
				t.Errorf("fields %v are neither all valid nor all zero", got)
			}
		})
	}
}

// TestEncodeDimensionsWithoutDepth checks that dimensions alone are never written: without a
// readable depth the block carries zeros.
func TestEncodeDimensionsWithoutDepth(t *testing.T) {
	t.Parallel()
	enc, err := Encode(types.Artwork{Type: types.ArtworkFrontCover, MIMEType: "image/gif", Data: []byte("GIF89a"), Width: 5, Height: 5})
	if err != nil {
		t.Fatal(err)
	}
	if got := blockFields(t, enc); got != [4]uint32{} {
		t.Errorf("fields = %v, want all zero", got)
	}
}
