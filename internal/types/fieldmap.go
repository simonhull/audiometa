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

// The binding tables are the single source of which raw keys fill which typed
// field: the parsers record raw keys only and derive every field from these
// tables ([DeriveFields]), and the writers plan their edits by them. Keys are
// listed literally so each table reads as data.
//
//nolint:goconst // native tag keys are table data, not magic strings
var (
	// MP3 raw keys: ID3v2 frame IDs (v2.2 IDs are normalized to v2.3), TXXX
	// frames by their upper-cased description, and UFID frames as "UFID:" and
	// their owner. COMM (Comment) is not in the raw map, so Comment has no
	// binding. The release fields follow the tags MusicBrainz Picard writes
	// (https://picard-docs.musicbrainz.org/en/latest/appendices/tag_mapping.html);
	// TPUB, which Picard fills with the label, is the ID3 "Publisher" frame and
	// is bound to Publisher, so Label has no MP3 key.
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
		{FieldPublisher, []string{"PUBLISHER", "TPUB"}},
		{FieldISBN, []string{"ISBN"}},
		{FieldASIN, []string{"ASIN", "AUDIBLE_ASIN"}},
		{FieldLanguage, []string{"LANGUAGE", "LANG"}},
		{FieldDescription, []string{"DESCRIPTION"}},
		{FieldLyrics, []string{"USLT"}},
		{FieldOriginalDate, []string{"TDOR", "TORY"}},
		{FieldMusicBrainzTrackID, []string{"UFID:http://musicbrainz.org"}},
		{FieldMusicBrainzAlbumID, []string{"MUSICBRAINZ ALBUM ID"}},
		{FieldMusicBrainzArtistID, []string{"MUSICBRAINZ ARTIST ID"}},
		{FieldISRC, []string{"TSRC"}},
		{FieldBarcode, []string{"BARCODE"}},
		{FieldCatalogNumber, []string{"CATALOGNUMBER"}},
		{FieldCopyright, []string{"TCOP"}},
	}

	// Vorbis comment raw keys (FLAC, Ogg Vorbis, Opus): the upper-cased field
	// name. TRACKNUMBER and DISCNUMBER carry an optional "N/Total", so they are
	// also the lowest-precedence keys of the total fields; an explicit total key
	// wins.
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
	// freeform (----) atom. trkn and disk are binary and not in the raw map, so
	// track and disc fields have no binding. The release fields follow the
	// freeform items MusicBrainz Picard writes
	// (https://picard-docs.musicbrainz.org/en/latest/appendices/tag_mapping.html).
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
		{FieldMusicBrainzTrackID, []string{"MUSICBRAINZ TRACK ID"}},
		{FieldMusicBrainzAlbumID, []string{"MUSICBRAINZ ALBUM ID"}},
		{FieldMusicBrainzArtistID, []string{"MUSICBRAINZ ARTIST ID"}},
		{FieldISRC, []string{"ISRC"}},
		{FieldBarcode, []string{"BARCODE"}},
		{FieldCatalogNumber, []string{"CATALOGNUMBER"}},
		{FieldLabel, []string{"LABEL"}},
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
// tags that fill the same fields, which the ID3 frames outrank.
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

// DeriveFields sets every typed field of t that format binds from the raw tags
// of t, the single rule by which a parser fills them: a single-valued field
// takes the first non-empty value of the highest-precedence key (the order of
// [FieldBindings]) whose values decode, and a slice field takes all values of
// that one key. A value that does not decode (a year outside the format's
// range, an M4A movement index of 0, a total the number key does not carry)
// falls through to the next key. A field no key yields keeps its value, so
// fields that a parser fills from outside the raw tags (ID3v1, the comment
// frames) survive.
//
// A parser records raw tags only, then calls DeriveFields once.
func DeriveFields(t *Tags, format Format) {
	for _, b := range bindingsFor(format) {
		deriveField(t, format, b)
	}
}

// deriveField sets the field of b from the highest-precedence key of b that
// decodes, skipping the empty values that lead a single-valued field's key. It
// leaves the field alone when no key decodes.
func deriveField(t *Tags, format Format, b FieldBinding) {
	slice := sliceField(t, b.Field) != nil
	for _, k := range b.Keys {
		values := t.raw[k]
		if !slice {
			for len(values) > 0 && values[0] == "" {
				values = values[1:]
			}
		}
		if decodeField(t, format, b.Field, k, values) {
			return
		}
	}
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
		n, ok := scanNumber(key, first, f == FieldTrackTotal || f == FieldDiscTotal)
		if ok {
			*p = n
		}
		return ok
	}
	setFieldValues(t, f, values)
	return true
}

// decodeYear sets Year from the leading four characters of value. ID3 formats
// accept only 1900-2100, and M4A requires four leading digits; other formats
// accept any positive year. The ID3 range must match validID3Year in
// internal/mp3, which this package cannot import and which the MP3 writer uses
// to refuse other years.
func decodeYear(t *Tags, format Format, value string) bool {
	if (format == FormatM4A || format == FormatM4B) && !startsWithFourDigits(value) {
		return false
	}
	year := leadingYear(value)
	if year <= 0 || (usesID3(format) && (year < 1900 || year > 2100)) {
		return false
	}
	t.Year = year
	return true
}

// startsWithFourDigits reports whether s begins with four ASCII digits.
func startsWithFourDigits(s string) bool {
	if len(s) < 4 {
		return false
	}
	for i := range 4 {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
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

// scanNumber decodes the number or, when total is set, the total of a track or
// disc value. A number key holds "N" or "N/Total" and has no total when the
// value carries no "/"; any other key (TRACKTOTAL, TOTALTRACKS, ...) holds one
// number, which is both. Scanning only the part asked for keeps the many reads
// of one open cheap.
func scanNumber(key, value string, total bool) (n int, ok bool) {
	if isNumberKey(key) {
		num, tot, hasTotal := strings.Cut(value, "/")
		switch {
		case !total:
			value = num
		case hasTotal:
			value = tot
		default:
			return 0, false
		}
	}
	_, _ = fmt.Sscanf(value, "%d", &n)
	return n, true
}
