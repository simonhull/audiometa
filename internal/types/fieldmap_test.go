package types

import (
	"slices"
	"testing"
)

// ID3 formats keep only years 1900-2100; internal/mp3's validID3Year (which
// the MP3 writer uses to refuse others) has the same bounds and is tested
// there.
func TestDecodeYear_ID3Bounds(t *testing.T) {
	t.Parallel()
	for _, format := range []Format{FormatMP3, FormatWAV, FormatAIFF} {
		for year, want := range map[string]bool{"1899": false, "1900": true, "2100": true, "2101": false} {
			var tags Tags
			if got := decodeYear(&tags, format, year); got != want {
				t.Errorf("%s: decodeYear(%s) = %v, want %v", format, year, got, want)
			}
		}
	}
}

var bindingFormats = []Format{FormatMP3, FormatFLAC, FormatOgg, FormatOpus, FormatM4A, FormatM4B, FormatWAV, FormatAIFF}

func TestFieldBindings_KeysAreNonEmpty(t *testing.T) {
	t.Parallel()
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
	if b := FieldBindings(FormatUnknown); b != nil {
		t.Errorf("Unknown: bindings = %v, want nil", b)
	}
}

// WAV and AIFF read the ID3v2 chunk as MP3 does, and their native text after it:
// every MP3 key keeps its place, and the native key comes last.
func TestFieldBindings_NativeTextAfterID3(t *testing.T) {
	t.Parallel()
	native := map[Format]map[Field]string{
		FormatWAV:  {FieldTitle: "INAM", FieldArtist: "IART", FieldArtists: "IART", FieldAlbum: "IPRD", FieldGenres: "IGNR", FieldYear: "ICRD", FieldCopyright: "ICOP"},
		FormatAIFF: {FieldTitle: "NAME", FieldArtist: "AUTH", FieldArtists: "AUTH", FieldCopyright: "(c) "},
	}
	mp3 := FieldBindings(FormatMP3)
	for format, want := range native {
		got := FieldBindings(format)
		if len(got) != len(mp3) {
			t.Fatalf("%s: %d bindings, want MP3's %d", format, len(got), len(mp3))
		}
		for i, b := range got {
			if b.Field != mp3[i].Field || !slices.Equal(b.Keys[:len(mp3[i].Keys)], mp3[i].Keys) {
				t.Errorf("%s: %s keys %v do not start with MP3's %v", format, b.Field, b.Keys, mp3[i].Keys)
			}
			if key, ok := want[b.Field]; ok && b.Keys[len(b.Keys)-1] != key {
				t.Errorf("%s: %s keys %v, want %q last", format, b.Field, b.Keys, key)
			}
		}
	}
	i := slices.IndexFunc(FieldBindings(FormatWAV), func(b FieldBinding) bool { return b.Field == FieldTrackNumber })
	if track := FieldBindings(FormatWAV)[i]; !slices.Equal(track.Keys, []string{"TRCK", "ITRK", "IPRT"}) {
		t.Errorf("WAV track binding = %v", track)
	}
}

func TestFieldBindings_ReturnsCopy(t *testing.T) {
	t.Parallel()
	first := FieldBindings(FormatMP3)
	first[0].Keys[0] = "MUTATED"
	first[0].Field = FieldLyrics
	second := FieldBindings(FormatMP3)
	if second[0].Field != FieldTitle || second[0].Keys[0] != "TIT2" {
		t.Errorf("mutating the result changed shared state: %+v", second[0])
	}
}

