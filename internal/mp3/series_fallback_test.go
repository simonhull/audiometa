package mp3

import (
	"bytes"
	"context"
	"testing"
)

// TestParse_NoInference verifies tags are reported exactly as stored: Series,
// SeriesPart and Narrator are never derived from Grouping, Composer or the path.
func TestParse_NoInference(t *testing.T) {
	txxx := func(k, v string) []byte { return id3Frame(4, "TXXX", latin1Text(k+"\x00"+v)) }
	text := func(id, v string) []byte { return id3Frame(4, id, latin1Text(v)) }
	tests := []struct {
		name         string
		frames       [][]byte
		path         string
		wantSeries   string
		wantPart     string
		wantGrouping string
		wantNarr     string
	}{
		{"grouping is not split", [][]byte{text("GRP1", "Foo, Book 3")}, "/a/b.mp3", "", "", "Foo, Book 3", ""},
		{"explicit tags used as-is", [][]byte{txxx("SERIES", "Explicit"), txxx("SERIES-PART", "7"), text("GRP1", "Other #2")}, "/a/b.mp3", "Explicit", "7", "Other #2", ""},
		{"path is not parsed", [][]byte{txxx("SERIES", "Foo")}, "/books/Foo/Book 03/x.mp3", "Foo", "", "", ""},
		{"title is not parsed", [][]byte{txxx("SERIES", "Foo"), text("TIT2", "Foo Book 4")}, "/x/b.mp3", "Foo", "", "", ""},
		{"composer is not narrator", [][]byte{text("TCOM", "Someone")}, "/a/b.mp3", "", "", "", ""},
		{"explicit narrator", [][]byte{text("TCOM", "Someone"), txxx("NARRATOR", "Reader")}, "/a/b.mp3", "", "", "", "Reader"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			data := append(id3Tag(4, tc.frames...), mpegFrame...)
			file, err := (&parser{}).Parse(context.Background(), bytes.NewReader(data), int64(len(data)), tc.path)
			if err != nil {
				t.Fatal(err)
			}
			g := file.Tags
			if g.Series != tc.wantSeries || g.SeriesPart != tc.wantPart || g.Grouping != tc.wantGrouping || g.Narrator != tc.wantNarr {
				t.Errorf("series/part/grouping/narrator = %q/%q/%q/%q, want %q/%q/%q/%q",
					g.Series, g.SeriesPart, g.Grouping, g.Narrator, tc.wantSeries, tc.wantPart, tc.wantGrouping, tc.wantNarr)
			}
		})
	}
}
