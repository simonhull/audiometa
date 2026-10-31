package ogg

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"slices"
	"strings"
	"testing"
	"time"

	binutil "github.com/simonhull/audiometa/internal/binary"
	"github.com/simonhull/audiometa/internal/types"
)

// pageSpec describes a page for buildOgg: lacing and data are written verbatim.
type pageSpec struct {
	headerType byte
	granule    uint64
	serial     uint32
	lacing     []byte
	data       []byte
}

func buildOgg(pages ...pageSpec) []byte {
	var buf bytes.Buffer
	for i, p := range pages {
		buf.WriteString("OggS")
		buf.WriteByte(0)
		buf.WriteByte(p.headerType)
		_ = binary.Write(&buf, binary.LittleEndian, p.granule)
		_ = binary.Write(&buf, binary.LittleEndian, p.serial)
		_ = binary.Write(&buf, binary.LittleEndian, uint32(i))
		_ = binary.Write(&buf, binary.LittleEndian, uint32(0))
		buf.WriteByte(byte(len(p.lacing)))
		buf.Write(p.lacing)
		buf.Write(p.data)
	}
	return buf.Bytes()
}

// lacingFor returns the conformant segment table for one packet of n bytes
// ending on this page (n/255 values of 255 followed by n%255).
func lacingFor(n int) []byte {
	l := bytes.Repeat([]byte{255}, n/255)
	return append(l, byte(n%255))
}

// splitAcrossPages builds pages that carry one packet, at most 255 segments each.
// The first page gets firstType; later pages are continuations.
func splitAcrossPages(packet []byte, firstType byte, serial uint32) []pageSpec {
	lacing := lacingFor(len(packet))
	var out []pageSpec
	for len(lacing) > 0 {
		n := min(len(lacing), 255)
		seg := lacing[:n]
		size := 0
		for _, l := range seg {
			size += int(l)
		}
		ht := byte(0x01)
		if len(out) == 0 {
			ht = firstType
		}
		out = append(out, pageSpec{headerType: ht, serial: serial, lacing: seg, data: packet[:size]})
		packet = packet[size:]
		lacing = lacing[n:]
	}
	return out
}

func TestSplitPackets(t *testing.T) {
	pk := func(n int, b byte) []byte { return bytes.Repeat([]byte{b}, n) }
	tests := []struct {
		name     string
		pages    []*Page
		want     [][]byte
		wantOpen bool
	}{
		{
			name:  "two packets in one page",
			pages: []*Page{{Lacing: []byte{3, 2}, Data: append(pk(3, 'a'), pk(2, 'b')...)}},
			want:  [][]byte{pk(3, 'a'), pk(2, 'b')},
		},
		{
			name:  "255 byte packet terminated by zero lacing",
			pages: []*Page{{Lacing: []byte{255, 0}, Data: pk(255, 'x')}},
			want:  [][]byte{pk(255, 'x')},
		},
		{
			name: "packet spanning two pages",
			pages: []*Page{
				{Lacing: []byte{255}, Data: pk(255, 'a')},
				{HeaderType: 0x01, Lacing: []byte{10}, Data: pk(10, 'b')},
			},
			want: [][]byte{append(pk(255, 'a'), pk(10, 'b')...)},
		},
		{
			name:     "unterminated trailing packet",
			pages:    []*Page{{Lacing: []byte{4, 255}, Data: append(pk(4, 'a'), pk(255, 'b')...)}},
			want:     [][]byte{pk(4, 'a'), pk(255, 'b')},
			wantOpen: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, open := splitPackets(tt.pages)
			if open != tt.wantOpen {
				t.Errorf("open = %v, want %v", open, tt.wantOpen)
			}
			if !slices.EqualFunc(got, tt.want, bytes.Equal) {
				t.Errorf("packets = %d, want %d", len(got), len(tt.want))
			}
		})
	}
}

