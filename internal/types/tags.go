package types

import (
	"iter"
	"maps"
	"slices"
	"strings"
)

// Tags represents format-agnostic audio metadata.
//
// Tags provides a unified view of metadata across different formats.
// Format-specific tags are mapped to the standard fields where possible;
// empty or zero fields were not present in the file.
//
// Every tag is also stored under its native, format-specific key. Use
// [Tags.Get], [Tags.GetFirst] or [Tags.GetBest] to look one up and [Tags.All]
// or [Tags.Filter] to iterate over them.
//
// Tags are reported exactly as they appear in the file. No field is inferred
// from another: Narrator is not derived from Composers, and Series and
// SeriesPart are not derived from Grouping, the title, the album or the file
// path. Apps that want such heuristics should derive them from the exposed
// fields.
//
// A tag fills a typed field only when it means that field: SHOW, EPISODE_ID,
// and generic PART/Volume tags are not series tags and are left in the raw
// tags (Get/All) only.
//
// Audiobook fields are filled only from explicit tags:
//
//   - Narrator: ID3 TXXX "NARRATOR"; M4A freeform "Narrator" atom; Vorbis NARRATOR.
//   - Series: ID3 TXXX "SERIES"/"MVNM"/"MOVEMENT NAME" and frame MVNM;
//     M4A freeform "Series"/"MVNM"/"Movement Name" atoms and the ©mvn atom;
//     Vorbis SERIES and MVNM.
//   - SeriesPart: ID3 TXXX "SERIES PART"/"SERIES-PART"/"SERIESPART"/"SERIES
//     POSITION"/"MVIN" and frame MVIN (an "N/total" value
//     yields "N"); M4A freeform "Series Part"/"Series Position"
//     atoms and the ©mvi atom; Vorbis SERIESPART (also SERIES-PART, SERIES_PART,
//     SERIES PART) and MVIN.
//   - Grouping: ID3 GRP1 (or TIT1 when GRP1 is absent); M4A ©grp; Vorbis GROUPING.
//   - Composers: ID3 TCOM; M4A ©wrt; Vorbis COMPOSER.
type Tags struct {
	raw                 map[string][]string
	format              Format // bound by BindFormat; zero = unbound (Set touches raw only)
	MusicBrainzAlbumID  string
	Narrator            string
	AlbumArtist         string
	Artist              string
	Copyright           string
	Label               string
	CatalogNumber       string
	Barcode             string
	Date                string
	OriginalDate        string
	ISRC                string
	MusicBrainzArtistID string
	Title               string
	Subtitle            string // Book/album subtitle (TIT3 in ID3v2)
	MusicBrainzTrackID  string
	Album               string
	Comment             string
	Description         string // Longer description text (separate from comment)
	Series              string
	Grouping            string // Content grouping (GRP1 or TIT1 in ID3v2, ©grp in M4A), reported as stored; never parsed into Series
	Publisher           string
	Lyrics              string
	SeriesPart          string
	ISBN                string
	ASIN                string
	Language            string // Language code or name (e.g., "en", "English")
	Performers          []string
	Composers           []string
	Genres              []string
	Artists             []string
	DiscTotal           int
	DiscNumber          int
	TrackTotal          int
	TrackNumber         int
	Year                int
}

// All returns an iterator over all raw tags.
//
// The iterator yields each raw key with its values (tags can have multiple
// values), in unspecified order. Sort the keys if you need a stable order.
//
// Example:
//
//	for key, values := range file.Tags.All() {
//		fmt.Printf("%s: %v\n", key, values)
//	}
//
// The returned iterator is read-only. Do not modify the returned slices.
func (t *Tags) All() iter.Seq2[string, []string] {
	return func(yield func(string, []string) bool) {
		if t.raw == nil {
			return
		}
		for key, values := range t.raw {
			if !yield(key, values) {
				return
			}
		}
	}
}

// Get retrieves all values for a tag key.
//
// Tag keys are format-specific:
//
//   - FLAC, Ogg Vorbis and Opus: the Vorbis comment field name, stored
//     upper-cased, so look it up as "TITLE", not "title".
//   - MP3: the ID3v2 frame ID ("TIT2"); TXXX frames use their upper-cased
//     description.
//   - M4A/M4B: the ilst atom name ("\xa9nam" for ©nam); freeform (----)
//     atoms use their upper-cased name.
//
// Lookups are exact-match. Get returns a copy of the values, or nil if the
// key doesn't exist.
//
// For standard fields, prefer accessing struct fields directly
// (Title, Artist, etc.) as they provide format-agnostic access.
//
// Example:
//
//	// Get custom tag
//	vendors := file.Tags.Get("VENDOR")
//	if len(vendors) > 0 {
//		fmt.Println("Encoder:", vendors[0])
//	}
func (t *Tags) Get(key string) []string {
	if t.raw == nil {
		return nil
	}
	values := t.raw[key]
	if values == nil {
		return nil
	}
	return slices.Clone(values) // Return a copy to prevent modification
}

