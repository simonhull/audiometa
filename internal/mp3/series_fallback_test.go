package mp3

import (
	"bytes"
	"context"
	"testing"
)

// TestParse_SeriesFallbacks pins the MP3 series resolution:
// Grouping -> Series(+part), then the parent directory name for the part.
// MP3 deliberately does not parse the title or album for a part.
func TestParse_SeriesFallbacks(t *testing.T) {
	txxx := func(k, v string) []byte { return id3Frame(4, "TXXX", latin1Text(k+"\x00"+v)) }
	text := func(id, v string) []byte { return id3Frame(4, id, latin1Text(v)) }
	tests := []struct {
		name       string
		frames     [][]byte
		path       string
		wantSeries string
		wantPart   string
	}{
		{"grouping with part", [][]byte{text("GRP1", "Stormlight #3")}, "/a/b.mp3", "Stormlight", "3"},
		{"explicit series wins over grouping", [][]byte{txxx("SERIES", "Explicit"), text("GRP1", "Other #2")}, "/a/b.mp3", "Explicit", ""},
		{"explicit part wins over grouping part", [][]byte{text("GRP1", "Stormlight #3"), txxx("SERIES-PART", "7")}, "/a/b.mp3", "Stormlight", "7"},
		{"series without part falls back to path", [][]byte{txxx("SERIES", "Foo")}, "/books/Foo/2 - Title/b.mp3", "Foo", "2"},
		{"series without part and plain path", [][]byte{txxx("SERIES", "Foo")}, "/books/Foo/b.mp3", "Foo", ""},
		{"title is not parsed for part", [][]byte{txxx("SERIES", "Foo"), text("TIT2", "Foo Book 4")}, "/x/b.mp3", "Foo", ""},
		{"no series no path fallback", [][]byte{text("TIT2", "T")}, "/books/Foo/2 - Title/b.mp3", "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			data := append(id3Tag(4, tc.frames...), mpegFrame...)
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
