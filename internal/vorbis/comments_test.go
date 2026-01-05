package vorbis

import (
	"testing"

	"github.com/simonhull/audiometa/internal/types"
)

func TestParseComment(t *testing.T) {
	tests := []struct {
		name    string
		comment string
		check   func(*types.Tags) bool
	}{
		// Basic metadata
		{"title", "TITLE=Test Song", func(t *types.Tags) bool { return t.Title == "Test Song" }},
		{"subtitle", "SUBTITLE=The Remix", func(t *types.Tags) bool { return t.Subtitle == "The Remix" }},
		{"artist", "ARTIST=Test Artist", func(t *types.Tags) bool { return t.Artist == "Test Artist" }},
		{"artist adds to artists", "ARTIST=Test Artist", func(t *types.Tags) bool {
			return len(t.Artists) == 1 && t.Artists[0] == "Test Artist"
		}},
		{"album", "ALBUM=Test Album", func(t *types.Tags) bool { return t.Album == "Test Album" }},
		{"album artist", "ALBUMARTIST=Various Artists", func(t *types.Tags) bool { return t.AlbumArtist == "Various Artists" }},

		// Date handling
		{"date full", "DATE=2024-05-15", func(t *types.Tags) bool { return t.Date == "2024-05-15" && t.Year == 2024 }},
		{"date year only", "DATE=2024", func(t *types.Tags) bool { return t.Date == "2024" && t.Year == 2024 }},
		{"original date", "ORIGINALDATE=1985-06-01", func(t *types.Tags) bool { return t.OriginalDate == "1985-06-01" }},

		// Track/disc numbers
		{"track number", "TRACKNUMBER=5", func(t *types.Tags) bool { return t.TrackNumber == 5 }},
		{"track total", "TRACKTOTAL=12", func(t *types.Tags) bool { return t.TrackTotal == 12 }},
		{"totaltracks", "TOTALTRACKS=15", func(t *types.Tags) bool { return t.TrackTotal == 15 }},
		{"disc number", "DISCNUMBER=2", func(t *types.Tags) bool { return t.DiscNumber == 2 }},
		{"disc total", "DISCTOTAL=3", func(t *types.Tags) bool { return t.DiscTotal == 3 }},
		{"totaldiscs", "TOTALDISCS=4", func(t *types.Tags) bool { return t.DiscTotal == 4 }},

		// Multi-value fields
		{"genre", "GENRE=Rock", func(t *types.Tags) bool {
			return len(t.Genres) == 1 && t.Genres[0] == "Rock"
		}},
		{"composer", "COMPOSER=John Williams", func(t *types.Tags) bool {
			return len(t.Composers) == 1 && t.Composers[0] == "John Williams"
		}},
		{"performer", "PERFORMER=Symphony Orchestra", func(t *types.Tags) bool {
			return len(t.Performers) == 1 && t.Performers[0] == "Symphony Orchestra"
		}},

		// Text fields
		{"comment", "COMMENT=Great album!", func(t *types.Tags) bool { return t.Comment == "Great album!" }},
		{"lyrics", "LYRICS=La la la", func(t *types.Tags) bool { return t.Lyrics == "La la la" }},
		{"description", "DESCRIPTION=A detailed description", func(t *types.Tags) bool { return t.Description == "A detailed description" }},

		// Audiobook fields
		{"narrator", "NARRATOR=Stephen Fry", func(t *types.Tags) bool { return t.Narrator == "Stephen Fry" }},
		{"publisher", "PUBLISHER=Penguin Books", func(t *types.Tags) bool { return t.Publisher == "Penguin Books" }},
		{"series", "SERIES=Harry Potter", func(t *types.Tags) bool { return t.Series == "Harry Potter" }},
		{"series part", "SERIESPART=1", func(t *types.Tags) bool { return t.SeriesPart == "1" }},
		{"isbn", "ISBN=978-0-06-112008-4", func(t *types.Tags) bool { return t.ISBN == "978-0-06-112008-4" }},
		{"asin", "ASIN=B00EXAMPLE", func(t *types.Tags) bool { return t.ASIN == "B00EXAMPLE" }},
		{"audible asin", "AUDIBLE_ASIN=B00AUDIBLE", func(t *types.Tags) bool { return t.ASIN == "B00AUDIBLE" }},
		{"language", "LANGUAGE=en", func(t *types.Tags) bool { return t.Language == "en" }},
		{"lang", "LANG=English", func(t *types.Tags) bool { return t.Language == "English" }},

		// MusicBrainz IDs
		{"musicbrainz track id", "MUSICBRAINZ_TRACKID=abc123", func(t *types.Tags) bool { return t.MusicBrainzTrackID == "abc123" }},
		{"musicbrainz album id", "MUSICBRAINZ_ALBUMID=def456", func(t *types.Tags) bool { return t.MusicBrainzAlbumID == "def456" }},
		{"musicbrainz artist id", "MUSICBRAINZ_ARTISTID=ghi789", func(t *types.Tags) bool { return t.MusicBrainzArtistID == "ghi789" }},

		// Catalog info
		{"isrc", "ISRC=USRC17607839", func(t *types.Tags) bool { return t.ISRC == "USRC17607839" }},
		{"barcode", "BARCODE=012345678901", func(t *types.Tags) bool { return t.Barcode == "012345678901" }},
		{"catalog number", "CATALOGNUMBER=ABC-123", func(t *types.Tags) bool { return t.CatalogNumber == "ABC-123" }},
		{"label", "LABEL=Sony Music", func(t *types.Tags) bool { return t.Label == "Sony Music" }},
		{"copyright", "COPYRIGHT=2024 Sony Music", func(t *types.Tags) bool { return t.Copyright == "2024 Sony Music" }},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tags := &types.Tags{}
			err := ParseComment(tc.comment, tags)
			if err != nil {
				t.Fatalf("ParseComment() error = %v", err)
			}
			if !tc.check(tags) {
				t.Errorf("ParseComment(%q) did not set expected field", tc.comment)
			}
		})
	}
}