func TestFindLastGranuleIgnoresOggSInPayload(t *testing.T) {
	// A fake "OggS" with an implausible header sits in the true final page's payload.
	fake := append([]byte("OggS\x07\xFF"), bytes.Repeat([]byte{0xAA}, 40)...)
	data := buildOgg(
		pageSpec{headerType: 0x02, granule: 0, serial: 1, lacing: []byte{4}, data: []byte("abcd")},
		pageSpec{headerType: 0x04, granule: 44100, serial: 1, lacing: lacingFor(len(fake)), data: fake},
	)
	sr := binutil.NewSafeReader(bytes.NewReader(data), int64(len(data)), "t.ogg")
	got, err := findLastGranulePosition(sr, int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	if got != 44100 {
		t.Errorf("granule = %d, want 44100", got)
	}
	d, err := calculateDuration(sr, int64(len(data)), 44100)
	if err != nil || d != time.Second {
		t.Errorf("duration = %v, %v", d, err)
	}
}

func vorbisIDPacket() []byte {
	var b bytes.Buffer
	b.WriteByte(1)
	b.WriteString("vorbis")
	_ = binary.Write(&b, binary.LittleEndian, uint32(0))
	b.WriteByte(2)
	_ = binary.Write(&b, binary.LittleEndian, uint32(44100))
	_ = binary.Write(&b, binary.LittleEndian, uint32(0))
	_ = binary.Write(&b, binary.LittleEndian, uint32(128000))
	_ = binary.Write(&b, binary.LittleEndian, uint32(0))
	b.WriteByte(0xB8)
	b.WriteByte(1)
	return b.Bytes()
}

func opusHeadPacket() []byte {
	var b bytes.Buffer
	b.WriteString("OpusHead")
	b.WriteByte(1)
	b.WriteByte(2)
	_ = binary.Write(&b, binary.LittleEndian, uint16(312))
	_ = binary.Write(&b, binary.LittleEndian, uint32(48000))
	_ = binary.Write(&b, binary.LittleEndian, int16(0))
	b.WriteByte(0)
	return b.Bytes()
}

func commentPacket(magic string, framing bool, comments ...string) []byte {
	var b bytes.Buffer
	b.WriteString(magic)
	_ = binary.Write(&b, binary.LittleEndian, uint32(4))
	b.WriteString("test")
	_ = binary.Write(&b, binary.LittleEndian, uint32(len(comments)))
	for _, c := range comments {
		_ = binary.Write(&b, binary.LittleEndian, uint32(len(c)))
		b.WriteString(c)
	}
	if framing {
		b.WriteByte(1)
	}
	return b.Bytes()
}

func pictureComment(image []byte) string {
	return "METADATA_BLOCK_PICTURE=" + base64.StdEncoding.EncodeToString(
		createTestPictureBlock(3, "image/jpeg", "cover", 1, 1, image))
}

func multiPageFile(idPacket, commentPkt []byte, serial uint32, tail pageSpec) []byte {
	pages := []pageSpec{{headerType: 0x02, serial: serial, lacing: lacingFor(len(idPacket)), data: idPacket}}
	pages = append(pages, splitAcrossPages(commentPkt, 0, serial)...)
	pages = append(pages, tail)
	return buildOgg(pages...)
}

func TestParseCommentPacketSpanningManyPages(t *testing.T) {
	image := bytes.Repeat([]byte{0xAB}, 200_000)
	tests := []struct {
		name   string
		id     []byte
		magic  string
		frame  bool
		tail   pageSpec
		format types.Format
	}{
		{"vorbis", vorbisIDPacket(), "\x03vorbis", true, pageSpec{headerType: 0x04, granule: 44100, serial: 7, lacing: []byte{100}, data: make([]byte, 100)}, types.FormatOgg},
		{"opus", opusHeadPacket(), "OpusTags", false, pageSpec{headerType: 0x04, granule: 48000, serial: 7, lacing: []byte{100}, data: make([]byte, 100)}, types.FormatOpus},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			comment := commentPacket(tt.magic, tt.frame, pictureComment(image), "TITLE=After The Picture")
			data := multiPageFile(tt.id, comment, 7, tt.tail)

			p := &parser{}
			f, err := p.Parse(context.Background(), bytes.NewReader(data), int64(len(data)), "x.ogg")
			if err != nil {
				t.Fatal(err)
			}
			if f.Format != tt.format {
				t.Errorf("format = %v", f.Format)
			}
			if f.Tags.Title != "After The Picture" {
				t.Errorf("title = %q (warnings %v)", f.Tags.Title, f.Warnings)
			}
			for _, w := range f.Warnings {
				if strings.Contains(w.Message, "truncated") {
					t.Errorf("unexpected warning: %s", w.Message)
				}
			}
			art, err := p.ExtractArtwork(context.Background(), bytes.NewReader(data), int64(len(data)), "x.ogg")
			if err != nil || len(art) != 1 || !bytes.Equal(art[0].Data, image) {
				t.Fatalf("artwork = %d images, err %v", len(art), err)
			}
		})
	}
}

