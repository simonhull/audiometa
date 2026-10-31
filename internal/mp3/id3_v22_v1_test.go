package mp3

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/simonhull/audiometa/internal/types"
)

// audioBytes is one MPEG frame followed by zero padding, n bytes in total.
func audioBytes(n int) []byte {
	return append(append([]byte(nil), mpegFrame...), make([]byte, n-len(mpegFrame))...)
}

func hasWarning(f *types.File, sub string) bool {
	for _, w := range f.Warnings {
		if strings.Contains(w.Message, sub) {
			return true
		}
	}
	return false
}

func TestID3v2Header_TotalSize(t *testing.T) {
	tests := []struct {
		h    ID3v2Header
		want int64
	}{
		{ID3v2Header{Version: 4, Flags: 0x10, Size: 100}, 120},
		{ID3v2Header{Version: 3, Flags: 0x10, Size: 100}, 110},
		{ID3v2Header{Version: 4, Size: 0}, 10},
	}
	for _, tt := range tests {
		if got := tt.h.totalSize(); got != tt.want {
			t.Errorf("%+v.totalSize() = %d, want %d", tt.h, got, tt.want)
		}
	}
}

func TestParse_UnsupportedID3VersionSkipsTag(t *testing.T) {
	body := make([]byte, 40)
	copy(body[20:], []byte{0xFF, 0xFB, 0x50, 0x00}) // false sync (64 kbps) inside the tag
	data := append(id3Tag(5, body), mpegFrame...)
	f := parseBytes(t, data)
	if f.Audio.Bitrate != 128000 {
		t.Errorf("bitrate = %d, want 128000", f.Audio.Bitrate)
	}
	if !hasWarning(f, "unsupported ID3v2 version") {
		t.Errorf("missing unsupported-version warning: %v", f.Warnings)
	}
}

func v22Tag(extra ...[]byte) []byte {
	pic := []byte{0x00, 'J', 'P', 'G', 0x03, 0x00}
	pic = append(pic, 0xFF, 0xD8, 0xFF, 0xE0, 0, 0, 0xFF, 0xFB, 0x50, 0x00) // false sync inside the picture
	frames := [][]byte{
		id3v22Frame("TT2", latin1Text("Title")),
		id3v22Frame("TP1", utf16Text("Artist")),
		id3v22Frame("TAL", latin1Text("Alb\xe9")),
		id3v22Frame("TRK", latin1Text("3/10")),
		id3v22Frame("MVN", latin1Text("Saga")),
		id3v22Frame("COM", []byte("\x00eng\x00note")),
		id3v22Frame("PIC", pic),
	}
	return id3Tag(2, append(frames, extra...)...)
}

func TestParse_ID3v22(t *testing.T) {
	f := parseBytes(t, append(v22Tag(), mpegFrame...))
	tags := f.Tags
	if tags.Title != "Title" || tags.Artist != "Artist" || tags.Album != "Albé" {
		t.Errorf("title %q artist %q album %q", tags.Title, tags.Artist, tags.Album)
	}
	if tags.TrackNumber != 3 || tags.TrackTotal != 10 {
		t.Errorf("track %d/%d", tags.TrackNumber, tags.TrackTotal)
	}
	if tags.Series != "Saga" || tags.Comment != "note" {
		t.Errorf("series %q comment %q", tags.Series, tags.Comment)
	}
	if f.Audio.Bitrate != 128000 {
		t.Errorf("bitrate = %d, want 128000", f.Audio.Bitrate)
	}
	if hasWarning(f, "unsupported") {
		t.Errorf("unexpected warning: %v", f.Warnings)
	}
}

func TestExtractArtwork_ID3v22(t *testing.T) {
	data := append(v22Tag(), mpegFrame...)
	art, err := (&parser{}).ExtractArtwork(context.Background(), bytes.NewReader(data), int64(len(data)), "t.mp3")
	if err != nil || len(art) != 1 {
		t.Fatalf("artwork = %d, err %v", len(art), err)
	}
	if art[0].MIMEType != "image/jpeg" || art[0].Type != types.ArtworkType(3) {
		t.Errorf("mime %q type %v", art[0].MIMEType, art[0].Type)
	}
}

func TestParse_ID3v22Compressed(t *testing.T) {
	tag := v22Tag()
	tag[5] = 0x40
	f := parseBytes(t, append(tag, mpegFrame...))
	if f.Tags.Title != "" {
		t.Errorf("title = %q, want none", f.Tags.Title)
	}
	if !hasWarning(f, "compressed") {
		t.Errorf("missing compressed warning: %v", f.Warnings)
	}
	if f.Audio.Bitrate != 128000 {
		t.Errorf("bitrate = %d", f.Audio.Bitrate)
	}
}

func TestParse_ID3v1Only(t *testing.T) {
	data := append(audioBytes(16000), id3v1Trailer("T\xe9", "Artist", "Album", "1999", "c", 0)...)
	f := parseBytes(t, data)
	tags := f.Tags
	if tags.Title != "Té" || tags.Artist != "Artist" || tags.Album != "Album" || tags.Year != 1999 || tags.Comment != "c" || tags.TrackNumber != 0 {
		t.Errorf("tags = %+v", tags)
	}
	if f.Audio.Duration != time.Second {
		t.Errorf("duration = %v, want exactly 1s", f.Audio.Duration)
	}
}

func TestParse_ID3v11Track(t *testing.T) {
	data := append(audioBytes(16000), id3v1Trailer("T", "A", "B", "2001", "comment", 7)...)
	f := parseBytes(t, data)
	if f.Tags.TrackNumber != 7 || f.Tags.Comment != "comment" {
		t.Errorf("track %d comment %q", f.Tags.TrackNumber, f.Tags.Comment)
	}
}

func TestParse_ID3v1IgnoredWhenV2HasTags(t *testing.T) {
	data := append(id3Tag(3, id3Frame(3, "TIT2", latin1Text("V2"))), audioBytes(16000)...)
	data = append(data, id3v1Trailer("V1", "V1Artist", "Alb", "1999", "", 0)...)
	f := parseBytes(t, data)
	if f.Tags.Title != "V2" || f.Tags.Artist != "" {
		t.Errorf("title %q artist %q", f.Tags.Title, f.Tags.Artist)
	}
}
