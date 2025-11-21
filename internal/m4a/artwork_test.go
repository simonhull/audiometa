package m4a

import (
	"bytes"
	"encoding/binary"
	"os"
	"testing"

	"github.com/simonhull/audiometa/internal/types"
)

func TestExtractArtwork_WithJPEGCover(t *testing.T) {
	// Create test JPEG data (minimal valid JPEG)
	jpegData := []byte{
		0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x4A, 0x46, // JPEG header
		0x49, 0x46, 0x00, 0x01, 0x01, 0x00, 0x00, 0x01,
		0x00, 0x01, 0x00, 0x00, 0xFF, 0xD9, // JPEG EOI
	}

	// Build M4B with covr atom containing JPEG
	fileData := createM4BWithCover(jpegData, 0x0D) // 0x0D = JPEG flag

	// Write to temp file
	tmpFile, err := os.CreateTemp("", "test-artwork-*.m4b")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.Write(fileData); err != nil {
		t.Fatal(err)
	}
	tmpFile.Close()

	// Open for reading
	f, err := os.Open(tmpFile.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}

	// Test ExtractArtwork
	p := &parser{}
	artwork, err := p.ExtractArtwork(f, stat.Size(), tmpFile.Name())
	if err != nil {
		t.Fatalf("ExtractArtwork failed: %v", err)
	}

	// Verify results
	if len(artwork) == 0 {
		t.Fatal("expected artwork, got none")
	}

	if len(artwork) != 1 {
		t.Errorf("expected 1 artwork, got %d", len(artwork))
	}

	art := artwork[0]

	if art.MIMEType != "image/jpeg" {
		t.Errorf("expected MIME type image/jpeg, got %s", art.MIMEType)
	}

	if art.Type != types.ArtworkFrontCover {
		t.Errorf("expected type FrontCover, got %v", art.Type)
	}

	if !bytes.Equal(art.Data, jpegData) {
		t.Error("artwork data does not match original JPEG data")
	}

	if len(art.Data) != len(jpegData) {
		t.Errorf("expected %d bytes, got %d", len(jpegData), len(art.Data))
	}
}

// Helper: creates M4B file with embedded cover art.
func createM4BWithCover(imageData []byte, flags byte) []byte {
	buf := &bytes.Buffer{}

	// ftyp atom
	ftypData := &bytes.Buffer{}
	ftypData.WriteString("M4B ")
	binary.Write(ftypData, binary.BigEndian, uint32(0))
	ftypData.WriteString("M4B ")
	ftypAtom := createMockAtom("ftyp", ftypData.Bytes())
	buf.Write(ftypAtom)

	// Build covr atom: data atom inside covr
	dataAtomData := &bytes.Buffer{}
	dataAtomData.WriteByte(0)                               // version
	dataAtomData.WriteByte(0)                               // flags byte 1
	dataAtomData.WriteByte(0)                               // flags byte 2
	dataAtomData.WriteByte(flags)                           // flags byte 3 (MIME type)
	binary.Write(dataAtomData, binary.BigEndian, uint32(0)) // reserved
	dataAtomData.Write(imageData)                           // image data
	dataAtom := createMockAtom("data", dataAtomData.Bytes())

	// covr contains data atom
	covrAtom := createMockAtom("covr", dataAtom)

	// ilst contains covr
	ilstAtom := createMockAtom("ilst", covrAtom)

	// meta contains ilst (with 4-byte version/flags header)
	metaData := make([]byte, 4)
	binary.BigEndian.PutUint32(metaData, 0)
	metaData = append(metaData, ilstAtom...)
	metaAtom := createMockAtom("meta", metaData)

	// udta contains meta
	udtaAtom := createMockAtom("udta", metaAtom)

	// moov contains udta
	moovAtom := createMockAtom("moov", udtaAtom)

	buf.Write(moovAtom)

	return buf.Bytes()
}

func TestExtractArtwork_WithPNGCover(t *testing.T) {
	// Create minimal valid PNG
	pngData := []byte{
		0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, // PNG signature
		0x00, 0x00, 0x00, 0x0D, 0x49, 0x48, 0x44, 0x52, // IHDR chunk
		0x00, 0x00, 0x00, 0x01, // width = 1
		0x00, 0x00, 0x00, 0x01, // height = 1
		0x08, 0x02, 0x00, 0x00, 0x00, 0x90, 0x77, 0x53, 0xDE, // rest of IHDR
		0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4E, 0x44, // IEND
		0xAE, 0x42, 0x60, 0x82,
	}

	fileData := createM4BWithCover(pngData, 0x0E) // 0x0E = PNG flag

	tmpFile, err := os.CreateTemp("", "test-png-*.m4b")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.Write(fileData); err != nil {
		t.Fatal(err)
	}
	tmpFile.Close()

	f, err := os.Open(tmpFile.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}

	p := &parser{}
	artwork, err := p.ExtractArtwork(f, stat.Size(), tmpFile.Name())
	if err != nil {
		t.Fatalf("ExtractArtwork failed: %v", err)
	}

	if len(artwork) != 1 {
		t.Fatalf("expected 1 artwork, got %d", len(artwork))
	}

	art := artwork[0]

	if art.MIMEType != "image/png" {
		t.Errorf("expected MIME type image/png, got %s", art.MIMEType)
	}

	if !bytes.Equal(art.Data, pngData) {
		t.Error("artwork data does not match original PNG data")
	}

	// Verify dimensions were detected
	if art.Width != 1 || art.Height != 1 {
		t.Errorf("expected dimensions 1x1, got %dx%d", art.Width, art.Height)
	}
}

