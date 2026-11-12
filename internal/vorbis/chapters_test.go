package vorbis

import (
	"testing"
	"time"

	"github.com/simonhull/audiometa/internal/types"
)

func TestParseChaptersSetsSource(t *testing.T) {
	t.Parallel()
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

func TestParseChapterTimestamp(t *testing.T) {
	t.Parallel()
	ok := []struct {
		in   string
		want time.Duration
	}{
		{"00:00:01.001", time.Second + time.Millisecond},
		{"00:00:08.200", 8200 * time.Millisecond},
		{"00:05:23.5", 5*time.Minute + 23*time.Second + 500*time.Millisecond},
		{"01:02:03.123456789", time.Hour + 2*time.Minute + 3*time.Second + 123456789},
		{"00:00:00.1234567891", 123456789}, // beyond nanoseconds: truncated
		{"5:07", 5*time.Minute + 7*time.Second},
		{"59.25", 59*time.Second + 250*time.Millisecond},
		{"7.", 7 * time.Second},
		{"100:00:00", 100 * time.Hour},
	}
	for _, tt := range ok {
		if got, err := parseChapterTimestamp(tt.in); err != nil || got != tt.want {
			t.Errorf("parseChapterTimestamp(%q) = %v, %v; want %v", tt.in, got, err, tt.want)
		}
	}
	for _, in := range []string{
		"", "1e1", "+5", "-1", "0x1p3", "NaN", "Inf", ".5", "00:60:00", "00:00:60",
		"60", "1:2:3:4", "00:00:01.0x", " 1", "100001:00:00",
	} {
		if got, err := parseChapterTimestamp(in); err == nil {
			t.Errorf("parseChapterTimestamp(%q) = %v, want an error", in, got)
		}
	}
}
