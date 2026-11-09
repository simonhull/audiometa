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
//
// Lyrics comes from ID3 USLT, M4A ©lyr or Vorbis LYRICS. An MP3 can hold several
// USLT frames: Lyrics is the first with text, and the raw key USLT lists every
// text. Writing Lyrics rewrites that one frame and keeps its language and
// description; the raw key USLT can be deleted but not set.
type Tags struct {
	raw                 map[string][]string
	names               map[string]string // raw key → stored spelling, where it differs from the key
	format              Format            // bound by BindFormat; zero = unbound (Set touches raw only)
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
//     description, and UFID frames "UFID:" followed by their owner
//     ("UFID:http://musicbrainz.org").
//   - M4A/M4B: the ilst atom name ("\xa9nam" for ©nam); freeform (----)
//     atoms use their upper-cased name.
//
// Lookups are exact-match on the raw key. Because TXXX descriptions and
// freeform names are upper-cased into their keys, a tag stored as
// "MusicBrainz Album Id" is found as "MUSICBRAINZ ALBUM ID" whatever its case
// in the file; [Tags.Name] reports that case. Get returns a copy of the
// values, or nil if the key doesn't exist.
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
	// Index the stored slice directly: Get would copy every value just to
	// read one, which is quadratic when a caller probes a growing key.
	values := t.raw[key]
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
// Writing refuses, with ErrUnsupportedWrite, a key no tag can hold: an empty
// key on any format, and on MP3 a key that names, in any case, an ID3v2 frame
// other than a writable text frame ("APIC", "COMM", "USLT", "tit2"), unless the
// file has a TXXX frame of that description. Other MP3 keys are TXXX
// descriptions.
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
		delete(t.names, key)
	} else {
		t.raw[key] = slices.Clone(values)
	}
	t.syncKey(key)
}

// SetNamed sets the tag called name, whose raw key is name upper-cased: an ID3
// TXXX description or an M4A freeform name, spelled the way the tag should be
// stored when it is new. For example, SetNamed("MusicBrainz Album Id", id)
// sets the raw key "MUSICBRAINZ ALBUM ID", and a TXXX frame or freeform item
// written for it is named "MusicBrainz Album Id".
//
// The spelling applies only to a tag the Tags does not hold yet. A tag that
// exists keeps its spelling: a file tagged "MUSICBRAINZ ALBUM ID" keeps that
// name, and one tagged "MusicBrainz Album Id" keeps that one, so writing never
// adds a second frame or item whose name differs only in case. Vorbis comment
// field names are written as their raw key. Otherwise SetNamed behaves as
// [Tags.Set] with the upper-cased key; empty values remove the tag.
func (t *Tags) SetNamed(name string, values ...string) {
	key := strings.ToUpper(name)
	created := len(values) > 0 && t.raw[key] == nil
	t.Set(key, values...)
	if created {
		t.setName(key, name)
	}
}

// Name returns the name of the tag held under raw key as it is spelled in the
// file, or as [Tags.SetNamed] spelled a new one: "MusicBrainz Album Id" for the
// key "MUSICBRAINZ ALBUM ID" of a file Picard tagged. When several TXXX frames
// or freeform items share the key, it is the first one's spelling. For any
// other key, including one whose name is spelled as its key and one that is
// not present, Name returns key.
func (t *Tags) Name(key string) string {
	if name, ok := t.names[key]; ok {
		return name
	}
	return key
}

