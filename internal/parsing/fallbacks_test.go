package parsing

import (
	"testing"

	"github.com/simonhull/audiometa/internal/types"
)

func TestResolveSeriesFallbacks(t *testing.T) {
	tests := []struct {
		name       string
		tags       types.Tags
		path       string
		src        SeriesSource
		wantSeries string
		wantPart   string
	}{
		{"grouping fills series and part", types.Tags{Grouping: "Stormlight #3"}, "", SeriesSource{}, "Stormlight", "3"},
		{"existing series kept", types.Tags{Series: "A", Grouping: "B #2"}, "", SeriesSource{}, "A", ""},
		{"existing part kept", types.Tags{Grouping: "B #2", SeriesPart: "9"}, "", SeriesSource{Title: "X Book 4"}, "B", "9"},
		{"path fallback", types.Tags{Series: "Foo"}, "/b/Foo/2 - T/f.mp3", SeriesSource{}, "Foo", "2"},
		{"no series means no part lookup", types.Tags{}, "/b/Foo/2 - T/f.mp3", SeriesSource{Title: "X Book 4"}, "", ""},
		{"explicit beats title", types.Tags{Series: "Foo"}, "", SeriesSource{ExplicitParts: []string{"", "5"}, Title: "Foo Book 4"}, "Foo", "5"},
		{"title beats album", types.Tags{Series: "Foo"}, "", SeriesSource{Title: "Foo Book 4", Album: "Foo Book 6"}, "Foo", "4"},
		{"album beats path", types.Tags{Series: "Foo"}, "/b/Foo/2 - T/f.mp3", SeriesSource{Album: "Foo Book 6"}, "Foo", "6"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tags := tc.tags
			ResolveSeriesFallbacks(&tags, tc.path, tc.src)
			if tags.Series != tc.wantSeries || tags.SeriesPart != tc.wantPart {
				t.Errorf("series/part = %q/%q, want %q/%q", tags.Series, tags.SeriesPart, tc.wantSeries, tc.wantPart)
			}
		})
	}
}

func TestApplyNarratorFallback(t *testing.T) {
	tests := []struct {
		name string
		tags types.Tags
		want string
	}{
		{"uses first composer", types.Tags{Composers: []string{"A", "B"}}, "A"},
		{"keeps narrator", types.Tags{Narrator: "N", Composers: []string{"A"}}, "N"},
		{"no composers", types.Tags{}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ApplyNarratorFallback(&tc.tags)
			if tc.tags.Narrator != tc.want {
				t.Errorf("Narrator = %q, want %q", tc.tags.Narrator, tc.want)
			}
		})
	}
}
