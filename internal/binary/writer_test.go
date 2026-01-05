package binary

import (
	"bytes"
	"testing"
)

func TestSafeWriter_WriteUint32BE(t *testing.T) {
	buf := &bytes.Buffer{}
	sw := NewSafeWriter(buf)

	err := Write[uint32](sw, 0x12345678)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := []byte{0x12, 0x34, 0x56, 0x78}
	if !bytes.Equal(buf.Bytes(), expected) {
		t.Errorf("expected %v, got %v", expected, buf.Bytes())
	}
}

func TestSafeWriter_Offset(t *testing.T) {
	buf := &bytes.Buffer{}
	sw := NewSafeWriter(buf)

	// Initial offset should be 0
	if sw.Offset() != 0 {
		t.Errorf("expected initial offset 0, got %d", sw.Offset())
	}

	// Write uint8 (1 byte)
	err := Write[uint8](sw, 0x01)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sw.Offset() != 1 {
		t.Errorf("expected offset 1 after writing uint8, got %d", sw.Offset())
	}

	// Write uint16 (2 bytes)
	err = Write[uint16](sw, 0x0203)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sw.Offset() != 3 {
		t.Errorf("expected offset 3 after writing uint16, got %d", sw.Offset())
	}

	// Write uint32 (4 bytes)
	err = Write[uint32](sw, 0x04050607)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sw.Offset() != 7 {
		t.Errorf("expected offset 7 after writing uint32, got %d", sw.Offset())
	}

	// Write uint64 (8 bytes)
	err = Write[uint64](sw, 0x08090A0B0C0D0E0F)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sw.Offset() != 15 {
		t.Errorf("expected offset 15 after writing uint64, got %d", sw.Offset())
	}
}

func TestSafeWriter_WriteString(t *testing.T) {
	buf := &bytes.Buffer{}
	sw := NewSafeWriter(buf)

	err := sw.WriteString("test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := []byte("test")
	if !bytes.Equal(buf.Bytes(), expected) {
		t.Errorf("expected %v, got %v", expected, buf.Bytes())
	}

	if sw.Offset() != 4 {
		t.Errorf("expected offset 4 after writing string, got %d", sw.Offset())
	}
}

func TestSafeWriter_WriteLE(t *testing.T) {
	buf := &bytes.Buffer{}
	sw := NewSafeWriter(buf)

	err := WriteLE[uint16](sw, 0x1234)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Little-endian: least significant byte first
	expected := []byte{0x34, 0x12}
	if !bytes.Equal(buf.Bytes(), expected) {
		t.Errorf("expected %v, got %v", expected, buf.Bytes())
	}
}

func TestSafeWriter_WriteBytes(t *testing.T) {
	buf := &bytes.Buffer{}
	sw := NewSafeWriter(buf)

	data := []byte{0xDE, 0xAD, 0xBE, 0xEF}
	err := sw.WriteBytes(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !bytes.Equal(buf.Bytes(), data) {
		t.Errorf("expected %v, got %v", data, buf.Bytes())
	}

	if sw.Offset() != 4 {
		t.Errorf("expected offset 4, got %d", sw.Offset())
	}
}

func TestSafeWriter_WriteUint8(t *testing.T) {
	buf := &bytes.Buffer{}
	sw := NewSafeWriter(buf)

	err := Write[uint8](sw, 0x42)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := []byte{0x42}
	if !bytes.Equal(buf.Bytes(), expected) {
		t.Errorf("expected %v, got %v", expected, buf.Bytes())
	}
}

func TestSafeWriter_WriteUint16BE(t *testing.T) {
	buf := &bytes.Buffer{}
	sw := NewSafeWriter(buf)

	err := Write[uint16](sw, 0xABCD)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Big-endian: most significant byte first
	expected := []byte{0xAB, 0xCD}
	if !bytes.Equal(buf.Bytes(), expected) {
		t.Errorf("expected %v, got %v", expected, buf.Bytes())
	}
}

func TestSafeWriter_WriteUint64BE(t *testing.T) {
	buf := &bytes.Buffer{}
	sw := NewSafeWriter(buf)

	err := Write[uint64](sw, 0x0102030405060708)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}
	if !bytes.Equal(buf.Bytes(), expected) {
		t.Errorf("expected %v, got %v", expected, buf.Bytes())
	}
}

func TestSafeWriter_WriteLEUint32(t *testing.T) {
	buf := &bytes.Buffer{}
	sw := NewSafeWriter(buf)

	err := WriteLE[uint32](sw, 0x12345678)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Little-endian: least significant byte first
	expected := []byte{0x78, 0x56, 0x34, 0x12}
	if !bytes.Equal(buf.Bytes(), expected) {
		t.Errorf("expected %v, got %v", expected, buf.Bytes())
	}
}

func TestSafeWriter_WriteLEUint64(t *testing.T) {
	buf := &bytes.Buffer{}
	sw := NewSafeWriter(buf)

	err := WriteLE[uint64](sw, 0x0102030405060708)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Little-endian: least significant byte first
	expected := []byte{0x08, 0x07, 0x06, 0x05, 0x04, 0x03, 0x02, 0x01}
	if !bytes.Equal(buf.Bytes(), expected) {
		t.Errorf("expected %v, got %v", expected, buf.Bytes())
	}
}

func TestSafeWriter_MultipleWrites(t *testing.T) {
	buf := &bytes.Buffer{}
	sw := NewSafeWriter(buf)

	// Write a mix of types
	_ = Write[uint8](sw, 0x01)
	_ = Write[uint16](sw, 0x0203)
	_ = sw.WriteString("AB")
	_ = Write[uint32](sw, 0x04050607)

	expected := []byte{0x01, 0x02, 0x03, 'A', 'B', 0x04, 0x05, 0x06, 0x07}
	if !bytes.Equal(buf.Bytes(), expected) {
		t.Errorf("expected %v, got %v", expected, buf.Bytes())
	}

	if sw.Offset() != 9 {
		t.Errorf("expected offset 9, got %d", sw.Offset())
	}
}
