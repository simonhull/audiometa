package vorbis

import (
	"testing"
	"time"
)

func TestParseChapters(t *testing.T) {
	const dur = 10 * time.Minute

	t.Run("contiguous", func(t *testing.T) {
		got := ParseChapters([]string{
			"CHAPTER001=00:00:00.000", "CHAPTER001NAME=One",
			"CHAPTER002=00:03:00.000", "CHAPTER002NAME=Two",
			"CHAPTER003=00:06:30.500",
		}, dur)
		if len(got) != 3 {
			t.Fatalf("got %d chapters, want 3", len(got))
		}
		wantStarts := []time.Duration{0, 3 * time.Minute, 6*time.Minute + 30*time.Second + 500*time.Millisecond}
		wantEnds := []time.Duration{wantStarts[1], wantStarts[2], dur}
		wantTitles := []string{"One", "Two", "Chapter 3"}
		for i, c := range got {
			if c.Index != i+1 || c.StartTime != wantStarts[i] || c.EndTime != wantEnds[i] || c.Title != wantTitles[i] {
				t.Errorf("chapter %d = %+v", i, c)
			}
		}
	})

	t.Run("invalid timestamp dropped", func(t *testing.T) {
		got := ParseChapters([]string{
			"CHAPTER001=00:00:00.000", "CHAPTER002=garbage", "CHAPTER003=00:05:00.000",
		}, dur)
		if len(got) != 2 {
			t.Fatalf("got %d chapters, want 2", len(got))
		}
		if got[0].Index != 1 || got[1].Index != 2 {
			t.Errorf("indices = %d,%d", got[0].Index, got[1].Index)
		}
		if got[0].EndTime != 5*time.Minute {
			t.Errorf("chapter 1 end = %v, want 5m", got[0].EndTime)
		}
		if got[1].Title == "" {
			t.Error("zero-value phantom chapter")
		}
	})

	t.Run("unknown duration", func(t *testing.T) {
		got := ParseChapters([]string{"CHAPTER001=00:00:00.000"}, 0)
		if len(got) != 1 || got[0].EndTime != 0 {
			t.Errorf("got %+v", got)
		}
	})

	t.Run("all invalid", func(t *testing.T) {
		if got := ParseChapters([]string{"CHAPTER001=x", "CHAPTER002=y"}, dur); got != nil {
			t.Errorf("got %+v, want nil", got)
		}
	})

	t.Run("name without timestamp", func(t *testing.T) {
		if got := ParseChapters([]string{"CHAPTER005NAME=Only name"}, dur); got != nil {
			t.Errorf("got %+v, want nil", got)
		}
	})
}
