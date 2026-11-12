package writeutil

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"slices"
	"sync"
	"testing"

	"github.com/simonhull/audiometa/internal/registry"
)

func testData(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i * 31)
	}
	return b
}

func TestCopyRange(t *testing.T) {
	t.Parallel()
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

func TestCopyRange_Concurrent(t *testing.T) {
	t.Parallel()
	data := testData(3 << 20)
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Go(func() {
			off := int64(i * 1000)
			var buf bytes.Buffer
			if _, err := CopyRange(context.Background(), &buf, bytes.NewReader(data), off, int64(len(data))-off); err != nil {
				t.Error(err)
				return
			}
			if !bytes.Equal(buf.Bytes(), data[off:]) {
				t.Errorf("copy %d differs from its source", i)
			}
		})
	}
	wg.Wait()
}

func BenchmarkCopyRange(b *testing.B) {
	for _, size := range []int{64 << 10, 4 << 20} {
		b.Run(fmt.Sprintf("%dKiB", size>>10), func(b *testing.B) {
			src := bytes.NewReader(testData(size))
			b.ReportAllocs()
			for b.Loop() {
				if _, err := CopyRange(context.Background(), io.Discard, src, 0, int64(size)); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestCopyRange_Canceled(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
	_, err := CopyRange(context.Background(), io.Discard, bytes.NewReader(testData(10)), 5, 10)
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("err = %v, want io.ErrUnexpectedEOF", err)
	}
}

func TestCopyRange_InvalidRange(t *testing.T) {
	t.Parallel()
	if _, err := CopyRange(context.Background(), io.Discard, bytes.NewReader(nil), -1, 1); err == nil {
		t.Error("negative offset: want error")
	}
}

func TestDigestRange(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
	var buf bytes.Buffer
	cw := &CountingWriter{W: &buf}
	_, _ = cw.Write([]byte("abc"))
	_, _ = cw.Write([]byte("de"))
	if cw.N != 5 || buf.String() != "abcde" {
		t.Errorf("N = %d, buf = %q", cw.N, buf.String())
	}
}

func TestDigestRanges(t *testing.T) {
	t.Parallel()
	data := testData(1000)
	got, err := DigestRanges(context.Background(), bytes.NewReader(data), []registry.Range{{Off: 0, End: 10}, {Off: 500, End: 500}, {Off: 900, End: 1000}})
	if err != nil {
		t.Fatal(err)
	}
	want := sha256.Sum256(append(slices.Clone(data[:10]), data[900:]...))
	if !bytes.Equal(got, want[:]) {
		t.Errorf("DigestRanges = %x, want %x (the ranges as one stream, empty ones skipped)", got, want)
	}
	if _, err := DigestRanges(context.Background(), bytes.NewReader(data), []registry.Range{{Off: 990, End: 1010}}); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("range past the end: err = %v, want io.ErrUnexpectedEOF", err)
	}
}
