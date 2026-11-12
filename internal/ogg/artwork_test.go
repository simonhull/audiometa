package ogg

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"testing"

	"github.com/simonhull/audiometa/internal/types"
)

func TestExtractArtworkSkipsTail(t *testing.T) {
	data := ioOpus(true)
	size := int64(len(data))
	if size < 128<<10 {
		t.Fatalf("fixture is %d bytes, want over 128 KiB", size)
	}
	c := &countingReaderAt{r: bytes.NewReader(data)}
	got, err := (&parser{}).ExtractArtwork(context.Background(), c, size, "x.opus")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !bytes.Equal(got[0].Data, ioCover()) || got[0].MIMEType != "image/jpeg" || got[0].Type != types.ArtworkFrontCover {
		t.Fatalf("artwork = %+v, want the embedded front cover", got)
	}
	for _, off := range c.offs {
		if off >= size-65536 {
			t.Errorf("read at offset %d: the file's tail was read (size %d)", off, size)
		}
	}
}

// TestExtractArtworkHugePictureLength gives the picture block a MIME length close to 4 GiB,
// which wraps negative when converted to int on 32-bit platforms.
func TestExtractArtworkHugePictureLength(t *testing.T) {
	block := createTestPictureBlock(3, "image/jpeg", "cover", 1, 1, []byte{1, 2, 3})
	binary.BigEndian.PutUint32(block[4:], 0xFFFFFFF0)
	value := "METADATA_BLOCK_PICTURE=" + base64.StdEncoding.EncodeToString(block)
	tail := pageSpec{headerType: 0x04, granule: 44100, serial: 7, lacing: lacingFor(100), data: make([]byte, 100)}
	data := multiPageFile(vorbisIDPacket(), commentPacket("\x03vorbis", true, value), 7, tail)
	art, err := (&parser{}).ExtractArtwork(context.Background(), bytes.NewReader(data), int64(len(data)), "x.ogg")
	if err != nil || len(art) != 0 {
		t.Errorf("ExtractArtwork = %d images, err %v; want no error and no images", len(art), err)
	}
}

func TestParseMetadataBlockPicture(t *testing.T) {
	// Create a valid METADATA_BLOCK_PICTURE
	pic := createTestPictureBlock(3, "image/jpeg", "Front Cover", 100, 100, []byte{0xFF, 0xD8, 0xFF, 0xE0})
	base64Pic := base64.StdEncoding.EncodeToString(pic)

	artwork, err := parseMetadataBlockPicture(base64Pic)
	if err != nil {
		t.Fatalf("parseMetadataBlockPicture() error = %v", err)
	}

	if artwork.MIMEType != "image/jpeg" {
		t.Errorf("MIMEType = %q, want %q", artwork.MIMEType, "image/jpeg")
	}
	if artwork.Description != "Front Cover" {
		t.Errorf("Description = %q, want %q", artwork.Description, "Front Cover")
	}
	if artwork.Width != 100 {
		t.Errorf("Width = %d, want %d", artwork.Width, 100)
	}
	if artwork.Height != 100 {
		t.Errorf("Height = %d, want %d", artwork.Height, 100)
	}
	if artwork.Type != types.ArtworkFrontCover {
		t.Errorf("Type = %v, want ArtworkFrontCover", artwork.Type)
	}
	if len(artwork.Data) != 4 {
		t.Errorf("Data length = %d, want 4", len(artwork.Data))
	}
}

func TestParseMetadataBlockPicture_BackCover(t *testing.T) {
	pic := createTestPictureBlock(4, "image/png", "Back Cover", 200, 200, []byte{0x89, 'P', 'N', 'G'})
	base64Pic := base64.StdEncoding.EncodeToString(pic)

	artwork, err := parseMetadataBlockPicture(base64Pic)
	if err != nil {
		t.Fatalf("parseMetadataBlockPicture() error = %v", err)
	}

	if artwork.Type != types.ArtworkBackCover {
		t.Errorf("Type = %v, want ArtworkBackCover", artwork.Type)
	}
	if artwork.MIMEType != "image/png" {
		t.Errorf("MIMEType = %q, want %q", artwork.MIMEType, "image/png")
	}
}

