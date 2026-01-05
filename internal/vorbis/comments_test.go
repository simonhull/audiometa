package vorbis

import (
	"math"
	"testing"

	"github.com/simonhull/audiometa/internal/types"
)

func TestParseComment(t *testing.T) {
	tests := []struct {
		name    string
		comment string
		check   func(*types.File) bool
	}{
		// Basic metadata
		{"title", "TITLE=Test Song", func(f *types.File) bool { return f.Tags.Title == "Test Song" }},
		{"subtitle", "SUBTITLE=The Remix", func(f *types.File) bool { return f.Tags.Subtitle == "The Remix" }},
		{"artist", "ARTIST=Test Artist", func(f *types.File) bool { return f.Tags.Artist == "Test Artist" }},
		{"artist adds to artists", "ARTIST=Test Artist", func(f *types.File) bool {
			return len(f.Tags.Artists) == 1 && f.Tags.Artists[0] == "Test Artist"
		}},
		{"album", "ALBUM=Test Album", func(f *types.File) bool { return f.Tags.Album == "Test Album" }},
		{"album artist", "ALBUMARTIST=Various Artists", func(f *types.File) bool { return f.Tags.AlbumArtist == "Various Artists" }},

		// Date handling
		{"date full", "DATE=2024-05-15", func(f *types.File) bool { return f.Tags.Date == "2024-05-15" && f.Tags.Year == 2024 }},
		{"date year only", "DATE=2024", func(f *types.File) bool { return f.Tags.Date == "2024" && f.Tags.Year == 2024 }},
		{"original date", "ORIGINALDATE=1985-06-01", func(f *types.File) bool { return f.Tags.OriginalDate == "1985-06-01" }},

		// Track/disc numbers
		{"track number", "TRACKNUMBER=5", func(f *types.File) bool { return f.Tags.TrackNumber == 5 }},
		{"track total", "TRACKTOTAL=12", func(f *types.File) bool { return f.Tags.TrackTotal == 12 }},
		{"totaltracks", "TOTALTRACKS=15", func(f *types.File) bool { return f.Tags.TrackTotal == 15 }},
		{"disc number", "DISCNUMBER=2", func(f *types.File) bool { return f.Tags.DiscNumber == 2 }},
		{"disc total", "DISCTOTAL=3", func(f *types.File) bool { return f.Tags.DiscTotal == 3 }},
		{"totaldiscs", "TOTALDISCS=4", func(f *types.File) bool { return f.Tags.DiscTotal == 4 }},

		// Multi-value fields
		{"genre", "GENRE=Rock", func(f *types.File) bool {
			return len(f.Tags.Genres) == 1 && f.Tags.Genres[0] == "Rock"
		}},
		{"composer", "COMPOSER=John Williams", func(f *types.File) bool {
			return len(f.Tags.Composers) == 1 && f.Tags.Composers[0] == "John Williams"
		}},
		{"performer", "PERFORMER=Symphony Orchestra", func(f *types.File) bool {
			return len(f.Tags.Performers) == 1 && f.Tags.Performers[0] == "Symphony Orchestra"
		}},

		// Text fields
		{"comment", "COMMENT=Great album!", func(f *types.File) bool { return f.Tags.Comment == "Great album!" }},
		{"lyrics", "LYRICS=La la la", func(f *types.File) bool { return f.Tags.Lyrics == "La la la" }},
		{"description", "DESCRIPTION=A detailed description", func(f *types.File) bool { return f.Tags.Description == "A detailed description" }},

		// Audiobook fields
		{"narrator", "NARRATOR=Stephen Fry", func(f *types.File) bool { return f.Tags.Narrator == "Stephen Fry" }},
		{"publisher", "PUBLISHER=Penguin Books", func(f *types.File) bool { return f.Tags.Publisher == "Penguin Books" }},
		{"series", "SERIES=Harry Potter", func(f *types.File) bool { return f.Tags.Series == "Harry Potter" }},
		{"series part", "SERIESPART=1", func(f *types.File) bool { return f.Tags.SeriesPart == "1" }},
		{"isbn", "ISBN=978-0-06-112008-4", func(f *types.File) bool { return f.Tags.ISBN == "978-0-06-112008-4" }},
		{"asin", "ASIN=B00EXAMPLE", func(f *types.File) bool { return f.Tags.ASIN == "B00EXAMPLE" }},
		{"audible asin", "AUDIBLE_ASIN=B00AUDIBLE", func(f *types.File) bool { return f.Tags.ASIN == "B00AUDIBLE" }},
		{"language", "LANGUAGE=en", func(f *types.File) bool { return f.Tags.Language == "en" }},
		{"lang", "LANG=English", func(f *types.File) bool { return f.Tags.Language == "English" }},

		// MusicBrainz IDs
		{"musicbrainz track id", "MUSICBRAINZ_TRACKID=abc123", func(f *types.File) bool { return f.Tags.MusicBrainzTrackID == "abc123" }},
		{"musicbrainz album id", "MUSICBRAINZ_ALBUMID=def456", func(f *types.File) bool { return f.Tags.MusicBrainzAlbumID == "def456" }},
		{"musicbrainz artist id", "MUSICBRAINZ_ARTISTID=ghi789", func(f *types.File) bool { return f.Tags.MusicBrainzArtistID == "ghi789" }},

		// Catalog info
		{"isrc", "ISRC=USRC17607839", func(f *types.File) bool { return f.Tags.ISRC == "USRC17607839" }},
		{"barcode", "BARCODE=012345678901", func(f *types.File) bool { return f.Tags.Barcode == "012345678901" }},
		{"catalog number", "CATALOGNUMBER=ABC-123", func(f *types.File) bool { return f.Tags.CatalogNumber == "ABC-123" }},
		{"label", "LABEL=Sony Music", func(f *types.File) bool { return f.Tags.Label == "Sony Music" }},
		{"copyright", "COPYRIGHT=2024 Sony Music", func(f *types.File) bool { return f.Tags.Copyright == "2024 Sony Music" }},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			file := &types.File{}
			err := ParseComment(tc.comment, file)
			if err != nil {
				t.Fatalf("ParseComment() error = %v", err)
			}
			if !tc.check(file) {
				t.Errorf("ParseComment(%q) did not set expected field", tc.comment)
			}
		})
	}
}

