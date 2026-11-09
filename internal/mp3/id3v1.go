package mp3

import (
	"bytes"
	"strconv"
	"strings"

	binutil "github.com/simonhull/audiometa/internal/binary"
	"github.com/simonhull/audiometa/internal/types"
)

const id3v1Size = 128

// id3v1Tag holds the text fields of an ID3v1 / ID3v1.1 trailer.
type id3v1Tag struct {
	Title, Artist, Album, Comment string
	Year, Track                   int
}

// readID3v1 reads the ID3v1 trailer from the last 128 bytes of the file. The
// genre byte is ignored.
func readID3v1(sr *binutil.SafeReader, size int64) (id3v1Tag, bool) {
	if size < id3v1Size {
		return id3v1Tag{}, false
	}
	buf := make([]byte, id3v1Size)
	if err := sr.ReadAt(buf, size-id3v1Size, "ID3v1 tag"); err != nil {
		return id3v1Tag{}, false
	}
	if !bytes.HasPrefix(buf, []byte("TAG")) {
		return id3v1Tag{}, false
	}

	tag := id3v1Tag{
		Title:  id3v1String(buf[3:33]),
		Artist: id3v1String(buf[33:63]),
		Album:  id3v1String(buf[63:93]),
		Year:   parseYear(id3v1String(buf[93:97])),
	}
	if buf[125] == 0 && buf[126] != 0 { // ID3v1.1: track number in the last comment bytes
		tag.Track = int(buf[126])
		tag.Comment = id3v1String(buf[97:125])
	} else {
		tag.Comment = id3v1String(buf[97:127])
	}
	return tag, true
}

// id3v1String decodes a fixed-width ID3v1 field: cut at the first NUL, decode as
// ISO-8859-1, trim surrounding spaces.
func id3v1String(b []byte) string {
	if i := bytes.IndexByte(b, 0); i >= 0 {
		b = b[:i]
	}
	return strings.TrimSpace(decodeText(b, 0))
}

// id3v1Fallback reports whether the reader takes tags from the ID3v1 trailer:
// only when the ID3v2 tag left both Title and Artist empty.
func id3v1Fallback(t *types.Tags) bool {
	return t.Title == "" && t.Artist == ""
}

// id3v1Fills returns the fields that applyID3v1 fills from tag, given what the
// ID3v2 tag already set in t, in the encoding of types.FieldEdit values and in
// Field order.
func id3v1Fills(t *types.Tags, tag id3v1Tag) []types.FieldEdit {
	var fills []types.FieldEdit
	text := func(f types.Field, have, v1 string) {
		if have == "" && v1 != "" {
			fills = append(fills, types.FieldEdit{Field: f, Values: []string{v1}})
		}
	}
	number := func(f types.Field, have, v1 int) {
		if have == 0 && v1 != 0 {
			fills = append(fills, types.FieldEdit{Field: f, Values: []string{strconv.Itoa(v1)}})
		}
	}
	text(types.FieldTitle, t.Title, tag.Title)
	text(types.FieldArtist, t.Artist, tag.Artist)
	text(types.FieldAlbum, t.Album, tag.Album)
	number(types.FieldYear, t.Year, tag.Year)
	number(types.FieldTrackNumber, t.TrackNumber, tag.Track)
	text(types.FieldComment, t.Comment, tag.Comment)
	return fills
}

// applyID3v1 copies ID3v1 fields into tags that are still empty.
func applyID3v1(file *types.File, tag id3v1Tag) {
	t := &file.Tags
	for _, f := range id3v1Fills(t, tag) {
		v := f.Values[0]
		switch f.Field { //nolint:exhaustive // id3v1Fills yields only the six ID3v1 fields
		case types.FieldTitle:
			t.Title = v
		case types.FieldArtist:
			t.Artist = v
		case types.FieldAlbum:
			t.Album = v
		case types.FieldComment:
			t.Comment = v
		case types.FieldYear:
			t.Year = atoi(v)
		case types.FieldTrackNumber:
			t.TrackNumber = atoi(v)
		default:
		}
	}
}

// atoi parses a decimal string produced by [strconv.Itoa].
func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}
