package flac

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/simonhull/audiometa/internal/types"
)

// id3Prefix is a 30-byte ID3v2.3 tag (one TIT2 frame and padding), as some
// taggers put in front of a FLAC stream.
func id3Prefix() []byte {
	body := append([]byte{'T', 'I', 'T', '2', 0, 0, 0, 5, 0, 0}, "\x00ID3!"...)
	body = append(body, make([]byte, 5)...)
	return append([]byte{'I', 'D', '3', 3, 0, 0, 0, 0, 0, byte(len(body))}, body...)
}

func TestParse_ID3Prefix(t *testing.T) {
	t.Parallel()
	prefix := id3Prefix()
	src := slices.Concat(prefix, pictureFLAC())
	f := parseBytes(t, src)
	if f.Tags.Title != "T" || f.Audio.SampleRate != 44100 {
		t.Errorf("Title %q, SampleRate %d; want the FLAC stream's", f.Tags.Title, f.Audio.SampleRate)
	}
	want := fmt.Sprintf("non-standard ID3v2 tag of %d bytes", len(prefix))
	if len(f.Warnings) != 1 || !strings.Contains(f.Warnings[0].Message, want) {
		t.Errorf("warnings = %v, want one containing %q", f.Warnings, want)
	}
	art, err := (&parser{}).ExtractArtwork(context.Background(), bytes.NewReader(src), int64(len(src)), "t.flac")
	if err != nil || len(art) != 2 {
		t.Errorf("ExtractArtwork = %d pictures, %v; want 2", len(art), err)
	}
}

func TestWriter_KeepsID3Prefix(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	prefix, plain := id3Prefix(), pictureFLAC()
	src := slices.Concat(prefix, plain)
	retitle := func(tg *types.Tags) { tg.Title = "New" }
	edits := editsFor(t, src, retitle)

	out := writeBytes(t, src, edits)
	if !bytes.HasPrefix(out, slices.Concat(prefix, []byte(flacMagic))) {
		t.Fatalf("output starts %q, want the ID3v2 tag then fLaC", out[:min(len(out), 40)])
	}
	if want := writeBytes(t, plain, editsFor(t, plain, retitle)); !bytes.Equal(out[len(prefix):], want) {
		t.Error("after the prefix, the output differs from editing the same file without it")
	}

	w := &writer{}
	p, err := w.InPlacePatch(ctx, bytes.NewReader(src), int64(len(src)), edits)
	if err != nil || p == nil || p.Off != int64(len(prefix)+len(flacMagic)) {
		t.Fatalf("InPlacePatch = %+v, err %v; want a patch at offset %d", p, err, len(prefix)+len(flacMagic))
	}
	patched := slices.Clone(src)
	copy(patched[p.Off:], p.Data)
	if !bytes.Equal(patched, out) {
		t.Error("the in-place patch does not give WriteFile's output")
	}

	digest := func(f func(context.Context, []byte) ([]byte, error), b []byte) []byte {
		t.Helper()
		d, err := f(ctx, b)
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	audio := func(ctx context.Context, b []byte) ([]byte, error) {
		return w.AudioDigest(ctx, bytes.NewReader(b), int64(len(b)))
	}
	preserved := func(ctx context.Context, b []byte) ([]byte, error) {
		return w.PreservedDigest(ctx, bytes.NewReader(b), int64(len(b)))
	}
	if !bytes.Equal(digest(audio, src), digest(audio, plain)) {
		t.Error("AudioDigest depends on the ID3v2 prefix")
	}
	tampered := slices.Clone(src)
	tampered[20] ^= 0xFF // inside the TIT2 frame
	if bytes.Equal(digest(preserved, src), digest(preserved, tampered)) {
		t.Error("PreservedDigest does not cover the ID3v2 prefix that WriteFile copies")
	}
}
