package iff

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/simonhull/audiometa/internal/testbudget"
	"github.com/simonhull/audiometa/internal/types"
)

func FuzzParseIFF(f *testing.F) {
	for _, name := range []string{"sample.wav", "sample_id3.wav", "sample_rf64.wav", "sample.aiff", "sample.aifc", "sample_ext.wav", "sample_float.wav"} {
		if data, err := os.ReadFile(filepath.Join("..", "..", "testdata", name)); err == nil {
			f.Add(data)
		}
	}
	f.Add([]byte("RIFF\x04\x00\x00\x00WAVE"))
	f.Add([]byte("FORM\x00\x00\x00\x04AIFC"))
	f.Add([]byte("RF64\xff\xff\xff\xffWAVEds64"))
	f.Fuzz(func(t *testing.T, data []byte) {
		// Must not panic or hang on any input; errors are fine.
		for _, format := range []types.Format{types.FormatWAV, types.FormatAIFF} {
			fuzzFormat(t, format, data)
		}
	})
}

// fuzzFormat runs every operation on data as format, each within its budget.
// A file WriteFile produces from a parsable input must parse again and keep the audio and every
// preserved byte of the input.
func fuzzFormat(t *testing.T, format types.Format, data []byte) {
	t.Helper()
	ctx, r, n := context.Background(), bytes.NewReader(data), int64(len(data))
	p, w := &parser{format: format}, &writer{format: format}
	read, write := testbudget.For(testbudget.Read, format), testbudget.For(testbudget.Write, format)

	var parseErr error
	testbudget.Check(t, read, len(data), "Parse", func() { _, parseErr = p.Parse(ctx, r, n, "fuzz") })
	testbudget.Check(t, read, len(data), "ExtractArtwork", func() { _, _ = p.ExtractArtwork(ctx, r, n, "fuzz") })
	var audio, preserved []byte
	var audioErr, preservedErr error
	testbudget.Check(t, read, len(data), "AudioDigest", func() { audio, audioErr = w.AudioDigest(ctx, r, n) })
	testbudget.Check(t, read, len(data), "PreservedDigest", func() { preserved, preservedErr = w.PreservedDigest(ctx, r, n) })

	var out bytes.Buffer
	edits := &types.EditSet{Orig: &types.Tags{}, Final: &types.Tags{}, Fields: []types.FieldEdit{{Field: types.FieldTitle, Values: []string{"x"}}}}
	var err error
	testbudget.Check(t, write, len(data), "WriteFile", func() { _, err = w.WriteFile(ctx, &out, r, n, edits) })
	if err != nil {
		return
	}

	or, on := bytes.NewReader(out.Bytes()), int64(out.Len())
	// Open refuses an input that does not parse, so only a parsable one promises a parsable output.
	if _, err := p.Parse(ctx, or, on, "fuzz"); err != nil && parseErr == nil {
		t.Fatalf("%v: output does not parse: %v", format, err)
	}
	if audioErr == nil {
		got, err := w.AudioDigest(ctx, or, on)
		if err != nil || !bytes.Equal(got, audio) {
			t.Fatalf("%v: AudioDigest of the output = %x, %v; input has %x", format, got, err, audio)
		}
	}
	if preservedErr == nil {
		got, err := w.PreservedDigest(ctx, or, on)
		if err != nil || !bytes.Equal(got, preserved) {
			t.Fatalf("%v: PreservedDigest of the output = %x, %v; input has %x", format, got, err, preserved)
		}
	}
}
