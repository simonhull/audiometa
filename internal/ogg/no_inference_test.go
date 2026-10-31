package ogg

import (
	"bytes"
	"context"
	"testing"
)

// TestParse_NoInference verifies tags are reported exactly as stored: Series,
// SeriesPart and Narrator are never derived from Grouping, Composer or the path.
func TestParse_NoInference(t *testing.T) {
	tests := []struct {
		name         string
		extra        []string
		path         string
		wantSeries   string
		wantPart     string
		wantGrouping string
		wantNarr     string
	}{
		{"grouping is not split", []string{"GROUPING=Foo, Book 3"}, "/a/b.ogg", "", "", "Foo, Book 3", ""},
		{"explicit tags used as-is", []string{"SERIES=Explicit", "SERIESPART=7", "GROUPING=Other #2"}, "/a/b.ogg", "Explicit", "7", "Other #2", ""},
		{"path is not parsed", []string{"SERIES=Foo"}, "/books/Foo/Book 03/b.ogg", "Foo", "", "", ""},
		{"composer is not narrator", []string{"COMPOSER=Someone"}, "/a/b.ogg", "", "", "", ""},
		{"explicit narrator", []string{"COMPOSER=Someone", "NARRATOR=Reader"}, "/a/b.ogg", "", "", "", "Reader"},
	}
	for _, tc := range tests {
		for _, codec := range []struct {
			name string
			make func(title, artist, album string, extra ...string) []byte
		}{{"vorbis", createMinimalOgg}, {"opus", createMinimalOpus}} {
			t.Run(tc.name+"/"+codec.name, func(t *testing.T) {
				data := codec.make("T", "A", "B", tc.extra...)
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
}
