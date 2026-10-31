package ogg

import (
	"bytes"
	"context"
	"testing"
)

// TestParse_SeriesFallbacks pins the post-parse series resolution:
// Grouping -> Series(+part), then the parent directory name for the part.
// Ogg deliberately does not parse the title or album for a part.
func TestParse_SeriesFallbacks(t *testing.T) {
	tests := []struct {
		name       string
		extra      []string
		path       string
		wantSeries string
		wantPart   string
	}{
		{"grouping with part", []string{"GROUPING=Stormlight #3"}, "/a/b.ogg", "Stormlight", "3"},
		{"explicit series wins over grouping", []string{"SERIES=Explicit", "GROUPING=Other #2"}, "/a/b.ogg", "Explicit", ""},
		{"explicit part wins over grouping part", []string{"GROUPING=Stormlight #3", "SERIESPART=7"}, "/a/b.ogg", "Stormlight", "7"},
		{"series without part falls back to path", []string{"SERIES=Foo"}, "/books/Foo/2 - Title/b.ogg", "Foo", "2"},
		{"series without part and plain path", []string{"SERIES=Foo"}, "/books/Foo/b.ogg", "Foo", ""},
		{"no series no path fallback", nil, "/books/Foo/2 - Title/b.ogg", "", ""},
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
				if file.Tags.Series != tc.wantSeries || file.Tags.SeriesPart != tc.wantPart {
					t.Errorf("series/part = %q/%q, want %q/%q", file.Tags.Series, file.Tags.SeriesPart, tc.wantSeries, tc.wantPart)
				}
			})
		}
	}
}

// TestParse_NoNarratorFromComposer pins the deliberate asymmetry with MP3 and M4A.
func TestParse_NoNarratorFromComposer(t *testing.T) {
	data := createMinimalOgg("T", "A", "B", "COMPOSER=Someone")
	file, err := (&parser{}).Parse(context.Background(), bytes.NewReader(data), int64(len(data)), "/a/b.ogg")
	if err != nil {
		t.Fatal(err)
	}
	if file.Tags.Narrator != "" {
		t.Errorf("Narrator = %q, want empty", file.Tags.Narrator)
	}
}
