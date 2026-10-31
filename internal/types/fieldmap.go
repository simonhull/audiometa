package types

import (
	"fmt"
	"slices"
	"strings"
)

// FieldBinding lists the raw keys a format's parser reads into one typed field,
// highest read precedence first.
//
// A key is bound to a field only when the parser assigns that key's value
// directly to that field. No key is ever bound to a field it merely implies
// (TCOM is bound to Composers, never to Narrator), so syncing and writing
// through bindings cannot infer one field from another.
type FieldBinding struct {
	Field Field
	Keys  []string
}

// Raw keys that decodeField treats specially.
const (
	keyTRCK        = "TRCK"    // ID3 track "N/Total"
	keyTPOS        = "TPOS"    // ID3 disc "N/Total"
	keyMVIN        = "MVIN"    // ID3 movement number "N/count"
	keyM4AMovement = "\xA9mvi" // M4A movement index; "0" means unset
)

// The binding tables mirror the parsers. Keys are listed literally so each
// table reads like the parser switch it mirrors.
//
//nolint:goconst // native tag keys are table data, not magic strings
var (
	// MP3 raw keys: ID3v2 frame IDs (v2.2 IDs are normalized to v2.3), and TXXX
	// frames by their upper-cased description. Mirrors internal/mp3 parseTextFrame
	// and txxxFieldHandlers. COMM (Comment) is not in the raw map, so Comment has
	// no binding.
	mp3Bindings = []FieldBinding{
		{FieldTitle, []string{"TIT2"}},
		{FieldSubtitle, []string{"TIT3"}},
		{FieldArtist, []string{"TPE1"}},
		{FieldArtists, []string{"TPE1"}},
		{FieldAlbum, []string{"TALB"}},
		{FieldAlbumArtist, []string{"TPE2"}},
		{FieldGenres, []string{"TCON"}},
		{FieldComposers, []string{"TCOM"}},
		{FieldYear, []string{"TDRC", "TYER"}},
		{FieldTrackNumber, []string{keyTRCK}},
		{FieldTrackTotal, []string{keyTRCK}},
		{FieldDiscNumber, []string{keyTPOS}},
		{FieldDiscTotal, []string{keyTPOS}},
		{FieldGrouping, []string{"GRP1", "TIT1", "GROUPING"}},
		{FieldNarrator, []string{"NARRATOR"}},
		{FieldSeries, []string{"SERIES", "MVNM", "MOVEMENT NAME", "MOVEMENT"}},
		{FieldSeriesPart, []string{"SERIES PART", "SERIESPART", "SERIES-PART", "SERIES POSITION", keyMVIN, "MOVEMENT NUMBER", "MOVEMENT INDEX"}},
		{FieldPublisher, []string{"PUBLISHER"}},
		{FieldISBN, []string{"ISBN"}},
		{FieldASIN, []string{"ASIN", "AUDIBLE_ASIN"}},
		{FieldLanguage, []string{"LANGUAGE", "LANG"}},
		{FieldDescription, []string{"DESCRIPTION"}},
	}

	// Vorbis comment raw keys (FLAC, Ogg Vorbis, Opus): the upper-cased field
	// name. Mirrors internal/vorbis ParseComment.
	vorbisBindings = []FieldBinding{
		{FieldTitle, []string{"TITLE"}},
		{FieldSubtitle, []string{"SUBTITLE"}},
		{FieldArtist, []string{"ARTIST"}},
		{FieldArtists, []string{"ARTIST"}},
		{FieldAlbum, []string{"ALBUM"}},
		{FieldAlbumArtist, []string{"ALBUMARTIST"}},
		{FieldDate, []string{"DATE"}},
		{FieldYear, []string{"DATE"}},
		{FieldOriginalDate, []string{"ORIGINALDATE"}},
		{FieldTrackNumber, []string{"TRACKNUMBER"}},
		{FieldTrackTotal, []string{"TRACKTOTAL", "TOTALTRACKS"}},
		{FieldDiscNumber, []string{"DISCNUMBER"}},
		{FieldDiscTotal, []string{"DISCTOTAL", "TOTALDISCS"}},
		{FieldGenres, []string{"GENRE"}},
		{FieldComposers, []string{"COMPOSER"}},
		{FieldPerformers, []string{"PERFORMER"}},
		{FieldComment, []string{"COMMENT"}},
		{FieldLyrics, []string{"LYRICS"}},
		{FieldNarrator, []string{"NARRATOR"}},
		{FieldPublisher, []string{"PUBLISHER"}},
		{FieldSeries, []string{"SERIES", "MVNM"}},
		{FieldSeriesPart, []string{"SERIESPART", "SERIES-PART", "SERIES_PART", "SERIES PART", keyMVIN}},
		{FieldGrouping, []string{"GROUPING"}},
		{FieldISBN, []string{"ISBN"}},
		{FieldASIN, []string{"ASIN", "AUDIBLE_ASIN"}},
		{FieldLanguage, []string{"LANGUAGE", "LANG"}},
		{FieldDescription, []string{"DESCRIPTION"}},
		{FieldMusicBrainzTrackID, []string{"MUSICBRAINZ_TRACKID"}},
		{FieldMusicBrainzAlbumID, []string{"MUSICBRAINZ_ALBUMID"}},
		{FieldMusicBrainzArtistID, []string{"MUSICBRAINZ_ARTISTID"}},
		{FieldISRC, []string{"ISRC"}},
		{FieldBarcode, []string{"BARCODE"}},
		{FieldCatalogNumber, []string{"CATALOGNUMBER"}},
		{FieldLabel, []string{"LABEL"}},
		{FieldCopyright, []string{"COPYRIGHT"}},
	}

	// M4A/M4B raw keys: the 4-byte ilst atom type, or the upper-cased name of a
	// freeform (----) atom. Mirrors internal/m4a mapTagToField, mapAudiobookField
	// and applyExplicitSeriesPart. trkn and disk are binary and not in the raw
	// map, so track and disc fields have no binding.
	m4aBindings = []FieldBinding{
		{FieldTitle, []string{"\xA9nam"}},
		{FieldArtist, []string{"\xA9ART"}},
		{FieldArtists, []string{"\xA9ART"}},
		{FieldAlbum, []string{"\xA9alb"}},
		{FieldComment, []string{"\xA9cmt"}},
		{FieldGrouping, []string{"\xA9grp"}},
		{FieldLyrics, []string{"\xA9lyr"}},
		{FieldAlbumArtist, []string{"aART"}},
		{FieldCopyright, []string{"cprt"}},
		{FieldDescription, []string{"ldes", "desc", "\xA9des", "DESCRIPTION"}},
		{FieldGenres, []string{"\xA9gen"}},
		{FieldComposers, []string{"\xA9wrt"}},
		{FieldDate, []string{"\xA9day"}},
		{FieldYear, []string{"\xA9day"}},
		{FieldSubtitle, []string{"SUBTITLE"}},
		{FieldNarrator, []string{"NARRATOR"}},
		{FieldSeries, []string{"SERIES", "\xA9mvn", "MVNM", "MOVEMENT NAME", "MOVEMENT"}},
		{FieldSeriesPart, []string{keyM4AMovement, keyMVIN, "MOVEMENT NUMBER", "MOVEMENT INDEX", "SERIES PART", "SERIES POSITION"}},
		{FieldPublisher, []string{"PUBLISHER"}},
		{FieldISBN, []string{"ISBN"}},
		{FieldASIN, []string{"ASIN", "AUDIBLE_ASIN"}},
		{FieldLanguage, []string{"LANGUAGE", "LANG"}},
	}
)

