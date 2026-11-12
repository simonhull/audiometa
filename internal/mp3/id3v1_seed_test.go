package mp3

import (
	"bytes"
	"context"
	"slices"
	"testing"

	"github.com/simonhull/audiometa/internal/types"
)

// v1Only is an MP3 with no ID3v2 tag and a full ID3v1.1 trailer.
func v1Only() []byte {
	return append(audioBytes(4096), id3v1Trailer("Old Title", "Some Artist", "Some Album", "2001", "a comment", 3)...)
}

// wantFrame fails unless the first value of the raw key in f is want; an empty
// want means the key must be absent.
func wantFrame(t *testing.T, f *types.File, key, want string) {
	t.Helper()
	got := f.Tags.Get(key)
	switch {
	case want == "" && len(got) != 0:
		t.Errorf("%s = %q, want absent", key, got)
	case want != "" && (len(got) == 0 || got[0] != want):
		t.Errorf("%s = %q, want %q", key, got, want)
	}
}

func TestID3v1Seed_SetTitleKeepsOtherFields(t *testing.T) {
	t.Parallel()
	src := v1Only()
	out := writeBytes(t, src, editsFor(t, src, func(tg *types.Tags) { tg.Title = "New" }))
	f := parseBytes(t, out)
	wantFrame(t, f, "TIT2", "New")
	wantFrame(t, f, "TPE1", "Some Artist")
	wantFrame(t, f, "TALB", "Some Album")
	wantFrame(t, f, "TDRC", "2001")
	wantFrame(t, f, "TRCK", "3")
	if f.Tags.Comment != "a comment" {
		t.Errorf("Comment = %q", f.Tags.Comment)
	}
	if !bytes.Equal(out[len(out)-id3v1Size:], src[len(src)-id3v1Size:]) {
		t.Error("ID3v1 trailer changed")
	}
}

func TestID3v1Seed_ClearArtist(t *testing.T) {
	t.Parallel()
	src := v1Only()
	out := writeBytes(t, src, editsFor(t, src, func(tg *types.Tags) { tg.Artist = "" }))
	f := parseBytes(t, out)
	wantFrame(t, f, "TPE1", "")
	wantFrame(t, f, "TIT2", "Old Title")
	wantFrame(t, f, "TALB", "Some Album")
	if f.Tags.Artist != "" {
		t.Errorf("Artist = %q, want empty", f.Tags.Artist)
	}
}

func TestID3v1Seed_FallbackKeepsID3v2Album(t *testing.T) {
	t.Parallel()
	src := append(append(id3Tag(4, id3Frame(4, "TALB", latin1Text("V2 Album"))), audioBytes(4096)...),
		id3v1Trailer("T1", "A1", "Al1", "2001", "c", 3)...)
	out := writeBytes(t, src, editsFor(t, src, func(tg *types.Tags) { tg.Year = 1999 }))
	f := parseBytes(t, out)
	wantFrame(t, f, "TIT2", "T1")
	wantFrame(t, f, "TPE1", "A1")
	wantFrame(t, f, "TALB", "V2 Album")
	wantFrame(t, f, "TDRC", "1999")
}

func TestID3v1Seed_NoFallbackSeedsNothing(t *testing.T) {
	t.Parallel()
	src := append(append(id3Tag(4, id3Frame(4, "TIT2", latin1Text("V2 Title"))), audioBytes(4096)...),
		id3v1Trailer("T1", "A1", "Al1", "2001", "c", 3)...)
	out := writeBytes(t, src, editsFor(t, src, func(tg *types.Tags) { tg.Album = "X" }))
	f := parseBytes(t, out)
	wantFrame(t, f, "TPE1", "")
	wantFrame(t, f, "TIT2", "V2 Title")
	wantFrame(t, f, "TALB", "X")
}

func TestID3v1Seed_ID3v23UsesTYER(t *testing.T) {
	t.Parallel()
	src := append(append(id3Tag(3, id3Frame(3, "TALB", latin1Text("V2"))), audioBytes(4096)...),
		id3v1Trailer("T1", "A1", "Al1", "2001", "c", 3)...)
	out := writeBytes(t, src, editsFor(t, src, func(tg *types.Tags) { tg.Album = "X" }))
	f := parseBytes(t, out)
	wantFrame(t, f, "TYER", "2001")
	wantFrame(t, f, "TDRC", "")
}

