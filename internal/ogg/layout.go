package ogg

import (
	"context"
	ebinary "encoding/binary"
	"errors"
	"fmt"
	"io"
	"slices"

	"github.com/simonhull/audiometa/internal/binary"
	"github.com/simonhull/audiometa/internal/types"
)

// layout is where the header packets of a file's first logical stream sit:
// everything a rewrite needs to replace the comment header and copy the rest.
//
// A writable stream has the shape Vorbis I §A.2 and RFC 7845 §3 give it: page
// 0 holds the identification packet alone, the other header packets follow on
// pages that each end where a packet ends or continue it, the last header
// packet finishes its page, and audio starts on a fresh page.
type layout struct {
	codec      *codec
	serial     uint32
	firstSeq   uint32 // sequence number of page 0
	lastSeq    uint32 // sequence number of the last header page
	idEnd      int64  // offset just after page 0
	audioStart int64  // offset of the first page after the header pages
	eos        bool   // the last header page ends the stream
	packets    [][]byte
	pages      []headerPage // the pages between idEnd and audioStart
}

// headerPage is a header page of a stream as the file stores it, without its
// payload.
type headerPage struct {
	start, end int64 // the page's bytes
	typ        byte
	granule    int64
	seq        uint32
	lacing     []byte
}

// corruptedAt returns a [*types.CorruptedFileError] for path at off.
func corruptedAt(path string, off int64, reason string) error {
	return &types.CorruptedFileError{Path: path, Offset: off, Reason: reason}
}

// errMultiStream is returned when a file holds more than one logical stream.
var errMultiStream = fmt.Errorf("%w: Ogg file has more than one logical stream", types.ErrUnsupportedWrite)

// readLayout reads the header pages of the first logical stream of r. It
// returns [*types.CorruptedFileError] for a stream that ends inside its header
// packets, whose page 0 holds more than the identification packet, whose last
// header page also holds audio, or whose continuation flags disagree with its
// packets; an error wrapping [types.ErrUnsupportedWrite] for a page of another
// stream among the header pages; and the context's error once ctx is done.
//
// It looks only at the header pages. A later page of another stream (a
// chained or multiplexed file) is for [scanStreams] to find.
func readLayout(ctx context.Context, r io.ReaderAt, size int64, path string) (*layout, error) {
	lr := &layoutReader{path: path, layout: &layout{}, want: 1}
	s := newPageStream(r, size, path, true)
	for i, off := 0, int64(0); lr.complete < lr.want; i++ {
		if err := binary.Canceled(ctx, i); err != nil {
			return nil, err
		}
		switch {
		case off >= size:
			return nil, corruptedAt(path, size, "Ogg stream ends inside its header packets")
		case off > maxHeaderBytes || i > maxHeaderPages:
			return nil, corruptedAt(path, off, "Ogg header pages exceed the reader's limits")
		}
		page, next, err := s.next()
		if err != nil {
			if i == 0 {
				return nil, fmt.Errorf("read first Ogg page: %w", err)
			}
			return nil, corruptedAt(path, off, "Ogg header page unreadable: "+err.Error())
		}
		if err := lr.add(i, off, next, page); err != nil {
			return nil, err
		}
		off = next
	}
	return lr.layout, nil
}

// layoutReader is the state of a [readLayout] pass.
type layoutReader struct {
	*layout

	path     string
	tracker  packetTracker
	cur      []byte // the packet being assembled
	want     int    // packets to have complete after the page being read; the first page names the codec
	complete int    // packets complete so far
}

