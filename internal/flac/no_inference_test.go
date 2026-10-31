package flac

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
		comments     []string
		path         string
		wantSeries   string
		wantPart     string
		wantGrouping string
		wantNarr     string
	}{
		{"grouping is not split", []string{"GROUPING=Foo, Book 3"}, "/a/b.flac", "", "", "Foo, Book 3", ""},
		{"explicit tags used as-is", []string{"SERIES=Explicit", "SERIESPART=7", "GROUPING=Other #2"}, "/a/b.flac", "Explicit", "7", "Other #2", ""},
		{"path is not parsed", []string{"SERIES=Foo"}, "/books/Foo/Book 03/b.flac", "Foo", "", "", ""},
		{"title is not parsed", []string{"SERIES=Foo", "TITLE=Foo Book 4"}, "/x/b.flac", "Foo", "", "", ""},
		{"composer is not narrator", []string{"COMPOSER=Someone"}, "/a/b.flac", "", "", "", ""},
		{"explicit narrator", []string{"COMPOSER=Someone", "NARRATOR=Reader"}, "/a/b.flac", "", "", "", "Reader"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			data := buildFLAC(
				flacBlock(false, 0, streamInfoPayload(44100, 44100*10)),
				flacBlock(true, 4, vorbisCommentPayload(tc.comments...)),
			)
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
