package m4a

import (
	"slices"
	"testing"

	"github.com/simonhull/audiometa/internal/types"
)

func TestMapTagToFieldRecordsRawAndArtists(t *testing.T) {
	var f types.File
	mapTagToField("\xA9ART", "Someone", &f)
	if got := f.Tags.Get("\xA9ART"); !slices.Equal(got, []string{"Someone"}) {
		t.Errorf("raw = %q", got)
	}
	if !slices.Equal(f.Tags.Artists, []string{"Someone"}) || f.Tags.Artist != "Someone" {
		t.Errorf("Artist=%q Artists=%q", f.Tags.Artist, f.Tags.Artists)
	}
}
