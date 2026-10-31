package mp3

import (
	"strings"
	"testing"
)

func TestParse_ITunesTextFrames(t *testing.T) {
	type want struct{ series, part, grouping string }
	cases := []struct {
		name   string
		frames func(v byte) [][]byte
		want   want
	}{
		{"mvnm+mvin with count", func(v byte) [][]byte {
			return [][]byte{id3Frame(v, "MVNM", latin1Text("Dune")), id3Frame(v, "MVIN", latin1Text("3/6"))}
		}, want{"Dune", "3", ""}},
		{"mvin alone", func(v byte) [][]byte {
			return [][]byte{id3Frame(v, "MVIN", latin1Text("2"))}
		}, want{"", "2", ""}},
		{"grp1 fallback to series", func(v byte) [][]byte {
			return [][]byte{id3Frame(v, "GRP1", latin1Text("The Expanse #4"))}
		}, want{"The Expanse", "4", "The Expanse #4"}},
		{"grp1 wins over tit1", func(v byte) [][]byte {
			return [][]byte{id3Frame(v, "TIT1", latin1Text("A")), id3Frame(v, "GRP1", latin1Text("B"))}
		}, want{"B", "", "B"}}, // Series derived from grouping
		{"txxx series wins, txxx first", func(v byte) [][]byte {
			return [][]byte{id3Frame(v, "TXXX", latin1Text("SERIES\x00Real")), id3Frame(v, "MVNM", latin1Text("Other"))}
		}, want{"Real", "", ""}},
		{"txxx series wins, mvnm first", func(v byte) [][]byte {
			return [][]byte{id3Frame(v, "MVNM", latin1Text("Other")), id3Frame(v, "TXXX", latin1Text("SERIES\x00Real"))}
		}, want{"Real", "", ""}},
	}
	for _, v := range []byte{3, 4} {
		for _, tc := range cases {
			t.Run(tc.name+"/v"+string('0'+v), func(t *testing.T) {
				f := parseBytes(t, append(id3Tag(v, tc.frames(v)...), mpegFrame...))
				if f.Tags.Series != tc.want.series || f.Tags.SeriesPart != tc.want.part || f.Tags.Grouping != tc.want.grouping {
					t.Errorf("got series=%q part=%q grouping=%q, want %+v",
						f.Tags.Series, f.Tags.SeriesPart, f.Tags.Grouping, tc.want)
				}
			})
		}
	}
}

func TestParse_ChapterTitles(t *testing.T) {
	long := strings.Repeat("x", 200)
	sub := func(v byte, id string, payload []byte) []byte { return id3Frame(v, id, payload) }
	chap := func(v byte, id string, subs ...[]byte) []byte {
		return id3Frame(v, "CHAP", chapFrameBody(id, 0, 1000, subs...))
	}
	trunc := sub(3, "TIT2", latin1Text("abc"))
	trunc[7] = 0xFF // declared size far exceeds remaining bytes

	cases := []struct {
		name string
		v    byte
		body []byte
		want string
	}{
		{"tit3 before tit2", 3, chap(3, "chp0", sub(3, "TIT3", latin1Text("sub")), sub(3, "TIT2", latin1Text("Opening"))), "Opening"},
		{"v2.3 long utf16", 3, chap(3, "chp0", sub(3, "TIT2", utf16Text(long))), long},
		{"v2.4 long utf16", 4, chap(4, "chp0", sub(4, "TIT2", utf16Text(long))), long},
		{"short element id", 3, chap(3, "c1"), "c1"},
		{"truncated subframe", 3, chap(3, "chp0", trunc), "chp0"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := parseBytes(t, append(id3Tag(tc.v, tc.body), mpegFrame...))
			if len(f.Chapters) != 1 {
				t.Fatalf("got %d chapters, want 1", len(f.Chapters))
			}
			if got := f.Chapters[0].Title; got != tc.want {
				t.Errorf("title = %q, want %q", got, tc.want)
			}
		})
	}
}