// GetFirst retrieves the first value for a tag key.
//
// Returns empty string if the key doesn't exist or has no values.
//
// Useful when you know a tag has a single value:
//
//	encoder := file.Tags.GetFirst("ENCODER")
func (t *Tags) GetFirst(key string) string {
	values := t.Get(key)
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

// GetBest tries multiple tag keys and returns the first non-empty value.
//
// This is useful for handling format differences where the same metadata
// might be stored under different tag keys:
//
//	// Try various artist tag formats
//	artist := tags.GetBest("ARTIST", "artist", "©ART", "TPE1")
//
// Returns empty string if none of the candidates have values.
func (t *Tags) GetBest(candidates ...string) string {
	for _, key := range candidates {
		if value := t.GetFirst(key); value != "" {
			return value
		}
	}
	return ""
}

// Set sets a tag value in the raw tag map. Keys are format-native (see Get).
//
// If values is empty, the tag is removed; [Tags.Delete] is the clearer
// spelling for that. Multiple values can be provided for multi-value tags.
//
// On Tags returned by Open, Set also updates the typed field that key
// represents, so Set("TIT2", "x") on an MP3 makes Title "x". It never changes
// any other field: Set("TCOM", ...) changes Composers, not Narrator. On a zero
// or caller-built Tags, Set changes only the raw map.
// Changes are in memory; use File.WriteTo or Write to persist them.
//
// Example:
//
//	tags.Set("COMMENT", "Remastered")
//	tags.Set("GENRE", "Rock", "Alternative") // Multi-value
func (t *Tags) Set(key string, values ...string) {
	if t.raw == nil {
		t.raw = make(map[string][]string)
	}

	if len(values) == 0 {
		delete(t.raw, key)
	} else {
		t.raw[key] = slices.Clone(values)
	}
	t.syncKey(key)
}

// Delete removes the raw tags with the given format-native keys (see Get).
// Keys that are not present are ignored.
//
// On Tags returned by Open, Delete also updates the typed field each key
// represents: the field is re-derived from any remaining key for that field,
// or cleared if none remains. When the File is written, deleted keys are
// removed from the file; no other tag is changed.
//
// Delete can remove any tag, including keys that fill no typed field, such
// as a Vorbis SHOW comment, an ID3 TXXX description or an iTunes freeform
// atom name.
func (t *Tags) Delete(keys ...string) {
	for _, key := range keys {
		if _, ok := t.raw[key]; !ok {
			continue
		}
		delete(t.raw, key)
		t.syncKey(key)
	}
}

// BindFormat records the format whose parser produced t, enabling Set and
// Delete to keep typed fields in sync with raw keys. Called by the root
// package after parsing.
//
// It also resolves every field that has several candidate keys from the
// highest-precedence key present, as FieldBindings orders them, so a parser that
// filled the field from whichever key came first in the file cannot disagree
// with Set, Delete and the writers. A field none of whose keys decodes keeps the
// parser's value, and no key fills a field it is not bound to.
func BindFormat(t *Tags, format Format) {
	t.format = format
	for _, b := range bindingsFor(format) {
		if len(b.Keys) < 2 {
			continue
		}
		for _, k := range b.Keys {
			if decodeField(t, format, b.Field, k, t.raw[k]) {
				break
			}
		}
	}
}

// syncKey recomputes every typed field bound to key on a bound Tags: each
// such field is decoded from the highest-precedence bound key still present
// in raw, or cleared if none is. Fields not bound to key are never touched.
func (t *Tags) syncKey(key string) {
	if t.format == FormatUnknown {
		return
	}
	for _, b := range bindingsFor(t.format) {
		if !slices.Contains(b.Keys, key) {
			continue
		}
		decoded := false
		for _, k := range b.Keys {
			if decodeField(t, t.format, b.Field, k, t.raw[k]) {
				decoded = true
				break
			}
		}
		if !decoded {
			setFieldValues(t, b.Field, nil)
		}
	}
}

// AddRaw appends a value to the raw tag map under key, creating the entry if
// absent. Unlike Set it never replaces existing values, so format parsers use
// it to record every occurrence of a repeated tag.
func (t *Tags) AddRaw(key, value string) {
	if t.raw == nil {
		t.raw = make(map[string][]string)
	}
	t.raw[key] = append(t.raw[key], value)
}

// Merge merges tags from another Tags object.
//
// For standard fields, non-empty values in other override empty values in t.
// For raw tags, all tags from other are copied to t.
//
// Example:
//
//	// Apply fallback tags
//	fileTags.Merge(defaultTags)
func (t *Tags) Merge(other *Tags) { //nolint:gocyclo // Merging requires checking all tag fields individually
	if other == nil {
		return
	}

	// Merge standard fields (non-empty wins)
	if t.Title == "" {
		t.Title = other.Title
	}
	if t.Subtitle == "" {
		t.Subtitle = other.Subtitle
	}
	if t.Artist == "" {
		t.Artist = other.Artist
	}
	if t.Album == "" {
		t.Album = other.Album
	}
	if t.AlbumArtist == "" {
		t.AlbumArtist = other.AlbumArtist
	}
	if t.Year == 0 {
		t.Year = other.Year
	}
	if t.Date == "" {
		t.Date = other.Date
	}
	if t.OriginalDate == "" {
		t.OriginalDate = other.OriginalDate
	}
	if t.Comment == "" {
		t.Comment = other.Comment
	}
	if t.Description == "" {
		t.Description = other.Description
	}
	if t.Lyrics == "" {
		t.Lyrics = other.Lyrics
	}
	if t.Narrator == "" {
		t.Narrator = other.Narrator
	}
	if t.Publisher == "" {
		t.Publisher = other.Publisher
	}
	if t.Series == "" {
		t.Series = other.Series
	}
	if t.Grouping == "" {
		t.Grouping = other.Grouping
	}
	if t.SeriesPart == "" {
		t.SeriesPart = other.SeriesPart
	}
	if t.ISBN == "" {
		t.ISBN = other.ISBN
	}
	if t.ASIN == "" {
		t.ASIN = other.ASIN
	}
	if t.Language == "" {
		t.Language = other.Language
	}
	if t.TrackNumber == 0 {
		t.TrackNumber = other.TrackNumber
	}
	if t.TrackTotal == 0 {
		t.TrackTotal = other.TrackTotal
	}
	if t.DiscNumber == 0 {
		t.DiscNumber = other.DiscNumber
	}
	if t.DiscTotal == 0 {
		t.DiscTotal = other.DiscTotal
	}

	// Merge multi-value fields (append unique)
	t.Artists = mergeUnique(t.Artists, other.Artists)
	t.Genres = mergeUnique(t.Genres, other.Genres)
	t.Composers = mergeUnique(t.Composers, other.Composers)
	t.Performers = mergeUnique(t.Performers, other.Performers)

	// Merge cataloging fields
	if t.MusicBrainzTrackID == "" {
		t.MusicBrainzTrackID = other.MusicBrainzTrackID
	}
	if t.MusicBrainzAlbumID == "" {
		t.MusicBrainzAlbumID = other.MusicBrainzAlbumID
	}
	if t.MusicBrainzArtistID == "" {
		t.MusicBrainzArtistID = other.MusicBrainzArtistID
	}
	if t.ISRC == "" {
		t.ISRC = other.ISRC
	}
	if t.Barcode == "" {
		t.Barcode = other.Barcode
	}
	if t.CatalogNumber == "" {
		t.CatalogNumber = other.CatalogNumber
	}
	if t.Label == "" {
		t.Label = other.Label
	}
	if t.Copyright == "" {
		t.Copyright = other.Copyright
	}

	// Merge raw tags
	if t.raw == nil {
		t.raw = make(map[string][]string)
	}
	for key, values := range other.raw {
		t.raw[key] = slices.Clone(values)
	}
}

// Clone creates a deep copy of the Tags.
//
// Example:
//
//	backup := originalTags.Clone()
func (t *Tags) Clone() *Tags {
	if t == nil {
		return nil
	}

	clone := &Tags{
		format: t.format,

		// Copy all standard fields
		Title:               t.Title,
		Subtitle:            t.Subtitle,
		Artist:              t.Artist,
		Album:               t.Album,
		AlbumArtist:         t.AlbumArtist,
		Year:                t.Year,
		Date:                t.Date,
		OriginalDate:        t.OriginalDate,
		TrackNumber:         t.TrackNumber,
		TrackTotal:          t.TrackTotal,
		DiscNumber:          t.DiscNumber,
		DiscTotal:           t.DiscTotal,
		Comment:             t.Comment,
		Description:         t.Description,
		Lyrics:              t.Lyrics,
		Narrator:            t.Narrator,
		Publisher:           t.Publisher,
		Series:              t.Series,
		Grouping:            t.Grouping,
		SeriesPart:          t.SeriesPart,
		ISBN:                t.ISBN,
		ASIN:                t.ASIN,
		Language:            t.Language,
		MusicBrainzTrackID:  t.MusicBrainzTrackID,
		MusicBrainzAlbumID:  t.MusicBrainzAlbumID,
		MusicBrainzArtistID: t.MusicBrainzArtistID,
		ISRC:                t.ISRC,
		Barcode:             t.Barcode,
		CatalogNumber:       t.CatalogNumber,
		Label:               t.Label,
		Copyright:           t.Copyright,

		// Clone slices
		Artists:    slices.Clone(t.Artists),
		Genres:     slices.Clone(t.Genres),
		Composers:  slices.Clone(t.Composers),
		Performers: slices.Clone(t.Performers),
	}

	// Clone raw tags
	if t.raw != nil {
		clone.raw = make(map[string][]string, len(t.raw))
		for key, values := range t.raw {
			clone.raw[key] = slices.Clone(values)
		}
	}

	return clone
}

// Equal checks if two Tags are equal.
//
// Compares all standard fields and raw tags for equality.
//
// Example:
//
//	if !tags1.Equal(tags2) {
//		fmt.Println("Tags differ")
//	}
func (t *Tags) Equal(other *Tags) bool { //nolint:gocyclo // Equality check requires comparing all tag fields individually
	if t == nil && other == nil {
		return true
	}
	if t == nil || other == nil {
		return false
	}

	// Compare standard fields
	if t.Title != other.Title ||
		t.Subtitle != other.Subtitle ||
		t.Artist != other.Artist ||
		t.Album != other.Album ||
		t.AlbumArtist != other.AlbumArtist ||
		t.Year != other.Year ||
		t.Date != other.Date ||
		t.OriginalDate != other.OriginalDate ||
		t.TrackNumber != other.TrackNumber ||
		t.TrackTotal != other.TrackTotal ||
		t.DiscNumber != other.DiscNumber ||
		t.DiscTotal != other.DiscTotal ||
		t.Comment != other.Comment ||
		t.Description != other.Description ||
		t.Lyrics != other.Lyrics ||
		t.Narrator != other.Narrator ||
		t.Publisher != other.Publisher ||
		t.Series != other.Series ||
		t.Grouping != other.Grouping ||
		t.SeriesPart != other.SeriesPart ||
		t.ISBN != other.ISBN ||
		t.ASIN != other.ASIN ||
		t.Language != other.Language ||
		t.MusicBrainzTrackID != other.MusicBrainzTrackID ||
		t.MusicBrainzAlbumID != other.MusicBrainzAlbumID ||
		t.MusicBrainzArtistID != other.MusicBrainzArtistID ||
		t.ISRC != other.ISRC ||
		t.Barcode != other.Barcode ||
		t.CatalogNumber != other.CatalogNumber ||
		t.Label != other.Label ||
		t.Copyright != other.Copyright {
		return false
	}

	// Compare slices
	if !slices.Equal(t.Artists, other.Artists) ||
		!slices.Equal(t.Genres, other.Genres) ||
		!slices.Equal(t.Composers, other.Composers) ||
		!slices.Equal(t.Performers, other.Performers) {
		return false
	}

	// Compare raw tags (uses maps.Equal from Go 1.21+)
	return maps.EqualFunc(t.raw, other.raw, slices.Equal)
}

// Filter returns an iterator over tags matching a predicate.
//
// Example:
//
//	// Find all MusicBrainz tags
//	for key, values := range file.Tags.Filter(func(k string) bool {
//		return strings.HasPrefix(k, "MUSICBRAINZ")
//	}) {
//		fmt.Printf("%s: %v\n", key, values)
//	}
func (t *Tags) Filter(predicate func(string) bool) iter.Seq2[string, []string] {
	return func(yield func(string, []string) bool) {
		if t.raw == nil {
			return
		}
		for key, values := range t.raw {
			if predicate(key) {
				if !yield(key, values) {
					return
				}
			}
		}
	}
}

// mergeUnique appends elements from b to a, skipping duplicates.
// Uses case-insensitive comparison for strings.
func mergeUnique(a, b []string) []string {
	if len(b) == 0 {
		return a
	}

	result := slices.Clone(a)

	for _, bVal := range b {
		found := false
		for _, aVal := range result {
			if strings.EqualFold(aVal, bVal) {
				found = true
				break
			}
		}
		if !found {
			result = append(result, bVal)
		}
	}

	return result
}
