package ogg

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/simonhull/audiometa/internal/types"
)

func layoutOf(t testing.TB, data []byte) (*layout, error) {
	t.Helper()
	return readLayout(t.Context(), bytes.NewReader(data), int64(len(data)), "x")
}

// TestReadLayoutCorpus checks the layout of each corpus file against what the
// note on the files records and against the reader's own header scan.
func TestReadLayoutCorpus(t *testing.T) {
	tests := []struct {
		file        string
		codec       *codec
		headerPages int
		packets     int
	}{
		{"sample.ogg", vorbisCodec, 1, 3}, // comment and setup share page 1
		{"sample_retagged.ogg", vorbisCodec, 1, 3},
		{"sample.opus", opusCodec, 1, 2},
		{"sample_retagged.opus", opusCodec, 1, 2},
		{"bench/book.opus", opusCodec, 1, 2},
	}
	for _, tt := range tests {
		data := readCorpus(t, tt.file)
		l, err := layoutOf(t, data)
		if err != nil {
			t.Fatalf("%s: %v", tt.file, err)
		}
		if l.codec != tt.codec || len(l.pages) != tt.headerPages || len(l.packets) != tt.packets {
			t.Errorf("%s: codec %s, %d header pages, %d packets", tt.file, l.codec.idName, len(l.pages), len(l.packets))
		}
		var file types.File
		h, err := readHeaderPackets(t.Context(), bytes.NewReader(data), int64(len(data)), "x", &file, maxHeaderBytes)
		if err != nil {
			t.Fatal(err)
		}
		if l.audioStart != h.audioStart || l.serial != h.serial {
			t.Errorf("%s: audioStart %d serial %d, header scan has %d and %d", tt.file, l.audioStart, l.serial, h.audioStart, h.serial)
		}
		for i, p := range h.packets {
			if !bytes.Equal(l.packets[i], p) {
				t.Errorf("%s: packet %d differs from the header scan's", tt.file, i)
			}
		}
		// The boundaries are page boundaries: page 0 ends at idEnd, audio starts at a page.
		var bounds []int64
		for off := int64(0); off < int64(len(data)); {
			off += int64(len(rawPagesOf(t, data[off:])[0].raw))
			bounds = append(bounds, off)
		}
		if !slices.Contains(bounds, l.idEnd) || !slices.Contains(bounds, l.audioStart) {
			t.Errorf("%s: idEnd %d and audioStart %d are not both page boundaries in %v", tt.file, l.idEnd, l.audioStart, bounds)
		}
	}
}

// TestReadLayoutSpansPages reads headers whose comment packet needs several pages.
func TestReadLayoutSpansPages(t *testing.T) {
	comment := commentPacket("OpusTags", false, "TITLE=big", "BIG="+string(bytes.Repeat([]byte("x"), 100_000)))
	file := multiPageFile(opusHeadPacket(), comment, 3, audioTail(3, 960))
	l, err := layoutOf(t, file)
	if err != nil {
		t.Fatal(err)
	}
	if len(l.pages) != 2 || l.lastSeq != 2 || l.firstSeq != 0 || !bytes.Equal(l.packets[1], comment) {
		t.Errorf("layout = %+v", l)
	}
}

func TestReadLayoutRefusals(t *testing.T) {
	id, head := vorbisIDPacket(), opusHeadPacket()
	comment := commentPacket("OpusTags", false, "TITLE=x")
	idPage := pageSpec{headerType: 0x02, serial: 1, lacing: lacingFor(len(head)), data: head}
	audio := audioTail(1, 960)

	tests := []struct {
		name    string
		file    []byte
		corrupt bool
		unsup   bool
	}{
		{"truncated inside the headers", buildOgg(idPage), true, false},
		{"page 0 holds more than the id packet", buildOgg(pageOf(1, head, comment)), true, false},
		{"page 0 packet continues", buildOgg(pageSpec{headerType: 0x02, serial: 1, lacing: []byte{255}, data: bytes.Repeat([]byte{1}, 255)}), true, false},
		{"audio shares the last header page", buildOgg(idPage, pageOf(1, comment, []byte{1, 2, 3})), true, false},
		{
			"dropped continuation",
			buildOgg(idPage, pageSpec{serial: 1, lacing: []byte{255}, data: bytes.Repeat([]byte{1}, 255)}, pageSpec{serial: 1, lacing: []byte{4}, data: []byte{1, 2, 3, 4}}),
			true, false,
		},
		{
			"continuation without an open packet",
			buildOgg(idPage, pageSpec{headerType: pageContinued, serial: 1, lacing: lacingFor(len(comment)), data: comment}),
			true, false,
		},
		{
			"stream ends on page 0",
			buildOgg(pageSpec{headerType: 0x02 | pageEOS, serial: 1, lacing: lacingFor(len(head)), data: head}, pageOf(1, comment), audio),
			true, false,
		},
		{"another stream among the headers", buildOgg(idPage, pageOf(2, comment), pageOf(1, comment), audio), false, true},
		{"unknown codec", buildOgg(pageSpec{headerType: 0x02, serial: 1, lacing: lacingFor(len(id) - 1), data: id[1:]}, pageOf(1, comment)), false, false},
	}
	for _, tt := range tests {
		_, err := layoutOf(t, tt.file)
		var ce *types.CorruptedFileError
		switch {
		case err == nil:
			t.Errorf("%s: no error", tt.name)
		case tt.corrupt && !errors.As(err, &ce):
			t.Errorf("%s: err = %v, want *CorruptedFileError", tt.name, err)
		case tt.unsup && !errors.Is(err, types.ErrUnsupportedWrite):
			t.Errorf("%s: err = %v, want ErrUnsupportedWrite", tt.name, err)
		}
	}
}

func TestReadLayoutCanceled(t *testing.T) {
	data := readCorpus(t, "sample.opus")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := readLayout(ctx, bytes.NewReader(data), int64(len(data)), "x"); !errors.Is(err, ctx.Err()) {
		t.Errorf("err = %v, want the context's", err)
	}
}

func TestScanStreams(t *testing.T) {
	head, comment := opusHeadPacket(), commentPacket("OpusTags", false, "TITLE=x")
	one := slices.Grow([]pageSpec{
		{headerType: 0x02, serial: 1, lacing: lacingFor(len(head)), data: head},
		pageOf(1, comment), audioTail(1, 960), audioTail(1, 1920),
	}, 1)
	file := buildOgg(one...)
	if err := scanStreams(t.Context(), bytes.NewReader(file), int64(len(file)), 0, 1); err != nil {
		t.Errorf("single stream: %v", err)
	}
	chained := buildOgg(append(one, audioTail(2, 5))...)
	if err := scanStreams(t.Context(), bytes.NewReader(chained), int64(len(chained)), 0, 1); !errors.Is(err, types.ErrUnsupportedWrite) {
		t.Errorf("chained: err = %v, want ErrUnsupportedWrite", err)
	}
	// Trailing garbage and a truncated page end the walk without an error.
	for _, tail := range [][]byte{[]byte("ID3 trailing tag, not a page"), file[:30]} {
		withTail := append(bytes.Clone(file), tail...)
		if err := scanStreams(t.Context(), bytes.NewReader(withTail), int64(len(withTail)), 0, 1); err != nil {
			t.Errorf("tail %q: %v", tail[:4], err)
		}
	}
}
