package mp3

import (
	"slices"
	"testing"

	"github.com/simonhull/audiometa/internal/types"
)

func TestParseTextFrameRecordsRawAndArtists(t *testing.T) {
	var f types.File
	parseTextFrame(ID3v2Frame{ID: "TPE1", Data: append([]byte{3}, "A\x00B"...)}, &f)
	if got := f.Tags.Get("TPE1"); !slices.Equal(got, []string{"A", "B"}) {
		t.Errorf("raw TPE1 = %q", got)
	}
	if !slices.Equal(f.Tags.Artists, []string{"A", "B"}) || f.Tags.Artist != "A" {
		t.Errorf("Artist=%q Artists=%q", f.Tags.Artist, f.Tags.Artists)
	}
}

func TestParseTXXXRecordsUppercasedDescription(t *testing.T) {
	var f types.File
	parseTXXXFrame(ID3v2Frame{ID: "TXXX", Data: append([]byte{3}, "Mood\x00Calm"...)}, &f)
	if got := f.Tags.Get("MOOD"); !slices.Equal(got, []string{"Calm"}) {
		t.Errorf("MOOD = %q", got)
	}
}
