package writeutil

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"testing"
)

func testData(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i * 31)
	}
	return b
}

func TestCopyRange(t *testing.T) {
	data := testData(3 << 20)
	for _, off := range []int64{0, 1} {
		var buf bytes.Buffer
		n := int64(len(data)) - off
		got, err := CopyRange(context.Background(), &buf, bytes.NewReader(data), off, n)
		if err != nil {
			t.Fatalf("off %d: %v", off, err)
		}
		if got != n || !bytes.Equal(buf.Bytes(), data[off:]) {
			t.Errorf("off %d: copied %d bytes, content equal = %v", off, got, bytes.Equal(buf.Bytes(), data[off:]))
		}
	}
}

func TestCopyRange_Canceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var buf bytes.Buffer
	_, err := CopyRange(ctx, &buf, bytes.NewReader(testData(10)), 0, 10)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
	if buf.Len() != 0 {
		t.Errorf("wrote %d bytes after cancel", buf.Len())
	}
}

func TestCopyRange_ShortSource(t *testing.T) {
	_, err := CopyRange(context.Background(), io.Discard, bytes.NewReader(testData(10)), 5, 10)
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("err = %v, want io.ErrUnexpectedEOF", err)
	}
}

func TestCopyRange_InvalidRange(t *testing.T) {
	if _, err := CopyRange(context.Background(), io.Discard, bytes.NewReader(nil), -1, 1); err == nil {
		t.Error("negative offset: want error")
	}
}

func TestDigestRange(t *testing.T) {
	data := testData(2<<20 + 17)
	got, err := DigestRange(context.Background(), bytes.NewReader(data), 3, int64(len(data))-10)
	if err != nil {
		t.Fatal(err)
	}
	want := sha256.Sum256(data[3 : len(data)-7])
	if !bytes.Equal(got, want[:]) {
		t.Error("DigestRange mismatch")
	}
}

func TestCountingWriter(t *testing.T) {
	var buf bytes.Buffer
	cw := &CountingWriter{W: &buf}
	_, _ = cw.Write([]byte("abc"))
	_, _ = cw.Write([]byte("de"))
	if cw.N != 5 || buf.String() != "abcde" {
		t.Errorf("N = %d, buf = %q", cw.N, buf.String())
	}
}
