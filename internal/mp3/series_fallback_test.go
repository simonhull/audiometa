package mp3

import (
	"bytes"
	"testing"

	binutil "github.com/simonhull/audiometa/internal/binary"
	"github.com/simonhull/audiometa/internal/types"
)

// TestResolveSeriesFallbacks pins the MP3 series resolution:
// Grouping -> Series(+part), then the parent directory name for the part.
// MP3 deliberately does not parse the title or album for a part.
func TestResolveSeriesFallbacks(t *testing.T) {
	tests := []struct {
		name       string
		tags       types.Tags
		path       string
		wantSeries string
		wantPart   string
	}{
		{"grouping with part", types.Tags{Grouping: "Stormlight #3"}, "/a/b.mp3", "Stormlight", "3"},
		{"explicit series wins over grouping", types.Tags{Series: "Explicit", Grouping: "Other #2"}, "/a/b.mp3", "Explicit", ""},
		{"explicit part wins over grouping part", types.Tags{Grouping: "Stormlight #3", SeriesPart: "7"}, "/a/b.mp3", "Stormlight", "7"},
		{"series without part falls back to path", types.Tags{Series: "Foo"}, "/books/Foo/2 - Title/b.mp3", "Foo", "2"},
		{"series without part and plain path", types.Tags{Series: "Foo"}, "/books/Foo/b.mp3", "Foo", ""},
		{"title is not parsed for part", types.Tags{Series: "Foo", Title: "Foo Book 4"}, "/x/b.mp3", "Foo", ""},
		{"no series no path fallback", types.Tags{}, "/books/Foo/2 - Title/b.mp3", "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sr := binutil.NewSafeReader(bytes.NewReader(nil), 0, tc.path)
			file := &types.File{Tags: tc.tags}
			resolveSeriesFallbacks(sr, file)
			if file.Tags.Series != tc.wantSeries || file.Tags.SeriesPart != tc.wantPart {
				t.Errorf("series/part = %q/%q, want %q/%q", file.Tags.Series, file.Tags.SeriesPart, tc.wantSeries, tc.wantPart)
			}
		})
	}
}
