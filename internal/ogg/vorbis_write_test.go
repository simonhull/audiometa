package ogg

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/simonhull/audiometa/internal/registry"
	"github.com/simonhull/audiometa/internal/types"
)

func TestWriter_RegisteredForVorbis(t *testing.T) {
	w, ok := registry.GetWriter(types.FormatOgg).(*writer)
	if !ok || w.codec != vorbisCodec {
		t.Errorf("writer for Ogg Vorbis = %T %+v", registry.GetWriter(types.FormatOgg), w)
	}
	if _, ok := registry.GetDigester(types.FormatOgg).(digester); !ok {
		t.Errorf("digester for Ogg Vorbis is %T", registry.GetDigester(types.FormatOgg))
	}
}

// TestWriteVorbisCarriesTheHeadersOpaquely checks what a Vorbis rewrite must
// not touch: the identification packet and the setup packet, which the writer
// never parses, stay identical however the comment packet grows or shrinks,
// and the setup packet still ends the last header page.
func TestWriteVorbisCarriesTheHeadersOpaquely(t *testing.T) {
	for _, name := range []string{"sample.ogg", "sample_retagged.ogg"} {
		src := readCorpus(t, name)
		before, err := layoutOf(t, src)
		if err != nil {
			t.Fatal(err)
		}
		for label, size := range map[string]int{"tiny": 1, "one page": 3000, "two pages": 70_000, "four pages": 200_000} {
			out := writeBytes(t, src, keyEdit("BIG", strings.Repeat("v", size)))
			checkStructure(t, name+" "+label, out)
			preserved(t, name+" "+label, src, out)
			after, err := layoutOf(t, out)
			if err != nil {
				t.Fatalf("%s %s: %v", name, label, err)
			}
			if !bytes.Equal(after.packets[0], before.packets[0]) || !bytes.Equal(after.packets[2], before.packets[2]) {
				t.Errorf("%s %s: identification or setup packet changed", name, label)
			}
			if got := parseOggBytes(t, out).Tags.GetFirst("BIG"); len(got) != size {
				t.Errorf("%s %s: BIG has %d bytes, want %d", name, label, len(got), size)
			}
			// A header page carries granule position 0 when a packet ends on it and -1 otherwise,
			// and the last one ends the setup packet.
			hdr := mustPages(t, out)[1 : 1+len(after.pages)]
			for i, p := range hdr {
				want := int64(noGranule)
				if packetEndsOn(p.page) {
					want = 0
				}
				if p.page.GranulePosition != want || (i == len(hdr)-1 && want != 0) {
					t.Errorf("%s %s: header page %d granule %d, want %d", name, label, i, p.page.GranulePosition, want)
				}
			}
		}
	}
}

// packetEndsOn reports whether a packet ends on page.
func packetEndsOn(page *oggPage) bool {
	for _, l := range page.Lacing {
		if l < 255 {
			return true
		}
	}
	return false
}

func TestWriteVorbisKeepsVendorAndFramingBit(t *testing.T) {
	src := readCorpus(t, "sample.ogg")
	before, err := layoutOf(t, src)
	if err != nil {
		t.Fatal(err)
	}
	out := writeBytes(t, src, keyEdit("TITLE", "Another title"))
	after, err := layoutOf(t, out)
	if err != nil {
		t.Fatal(err)
	}
	vendor := func(l *layout) string {
		body := l.packets[1][len(vorbisCodec.commentMagic):]
		n := int(body[0]) | int(body[1])<<8
		return string(body[4 : 4+n])
	}
	if vendor(before) != vendor(after) || vendor(after) == "" {
		t.Errorf("vendor %q became %q", vendor(before), vendor(after))
	}
	if last := after.packets[1][len(after.packets[1])-1]; last != 1 {
		t.Errorf("the comment packet ends with %#x, want the framing bit 1", last)
	}
	if !bytes.HasPrefix(after.packets[1], []byte("\x03vorbis")) {
		t.Errorf("comment packet starts %q", after.packets[1][:7])
	}
}

func TestWriteVorbisRefusals(t *testing.T) {
	good := readCorpus(t, "sample.ogg")
	chained := append(bytes.Clone(good), withSerial(t, good, 99)...)
	if _, err := tryWrite(t, chained, keyEdit("TITLE", "x")); !errors.Is(err, types.ErrUnsupportedWrite) {
		t.Errorf("chained: err = %v, want ErrUnsupportedWrite", err)
	}
	l, err := layoutOf(t, good)
	if err != nil {
		t.Fatal(err)
	}
	// Ending inside the setup packet leaves the stream without its headers.
	var ce *types.CorruptedFileError
	if _, err := tryWrite(t, good[:l.audioStart-100], keyEdit("TITLE", "x")); !errors.As(err, &ce) {
		t.Errorf("truncated setup: err = %v, want *CorruptedFileError", err)
	}
	// The Opus writer does not take a Vorbis stream.
	var buf bytes.Buffer
	w := testWriter(t)
	if _, err := w.WriteFile(t.Context(), &buf, bytes.NewReader(good), int64(len(good)), keyEdit("TITLE", "x")); !errors.As(err, &ce) || buf.Len() != 0 {
		t.Errorf("Opus writer on Vorbis: err = %v, %d bytes", err, buf.Len())
	}
}

func TestWriteVorbisTrailingBytesAndRenumbering(t *testing.T) {
	src := append(readCorpus(t, "sample_retagged.ogg"), "tail bytes"...)
	for name, size := range map[string]int{"same": 10, "more pages": 80_000} {
		out := writeBytes(t, src, keyEdit("BIG", strings.Repeat("t", size)))
		if !bytes.HasSuffix(out, []byte("tail bytes")) {
			t.Errorf("%s: trailing bytes lost", name)
		}
		preserved(t, name, src, out)
	}
}
