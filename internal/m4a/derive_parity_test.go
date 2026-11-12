package m4a

import (
	"reflect"
	"testing"

	"github.com/simonhull/audiometa/internal/types"
)

// TestFieldParity pins the typed field each combination of ilst items yields:
// every field with several keys in every order, repeated items and values, and
// year prefixes. A row marked "changed" is a read result that plan 089 moves
// to the one derivation rule (the first non-empty value of the
// highest-precedence decodable key); the CHANGELOG names it.
func TestFieldParity(t *testing.T) {
	t.Parallel()
	text := func(f func(*types.Tags) string) func(*types.Tags) any {
		return func(tg *types.Tags) any { return f(tg) }
	}
	title := text(func(tg *types.Tags) string { return tg.Title })
	date := text(func(tg *types.Tags) string { return tg.Date })
	desc := text(func(tg *types.Tags) string { return tg.Description })
	series := text(func(tg *types.Tags) string { return tg.Series })
	part := text(func(tg *types.Tags) string { return tg.SeriesPart })
	narrator := text(func(tg *types.Tags) string { return tg.Narrator })
	asin := text(func(tg *types.Tags) string { return tg.ASIN })
	lang := text(func(tg *types.Tags) string { return tg.Language })
	comment := text(func(tg *types.Tags) string { return tg.Comment })
	year := func(tg *types.Tags) any { return tg.Year }
	artists := func(tg *types.Tags) any { return tg.Artists }
	genres := func(tg *types.Tags) any { return tg.Genres }

	text1 := ilstText
	free := ilstFreeform

	tests := []struct {
		name  string
		items [][]byte
		got   func(*types.Tags) any
		want  any
	}{
		// Several keys for one field: the binding order wins whatever the atom order.
		{"ldes after desc", [][]byte{text1("desc", "d"), text1("ldes", "l")}, desc, "l"},
		{"ldes before desc", [][]byte{text1("ldes", "l"), text1("desc", "d")}, desc, "l"},
		{"desc before ©des", [][]byte{text1("\xA9des", "x"), text1("desc", "d")}, desc, "d"},
		{"freeform description is last", [][]byte{free("Description", "f"), text1("\xA9des", "x")}, desc, "x"},
		{"freeform description alone", [][]byte{free("Description", "f")}, desc, "f"},
		{"freeform series over ©mvn", [][]byte{text1("\xA9mvn", "M"), free("Series", "S")}, series, "S"},
		{"©mvn before freeform series", [][]byte{free("Series", "S"), text1("\xA9mvn", "M")}, series, "S"},
		{"©mvn over mvnm", [][]byte{free("MVNM", "V"), text1("\xA9mvn", "M")}, series, "M"},
		{"©mvi zero falls through", [][]byte{text1("\xA9mvi", "0"), free("MVIN", "3")}, part, "3"},
		{"©mvi first", [][]byte{free("MVIN", "3"), text1("\xA9mvi", "5")}, part, "5"},
		{"mvin over series part", [][]byte{free("Series Part", "7"), free("MVIN", "3")}, part, "3"},
		{"series part over series position", [][]byte{free("Series Position", "8"), free("Series Part", "7")}, part, "7"},
		{"asin before audible_asin", [][]byte{free("AUDIBLE_ASIN", "B"), free("ASIN", "A")}, asin, "A"},
		{"audible_asin alone", [][]byte{free("Audible_ASIN", "B")}, asin, "B"},
		{"language before lang", [][]byte{free("LANG", "fra"), free("Language", "eng")}, lang, "eng"},

		// Repeated items and values: the first value of the key.
		{"repeated ©nam", [][]byte{text1("\xA9nam", "A"), text1("\xA9nam", "B")}, title, "A"},
		{"two values in one ©nam", [][]byte{text1("\xA9nam", "A", "B")}, title, "A"},
		{"repeated ©cmt", [][]byte{text1("\xA9cmt", "A"), text1("\xA9cmt", "B")}, comment, "A"},
		{"repeated ©day", [][]byte{text1("\xA9day", "1999-05"), text1("\xA9day", "2001")}, date, "1999-05"},
		{"repeated narrator atoms", [][]byte{free("Narrator", "N1"), free("narrator", "N2")}, narrator, "N1"},
		{"two narrator values", [][]byte{free("Narrator", "N1", "N2")}, narrator, "N1"},
		{"artists of two items", [][]byte{text1("\xA9ART", "A", "B"), text1("\xA9ART", "C")}, artists, []string{"A", "B", "C"}},
		{"genres of two items", [][]byte{text1("\xA9gen", "Rock"), text1("\xA9gen", "Jazz")}, genres, []string{"Rock", "Jazz"}},

		// Years: four leading digits.
		{"year from date", [][]byte{text1("\xA9day", "2021-05-01")}, year, 2021},
		{"year only", [][]byte{text1("\xA9day", "1999")}, year, 1999},
		{"year before 1900", [][]byte{text1("\xA9day", "1850-01-01")}, year, 1850},
		{"year of letters", [][]byte{text1("\xA9day", "abcd")}, year, 0},
		{"year of two digits and letters", [][]byte{text1("\xA9day", "12ab-01-01")}, year, 0},
		{"year too short", [][]byte{text1("\xA9day", "199")}, year, 0},
		{"year zero", [][]byte{text1("\xA9day", "0000")}, year, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			file := parseBytes(t, ilstFile(tt.items...))
			types.BindFormat(&file.Tags, types.FormatM4B)
			if got := tt.got(&file.Tags); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %#v, want %#v", got, tt.want)
			}
		})
	}
}
