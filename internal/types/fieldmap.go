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
	keyTRCK        = "TRCK"        // ID3 track "N/Total"
	keyTPOS        = "TPOS"        // ID3 disc "N/Total"
	keyTRACKNUMBER = "TRACKNUMBER" // Vorbis track, optionally "N/Total"
	keyDISCNUMBER  = "DISCNUMBER"  // Vorbis disc, optionally "N/Total"
	keyMVIN        = "MVIN"        // ID3 movement number "N/count"
	keyM4AMovement = "\xA9mvi"     // M4A movement index; "0" means unset
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
		{FieldLyrics, []string{"USLT"}},
	}

	// Vorbis comment raw keys (FLAC, Ogg Vorbis, Opus): the upper-cased field
	// name. Mirrors internal/vorbis ParseComment. TRACKNUMBER and DISCNUMBER
	// carry an optional "N/Total", so they are also the lowest-precedence keys
	// of the total fields; an explicit total key wins.
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
		{FieldTrackNumber, []string{keyTRACKNUMBER}},
		{FieldTrackTotal, []string{"TRACKTOTAL", "TOTALTRACKS", keyTRACKNUMBER}},
		{FieldDiscNumber, []string{keyDISCNUMBER}},
		{FieldDiscTotal, []string{"DISCTOTAL", "TOTALDISCS", keyDISCNUMBER}},
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

// The native text tags of WAV (LIST/INFO subchunks) and AIFF (text chunks)
// that hold a typed field, in the order the writer prefers them: a field's
// first key is the one written to a file that has none. internal/iff mirrors
// typed-field edits into exactly these keys. The native comment (ICMT, ANNO)
// is not here: it fills Comment only when there is no ID3 comment, which is
// not a raw key, so Comment has no binding.
var (
	wavNative = []FieldBinding{
		{FieldTitle, []string{"INAM"}},
		{FieldArtist, []string{"IART"}},
		{FieldArtists, []string{"IART"}},
		{FieldAlbum, []string{"IPRD"}},
		{FieldGenres, []string{"IGNR"}},
		{FieldYear, []string{"ICRD"}},
		{FieldTrackNumber, []string{"ITRK", "IPRT"}},
	}
	aiffNative = []FieldBinding{
		{FieldTitle, []string{"NAME"}},
		{FieldArtist, []string{"AUTH"}},
		{FieldArtists, []string{"AUTH"}},
	}
)

// WAV and AIFF raw keys: the ID3v2 chunk's, as for MP3, then the native text
// tags that fill the same fields, which the ID3 frames outrank. Mirrors
// internal/iff.
var (
	wavBindings  = withNative(mp3Bindings, wavNative)
	aiffBindings = withNative(mp3Bindings, aiffNative)
)

// withNative returns base with each native key appended to its field's keys.
func withNative(base, native []FieldBinding) []FieldBinding {
	out := make([]FieldBinding, len(base))
	for i, b := range base {
		out[i] = FieldBinding{Field: b.Field, Keys: slices.Clone(b.Keys)}
		for _, n := range native {
			if n.Field == b.Field {
				out[i].Keys = append(out[i].Keys, n.Keys...)
			}
		}
	}
	return out
}

// usesID3 reports whether format's tags are ID3v2 frames, whose readers take
// years from 1900 to 2100 and MVIN as "N/count".
func usesID3(format Format) bool {
	return format == FormatMP3 || format == FormatWAV || format == FormatAIFF
}

// bindingsFor returns the shared (read-only) binding table for format.
func bindingsFor(format Format) []FieldBinding {
	switch format {
	case FormatMP3:
		return mp3Bindings
	case FormatFLAC, FormatOgg, FormatOpus:
		return vorbisBindings
	case FormatM4A, FormatM4B:
		return m4aBindings
	case FormatWAV:
		return wavBindings
	case FormatAIFF:
		return aiffBindings
	case FormatUnknown:
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

// NativeTextBindings returns the native text keys of format that hold a
// typed field (a fresh copy): LIST/INFO subchunk IDs for WAV, text chunk IDs
// for AIFF, nil for other formats.
func NativeTextBindings(format Format) []FieldBinding {
	src := map[Format][]FieldBinding{FormatWAV: wavNative, FormatAIFF: aiffNative}[format]
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
		number, total, hasTotal := splitNumberTotal(key, first)
		if f == FieldTrackTotal || f == FieldDiscTotal {
			if !hasTotal {
				return false
			}
			*p = total
			return true
		}
		*p = number
		return true
	}
	setFieldValues(t, f, values)
	return true
}

// decodeYear sets Year from the leading four characters of value. MP3 accepts
// only 1900-2100, as its parser does; other formats accept any positive year.
// The range must match validID3Year in internal/mp3, which this package cannot
// import and which the MP3 writer uses to refuse other years.
func decodeYear(t *Tags, format Format, value string) bool {
	year := leadingYear(value)
	if year <= 0 || (usesID3(format) && (year < 1900 || year > 2100)) {
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
	if usesID3(format) && key == keyMVIN {
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

// isNumberKey reports whether key holds "N" or "N/Total": ID3 TRCK and TPOS,
// and Vorbis TRACKNUMBER and DISCNUMBER as ffmpeg and other tools write them.
func isNumberKey(key string) bool {
	return key == keyTRCK || key == keyTPOS || key == keyTRACKNUMBER || key == keyDISCNUMBER
}

// splitNumberTotal decodes a track or disc value. A number key yields its "N"
// and, when present, its "/Total" (hasTotal reports which); any other key
// (TRACKTOTAL, TOTALTRACKS, ...) holds one number, returned as both results.
func splitNumberTotal(key, value string) (number, total int, hasTotal bool) {
	if !isNumberKey(key) {
		_, _ = fmt.Sscanf(value, "%d", &number)
		return number, number, true
	}
	num, tot, ok := strings.Cut(value, "/")
	_, _ = fmt.Sscanf(num, "%d", &number)
	if ok {
		_, _ = fmt.Sscanf(tot, "%d", &total)
	}
	return number, total, ok
}
