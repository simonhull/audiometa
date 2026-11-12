package audiometa_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/simonhull/audiometa"
)

// id3v1OnlyMP3 writes an MP3 with no ID3v2 tag and a full ID3v1.1 trailer
// (title, artist, album, year 2001, comment, track 5, genre 17) into a temp
// dir and returns its path.
func id3v1OnlyMP3(t *testing.T) string {
	t.Helper()
	src := readTestdata(t, "testdata/sample.mp3")
	skip := 10 + (int(src[6])<<21 | int(src[7])<<14 | int(src[8])<<7 | int(src[9]))
	if src[5]&0x10 != 0 {
		skip += 10
	}
	trailer := make([]byte, id3v1Len)
	copy(trailer, "TAG")
	copy(trailer[3:], "V1 Title")
	copy(trailer[33:], "V1 Artist")
	copy(trailer[63:], "V1 Album")
	copy(trailer[93:], "2001")
	copy(trailer[97:], "V1 comment")
	trailer[126], trailer[127] = 5, 17
	p := filepath.Join(t.TempDir(), "v1only.mp3")
	if err := os.WriteFile(p, append(bytes.Clone(src[skip:]), trailer...), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestWriteID3v1_OnlyTrailerModes(t *testing.T) { //nolint:cyclop,gocyclo // one scenario table with a check per mode
	t.Parallel()
	noEdit := func(*audiometa.File) error { return nil }
	checkValues := func(t *testing.T, p, title string) {
		t.Helper()
		f := openFile(t, p)
		if f.Tags.Title != title || f.Tags.Artist != "V1 Artist" || f.Tags.Album != "V1 Album" ||
			f.Tags.Year != 2001 || f.Tags.TrackNumber != 5 || f.Tags.Comment != "V1 comment" {
			t.Errorf("tags = %q, %q, %q, %d, %d, %q", f.Tags.Title, f.Tags.Artist, f.Tags.Album, f.Tags.Year, f.Tags.TrackNumber, f.Tags.Comment)
		}
	}

	for name, mode := range map[string]audiometa.ID3v1Mode{
		"keep": audiometa.ID3v1Keep, "update": audiometa.ID3v1Update, "remove": audiometa.ID3v1Remove,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			p := id3v1OnlyMP3(t)
			if err := audiometa.Write(p, setTitle("New"), audiometa.WithID3v1(mode)); err != nil {
				t.Fatal(err)
			}
			checkValues(t, p, "New")
			tr := trailerOf(readFileBytes(t, p))
			switch mode {
			case audiometa.ID3v1Update:
				if tr == nil || trailerText(tr, 3, 33) != "New" || tr[127] != 17 {
					t.Errorf("trailer = %q", tr)
				}
			case audiometa.ID3v1Remove:
				if tr != nil {
					t.Error("trailer still present")
				}
			case audiometa.ID3v1Keep:
				if tr == nil || trailerText(tr, 3, 33) != "V1 Title" {
					t.Errorf("trailer = %q", tr)
				}
			}
		})
	}

	t.Run("remove with no tag edits", func(t *testing.T) {
		p := id3v1OnlyMP3(t)
		if err := audiometa.Write(p, noEdit, audiometa.WithID3v1(audiometa.ID3v1Remove)); err != nil {
			t.Fatal(err)
		}
		if trailerOf(readFileBytes(t, p)) != nil {
			t.Error("trailer still present")
		}
		checkValues(t, p, "V1 Title")
	})
}