// add takes the i-th page of the stream, found at off and ending at next.
func (lr *layoutReader) add(i int, off, next int64, page *oggPage) error {
	if i == 0 {
		lr.serial, lr.firstSeq = page.SerialNumber, page.SequenceNumber
	} else if page.SerialNumber != lr.serial {
		return errMultiStream
	}
	if (page.HeaderType&pageContinued != 0) != lr.tracker.open {
		return corruptedAt(lr.path, off, "Ogg page continuation flag disagrees with its packets")
	}
	if ended := lr.take(page); ended >= 0 && ended != len(page.Lacing)-1 {
		if i == 0 {
			return corruptedAt(lr.path, 0, "Ogg page 0 does not hold only the identification packet")
		}
		return corruptedAt(lr.path, off, "Ogg header packets share a page with audio")
	}
	if i == 0 {
		if err := lr.setCodec(); err != nil {
			return err
		}
		lr.want = lr.codec.headerPackets
		lr.idEnd = next
	} else {
		lr.pages = append(lr.pages, headerPage{
			start: off, end: next, typ: page.HeaderType, granule: page.GranulePosition,
			seq: page.SequenceNumber, lacing: slices.Clone(page.Lacing),
		})
	}
	lr.lastSeq, lr.eos, lr.audioStart = page.SequenceNumber, page.HeaderType&pageEOS != 0, next
	if lr.eos && lr.complete < lr.want {
		return corruptedAt(lr.path, off, "Ogg stream ends inside its header packets")
	}
	return nil
}

// take appends page's packets to the layout until the wanted number are
// complete. It returns the lacing index of the segment that completed the last
// of them, or -1 when the page ends before that.
func (lr *layoutReader) take(page *oggPage) (ended int) {
	k := 0
	for seg := range lr.tracker.segments(page) {
		lr.cur = append(lr.cur, seg.data...)
		if seg.ends {
			lr.packets = append(lr.packets, lr.cur)
			lr.cur = nil
			if lr.complete++; lr.complete == lr.want {
				return k
			}
		}
		k++
	}
	return -1
}

// setCodec identifies the stream from its identification packet, which page 0
// must hold alone.
func (lr *layoutReader) setCodec() error {
	if len(lr.packets) != 1 {
		return corruptedAt(lr.path, 0, "Ogg page 0 does not hold only the identification packet")
	}
	if lr.codec = detectCodec(lr.packets[0]); lr.codec == nil {
		return errUnknownCodec
	}
	return nil
}

// pageSpan is where a page of the first stream lies in a file.
type pageSpan struct{ start, end int64 }

// walkPages walks the page headers of src from offset off, which must be a page
// boundary, to its end, calling visit (when non-nil) with each page. It
// returns an error wrapping [types.ErrUnsupportedWrite] when a page belongs to
// a stream other than serial: the file is chained or multiplexed. A page that
// is not whole in the file ends the walk without an error, as it ends the
// audio for [digester.AudioDigest]; the walk returns the offset where it
// stopped, where the trailing bytes begin. It reads each page's header and
// segment table only.
func walkPages(ctx context.Context, src io.ReaderAt, size, off int64, serial uint32, visit func(pageSpan)) (end int64, err error) {
	var buf [pageFixedHeader + maxSegments]byte
	for i := 0; off+pageFixedHeader <= size; i++ {
		if err := binary.Canceled(ctx, i); err != nil {
			return off, err
		}
		n, err := src.ReadAt(buf[:min(int64(len(buf)), size-off)], off)
		if n < pageFixedHeader {
			if err != nil && !errors.Is(err, io.EOF) {
				return off, fmt.Errorf("walk Ogg pages at offset %d: %w", off, err)
			}
			return off, nil
		}
		h := buf[:n]
		if string(h[:4]) != capturePattern || h[4] != 0 || n < pageFixedHeader+int(h[26]) {
			return off, nil
		}
		next := off + pageFixedHeader + int64(h[26])
		for _, l := range h[pageFixedHeader : pageFixedHeader+int(h[26])] {
			next += int64(l)
		}
		if next > size {
			return off, nil
		}
		if ebinary.LittleEndian.Uint32(h[14:18]) != serial {
			return off, errMultiStream
		}
		if visit != nil {
			visit(pageSpan{off, next})
		}
		off = next
	}
	return off, nil
}

// scanStreams returns an error wrapping [types.ErrUnsupportedWrite] when a
// page of src at or after off belongs to a stream other than serial; see
// [walkPages].
func scanStreams(ctx context.Context, src io.ReaderAt, size, off int64, serial uint32) error {
	_, err := walkPages(ctx, src, size, off, serial, nil)
	return err
}