func TestID3v1Seed_InPlace(t *testing.T) {
	t.Parallel()
	trailer := id3v1Trailer("T1", "A1", "Al1", "2001", "c", 3)
	padded := id3TagFlags(4, 0, 512, id3Frame(4, "TALB", latin1Text("V2 Album")))
	src := append(append(slices.Clone(padded), audioBytes(4096)...), trailer...)
	edits := editsFor(t, src, func(tg *types.Tags) { tg.Album = "X" })
	p, err := (&writer{}).InPlacePatch(context.Background(), bytes.NewReader(src), int64(len(src)), edits)
	if err != nil || p == nil || p.Off != 0 {
		t.Fatalf("InPlacePatch = %+v, err %v; want a patch at 0", p, err)
	}
	if len(p.Data) != len(padded) {
		t.Fatalf("patch is %d bytes, want %d", len(p.Data), len(padded))
	}
	patched := slices.Clone(src)
	copy(patched, p.Data)
	f := parseBytes(t, patched)
	wantFrame(t, f, "TIT2", "T1")
	wantFrame(t, f, "TPE1", "A1")
	wantFrame(t, f, "TALB", "X")

	// Without padding the seeded frames do not fit.
	tight := append(append(id3Tag(4, id3Frame(4, "TALB", latin1Text("V2 Album"))), audioBytes(4096)...), trailer...)
	edits = editsFor(t, tight, func(tg *types.Tags) { tg.Album = "X" })
	p, err = (&writer{}).InPlacePatch(context.Background(), bytes.NewReader(tight), int64(len(tight)), edits)
	if err != nil || p != nil {
		t.Fatalf("unpadded InPlacePatch = %+v, err = %v; want nil, nil", p, err)
	}
}

func TestID3v1Seed_EmbeddedTagNeverSeeds(t *testing.T) {
	t.Parallel()
	tagOnly := id3Tag(4, id3Frame(4, "TALB", latin1Text("x")))
	chunk := append(slices.Clone(tagOnly), id3v1Trailer("T1", "A1", "Al1", "2001", "c", 3)...)
	edits := editsFor(t, append(slices.Clone(tagOnly), audioBytes(64)...), func(tg *types.Tags) { tg.Album = "Y" })
	tag, err := EditEmbeddedTag(context.Background(), bytes.NewReader(chunk), int64(len(tagOnly)), edits)
	if err != nil {
		t.Fatalf("EditEmbeddedTag: %v", err)
	}
	f := parseBytes(t, append(tag, audioBytes(64)...))
	wantFrame(t, f, "TIT2", "")
	wantFrame(t, f, "TPE1", "")
	wantFrame(t, f, "TALB", "Y")
}

func TestID3v1Seed_RawKeysWin(t *testing.T) {
	t.Parallel()
	src := v1Only()
	out := writeBytes(t, src, editsFor(t, src, func(tg *types.Tags) {
		tg.Set("TIT2", "KeyTitle")
		tg.Set("TDRC", "2005")
	}))
	f := parseBytes(t, out)
	wantFrame(t, f, "TIT2", "KeyTitle")
	wantFrame(t, f, "TDRC", "2005")
	wantFrame(t, f, "TPE1", "Some Artist")
	wantFrame(t, f, "TALB", "Some Album")
}

// Clearing both Title and Artist writes neither frame, so the reader falls back
// to the stale ID3v1 trailer again. Plan 079's Update and Remove modes fix it.
func TestID3v1Seed_ClearTitleAndArtistFallsBack(t *testing.T) {
	t.Parallel()
	src := v1Only()
	out := writeBytes(t, src, editsFor(t, src, func(tg *types.Tags) {
		tg.Title = ""
		tg.Artist = ""
	}))
	f := parseBytes(t, out)
	wantFrame(t, f, "TIT2", "")
	wantFrame(t, f, "TPE1", "")
	if f.Tags.Title != "Old Title" || f.Tags.Artist != "Some Artist" {
		t.Errorf("Title, Artist = %q, %q; want the ID3v1 values", f.Tags.Title, f.Tags.Artist)
	}
}

func TestID3v1Seed_DoesNotMutateEdits(t *testing.T) {
	t.Parallel()
	src := v1Only()
	edits := editsFor(t, src, func(tg *types.Tags) { tg.Title = "New" })
	before := slices.Clone(edits.Fields)
	writeBytes(t, src, edits)
	if !slices.EqualFunc(before, edits.Fields, func(a, b types.FieldEdit) bool {
		return a.Field == b.Field && slices.Equal(a.Values, b.Values)
	}) {
		t.Errorf("edits.Fields changed: %v -> %v", before, edits.Fields)
	}
}