// setName records name as the spelling of key, unless it is spelled as key.
func (t *Tags) setName(key, name string) {
	if name == key {
		return
	}
	if t.names == nil {
		t.names = make(map[string]string)
	}
	t.names[key] = name
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
		delete(t.names, key)
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
// it to record every occurrence of a repeated tag. AddRaw is for format
// parsers building Tags: it never syncs typed fields, even on Tags returned by
// Open. To change a tag, use [Tags.Set] or [Tags.Delete].
func (t *Tags) AddRaw(key, value string) {
	if t.raw == nil {
		t.raw = make(map[string][]string)
	}
	t.raw[key] = append(t.raw[key], value)
}

// AddRawNamed is [Tags.AddRaw] for a tag found under name, a TXXX description
// or freeform name whose raw key is name upper-cased. The first name recorded
// for a key is the spelling [Tags.Name] reports; format parsers use it to keep
// the spelling a file has.
func (t *Tags) AddRawNamed(name, value string) {
	key := strings.ToUpper(name)
	if t.raw[key] == nil {
		t.setName(key, name)
	}
	t.AddRaw(key, value)
}

// Merge fills the gaps in t from other; nothing t already holds is replaced.
//
// A typed field that is empty or zero in t takes other's value, and the slice
// fields Artists, Genres, Composers and Performers gain the values of other
// that t lacks, compared case-insensitively. A raw key of other is copied when
// t has no entry for it and t holds none of the typed fields that key fills;
// on Tags returned by Open those fields are then decoded from it, as
// [Tags.Set] does. Raw keys are format-native, so between Tags of two
// different formats only the typed fields are merged.
//
// On a File's Tags the merged values are ordinary edits: File.WriteTo and
// Write store them like any other change. other is not modified.
//
// Example:
//
//	// Apply fallback tags
//	fileTags.Merge(defaultTags)
func (t *Tags) Merge(other *Tags) {
	if other == nil {
		return
	}
	owned := t.mergeRaw(other)
	for f := range AllFields() {
		if !owned[f] {
			mergeField(t, other, f)
		}
	}
}

// mergeRaw copies the raw keys of other that t may take (see [Tags.Merge]) and
// returns the fields those keys now fill, which the typed merge must leave as
// the sync made them. Eligibility is judged on t as it was before the merge,
// so the result does not depend on map order.
func (t *Tags) mergeRaw(other *Tags) (owned [fieldCount]bool) {
	if t.format != FormatUnknown && other.format != FormatUnknown && t.format != other.format {
		return owned
	}
	var take []string
	for _, key := range slices.Sorted(maps.Keys(other.raw)) {
		if _, has := t.raw[key]; !has && !t.holdsFieldBoundTo(key) {
			take = append(take, key)
		}
	}
	if len(take) > 0 && t.raw == nil {
		t.raw = make(map[string][]string, len(take))
	}
	for _, key := range take {
		t.raw[key] = slices.Clone(other.raw[key])
		if name, ok := other.names[key]; ok {
			t.setName(key, name)
		}
	}
	for _, key := range take {
		t.syncKey(key)
		for _, b := range bindingsFor(t.format) {
			if slices.Contains(b.Keys, key) {
				owned[b.Field] = true
			}
		}
	}
	return owned
}

// holdsFieldBoundTo reports whether t already holds a typed field that key
// fills in t's format.
func (t *Tags) holdsFieldBoundTo(key string) bool {
	for _, b := range bindingsFor(t.format) {
		if slices.Contains(b.Keys, key) && valuesOf(t, b.Field) != nil {
			return true
		}
	}
	return false
}

// mergeField fills field f of t from other: an empty string or zero int takes
// other's value, and a slice gains the values of other that it lacks.
func mergeField(t, other *Tags, f Field) {
	switch {
	case stringField(t, f) != nil:
		if p := stringField(t, f); *p == "" {
			*p = *stringField(other, f)
		}
	case sliceField(t, f) != nil:
		p := sliceField(t, f)
		*p = mergeUnique(*p, *sliceField(other, f))
	default:
		if p := intField(t, f); *p == 0 {
			*p = *intField(other, f)
		}
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
	c := *t
	for f := range AllFields() {
		if p := sliceField(&c, f); p != nil {
			*p = slices.Clone(*p)
		}
	}
	if t.raw != nil {
		c.raw = make(map[string][]string, len(t.raw))
		for key, values := range t.raw {
			c.raw[key] = slices.Clone(values)
		}
	}
	c.names = maps.Clone(t.names)
	return &c
}

// Equal checks if two Tags are equal.
//
// Compares all standard fields, raw tags and the spelling of their names
// ([Tags.Name]) for equality. The format a Tags is bound to is not compared.
//
// Example:
//
//	if !tags1.Equal(tags2) {
//		fmt.Println("Tags differ")
//	}
func (t *Tags) Equal(other *Tags) bool {
	if t == nil || other == nil {
		return t == other
	}
	for f := range AllFields() {
		if !fieldEqual(t, other, f) {
			return false
		}
	}
	return maps.EqualFunc(t.raw, other.raw, slices.Equal) && maps.Equal(t.names, other.names)
}

// fieldEqual reports whether field f holds the same value in a and b. It
// dereferences the storage pointers without nil checks because every valid
// Field has exactly one storage table entry (see TestFieldValuesRoundTrip).
func fieldEqual(a, b *Tags, f Field) bool {
	if p := stringField(a, f); p != nil {
		return *p == *stringField(b, f)
	}
	if p := sliceField(a, f); p != nil {
		return slices.Equal(*p, *sliceField(b, f))
	}
	return *intField(a, f) == *intField(b, f)
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
