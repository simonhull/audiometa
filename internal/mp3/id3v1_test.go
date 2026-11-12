package mp3

import (
	"bytes"
	"strings"
	"testing"

	binutil "github.com/simonhull/audiometa/internal/binary"
	"github.com/simonhull/audiometa/internal/types"
)

func TestEncodeID3v1(t *testing.T) {
	t.Parallel()
	var old [id3v1Size]byte
	old[127] = 17

	yearNULs := func(t *testing.T, b [id3v1Size]byte) {
		t.Helper()
		if !bytes.Equal(b[93:97], make([]byte, 4)) {
			t.Errorf("year bytes = %q, want NULs", b[93:97])
		}
	}
	tests := []struct {
		name string
		tags types.Tags
		want id3v1Tag
		raw  func(t *testing.T, b [id3v1Size]byte) // extra byte-level checks
	}{
		{
			name: "ASCII round trip",
			tags: types.Tags{Title: "Title", Artist: "Artist", Album: "Album", Year: 2021, Comment: "note", TrackNumber: 7},
			want: id3v1Tag{Title: "Title", Artist: "Artist", Album: "Album", Year: 2021, Comment: "note", Track: 7},
		},
		{
			name: "long title is cut to 30 bytes",
			tags: types.Tags{Title: strings.Repeat("t", 40)},
			want: id3v1Tag{Title: strings.Repeat("t", 30)},
		},
		{
			name: "Latin-1 characters become one byte",
			tags: types.Tags{Artist: "Björk"},
			want: id3v1Tag{Artist: "Björk"},
			raw: func(t *testing.T, b [id3v1Size]byte) {
				t.Helper()
				if b[33+2] != 0xF6 {
					t.Errorf("byte of ö = %#x, want 0xF6", b[33+2])
				}
			},
		},
		{
			name: "characters outside Latin-1 become question marks",
			tags: types.Tags{Title: "東京"},
			want: id3v1Tag{Title: "??"},
		},
		{
			name: "a NUL ends the field",
			tags: types.Tags{Title: "ab\x00cd"},
			want: id3v1Tag{Title: "ab"},
		},
		{
			name: "a cut never splits a character",
			tags: types.Tags{Album: strings.Repeat("é", 31)},
			want: id3v1Tag{Album: strings.Repeat("é", 30)},
		},
		{
			name: "year zero is four NULs",
			tags: types.Tags{Title: "x"},
			want: id3v1Tag{Title: "x"},
			raw:  yearNULs,
		},
		{
			name: "years outside 1900 to 2100 are four NULs",
			tags: types.Tags{Year: 12345},
			raw:  yearNULs,
		},
		{
			name: "a track number cuts the comment to 28 bytes",
			tags: types.Tags{Comment: strings.Repeat("c", 40), TrackNumber: 7},
			want: id3v1Tag{Comment: strings.Repeat("c", 28), Track: 7},
			raw: func(t *testing.T, b [id3v1Size]byte) {
				t.Helper()
				if b[125] != 0 || b[126] != 7 {
					t.Errorf("track bytes = %d, %d, want 0, 7", b[125], b[126])
				}
			},
		},
		{
			name: "track zero keeps a 30-byte comment",
			tags: types.Tags{Comment: strings.Repeat("c", 40)},
			want: id3v1Tag{Comment: strings.Repeat("c", 30)},
		},
		{
			name: "track above 255 keeps a 30-byte comment",
			tags: types.Tags{Comment: strings.Repeat("c", 40), TrackNumber: 256},
			want: id3v1Tag{Comment: strings.Repeat("c", 30)},
		},
		{
			name: "empty tags",
			tags: types.Tags{},
			want: id3v1Tag{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := encodeID3v1(&tt.tags, old)
			if string(got[:3]) != "TAG" {
				t.Fatalf("trailer starts with %q, want TAG", got[:3])
			}
			if got[127] != 17 {
				t.Errorf("genre = %d, want the old 17", got[127])
			}
			sr := binutil.NewSafeReader(bytes.NewReader(got[:]), id3v1Size, "")
			dec, ok := readID3v1(sr, id3v1Size)
			if !ok {
				t.Fatal("readID3v1 does not recognize the trailer")
			}
			if dec != tt.want {
				t.Errorf("decoded %+v, want %+v", dec, tt.want)
			}
			if tt.raw != nil {
				tt.raw(t, got)
			}
		})
	}
}
