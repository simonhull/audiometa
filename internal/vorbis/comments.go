// Package vorbis provides shared Vorbis comment parsing utilities.
//
// Vorbis comments are used by both FLAC and Ogg Vorbis formats.
// The format is identical: UTF-8 strings in "KEY=VALUE" format.
package vorbis

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/simonhull/audiometa/internal/types"
)

// ParseComment parses a single Vorbis comment in "KEY=VALUE" format
// and populates the appropriate fields in the File struct.
//
// Vorbis comment field names are case-insensitive but typically uppercase.
// The comment is also stored in the raw tags map.
//
// Returns an error if the comment is not in valid "KEY=VALUE" format.
func ParseComment(comment string, file *types.File) error { //nolint:gocyclo // Complexity from many simple field mappings - intentionally kept together
	tags := &file.Tags
	// Find the '=' separator
	eq := -1
	for i := 0; i < len(comment); i++ {
		if comment[i] == '=' {
			eq = i
			break
		}
	}
	if eq == -1 {
		return fmt.Errorf("missing '=' in comment: %s", comment)
	}

	key := comment[:eq]
	value := comment[eq+1:]

	// Map Vorbis comments to standard Tags fields
	// Vorbis comment field names are case-insensitive, but typically uppercase
	switch key {
	case "TITLE":
		tags.Title = value
	case "SUBTITLE":
		tags.Subtitle = value
	case "ARTIST":
		tags.Artist = value
		tags.Artists = append(tags.Artists, value)
	case "ALBUM":
		tags.Album = value
	case "ALBUMARTIST":
		tags.AlbumArtist = value
	case "DATE":
		tags.Date = value
		// Try to extract year from date
		if len(value) >= 4 {
			var year int
			if _, err := fmt.Sscanf(value[:4], "%d", &year); err == nil && year > 0 {
				tags.Year = year
			}
		}
	case "ORIGINALDATE":
		tags.OriginalDate = value
	case "TRACKNUMBER":
		_, _ = fmt.Sscanf(value, "%d", &tags.TrackNumber) //nolint:errcheck // Best effort parsing, zero value is fine
	case "TRACKTOTAL", "TOTALTRACKS":
		_, _ = fmt.Sscanf(value, "%d", &tags.TrackTotal) //nolint:errcheck // Best effort parsing, zero value is fine
	case "DISCNUMBER":
		_, _ = fmt.Sscanf(value, "%d", &tags.DiscNumber) //nolint:errcheck // Best effort parsing, zero value is fine
	case "DISCTOTAL", "TOTALDISCS":
		_, _ = fmt.Sscanf(value, "%d", &tags.DiscTotal) //nolint:errcheck // Best effort parsing, zero value is fine
	case "GENRE":
		tags.Genres = append(tags.Genres, value)
	case "COMPOSER":
		tags.Composers = append(tags.Composers, value)
	case "PERFORMER":
		tags.Performers = append(tags.Performers, value)
	case "COMMENT":
		tags.Comment = value
	case "LYRICS":
		tags.Lyrics = value
	case "NARRATOR":
		tags.Narrator = value
	case "PUBLISHER":
		tags.Publisher = value
	case "SERIES":
		tags.Series = value
	case "SERIESPART":
		tags.SeriesPart = value
	case "ISBN":
		tags.ISBN = value
	case "ASIN", "AUDIBLE_ASIN":
		tags.ASIN = value
	case "LANGUAGE", "LANG":
		tags.Language = value
	case "DESCRIPTION":
		if tags.Description == "" {
			tags.Description = value
		}
	case "MUSICBRAINZ_TRACKID":
		tags.MusicBrainzTrackID = value
	case "MUSICBRAINZ_ALBUMID":
		tags.MusicBrainzAlbumID = value
	case "MUSICBRAINZ_ARTISTID":
		tags.MusicBrainzArtistID = value
	case "ISRC":
		tags.ISRC = value
	case "BARCODE":
		tags.Barcode = value
	case "CATALOGNUMBER":
		tags.CatalogNumber = value
	case "LABEL":
		tags.Label = value
	case "COPYRIGHT":
		tags.Copyright = value

	// ReplayGain tags
	case "REPLAYGAIN_TRACK_GAIN":
		if file.Audio.ReplayGain == nil {
			file.Audio.ReplayGain = &types.ReplayGainInfo{}
		}
		file.Audio.ReplayGain.TrackGain = parseReplayGainValue(value)
	case "REPLAYGAIN_TRACK_PEAK":
		if file.Audio.ReplayGain == nil {
			file.Audio.ReplayGain = &types.ReplayGainInfo{}
		}
		file.Audio.ReplayGain.TrackPeak = parseReplayGainPeak(value)
	case "REPLAYGAIN_ALBUM_GAIN":
		if file.Audio.ReplayGain == nil {
			file.Audio.ReplayGain = &types.ReplayGainInfo{}
		}
		file.Audio.ReplayGain.AlbumGain = parseReplayGainValue(value)
	case "REPLAYGAIN_ALBUM_PEAK":
		if file.Audio.ReplayGain == nil {
			file.Audio.ReplayGain = &types.ReplayGainInfo{}
		}
		file.Audio.ReplayGain.AlbumPeak = parseReplayGainPeak(value)
	}

	// Store in raw tags as well
	tags.Set(key, value)

	return nil
}

// parseReplayGainValue parses a ReplayGain gain value like "-6.50 dB" or "-6.50".
func parseReplayGainValue(s string) float64 {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, " dB")
	s = strings.TrimSuffix(s, "dB")
	s = strings.TrimSpace(s)
	val, _ := strconv.ParseFloat(s, 64) //nolint:errcheck // Best effort parsing, zero value is fine
	return val
}

// parseReplayGainPeak parses a ReplayGain peak value like "0.988127".
func parseReplayGainPeak(s string) float64 {
	val, _ := strconv.ParseFloat(strings.TrimSpace(s), 64) //nolint:errcheck // Best effort parsing, zero value is fine
	return val
}
