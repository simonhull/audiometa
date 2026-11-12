package flac

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"testing"

	"github.com/simonhull/audiometa/internal/picture"
	"github.com/simonhull/audiometa/internal/types"
)

// onePixelPNG returns a 1x1 opaque 24-bit PNG.
func onePixelPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.Black)
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// pictureNumbers returns the width, height, color depth and color count of a PICTURE payload.
func pictureNumbers(payload []byte) [4]uint32 {
	off := 4 // picture type
	for range 2 {
		off += 4 + int(binary.BigEndian.Uint32(payload[off:])) // MIME type, then description
	}
	var n [4]uint32
	for i := range n {
		n[i] = binary.BigEndian.Uint32(payload[off+4*i:])
	}
	return n
}

// TestPicture_NumericFieldsConsistent checks that a written picture's width, height, color
// depth and color count are all valid, as the FLAC format requires.
func TestPicture_NumericFieldsConsistent(t *testing.T) {
	t.Parallel()
	src := pictureFLAC()
	art := types.Artwork{MIMEType: "image/png", Data: onePixelPNG(t), Type: types.ArtworkFrontCover, Width: 1, Height: 1}
	out := writeBytes(t, src, editsFor(t, src, nil, types.ArtworkOp{Kind: types.ArtworkOpAdd, Art: art}))

	pics := blocksOfType(metaOf(t, out).blocks, blockTypePicture)
	payload := pics[len(pics)-1].payload // new pictures go last
	got, err := picture.Decode(payload)
	if err != nil {
		t.Fatal(err)
	}
	if got.Width != 1 || got.Height != 1 || !bytes.Equal(got.Data, art.Data) {
		t.Fatalf("decoded = %+v", got)
	}
	if n := pictureNumbers(payload); n != [4]uint32{1, 1, 24, 0} {
		t.Errorf("width, height, depth, colors = %v, want [1 1 24 0]", n)
	}
}
