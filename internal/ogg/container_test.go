package ogg

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
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
func splitAcrossPages(packet []byte, serial uint32) []pageSpec {
	const firstType byte = 0
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

// readPage is the reference reader that pageStream must agree with: it reads
// the Ogg page at offset field by field through a SafeReader and returns the
// page, the next offset and any error.
func readPage(sr *binutil.SafeReader, offset int64) (*oggPage, int64, error) {
	// Verify "OggS" magic marker
	magic := make([]byte, 4)
	if err := sr.ReadAt(magic, offset, "Ogg magic"); err != nil {
		return nil, 0, err
	}
	if string(magic) != capturePattern {
		return nil, 0, fmt.Errorf("invalid Ogg page at offset %d", offset)
	}

	// Read stream structure version (should be 0x00)
	version, err := sr.Read[uint8](offset+4, "version")
	if err != nil {
		return nil, 0, err
	}
	if version != 0 {
		return nil, 0, fmt.Errorf("unsupported Ogg version: %d", version)
	}

	// Read header fields
	headerType, err := sr.Read[uint8](offset+5, "header type")
	if err != nil {
		return nil, 0, err
	}

	granule, err := sr.ReadLE[uint64](offset+6, "granule position")
	if err != nil {
		return nil, 0, err
	}

	serial, err := sr.ReadLE[uint32](offset+14, "serial number")
	if err != nil {
		return nil, 0, err
	}

	sequence, err := sr.ReadLE[uint32](offset+18, "sequence number")
	if err != nil {
		return nil, 0, err
	}

	segmentCount, err := sr.Read[uint8](offset+26, "segment count")
	if err != nil {
		return nil, 0, err
	}

	// Read segment table (each byte is size of a segment, 0-255)
	segments := make([]byte, segmentCount)
	if err := sr.ReadAt(segments, offset+27, "segment table"); err != nil {
		return nil, 0, err
	}

	dataSize := 0
	for _, seg := range segments {
		dataSize += int(seg)
	}

	// Read page data
	data := make([]byte, dataSize)
	dataOffset := offset + 27 + int64(segmentCount)
	if err := sr.ReadAt(data, dataOffset, "page data"); err != nil {
		return nil, 0, err
	}

	page := &oggPage{
		HeaderType:      headerType,
		GranulePosition: int64(granule),
		SerialNumber:    serial,
		SequenceNumber:  sequence,
		Data:            data,
		Lacing:          segments,
	}
	return page, dataOffset + int64(dataSize), nil
}

// samePageResults compares what readPage and a pageStream return for every
// page of data, including the error that ends the walk.
func samePageResults(t *testing.T, data []byte, shared bool) {
	t.Helper()
	size := int64(len(data))
	sr := binutil.NewSafeReader(bytes.NewReader(data), size, "x")
	ps := newPageStream(bytes.NewReader(data), size, "x", shared)
	for off, i := int64(0), 0; off < size; i++ {
		want, wantNext, wantErr := readPage(sr, off)
		got, gotNext, gotErr := ps.next()
		if (wantErr == nil) != (gotErr == nil) {
			t.Fatalf("len %d page %d: err = %v, want %v", len(data), i, gotErr, wantErr)
		}
		if wantErr != nil {
			wantOOB, wantIs := errors.AsType[*binutil.OutOfBoundsError](wantErr)
			gotOOB, gotIs := errors.AsType[*binutil.OutOfBoundsError](gotErr)
			if wantIs != gotIs || (wantIs && *wantOOB != *gotOOB) || wantErr.Error() != gotErr.Error() {
				t.Fatalf("len %d page %d: err = %q, want %q", len(data), i, gotErr, wantErr)
			}
			return
		}
		if gotNext != wantNext || !reflect.DeepEqual(got, want) {
			t.Fatalf("len %d page %d: got %+v next %d, want %+v next %d", len(data), i, got, gotNext, want, wantNext)
		}
		off = wantNext
	}
}

func TestPageStreamMatchesReadPage(t *testing.T) {
	t.Parallel()
	inputs := map[string][]byte{
		"opus+cover": ioOpus(true),
		"empty pages": buildOgg(pageSpec{headerType: 2, serial: 1, lacing: []byte{3}, data: []byte("abc")},
			pageSpec{serial: 1}, pageSpec{serial: 1, lacing: []byte{0}}, pageSpec{serial: 1, lacing: lacingFor(300), data: make([]byte, 300)}),
		"leading empty page": buildOgg(pageSpec{serial: 1}, pageSpec{serial: 1, lacing: []byte{2}, data: []byte("ab")}),
		"bad version":        append(buildOgg(pageSpec{serial: 1, lacing: []byte{1}, data: []byte{1}}), append([]byte("OggS\x01"), make([]byte, 40)...)...),
		"bad magic":          append(buildOgg(pageSpec{serial: 1, lacing: []byte{1}, data: []byte{1}}), []byte("junk-junk-junk-junk-junk-junk-junk-junk")...),
		"trailing tag":       append(buildOgg(pageSpec{serial: 1, lacing: []byte{1}, data: []byte{1}}), []byte("TAG")...),
	}
	for _, name := range []string{"sample.ogg", "sample.opus", "sample_retagged.ogg", "sample_retagged.opus"} {
		data, err := os.ReadFile(filepath.Join("..", "..", "testdata", name))
		if err != nil {
			t.Fatal(err)
		}
		inputs[name] = data
	}
	for name, data := range inputs {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			for _, shared := range []bool{false, true} {
				samePageResults(t, data, shared)
				// Every cut inside the first pages, then cuts spread over the file.
				for n := 0; n <= min(len(data), 400); n++ {
					samePageResults(t, data[:n], shared)
				}
				for n := 400; n < len(data); n += max(len(data)/97, 1) {
					samePageResults(t, data[:n], shared)
				}
			}
		})
	}
}

