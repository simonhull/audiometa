package audiometa_test

import (
	"bytes"
	"encoding/binary"
	"os"
	"testing"

	"github.com/simonhull/audiometa"
	_ "github.com/simonhull/audiometa/internal/flac" // Register FLAC parser
	_ "github.com/simonhull/audiometa/internal/m4a"  // Register M4A/M4B parser
	_ "github.com/simonhull/audiometa/internal/mp3"  // Register MP3 parser
	_ "github.com/simonhull/audiometa/internal/ogg"  // Register Ogg Vorbis parser
)

// createSimpleM4B creates a minimal M4B for testing
// This duplicates some logic from m4a/parser_test.go but keeps the public API tests independent
func createSimpleM4B() []byte {
	buf := &bytes.Buffer{}

	// ftyp atom
	ftypBuf := &bytes.Buffer{}
	ftypBuf.WriteString("M4B ")
	binary.Write(ftypBuf, binary.BigEndian, uint32(0))
	ftypBuf.WriteString("M4B ")

	// ftyp atom size
	ftypSize := uint32(8 + ftypBuf.Len())
	binary.Write(buf, binary.BigEndian, ftypSize)
	buf.WriteString("ftyp")
	buf.Write(ftypBuf.Bytes())

	// Simple moov atom (empty)
	binary.Write(buf, binary.BigEndian, uint32(8))
	buf.WriteString("moov")

	return buf.Bytes()
}

func TestParse_M4B(t *testing.T) {
	data := createSimpleM4B()

	tmpFile, err := os.CreateTemp("", "test*.m4b")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())

	tmpFile.Write(data)
	tmpFile.Close()

	// Use the new public API
	file, err := audiometa.Open(tmpFile.Name())
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer file.Close()

	if file.Format != audiometa.FormatM4B {
		t.Errorf("expected FormatM4B, got %v", file.Format)
	}
}

func TestParse_FileNotFound(t *testing.T) {
	_, err := audiometa.Open("/nonexistent/path.m4b")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestParse_UnsupportedFormat(t *testing.T) {
	// Create a file with unsupported format
	tmpFile, err := os.CreateTemp("", "test*.xyz")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())

	// Write some random data
	tmpFile.Write([]byte("not a valid audio file"))
	tmpFile.Close()

	_, err = audiometa.Open(tmpFile.Name())
	if err == nil {
		t.Error("expected error for unsupported format")
	}

	if _, ok := err.(*audiometa.UnsupportedFormatError); !ok {
		t.Errorf("expected UnsupportedFormatError, got %T", err)
	}
}
