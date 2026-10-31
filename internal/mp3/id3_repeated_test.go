package mp3

import (
	"slices"
	"testing"
)

// Repeated single-valued text frames: the first frame wins for the typed
// field, while the multi-valued Artists collects every value.
func TestParse_RepeatedTextFramesFirstWins(t *testing.T) {
	for _, version := range []byte{3, 4} {
		data := append(id3Tag(version,
			id3Frame(version, "TIT2", latin1Text("First Title")),
			id3Frame(version, "TPE1", latin1Text("Artist One")),
			id3Frame(version, "TIT2", latin1Text("Second Title")),
			id3Frame(version, "TPE1", latin1Text("Artist Two")),
		), mpegFrame...)
		f := parseBytes(t, data)
		if f.Tags.Title != "First Title" {
			t.Errorf("v2.%d: Title = %q, want the first TIT2", version, f.Tags.Title)
		}
		if f.Tags.Artist != "Artist One" {
			t.Errorf("v2.%d: Artist = %q, want the first TPE1", version, f.Tags.Artist)
		}
		if want := []string{"Artist One", "Artist Two"}; !slices.Equal(f.Tags.Artists, want) {
			t.Errorf("v2.%d: Artists = %q, want %q", version, f.Tags.Artists, want)
		}
	}
}
