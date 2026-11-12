package iff

import (
	"encoding/binary"
	"slices"
	"testing"

	"github.com/simonhull/audiometa/internal/types"
)

// id3v23 builds an ID3v2.3 tag of Latin-1 text frames, given as ID, text pairs.
func id3v23(pairs ...string) string {
	var body []byte
	for i := 0; i < len(pairs); i += 2 {
		text := append([]byte{0}, pairs[i+1]...)
		body = append(body, pairs[i]...)
		body = binary.BigEndian.AppendUint32(body, uint32(len(text)))
		body = append(body, 0, 0)
		body = append(body, text...)
	}
	size := len(body)
	tag := []byte{'I', 'D', '3', 3, 0, 0, byte(size >> 21 & 0x7F), byte(size >> 14 & 0x7F), byte(size >> 7 & 0x7F), byte(size & 0x7F)}
	return string(slices.Concat(tag, body))
}

// A WAV's ID3 frames outrank its native text tags, whatever the chunk order,
// and a native tag fills a field the ID3 tag leaves empty.
func TestParse_ID3OutranksNativeText(t *testing.T) {
	t.Parallel()
	le := binary.LittleEndian
	tests := []struct {
		name   string
		chunks []string
		title  string
		year   int
	}{
		{"id3 after native", []string{"LIST", info(le, "INAM", "Native", "ICRD", "2001"), "id3 ", id3v23("TIT2", "ID3", "TYER", "1999")}, "ID3", 1999},
		{"id3 before native", []string{"id3 ", id3v23("TIT2", "ID3", "TYER", "1999"), "LIST", info(le, "INAM", "Native", "ICRD", "2001")}, "ID3", 1999},
		{"native alone", []string{"LIST", info(le, "INAM", "Native", "ICRD", "2001")}, "Native", 2001},
		{"native fills what id3 lacks", []string{"LIST", info(le, "INAM", "Native", "ICRD", "2001"), "id3 ", id3v23("TPE1", "Someone")}, "Native", 2001},
		{"native year when the id3 year is out of range", []string{"LIST", info(le, "ICRD", "2001"), "id3 ", id3v23("TYER", "1500")}, "", 2001},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			parts := append([]string{"fmt ", pcmFmt(le)}, tc.chunks...)
			parts = append(parts, "data", "\x00\x00")
			f := parse(t, types.FormatWAV, riff(le, magicRIFF, parts...))
			if f.Tags.Title != tc.title || f.Tags.Year != tc.year {
				t.Errorf("Title %q, Year %d; want %q, %d", f.Tags.Title, f.Tags.Year, tc.title, tc.year)
			}
		})
	}
}
