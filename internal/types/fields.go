package types

import (
	"iter"
	"slices"
	"strconv"
)

// Field identifies one exported, typed field of [Tags]. The zero value is
// invalid. Field is internal: format writers use it to map typed-field edits
// to their native keys.
type Field uint8

// Typed Tags fields, in a fixed order.
const (
	FieldTitle Field = iota + 1
	FieldSubtitle
	FieldArtist
	FieldArtists
	FieldAlbum
	FieldAlbumArtist
	FieldGenres
	FieldComposers
	FieldPerformers
	FieldYear
	FieldDate
	FieldOriginalDate
	FieldTrackNumber
	FieldTrackTotal
	FieldDiscNumber
	FieldDiscTotal
	FieldComment
	FieldDescription
	FieldLyrics
	FieldGrouping
	FieldNarrator
	FieldSeries
	FieldSeriesPart
	FieldPublisher
	FieldISBN
	FieldASIN
	FieldLanguage
	FieldCopyright
	FieldLabel
	FieldCatalogNumber
	FieldBarcode
	FieldISRC
	FieldMusicBrainzTrackID
	FieldMusicBrainzAlbumID
	FieldMusicBrainzArtistID

	fieldCount // sentinel: one past the last field
)

// fieldNames holds the Go field name of each Field, indexed by Field.
//
//nolint:goconst // field names are table data, not magic strings
var fieldNames = [...]string{
	FieldTitle:               "Title",
	FieldSubtitle:            "Subtitle",
	FieldArtist:              "Artist",
	FieldArtists:             "Artists",
	FieldAlbum:               "Album",
	FieldAlbumArtist:         "AlbumArtist",
	FieldGenres:              "Genres",
	FieldComposers:           "Composers",
	FieldPerformers:          "Performers",
	FieldYear:                "Year",
	FieldDate:                "Date",
	FieldOriginalDate:        "OriginalDate",
	FieldTrackNumber:         "TrackNumber",
	FieldTrackTotal:          "TrackTotal",
	FieldDiscNumber:          "DiscNumber",
	FieldDiscTotal:           "DiscTotal",
	FieldComment:             "Comment",
	FieldDescription:         "Description",
	FieldLyrics:              "Lyrics",
	FieldGrouping:            "Grouping",
	FieldNarrator:            "Narrator",
	FieldSeries:              "Series",
	FieldSeriesPart:          "SeriesPart",
	FieldPublisher:           "Publisher",
	FieldISBN:                "ISBN",
	FieldASIN:                "ASIN",
	FieldLanguage:            "Language",
	FieldCopyright:           "Copyright",
	FieldLabel:               "Label",
	FieldCatalogNumber:       "CatalogNumber",
	FieldBarcode:             "Barcode",
	FieldISRC:                "ISRC",
	FieldMusicBrainzTrackID:  "MusicBrainzTrackID",
	FieldMusicBrainzAlbumID:  "MusicBrainzAlbumID",
	FieldMusicBrainzArtistID: "MusicBrainzArtistID",
}

// String returns the Go name of the Tags field, such as "Title", or
// "Field(n)" for an invalid value.
func (f Field) String() string {
	if f > 0 && f < fieldCount {
		return fieldNames[f]
	}
	return "Field(" + strconv.Itoa(int(f)) + ")"
}

// AllFields yields every valid Field, from FieldTitle to
// FieldMusicBrainzArtistID, in order.
func AllFields() iter.Seq[Field] {
	return func(yield func(Field) bool) {
		for f := FieldTitle; f < fieldCount; f++ {
			if !yield(f) {
				return
			}
		}
	}
}

