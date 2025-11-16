package m4a

import (
	"bytes"
	"encoding/binary"
	"testing"

	audiobinary "github.com/simonhull/audiometa/internal/binary"
)

// createMockAtom creates a test atom with given type and data.
func createMockAtom(atomType string, data []byte) []byte {
	buf := &bytes.Buffer{}

	// Write size (8 byte header + data length)
	size := uint32(8 + len(data))
	binary.Write(buf, binary.BigEndian, size)

	// Write type
	buf.WriteString(atomType)

	// Write data
	buf.Write(data)

	return buf.Bytes()
}

func TestReadAtomHeader_Success(t *testing.T) {
	data := createMockAtom("moov", []byte{0x01, 0x02, 0x03, 0x04})

	sr := audiobinary.NewSafeReader(bytes.NewReader(data), int64(len(data)), "test.m4b")

	atom, err := readAtomHeader(sr, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if atom.Size != 12 {
		t.Errorf("expected size 12, got %d", atom.Size)
	}

	if atom.Type != "moov" {
		t.Errorf("expected type 'moov', got %s", atom.Type)
	}

	if atom.Offset != 0 {
		t.Errorf("expected offset 0, got %d", atom.Offset)
	}

	if atom.DataSize() != 4 {
		t.Errorf("expected data size 4, got %d", atom.DataSize())
	}

	if atom.DataOffset() != 8 {
		t.Errorf("expected data offset 8, got %d", atom.DataOffset())
	}
}

func TestReadAtomHeader_Extended(t *testing.T) {
	buf := &bytes.Buffer{}

	// Extended size atom: size=1, then 64-bit size
	binary.Write(buf, binary.BigEndian, uint32(1))
	buf.WriteString("mdat")
	binary.Write(buf, binary.BigEndian, uint64(1000))

	data := buf.Bytes()
	sr := audiobinary.NewSafeReader(bytes.NewReader(data), int64(len(data)), "test.m4b")

	atom, err := readAtomHeader(sr, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if atom.Size != 1000 {
		t.Errorf("expected size 1000, got %d", atom.Size)
	}

	if atom.Type != "mdat" {
		t.Errorf("expected type 'mdat', got %s", atom.Type)
	}

	// Extended header is 16 bytes
	if atom.DataOffset() != 16 {
		t.Errorf("expected data offset 16, got %d", atom.DataOffset())
	}
}

func TestReadAtomHeader_TooSmall(t *testing.T) {
	// File too small to contain atom header
	data := []byte{0x00, 0x00, 0x00}

	sr := audiobinary.NewSafeReader(bytes.NewReader(data), int64(len(data)), "test.m4b")

	_, err := readAtomHeader(sr, 0)
	if err == nil {
		t.Fatal("expected error for file too small")
	}
}

func TestReadAtomHeader_InvalidSize(t *testing.T) {
	buf := &bytes.Buffer{}

	// Size too small (less than 8)
	binary.Write(buf, binary.BigEndian, uint32(4))
	buf.WriteString("test")

	data := buf.Bytes()
	sr := audiobinary.NewSafeReader(bytes.NewReader(data), int64(len(data)), "test.m4b")

	_, err := readAtomHeader(sr, 0)
	if err == nil {
		t.Fatal("expected error for invalid atom size")
	}
}

func TestFindAtom_Found(t *testing.T) {
	// Create a container with multiple atoms
	atom1 := createMockAtom("free", []byte{0x00, 0x00})
	atom2 := createMockAtom("moov", []byte{0x01, 0x02, 0x03})
	atom3 := createMockAtom("mdat", []byte{0x04, 0x05})

	data := append(atom1, atom2...)
	data = append(data, atom3...)

	sr := audiobinary.NewSafeReader(bytes.NewReader(data), int64(len(data)), "test.m4b")

	atom, err := findAtom(sr, 0, int64(len(data)), "moov")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if atom.Type != "moov" {
		t.Errorf("expected type 'moov', got %s", atom.Type)
	}

	// moov should be at offset 10 (after first atom)
	if atom.Offset != int64(len(atom1)) {
		t.Errorf("expected offset %d, got %d", len(atom1), atom.Offset)
	}
}

func TestFindAtom_NotFound(t *testing.T) {
	atom1 := createMockAtom("free", []byte{0x00})
	atom2 := createMockAtom("mdat", []byte{0x01})

	data := append(atom1, atom2...)

	sr := audiobinary.NewSafeReader(bytes.NewReader(data), int64(len(data)), "test.m4b")

	_, err := findAtom(sr, 0, int64(len(data)), "moov")
	if err == nil {
		t.Fatal("expected error when atom not found")
	}
}

func TestFindAtom_Nested(t *testing.T) {
	// Create nested structure: udta contains meta
	metaAtom := createMockAtom("meta", []byte{0x01, 0x02})
	udtaData := createMockAtom("udta", metaAtom)

	sr := audiobinary.NewSafeReader(bytes.NewReader(udtaData), int64(len(udtaData)), "test.m4b")

	// Find the udta atom first
	udtaAtom, err := readAtomHeader(sr, 0)
	if err != nil {
		t.Fatalf("failed to read udta: %v", err)
	}

	if udtaAtom.Type != "udta" {
		t.Fatalf("expected type 'udta', got %s", udtaAtom.Type)
	}

	// Now find meta inside udta
	foundMeta, err := findAtom(sr, udtaAtom.DataOffset(), udtaAtom.DataOffset()+int64(udtaAtom.DataSize()), "meta")
	if err != nil {
		t.Fatalf("failed to find nested meta: %v", err)
	}

	if foundMeta.Type != "meta" {
		t.Errorf("expected type 'meta', got %s", foundMeta.Type)
	}
}

func TestAtom_IsContainer(t *testing.T) {
	tests := []struct {
		atomType    string
		isContainer bool
	}{
		{"moov", true},
		{"udta", true},
		{"meta", true},
		{"ilst", true},
		{"trak", true},
		{"mdia", true},
		{"minf", true},
		{"stbl", true},
		{"mdat", false},
		{"free", false},
		{"ftyp", false},
		{"data", false},
	}

	for _, tt := range tests {
		atom := &Atom{Type: tt.atomType}
		if got := atom.IsContainer(); got != tt.isContainer {
			t.Errorf("Atom(%s).IsContainer() = %v, want %v", tt.atomType, got, tt.isContainer)
		}
	}
}