func TestParseComment_ReplayGain(t *testing.T) {
	tests := []struct {
		name    string
		comment string
		check   func(*types.File) bool
	}{
		{
			"track gain with dB",
			"REPLAYGAIN_TRACK_GAIN=-6.50 dB",
			func(f *types.File) bool {
				return f.Audio.ReplayGain != nil && math.Abs(f.Audio.ReplayGain.TrackGain-(-6.50)) < 0.001
			},
		},
		{
			"track gain without dB",
			"REPLAYGAIN_TRACK_GAIN=-6.50",
			func(f *types.File) bool {
				return f.Audio.ReplayGain != nil && math.Abs(f.Audio.ReplayGain.TrackGain-(-6.50)) < 0.001
			},
		},
		{
			"track gain with dB no space",
			"REPLAYGAIN_TRACK_GAIN=-6.50dB",
			func(f *types.File) bool {
				return f.Audio.ReplayGain != nil && math.Abs(f.Audio.ReplayGain.TrackGain-(-6.50)) < 0.001
			},
		},
		{
			"track peak",
			"REPLAYGAIN_TRACK_PEAK=0.988127",
			func(f *types.File) bool {
				return f.Audio.ReplayGain != nil && math.Abs(f.Audio.ReplayGain.TrackPeak-0.988127) < 0.000001
			},
		},
		{
			"album gain",
			"REPLAYGAIN_ALBUM_GAIN=-8.23 dB",
			func(f *types.File) bool {
				return f.Audio.ReplayGain != nil && math.Abs(f.Audio.ReplayGain.AlbumGain-(-8.23)) < 0.001
			},
		},
		{
			"album peak",
			"REPLAYGAIN_ALBUM_PEAK=1.0",
			func(f *types.File) bool {
				return f.Audio.ReplayGain != nil && math.Abs(f.Audio.ReplayGain.AlbumPeak-1.0) < 0.001
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			file := &types.File{}
			err := ParseComment(tc.comment, file)
			if err != nil {
				t.Fatalf("ParseComment() error = %v", err)
			}
			if !tc.check(file) {
				t.Errorf("ParseComment(%q) did not set expected ReplayGain field", tc.comment)
			}
		})
	}
}

func TestParseComment_ReplayGain_AllFields(t *testing.T) {
	file := &types.File{}

	_ = ParseComment("REPLAYGAIN_TRACK_GAIN=-6.50 dB", file)
	_ = ParseComment("REPLAYGAIN_TRACK_PEAK=0.988127", file)
	_ = ParseComment("REPLAYGAIN_ALBUM_GAIN=-8.23 dB", file)
	_ = ParseComment("REPLAYGAIN_ALBUM_PEAK=1.0", file)

	if file.Audio.ReplayGain == nil {
		t.Fatal("ReplayGain should not be nil")
	}

	rg := file.Audio.ReplayGain
	if math.Abs(rg.TrackGain-(-6.50)) > 0.001 {
		t.Errorf("TrackGain = %v, want -6.50", rg.TrackGain)
	}
	if math.Abs(rg.TrackPeak-0.988127) > 0.000001 {
		t.Errorf("TrackPeak = %v, want 0.988127", rg.TrackPeak)
	}
	if math.Abs(rg.AlbumGain-(-8.23)) > 0.001 {
		t.Errorf("AlbumGain = %v, want -8.23", rg.AlbumGain)
	}
	if math.Abs(rg.AlbumPeak-1.0) > 0.001 {
		t.Errorf("AlbumPeak = %v, want 1.0", rg.AlbumPeak)
	}
}

func TestParseComment_MultipleGenres(t *testing.T) {
	file := &types.File{}

	_ = ParseComment("GENRE=Rock", file)
	_ = ParseComment("GENRE=Alternative", file)
	_ = ParseComment("GENRE=Indie", file)

	if len(file.Tags.Genres) != 3 {
		t.Errorf("Genres = %v, want 3 genres", file.Tags.Genres)
	}
	if file.Tags.Genres[0] != "Rock" || file.Tags.Genres[1] != "Alternative" || file.Tags.Genres[2] != "Indie" {
		t.Errorf("Genres = %v, want [Rock Alternative Indie]", file.Tags.Genres)
	}
}

