package picture

import (
	"encoding/binary"
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/simonhull/audiometa/internal/types"
)

// block builds a valid PICTURE block by hand.
func block(pictureType uint32, mime, desc string, width, height uint32, data []byte) []byte {
	var b []byte
	b = binary.BigEndian.AppendUint32(b, pictureType)
	b = binary.BigEndian.AppendUint32(b, uint32(len(mime)))
	b = append(b, mime...)
	b = binary.BigEndian.AppendUint32(b, uint32(len(desc)))
	b = append(b, desc...)
	b = binary.BigEndian.AppendUint32(b, width)
	b = binary.BigEndian.AppendUint32(b, height)
	b = binary.BigEndian.AppendUint32(b, 24) // color depth
	b = binary.BigEndian.AppendUint32(b, 0)  // colors used
	b = binary.BigEndian.AppendUint32(b, uint32(len(data)))
	return append(b, data...)
}

func TestDecode(t *testing.T) {
	t.Parallel()
	b := block(3, "image/png", "front", 600, 400, []byte{1, 2, 3})
	got, err := Decode(append(b, 9, 9)) // bytes after the data are ignored
	if err != nil {
		t.Fatal(err)
	}
	if got.Type != types.ArtworkFrontCover || got.MIMEType != "image/png" || got.Description != "front" ||
		got.Width != 600 || got.Height != 400 || string(got.Data) != "\x01\x02\x03" {
		t.Errorf("Decode = %+v", got)
	}
}

func TestDecodeTruncated(t *testing.T) {
	t.Parallel()
	b := block(3, "image/png", "front", 600, 400, []byte{1, 2, 3})
	for cut := range b {
		if _, err := Decode(b[:cut]); !errors.Is(err, ErrMalformed) {
			t.Errorf("cut at %d: err = %v, want ErrMalformed", cut, err)
		}
	}
}

// TestDecodeHugeLengths uses lengths near 4 GiB, which wrap negative when converted to int
// on 32-bit platforms.
func TestDecodeHugeLengths(t *testing.T) {
	t.Parallel()
	const huge = 0xFFFFFFF0
	mimeHuge := binary.BigEndian.AppendUint32(binary.BigEndian.AppendUint32(nil, 3), huge)
	mimeHuge = append(mimeHuge, make([]byte, 40)...)

	descHuge := binary.BigEndian.AppendUint32(binary.BigEndian.AppendUint32(nil, 3), 0)
	descHuge = binary.BigEndian.AppendUint32(descHuge, huge)
	descHuge = append(descHuge, make([]byte, 40)...)

	dataHuge := block(3, "", "", 1, 1, nil)
	binary.BigEndian.PutUint32(dataHuge[len(dataHuge)-4:], huge)
	dataHuge = append(dataHuge, make([]byte, 40)...)

	for name, b := range map[string][]byte{"mime": mimeHuge, "description": descHuge, "data": dataHuge} {
		if _, err := Decode(b); !errors.Is(err, ErrMalformed) {
			t.Errorf("%s: err = %v, want ErrMalformed", name, err)
		}
	}
}

func TestDecodeClampsDimensions(t *testing.T) {
	t.Parallel()
	got, err := Decode(block(3, "image/png", "", math.MaxUint32, math.MaxUint32, nil))
	if err != nil {
		t.Fatal(err)
	}
	if got.Width != math.MaxInt32 || got.Height != math.MaxInt32 {
		t.Errorf("dimensions = %d x %d, want %d", got.Width, got.Height, math.MaxInt32)
	}
}

func TestEncodeRoundTrip(t *testing.T) {
	t.Parallel()
	for _, a := range []types.Artwork{
		{Type: types.ArtworkFrontCover, MIMEType: "image/png", Data: rgbPNG(), Width: 10, Height: 20},
		{Type: types.ArtworkPublisherLogotype, MIMEType: "image/png", Description: "logo é", Data: []byte("png")},
		{Type: types.ArtworkOther, Data: nil},
	} {
		enc, err := Encode(a)
		if err != nil {
			t.Fatalf("Encode(%+v): %v", a, err)
		}
		if n := EncodedLen(a); n != len(enc) {
			t.Errorf("EncodedLen = %d, len(Encode) = %d", n, len(enc))
		}
		got, err := Decode(enc)
		if err != nil {
			t.Fatalf("Decode: %v", err)
		}
		if got.Type != a.Type || got.MIMEType != a.MIMEType || got.Description != a.Description ||
			got.Width != a.Width || got.Height != a.Height || string(got.Data) != string(a.Data) {
			t.Errorf("round trip = %+v, want %+v", got, a)
		}
	}
}

func TestEncodeRefuses(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		art  types.Artwork
		want string
	}{
		{"MIME not printable", types.Artwork{Type: types.ArtworkFrontCover, MIMEType: "image/\x00png"}, "is not printable ASCII"},
		{"type below range", types.Artwork{Type: types.ArtworkOther - 1, MIMEType: "image/png"}, "picture type"},
		{"type above range", types.Artwork{Type: types.ArtworkPublisherLogotype + 1, MIMEType: "image/png"}, "picture type"},
		{"description not UTF-8", types.Artwork{Type: types.ArtworkFrontCover, MIMEType: "image/png", Description: "\xff"}, "not valid UTF-8"},
	}
	for _, tt := range tests {
		_, err := Encode(tt.art)
		if !errors.Is(err, types.ErrUnsupportedWrite) || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: err = %v, want ErrUnsupportedWrite containing %q", tt.name, err, tt.want)
		}
	}
}

func TestType(t *testing.T) {
	t.Parallel()
	if got := Type(block(6, "", "", 0, 0, nil)); got != 6 {
		t.Errorf("Type = %d, want 6", got)
	}
	if got := Type([]byte{0, 0, 3}); got != 0 {
		t.Errorf("Type of a short block = %d, want 0", got)
	}
}
