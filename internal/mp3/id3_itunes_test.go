package mp3

import (
	"bytes"
	"strings"
	"testing"

	"github.com/simonhull/audiometa/internal/types"
)

// plainV24Tag builds an ID3v2.4 tag whose frame sizes are plain big-endian
// integers, as older iTunes wrote them, around a 299-byte title.
func plainV24Tag(title string, extra ...[]byte) []byte {
	frames := append([][]byte{
		id3Frame(3, "TIT2", latin1Text(title)),
		id3Frame(3, "TALB", latin1Text("Album")),
	}, extra...)
	return append(id3Tag(4, frames...), bytes.Repeat(mpegFrame, 4)...)
}

func TestParse_ITunesV24PlainFrameSizes(t *testing.T) {
	title := strings.Repeat("a", 299) // 300-byte payload: plain size 0x0000012C, synchsafe reading 172
	f := parseBytes(t, plainV24Tag(title))
	if f.Tags.Title != title || f.Tags.Album != "Album" {
		t.Errorf("title %d bytes, album %q; want 299 bytes and %q", len(f.Tags.Title), f.Tags.Album, "Album")
	}
}

func TestParse_StandardV24SizesAreNotSwitched(t *testing.T) {
	long := strings.Repeat("a", 299)
	tests := map[string]string{
		"300-byte frame": long,
		"small frames":   "Title",
	}
	for name, title := range tests {
		t.Run(name, func(t *testing.T) {
			tag := id3Tag(4, id3Frame(4, "TIT2", latin1Text(title)), id3Frame(4, "TALB", latin1Text("Album")))
			f := parseBytes(t, append(tag, bytes.Repeat(mpegFrame, 4)...))
			if f.Tags.Title != title || f.Tags.Album != "Album" {
				t.Errorf("title %d bytes, album %q; want %d bytes and %q", len(f.Tags.Title), f.Tags.Album, len(title), "Album")
			}
		})
	}
}

func TestExtractArtwork_ITunesV24PlainFrameSizes(t *testing.T) {
	image := append([]byte{0xFF, 0xD8, 0xFF, 0xE0}, bytes.Repeat([]byte{7}, 400)...)
	apic := id3Frame(3, "APIC", apicPayload("image/jpeg", 3, "", image))
	art := extractAll(t, plainV24Tag(strings.Repeat("a", 299), apic))
	if len(art) != 1 || !bytes.Equal(art[0].Data, image) {
		t.Fatalf("got %d pictures, want one of %d bytes", len(art), len(image))
	}
}

func TestWrite_ITunesV24PlainFrameSizesBecomeStandard(t *testing.T) {
	src := plainV24Tag(strings.Repeat("a", 299))
	out := writeBytes(t, src, editsFor(t, src, func(tg *types.Tags) { tg.Title = "New" }))

	f := parseBytes(t, out)
	if f.Tags.Title != "New" || f.Tags.Album != "Album" {
		t.Errorf("title %q, album %q; want %q and %q", f.Tags.Title, f.Tags.Album, "New", "Album")
	}

	// Every frame header must be standard: a synchsafe size that reaches the next frame.
	frames := framesOf(t, out).frames
	if len(frames) < 2 {
		t.Fatalf("output tag has %d frames, want at least 2", len(frames))
	}
	body := out[10:]
	for off, i := 0, 0; off+10 <= len(body) && body[off] != 0; i++ {
		size := body[off+4 : off+8]
		for _, b := range size {
			if b&0x80 != 0 {
				t.Fatalf("frame %d at %d has size bytes %x, not synchsafe", i, off, size)
			}
		}
		if i >= len(frames) {
			t.Fatalf("more frames in the output than the %d parsed", len(frames))
		}
		if got := int(decodeSynchsafe(size)); got != len(frames[i].payload) {
			t.Errorf("frame %d (%s) size %d, payload %d bytes", i, frames[i].id, got, len(frames[i].payload))
		}
		off += 10 + int(decodeSynchsafe(size))
	}
}
