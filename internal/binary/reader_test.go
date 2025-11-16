package binary

import (
	"encoding/binary"
	"io"
	"strings"
	"testing"
)

// mockReader implements io.ReaderAt for testing.
type mockReader struct {
	data []byte
}

func (m *mockReader) ReadAt(p []byte, off int64) (n int, err error) {
	if off >= int64(len(m.data)) {
		return 0, io.EOF
	}
	n = copy(p, m.data[off:])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

func TestSafeReader_ReadAt_Success(t *testing.T) {
	data := []byte{0x01, 0x02, 0x03, 0x04}
	mock := &mockReader{data: data}
	sr := NewSafeReader(mock, int64(len(data)), "test.m4b")

	buf := make([]byte, 2)
	err := sr.ReadAt(buf, 0, "test read")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if buf[0] != 0x01 || buf[1] != 0x02 {
		t.Errorf("expected [0x01, 0x02], got [0x%02x, 0x%02x]", buf[0], buf[1])
	}
}

func TestSafeReader_ReadAt_OutOfBounds(t *testing.T) {
	data := []byte{0x01, 0x02, 0x03, 0x04}
	mock := &mockReader{data: data}
	sr := NewSafeReader(mock, int64(len(data)), "test.m4b")

	buf := make([]byte, 2)
	err := sr.ReadAt(buf, 10, "out of bounds read")
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	// Check error message contains useful info
	errMsg := err.Error()
	if !strings.Contains(errMsg, "test.m4b") {
		t.Errorf("error should contain filename: %v", errMsg)
	}
	if !strings.Contains(errMsg, "out of bounds read") {
		t.Errorf("error should contain context: %v", errMsg)
	}
}

func TestRead_Uint8(t *testing.T) {
	data := []byte{0x42}
	mock := &mockReader{data: data}
	sr := NewSafeReader(mock, int64(len(data)), "test.m4b")

	val, err := Read[uint8](sr, 0, "test uint8")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if val != 0x42 {
		t.Errorf("expected 0x42, got 0x%02x", val)
	}
}

func TestRead_Uint16(t *testing.T) {
	data := make([]byte, 2)
	binary.BigEndian.PutUint16(data, 0x1234)
	mock := &mockReader{data: data}
	sr := NewSafeReader(mock, int64(len(data)), "test.m4b")

	val, err := Read[uint16](sr, 0, "test uint16")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if val != 0x1234 {
		t.Errorf("expected 0x1234, got 0x%04x", val)
	}
}

func TestRead_Uint32(t *testing.T) {
	data := make([]byte, 4)
	binary.BigEndian.PutUint32(data, 0x12345678)
	mock := &mockReader{data: data}
	sr := NewSafeReader(mock, int64(len(data)), "test.m4b")

	val, err := Read[uint32](sr, 0, "test uint32")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if val != 0x12345678 {
		t.Errorf("expected 0x12345678, got 0x%08x", val)
	}
}

func TestRead_Uint64(t *testing.T) {
	data := make([]byte, 8)
	binary.BigEndian.PutUint64(data, 0x123456789ABCDEF0)
	mock := &mockReader{data: data}
	sr := NewSafeReader(mock, int64(len(data)), "test.m4b")

	val, err := Read[uint64](sr, 0, "test uint64")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if val != 0x123456789ABCDEF0 {
		t.Errorf("expected 0x123456789ABCDEF0, got 0x%016x", val)
	}
}

func TestReader_Sequential(t *testing.T) {
	data := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}
	mock := &mockReader{data: data}
	sr := NewSafeReader(mock, int64(len(data)), "test.m4b")
	r := NewReader(sr, 0)

	val1, err := ReadValue[uint8](r, "first byte")
	if err != nil {
		t.Fatalf("read 1 failed: %v", err)
	}
	if val1 != 0x01 {
		t.Errorf("expected 0x01, got 0x%02x", val1)
	}

	val2, err := ReadValue[uint16](r, "second word")
	if err != nil {
		t.Fatalf("read 2 failed: %v", err)
	}
	expected := binary.BigEndian.Uint16([]byte{0x02, 0x03})
	if val2 != expected {
		t.Errorf("expected 0x%04x, got 0x%04x", expected, val2)
	}

	// Verify offset advanced correctly
	if r.Offset() != 3 {
		t.Errorf("expected offset 3, got %d", r.Offset())
	}
}

