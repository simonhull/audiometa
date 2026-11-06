package mp3

import (
	"errors"
	"slices"
	"testing"

	"github.com/simonhull/audiometa/internal/types"
)

func TestRawKey_RefusesFalseTXXX(t *testing.T) {
	keys := []string{
		"", "USLT", "COMM", "APIC", "UFID", "PRIV", "POPM", "WOAR", "TXXX", "WXXX", "GEOB", "CHAP", "CTOC", "IPLS", "TCAT",
	}
	for _, v := range []byte{3, 4} {
		src := file(id3Tag(v, id3Frame(v, "TIT2", latin1Text("T"))))
		for _, key := range keys {
			_, err := tryWrite(src, editsFor(t, src, func(tg *types.Tags) { tg.Set(key, "x") }))
			if !errors.Is(err, types.ErrUnsupportedWrite) {
				t.Errorf("v2.%d %q: err = %v, want ErrUnsupportedWrite", v, key, err)
			}
		}
	}
}

func TestRawKey_CustomKeysStillWriteTXXX(t *testing.T) {
	for _, key := range []string{"MOOD", "ISBN", "ASIN", "LANG", "ISRC", "DATE", "WORK", "TEST", "CODE", "ZZZ", "X", "mood", "My Key", "MusicBrainz Album Id"} {
		src := file(id3Tag(4, id3Frame(4, "TIT2", latin1Text("T"))))
		out := writeBytes(t, src, editsFor(t, src, func(tg *types.Tags) { tg.Set(key, "x") }))
		if got := frameIDs(framesOf(t, out).frames); !slices.Equal(got, []string{"TIT2", "TXXX"}) {
			t.Errorf("%q: frames %v", key, got)
		}
	}
}

// TestRawKey_EveryBoundKeyWrites guards the documented paths: no key a typed
// field reads or writes may be refused, whichever one a file holds.
func TestRawKey_EveryBoundKeyWrites(t *testing.T) {
	wt := framesOf(t, file(id3Tag(4)))
	refused := func(key string) bool { return !wt.isTextKey(key) && wt.isFalseTXXX(key) }
	for _, b := range types.FieldBindings(types.FormatMP3) {
		for _, key := range b.Keys {
			if refused(key) && key != frameIDUSLT { // USLT goes through applyLyrics
				t.Errorf("%s key %q is refused", b.Field, key)
			}
		}
	}
	for field, key := range canonicalKeys {
		if refused(key) {
			t.Errorf("canonical %s key %q is refused", field, key)
		}
	}
}

func TestRawKey_TypedFieldsWithFrameShapedKeysWrite(t *testing.T) {
	src := file(id3Tag(3, id3Frame(3, "TIT2", latin1Text("T"))))
	out := writeBytes(t, src, editsFor(t, src, func(tg *types.Tags) { tg.ISBN, tg.ASIN = "978", "B00" }))
	if tags := parseBytes(t, out).Tags; tags.ISBN != "978" || tags.ASIN != "B00" {
		t.Errorf("ISBN = %q, ASIN = %q", tags.ISBN, tags.ASIN)
	}
}

func TestRawKey_ExistingTXXXStaysEditable(t *testing.T) {
	for _, desc := range []string{"POPM", "Comm", "WOAR"} {
		src := file(id3Tag(4, id3Frame(4, "TXXX", txxxPayload(3, desc, "old"))))
		key := parseRawKey(t, src)

		out := writeBytes(t, src, editsFor(t, src, func(tg *types.Tags) { tg.Set(key, "new") }))
		if got := parseBytes(t, out).Tags.Get(key); !slices.Equal(got, []string{"new"}) {
			t.Errorf("%s: set reads %q", desc, got)
		}
		if got := framesOf(t, out); len(got.frames) != 1 || got.frames[0].id != frameIDTXXX {
			t.Errorf("%s: frames %v", desc, frameIDs(got.frames))
		}

		out = writeBytes(t, src, editsFor(t, src, func(tg *types.Tags) { tg.Delete(key) }))
		if got := framesOf(t, out).frames; len(got) != 0 {
			t.Errorf("%s: delete left %v", desc, frameIDs(got))
		}
	}
}

// parseRawKey returns the only raw key of src.
func parseRawKey(t *testing.T, src []byte) string {
	t.Helper()
	tags := parseBytes(t, src).Tags
	var keys []string
	for k := range tags.All() {
		keys = append(keys, k)
	}
	if len(keys) != 1 {
		t.Fatalf("raw keys %q", keys)
	}
	return keys[0]
}

func TestRawKey_RealNonTextFrameIsNotReplaced(t *testing.T) {
	src := file(id3Tag(4, id3Frame(4, "WOAR", []byte("http://x"))))
	if _, err := tryWrite(src, editsFor(t, src, func(tg *types.Tags) { tg.Set("WOAR", "y") })); !errors.Is(err, types.ErrUnsupportedWrite) {
		t.Errorf("err = %v, want ErrUnsupportedWrite", err)
	}
}

func TestRawKey_PresentNonStandardTextFrameStaysEditable(t *testing.T) {
	src := file(id3Tag(4, id3Frame(4, "TCAT", latin1Text("old"))))
	out := writeBytes(t, src, editsFor(t, src, func(tg *types.Tags) { tg.Set("TCAT", "new") }))
	if got := frameIDs(framesOf(t, out).frames); !slices.Equal(got, []string{"TCAT"}) {
		t.Errorf("frames %v", got)
	}
}

func TestRawKey_V22SourceMapsBeforeTheCheck(t *testing.T) {
	src := file(id3Tag(2, id3v22Frame("TT2", latin1Text("T"))))
	for key, want := range map[string]error{"COM": types.ErrUnsupportedWrite, "WAR": types.ErrUnsupportedWrite, "TAL": nil} {
		_, err := tryWrite(src, editsFor(t, src, func(tg *types.Tags) { tg.Set(key, "x") }))
		if !errors.Is(err, want) {
			t.Errorf("%s: err = %v, want %v", key, err, want)
		}
	}
}

// A frame ID is upper-case, so any other spelling is an ordinary description.
func TestRawKey_OtherCasesStayCustom(t *testing.T) {
	for _, key := range []string{"text", "time", "user", "link", "tit2", "Popm"} {
		src := file(id3Tag(4, id3Frame(4, "TIT2", latin1Text("T"))))
		out := writeBytes(t, src, editsFor(t, src, func(tg *types.Tags) { tg.Set(key, "x") }))
		if got := frameIDs(framesOf(t, out).frames); !slices.Equal(got, []string{"TIT2", "TXXX"}) {
			t.Errorf("%q: frames %v", key, got)
		}
	}
}
