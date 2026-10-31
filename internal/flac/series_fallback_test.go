package flac

import (
	"bytes"
	"context"
	"testing"
)

// TestParse_SeriesFallbacks pins the post-parse series resolution:
// Grouping -> Series(+part), then the parent directory name for the part.
// FLAC deliberately does not parse the title or album for a part.
func TestParse_SeriesFallbacks(t *testing.T) {
	tests := []struct {
		name       string
		comments   []string
		path       string
		wantSeries string
		wantPart   string
	}{
		{"grouping with part", []string{"GROUPING=Stormlight #3"}, "/a/b.flac", "Stormlight", "3"},
		{"explicit series wins over grouping", []string{"SERIES=Explicit", "GROUPING=Other #2"}, "/a/b.flac", "Explicit", ""},
		{"explicit part wins over grouping part", []string{"GROUPING=Stormlight #3", "SERIESPART=7"}, "/a/b.flac", "Stormlight", "7"},
		{"series without part falls back to path", []string{"SERIES=Foo"}, "/books/Foo/2 - Title/b.flac", "Foo", "2"},
		{"series without part and plain path", []string{"SERIES=Foo"}, "/books/Foo/b.flac", "Foo", ""},
		{"title is not parsed for part", []string{"SERIES=Foo", "TITLE=Foo Book 4"}, "/x/b.flac", "Foo", ""},
		{"no series no path fallback", []string{"TITLE=T"}, "/books/Foo/2 - Title/b.flac", "", ""},
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
			if file.Tags.Series != tc.wantSeries || file.Tags.SeriesPart != tc.wantPart {
				t.Errorf("series/part = %q/%q, want %q/%q", file.Tags.Series, file.Tags.SeriesPart, tc.wantSeries, tc.wantPart)
			}
		})
	}
}

// TestParse_NoNarratorFromComposer pins the deliberate asymmetry with MP3 and M4A.
func TestParse_NoNarratorFromComposer(t *testing.T) {
	data := buildFLAC(
		flacBlock(false, 0, streamInfoPayload(44100, 44100*10)),
		flacBlock(true, 4, vorbisCommentPayload("COMPOSER=Someone")),
	)
	file, err := (&parser{}).Parse(context.Background(), bytes.NewReader(data), int64(len(data)), "/a/b.flac")
	if err != nil {
		t.Fatal(err)
	}
	if file.Tags.Narrator != "" {
		t.Errorf("Narrator = %q, want empty", file.Tags.Narrator)
	}
}
