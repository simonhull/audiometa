package types

import (
	"slices"
	"testing"
)

func TestYearValue(t *testing.T) {
	t.Parallel()
	tests := []struct {
		year int
		old  string
		want string
	}{
		{1999, "", "1999"},
		{1999, "2021-05-03", "1999-05-03"},
		{7, "x", "0007"},
		{1999, "20a1-05", "1999"},
		{1999, "2021", "1999"},
	}
	for _, tt := range tests {
		if got := YearValue(tt.year, tt.old); got != tt.want {
			t.Errorf("YearValue(%d, %q) = %q, want %q", tt.year, tt.old, got, tt.want)
		}
	}
}

// planEdits builds an EditSet whose Orig holds raw (key, value) pairs.
func planEdits(keys []KeyEdit, raw ...string) *EditSet {
	orig := &Tags{}
	for i := 0; i < len(raw); i += 2 {
		orig.AddRaw(raw[i], raw[i+1])
	}
	return &EditSet{Keys: keys, Orig: orig, Final: &Tags{}}
}

func TestFieldTargets(t *testing.T) {
	t.Parallel()
	lang := FieldEdit{Field: FieldLanguage, Values: []string{"fre"}}
	tests := []struct {
		name    string
		format  Format
		field   Field
		edits   *EditSet
		setting bool
		want    []string
	}{
		{"none present, setting", FormatFLAC, FieldLanguage, planEdits(nil), true, []string{"LANGUAGE"}},
		{"lower key present, setting", FormatFLAC, FieldLanguage, planEdits(nil, "LANG", "eng"), true, []string{"LANG"}},
		{"both present, setting", FormatFLAC, FieldLanguage, planEdits(nil, "LANG", "a", "LANGUAGE", "b"), true, []string{"LANGUAGE"}},
		{"both present, clearing", FormatFLAC, FieldLanguage, planEdits(nil, "LANG", "a", "LANGUAGE", "b"), false, []string{"LANGUAGE", "LANG"}},
		{"none present, clearing", FormatFLAC, FieldLanguage, planEdits(nil), false, nil},
		{"MP3 Year, TYER present", FormatMP3, FieldYear, planEdits(nil, "TYER", "1999"), true, []string{"TYER"}},
		{"MP3 Year, none present", FormatMP3, FieldYear, planEdits(nil), true, []string{"TDRC"}},
		{
			"key edit deletes the only key, setting",
			FormatFLAC, FieldLanguage, planEdits([]KeyEdit{{Key: "LANGUAGE"}}, "LANGUAGE", "eng"), true,
			[]string{"LANGUAGE"},
		},
		{
			"key edit deletes the only key, clearing",
			FormatFLAC, FieldLanguage, planEdits([]KeyEdit{{Key: "LANGUAGE"}}, "LANGUAGE", "eng"), false, nil,
		},
		{
			"key edit adds LANG, setting",
			FormatFLAC, FieldLanguage, planEdits([]KeyEdit{{Key: "LANG", Values: []string{"fre"}}}), true,
			[]string{"LANG"},
		},
		{
			"key edit adds LANG, clearing",
			FormatFLAC, FieldLanguage, planEdits([]KeyEdit{{Key: "LANG", Values: []string{"fre"}}}, "LANGUAGE", "eng"), false,
			[]string{"LANGUAGE", "LANG"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			canonical := ""
			for _, b := range bindingsFor(tt.format) {
				if b.Field == tt.field {
					canonical = b.Keys[0]
					break
				}
			}
			e := FieldEdit{Field: tt.field, Values: lang.Values}
			if got := FieldTargets(tt.format, e, tt.edits, canonical, tt.setting); !slices.Equal(got, tt.want) {
				t.Errorf("FieldTargets = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFieldValues(t *testing.T) {
	t.Parallel()
	artists := planEdits(nil)
	artists.Orig.Artists = []string{"A", "B", "C"}
	artists.Final.Artists = []string{"X", "Y"}
	artists.Final.Artist = "Z"

	tests := []struct {
		name  string
		e     FieldEdit
		key   string
		edits *EditSet
		want  []string
	}{
		{
			"year keeps the rest of the date",
			FieldEdit{Field: FieldYear, Values: []string{"1999"}},
			"DATE",
			planEdits(nil, "DATE", "2021-05-03"),
			[]string{"1999-05-03"},
		},
		{
			"year reads the date after key edits",
			FieldEdit{Field: FieldYear, Values: []string{"1999"}},
			"DATE",
			planEdits([]KeyEdit{{Key: "DATE", Values: []string{"2030-01-02"}}}, "DATE", "2021-05-03"),
			[]string{"1999-01-02"},
		},
		{"year without an old date", FieldEdit{Field: FieldYear, Values: []string{"1999"}}, "DATE", planEdits(nil), []string{"1999"}},
		{"invalid year", FieldEdit{Field: FieldYear, Values: []string{"x"}}, "DATE", planEdits(nil), nil},
		{"zero year", FieldEdit{Field: FieldYear, Values: []string{"0"}}, "DATE", planEdits(nil), nil},
		{"cleared year", FieldEdit{Field: FieldYear}, "DATE", planEdits(nil), nil},
		{"artists", FieldEdit{Field: FieldArtists, Values: []string{"ignored"}}, "ARTIST", artists, []string{"X", "Y"}},
		{"artist keeps the other original artists", FieldEdit{Field: FieldArtist, Values: []string{"Z"}}, "ARTIST", artists, []string{"Z", "B", "C"}},
		{"cleared artist", FieldEdit{Field: FieldArtist}, "ARTIST", planEdits(nil), nil},
		{"other fields", FieldEdit{Field: FieldTitle, Values: []string{"T"}}, "TITLE", planEdits(nil), []string{"T"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := FieldValues(tt.e, tt.key, tt.edits); !slices.Equal(got, tt.want) {
				t.Errorf("FieldValues = %q, want %q", got, tt.want)
			}
		})
	}
	// The values are a copy: editing them leaves the edit alone.
	e := FieldEdit{Field: FieldTitle, Values: []string{"T"}}
	FieldValues(e, "TITLE", planEdits(nil))[0] = "changed"
	if e.Values[0] != "T" {
		t.Error("FieldValues aliases the edit's values")
	}
}
