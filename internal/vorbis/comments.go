// Package vorbis provides shared Vorbis comment parsing utilities.
//
// Vorbis comments are used by FLAC, Ogg Vorbis and Opus. The format is the
// same in all three: UTF-8 strings in "KEY=VALUE" form, with keys matched
// case-insensitively.
package vorbis

import (
	"fmt"
	"strings"

	"github.com/simonhull/audiometa/internal/types"
)

// ParseComment parses a single Vorbis comment in "KEY=VALUE" format
// and populates the appropriate fields in the File struct.
//
// Field names are matched case-insensitively, and the comment is stored in the raw tags
// map under its upper-cased name (e.g. "title=Foo" is retrievable as Get("TITLE")).
//
// Returns an error if the comment is not in valid "KEY=VALUE" format.
func ParseComment(comment string, file *types.File) error { //nolint:gocyclo // Complexity from many simple field mappings - intentionally kept together
	tags := &file.Tags
	// Find the '=' separator
	eq := -1
	for i := range len(comment) {
		if comment[i] == '=' {
			eq = i
			break
		}
	}
	if eq == -1 {
		return fmt.Errorf("missing '=' in comment: %s", comment)
	}

	// Field names are case-insensitive per the Vorbis comment spec; canonicalize to upper case so
	// "title=" maps like "TITLE=" and the raw map has one key per logical tag.
	key := strings.ToUpper(comment[:eq])
	value := comment[eq+1:]

	// Map Vorbis comments to standard Tags fields
	switch key {
	case "TITLE":
		setFirst(&tags.Title, value)
	case "SUBTITLE":
		setFirst(&tags.Subtitle, value)
	case "ARTIST":
		setFirst(&tags.Artist, value)
		tags.Artists = append(tags.Artists, value)
	case "ALBUM":
		setFirst(&tags.Album, value)
	case "ALBUMARTIST":
		setFirst(&tags.AlbumArtist, value)
	case "DATE":
		if tags.Date != "" {
			break
		}
		tags.Date = value
		// Try to extract year from date
		if len(value) >= 4 {
			var year int
			if _, err := fmt.Sscanf(value[:4], "%d", &year); err == nil && year > 0 {
				tags.Year = year
			}
		}
	case "ORIGINALDATE":
		setFirst(&tags.OriginalDate, value)
	case "TRACKNUMBER":
		if tags.TrackNumber == 0 {
			_, _ = fmt.Sscanf(value, "%d", &tags.TrackNumber)
		}
	case "TRACKTOTAL", "TOTALTRACKS":
		if tags.TrackTotal == 0 {
			_, _ = fmt.Sscanf(value, "%d", &tags.TrackTotal)
		}
	case "DISCNUMBER":
		if tags.DiscNumber == 0 {
			_, _ = fmt.Sscanf(value, "%d", &tags.DiscNumber)
		}
	case "DISCTOTAL", "TOTALDISCS":
		if tags.DiscTotal == 0 {
			_, _ = fmt.Sscanf(value, "%d", &tags.DiscTotal)
		}
	case "GENRE":
		tags.Genres = append(tags.Genres, value)
	case "COMPOSER":
		tags.Composers = append(tags.Composers, value)
	case "PERFORMER":
		tags.Performers = append(tags.Performers, value)
	case "COMMENT":
		setFirst(&tags.Comment, value)
	case "LYRICS":
		setFirst(&tags.Lyrics, value)
	case "NARRATOR":
		setFirst(&tags.Narrator, value)
	case "PUBLISHER":
		setFirst(&tags.Publisher, value)
	case "SERIES":
		setFirst(&tags.Series, value)
	case "SERIESPART", "SERIES-PART", "SERIES_PART", "SERIES PART":
		if tags.SeriesPart == "" {
			tags.SeriesPart = value
		}
	case "GROUPING":
		setFirst(&tags.Grouping, value)
	case "MVNM":
		if tags.Series == "" {
			tags.Series = value
		}
	case "MVIN":
		if tags.SeriesPart == "" {
			tags.SeriesPart = value
		}
	case "ISBN":
		setFirst(&tags.ISBN, value)
	case "ASIN", "AUDIBLE_ASIN":
		setFirst(&tags.ASIN, value)
	case "LANGUAGE", "LANG":
		setFirst(&tags.Language, value)
	case "DESCRIPTION":
		if tags.Description == "" {
			tags.Description = value
		}
	case "MUSICBRAINZ_TRACKID":
		setFirst(&tags.MusicBrainzTrackID, value)
	case "MUSICBRAINZ_ALBUMID":
		setFirst(&tags.MusicBrainzAlbumID, value)
	case "MUSICBRAINZ_ARTISTID":
		setFirst(&tags.MusicBrainzArtistID, value)
	case "ISRC":
		setFirst(&tags.ISRC, value)
	case "BARCODE":
		setFirst(&tags.Barcode, value)
	case "CATALOGNUMBER":
		setFirst(&tags.CatalogNumber, value)
	case "LABEL":
		setFirst(&tags.Label, value)
	case "COPYRIGHT":
		setFirst(&tags.Copyright, value)
	}

	// Store in raw tags as well
	tags.AddRaw(key, value)

	return nil
}

// setFirst stores value in dst unless dst already holds one, so the first
// occurrence of a repeated single-valued key wins.
func setFirst(dst *string, value string) {
	if *dst == "" {
		*dst = value
	}
}
