package vorbis

import (
	"reflect"
	"testing"

	"github.com/simonhull/audiometa/internal/types"
)

// parseAll runs comments through Apply and binds the format, as the root
// package does after a parser returns.
func parseAll(t *testing.T, format types.Format, comments ...string) *types.Tags {
	t.Helper()
	entries := make([][]byte, len(comments))
	for i, c := range comments {
		entries[i] = []byte(c)
	}
	var file types.File
	Apply(entries, &file, format)
	types.BindFormat(&file.Tags, format)
	return &file.Tags
}

// TestFieldParity pins the typed field each combination of Vorbis comments
// yields: every field with several keys in every order, repeated keys, and
// empty values. A row marked "changed" is a read result that plan 089 moves to
// the one derivation rule (the first non-empty value of the highest-precedence
// decodable key); the CHANGELOG names it.
func TestFieldParity(t *testing.T) {
	t.Parallel()
	text := func(f func(*types.Tags) string) func(*types.Tags) any {
		return func(tg *types.Tags) any { return f(tg) }
	}
	title := text(func(tg *types.Tags) string { return tg.Title })
	series := text(func(tg *types.Tags) string { return tg.Series })
	part := text(func(tg *types.Tags) string { return tg.SeriesPart })
	asin := text(func(tg *types.Tags) string { return tg.ASIN })
	lang := text(func(tg *types.Tags) string { return tg.Language })
	desc := text(func(tg *types.Tags) string { return tg.Description })
	date := text(func(tg *types.Tags) string { return tg.Date })
	artist := text(func(tg *types.Tags) string { return tg.Artist })
	comment := text(func(tg *types.Tags) string { return tg.Comment })
	ints := func(f func(*types.Tags) (int, int)) func(*types.Tags) any {
		return func(tg *types.Tags) any { a, b := f(tg); return [2]int{a, b} }
	}
	track := ints(func(tg *types.Tags) (int, int) { return tg.TrackNumber, tg.TrackTotal })
	disc := ints(func(tg *types.Tags) (int, int) { return tg.DiscNumber, tg.DiscTotal })
	year := func(tg *types.Tags) any { return tg.Year }
	artists := func(tg *types.Tags) any { return tg.Artists }
	genres := func(tg *types.Tags) any { return tg.Genres }

	tests := []struct {
		name     string
		comments []string
		got      func(*types.Tags) any
		want     any
	}{
		// Several keys for one field: the binding order wins whatever the file order.
		{"total suffix before explicit", []string{"TRACKNUMBER=1/12", "TRACKTOTAL=14"}, track, [2]int{1, 14}},
		{"explicit before total suffix", []string{"TRACKTOTAL=14", "TRACKNUMBER=1/12"}, track, [2]int{1, 14}},
		{"totaltracks before tracktotal", []string{"TOTALTRACKS=9", "TRACKTOTAL=14"}, track, [2]int{0, 14}},
		{"disc suffix before explicit", []string{"DISCNUMBER=1/2", "DISCTOTAL=3"}, disc, [2]int{1, 3}},
		{"series before mvnm", []string{"SERIES=S", "MVNM=M"}, series, "S"},
		{"mvnm before series", []string{"MVNM=M", "SERIES=S"}, series, "S"},
		{"seriespart before mvin", []string{"SERIESPART=1", "MVIN=2"}, part, "1"},
		{"mvin before seriespart", []string{"MVIN=2", "SERIESPART=1"}, part, "1"},
		{"series_part before series part", []string{"SERIES PART=4", "SERIES_PART=3"}, part, "3"},
		{"asin before audible_asin", []string{"ASIN=A", "AUDIBLE_ASIN=B"}, asin, "A"},
		{"audible_asin before asin", []string{"AUDIBLE_ASIN=B", "ASIN=A"}, asin, "A"},
		{"language before lang", []string{"LANGUAGE=eng", "LANG=fra"}, lang, "eng"},
		{"lang before language", []string{"LANG=fra", "LANGUAGE=eng"}, lang, "eng"},
		{"lowercase keys alias", []string{"lang=fra", "Language=eng"}, lang, "eng"},

		// Repeated keys: the first non-empty value of the key.
		{"repeated title", []string{"TITLE=A", "TITLE=B"}, title, "A"},
		{"empty first title", []string{"TITLE=", "TITLE=B"}, title, "B"},
		{"empty first artist", []string{"ARTIST=", "ARTIST=B"}, artist, "B"},
		{"artists keep empties", []string{"ARTIST=", "ARTIST=B"}, artists, []string{"", "B"}},
		{"artists across cases", []string{"artist=A", "ARTIST=B"}, artists, []string{"A", "B"}},
		{"genres", []string{"GENRE=Rock", "genre=Jazz"}, genres, []string{"Rock", "Jazz"}},
		{"empty first comment", []string{"COMMENT=", "COMMENT=c"}, comment, "c"},
		{"empty first description", []string{"DESCRIPTION=", "DESCRIPTION=d"}, desc, "d"},
		{"empty first date", []string{"DATE=", "DATE=1999-05"}, date, "1999-05"},
		{"empty first date year", []string{"DATE=", "DATE=1999-05"}, year, 1999},
		{"first date year", []string{"DATE=1999-05", "DATE=2001"}, year, 1999},
		{"unparsable first date", []string{"DATE=abcd", "DATE=2001"}, year, 0},
		{"track number repeated", []string{"TRACKNUMBER=3/12", "TRACKNUMBER=4/20"}, track, [2]int{3, 12}},

		// Moved by plan 089: see the CHANGELOG entry for each.
		// changed: a repeated number key reads its first value, not the first non-zero one.
		{"track leading zero", []string{"TRACKNUMBER=0", "TRACKNUMBER=3"}, track, [2]int{0, 0}},
		{"track unparsable first", []string{"TRACKNUMBER=x", "TRACKNUMBER=3"}, track, [2]int{0, 0}},
		{"disc leading zero", []string{"DISCNUMBER=0", "DISCNUMBER=2"}, disc, [2]int{0, 0}},
		// changed: an empty value of a higher-precedence key falls through to the next key.
		{"empty series falls to mvnm", []string{"SERIES=", "MVNM=M"}, series, "M"},
		{"empty asin falls to audible_asin", []string{"ASIN=", "AUDIBLE_ASIN=B"}, asin, "B"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := tt.got(parseAll(t, types.FormatFLAC, tt.comments...))
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %#v, want %#v", got, tt.want)
			}
		})
	}
}
