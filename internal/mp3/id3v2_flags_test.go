package mp3

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"testing"

	binutil "github.com/simonhull/audiometa/internal/binary"
	"github.com/simonhull/audiometa/internal/types"
)

// frameBytes builds a frame; v2.4 sizes are synchsafe, v2.3 plain big-endian.
func frameBytes(version byte, id string, flags uint16, payload []byte) []byte {
	out := []byte(id)
	if version == 4 {
		out = append(out, synchsafe(len(payload))...)
	} else {
		n := len(payload)
		out = append(out, byte(n>>24), byte(n>>16), byte(n>>8), byte(n))
	}
	out = append(out, byte(flags>>8), byte(flags))
	return append(out, payload...)
}

func tagBytes(version, flags byte, body []byte) []byte {
	out := []byte{'I', 'D', '3', version, 0, flags}
	out = append(out, synchsafe(len(body))...)
	return append(out, body...)
}

func parseTag(t *testing.T, data []byte) (*types.File, int64) {
	t.Helper()
	sr := binutil.NewSafeReader(bytes.NewReader(data), int64(len(data)), "x.mp3")
	file := &types.File{}
	n, err := parseID3v2(context.Background(), sr, file)
	if err != nil {
		t.Fatal(err)
	}
	return file, n
}

func TestDeunsync(t *testing.T) {
	got := deunsync([]byte{0xFF, 0x00, 0xE0, 0xFF, 0x00, 0xFF})
	if !slices.Equal(got, []byte{0xFF, 0xE0, 0xFF, 0xFF}) {
		t.Errorf("deunsync = %x", got)
	}
}

func TestID3v23TagLevelUnsync(t *testing.T) {
	// Title "ÿà" (Latin-1 FF E0) followed by an Album frame; the on-disk tag is unsynchronised.
	title := frameBytes(3, "TIT2", 0, []byte{0, 0xFF, 0xE0})
	album := frameBytes(3, "TALB", 0, append([]byte{0}, "Album"...))
	plain := slices.Concat(title, album)
	disk := bytes.ReplaceAll(plain, []byte{0xFF, 0xE0}, []byte{0xFF, 0x00, 0xE0})
	data := tagBytes(3, 0x80, disk)

	file, n := parseTag(t, data)
	if file.Tags.Title != "ÿà" || file.Tags.Album != "Album" {
		t.Errorf("title %q album %q warnings %v", file.Tags.Title, file.Tags.Album, file.Warnings)
	}
	if n != int64(len(data)) {
		t.Errorf("tag size = %d, want %d", n, len(data))
	}
}

func TestID3v24FrameUnsyncAndDataLength(t *testing.T) {
	// Frame 1: unsync flag, title bytes FF 00 E0 on disk (encoding 0 + "ÿà").
	f1 := frameBytes(4, "TIT2", v24FrameUnsync, []byte{0, 0xFF, 0x00, 0xE0})
	// Frame 2: data length indicator prefix (4 synchsafe bytes) before the text.
	payload := slices.Concat(synchsafe(6), []byte{0}, []byte("Album"))
	f2 := frameBytes(4, "TALB", v24FrameDataLength, payload)
	file, _ := parseTag(t, tagBytes(4, 0, slices.Concat(f1, f2)))
	if file.Tags.Title != "ÿà" {
		t.Errorf("title = %q", file.Tags.Title)
	}
	if file.Tags.Album != "Album" {
		t.Errorf("album = %q", file.Tags.Album)
	}
}

func TestID3SkipsCompressedAndEncryptedFrames(t *testing.T) {
	tests := []struct {
		name    string
		version byte
		flags   uint16
		word    string
	}{
		{"v2.4 compressed", 4, v24FrameCompression, "compressed"},
		{"v2.4 encrypted", 4, v24FrameEncryption, "encrypted"},
		{"v2.3 compressed", 3, v23FrameCompression, "compressed"},
		{"v2.3 encrypted", 3, v23FrameEncryption, "encrypted"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bad := frameBytes(tt.version, "TIT2", tt.flags, []byte{0, 'x', 'y', 'z', 'w'})
			good := frameBytes(tt.version, "TALB", 0, append([]byte{0}, "After"...))
			file, _ := parseTag(t, tagBytes(tt.version, 0, slices.Concat(bad, good)))
			if file.Tags.Title != "" {
				t.Errorf("title = %q, want skipped", file.Tags.Title)
			}
			if file.Tags.Album != "After" {
				t.Errorf("album = %q, following frame lost", file.Tags.Album)
			}
			var warned bool
			for _, w := range file.Warnings {
				warned = warned || strings.Contains(w.Message, tt.word)
			}
			if !warned {
				t.Errorf("no %q warning: %v", tt.word, file.Warnings)
			}
		})
	}
}
