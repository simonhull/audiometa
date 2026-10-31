package mp3

import (
	"bytes"
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

// applyID3v1 copies ID3v1 fields into tags that are still empty.
func applyID3v1(file *types.File, tag id3v1Tag) {
	t := &file.Tags
	if t.Title == "" {
		t.Title = tag.Title
	}
	if t.Artist == "" {
		t.Artist = tag.Artist
	}
	if t.Album == "" {
		t.Album = tag.Album
	}
	if t.Comment == "" {
		t.Comment = tag.Comment
	}
	if t.Year == 0 {
		t.Year = tag.Year
	}
	if t.TrackNumber == 0 {
		t.TrackNumber = tag.Track
	}
}
