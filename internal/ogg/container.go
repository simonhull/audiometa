// Package ogg parses Ogg Vorbis and Opus files.
//
// It walks Ogg pages, reassembles the identification and comment header
// packets, reads Vorbis comments (including CHAPTER comments and
// METADATA_BLOCK_PICTURE artwork) and derives duration from the last granule
// position of the first logical stream. It registers itself for
// [types.FormatOgg] and [types.FormatOpus].
package ogg

import (
	"bufio"
	"context"
	ebinary "encoding/binary"
	"errors"
	"fmt"
	"io"
	"iter"
	"slices"

	"github.com/simonhull/audiometa/internal/binary"
)

// capturePattern starts every Ogg page.
const capturePattern = "OggS"

// oggPage represents an Ogg page.
//
// An Ogg page is the fundamental unit of the Ogg container format.
// Each page contains a header and payload data.
type oggPage struct {
	Data            []byte
	Lacing          []byte // segment table; each value < 255 terminates a packet
	GranulePosition int64
	SerialNumber    uint32
	SequenceNumber  uint32
	HeaderType      byte
}

// pageStream reads consecutive Ogg pages from the start of a file through one
// buffered sequential reader, so each byte is fetched once and a page costs no
// extra read. A payload larger than the buffer is read straight into its
// destination.
//
// Errors match what reading each field through a [binary.SafeReader] reports:
// an [*binary.OutOfBoundsError] for a page cut short by the end of the file,
// "invalid Ogg page" or "unsupported Ogg version" for a bad header, and the
// reader's own error, wrapped, for a failing read.
type pageStream struct {
	br     *bufio.Reader
	off    int64 // file offset of the next page
	size   int64
	path   string
	shared bool    // next reuses its buffers: the caller must not keep the page
	page   oggPage // the page returned by next when shared
	data   []byte
	lacing []byte
}

// pageStreamBuffer is the read size of a pageStream: large enough that a page
// of typical audio size costs about one read.
const pageStreamBuffer = 16 << 10

// newPageStream returns a pageStream over the first size bytes of r. When
// shared is true, the page next returns and its Data and Lacing are only valid
// until the following call.
func newPageStream(r io.ReaderAt, size int64, path string, shared bool) *pageStream {
	return &pageStream{
		br:     bufio.NewReaderSize(io.NewSectionReader(r, 0, size), pageStreamBuffer),
		size:   size,
		path:   path,
		shared: shared,
	}
}

// check returns the error SafeReader reports for reading n bytes of what at off.
func (s *pageStream) check(what string, off int64, n int) error {
	// Mirrors binary.SafeReader.ReadAt: a zero-length read is valid anywhere in [0, size].
	if n == 0 {
		if off < 0 || off > s.size {
			return &binary.OutOfBoundsError{Path: s.path, What: what, Offset: off, Length: n, Size: s.size}
		}
		return nil
	}
	if off < 0 || off >= s.size || off+int64(n) > s.size {
		return &binary.OutOfBoundsError{Path: s.path, What: what, Offset: off, Length: n, Size: s.size}
	}
	return nil
}

// fill reads len(b) bytes of what, found at off, from the stream.
func (s *pageStream) fill(b []byte, what string, off int64) error {
	n, err := io.ReadFull(s.br, b)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF):
		return fmt.Errorf("%s: short read for %s at offset %d: got %d bytes, expected %d", s.path, what, off, n, len(b))
	default:
		return fmt.Errorf("%s: failed to read %s at offset %d: %w", s.path, what, off, err)
	}
}

// header reads and validates the fixed 27-byte header of the page at off, field
// by field in file order, so a truncated page reports the first field that
// does not fit.
func (s *pageStream) header(off int64) (fixed [27]byte, err error) {
	if avail := s.size - off; avail > 0 {
		if err := s.fill(fixed[:min(avail, int64(len(fixed)))], "Ogg magic", off); err != nil {
			return fixed, err
		}
	}
	if err := s.check("Ogg magic", off, 4); err != nil {
		return fixed, err
	}
	if string(fixed[:4]) != capturePattern {
		return fixed, fmt.Errorf("invalid Ogg page at offset %d", off)
	}
	if err := s.check("version", off+4, 1); err != nil {
		return fixed, err
	}
	if fixed[4] != 0 {
		return fixed, fmt.Errorf("unsupported Ogg version: %d", fixed[4])
	}
	for _, f := range [...]struct {
		what      string
		at, bytes int
	}{
		{"header type", 5, 1}, {"granule position", 6, 8}, {"serial number", 14, 4},
		{"sequence number", 18, 4}, {"segment count", 26, 1},
	} {
		if err := s.check(f.what, off+int64(f.at), f.bytes); err != nil {
			return fixed, err
		}
	}
	return fixed, nil
}

