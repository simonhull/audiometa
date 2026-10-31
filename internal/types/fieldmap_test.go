package types

import "testing"

var bindingFormats = []Format{FormatMP3, FormatFLAC, FormatOgg, FormatOpus, FormatM4A, FormatM4B}

func TestFieldBindings_KeysAreNonEmpty(t *testing.T) {
	for _, format := range bindingFormats {
		bindings := FieldBindings(format)
		if len(bindings) == 0 {
			t.Errorf("%s: no bindings", format)
		}
		seen := map[Field]bool{}
		for _, b := range bindings {
			if b.Field <= 0 || b.Field >= fieldCount {
				t.Errorf("%s: invalid field %d", format, b.Field)
			}
			if seen[b.Field] {
				t.Errorf("%s: %s bound twice", format, b.Field)
			}
			seen[b.Field] = true
			if len(b.Keys) == 0 {
				t.Errorf("%s: %s has no keys", format, b.Field)
			}
			for _, k := range b.Keys {
				if k == "" {
					t.Errorf("%s: %s has an empty key", format, b.Field)
				}
			}
		}
	}
	for _, format := range []Format{FormatUnknown, FormatWAV, FormatAIFF} {
		if b := FieldBindings(format); b != nil {
			t.Errorf("%s: bindings = %v, want nil", format, b)
		}
	}
}

func TestFieldBindings_ReturnsCopy(t *testing.T) {
	first := FieldBindings(FormatMP3)
	first[0].Keys[0] = "MUTATED"
	first[0].Field = FieldLyrics
	second := FieldBindings(FormatMP3)
	if second[0].Field != FieldTitle || second[0].Keys[0] != "TIT2" {
		t.Errorf("mutating the result changed shared state: %+v", second[0])
	}
}

func TestFieldBindings_NoCrossFieldKeys(t *testing.T) {
	composerKeys := map[string]bool{"TCOM": true, "COMPOSER": true, "\xA9wrt": true}
	groupingKeys := map[string]bool{"GRP1": true, "TIT1": true, "GROUPING": true, "\xA9grp": true}
	for _, format := range bindingFormats {
		for _, b := range FieldBindings(format) {
			for _, k := range b.Keys {
				if composerKeys[k] && b.Field != FieldComposers {
					t.Errorf("%s: composer key %q bound to %s", format, k, b.Field)
				}
				if groupingKeys[k] && b.Field != FieldGrouping {
					t.Errorf("%s: grouping key %q bound to %s", format, k, b.Field)
				}
			}
		}
	}
}

func TestDecodeField(t *testing.T) {
	tests := []struct {
		name   string
		format Format
		field  Field
		key    string
		values []string
		ok     bool
		want   []string
	}{
		{"mp3 year", FormatMP3, FieldYear, "TDRC", []string{"2021-05-03"}, true, []string{"2021"}},
		{"mp3 year out of range", FormatMP3, FieldYear, "TDRC", []string{"0042"}, false, nil},
		{"vorbis year", FormatFLAC, FieldYear, "DATE", []string{"0042"}, true, []string{"42"}},
		{"trck number", FormatMP3, FieldTrackNumber, "TRCK", []string{"3/12"}, true, []string{"3"}},
		{"trck total", FormatMP3, FieldTrackTotal, "TRCK", []string{"3/12"}, true, []string{"12"}},
		{"trck no total", FormatMP3, FieldTrackTotal, "TRCK", []string{"3"}, true, nil},
		{"vorbis total", FormatFLAC, FieldTrackTotal, "TRACKTOTAL", []string{"9"}, true, []string{"9"}},
		{"m4a mvi zero", FormatM4A, FieldSeriesPart, "\xA9mvi", []string{"0"}, false, nil},
		{"mp3 mvin cut", FormatMP3, FieldSeriesPart, "MVIN", []string{"2/7"}, true, []string{"2"}},
		{"vorbis mvin kept", FormatFLAC, FieldSeriesPart, "MVIN", []string{"2/7"}, true, []string{"2/7"}},
		{"slice", FormatFLAC, FieldGenres, "GENRE", []string{"a", "b"}, true, []string{"a", "b"}},
		{"empty", FormatFLAC, FieldTitle, "TITLE", nil, false, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tags := &Tags{}
			if ok := decodeField(tags, tc.format, tc.field, tc.key, tc.values); ok != tc.ok {
				t.Fatalf("ok = %v, want %v", ok, tc.ok)
			}
			got := fieldValues(tags, tc.field)
			if len(got) != len(tc.want) || (len(got) > 0 && got[0] != tc.want[0]) {
				t.Errorf("field = %v, want %v", got, tc.want)
			}
		})
	}
}