func TestParseComment_MultipleArtists(t *testing.T) {
	file := &types.File{}

	_ = ParseComment("ARTIST=Artist One", file)
	_ = ParseComment("ARTIST=Artist Two", file)

	// Last artist should be set
	if file.Tags.Artist != "Artist Two" {
		t.Errorf("Artist = %q, want %q (last value)", file.Tags.Artist, "Artist Two")
	}
	// Both should be in Artists slice
	if len(file.Tags.Artists) != 2 {
		t.Errorf("Artists = %v, want 2 artists", file.Tags.Artists)
	}
}

func TestParseComment_InvalidFormat(t *testing.T) {
	file := &types.File{}
	err := ParseComment("NOEQUALSIGN", file)
	if err == nil {
		t.Error("ParseComment() should return error for comment without '='")
	}
}

func TestParseComment_EmptyValue(t *testing.T) {
	file := &types.File{}
	err := ParseComment("TITLE=", file)
	if err != nil {
		t.Errorf("ParseComment() error = %v, want nil for empty value", err)
	}
	if file.Tags.Title != "" {
		t.Errorf("Title = %q, want empty string", file.Tags.Title)
	}
}

func TestParseComment_EmptyKey(t *testing.T) {
	file := &types.File{}
	err := ParseComment("=value", file)
	if err != nil {
		t.Errorf("ParseComment() error = %v", err)
	}
	// Empty key should still work (sets raw tag)
	if file.Tags.GetFirst("") != "value" {
		t.Errorf("Raw tag with empty key not set")
	}
}

func TestParseComment_ValueWithEquals(t *testing.T) {
	file := &types.File{}
	err := ParseComment("COMMENT=x=y=z", file)
	if err != nil {
		t.Errorf("ParseComment() error = %v", err)
	}
	if file.Tags.Comment != "x=y=z" {
		t.Errorf("Comment = %q, want %q", file.Tags.Comment, "x=y=z")
	}
}

func TestParseComment_StoresRawTag(t *testing.T) {
	file := &types.File{}
	_ = ParseComment("TITLE=Test Song", file)

	raw := file.Tags.Get("TITLE")
	if len(raw) != 1 || raw[0] != "Test Song" {
		t.Errorf("Raw tag TITLE = %v, want [Test Song]", raw)
	}
}

func TestParseComment_UnknownTag(t *testing.T) {
	file := &types.File{}
	err := ParseComment("CUSTOMTAG=CustomValue", file)
	if err != nil {
		t.Errorf("ParseComment() error = %v for unknown tag", err)
	}

	// Should still be stored in raw tags
	if file.Tags.GetFirst("CUSTOMTAG") != "CustomValue" {
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
			file := &types.File{}
			_ = ParseComment("DATE="+tc.date, file)
			if file.Tags.Year != tc.year {
				t.Errorf("Year = %d for DATE=%s, want %d", file.Tags.Year, tc.date, tc.year)
			}
		})
	}
}

func TestParseComment_DescriptionNotOverwritten(t *testing.T) {
	file := &types.File{}
	file.Tags.Description = "Original"
	_ = ParseComment("DESCRIPTION=New", file)

	if file.Tags.Description != "Original" {
		t.Errorf("Description = %q, want %q (should not be overwritten)", file.Tags.Description, "Original")
	}
}

func TestParseComment_InvalidNumbers(t *testing.T) {
	file := &types.File{}

	_ = ParseComment("TRACKNUMBER=abc", file)
	if file.Tags.TrackNumber != 0 {
		t.Errorf("TrackNumber = %d for invalid input, want 0", file.Tags.TrackNumber)
	}

	_ = ParseComment("DISCNUMBER=", file)
	if file.Tags.DiscNumber != 0 {
		t.Errorf("DiscNumber = %d for empty input, want 0", file.Tags.DiscNumber)
	}
}

func TestParseReplayGainValue(t *testing.T) {
	tests := []struct {
		input string
		want  float64
	}{
		{"-6.50 dB", -6.50},
		{"-6.50dB", -6.50},
		{"-6.50", -6.50},
		{"  -6.50 dB  ", -6.50},
		{"+3.20 dB", 3.20},
		{"0", 0.0},
		{"invalid", 0.0},
	}

	for _, tc := range tests {
		got := parseReplayGainValue(tc.input)
		if math.Abs(got-tc.want) > 0.001 {
			t.Errorf("parseReplayGainValue(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}

func TestParseReplayGainPeak(t *testing.T) {
	tests := []struct {
		input string
		want  float64
	}{
		{"0.988127", 0.988127},
		{"1.0", 1.0},
		{"  0.5  ", 0.5},
		{"invalid", 0.0},
	}

	for _, tc := range tests {
		got := parseReplayGainPeak(tc.input)
		if math.Abs(got-tc.want) > 0.000001 {
			t.Errorf("parseReplayGainPeak(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}
