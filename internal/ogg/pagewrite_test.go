package ogg

import (
	"bytes"
	ebinary "encoding/binary"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"
)

// corpusFiles are the Ogg Vorbis and Opus files of the repository's corpus.
var corpusFiles = []string{
	"sample.ogg", "sample_retagged.ogg", "sample.opus", "sample_retagged.opus", "bench/book.opus",
}

func readCorpus(tb testing.TB, name string) []byte {
	tb.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", filepath.FromSlash(name)))
	if err != nil {
		tb.Fatal(err)
	}
	return data
}

// rawPage is one page of a file as stored: its bytes, its parsed fields and
// the CRC the file carries (which the page parser never reads).
type rawPage struct {
	raw    []byte
	page   *oggPage
	stored uint32
}

// rawPagesOf splits data, which must be whole pages, into rawPages.
func rawPagesOf(tb testing.TB, data []byte) []rawPage {
	tb.Helper()
	s := newPageStream(bytes.NewReader(data), int64(len(data)), "x", false)
	var out []rawPage
	for off := int64(0); off < int64(len(data)); {
		page, next, err := s.next()
		if err != nil {
			tb.Fatalf("page at %d: %v", off, err)
		}
		raw := data[off:next]
		out = append(out, rawPage{raw, page, ebinary.LittleEndian.Uint32(raw[pageCRCOffset:])})
		off = next
	}
	return out
}

// slowCRC is the checksum bit by bit, straight from RFC 3533: polynomial
// 0x04C11DB7, not reflected, initial value 0, no final XOR.
func slowCRC(b []byte) uint32 {
	var crc uint32
	for _, c := range b {
		crc ^= uint32(c) << 24
		for range 8 {
			if crc&0x80000000 != 0 {
				crc = crc<<1 ^ 0x04C11DB7
			} else {
				crc <<= 1
			}
		}
	}
	return crc
}

func TestCRCUpdateMatchesTheBitwiseDefinition(t *testing.T) {
	t.Parallel()
	rng := rand.New(rand.NewPCG(1, 2))
	for _, n := range []int{0, 1, 4, 27, 282, 65307} {
		b := make([]byte, n)
		for i := range b {
			b[i] = byte(rng.UintN(256))
		}
		if got, want := crcUpdate(0, b), slowCRC(b); got != want {
			t.Errorf("%d bytes: crc %#08x, want %#08x", n, got, want)
		}
	}
	// The update is incremental: splitting the input changes nothing.
	b := []byte("an Ogg page, in pieces")
	if got, want := crcUpdate(crcUpdate(0, b[:7]), b[7:]), crcUpdate(0, b); got != want {
		t.Errorf("split crc %#08x, want %#08x", got, want)
	}
}

// TestCorpusPagesCarryTheirCRC recomputes the checksum of every page of every
// corpus file, with the CRC field zeroed, and compares it with the stored one:
// a wrong table, polynomial or initial value fails here.
func TestCorpusPagesCarryTheirCRC(t *testing.T) {
	t.Parallel()
	for _, name := range corpusFiles {
		for i, p := range rawPagesOf(t, readCorpus(t, name)) {
			zeroed := slices.Clone(p.raw)
			clear(zeroed[pageCRCOffset : pageCRCOffset+4])
			if got := crcUpdate(0, zeroed); got != p.stored {
				t.Errorf("%s page %d: crc %#08x, file has %#08x", name, i, got, p.stored)
			}
		}
	}
}

// TestCorpusPagesReencodeByteForByte encodes every parsed page of every corpus
// file again: header layout, lacing and CRC must reproduce the original bytes.
func TestCorpusPagesReencodeByteForByte(t *testing.T) {
	t.Parallel()
	for _, name := range corpusFiles {
		pages := rawPagesOf(t, readCorpus(t, name))
		if len(pages) == 0 {
			t.Fatalf("%s: no pages", name)
		}
		for i, p := range pages {
			if got := appendPage(nil, p.page); !bytes.Equal(got, p.raw) {
				t.Errorf("%s page %d: re-encoded page differs from the original (%d bytes against %d)", name, i, len(got), len(p.raw))
			}
		}
	}
}

