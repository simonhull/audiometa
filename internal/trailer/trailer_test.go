package trailer

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"slices"
	"testing"

	binutil "github.com/simonhull/audiometa/internal/binary"
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
			got, err := Start(t.Context(), sr(data), 0, int64(len(data)))
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
			got, err := Start(t.Context(), sr(data), 0, int64(len(data)))
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
	got, err := Start(t.Context(), sr(data), 1, int64(len(data)))
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
	if _, err := Start(t.Context(), binutil.NewSafeReader(failingReader{}, 1000, ""), 0, 1000); !errors.Is(err, errDisk) {
		t.Errorf("err = %v, want the read error", err)
	}
}

// sr wraps data in a SafeReader.
func sr(data []byte) *binutil.SafeReader {
	return binutil.NewSafeReader(bytes.NewReader(data), int64(len(data)), "")
}

func TestStart_RejectsLookalikes(t *testing.T) {
	t.Parallel()
	badVersion := ape(40, false)
	binary.LittleEndian.PutUint32(badVersion[len(badVersion)-24:], 3000)
	headerFlagNoHeader := ape(40, false)
	binary.LittleEndian.PutUint32(headerFlagNoHeader[len(headerFlagNoHeader)-12:], 1<<31)
	signedLyrics := lyrics3v2("IND0000210")
	copy(signedLyrics[len(signedLyrics)-15:], "+00021")
	notSynchsafe := id3v2WithFooter(30)
	notSynchsafe[len(notSynchsafe)-1] |= 0x80
	noFooterFlag := id3v2WithFooter(30)
	noFooterFlag[len(noFooterFlag)-5] &^= 0x10
	mismatched := id3v2WithFooter(30)
	mismatched[3] = 3 // header says v2.3, footer says v2.4
	headerVersionDiffers := ape(40, true)
	binary.LittleEndian.PutUint32(headerVersionDiffers[8:], 1000)
	headerSizeDiffers := ape(40, true)
	binary.LittleEndian.PutUint32(headerSizeDiffers[12:], 99)
	headerNotMarked := ape(40, true)
	binary.LittleEndian.PutUint32(headerNotMarked[20:], 1<<31) // bit 29 ("this is the header") clear

	for name, trailer := range map[string][]byte{
		"APE version 3000":           badVersion,
		"APE header flag, no header": headerFlagNoHeader,
		"Lyrics3v2 signed size":      signedLyrics,
		"ID3v2 size not synchsafe":   notSynchsafe,
		"ID3v2 footer flag missing":  noFooterFlag,
		"ID3v2 header differs":       mismatched,
		"APE header version differs": headerVersionDiffers,
		"APE header size differs":    headerSizeDiffers,
		"APE header not marked":      headerNotMarked,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			data := slices.Concat(audio, trailer)
			got, err := Start(t.Context(), sr(data), 0, int64(len(data)))
			if err != nil {
				t.Fatal(err)
			}
			if got != int64(len(data)) {
				t.Errorf("Start = %d, want %d (look-alike kept as audio)", got, len(data))
			}
		})
	}
}

func TestStart_TAGInsideAPEIsNotID3v1(t *testing.T) {
	t.Parallel()
	tag := ape(200, true)
	copy(tag[len(tag)-128:], "TAG") // inside the item bytes, 128 bytes from the end
	data := slices.Concat(audio, tag)
	got, err := Start(t.Context(), sr(data), 0, int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	if got != int64(len(audio)) {
		t.Errorf("Start = %d, want %d (whole APE tag)", got, len(audio))
	}
}

func TestStart_APEv1(t *testing.T) {
	t.Parallel()
	tag := ape(40, false)
	binary.LittleEndian.PutUint32(tag[len(tag)-24:], 1000)
	data := slices.Concat(audio, tag)
	got, err := Start(t.Context(), sr(data), 0, int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	if got != int64(len(audio)) {
		t.Errorf("Start = %d, want %d", got, len(audio))
	}
}

func TestStart_Canceled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	data := slices.Concat(audio, id3v1())
	if _, err := Start(ctx, sr(data), 0, int64(len(data))); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

func FuzzStart(f *testing.F) {
	f.Add(slices.Concat(audio, ape(40, true), lyrics3v2("IND0000210"), id3v1()))
	f.Add(slices.Concat(audio, id3v2WithFooter(30)))
	f.Add([]byte("TAGAPETAGEXLYRICS2003DI"))
	f.Fuzz(func(t *testing.T, data []byte) {
		end := int64(len(data))
		start := end / 3
		got, err := Start(t.Context(), sr(data), start, end)
		if err != nil {
			t.Fatalf("Start on in-memory data: %v", err)
		}
		if got < start || got > end {
			t.Fatalf("Start = %d, outside [%d, %d]", got, start, end)
		}
		again, err := Start(t.Context(), sr(data), start, got)
		if err != nil || again != got {
			t.Fatalf("Start is not stable: Start(%d) = %d, then Start(%d) = %d, %v", end, got, got, again, err)
		}
	})
}
