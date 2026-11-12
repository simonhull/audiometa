package mp3

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/simonhull/audiometa/internal/types"
)

// usltPayload builds a USLT payload in encoding 0 (Latin-1) or 3 (UTF-8).
func usltPayload(enc byte, lang, desc, text string) []byte {
	b := append([]byte{enc}, lang...)
	b = append(b, desc...)
	b = append(b, 0)
	return append(b, text...)
}

// usltUTF16Payload builds a USLT payload in encoding 1, each string with a BOM.
func usltUTF16Payload(lang, desc, text string) []byte {
	b := append([]byte{encUTF16}, lang...)
	b = append(b, utf16Text(desc)[1:]...)
	b = append(b, 0, 0)
	return append(b, utf16Text(text)[1:]...)
}

// usltFrames decodes every USLT frame of data.
func usltFrames(t *testing.T, data []byte) []langText {
	t.Helper()
	wt := framesOf(t, data)
	out := make([]langText, 0, len(framesWithID(wt.frames, frameIDUSLT)))
	for _, f := range framesWithID(wt.frames, frameIDUSLT) {
		c, ok := wt.frameLangText(f)
		if !ok {
			t.Fatalf("unreadable USLT frame %x", f.payload)
		}
		out = append(out, c)
	}
	return out
}

func TestParseLyrics(t *testing.T) {
	lines := "first line\r\nsecond line\n"
	tests := []struct {
		name string
		data []byte
		want string
		raw  []string
	}{
		{"v2.3 Latin-1", id3Tag(3, id3Frame(3, "USLT", usltPayload(0, "eng", "", lines))), lines, []string{lines}},
		{"v2.3 UTF-16", id3Tag(3, id3Frame(3, "USLT", usltUTF16Payload("eng", "d", "歌詞"))), "歌詞", []string{"歌詞"}},
		{"v2.4 UTF-8", id3Tag(4, id3Frame(4, "USLT", usltPayload(3, "jpn", "d", "歌詞"))), "歌詞", []string{"歌詞"}},
		{"v2.2 ULT", id3Tag(2, id3v22Frame("ULT", usltPayload(0, "eng", "", "la"))), "la", []string{"la"}},
		{"first frame fills the field, every frame is raw", id3Tag(4,
			id3Frame(4, "USLT", usltPayload(3, "deu", "", "eins")),
			id3Frame(4, "USLT", usltPayload(3, "eng", "", "one"))), "eins", []string{"eins", "one"}},
		{"an empty frame is passed over", id3Tag(4,
			id3Frame(4, "USLT", usltPayload(3, "eng", "", "")),
			id3Frame(4, "USLT", usltPayload(3, "eng", "b", "one"))), "one", []string{"one"}},
		{"no terminator reads as text", id3Tag(4, id3Frame(4, "USLT", append([]byte{3}, "engla"...))), "la", []string{"la"}},
		{"too short", id3Tag(4, id3Frame(4, "USLT", []byte{3, 'e'})), "", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tags := parseBytes(t, file(tt.data)).Tags
			if tags.Lyrics != tt.want {
				t.Errorf("Lyrics = %q, want %q", tags.Lyrics, tt.want)
			}
			if got := tags.Get(frameIDUSLT); !slices.Equal(got, tt.raw) {
				t.Errorf("raw USLT = %q, want %q", got, tt.raw)
			}
		})
	}
}

func TestLyricsNeverFillOtherFields(t *testing.T) {
	tags := parseBytes(t, file(id3Tag(4, id3Frame(4, "USLT", usltPayload(3, "eng", "", "la"))))).Tags
	tags.Lyrics = ""
	want := types.Tags{}
	want.AddRaw(frameIDUSLT, "la")
	if !tags.Equal(&want) {
		t.Errorf("tags = %+v, want only the raw USLT", tags)
	}
}

func TestParseLyrics_CompressedIsAWarning(t *testing.T) {
	src := file(id3Tag(4, id3FrameFlags(4, "USLT", v24FrameCompression, usltPayload(3, "eng", "", "la"))))
	f := parseBytes(t, src)
	if f.Tags.Lyrics != "" {
		t.Errorf("Lyrics = %q", f.Tags.Lyrics)
	}
	if len(f.Warnings) != 1 || !strings.Contains(f.Warnings[0].Message, "USLT") {
		t.Errorf("warnings = %+v", f.Warnings)
	}
}

func TestApplyLyrics_NewFrame(t *testing.T) {
	for _, v := range []byte{3, 4} {
		src := file(id3Tag(v, id3Frame(v, "TIT2", latin1Text("T"))))
		out := writeBytes(t, src, editsFor(t, src, func(tg *types.Tags) { tg.Lyrics = "歌詞\nline" }))
		want := []langText{{lang: "XXX", text: "歌詞\nline"}}
		if got := usltFrames(t, out); !slices.Equal(got, want) {
			t.Errorf("v2.%d: frames %+v, want %+v", v, got, want)
		}
		if got := parseBytes(t, out).Tags.Lyrics; got != "歌詞\nline" {
			t.Errorf("v2.%d: Lyrics = %q", v, got)
		}
	}
}