// buffer returns an n-byte slice for a page field: a fresh one, or when the
// stream is shared one backed by *keep, which grows to fit.
func (s *pageStream) buffer(keep *[]byte, n int) []byte {
	if !s.shared {
		return make([]byte, n)
	}
	if *keep == nil {
		*keep = make([]byte, 0, max(n, 1)) // never nil: a nil lacing means "synthesize one"
	}
	*keep = slices.Grow((*keep)[:0], n)
	return (*keep)[:n]
}

// next reads the page at the stream's offset and advances past it. It returns
// the page and the offset of the one after it.
func (s *pageStream) next() (*oggPage, int64, error) {
	off := s.off
	fixed, err := s.header(off)
	if err != nil {
		return nil, 0, err
	}

	// Segment table (each byte is the size of a segment, 0-255), then payload.
	segmentCount := int(fixed[26])
	if err := s.check("segment table", off+27, segmentCount); err != nil {
		return nil, 0, err
	}
	lacing := s.buffer(&s.lacing, segmentCount)
	if err := s.fill(lacing, "segment table", off+27); err != nil {
		return nil, 0, err
	}
	dataSize := 0
	for _, seg := range lacing {
		dataSize += int(seg)
	}
	dataOffset := off + 27 + int64(segmentCount)
	if err := s.check("page data", dataOffset, dataSize); err != nil {
		return nil, 0, err
	}
	data := s.buffer(&s.data, dataSize)
	if err := s.fill(data, "page data", dataOffset); err != nil {
		return nil, 0, err
	}

	page := &s.page
	if !s.shared {
		page = new(oggPage)
	}
	*page = oggPage{
		HeaderType:      fixed[5],
		GranulePosition: int64(ebinary.LittleEndian.Uint64(fixed[6:14])), //nolint:gosec // G115: RFC 3533 granule position is a signed field; all-ones (-1) means no packet ends on the page
		SerialNumber:    ebinary.LittleEndian.Uint32(fixed[14:18]),
		SequenceNumber:  ebinary.LittleEndian.Uint32(fixed[18:22]),
		Data:            data,
		Lacing:          lacing,
	}
	s.off = dataOffset + int64(dataSize)
	return page, s.off, nil
}

// pageContinued marks a page whose first segment continues the packet left
// open by the previous page of its stream (RFC 3533 §6).
const pageContinued = 0x01

// segment is one lacing segment of a page's payload.
type segment struct {
	data []byte // the segment's bytes; empty for a packet ended by a dropped continuation
	ends bool   // the segment ends its packet
}

// packetTracker follows packet boundaries across the pages of one logical
// stream. Its zero value starts at a packet boundary.
type packetTracker struct {
	open bool // the last segment seen continues onto the next page
}

// segments yields page's lacing segments in order (RFC 3533 §6): a lacing
// value below 255 ends a packet, 255 continues it, possibly onto the next
// page. When a packet is open but page does not continue it, the stream
// dropped the continuation: an empty segment ends that packet first. Lacing
// values that run past the payload are clamped to it.
//
// The tracker's state is updated as each segment is yielded, so a caller that
// stops early must not feed it further pages.
func (t *packetTracker) segments(page *oggPage) iter.Seq[segment] {
	return func(yield func(segment) bool) {
		if t.open && page.HeaderType&pageContinued == 0 {
			t.open = false
			if !yield(segment{ends: true}) {
				return
			}
		}
		data := page.Data
		for _, lace := range page.Lacing {
			n := min(int(lace), len(data))
			seg := segment{data: data[:n], ends: lace < 255}
			data = data[n:]
			t.open = !seg.ends
			if !yield(seg) {
				return
			}
		}
	}
}

// splitPackets assembles packets from a series of pages of one logical
// stream; see [packetTracker.segments]. The final packet is returned even if
// it is still open; open reports whether the last packet is unterminated.
//
// Packets are assembled in two passes so each is allocated once at its final
// size: the first sums the segment lengths, the second copies them.
func splitPackets(pages []*oggPage) (packets [][]byte, open bool) {
	var sizes []int
	var t packetTracker
	n := 0
	for _, page := range pages {
		for seg := range t.segments(page) {
			n += len(seg.data)
			if seg.ends {
				sizes = append(sizes, n)
				n = 0
			}
		}
	}
	if t.open {
		sizes = append(sizes, n)
	}

	alloc := func(k int) []byte {
		if k >= len(sizes) || sizes[k] == 0 {
			return nil
		}
		return make([]byte, 0, sizes[k])
	}
	packets = make([][]byte, 0, len(sizes))
	t = packetTracker{}
	cur := alloc(0)
	for _, page := range pages {
		for seg := range t.segments(page) {
			cur = append(cur, seg.data...)
			if seg.ends {
				packets = append(packets, cur)
				cur = alloc(len(packets))
			}
		}
	}
	if t.open {
		packets = append(packets, cur)
	}
	return packets, t.open
}

