package types

import (
	"testing"
	"time"
)

func TestFillChapterEndTimes(t *testing.T) {
	sec := func(n int) time.Duration { return time.Duration(n) * time.Second }
	mk := func() []Chapter {
		return []Chapter{{Index: 1, StartTime: 0, Source: ChapterSourceNero}, {Index: 2, StartTime: sec(10), Source: ChapterSourceNero}, {Index: 3, StartTime: sec(25), Source: ChapterSourceNero}}
	}

	t.Run("known duration", func(t *testing.T) {
		ch := mk()
		FillChapterEndTimes(ch, sec(60))
		if ch[0].EndTime != sec(10) || ch[1].EndTime != sec(25) || ch[2].EndTime != sec(60) {
			t.Errorf("end times = %v %v %v", ch[0].EndTime, ch[1].EndTime, ch[2].EndTime)
		}
		if ch[1].Source != ChapterSourceNero || ch[2].Index != 3 {
			t.Error("other fields must be preserved")
		}
	})
	t.Run("unknown duration leaves last at zero", func(t *testing.T) {
		ch := mk()
		FillChapterEndTimes(ch, 0)
		if ch[1].EndTime != sec(25) || ch[2].EndTime != 0 {
			t.Errorf("end times = %v %v", ch[1].EndTime, ch[2].EndTime)
		}
	})
	t.Run("single chapter", func(t *testing.T) {
		ch := mk()[:1]
		FillChapterEndTimes(ch, sec(5))
		if ch[0].EndTime != sec(5) {
			t.Errorf("end = %v", ch[0].EndTime)
		}
	})
	t.Run("empty", func(t *testing.T) { FillChapterEndTimes(nil, sec(5)) })
}
