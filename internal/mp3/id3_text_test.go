package mp3

import (
	"slices"
	"testing"
	"unicode/utf8"
)

func TestDecodeText(t *testing.T) {
	cases := []struct {
		name string
		enc  byte
		in   []byte
		want string
	}{
		{"latin1 e-acute", 0, []byte("Caf\xe9"), "Café"},
		{"utf8 in latin1 frame", 0, []byte("Café"), "Café"},
		{"utf8 invalid bytes", 3, []byte("\xe9t\xe9"), "été"},
		{"utf8 trailing nul", 3, []byte("Café\x00"), "Café"},
		{"single nul", 0, []byte("Title\x00"), "Title"},
		{"double nul", 0, []byte("Title\x00\x00"), "Title"},
		{"unknown encoding", 7, []byte("\xfc"), "ü"},
		{"empty", 0, nil, ""},
		{"utf16 terminator", 1, []byte{0xFF, 0xFE, 0x41, 0x00, 0x00, 0x00}, "A"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := decodeText(tc.in, tc.enc)
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
			if !utf8.ValidString(got) {
				t.Errorf("result %q is not valid UTF-8", got)
			}
		})
	}
}

func TestDecodeUTF16(t *testing.T) {
	cases := []struct {
		name string
		fn   func([]byte) string
		in   []byte
		want string
	}{
		{"LE BOM", decodeUTF16, []byte{0xFF, 0xFE, 'H', 0, 0xE9, 0}, "Hé"},
		{"BE BOM", decodeUTF16, []byte{0xFE, 0xFF, 0, 'H', 0, 0xE9}, "Hé"},
		{"no BOM is BE", decodeUTF16, []byte{0, 'H', 0, 0xE9}, "Hé"},
		{"too short", decodeUTF16, []byte{0xFF}, ""},
		{"BOM only", decodeUTF16, []byte{0xFF, 0xFE}, ""},
		{"odd length LE", decodeUTF16LE, []byte{'H', 0, 'i', 0, 0x41}, "Hi"},
		{"odd length BE", decodeUTF16BE, []byte{0, 'H', 0, 'i', 0x41}, "Hi"},
		{"surrogate LE", decodeUTF16LE, []byte{0x3D, 0xD8, 0x00, 0xDE}, "\U0001F600"},
		{"surrogate BE", decodeUTF16BE, []byte{0xD8, 0x3D, 0xDE, 0x00}, "\U0001F600"},
		{"surrogate via BOM", decodeUTF16, []byte{0xFF, 0xFE, 0x3D, 0xD8, 0x00, 0xDE}, "\U0001F600"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.fn(tc.in); got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestParse_TextDecoding(t *testing.T) {
	for _, v := range []byte{3, 4} {
		t.Run("v"+string('0'+v), func(t *testing.T) {
			utf16Book := append(utf16Text("Book"), 0, 0)
			frames := [][]byte{
				id3Frame(v, "TALB", append([]byte{0x00}, "Caf\xe9\x00"...)),
				id3Frame(v, "TIT2", utf16Book),
				id3Frame(v, "TCON", append([]byte{0x03}, "Fantasy\x00Science Fiction"...)),
				id3Frame(v, "TPE1", append([]byte{0x03}, "A\x00B"...)),
				id3Frame(v, "TXXX", append([]byte{0x00}, "NARRATOR\x00Jane Doe\x00"...)),
			}
			f := parseBytes(t, append(id3Tag(v, frames...), mpegFrame...))
			if f.Tags.Album != "Café" {
				t.Errorf("Album = %q", f.Tags.Album)
			}
			if f.Tags.Title != "Book" {
				t.Errorf("Title = %q", f.Tags.Title)
			}
			if want := []string{"Fantasy", "Science Fiction"}; !slices.Equal(f.Tags.Genres, want) {
				t.Errorf("Genres = %q, want %q", f.Tags.Genres, want)
			}
			if f.Tags.Artist != "A" {
				t.Errorf("Artist = %q", f.Tags.Artist)
			}
			if f.Tags.Narrator != "Jane Doe" {
				t.Errorf("Narrator = %q", f.Tags.Narrator)
			}
		})
	}
}

func TestParse_CommentSelection(t *testing.T) {
	type c struct{ desc, text string }
	cases := []struct {
		name string
		in   []c
		want string
	}{
		{"real then iTunNORM", []c{{"", "real"}, {"iTunNORM", " 0000"}}, "real"},
		{"iTunNORM then real", []c{{"iTunNORM", " 0000"}, {"", "real"}}, "real"},
		{"only iTunSMPB", []c{{"iTunSMPB", " 00000000"}}, ""},
		{"only described", []c{{"Description", "x"}}, "x"},
		{"described then empty desc", []c{{"Description", "x"}, {"", "y"}}, "y"},
		{"first empty desc wins", []c{{"", "first"}, {"", "second"}}, "first"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var frames [][]byte
			for _, cm := range tc.in {
				payload := append([]byte{0x00}, "eng"...)
				payload = append(payload, cm.desc...)
				payload = append(payload, 0)
				payload = append(payload, cm.text...)
				frames = append(frames, id3Frame(4, "COMM", payload))
			}
			f := parseBytes(t, append(id3Tag(4, frames...), mpegFrame...))
			if f.Tags.Comment != tc.want {
				t.Errorf("Comment = %q, want %q", f.Tags.Comment, tc.want)
			}
		})
	}
}