func TestFieldBindings_NoCrossFieldKeys(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
		{"trck no total", FormatMP3, FieldTrackTotal, "TRCK", []string{"3"}, false, nil},
		{"vorbis number with total", FormatFLAC, FieldTrackNumber, "TRACKNUMBER", []string{"3/12"}, true, []string{"3"}},
		{"vorbis total from number", FormatFLAC, FieldTrackTotal, "TRACKNUMBER", []string{"3/12"}, true, []string{"12"}},
		{"vorbis number without total", FormatFLAC, FieldTrackTotal, "TRACKNUMBER", []string{"3"}, false, nil},
		{"vorbis disc total from number", FormatOpus, FieldDiscTotal, "DISCNUMBER", []string{"1/2"}, true, []string{"2"}},
		{"vorbis total", FormatFLAC, FieldTrackTotal, "TRACKTOTAL", []string{"9"}, true, []string{"9"}},
		{"m4a mvi zero", FormatM4A, FieldSeriesPart, "\xA9mvi", []string{"0"}, false, nil},
		{"mp3 mvin cut", FormatMP3, FieldSeriesPart, "MVIN", []string{"2/7"}, true, []string{"2"}},
		{"vorbis mvin kept", FormatFLAC, FieldSeriesPart, "MVIN", []string{"2/7"}, true, []string{"2/7"}},
		{"slice", FormatFLAC, FieldGenres, "GENRE", []string{"a", "b"}, true, []string{"a", "b"}},
		{"empty", FormatFLAC, FieldTitle, "TITLE", nil, false, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tags := &Tags{}
			if ok := decodeField(tags, tc.format, tc.field, tc.key, tc.values); ok != tc.ok {
				t.Fatalf("ok = %v, want %v", ok, tc.ok)
			}
			got := valuesOf(tags, tc.field)
			if len(got) != len(tc.want) || (len(got) > 0 && got[0] != tc.want[0]) {
				t.Errorf("field = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestNativeTextBindings(t *testing.T) {
	t.Parallel()
	mp3 := FieldBindings(FormatMP3)
	for _, format := range []Format{FormatWAV, FormatAIFF} {
		all := FieldBindings(format)
		native := NativeTextBindings(format)
		if len(native) == 0 {
			t.Fatalf("%s: no native text bindings", format)
		}
		for _, n := range native {
			i := slices.IndexFunc(mp3, func(b FieldBinding) bool { return b.Field == n.Field })
			if i < 0 {
				t.Errorf("%s: native %s has no MP3 binding to extend", format, n.Field)
				continue
			}
			if keys := all[i].Keys; !slices.Equal(keys[len(keys)-len(n.Keys):], n.Keys) {
				t.Errorf("%s: %s keys %v do not end with the native %v", format, n.Field, keys, n.Keys)
			}
		}
		native[0].Keys[0] = "MUTATED"
		if NativeTextBindings(format)[0].Keys[0] == "MUTATED" {
			t.Errorf("%s: mutating the result changed shared state", format)
		}
	}
	if b := NativeTextBindings(FormatMP3); b != nil {
		t.Errorf("MP3: native text bindings = %v, want nil", b)
	}
}

// keysOf returns the keys format binds to field, or nil when it binds none.
func keysOf(format Format, field Field) []string {
	for _, b := range FieldBindings(format) {
		if b.Field == field {
			return b.Keys
		}
	}
	return nil
}

func TestFieldBindings_ReleaseFields(t *testing.T) {
	t.Parallel()
	id3 := map[Field][]string{
		FieldMusicBrainzAlbumID:  {"MUSICBRAINZ ALBUM ID"},
		FieldMusicBrainzArtistID: {"MUSICBRAINZ ARTIST ID"},
		FieldMusicBrainzTrackID:  {"UFID:http://musicbrainz.org"},
		FieldISRC:                {"TSRC"},
		FieldBarcode:             {"BARCODE"},
		FieldCatalogNumber:       {"CATALOGNUMBER"},
		FieldCopyright:           {"TCOP"},
		FieldOriginalDate:        {"TDOR", "TORY"},
		FieldPublisher:           {"PUBLISHER", "TPUB"},
	}
	m4a := map[Field][]string{
		FieldMusicBrainzTrackID:  {"MUSICBRAINZ TRACK ID"},
		FieldMusicBrainzAlbumID:  {"MUSICBRAINZ ALBUM ID"},
		FieldMusicBrainzArtistID: {"MUSICBRAINZ ARTIST ID"},
		FieldISRC:                {"ISRC"},
		FieldBarcode:             {"BARCODE"},
		FieldCatalogNumber:       {"CATALOGNUMBER"},
		FieldLabel:               {"LABEL"},
		FieldCopyright:           {"cprt"},
	}
	want := map[Format]map[Field][]string{
		FormatMP3: id3, FormatWAV: id3, FormatAIFF: id3, FormatM4A: m4a, FormatM4B: m4a,
	}
	// The native copyright text follows the ID3 frame.
	nativeCopyright := map[Format][]string{FormatWAV: {"TCOP", "ICOP"}, FormatAIFF: {"TCOP", "(c) "}}
	for format, fields := range want {
		for field, keys := range fields {
			if native, ok := nativeCopyright[format]; ok && field == FieldCopyright {
				keys = native
			}
			if got := keysOf(format, field); !slices.Equal(got, keys) {
				t.Errorf("%s %s: keys %v, want %v", format, field, got, keys)
			}
		}
	}

	unbound := []struct {
		format Format
		field  Field
	}{
		{FormatMP3, FieldLabel},
		{FormatMP3, FieldPerformers},
		{FormatMP3, FieldDate},
		{FormatWAV, FieldLabel},
		{FormatAIFF, FieldLabel},
		{FormatM4A, FieldOriginalDate},
		{FormatM4A, FieldPerformers},
		{FormatM4B, FieldOriginalDate},
		{FormatM4B, FieldPerformers},
	}
	for _, u := range unbound {
		if got := keysOf(u.format, u.field); got != nil {
			t.Errorf("%s %s: keys %v, want none", u.format, u.field, got)
		}
	}
}

// TPUB is one frame, so it may feed only one field: a Label edit and a
// Publisher edit must never overwrite each other.
func TestFieldBindings_TPUBBoundOnce(t *testing.T) {
	t.Parallel()
	for _, format := range []Format{FormatMP3, FormatWAV, FormatAIFF} {
		var fields []Field
		for _, b := range FieldBindings(format) {
			if slices.Contains(b.Keys, "TPUB") {
				fields = append(fields, b.Field)
			}
		}
		if !slices.Equal(fields, []Field{FieldPublisher}) {
			t.Errorf("%s: TPUB is bound to %v, want only Publisher", format, fields)
		}
	}
}