func TestAppendLacing(t *testing.T) {
	t.Parallel()
	tests := []struct {
		n    int
		want []byte
	}{
		{0, []byte{0}},
		{1, []byte{1}},
		{254, []byte{254}},
		{255, []byte{255, 0}},
		{256, []byte{255, 1}},
		{510, []byte{255, 255, 0}},
		{511, []byte{255, 255, 1}},
	}
	for _, tt := range tests {
		if got := appendLacing([]byte{9}, tt.n); !bytes.Equal(got, append([]byte{9}, tt.want...)) {
			t.Errorf("appendLacing(%d) = %v, want %v", tt.n, got[1:], tt.want)
		}
	}
}

func TestPaginateSinglePage(t *testing.T) {
	t.Parallel()
	pages := paginate([][]byte{bytes.Repeat([]byte{1}, 100), bytes.Repeat([]byte{2}, 300)}, 7, 5, false)
	if len(pages) != 1 {
		t.Fatalf("%d pages, want 1", len(pages))
	}
	p := pages[0]
	if p.HeaderType != 0 || p.GranulePosition != 0 || p.SerialNumber != 7 || p.SequenceNumber != 5 ||
		!bytes.Equal(p.Lacing, []byte{100, 255, 45}) || len(p.Data) != 400 {
		t.Errorf("page = %+v", p)
	}
}

func TestPaginateSpansPages(t *testing.T) {
	t.Parallel()
	big := bytes.Repeat([]byte{0xAB}, 100_000)
	setup := bytes.Repeat([]byte{0xCD}, 40_000)
	pages := paginate([][]byte{big, setup}, 9, 1, true)
	// 100000 B = 392 values of 255 and one of 160; 40000 B = 156 of 255 and one of 220: 550 values.
	if len(pages) != 3 {
		t.Fatalf("%d pages, want 3", len(pages))
	}
	wantType := []byte{0, pageContinued, pageContinued | pageEOS}
	wantGranule := []int64{noGranule, 0, 0}
	for i, p := range pages {
		if p.HeaderType != wantType[i] || p.GranulePosition != wantGranule[i] || p.SequenceNumber != uint32(i+1) {
			t.Errorf("page %d: type %#x granule %d seq %d", i, p.HeaderType, p.GranulePosition, p.SequenceNumber)
		}
		if i < 2 && len(p.Lacing) != 255 {
			t.Errorf("page %d has %d lacing values, want 255", i, len(p.Lacing))
		}
	}
	// What the reader makes of the pages is what went in.
	var file bytes.Buffer
	for _, p := range pages {
		file.Write(appendPage(nil, p))
	}
	read := rawPagesOf(t, file.Bytes())
	got := make([]*oggPage, 0, len(read))
	for _, p := range read {
		got = append(got, p.page)
	}
	packets := extractPackets(got)
	if len(packets) != 2 || !bytes.Equal(packets[0], big) || !bytes.Equal(packets[1], setup) {
		t.Errorf("packets read back: %d, lengths %v", len(packets), []int{len(packets[0])})
	}
}

// TestPaginateAtTheSegmentLimit covers a packet that ends exactly on the last
// lacing value of a page, and one whose final 0 value starts the next page.
func TestPaginateAtTheSegmentLimit(t *testing.T) {
	t.Parallel()
	t.Run("packet ends on the last segment", func(t *testing.T) {
		t.Parallel()
		pk := bytes.Repeat([]byte{1}, 254*255+10) // 254 values of 255 and one of 10: 255 values
		pages := paginate([][]byte{pk, {7}}, 1, 0, false)
		if len(pages) != 2 || len(pages[0].Lacing) != 255 || pages[1].HeaderType != 0 || !bytes.Equal(pages[1].Lacing, []byte{1}) {
			t.Fatalf("pages = %+v", pages)
		}
		if pages[0].GranulePosition != 0 || pages[1].GranulePosition != 0 {
			t.Errorf("granules %d, %d, want 0, 0", pages[0].GranulePosition, pages[1].GranulePosition)
		}
	})
	t.Run("terminating zero on the next page", func(t *testing.T) {
		t.Parallel()
		pk := bytes.Repeat([]byte{1}, 255*255) // 255 values of 255, then a 0
		pages := paginate([][]byte{pk}, 1, 0, false)
		if len(pages) != 2 || pages[0].GranulePosition != noGranule || pages[1].HeaderType != pageContinued ||
			!bytes.Equal(pages[1].Lacing, []byte{0}) || len(pages[1].Data) != 0 {
			t.Fatalf("pages = %+v", pages)
		}
	})
	t.Run("an empty packet", func(t *testing.T) {
		t.Parallel()
		pages := paginate([][]byte{{}}, 1, 0, false)
		if len(pages) != 1 || !bytes.Equal(pages[0].Lacing, []byte{0}) {
			t.Fatalf("pages = %+v", pages)
		}
	})
	t.Run("no packets", func(t *testing.T) {
		t.Parallel()
		if pages := paginate(nil, 1, 0, true); len(pages) != 0 {
			t.Errorf("%d pages for no packets", len(pages))
		}
	})
}