func TestApplyLyrics_KeepsLanguageDescriptionAndOtherFrames(t *testing.T) {
	for _, v := range []byte{3, 4} {
		src := file(id3Tag(v,
			id3Frame(v, "USLT", usltPayload(0, "deu", "Fassung", "eins")),
			id3Frame(v, "TIT2", latin1Text("T")),
			id3Frame(v, "USLT", usltPayload(0, "eng", "", "one")),
		))
		out := writeBytes(t, src, editsFor(t, src, func(tg *types.Tags) { tg.Lyrics = "zwei" }))
		want := []langText{{"deu", "Fassung", "zwei"}, {"eng", "", "one"}}
		if got := usltFrames(t, out); !slices.Equal(got, want) {
			t.Errorf("v2.%d: frames %+v, want %+v", v, got, want)
		}
		if got := frameIDs(framesOf(t, out).frames); !slices.Equal(got, []string{"USLT", "TIT2", "USLT"}) {
			t.Errorf("v2.%d: frame order %v", v, got)
		}
		if got := parseBytes(t, out).Tags.Get(frameIDUSLT); !slices.Equal(got, []string{"zwei", "one"}) {
			t.Errorf("v2.%d: raw USLT = %q", v, got)
		}
	}
}

func TestApplyLyrics_TargetsTheFrameTheFieldCameFrom(t *testing.T) {
	src := file(id3Tag(4,
		id3Frame(4, "USLT", usltPayload(3, "deu", "", "")),
		id3Frame(4, "USLT", usltPayload(3, "eng", "d", "one")),
	))
	out := writeBytes(t, src, editsFor(t, src, func(tg *types.Tags) { tg.Lyrics = "two" }))
	want := []langText{{lang: "deu"}, {"eng", "d", "two"}}
	if got := usltFrames(t, out); !slices.Equal(got, want) {
		t.Errorf("frames %+v, want %+v", got, want)
	}
}

func TestApplyLyrics_ReusesAnEmptyFrame(t *testing.T) {
	src := file(id3Tag(4, id3Frame(4, "USLT", usltPayload(3, "deu", "d", ""))))
	out := writeBytes(t, src, editsFor(t, src, func(tg *types.Tags) { tg.Lyrics = "two" }))
	want := []langText{{"deu", "d", "two"}}
	if got := usltFrames(t, out); !slices.Equal(got, want) {
		t.Errorf("frames %+v, want %+v", got, want)
	}
}

func TestApplyLyrics_DescriptionPicksTheEncoding(t *testing.T) {
	src := file(id3Tag(3, id3Frame(3, "USLT", usltUTF16Payload("jpn", "歌", "old"))))
	out := writeBytes(t, src, editsFor(t, src, func(tg *types.Tags) { tg.Lyrics = "ascii" }))
	want := []langText{{"jpn", "歌", "ascii"}}
	if got := usltFrames(t, out); !slices.Equal(got, want) {
		t.Errorf("frames %+v, want %+v", got, want)
	}
}

func TestLyricsClearAndDeleteRemoveEveryFrame(t *testing.T) {
	src := file(id3Tag(3,
		id3Frame(3, "TIT2", latin1Text("T")),
		id3Frame(3, "USLT", usltPayload(0, "deu", "", "eins")),
		id3Frame(3, "USLT", usltPayload(0, "eng", "", "one")),
	))
	for name, mutate := range map[string]func(*types.Tags){
		"clear":  func(tg *types.Tags) { tg.Lyrics = "" },
		"delete": func(tg *types.Tags) { tg.Delete(frameIDUSLT) },
	} {
		t.Run(name, func(t *testing.T) {
			out := writeBytes(t, src, editsFor(t, src, mutate))
			if got := frameIDs(framesOf(t, out).frames); !slices.Equal(got, []string{"TIT2"}) {
				t.Errorf("frames %v", got)
			}
			if tags := parseBytes(t, out).Tags; tags.Lyrics != "" || tags.Get(frameIDUSLT) != nil {
				t.Errorf("Lyrics = %q, raw = %q", tags.Lyrics, tags.Get(frameIDUSLT))
			}
		})
	}
}

func TestRawUSLTSetIsRefused(t *testing.T) {
	src := file(id3Tag(4, id3Frame(4, "USLT", usltPayload(3, "eng", "", "one"))))
	_, err := tryWrite(src, editsFor(t, src, func(tg *types.Tags) { tg.Set(frameIDUSLT, "two") }))
	if !errors.Is(err, types.ErrUnsupportedWrite) {
		t.Errorf("err = %v, want ErrUnsupportedWrite", err)
	}
}

func TestApplyLyrics_RefusesInvalidText(t *testing.T) {
	src := file(id3Tag(4, id3Frame(4, "TIT2", latin1Text("T"))))
	for _, v := range []string{"a\x00b", "\xff"} {
		_, err := tryWrite(src, editsFor(t, src, func(tg *types.Tags) { tg.Lyrics = v }))
		if !errors.Is(err, types.ErrUnsupportedWrite) {
			t.Errorf("%q: err = %v, want ErrUnsupportedWrite", v, err)
		}
	}
}

func TestLyricsV22ConvertsAndKeepsLanguage(t *testing.T) {
	src := file(id3Tag(2, id3v22Frame("ULT", usltPayload(0, "deu", "d", "eins"))))
	out := writeBytes(t, src, editsFor(t, src, func(tg *types.Tags) { tg.Lyrics = "zwei" }))
	want := []langText{{"deu", "d", "zwei"}}
	if got := usltFrames(t, out); !slices.Equal(got, want) {
		t.Errorf("frames %+v, want %+v", got, want)
	}
}