func TestParseMetadataBlockPicture_OtherType(t *testing.T) {
	// Type 0 = Other
	pic := createTestPictureBlock(0, "image/gif", "", 50, 50, []byte{0x47, 0x49, 0x46})
	base64Pic := base64.StdEncoding.EncodeToString(pic)

	artwork, err := parseMetadataBlockPicture(base64Pic)
	if err != nil {
		t.Fatalf("parseMetadataBlockPicture() error = %v", err)
	}

	if artwork.Type != types.ArtworkOther {
		t.Errorf("Type = %v, want ArtworkOther", artwork.Type)
	}
}

func TestParseMetadataBlockPicture_InvalidBase64(t *testing.T) {
	_, err := parseMetadataBlockPicture("not valid base64!!!")
	if err == nil {
		t.Error("parseMetadataBlockPicture() should return error for invalid base64")
	}
}

func TestParseMetadataBlockPicture_TooSmall(t *testing.T) {
	// Less than 32 bytes
	data := make([]byte, 20)
	base64Data := base64.StdEncoding.EncodeToString(data)

	_, err := parseMetadataBlockPicture(base64Data)
	if err == nil {
		t.Error("parseMetadataBlockPicture() should return error for data too small")
	}
}

func TestParseMetadataBlockPicture_InvalidMimeLength(t *testing.T) {
	// Create data with MIME length that exceeds data
	data := make([]byte, 40)
	binary.BigEndian.PutUint32(data[0:], 3)    // picture type
	binary.BigEndian.PutUint32(data[4:], 1000) // MIME length (too large)
	base64Data := base64.StdEncoding.EncodeToString(data)

	_, err := parseMetadataBlockPicture(base64Data)
	if err == nil {
		t.Error("parseMetadataBlockPicture() should return error for invalid MIME length")
	}
}

func TestParseMetadataBlockPicture_EmptyDescription(t *testing.T) {
	pic := createTestPictureBlock(3, "image/jpeg", "", 100, 100, []byte{0xFF, 0xD8})
	base64Pic := base64.StdEncoding.EncodeToString(pic)

	artwork, err := parseMetadataBlockPicture(base64Pic)
	if err != nil {
		t.Fatalf("parseMetadataBlockPicture() error = %v", err)
	}

	if artwork.Description != "" {
		t.Errorf("Description = %q, want empty", artwork.Description)
	}
}

// createTestPictureBlock creates a valid FLAC picture block.
func createTestPictureBlock(pictureType uint32, mimeType, description string, width, height uint32, imageData []byte) []byte {
	// Calculate total size
	size := 4 + 4 + len(mimeType) + 4 + len(description) + 4 + 4 + 4 + 4 + 4 + len(imageData)
	data := make([]byte, size)

	offset := 0

	// Picture type
	binary.BigEndian.PutUint32(data[offset:], pictureType)
	offset += 4

	// MIME type length + data
	binary.BigEndian.PutUint32(data[offset:], uint32(len(mimeType)))
	offset += 4
	copy(data[offset:], mimeType)
	offset += len(mimeType)

	// Description length + data
	binary.BigEndian.PutUint32(data[offset:], uint32(len(description)))
	offset += 4
	copy(data[offset:], description)
	offset += len(description)

	// Width
	binary.BigEndian.PutUint32(data[offset:], width)
	offset += 4

	// Height
	binary.BigEndian.PutUint32(data[offset:], height)
	offset += 4

	// Color depth (24-bit)
	binary.BigEndian.PutUint32(data[offset:], 24)
	offset += 4

	// Indexed colors (0 for non-indexed)
	binary.BigEndian.PutUint32(data[offset:], 0)
	offset += 4

	// Image data length + data
	binary.BigEndian.PutUint32(data[offset:], uint32(len(imageData)))
	offset += 4
	copy(data[offset:], imageData)

	return data
}

func TestParseMetadataBlockPicture_ExtendedTypes(t *testing.T) {
	tests := []struct {
		code uint32
		want types.ArtworkType
	}{
		{6, types.ArtworkMedia},
		{20, types.ArtworkPublisherLogotype},
		{21, types.ArtworkOther},
		{0xFFFFFFFF, types.ArtworkOther},
	}
	for _, tt := range tests {
		pic := createTestPictureBlock(tt.code, "image/png", "", 1, 1, []byte{1, 2, 3})
		artwork, err := parseMetadataBlockPicture(base64.StdEncoding.EncodeToString(pic))
		if err != nil {
			t.Fatalf("type %d: %v", tt.code, err)
		}
		if artwork.Type != tt.want {
			t.Errorf("type %d = %v, want %v", tt.code, artwork.Type, tt.want)
		}
	}
}
