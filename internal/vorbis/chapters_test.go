package vorbis

import (
	"testing"
	"time"

	"github.com/simonhull/audiometa/internal/types"
)

func TestParseChaptersSetsSource(t *testing.T) {
	chapters := ParseChapters([]string{
		"CHAPTER001=00:00:00.000", "CHAPTER001NAME=Intro",
		"CHAPTER002=00:05:00.000", "CHAPTER002NAME=Body",
	}, 10*time.Minute)
	if len(chapters) != 2 {
		t.Fatalf("got %d chapters, want 2", len(chapters))
	}
	for i, c := range chapters {
		if c.Source != types.ChapterSourceVorbisComment {
			t.Errorf("chapter %d: source = %q", i, c.Source)
		}
	}
	if chapters[1].Title != "Body" || chapters[1].StartTime != 5*time.Minute {
		t.Errorf("chapter 1 = %+v", chapters[1])
	}
}
