package audiometa

import (
	"iter"
	"maps"
	"slices"
	"strings"
)

// Tags represents format-agnostic audio metadata.
//
// Tags provides a unified view of metadata across different formats.
// Format-specific tags are mapped to standard fields where possible.
//
// For access to unmapped or custom tags, use the All() iterator or
// Get() method to retrieve raw tag values by key.
type Tags struct {
	// Core identification
	Title       string // Track/song title
	Artist      string // Primary artist
	Album       string // Album name
	AlbumArtist string // Album-level artist (for compilations)

	// Multi-value fields
	Artists    []string // All artists (may include featured artists)
	Genres     []string // Music genres
	Composers  []string // Composers/songwriters
	Performers []string // Performers/musicians

	// Temporal metadata
	Year         int    // Year of release (convenience field)
	Date         string // ISO 8601 date: "2024-03-15"
	OriginalDate string // Original release date

	// Organization
	TrackNumber int // Track number (1-based)
	TrackTotal  int // Total tracks on disc
	DiscNumber  int // Disc number (1-based)
	DiscTotal   int // Total discs in set

	// Extended content
	Comment string // Free-form comment
	Lyrics  string // Song lyrics

	// Audiobook-specific
	Narrator   string // Narrator/reader (audiobooks)
	Publisher  string // Publisher name
	Series     string // Book series name
	SeriesPart string // Position in series ("1", "2.5", etc.)
	ISBN       string // ISBN for books
	ASIN       string // Amazon ASIN

	// Cataloging and identification
	MusicBrainzTrackID  string // MusicBrainz recording ID
	MusicBrainzAlbumID  string // MusicBrainz release ID
	MusicBrainzArtistID string // MusicBrainz artist ID
	ISRC                string // International Standard Recording Code
	Barcode             string // Album UPC/EAN barcode
	CatalogNumber       string // Record label catalog number
	Label               string // Record label name
	Copyright           string // Copyright notice

	// Internal storage (unexported)
	raw map[string][]string // Format-specific raw tags
}

// All returns an iterator over all raw tags.
//
// This uses Go 1.23+ iterator pattern for zero-allocation iteration.
// The iterator yields key-value pairs where values are string slices
// (as tags can have multiple values).
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
// Tag keys are format-specific (e.g., "TITLE", "TIT2", "©nam").
// Returns an empty slice if the key doesn't exist.
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

// Set sets a tag value (for future write support).
//
// If values is empty, the tag is removed.
// Multiple values can be provided for multi-value tags.
//
// Note: This only modifies the in-memory representation.
// Write support is planned for a future release.
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
		return
	}

	t.raw[key] = slices.Clone(values)
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
func (t *Tags) Merge(other *Tags) {
	if other == nil {
		return
	}

	// Merge standard fields (non-empty wins)
	if t.Title == "" {
		t.Title = other.Title
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
	if t.SeriesPart == "" {
		t.SeriesPart = other.SeriesPart
	}
	if t.ISBN == "" {
		t.ISBN = other.ISBN
	}
	if t.ASIN == "" {
		t.ASIN = other.ASIN
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
		// Copy all standard fields
		Title:               t.Title,
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
		Lyrics:              t.Lyrics,
		Narrator:            t.Narrator,
		Publisher:           t.Publisher,
		Series:              t.Series,
		SeriesPart:          t.SeriesPart,
		ISBN:                t.ISBN,
		ASIN:                t.ASIN,
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
func (t *Tags) Equal(other *Tags) bool {
	if t == nil && other == nil {
		return true
	}
	if t == nil || other == nil {
		return false
	}

	// Compare standard fields
	if t.Title != other.Title ||
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
		t.Lyrics != other.Lyrics ||
		t.Narrator != other.Narrator ||
		t.Publisher != other.Publisher ||
		t.Series != other.Series ||
		t.SeriesPart != other.SeriesPart ||
		t.ISBN != other.ISBN ||
		t.ASIN != other.ASIN ||
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
