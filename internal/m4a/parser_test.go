package m4a

import (
	"bytes"
	"encoding/binary"
	"os"
	"testing"

	"github.com/simonhull/audiometa"
)

// createMinimalM4B creates a minimal M4B file with ftyp, moov, udta, meta, and ilst atoms
func createMinimalM4B(title, artist, album string) []byte {
	buf := &bytes.Buffer{}

	// 1. ftyp atom
	ftypBuf := &bytes.Buffer{}
	ftypBuf.WriteString("M4B ")                        // major brand
	binary.Write(ftypBuf, binary.BigEndian, uint32(0)) // minor version
	ftypBuf.WriteString("M4B ")                        // compatible brand
	ftypAtom := createMockAtom("ftyp", ftypBuf.Bytes())
	buf.Write(ftypAtom)

	// 2. Build metadata atoms from inside out: ilst → meta → udta → moov

	// Create ilst with metadata items
	var ilstData []byte
	if title != "" {
		ilstData = append(ilstData, createMetadataItem([]byte{0xA9, 'n', 'a', 'm'}, title)...)
	}
	if artist != "" {
		ilstData = append(ilstData, createMetadataItem([]byte{0xA9, 'A', 'R', 'T'}, artist)...)
	}
	if album != "" {
		ilstData = append(ilstData, createMetadataItem([]byte{0xA9, 'a', 'l', 'b'}, album)...)
	}
	ilstAtom := createMockAtom("ilst", ilstData)

	// meta atom contains ilst
	// meta atom has 4 bytes of version+flags before the data
	metaData := make([]byte, 4)
	binary.BigEndian.PutUint32(metaData, 0) // version=0, flags=0
	metaData = append(metaData, ilstAtom...)
	metaAtom := createMockAtom("meta", metaData)

	// udta contains meta
	udtaAtom := createMockAtom("udta", metaAtom)

	// moov contains udta
	moovAtom := createMockAtom("moov", udtaAtom)

	buf.Write(moovAtom)

	return buf.Bytes()
}

func TestParse_Success(t *testing.T) {
	data := createMinimalM4B("My Audiobook", "Author Name", "Series 1")

	// Write to temp file
	tmpFile, err := os.CreateTemp("", "test*.m4b")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.Write(data); err != nil {
		t.Fatal(err)
	}
	tmpFile.Close()

	// Parse the file
	file, err := audiometa.Open(tmpFile.Name())
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	defer file.Close()

	if file == nil {
		t.Fatal("expected file, got nil")
	}

	if file.Tags.Title != "My Audiobook" {
		t.Errorf("expected title 'My Audiobook', got '%s'", file.Tags.Title)
	}

	if file.Tags.Artist != "Author Name" {
		t.Errorf("expected artist 'Author Name', got '%s'", file.Tags.Artist)
	}

	if file.Tags.Album != "Series 1" {
		t.Errorf("expected album 'Series 1', got '%s'", file.Tags.Album)
	}

	if file.Format != audiometa.FormatM4B {
		t.Errorf("expected format M4B, got %v", file.Format)
	}

	if file.Size == 0 {
		t.Error("expected file size to be set")
	}
}

func TestParse_FileNotFound(t *testing.T) {
	_, err := audiometa.Open("/nonexistent/file.m4b")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestParse_UnsupportedFormat(t *testing.T) {
	// Create a file with wrong ftyp
	data := createMockAtom("ftyp", []byte("XXXX")) // Wrong brand

	tmpFile, err := os.CreateTemp("", "test*.m4b")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())

	tmpFile.Write(data)
	tmpFile.Close()

	_, err = audiometa.Open(tmpFile.Name())
	if err == nil {
		t.Error("expected error for unsupported format")
	}

	if _, ok := err.(*audiometa.UnsupportedFormatError); !ok {
		t.Errorf("expected UnsupportedFormatError, got %T", err)
	}
}

func TestParse_NoMetadata(t *testing.T) {
	// Create M4B with no metadata
	data := createMinimalM4B("", "", "")

	tmpFile, err := os.CreateTemp("", "test*.m4b")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())

	tmpFile.Write(data)
	tmpFile.Close()

	file, err := audiometa.Open(tmpFile.Name())
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	defer file.Close()

	// Should succeed but have empty metadata
	if file.Tags.Title != "" || file.Tags.Artist != "" || file.Tags.Album != "" {
		t.Error("expected empty metadata")
	}
}
