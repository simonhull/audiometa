package imagemeta

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"testing"
)

// encoded returns the bytes enc writes, failing the test on error.
func encoded(t *testing.T, enc func(io.Writer) error) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := enc(&buf); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// TestColorDepth checks the bits per pixel and palette size read from real encoder output.
func TestColorDepth(t *testing.T) {
	t.Parallel()
	bounds := image.Rect(0, 0, 2, 2)
	pal := image.NewPaletted(bounds, color.Palette{color.Black, color.White, color.RGBA{R: 255, A: 255}})
	nrgba := image.NewNRGBA(bounds)
	nrgba.Set(0, 0, color.NRGBA{R: 1, A: 10}) // keeps the encoder from dropping the alpha channel
	opaque := image.NewRGBA(bounds)
	draw.Draw(opaque, bounds, image.NewUniform(color.RGBA{R: 1, G: 2, B: 3, A: 255}), image.Point{}, draw.Src)

	indexed := encoded(t, func(w io.Writer) error { return png.Encode(w, pal) })
	noPLTE := bytes.Replace(indexed, []byte("PLTE"), []byte("plTE"), 1)

	tests := []struct {
		name       string
		data       []byte
		mime       string
		wantDepth  int
		wantColors int
	}{
		{"png RGBA", encoded(t, func(w io.Writer) error { return png.Encode(w, nrgba) }), MIMEPNG, 32, 0},
		{"png RGB", encoded(t, func(w io.Writer) error { return png.Encode(w, opaque) }), MIMEPNG, 24, 0},
		{"png gray", encoded(t, func(w io.Writer) error { return png.Encode(w, image.NewGray(bounds)) }), MIMEPNG, 8, 0},
		{"png gray 16-bit", encoded(t, func(w io.Writer) error { return png.Encode(w, image.NewGray16(bounds)) }), MIMEPNG, 16, 0},
		{"png RGBA 16-bit", encoded(t, func(w io.Writer) error { return png.Encode(w, image.NewRGBA64(bounds)) }), MIMEPNG, 64, 0},
		{"png indexed", indexed, MIMEPNG, 2, 3},
		{"png indexed without PLTE", noPLTE, MIMEPNG, 0, 0},
		{"png truncated", indexed[:25], MIMEPNG, 0, 0},
		{"png bad signature", append([]byte{0}, indexed[1:]...), MIMEPNG, 0, 0},
		{"jpeg color", encoded(t, func(w io.Writer) error { return jpeg.Encode(w, opaque, nil) }), MIMEJPEG, 24, 0},
		{"jpeg gray", encoded(t, func(w io.Writer) error { return jpeg.Encode(w, image.NewGray(bounds), nil) }), MIMEJPEG, 8, 0},
		{"jpeg without SOF", []byte{0xFF, 0xD8, 0xFF, 0xE0, 0, 0, 0, 0, 0, 0, 0, 0}, MIMEJPEG, 0, 0},
		{"gif", encoded(t, func(w io.Writer) error { return gif.Encode(w, pal, nil) }), MIMEGIF, 2, 4},
		{"gif without global color table", []byte("GIF89a\x01\x00\x01\x00\x00\x00\x00"), MIMEGIF, 0, 0},
		{"unsupported type", []byte("RIFF\x00\x00\x00\x00WEBP"), "image/webp", 0, 0},
		{"empty", nil, MIMEPNG, 0, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			depth, colors := ColorDepth(tc.data, tc.mime)
			if depth != tc.wantDepth || colors != tc.wantColors {
				t.Errorf("ColorDepth = %d bits, %d colors; want %d, %d", depth, colors, tc.wantDepth, tc.wantColors)
			}
		})
	}
}