func TestReader_Skip(t *testing.T) {
	data := make([]byte, 100)
	mock := &mockReader{data: data}
	sr := NewSafeReader(mock, int64(len(data)), "test.m4b")
	r := NewReader(sr, 10)

	initialOffset := r.Offset()
	if initialOffset != 10 {
		t.Errorf("expected initial offset 10, got %d", initialOffset)
	}

	r.Skip(20)
	if r.Offset() != 30 {
		t.Errorf("expected offset 30 after skip, got %d", r.Offset())
	}
}

func TestReader_ReadString(t *testing.T) {
	data := []byte("Hello, World!")
	mock := &mockReader{data: data}
	sr := NewSafeReader(mock, int64(len(data)), "test.m4b")
	r := NewReader(sr, 0)

	str, err := r.ReadString(5, "greeting")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if str != "Hello" {
		t.Errorf("expected 'Hello', got '%s'", str)
	}

	if r.Offset() != 5 {
		t.Errorf("expected offset 5, got %d", r.Offset())
	}
}

func TestChainReader_Success(t *testing.T) {
	data := []byte{0x01, 0x02, 0x03, 0x04}
	mock := &mockReader{data: data}
	sr := NewSafeReader(mock, int64(len(data)), "test.m4b")
	r := NewReader(sr, 0)
	cr := NewChainReader(r)

	v1 := ReadChained[uint8](cr, "first")
	v2 := ReadChained[uint8](cr, "second")
	v3 := ReadChained[uint8](cr, "third")

	if err := cr.Error(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if v1 != 0x01 || v2 != 0x02 || v3 != 0x03 {
		t.Errorf("unexpected values: %02x %02x %02x", v1, v2, v3)
	}
}

func TestChainReader_ErrorAccumulation(t *testing.T) {
	data := []byte{0x01, 0x02}
	mock := &mockReader{data: data}
	sr := NewSafeReader(mock, int64(len(data)), "test.m4b")
	r := NewReader(sr, 0)
	cr := NewChainReader(r)

	_ = ReadChained[uint8](cr, "first")  // OK
	_ = ReadChained[uint8](cr, "second") // OK
	_ = ReadChained[uint8](cr, "third")  // Error - out of bounds

	if cr.Error() == nil {
		t.Fatal("expected error, got nil")
	}

	// Once error occurs, subsequent reads should not execute
	_ = ReadChained[uint8](cr, "fourth")
	// Should still have the same (first) error
	if cr.Error() == nil {
		t.Fatal("error should persist")
	}
}

func BenchmarkRead_Uint32(b *testing.B) {
	data := make([]byte, 1024*1024) // 1MB
	for i := 0; i < len(data); i += 4 {
		binary.BigEndian.PutUint32(data[i:], uint32(i))
	}
	mock := &mockReader{data: data}
	sr := NewSafeReader(mock, int64(len(data)), "bench.m4b")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		offset := int64((i % (len(data) / 4)) * 4)
		_, _ = Read[uint32](sr, offset, "benchmark")
	}
}

func BenchmarkReader_Sequential(b *testing.B) {
	data := make([]byte, 1024*1024) // 1MB
	mock := &mockReader{data: data}
	sr := NewSafeReader(mock, int64(len(data)), "bench.m4b")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r := NewReader(sr, 0)
		for j := 0; j < 1000; j++ {
			_, _ = ReadValue[uint32](r, "test")
		}
	}
}