func TestParseComment_MultipleGenres(t *testing.T) {
	tags := &types.Tags{}

	_ = ParseComment("GENRE=Rock", tags)
	_ = ParseComment("GENRE=Alternative", tags)
	_ = ParseComment("GENRE=Indie", tags)

	if len(tags.Genres) != 3 {
		t.Errorf("Genres = %v, want 3 genres", tags.Genres)
	}
	if tags.Genres[0] != "Rock" || tags.Genres[1] != "Alternative" || tags.Genres[2] != "Indie" {
		t.Errorf("Genres = %v, want [Rock Alternative Indie]", tags.Genres)
	}
}

func TestParseComment_MultipleArtists(t *testing.T) {
	tags := &types.Tags{}

	_ = ParseComment("ARTIST=Artist One", tags)
	_ = ParseComment("ARTIST=Artist Two", tags)

	// First artist should be set
	if tags.Artist != "Artist Two" {
		t.Errorf("Artist = %q, want %q (last value)", tags.Artist, "Artist Two")
	}
	// Both should be in Artists slice
	if len(tags.Artists) != 2 {
		t.Errorf("Artists = %v, want 2 artists", tags.Artists)
	}
}

func TestParseComment_InvalidFormat(t *testing.T) {
	tags := &types.Tags{}
	err := ParseComment("NOEQUALSIGN", tags)
	if err == nil {
		t.Error("ParseComment() should return error for comment without '='")
	}
}

func TestParseComment_EmptyValue(t *testing.T) {
	tags := &types.Tags{}
	err := ParseComment("TITLE=", tags)
	if err != nil {
		t.Errorf("ParseComment() error = %v, want nil for empty value", err)
	}
	if tags.Title != "" {
		t.Errorf("Title = %q, want empty string", tags.Title)
	}
}

func TestParseComment_EmptyKey(t *testing.T) {
	tags := &types.Tags{}
	err := ParseComment("=value", tags)
	if err != nil {
		t.Errorf("ParseComment() error = %v", err)
	}
	// Empty key should still work (sets raw tag)
	if tags.GetFirst("") != "value" {
		t.Errorf("Raw tag with empty key not set")
	}
}

func TestParseComment_ValueWithEquals(t *testing.T) {
	tags := &types.Tags{}
	err := ParseComment("COMMENT=x=y=z", tags)
	if err != nil {
		t.Errorf("ParseComment() error = %v", err)
	}
	if tags.Comment != "x=y=z" {
		t.Errorf("Comment = %q, want %q", tags.Comment, "x=y=z")
	}
}

func TestParseComment_StoresRawTag(t *testing.T) {
	tags := &types.Tags{}
	_ = ParseComment("TITLE=Test Song", tags)

	raw := tags.Get("TITLE")
	if len(raw) != 1 || raw[0] != "Test Song" {
		t.Errorf("Raw tag TITLE = %v, want [Test Song]", raw)
	}
}

func TestParseComment_UnknownTag(t *testing.T) {
	tags := &types.Tags{}
	err := ParseComment("CUSTOMTAG=CustomValue", tags)
	if err != nil {
		t.Errorf("ParseComment() error = %v for unknown tag", err)
	}

	// Should still be stored in raw tags
	if tags.GetFirst("CUSTOMTAG") != "CustomValue" {
		t.Errorf("Unknown tag not stored in raw tags")
	}
}

func TestParseComment_DateYearExtraction(t *testing.T) {
	tests := []struct {
		date string
		year int
	}{
		{"2024", 2024},
		{"2024-05-15", 2024},
		{"2024-05-15T12:00:00", 2024},
		{"202", 0},  // Too short
		{"ABCD", 0}, // Not a number
	}

	for _, tc := range tests {
		t.Run(tc.date, func(t *testing.T) {
			tags := &types.Tags{}
			_ = ParseComment("DATE="+tc.date, tags)
			if tags.Year != tc.year {
				t.Errorf("Year = %d for DATE=%s, want %d", tags.Year, tc.date, tc.year)
			}
		})
	}
}

func TestParseComment_DescriptionNotOverwritten(t *testing.T) {
	tags := &types.Tags{Description: "Original"}
	_ = ParseComment("DESCRIPTION=New", tags)

	if tags.Description != "Original" {
		t.Errorf("Description = %q, want %q (should not be overwritten)", tags.Description, "Original")
	}
}

func TestParseComment_InvalidNumbers(t *testing.T) {
	tags := &types.Tags{}

	_ = ParseComment("TRACKNUMBER=abc", tags)
	if tags.TrackNumber != 0 {
		t.Errorf("TrackNumber = %d for invalid input, want 0", tags.TrackNumber)
	}

	_ = ParseComment("DISCNUMBER=", tags)
	if tags.DiscNumber != 0 {
		t.Errorf("DiscNumber = %d for empty input, want 0", tags.DiscNumber)
	}
}
