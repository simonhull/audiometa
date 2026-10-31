package audiometa_test

import (
	"slices"
	"testing"

	"github.com/simonhull/audiometa"
)

func TestRawTagParity(t *testing.T) {
	tests := []struct {
		path     string
		titleKey string
	}{
		{"testdata/sample.mp3", "TIT2"},
		{"testdata/sample.m4a", "\xA9nam"},
		{"testdata/sample.m4b", "\xA9nam"},
		{"testdata/sample.flac", "TITLE"},
		{"testdata/sample.ogg", "TITLE"},
		{"testdata/sample.opus", "TITLE"},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			f, err := audiometa.Open(tt.path)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			n := 0
			for range f.Tags.All() {
				n++
			}
			if n == 0 {
				t.Error("Tags.All() is empty")
			}
			if f.Tags.Title == "" {
				t.Skip("fixture has no title")
			}
			if got := f.Tags.Get(tt.titleKey); !slices.Equal(got, []string{f.Tags.Title}) {
				t.Errorf("Get(%q) = %q, want [%q]", tt.titleKey, got, f.Tags.Title)
			}
			if f.Tags.Artist != "" && !slices.Contains(f.Tags.Artists, f.Tags.Artist) {
				t.Errorf("Artists = %q, want to contain %q", f.Tags.Artists, f.Tags.Artist)
			}
		})
	}
}
