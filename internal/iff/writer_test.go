package iff

import (
	"bytes"
	"encoding/binary"
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
