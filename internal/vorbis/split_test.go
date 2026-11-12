package vorbis

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/simonhull/audiometa/internal/types"
)

func le32(n uint32) []byte { return binary.LittleEndian.AppendUint32(nil, n) }

func payload(vendor string, tail []byte, entries ...string) []byte {
	var out []byte
	out = append(out, le32(uint32(len(vendor)))...)
	out = append(out, vendor...)
	out = append(out, le32(uint32(len(entries)))...)
	for _, e := range entries {
		out = append(out, le32(uint32(len(e)))...)
		out = append(out, e...)
	}
	return append(out, tail...)
}

func strs(b [][]byte) []string {
	out := make([]string, len(b))
	for i, e := range b {
		out[i] = string(e)
	}
	return out
}

func TestSplitValid(t *testing.T) {
	t.Parallel()
	for _, tail := range [][]byte{nil, {1, 2, 3}} {
		p := payload("vend", tail, "A=1", "B=2", "C=3")
		c, err := Split(t.Context(), p)
		if err != nil {
			t.Fatalf("Split: %v", err)
		}
		if string(c.Vendor) != "vend" || !slices.Equal(strs(c.Entries), []string{"A=1", "B=2", "C=3"}) {
			t.Errorf("vendor %q, entries %q", c.Vendor, strs(c.Entries))
		}
		if !bytes.Equal(c.Tail, tail) {
			t.Errorf("tail %v, want %v", c.Tail, tail)
		}
		if got := c.Encode(); !bytes.Equal(got, p) {
			t.Errorf("Encode = %x, want %x", got, p)
		}
	}
}

func TestSplitTruncated(t *testing.T) {
	t.Parallel()
	good := payload("vend", nil, "A=1", "B=2")
	countOverrun := append(append(le32(1), 'v'), le32(0x0FFFFFFF)...)
	countOverrun = append(countOverrun, le32(3)...)
	countOverrun = append(countOverrun, "A=1"...)
	hugeVendor := append(le32(0xFFFFFFF0), "xx"...)
	hugeComment := append(append(le32(1), 'v'), le32(1)...)
	hugeComment = append(hugeComment, le32(0xFFFFFFF0)...)
	hugeComment = append(hugeComment, "xx"...)

	tests := []struct {
		name    string
		payload []byte
		vendor  string
		entries []string
	}{
		{"nil", nil, "", nil},
		{"vendor overruns", le32(9), "", nil},
		{"count missing", append(le32(1), 'v'), "v", nil},
		{"count overrun", countOverrun, "v", []string{"A=1"}},
		{"last comment cut", good[:len(good)-1], "vend", []string{"A=1"}},
		{"huge vendor", hugeVendor, "", nil},
		{"huge comment", hugeComment, "v", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c, err := Split(t.Context(), tt.payload)
			if !errors.Is(err, ErrTruncated) {
				t.Fatalf("err = %v, want ErrTruncated", err)
			}
			if string(c.Vendor) != tt.vendor || !slices.Equal(strs(c.Entries), tt.entries) {
				t.Errorf("vendor %q, entries %q; want %q, %q", c.Vendor, strs(c.Entries), tt.vendor, tt.entries)
			}
		})
	}
}

func TestSplitCanceled(t *testing.T) {
	t.Parallel()
	entries := make([]string, 5000)
	for i := range entries {
		entries[i] = "A=1"
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := Split(ctx, payload("v", nil, entries...)); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

// TestSplitAllocationBound declares a million comments in a 10-byte body: nothing may be sized
// from the declared count. Bytes per call are measured, since the count of allocations varies
// under the race detector.
func TestSplitAllocationBound(t *testing.T) {
	p := append(append(le32(0), le32(1_000_000)...), 1, 2)
	r := testing.Benchmark(func(b *testing.B) {
		b.Helper()
		b.ReportAllocs()
		for b.Loop() {
			if _, err := Split(context.Background(), p); err == nil {
				b.Fatal("want an error")
			}
		}
	})
	if got := r.AllocedBytesPerOp(); got > 1024 {
		t.Errorf("%d bytes allocated per call, want under 1 KiB (a million-entry slice is 24 MB)", got)
	}
}

func TestApply(t *testing.T) {
	t.Parallel()
	var file types.File
	got := Apply([][]byte{[]byte("TITLE=Foo"), []byte("no equals"), []byte("ARTIST=Bar")}, &file, types.FormatFLAC)
	want := []string{"TITLE=Foo", "no equals", "ARTIST=Bar"}
	if !slices.Equal(got, want) {
		t.Errorf("strings = %q, want %q", got, want)
	}
	if file.Tags.Get("TITLE") == nil || file.Tags.Get("ARTIST") == nil {
		t.Errorf("raw keys missing: %v", file.Tags)
	}
	if len(file.Warnings) != 1 {
		t.Fatalf("warnings = %v, want 1", file.Warnings)
	}
	w := file.Warnings[0]
	if w.Err == nil || w.Stage != types.StageMetadata || !strings.HasPrefix(w.Message, "invalid Vorbis comment:") {
		t.Errorf("warning = %+v", w)
	}
}