// assembled returns the corpus file name with its header packets replaced by
// the pages paginate makes of newPackets and every audio page renumbered and
// re-checksummed: a file built only from this package's page encoder.
func assembled(tb testing.TB, name string, comment []byte) []byte {
	tb.Helper()
	data := readCorpus(tb, name)
	l, err := readLayout(tb.Context(), bytes.NewReader(data), int64(len(data)), name)
	if err != nil {
		tb.Fatal(err)
	}
	packets := slices.Clone(l.packets)
	packets[1] = comment
	out := slices.Clone(data[:l.idEnd])
	hdr := paginate(packets[1:], l.serial, l.firstSeq+1, l.eos)
	for _, p := range hdr {
		out = appendPage(out, p)
	}
	seq := hdr[len(hdr)-1].SequenceNumber
	rest := rawPagesOf(tb, data[l.audioStart:])
	for _, p := range rest {
		seq++
		p.page.SequenceNumber = seq
		out = appendPage(out, p.page)
	}
	return out
}

func TestAssembledFileParses(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct{ file, magic string }{{"sample.opus", "OpusTags"}, {"sample.ogg", "\x03vorbis"}} {
		comment := commentPacket(tt.magic, tt.magic[0] == 3, "TITLE=assembled", "BIG="+string(bytes.Repeat([]byte("x"), 100_000)))
		out := assembled(t, tt.file, comment)
		file := parseOggBytes(t, out)
		if got := file.Tags.GetFirst("TITLE"); got != "assembled" {
			t.Errorf("%s: title %q", tt.file, got)
		}
		for i, p := range rawPagesOf(t, out) {
			zeroed := slices.Clone(p.raw)
			clear(zeroed[pageCRCOffset : pageCRCOffset+4])
			if p.stored != crcUpdate(0, zeroed) || p.page.SequenceNumber != uint32(i) {
				t.Errorf("%s page %d: crc ok %v, seq %d", tt.file, i, p.stored == crcUpdate(0, zeroed), p.page.SequenceNumber)
			}
		}
	}
}

// TestAssembledFilePassesFFmpeg decodes a hand-assembled file with ffmpeg, an
// implementation that shares no code with this package: it flags a bad CRC on
// stderr. It skips when ffmpeg is absent, unless AUDIOMETA_REQUIRE_ORACLES=1.
func TestAssembledFilePassesFFmpeg(t *testing.T) {
	t.Parallel()
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		if os.Getenv("AUDIOMETA_REQUIRE_ORACLES") == "1" {
			t.Fatalf("oracle ffmpeg is required but not on PATH: %v", err)
		}
		t.Skip("oracle ffmpeg not on PATH")
	}
	for _, tt := range []struct{ file, magic string }{{"sample.opus", "OpusTags"}, {"sample.ogg", "\x03vorbis"}} {
		comment := commentPacket(tt.magic, tt.magic[0] == 3, "TITLE=assembled", "BIG="+string(bytes.Repeat([]byte("x"), 100_000)))
		path := filepath.Join(t.TempDir(), tt.file)
		if err := os.WriteFile(path, assembled(t, tt.file, comment), 0o600); err != nil {
			t.Fatal(err)
		}
		var stderr bytes.Buffer
		cmd := exec.CommandContext(t.Context(), ffmpeg, "-v", "error", "-i", path, "-f", "null", "-")
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil || stderr.Len() > 0 {
			t.Errorf("%s: ffmpeg: %v\n%s", tt.file, err, stderr.String())
		}
	}
}