// Field storage, by kind. Exactly one table has a non-nil entry for each
// valid Field; TestFieldValuesRoundTrip checks that.
var (
	stringFields = [fieldCount]func(*Tags) *string{
		FieldTitle:               func(t *Tags) *string { return &t.Title },
		FieldSubtitle:            func(t *Tags) *string { return &t.Subtitle },
		FieldArtist:              func(t *Tags) *string { return &t.Artist },
		FieldAlbum:               func(t *Tags) *string { return &t.Album },
		FieldAlbumArtist:         func(t *Tags) *string { return &t.AlbumArtist },
		FieldDate:                func(t *Tags) *string { return &t.Date },
		FieldOriginalDate:        func(t *Tags) *string { return &t.OriginalDate },
		FieldComment:             func(t *Tags) *string { return &t.Comment },
		FieldDescription:         func(t *Tags) *string { return &t.Description },
		FieldLyrics:              func(t *Tags) *string { return &t.Lyrics },
		FieldGrouping:            func(t *Tags) *string { return &t.Grouping },
		FieldNarrator:            func(t *Tags) *string { return &t.Narrator },
		FieldSeries:              func(t *Tags) *string { return &t.Series },
		FieldSeriesPart:          func(t *Tags) *string { return &t.SeriesPart },
		FieldPublisher:           func(t *Tags) *string { return &t.Publisher },
		FieldISBN:                func(t *Tags) *string { return &t.ISBN },
		FieldASIN:                func(t *Tags) *string { return &t.ASIN },
		FieldLanguage:            func(t *Tags) *string { return &t.Language },
		FieldCopyright:           func(t *Tags) *string { return &t.Copyright },
		FieldLabel:               func(t *Tags) *string { return &t.Label },
		FieldCatalogNumber:       func(t *Tags) *string { return &t.CatalogNumber },
		FieldBarcode:             func(t *Tags) *string { return &t.Barcode },
		FieldISRC:                func(t *Tags) *string { return &t.ISRC },
		FieldMusicBrainzTrackID:  func(t *Tags) *string { return &t.MusicBrainzTrackID },
		FieldMusicBrainzAlbumID:  func(t *Tags) *string { return &t.MusicBrainzAlbumID },
		FieldMusicBrainzArtistID: func(t *Tags) *string { return &t.MusicBrainzArtistID },
	}
	sliceFields = [fieldCount]func(*Tags) *[]string{
		FieldArtists:    func(t *Tags) *[]string { return &t.Artists },
		FieldGenres:     func(t *Tags) *[]string { return &t.Genres },
		FieldComposers:  func(t *Tags) *[]string { return &t.Composers },
		FieldPerformers: func(t *Tags) *[]string { return &t.Performers },
	}
	intFields = [fieldCount]func(*Tags) *int{
		FieldYear:        func(t *Tags) *int { return &t.Year },
		FieldTrackNumber: func(t *Tags) *int { return &t.TrackNumber },
		FieldTrackTotal:  func(t *Tags) *int { return &t.TrackTotal },
		FieldDiscNumber:  func(t *Tags) *int { return &t.DiscNumber },
		FieldDiscTotal:   func(t *Tags) *int { return &t.DiscTotal },
	}
)

// stringField returns a pointer to f's storage when f is a string field, else nil.
func stringField(t *Tags, f Field) *string {
	if f < fieldCount && stringFields[f] != nil {
		return stringFields[f](t)
	}
	return nil
}

// sliceField returns a pointer to f's storage when f is a slice field, else nil.
func sliceField(t *Tags, f Field) *[]string {
	if f < fieldCount && sliceFields[f] != nil {
		return sliceFields[f](t)
	}
	return nil
}

// intField returns a pointer to f's storage when f is an int field, else nil.
func intField(t *Tags, f Field) *int {
	if f < fieldCount && intFields[f] != nil {
		return intFields[f](t)
	}
	return nil
}

// fieldValues returns field f of t in a uniform encoding: nil when the field
// is empty or zero, a one-element slice for string and int fields, and a copy
// of the values for slice fields.
func fieldValues(t *Tags, f Field) []string {
	if p := stringField(t, f); p != nil {
		if *p == "" {
			return nil
		}
		return []string{*p}
	}
	if p := sliceField(t, f); p != nil {
		if len(*p) == 0 {
			return nil
		}
		return slices.Clone(*p)
	}
	if p := intField(t, f); p != nil {
		if *p == 0 {
			return nil
		}
		return []string{strconv.Itoa(*p)}
	}
	return nil
}

// setFieldValues is the inverse of fieldValues: a string field takes v[0],
// a slice field a copy of v, and an int field strconv.Atoi(v[0]) (0 on error).
// An empty v clears the field.
func setFieldValues(t *Tags, f Field, v []string) {
	if p := stringField(t, f); p != nil {
		*p = ""
		if len(v) > 0 {
			*p = v[0]
		}
		return
	}
	if p := sliceField(t, f); p != nil {
		*p = nil
		if len(v) > 0 {
			*p = slices.Clone(v)
		}
		return
	}
	if p := intField(t, f); p != nil {
		*p = 0
		if len(v) > 0 {
			if n, err := strconv.Atoi(v[0]); err == nil {
				*p = n
			}
		}
	}
}
