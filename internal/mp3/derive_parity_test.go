package mp3

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/simonhull/audiometa/internal/types"
)

// TestFieldParity pins the typed field each combination of ID3v2 frames
// yields, for v2.3 and v2.4 tags: every field with several keys in every
// order, repeated frames, and empty values. A row marked "changed" is a read
// result that plan 089 moves to the one derivation rule (the first non-empty
// value of the highest-precedence decodable key); the CHANGELOG names it.
func TestFieldParity(t *testing.T) {
	t.Parallel()
	text := func(f func(*types.Tags) string) func(*types.Tags) any {
		return func(tg *types.Tags) any { return f(tg) }
	}
	title := text(func(tg *types.Tags) string { return tg.Title })
	artist := text(func(tg *types.Tags) string { return tg.Artist })
	grouping := text(func(tg *types.Tags) string { return tg.Grouping })
	series := text(func(tg *types.Tags) string { return tg.Series })
	part := text(func(tg *types.Tags) string { return tg.SeriesPart })
	narrator := text(func(tg *types.Tags) string { return tg.Narrator })
	publisher := text(func(tg *types.Tags) string { return tg.Publisher })
	isbn := text(func(tg *types.Tags) string { return tg.ISBN })
	asin := text(func(tg *types.Tags) string { return tg.ASIN })
	lang := text(func(tg *types.Tags) string { return tg.Language })
	desc := text(func(tg *types.Tags) string { return tg.Description })
	comment := text(func(tg *types.Tags) string { return tg.Comment })
	lyrics := text(func(tg *types.Tags) string { return tg.Lyrics })
	year := func(tg *types.Tags) any { return tg.Year }
	track := func(tg *types.Tags) any { return [2]int{tg.TrackNumber, tg.TrackTotal} }
	disc := func(tg *types.Tags) any { return [2]int{tg.DiscNumber, tg.DiscTotal} }
	artists := func(tg *types.Tags) any { return tg.Artists }
	genres := func(tg *types.Tags) any { return tg.Genres }

	txt := func(id, v string) func(byte) []byte {
		return func(version byte) []byte { return id3Frame(version, id, latin1Text(v)) }
	}
	xxx := func(desc, v string) func(byte) []byte {
		return func(version byte) []byte { return id3Frame(version, "TXXX", txxxPayload(0, desc, v)) }
	}
	comm := func(desc, v string) func(byte) []byte {
		return func(version byte) []byte { return id3Frame(version, "COMM", commPayload("eng", desc, v)) }
	}
	uslt := func(v string) func(byte) []byte {
		return func(version byte) []byte { return id3Frame(version, "USLT", commPayload("eng", "", v)) }
	}
	type frame = func(byte) []byte

	tests := []struct {
		name   string
		frames []frame
		got    func(*types.Tags) any
		want   any
	}{
		// Several keys for one field: the binding order wins whatever the file order.
		{"tdrc before tyer", []frame{txt("TDRC", "2021"), txt("TYER", "1999")}, year, 2021},
		{"tyer before tdrc", []frame{txt("TYER", "1999"), txt("TDRC", "2021")}, year, 2021},
		{"invalid tdrc falls to tyer", []frame{txt("TDRC", "3000"), txt("TYER", "1999")}, year, 1999},
		{"tyer alone", []frame{txt("TYER", "1999")}, year, 1999},
		{"grp1 before tit1", []frame{txt("GRP1", "G"), txt("TIT1", "T")}, grouping, "G"},
		{"tit1 before grp1", []frame{txt("TIT1", "T"), txt("GRP1", "G")}, grouping, "G"},
		{"grouping txxx before tit1", []frame{xxx("GROUPING", "X"), txt("TIT1", "T")}, grouping, "T"},
		{"tit1 before grouping txxx", []frame{txt("TIT1", "T"), xxx("GROUPING", "X")}, grouping, "T"},
		{"series txxx before mvnm", []frame{xxx("SERIES", "S"), txt("MVNM", "M")}, series, "S"},
		{"mvnm before series txxx", []frame{txt("MVNM", "M"), xxx("SERIES", "S")}, series, "S"},
		{"mvnm before movement", []frame{xxx("MOVEMENT", "V"), txt("MVNM", "M")}, series, "M"},
		{"series part before mvin", []frame{txt("MVIN", "2/9"), xxx("SERIES PART", "1")}, part, "1"},
		{"mvin count is cut", []frame{txt("MVIN", "2/9")}, part, "2"},
		{"asin before audible_asin", []frame{xxx("AUDIBLE_ASIN", "B"), xxx("ASIN", "A")}, asin, "A"},
		{"language before lang", []frame{xxx("LANG", "fra"), xxx("LANGUAGE", "eng")}, lang, "eng"},

		// Repeated frames: the first value of the key.
		{"repeated tit2", []frame{txt("TIT2", "A"), txt("TIT2", "B")}, title, "A"},
		{"repeated tpe1", []frame{txt("TPE1", "A"), txt("TPE1", "B")}, artist, "A"},
		{"artists of repeated tpe1", []frame{txt("TPE1", "A"), txt("TPE1", "B")}, artists, []string{"A", "B"}},
		{"genres of repeated tcon", []frame{txt("TCON", "Rock"), txt("TCON", "Jazz")}, genres, []string{"Rock", "Jazz"}},
		{"repeated series txxx", []frame{xxx("SERIES", "S1"), xxx("SERIES", "S2")}, series, "S1"},
		{"repeated series part txxx", []frame{xxx("SERIES PART", "1"), xxx("SERIES PART", "2")}, part, "1"},
		{"repeated asin txxx", []frame{xxx("ASIN", "A1"), xxx("ASIN", "A2")}, asin, "A1"},
		{"repeated language txxx", []frame{xxx("LANGUAGE", "eng"), xxx("LANGUAGE", "fra")}, lang, "eng"},
		{"repeated description txxx", []frame{xxx("DESCRIPTION", "D1"), xxx("DESCRIPTION", "D2")}, desc, "D1"},
		{"empty description then value", []frame{xxx("DESCRIPTION", ""), xxx("DESCRIPTION", "D2")}, desc, "D2"},
		{"repeated trck", []frame{txt("TRCK", "0/12"), txt("TRCK", "3/10")}, track, [2]int{0, 12}},
		{"trck then tpos", []frame{txt("TRCK", "3/10"), txt("TPOS", "1/2")}, disc, [2]int{1, 2}},
		{"first comm without description", []frame{comm("", "C1"), comm("", "C2")}, comment, "C1"},
		{"described comm only", []frame{comm("d", "C1"), comm("e", "C2")}, comment, "C1"},
		{"comm without description wins", []frame{comm("d", "C1"), comm("", "C2")}, comment, "C2"},
		{"itunes comm ignored", []frame{comm("iTunNORM", "N")}, comment, ""},
		{"first uslt", []frame{uslt("L1"), uslt("L2")}, lyrics, "L1"},

		// Moved by plan 089: see the CHANGELOG entry for each.
		// changed: repeated single-key TXXX frames read the first frame, not the last.
		{"repeated narrator txxx", []frame{xxx("NARRATOR", "N1"), xxx("NARRATOR", "N2")}, narrator, "N1"},
		{"repeated publisher txxx", []frame{xxx("PUBLISHER", "P1"), xxx("PUBLISHER", "P2")}, publisher, "P1"},
		{"repeated isbn txxx", []frame{xxx("ISBN", "I1"), xxx("ISBN", "I2")}, isbn, "I1"},
		// changed: a repeated TRCK reads its first frame, not the first with a number.
		{"repeated trck, first unparsable", []frame{txt("TRCK", "x"), txt("TRCK", "3/10")}, track, [2]int{0, 0}},
		// changed: an empty TXXX value of a higher-precedence key falls through to the next key.
		{"empty series txxx falls to mvnm", []frame{xxx("SERIES", ""), txt("MVNM", "M")}, series, "M"},
	}
	for _, tt := range tests {
		for _, version := range []byte{3, 4} {
			t.Run(fmt.Sprintf("v2.%d/%s", version, tt.name), func(t *testing.T) {
				t.Parallel()
				frames := make([][]byte, len(tt.frames))
				for i, f := range tt.frames {
					frames[i] = f(version)
				}
				data := append(id3Tag(version, frames...), mpegFrame...)
				f := parseBytes(t, data)
				types.BindFormat(&f.Tags, types.FormatMP3)
				if got := tt.got(&f.Tags); !reflect.DeepEqual(got, tt.want) {
					t.Errorf("v2.%d: got %#v, want %#v", version, got, tt.want)
				}
			})
		}
	}
}