// failingAt fails every read that reaches offset at, with err.
type failingAt struct {
	r   io.ReaderAt
	at  int64
	err error
}

func (f failingAt) ReadAt(p []byte, off int64) (int, error) {
	if off+int64(len(p)) > f.at {
		return 0, f.err
	}
	return f.r.ReadAt(p, off)
}

func TestPageStreamReadError(t *testing.T) {
	t.Parallel()
	errDisk := errors.New("disk on fire")
	data := ioOpus(false)
	ps := newPageStream(failingAt{bytes.NewReader(data), 40_000, errDisk}, int64(len(data)), "x", false)
	var err error
	for err == nil {
		_, _, err = ps.next()
	}
	if !errors.Is(err, errDisk) {
		t.Errorf("err = %v, want it to wrap the reader's error", err)
	}
}

// pagesOf reads every page of an Ogg file built by buildOgg.
func pagesOf(tb testing.TB, data []byte) []*oggPage {
	tb.Helper()
	sr := binutil.NewSafeReader(bytes.NewReader(data), int64(len(data)), "x")
	var pages []*oggPage
	for off := int64(0); off < int64(len(data)); {
		page, next, err := readPage(sr, off)
		if err != nil {
			tb.Fatal(err)
		}
		pages = append(pages, page)
		off = next
	}
	return pages
}

func TestSplitPacketsAllocs(t *testing.T) {
	packet := bytes.Repeat([]byte{7}, 300_000)
	pages := pagesOf(t, buildOgg(splitAcrossPages(packet, 1)...))
	if len(pages) < 2 {
		t.Fatalf("pages = %d, want a packet spanning several", len(pages))
	}
	if got, _ := splitPackets(pages); len(got) != 1 || !bytes.Equal(got[0], packet) {
		t.Fatal("packet not reassembled")
	}
	if n := testing.AllocsPerRun(10, func() { splitPackets(pages) }); n > 4 {
		t.Errorf("allocs = %v, want <= 4", n)
	}
}