// extractPackets extracts packets from a series of pages, including a trailing
// unterminated packet. See splitPackets.
func extractPackets(pages []*oggPage) [][]byte {
	packets, _ := splitPackets(pages)
	return packets
}

// plausiblePageAt reports whether buf[i:] starts a well-formed Ogg page header
// whose declared length fits the buffer.
func plausiblePageAt(buf []byte, i int) bool {
	const fixedHeader = 27
	if i+fixedHeader > len(buf) || string(buf[i:i+4]) != capturePattern {
		return false
	}
	if buf[i+4] != 0 || buf[i+5] > 0x07 {
		return false
	}
	segCount := int(buf[i+26])
	if i+fixedHeader+segCount > len(buf) {
		return false
	}
	total := fixedHeader + segCount
	for _, l := range buf[i+fixedHeader : i+fixedHeader+segCount] {
		total += int(l)
	}
	return i+total <= len(buf)
}

// tailSize is how much of the file's end lastGranule searches first; a page is
// at most 65307 bytes.
const tailSize = 65536

// pageHeader is the fixed part of an Ogg page header, without the payload.
type pageHeader struct {
	granule    int64
	serial     uint32
	headerType byte
	next       int64 // offset of the following page
}

// readPageHeader reads the header and segment table of the page at off and
// computes where the next page starts, without reading the payload.
func readPageHeader(sr *binary.SafeReader, off int64) (pageHeader, error) {
	var fixed [27]byte
	if err := sr.ReadAt(fixed[:], off, "Ogg page header"); err != nil {
		return pageHeader{}, err
	}
	if string(fixed[:4]) != capturePattern || fixed[4] != 0 {
		return pageHeader{}, fmt.Errorf("invalid Ogg page at offset %d", off)
	}
	segments := make([]byte, fixed[26])
	if err := sr.ReadAt(segments, off+int64(len(fixed)), "segment table"); err != nil {
		return pageHeader{}, err
	}
	next := off + int64(len(fixed)) + int64(len(segments))
	for _, s := range segments {
		next += int64(s)
	}
	return pageHeader{
		granule:    int64(ebinary.LittleEndian.Uint64(fixed[6:14])), //nolint:gosec // G115: RFC 3533 granule position is a signed field; all-ones (-1) means no packet ends on the page
		serial:     ebinary.LittleEndian.Uint32(fixed[14:18]),
		headerType: fixed[5],
		next:       next,
	}, nil
}

// lastGranule returns the granule position of the last page of the first
// logical stream (h.serial) that has one set.
//
// It looks in the last 64 KiB of the file first. When the first stream ends
// earlier, as in a chained or multiplexed file, it walks page headers forward
// from the start of the stream's audio instead, stopping at the stream's
// end-of-stream page. It returns an error if no page of the stream carries a
// granule position, or the context's error if ctx is canceled.
func lastGranule(ctx context.Context, sr *binary.SafeReader, size int64, h headers) (int64, error) {
	tailStart := max(size-tailSize, 0)
	buf := make([]byte, size-tailStart)
	if err := sr.ReadAt(buf, tailStart, "search region"); err != nil {
		return 0, err
	}

	// Payload bytes can contain "OggS", so only accept a plausible page header.
	for i := len(buf) - 4; i >= 0; i-- {
		if !plausiblePageAt(buf, i) || ebinary.LittleEndian.Uint32(buf[i+14:]) != h.serial {
			continue
		}
		if g := int64(ebinary.LittleEndian.Uint64(buf[i+6:])); g >= 0 { //nolint:gosec // G115: RFC 3533 granule position is a signed field; all-ones (-1) means no packet ends on the page
			return g, nil
		}
	}

	var last int64 = -1
	for i, off := 0, max(h.audioStart, 0); off < size; i++ {
		if err := binary.Canceled(ctx, i); err != nil {
			return 0, err
		}
		ph, err := readPageHeader(sr, off)
		if err != nil {
			break
		}
		if ph.serial == h.serial {
			if ph.granule >= 0 {
				last = ph.granule
			}
			if ph.headerType&pageEOS != 0 {
				break
			}
		}
		off = ph.next
	}
	if last < 0 {
		return 0, errors.New("could not find a granule position for the first Ogg stream")
	}
	return last, nil
}
