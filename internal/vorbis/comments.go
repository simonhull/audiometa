// Package vorbis provides shared Vorbis comment parsing utilities.
//
// Vorbis comments are used by FLAC, Ogg Vorbis and Opus. The format is the
// same in all three: UTF-8 strings in "KEY=VALUE" form, with keys matched
// case-insensitively.
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
		_, _ = fmt.Sscanf(value, "%d", &tags.TrackNumber)
	case "TRACKTOTAL", "TOTALTRACKS":
		_, _ = fmt.Sscanf(value, "%d", &tags.TrackTotal)
	case "DISCNUMBER":
		_, _ = fmt.Sscanf(value, "%d", &tags.DiscNumber)
	case "DISCTOTAL", "TOTALDISCS":
		_, _ = fmt.Sscanf(value, "%d", &tags.DiscTotal)
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
	case "SERIESPART", "SERIES-PART", "SERIES_PART", "SERIES PART", "PART":
		if tags.SeriesPart == "" {
			tags.SeriesPart = value
		}
	case "GROUPING":
		tags.Grouping = value
	case "SHOW", "MVNM":
		if tags.Series == "" {
			tags.Series = value
		}
	case "EPISODE_ID", "MVIN":
		if tags.SeriesPart == "" {
			tags.SeriesPart = value
		}
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
	tags.AddRaw(key, value)

	return nil
}

// parseReplayGainValue parses a ReplayGain gain value like "-6.50 dB" or "-6.50".
func parseReplayGainValue(s string) float64 {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, " dB")
	s = strings.TrimSuffix(s, "dB")
	s = strings.TrimSpace(s)
	val, _ := strconv.ParseFloat(s, 64)
	return val
}

// parseReplayGainPeak parses a ReplayGain peak value like "0.988127".
func parseReplayGainPeak(s string) float64 {
	val, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return val
}