// bindingsFor returns the shared (read-only) binding table for format.
func bindingsFor(format Format) []FieldBinding {
	switch format {
	case FormatMP3:
		return mp3Bindings
	case FormatFLAC, FormatOgg, FormatOpus:
		return vorbisBindings
	case FormatM4A, FormatM4B:
		return m4aBindings
	case FormatUnknown, FormatWAV, FormatAIFF:
		return nil
	default:
		return nil
	}
}

// FieldBindings returns the bindings for format (a fresh copy; callers may not
// mutate shared state). Formats without bindings return nil.
func FieldBindings(format Format) []FieldBinding {
	src := bindingsFor(format)
	if src == nil {
		return nil
	}
	out := make([]FieldBinding, len(src))
	for i, b := range src {
		out[i] = FieldBinding{Field: b.Field, Keys: slices.Clone(b.Keys)}
	}
	return out
}

// decodeField sets field f of t from the raw values of key, decoding them the
// way format's parser does. It reports whether the values yielded a field
// value; when they do not (an out-of-range year, an M4A ©mvi of "0"), t is
// unchanged and the caller should try the next candidate key.
func decodeField(t *Tags, format Format, f Field, key string, values []string) bool {
	if len(values) == 0 {
		return false
	}
	first := values[0]
	if f == FieldYear {
		return decodeYear(t, format, first)
	}
	if f == FieldSeriesPart {
		return decodeSeriesPart(t, format, key, first)
	}
	if p := intField(t, f); p != nil { // track and disc number/total
		number, total := splitNumberTotal(key, first)
		*p = number
		if f == FieldTrackTotal || f == FieldDiscTotal {
			*p = total
		}
		return true
	}
	setFieldValues(t, f, values)
	return true
}

// decodeYear sets Year from the leading four characters of value. MP3 accepts
// only 1900-2100, as its parser does; other formats accept any positive year.
func decodeYear(t *Tags, format Format, value string) bool {
	year := leadingYear(value)
	if year <= 0 || (format == FormatMP3 && (year < 1900 || year > 2100)) {
		return false
	}
	t.Year = year
	return true
}

// decodeSeriesPart sets SeriesPart from value. An M4A ©mvi of "0" means
// unset, and an ID3 MVIN "N/count" yields "N".
func decodeSeriesPart(t *Tags, format Format, key, value string) bool {
	if key == keyM4AMovement && value == "0" {
		return false
	}
	if format == FormatMP3 && key == keyMVIN {
		part, _, _ := strings.Cut(value, "/")
		value = strings.TrimSpace(part)
	}
	t.SeriesPart = value
	return true
}

// leadingYear parses the leading four characters of s as an integer, or 0.
func leadingYear(s string) int {
	if len(s) < 4 {
		return 0
	}
	var year int
	if _, err := fmt.Sscanf(s[:4], "%d", &year); err != nil {
		return 0
	}
	return year
}

// splitNumberTotal decodes a track or disc value. ID3 TRCK and TPOS hold
// "N/Total"; every other key (TRACKNUMBER, TRACKTOTAL, ...) holds a single
// number, returned as both results so the caller picks the one its field needs.
func splitNumberTotal(key, value string) (number, total int) {
	if key != keyTRCK && key != keyTPOS {
		_, _ = fmt.Sscanf(value, "%d", &number)
		return number, number
	}
	num, tot, _ := strings.Cut(value, "/")
	_, _ = fmt.Sscanf(num, "%d", &number)
	_, _ = fmt.Sscanf(tot, "%d", &total)
	return number, total
}
