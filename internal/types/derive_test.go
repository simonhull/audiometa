package types

import (
	"slices"
	"strconv"
	"testing"
)

func TestDecodeYear_M4AFourDigits(t *testing.T) {
	t.Parallel()
	for _, format := range []Format{FormatM4A, FormatM4B} {
		for value, want := range map[string]int{"2021-05-01": 2021, "1850": 1850, "0099": 99, "12ab-01": 0, "199": 0, "": 0, "abcd": 0, "+199": 0} {
			var tags Tags
			if got := decodeYear(&tags, format, value); got != (want > 0) || tags.Year != want {
				t.Errorf("%s: decodeYear(%q) = %v with Year %d, want Year %d", format, value, got, tags.Year, want)
			}
		}
	}
}

func TestDeriveFields(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		format Format
		raw    map[string][]string
		before Tags
		field  Field
		want   []string
	}{
		{"empty first value is skipped", FormatFLAC, map[string][]string{"TITLE": {"", "B"}}, Tags{}, FieldTitle, []string{"B"}},
		{"all values empty leaves the field", FormatFLAC, map[string][]string{"TITLE": {"", ""}}, Tags{Title: "kept"}, FieldTitle, []string{"kept"}},
		{"slice keeps empties", FormatFLAC, map[string][]string{"ARTIST": {"", "B"}}, Tags{}, FieldArtists, []string{"", "B"}},
		{"slice takes one key", FormatFLAC, map[string][]string{"GENRE": {"a", "b"}}, Tags{}, FieldGenres, []string{"a", "b"}},
		{"empty value falls to the next key", FormatFLAC, map[string][]string{"SERIES": {""}, "MVNM": {"M"}}, Tags{}, FieldSeries, []string{"M"}},
		{"highest precedence key wins", FormatFLAC, map[string][]string{"ASIN": {"A"}, "AUDIBLE_ASIN": {"B"}}, Tags{}, FieldASIN, []string{"A"}},
		{"undecodable year falls to the next key", FormatMP3, map[string][]string{"TDRC": {"3000"}, "TYER": {"1999"}}, Tags{}, FieldYear, []string{"1999"}},
		{"tdrc wins over tyer", FormatMP3, map[string][]string{"TYER": {"1999"}, "TDRC": {"2021"}}, Tags{}, FieldYear, []string{"2021"}},
		{"no key leaves the field", FormatMP3, map[string][]string{"TDRC": {"3000"}}, Tags{Year: 1980}, FieldYear, []string{"1980"}},
		{"explicit total over number suffix", FormatFLAC, map[string][]string{"TRACKNUMBER": {"1/12"}, "TRACKTOTAL": {"14"}}, Tags{}, FieldTrackTotal, []string{"14"}},
		{"suffix total without explicit", FormatFLAC, map[string][]string{"TRACKNUMBER": {"1/12"}}, Tags{}, FieldTrackTotal, []string{"12"}},
		{"number is the first value", FormatFLAC, map[string][]string{"TRACKNUMBER": {"0", "3"}}, Tags{}, FieldTrackNumber, nil},
		{"m4a movement zero falls through", FormatM4A, map[string][]string{"\xA9mvi": {"0"}, "MVIN": {"3"}}, Tags{}, FieldSeriesPart, []string{"3"}},
		{"m4a year needs four digits", FormatM4A, map[string][]string{"\xA9day": {"12ab-01"}}, Tags{}, FieldYear, nil},
		{"wav id3 over native", FormatWAV, map[string][]string{"INAM": {"native"}, "TIT2": {"id3"}}, Tags{}, FieldTitle, []string{"id3"}},
		{"wav native alone", FormatWAV, map[string][]string{"INAM": {"native"}}, Tags{}, FieldTitle, []string{"native"}},
		{"unbound key fills nothing", FormatFLAC, map[string][]string{"TIT2": {"x"}}, Tags{}, FieldTitle, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tags := tc.before
			for k, v := range tc.raw {
				for _, s := range v {
					tags.AddRaw(k, s)
				}
			}
			DeriveFields(&tags, tc.format)
			if got := valuesOf(&tags, tc.field); !slices.Equal(got, tc.want) {
				t.Errorf("%s = %q, want %q", tc.field, got, tc.want)
			}
		})
	}
}

// DeriveFields is idempotent, and BindFormat leaves what it derived alone.
func TestDeriveFieldsIsStable(t *testing.T) {
	t.Parallel()
	for _, format := range bindingFormats {
		var tags Tags
		for _, b := range FieldBindings(format) {
			for i, k := range b.Keys {
				tags.AddRaw(k, "2000")
				tags.AddRaw(k, strconv.Itoa(i))
			}
		}
		derived := tags.Clone()
		DeriveFields(derived, format)
		again := derived.Clone()
		DeriveFields(again, format)
		bound := derived.Clone()
		BindFormat(bound, format)
		if !derived.Equal(again) || !derived.Equal(bound) {
			t.Errorf("%s: DeriveFields is not stable under repeat or BindFormat", format)
		}
	}
}