func TestSplitPackets(t *testing.T) {
	t.Parallel()
	pk := func(n int, b byte) []byte { return bytes.Repeat([]byte{b}, n) }
	tests := []struct {
		name     string
		pages    []*oggPage
		want     [][]byte
		wantOpen bool
	}{
		{
			name:  "two packets in one page",
			pages: []*oggPage{{Lacing: []byte{3, 2}, Data: append(pk(3, 'a'), pk(2, 'b')...)}},
			want:  [][]byte{pk(3, 'a'), pk(2, 'b')},
		},
		{
			name:  "255 byte packet terminated by zero lacing",
			pages: []*oggPage{{Lacing: []byte{255, 0}, Data: pk(255, 'x')}},
			want:  [][]byte{pk(255, 'x')},
		},
		{
			name: "packet spanning two pages",
			pages: []*oggPage{
				{Lacing: []byte{255}, Data: pk(255, 'a')},
				{HeaderType: 0x01, Lacing: []byte{10}, Data: pk(10, 'b')},
			},
			want: [][]byte{append(pk(255, 'a'), pk(10, 'b')...)},
		},
		{
			name: "continuation dropped by the next page",
			pages: []*oggPage{
				{Lacing: []byte{255}, Data: pk(255, 'a')},
				{Lacing: []byte{3}, Data: pk(3, 'b')},
			},
			want: [][]byte{pk(255, 'a'), pk(3, 'b')},
		},
		{
			name:     "unterminated trailing packet",
			pages:    []*oggPage{{Lacing: []byte{4, 255}, Data: append(pk(4, 'a'), pk(255, 'b')...)}},
			want:     [][]byte{pk(4, 'a'), pk(255, 'b')},
			wantOpen: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
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
	t.Parallel()
	// A fake "OggS" with an implausible header sits in the true final page's payload.
	fake := append([]byte("OggS\x07\xFF"), bytes.Repeat([]byte{0xAA}, 40)...)
	data := buildOgg(
		pageSpec{headerType: 0x02, granule: 0, serial: 1, lacing: []byte{4}, data: []byte("abcd")},
		pageSpec{headerType: 0x04, granule: 44100, serial: 1, lacing: lacingFor(len(fake)), data: fake},
	)
	sr := binutil.NewSafeReader(bytes.NewReader(data), int64(len(data)), "t.ogg")
	h := headers{serial: 1}
	got, err := lastGranule(context.Background(), sr, int64(len(data)), h)
	if err != nil {
		t.Fatal(err)
	}
	if got != 44100 {
		t.Errorf("granule = %d, want 44100", got)
	}
	d, err := calculateDuration(context.Background(), sr, int64(len(data)), h, 44100)
	if err != nil || d != time.Second {
		t.Errorf("duration = %v, %v", d, err)
	}
}

func TestLastGranuleSkipsUnsetAndOtherStreams(t *testing.T) {
	t.Parallel()
	const unset = ^uint64(0)
	data := buildOgg(
		pageSpec{headerType: 0x02, serial: 1, lacing: []byte{4}, data: []byte("abcd")},
		pageSpec{granule: 4 * 48000, serial: 1, lacing: []byte{4}, data: []byte("abcd")},
		pageSpec{granule: 9 * 48000, serial: 2, lacing: []byte{4}, data: []byte("abcd")},
		pageSpec{headerType: 0x04, granule: unset, serial: 1, lacing: []byte{4}, data: []byte("abcd")},
		pageSpec{headerType: 0x04, granule: 9 * 48000, serial: 2, lacing: []byte{4}, data: []byte("abcd")},
	)
	sr := binutil.NewSafeReader(bytes.NewReader(data), int64(len(data)), "t.ogg")
	got, err := lastGranule(context.Background(), sr, int64(len(data)), headers{serial: 1})
	if err != nil {
		t.Fatal(err)
	}
	if got != 4*48000 {
		t.Errorf("granule = %d, want %d", got, 4*48000)
	}
	if _, err := lastGranule(context.Background(), sr, int64(len(data)), headers{serial: 3}); err == nil {
		t.Error("lastGranule for an absent serial returned no error")
	}
}

// opusLink is one complete Opus logical stream of the given length in seconds.
func opusLink(serial uint32, seconds uint64, audio ...pageSpec) []pageSpec {
	head, tags := opusHeadPacket(), commentPacket("OpusTags", false)
	pages := []pageSpec{
		{headerType: 0x02, serial: serial, lacing: lacingFor(len(head)), data: head},
		{serial: serial, lacing: lacingFor(len(tags)), data: tags},
	}
	pages = append(pages, audio...)
	return append(pages, pageSpec{headerType: 0x04, granule: seconds * 48000, serial: serial, lacing: lacingFor(10), data: make([]byte, 10)})
}

func TestDuration_ChainedOggUsesFirstStream(t *testing.T) {
	t.Parallel()
	big := pageSpec{granule: 48000, serial: 2, lacing: lacingFor(60000), data: make([]byte, 60000)}
	unsetEnd := opusLink(1, 0, pageSpec{granule: 4 * 48000, serial: 1, lacing: lacingFor(10), data: make([]byte, 10)})
	unsetEnd[len(unsetEnd)-1].granule = ^uint64(0)
	tests := map[string]struct {
		pages []pageSpec
		want  time.Duration
	}{
		"short chain":                  {slices.Concat(opusLink(1, 5), opusLink(2, 7)), 5 * time.Second},
		"first stream beyond the tail": {slices.Concat(opusLink(1, 5), opusLink(2, 7, big, big)), 5 * time.Second},
		"unset final granule":          {slices.Concat(unsetEnd, opusLink(2, 7)), 4 * time.Second},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := parseOggBytes(t, buildOgg(tt.pages...))
			if f.Audio.Duration != tt.want {
				t.Errorf("Duration = %v, want %v (the first stream)", f.Audio.Duration, tt.want)
			}
		})
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
	pages := slices.Concat(
		[]pageSpec{{headerType: 0x02, serial: serial, lacing: lacingFor(len(idPacket)), data: idPacket}},
		splitAcrossPages(commentPkt, serial),
		[]pageSpec{tail},
	)
	return buildOgg(pages...)
}

func TestParseCommentPacketSpanningManyPages(t *testing.T) {
	t.Parallel()
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
			t.Parallel()
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
	t.Parallel()
	comment := commentPacket("\x03vorbis", true, "TITLE="+strings.Repeat("x", 200_000))
	data := multiPageFile(vorbisIDPacket(), comment, 7, pageSpec{headerType: 0x04, serial: 7, lacing: []byte{1}, data: []byte{0}})
	file := &types.File{}
	h, err := readHeaderPackets(context.Background(), bytes.NewReader(data), int64(len(data)), "x.ogg", file, 70_000)
	if err != nil {
		t.Fatal(err)
	}
	packets := h.packets
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
	t.Parallel()
	id := vorbisIDPacket()
	comment := commentPacket("\x03vorbis", true, "TITLE=Mine")
	data := buildOgg(
		pageSpec{headerType: 0x02, serial: 1, lacing: lacingFor(len(id)), data: id},
		pageSpec{headerType: 0x02, serial: 2, lacing: []byte{3}, data: []byte("zzz")},
		pageSpec{serial: 1, lacing: lacingFor(len(comment)), data: comment},
	)
	h, err := readHeaderPackets(context.Background(), bytes.NewReader(data), int64(len(data)), "x.ogg", &types.File{}, maxHeaderBytes)
	if err != nil {
		t.Fatal(err)
	}
	packets := h.packets
	if len(packets) != 2 || !bytes.Equal(packets[1], comment) {
		t.Errorf("packets = %d", len(packets))
	}
}

func TestParseHeadersSharingOnePage(t *testing.T) {
	t.Parallel()
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

func TestReadHeaderPacketsDroppedContinuation(t *testing.T) {
	t.Parallel()
	// The comment header's last segment is 255, but the next page does not
	// continue it. The comment ends where that page begins, so it is read too,
	// and reading stops before the audio page.
	id := vorbisIDPacket()
	comment := bytes.Repeat([]byte{3}, 255)
	data := buildOgg(
		pageSpec{headerType: 0x02, serial: 1, lacing: lacingFor(len(id)), data: id},
		pageSpec{serial: 1, lacing: []byte{255}, data: comment},
		pageSpec{serial: 1, lacing: []byte{4}, data: []byte("\x05set")},
		pageSpec{serial: 1, lacing: []byte{1}, data: []byte{0}},
	)
	h, err := readHeaderPackets(context.Background(), bytes.NewReader(data), int64(len(data)), "x.ogg", &types.File{}, maxHeaderBytes)
	if err != nil {
		t.Fatal(err)
	}
	packets := h.packets
	if len(packets) != 3 || !bytes.Equal(packets[1], comment) {
		t.Errorf("packets = %d, want identification, comment and setup", len(packets))
	}
}

// audioTail is the stream's last page: 100 bytes of audio, granule g. With
// its 28-byte header it is 128 bytes, so one second of it is 1024 bit/s.
func audioTail(serial uint32, g uint64) pageSpec {
	return pageSpec{headerType: 0x04, granule: g, serial: serial, lacing: lacingFor(100), data: make([]byte, 100)}
}

func parseOggBytes(t *testing.T, data []byte) *types.File {
	t.Helper()
	f, err := (&parser{}).Parse(context.Background(), bytes.NewReader(data), int64(len(data)), "x.ogg")
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestParse_OpusBitrateExcludesHeaders(t *testing.T) {
	t.Parallel()
	for name, comments := range map[string][]string{
		"no picture":    nil,
		"30 KB picture": {pictureComment(bytes.Repeat([]byte{0xAB}, 30000))},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			data := multiPageFile(opusHeadPacket(), commentPacket("OpusTags", false, comments...), 7, audioTail(7, 48000))
			if got := parseOggBytes(t, data).Audio.Bitrate; got != 1024 {
				t.Errorf("Bitrate = %d, want 1024", got)
			}
		})
	}
}

func TestParse_VorbisUnsetNominalBitrateIsEstimated(t *testing.T) {
	t.Parallel()
	for name, nominal := range map[string]uint32{"-1": 0xFFFFFFFF, "0": 0} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			id := vorbisIDPacket()
			binary.LittleEndian.PutUint32(id[20:24], nominal)
			comment := commentPacket("\x03vorbis", true, "TITLE=x")
			setup := []byte("\x05vorbis\x00\x01")
			data := buildOgg(
				pageSpec{headerType: 0x02, serial: 9, lacing: lacingFor(len(id)), data: id},
				pageSpec{serial: 9, lacing: lacingFor(len(comment)), data: comment},
				pageSpec{serial: 9, lacing: lacingFor(len(setup)), data: setup},
				audioTail(9, 44100),
			)
			if got := parseOggBytes(t, data).Audio.Bitrate; got != 1024 {
				t.Errorf("Bitrate = %d, want 1024 (estimated)", got)
			}
		})
	}
}
