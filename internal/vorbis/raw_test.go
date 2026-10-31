package vorbis

import (
	"slices"
	"testing"

	"github.com/simonhull/audiometa/internal/types"
)

func TestParseCommentRepeatedKeysAccumulate(t *testing.T) {
	var f types.File
	for _, c := range []string{"genre=Rock", "GENRE=Jazz", "Artist=A", "ARTIST=B"} {
		if err := ParseComment(c, &f); err != nil {
			t.Fatal(err)
		}
	}
	if got := f.Tags.Get("GENRE"); !slices.Equal(got, []string{"Rock", "Jazz"}) {
		t.Errorf("GENRE = %q", got)
	}
	if got := f.Tags.Get("ARTIST"); !slices.Equal(got, []string{"A", "B"}) {
		t.Errorf("ARTIST = %q", got)
	}
}