func TestExtractArtwork_MultipleImages(t *testing.T) {
	// Some files have front + back cover
	jpegData1 := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x4A, 0x46, 0x49, 0x46, 0x00, 0x01, 0x01, 0x00, 0x00, 0x01, 0x00, 0x01, 0x00, 0x00, 0xFF, 0xD9}
	jpegData2 := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x4A, 0x46, 0x49, 0x46, 0x00, 0x01, 0x01, 0x00, 0x00, 0x02, 0x00, 0x02, 0x00, 0x00, 0xFF, 0xD9}

	fileData := createM4BWithMultipleCovers([][]byte{jpegData1, jpegData2})

	tmpFile, err := os.CreateTemp("", "test-multi-*.m4b")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())

	tmpFile.Write(fileData)
	tmpFile.Close()

	f, err := os.Open(tmpFile.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}

	p := &parser{}
	artwork, err := p.ExtractArtwork(f, stat.Size(), tmpFile.Name())
	if err != nil {
		t.Fatalf("ExtractArtwork failed: %v", err)
	}

	if len(artwork) != 2 {
		t.Fatalf("expected 2 artworks, got %d", len(artwork))
	}

	if !bytes.Equal(artwork[0].Data, jpegData1) {
		t.Error("first artwork data mismatch")
	}

	if !bytes.Equal(artwork[1].Data, jpegData2) {
		t.Error("second artwork data mismatch")
	}
}

// Helper: creates M4B with multiple cover images.
func createM4BWithMultipleCovers(images [][]byte) []byte {
	buf := &bytes.Buffer{}

	// ftyp
	ftypData := &bytes.Buffer{}
	ftypData.WriteString("M4B ")
	binary.Write(ftypData, binary.BigEndian, uint32(0))
	ftypData.WriteString("M4B ")
	ftypAtom := createMockAtom("ftyp", ftypData.Bytes())
	buf.Write(ftypAtom)

	// Build multiple data atoms
	var dataAtoms []byte
	for _, img := range images {
		dataAtomData := &bytes.Buffer{}
		dataAtomData.WriteByte(0)
		dataAtomData.WriteByte(0)
		dataAtomData.WriteByte(0)
		dataAtomData.WriteByte(0x0D) // JPEG
		binary.Write(dataAtomData, binary.BigEndian, uint32(0))
		dataAtomData.Write(img)
		dataAtoms = append(dataAtoms, createMockAtom("data", dataAtomData.Bytes())...)
	}

	covrAtom := createMockAtom("covr", dataAtoms)
	ilstAtom := createMockAtom("ilst", covrAtom)

	metaData := make([]byte, 4)
	binary.BigEndian.PutUint32(metaData, 0)
	metaData = append(metaData, ilstAtom...)
	metaAtom := createMockAtom("meta", metaData)

	udtaAtom := createMockAtom("udta", metaAtom)
	moovAtom := createMockAtom("moov", udtaAtom)

	buf.Write(moovAtom)
	return buf.Bytes()
}

func TestExtractArtwork_NoArtwork(t *testing.T) {
	// File with metadata but no covr atom
	fileData := createMinimalM4B("Test", "Artist", "Album")

	tmpFile, err := os.CreateTemp("", "test-no-art-*.m4b")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())

	tmpFile.Write(fileData)
	tmpFile.Close()

	f, err := os.Open(tmpFile.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}

	p := &parser{}
	artwork, err := p.ExtractArtwork(f, stat.Size(), tmpFile.Name())
	if err != nil {
		t.Fatalf("ExtractArtwork should not error: %v", err)
	}

	if len(artwork) != 0 {
		t.Errorf("expected no artwork, got %d", len(artwork))
	}
}

func TestExtractArtwork_NoMoovAtom(t *testing.T) {
	// Minimal file with just ftyp (corrupted/incomplete)
	fileData := createMockAtom("ftyp", []byte("M4B \x00\x00\x00\x00M4B "))

	tmpFile, err := os.CreateTemp("", "test-no-moov-*.m4b")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())

	tmpFile.Write(fileData)
	tmpFile.Close()

	f, err := os.Open(tmpFile.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}

	p := &parser{}
	artwork, err := p.ExtractArtwork(f, stat.Size(), tmpFile.Name())

	// Should not error - graceful degradation
	if err != nil {
		t.Errorf("should not error on missing moov: %v", err)
	}

	if len(artwork) != 0 {
		t.Error("expected no artwork from file without moov")
	}
}

func TestExtractArtwork_CorruptedImageData(t *testing.T) {
	// covr atom with invalid/truncated image data
	invalidData := []byte{0x00, 0x01, 0x02} // Not a valid image

	fileData := createM4BWithCover(invalidData, 0x0D)

	tmpFile, err := os.CreateTemp("", "test-corrupt-*.m4b")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())

	tmpFile.Write(fileData)
	tmpFile.Close()

	f, err := os.Open(tmpFile.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}

	p := &parser{}
	artwork, err := p.ExtractArtwork(f, stat.Size(), tmpFile.Name())

	// Should extract successfully (we don't validate image format)
	if err != nil {
		t.Fatalf("ExtractArtwork failed: %v", err)
	}

	if len(artwork) != 1 {
		t.Fatalf("expected 1 artwork, got %d", len(artwork))
	}

	// Data should match even if invalid
	if !bytes.Equal(artwork[0].Data, invalidData) {
		t.Error("data mismatch")
	}
}
