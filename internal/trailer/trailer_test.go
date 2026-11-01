package trailer

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"slices"
	"testing"
)

// audio stands in for the bytes before any trailing tag.
var audio = bytes.Repeat([]byte{0xFF, 0xFB, 0x90, 0x00}, 64)

func id3v1() []byte {
	tag := make([]byte, 128)
	copy(tag, "TAG")
	return tag
}

// ape builds an APEv2 tag with a body of n item bytes, optionally with a header.
func ape(n int, withHeader bool) []byte {
	footer := func(flags uint32) []byte {
		b := make([]byte, 32)
		copy(b, "APETAGEX")
		binary.LittleEndian.PutUint32(b[8:], 2000)
		binary.LittleEndian.PutUint32(b[12:], uint32(n+32))
		binary.LittleEndian.PutUint32(b[16:], 1)
		binary.LittleEndian.PutUint32(b[20:], flags)
		return b
	}
	var tag []byte
	var hasHeader uint32
	if withHeader {
		hasHeader = 1 << 31
		tag = append(tag, footer(hasHeader|1<<29)...)
	}
	tag = append(tag, bytes.Repeat([]byte{'a'}, n)...)
	return append(tag, footer(hasHeader)...)
}

func lyrics3v2(body string) []byte {
	content := "LYRICSBEGIN" + body
	return fmt.Appendf(nil, "%s%06dLYRICS200", content, len(content))
}

// id3v2WithFooter builds an appended ID3v2.4 tag of n frame bytes.
func id3v2WithFooter(n int) []byte {
	synchsafe := []byte{byte(n >> 21 & 0x7F), byte(n >> 14 & 0x7F), byte(n >> 7 & 0x7F), byte(n & 0x7F)}
	header := append([]byte{'I', 'D', '3', 4, 0, 0x10}, synchsafe...)
	footer := append([]byte{'3', 'D', 'I', 4, 0, 0x10}, synchsafe...)
	return slices.Concat(header, bytes.Repeat([]byte{0}, n), footer)
}

func TestStart(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		trailers [][]byte
	}{
		{"no trailing tags", nil},
		{"ID3v1", [][]byte{id3v1()}},
		{"APEv2 without header", [][]byte{ape(40, false)}},
		{"APEv2 with header", [][]byte{ape(40, true)}},
		{"Lyrics3v2", [][]byte{lyrics3v2("IND0000210")}},
		{"ID3v2 with footer", [][]byte{id3v2WithFooter(30)}},
		{"APEv2, Lyrics3v2 and ID3v1 stacked", [][]byte{ape(40, true), lyrics3v2("IND0000210"), id3v1()}},
		{"ID3v1 before APEv2", [][]byte{id3v1(), ape(12, false)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			data := slices.Concat(append([][]byte{audio}, tt.trailers...)...)
			got, err := Start(bytes.NewReader(data), 0, int64(len(data)))
			if err != nil {
				t.Fatal(err)
			}
			if got != int64(len(audio)) {
				t.Errorf("Start = %d, want %d", got, len(audio))
			}
		})
	}
}

func TestStart_LeavesBrokenBlocksAsAudio(t *testing.T) {
	t.Parallel()
	tooBig := ape(40, false)
	binary.LittleEndian.PutUint32(tooBig[len(tooBig)-20:], 1<<30) // tag size larger than the range
	noBegin := lyrics3v2("IND0000210")
	copy(noBegin, "XXXXXXXXXXX") // LYRICSBEGIN missing
	noID3 := id3v2WithFooter(30)
	copy(noID3, "XXX") // footer without its header

	for name, trailer := range map[string][]byte{
		"APEv2 larger than the range":   tooBig,
		"Lyrics3v2 without LYRICSBEGIN": noBegin,
		"ID3v2 footer without header":   noID3,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			data := slices.Concat(audio, trailer)
			got, err := Start(bytes.NewReader(data), 0, int64(len(data)))
			if err != nil {
				t.Fatal(err)
			}
			if got != int64(len(data)) {
				t.Errorf("Start = %d, want %d (block kept as audio)", got, len(data))
			}
		})
	}
}

func TestStart_RespectsRangeStart(t *testing.T) {
	t.Parallel()
	data := id3v1() // the whole range is one tag-shaped block
	got, err := Start(bytes.NewReader(data), 1, int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	if got != int64(len(data)) {
		t.Errorf("Start = %d, want %d: a block may not reach before start", got, len(data))
	}
}

// failingReader fails every read.
type failingReader struct{}

var errDisk = errors.New("disk error")

func (failingReader) ReadAt([]byte, int64) (int, error) { return 0, errDisk }

func TestStart_ReadErrorsPassThrough(t *testing.T) {
	t.Parallel()
	if _, err := Start(failingReader{}, 0, 1000); !errors.Is(err, errDisk) {
		t.Errorf("err = %v, want the read error", err)
	}
}
