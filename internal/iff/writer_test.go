package iff

import (
	"bytes"
	"encoding/binary"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/simonhull/audiometa/internal/types"
)

// rewriteWith parses data as format, applies edit to its tags and returns what
// the writer makes of it.
func rewriteWith(t *testing.T, format types.Format, data []byte, edit func(*types.Tags)) ([]byte, error) {
	t.Helper()
	orig := parse(t, format, data)
	cur := orig.Tags.Clone()
	edit(cur)
	keys, fields, err := types.Diff(&orig.Tags, cur, format)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	edits := &types.EditSet{Keys: keys, Fields: fields, Orig: &orig.Tags, Final: cur}
	var out bytes.Buffer
	_, err = (&writer{format: format}).WriteFile(t.Context(), &out, bytes.NewReader(data), int64(len(data)), edits)
	return out.Bytes(), err
}

// preserved returns the writer's PreservedDigest of data.
func preserved(t *testing.T, format types.Format, data []byte) []byte {
	t.Helper()
	sum, err := (&writer{format: format}).PreservedDigest(t.Context(), bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("PreservedDigest: %v", err)
	}
	return sum
}

func TestWriteFile_ManyChunksIsLinear(t *testing.T) {
	t.Parallel()
	le := binary.LittleEndian
	chunks := []string{"fmt ", pcmFmt(le)}
	for range maxChunks - 2 {
		chunks = append(chunks, "JUNK", "")
	}
	data := riff(le, magicRIFF, append(chunks, "data", "\x00\x00")...)
	start := time.Now()
	if _, err := rewriteWith(t, types.FormatWAV, data, func(tags *types.Tags) { tags.Title = "x" }); err != nil {
		t.Fatal(err)
	}
	preserved(t, types.FormatWAV, data)
	if d := time.Since(start); d > 10*time.Second {
		t.Errorf("writing and digesting %d chunks took %v, want well under 10s", maxChunks, d)
	}
}

// rf64 builds an RF64 WAVE file: a ds64 chunk with payload ds, then chunks
// as for riff. It fills in the ds64 RIFF and data sizes.
func rf64(ds []byte, chunks ...string) []byte {
	le := binary.LittleEndian
	data := riff(le, magicRF64, append([]string{idDS64, string(ds)}, chunks...)...)
	le.PutUint32(data[4:], unsetSize)
	le.PutUint64(data[20:], uint64(len(data)-8))
	for i := 0; i+1 < len(chunks); i += 2 {
		if chunks[i] == idData {
			le.PutUint64(data[28:], uint64(len(chunks[i+1])))
		}
	}
	return data
}

func TestWalk_DS64Table(t *testing.T) {
	t.Parallel()
	le := binary.LittleEndian
	ds := make([]byte, ds64MinSize+ds64EntrySize)
	le.PutUint32(ds[24:], 1) // one table entry
	copy(ds[28:], "junk")
	le.PutUint64(ds[32:], 3)
	data := rf64(ds, "fmt ", pcmFmt(le), "junk", "abc", "data", "\x00\x00")
	at := 20 + len(ds) // past the ds64 chunk, whose table also holds "junk"
	at += bytes.Index(data[at:], []byte("junk"))
	le.PutUint32(data[at+4:], unsetSize) // the junk chunk's size now comes from ds64
	c, err := walk(t.Context(), newReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	junk, ok := c.first("junk")
	if !ok || junk.size != 3 || c.broken || len(c.warnings) > 0 {
		t.Errorf("junk = %+v (found %v), broken %v, warnings %v; want size 3 from ds64", junk, ok, c.broken, c.warnings)
	}
}

func TestWriteFile_OddDS64KeepsItsPad(t *testing.T) {
	t.Parallel()
	le := binary.LittleEndian
	data := rf64(make([]byte, ds64MinSize+1), "fmt ", pcmFmt(le), "data", strings.Repeat("\x00", 1600))
	out, err := rewriteWith(t, types.FormatWAV, data, func(tags *types.Tags) { tags.Title = "Odd" })
	if err != nil {
		t.Fatal(err)
	}
	f, err := (&parser{format: types.FormatWAV}).Parse(t.Context(), bytes.NewReader(out), int64(len(out)), "out")
	if err != nil {
		t.Fatalf("output does not parse: %v", err)
	}
	if f.Tags.Title != "Odd" || f.Audio.Duration != 100*time.Millisecond || len(f.Warnings) > 0 {
		t.Errorf("Title %q, Duration %v, Warnings %v", f.Tags.Title, f.Audio.Duration, f.Warnings)
	}
	if !bytes.Equal(preserved(t, types.FormatWAV, out), preserved(t, types.FormatWAV, data)) {
		t.Error("preserved digest changed")
	}
}

// Not parallel: testing.AllocsPerRun refuses to run in a parallel test.
func TestWalk_DS64TableTooLong(t *testing.T) {
	le := binary.LittleEndian
	data := rf64(make([]byte, ds64MinSize), "fmt ", pcmFmt(le), "data", "\x00\x00")
	data = append(data, make([]byte, 1<<20)...)
	le.PutUint32(data[16:], 0xFFFFFFF0) // ds64 chunk size
	le.PutUint32(data[44:], 0xFFFFFFFF) // ds64 table length
	_, err := walk(t.Context(), newReader(data), int64(len(data)))
	if _, ok := errors.AsType[*types.CorruptedFileError](err); !ok {
		t.Fatalf("err = %v, want a CorruptedFileError", err)
	}
	if allocs := testing.AllocsPerRun(5, func() { _, _ = walk(t.Context(), newReader(data), int64(len(data))) }); allocs > 50 {
		t.Errorf("walk made %v allocations, want a handful", allocs)
	}
}