func TestReadHeaderPacketsCap(t *testing.T) {
	comment := commentPacket("\x03vorbis", true, "TITLE="+strings.Repeat("x", 200_000))
	data := multiPageFile(vorbisIDPacket(), comment, 7, pageSpec{headerType: 0x04, serial: 7, lacing: []byte{1}, data: []byte{0}})
	sr := binutil.NewSafeReader(bytes.NewReader(data), int64(len(data)), "x.ogg")
	file := &types.File{}
	packets, err := readHeaderPackets(context.Background(), sr, int64(len(data)), file, 70_000)
	if err != nil {
		t.Fatal(err)
	}
	if len(packets) < 1 {
		t.Fatal("no packets")
	}
	var warned bool
	for _, w := range file.Warnings {
		warned = warned || strings.Contains(w.Message, "exceeds 70000 bytes")
	}
	if !warned {
		t.Errorf("missing cap warning: %v", file.Warnings)
	}
}

func TestReadHeaderPacketsSkipsOtherStreams(t *testing.T) {
	id := vorbisIDPacket()
	comment := commentPacket("\x03vorbis", true, "TITLE=Mine")
	data := buildOgg(
		pageSpec{headerType: 0x02, serial: 1, lacing: lacingFor(len(id)), data: id},
		pageSpec{headerType: 0x02, serial: 2, lacing: []byte{3}, data: []byte("zzz")},
		pageSpec{serial: 1, lacing: lacingFor(len(comment)), data: comment},
	)
	sr := binutil.NewSafeReader(bytes.NewReader(data), int64(len(data)), "x.ogg")
	packets, err := readHeaderPackets(context.Background(), sr, int64(len(data)), &types.File{}, maxHeaderBytes)
	if err != nil {
		t.Fatal(err)
	}
	if len(packets) != 2 || !bytes.Equal(packets[1], comment) {
		t.Errorf("packets = %d", len(packets))
	}
}

func TestParseHeadersSharingOnePage(t *testing.T) {
	id := vorbisIDPacket()
	comment := commentPacket("\x03vorbis", true, "TITLE=Shared")
	setup := []byte("\x05vorbis\x01")
	// Identification alone, then comment+setup together.
	data := buildOgg(
		pageSpec{headerType: 0x02, serial: 1, lacing: lacingFor(len(id)), data: id},
		pageSpec{serial: 1, lacing: append(lacingFor(len(comment)), lacingFor(len(setup))...), data: append(slices.Clone(comment), setup...)},
		pageSpec{headerType: 0x04, granule: 44100, serial: 1, lacing: []byte{1}, data: []byte{0}},
	)
	f, err := (&parser{}).Parse(context.Background(), bytes.NewReader(data), int64(len(data)), "x.ogg")
	if err != nil {
		t.Fatal(err)
	}
	if f.Tags.Title != "Shared" {
		t.Errorf("title = %q, warnings %v", f.Tags.Title, f.Warnings)
	}
}
