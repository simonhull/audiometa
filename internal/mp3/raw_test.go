package mp3

import (
	"slices"
	"strings"
	"testing"

	"github.com/simonhull/audiometa/internal/types"
)

func TestParseTextFrameRecordsRawAndArtists(t *testing.T) {
	var f types.File
	parseTextFrame(id3v2Frame{ID: "TPE1", Data: append([]byte{3}, "A\x00B"...)}, &f)
	if got := f.Tags.Get("TPE1"); !slices.Equal(got, []string{"A", "B"}) {
		t.Errorf("raw TPE1 = %q", got)
	}
	if !slices.Equal(f.Tags.Artists, []string{"A", "B"}) || f.Tags.Artist != "A" {
		t.Errorf("Artist=%q Artists=%q", f.Tags.Artist, f.Tags.Artists)
	}
}

func TestParseTXXXRecordsUppercasedDescription(t *testing.T) {
	var f types.File
	parseTXXXFrame(id3v2Frame{ID: "TXXX", Data: append([]byte{3}, "Mood\x00Calm"...)}, &f)
	if got := f.Tags.Get("MOOD"); !slices.Equal(got, []string{"Calm"}) {
		t.Errorf("MOOD = %q", got)
	}
}

func TestParseTXXXNonSeriesTagsStayRaw(t *testing.T) {
	for _, desc := range []string{"show", "episode_id", "part"} {
		t.Run(desc, func(t *testing.T) {
			var f types.File
			parseTXXXFrame(id3v2Frame{ID: "TXXX", Data: append([]byte{3}, desc+"\x00Foo"...)}, &f)
			if f.Tags.Series != "" || f.Tags.SeriesPart != "" {
				t.Errorf("Series=%q SeriesPart=%q, want both empty", f.Tags.Series, f.Tags.SeriesPart)
			}
			if got := f.Tags.Get(strings.ToUpper(desc)); !slices.Equal(got, []string{"Foo"}) {
				t.Errorf("raw %s = %q", desc, got)
			}
		})
	}
}

func TestParseTXXXReadsEveryValue(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want []string
	}{
		{"UTF-8", append([]byte{3}, "Artists\x00A\x00B\x00C"...), []string{"A", "B", "C"}},
		{"Latin-1", append([]byte{0}, "Artists\x00A\x00B"...), []string{"A", "B"}},
		{"UTF-16, a BOM per value", slices.Concat([]byte{1}, utf16Text("Artists")[1:], []byte{0, 0}, utf16Text("A")[1:], []byte{0, 0}, utf16Text("B")[1:]), []string{"A", "B"}},
		{"trailing terminator", append([]byte{3}, "Artists\x00A\x00"...), []string{"A"}},
		{"empty values dropped", append([]byte{3}, "Artists\x00A\x00\x00B"...), []string{"A", "B"}},
		{"no value keeps the key", append([]byte{3}, "Artists\x00"...), []string{""}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var f types.File
			parseTXXXFrame(id3v2Frame{ID: "TXXX", Data: tt.data}, &f)
			if got := f.Tags.Get("ARTISTS"); !slices.Equal(got, tt.want) {
				t.Errorf("ARTISTS = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseTXXXFirstValueFillsTheField(t *testing.T) {
	var f types.File
	parseTXXXFrame(id3v2Frame{ID: "TXXX", Data: append([]byte{3}, "Narrator\x00A\x00B"...)}, &f)
	if f.Tags.Narrator != "A" {
		t.Errorf("Narrator = %q, want A", f.Tags.Narrator)
	}
	if got := f.Tags.Get("NARRATOR"); !slices.Equal(got, []string{"A", "B"}) {
		t.Errorf("raw = %q", got)
	}
}

func TestTXXXMultiValueWriteRoundTrip(t *testing.T) {
	for _, v := range []byte{3, 4} {
		frame := id3Frame(v, "TXXX", txxxPayload(0, "Mood", "A\x00B"))
		src := file(id3Tag(v, id3Frame(v, "TIT2", latin1Text("T")), frame))

		out := writeBytes(t, src, editsFor(t, src, func(tg *types.Tags) { tg.Title = "New" }))
		if got := parseBytes(t, out).Tags.Get("MOOD"); !slices.Equal(got, []string{"A", "B"}) {
			t.Errorf("v2.%d: an untouched key reads %q", v, got)
		}

		out = writeBytes(t, src, editsFor(t, src, func(tg *types.Tags) { tg.Set("MOOD", "B", "Ç", "歌") }))
		if got := parseBytes(t, out).Tags.Get("MOOD"); !slices.Equal(got, []string{"B", "Ç", "歌"}) {
			t.Errorf("v2.%d: a rewritten key reads %q", v, got)
		}
	}
}
