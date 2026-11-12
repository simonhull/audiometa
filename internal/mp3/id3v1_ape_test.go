package mp3

import (
	"bytes"
	"encoding/binary"
	"testing"

	binutil "github.com/simonhull/audiometa/internal/binary"
	"github.com/simonhull/audiometa/internal/types"
)

// apeWithTAG builds a header-less APEv2 tag of total bytes (body plus 32-byte
// footer) whose body holds an ID3v1-looking "TAG" block exactly id3v1Size bytes
// before the tag's end.
func apeWithTAG(total int) []byte {
	b := make([]byte, total)
	copy(b[total-id3v1Size:], id3v1Trailer("Ape Title", "Ape Artist", "Ape Album", "1999", "ape", 7))
	f := b[total-32:]
	copy(f, "APETAGEX")
	binary.LittleEndian.PutUint32(f[8:], 2000)
	binary.LittleEndian.PutUint32(f[12:], uint32(total)) // tag size: items plus footer
	return b
}

// apeTrailerFixture is an MP3 whose ID3v2 tag has no Title or Artist, followed
// by audio and an APEv2 tag with "TAG" 128 bytes before the end of the file.
func apeTrailerFixture() (file, ape []byte) {
	ape = apeWithTAG(300)
	v2 := id3Tag(4, id3Frame(4, "TALB", latin1Text("V2 Album")))
	file = append(append(v2, audioBytes(4096)...), ape...)
	return file, ape
}

func TestParse_APETagWithTAGIsNotID3v1(t *testing.T) {
	file, _ := apeTrailerFixture()
	f := parseBytes(t, file)
	if f.Tags.Title != "" || f.Tags.Artist != "" || f.Tags.Year != 0 || f.Tags.TrackNumber != 0 || f.Tags.Comment != "" {
		t.Errorf("tags filled from APE bytes: %+v", f.Tags)
	}
}

func TestReadID3v1_APETagWithTAGIsNotID3v1(t *testing.T) {
	file, _ := apeTrailerFixture()
	sr := binutil.NewSafeReader(bytes.NewReader(file), int64(len(file)), "")
	if v1, ok := readID3v1(sr, int64(len(file))); ok {
		t.Errorf("readID3v1 = %+v, true; want false", v1)
	}
}

func TestAudioRange_APETagWithTAGIsNotID3v1(t *testing.T) {
	file, _ := apeTrailerFixture()
	sr := binutil.NewSafeReader(bytes.NewReader(file), int64(len(file)), "")
	_, end, hasV1, err := audioRange(sr, int64(len(file)))
	if err != nil {
		t.Fatal(err)
	}
	if hasV1 || end != int64(len(file)) {
		t.Errorf("audioRange end = %d, hasV1 = %v; want end %d, false", end, hasV1, len(file))
	}
}

func TestWrite_APETagWithTAGKeptAndNotSeeded(t *testing.T) {
	file, ape := apeTrailerFixture()
	out := writeBytes(t, file, editsFor(t, file, func(tg *types.Tags) { tg.Title = "New" }))
	if !bytes.HasSuffix(out, ape) {
		t.Error("APE tag not preserved byte for byte")
	}
	f := parseBytes(t, out)
	wantFrame(t, f, "TIT2", "New")
	wantFrame(t, f, "TPE1", "")
	wantFrame(t, f, "TALB", "V2 Album")
	wantFrame(t, f, "TDRC", "")
	wantFrame(t, f, "TRCK", "")
	if f.Tags.Comment != "" {
		t.Errorf("Comment = %q, want empty", f.Tags.Comment)
	}
}

// A real ID3v1 trailer after an APEv2 tag is the last block, so it still reads.
func TestParse_ID3v1AfterAPETagStillRead(t *testing.T) {
	file, _ := apeTrailerFixture()
	file = append(file, id3v1Trailer("Real Title", "Real Artist", "Real Album", "2001", "real", 3)...)
	f := parseBytes(t, file)
	if f.Tags.Title != "Real Title" || f.Tags.Artist != "Real Artist" {
		t.Errorf("Title, Artist = %q, %q; want the ID3v1 trailer's", f.Tags.Title, f.Tags.Artist)
	}
	sr := binutil.NewSafeReader(bytes.NewReader(file), int64(len(file)), "")
	_, end, hasV1, err := audioRange(sr, int64(len(file)))
	if err != nil || !hasV1 || end != int64(len(file))-id3v1Size {
		t.Errorf("audioRange end = %d, hasV1 = %v, err = %v", end, hasV1, err)
	}
}

// Plain-trailer controls: TestID3v1Seed_SetTitleKeepsOtherFields and
// TestID3v1Seed_FallbackKeepsID3v2Album (id3v1_seed_test.go).
