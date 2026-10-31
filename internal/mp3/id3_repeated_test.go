package mp3

import (
	"slices"
	"testing"

	"github.com/simonhull/audiometa/internal/types"
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

// Frames that the writer cannot combine (TDRC with TYER, v2.3 and v2.4 alike)
// still occur in files: the highest-precedence key must fill the typed field
// whatever order the frames come in.
func TestParse_FramePrecedenceBeatsFileOrder(t *testing.T) {
	for _, version := range []byte{3, 4} {
		data := append(id3Tag(version,
			id3Frame(version, "TYER", latin1Text("1999")),
			id3Frame(version, "TIT1", latin1Text("Low Grouping")),
			id3Frame(version, "TDRC", latin1Text("2021")),
			id3Frame(version, "GRP1", latin1Text("High Grouping")),
		), mpegFrame...)
		f := parseBytes(t, data)
		types.BindFormat(&f.Tags, types.FormatMP3)
		if f.Tags.Year != 2021 {
			t.Errorf("v2.%d: Year = %d, want 2021 (TDRC outranks TYER)", version, f.Tags.Year)
		}
		if f.Tags.Grouping != "High Grouping" {
			t.Errorf("v2.%d: Grouping = %q, want GRP1 (outranks TIT1)", version, f.Tags.Grouping)
		}
	}
}
