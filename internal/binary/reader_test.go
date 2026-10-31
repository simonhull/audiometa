package binary

import (
	"encoding/binary"
	"errors"
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
	for i := range b.N {
		offset := int64((i % (len(data) / 4)) * 4)
		_, _ = Read[uint32](sr, offset, "benchmark")
	}
}

func BenchmarkReader_Sequential(b *testing.B) {
	data := make([]byte, 1024*1024) // 1MB
	mock := &mockReader{data: data}
	sr := NewSafeReader(mock, int64(len(data)), "bench.m4b")

	b.ResetTimer()
	for range b.N {
		r := NewReader(sr, 0)
		for range 1000 {
			_, _ = ReadValue[uint32](r, "test")
		}
	}
}

func TestSafeReader_CheckLen(t *testing.T) {
	sr := NewSafeReader(&mockReader{data: make([]byte, 100)}, 100, "t")
	tests := []struct {
		name            string
		off             int64
		count, elemSize int
		wantErr         bool
	}{
		{"valid", 10, 90, 1, false},
		{"valid elem size", 0, 25, 4, false},
		{"exceeds file", 10, 91, 1, true},
		{"exceeds file via elem size", 0, 26, 4, true},
		{"exceeds max alloc", 0, MaxAllocBytes + 1, 1, true},
		{"negative count", 0, -1, 1, true},
		{"negative offset", -1, 1, 1, true},
		{"offset past end", 101, 0, 1, true},
		{"zero elem size", 0, 1, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := sr.CheckLen(tt.off, tt.count, tt.elemSize, "x")
			if (err != nil) != tt.wantErr {
				t.Errorf("err = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// countingReader counts calls to ReadAt on the wrapped mockReader.
type countingReader struct {
	mockReader
	calls int
}

func (c *countingReader) ReadAt(p []byte, off int64) (int, error) {
	c.calls++
	return c.mockReader.ReadAt(p, off)
}

func seqData(n int) []byte {
	d := make([]byte, n)
	for i := range d {
		d[i] = byte(i * 7)
	}
	return d
}

func TestSafeReader_Window_SequentialReadsOneCall(t *testing.T) {
	data := seqData(windowSize * 2)
	cr := &countingReader{mockReader: mockReader{data: data}}
	sr := NewSafeReader(cr, int64(len(data)), "w.bin")
	for i := range 100 {
		v, err := Read[uint32](sr, int64(i*4), "x")
		if err != nil {
			t.Fatal(err)
		}
		if want := binary.BigEndian.Uint32(data[i*4:]); v != want {
			t.Fatalf("read %d = %x, want %x", i, v, want)
		}
	}
	if cr.calls != 1 {
		t.Errorf("underlying calls = %d, want 1", cr.calls)
	}
}

func TestSafeReader_Window_MissRefills(t *testing.T) {
	data := seqData(windowSize * 3)
	cr := &countingReader{mockReader: mockReader{data: data}}
	sr := NewSafeReader(cr, int64(len(data)), "w.bin")
	buf := make([]byte, 4)
	if err := sr.ReadAt(buf, 0, "a"); err != nil {
		t.Fatal(err)
	}
	off := int64(windowSize + 100)
	if err := sr.ReadAt(buf, off, "b"); err != nil {
		t.Fatal(err)
	}
	if cr.calls != 2 {
		t.Errorf("calls = %d, want 2", cr.calls)
	}
	if string(buf) != string(data[off:off+4]) {
		t.Errorf("wrong bytes after refill")
	}
}

func TestSafeReader_Window_LargeReadBypasses(t *testing.T) {
	data := seqData(windowSize * 2)
	cr := &countingReader{mockReader: mockReader{data: data}}
	sr := NewSafeReader(cr, int64(len(data)), "w.bin")
	small := make([]byte, 4)
	if err := sr.ReadAt(small, 0, "small"); err != nil {
		t.Fatal(err)
	}
	big := make([]byte, windowSize/4+1)
	if err := sr.ReadAt(big, 10, "big"); err != nil {
		t.Fatal(err)
	}
	if cr.calls != 2 {
		t.Fatalf("calls after big read = %d, want 2", cr.calls)
	}
	if string(big) != string(data[10:10+len(big)]) {
		t.Error("big read returned wrong bytes")
	}
	if err := sr.ReadAt(small, 100, "small again"); err != nil {
		t.Fatal(err)
	}
	if cr.calls != 2 {
		t.Errorf("window was evicted by big read: calls = %d, want 2", cr.calls)
	}
}

func TestSafeReader_ErrorMessagesUnchanged(t *testing.T) {
	data := seqData(10)
	sr := NewSafeReader(&mockReader{data: data}, 10, "p.bin")
	buf := make([]byte, 4)
	tests := []struct {
		off  int64
		n    int
		want string
	}{
		{-1, 4, "p.bin: offset -1 out of bounds (file size: 10) while reading w"},
		{10, 4, "p.bin: offset 10 out of bounds (file size: 10) while reading w"},
		{8, 4, "p.bin: read of 4 bytes at offset 8 would exceed file size 10 while reading w"},
	}
	for _, tt := range tests {
		err := sr.ReadAt(buf[:tt.n], tt.off, "w")
		if err == nil || err.Error() != tt.want {
			t.Errorf("off %d: err = %v, want %q", tt.off, err, tt.want)
		}
	}
}

type failingReader struct {
	err   error
	short int // bytes to return with nil error when err == nil
}

func (f failingReader) ReadAt(p []byte, _ int64) (int, error) {
	if f.err != nil {
		return 0, f.err
	}
	return f.short, nil
}

func TestSafeReader_UnderlyingError(t *testing.T) {
	boom := errors.New("boom")
	sr := NewSafeReader(failingReader{err: boom}, 100, "p.bin")
	err := sr.ReadAt(make([]byte, 4), 8, "thing")
	if err == nil || err.Error() != "p.bin: failed to read thing at offset 8: boom" {
		t.Errorf("err = %v", err)
	}
	if !errors.Is(err, boom) {
		t.Error("errors.Is(err, boom) = false")
	}
}

func TestSafeReader_ShortRead(t *testing.T) {
	sr := NewSafeReader(failingReader{short: 2}, 100, "p.bin")
	err := sr.ReadAt(make([]byte, 4), 8, "thing")
	if err == nil || err.Error() != "p.bin: short read for thing at offset 8: got 2 bytes, expected 4" {
		t.Errorf("err = %v", err)
	}
}

func TestSafeReader_Window_NearEOF(t *testing.T) {
	data := seqData(windowSize + 50)
	cr := &countingReader{mockReader: mockReader{data: data}}
	sr := NewSafeReader(cr, int64(len(data)), "w.bin")
	buf := make([]byte, 8)
	off := int64(len(data) - 8)
	if err := sr.ReadAt(buf, off, "tail"); err != nil {
		t.Fatal(err)
	}
	if string(buf) != string(data[off:]) {
		t.Error("tail bytes wrong")
	}
	if err := sr.ReadAt(buf[:4], off+2, "tail2"); err != nil {
		t.Fatal(err)
	}
	if cr.calls != 1 {
		t.Errorf("calls = %d, want 1", cr.calls)
	}
}

func TestRead_ZeroAllocsAfterWarmup(t *testing.T) {
	data := seqData(64)
	sr := NewSafeReader(&mockReader{data: data}, int64(len(data)), "w.bin")
	if _, err := Read[uint32](sr, 0, "warm"); err != nil {
		t.Fatal(err)
	}
	if n := testing.AllocsPerRun(100, func() { _, _ = Read[uint32](sr, 0, "x") }); n != 0 {
		t.Errorf("allocs = %v, want 0", n)
	}
}
