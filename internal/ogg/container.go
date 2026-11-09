// Package ogg parses Ogg Vorbis and Opus files.
//
// It walks Ogg pages, reassembles the identification and comment header
// packets, reads Vorbis comments (including CHAPTER comments and
// METADATA_BLOCK_PICTURE artwork) and derives duration from the last granule
// position of the first logical stream. It registers itself for
// [types.FormatOgg] and [types.FormatOpus].
package ogg

import (
	"context"
	ebinary "encoding/binary"
	"errors"
	"fmt"
	"iter"

	"github.com/simonhull/audiometa/internal/binary"
)

// capturePattern starts every Ogg page.
const capturePattern = "OggS"

// Page represents an Ogg page.
//
// An Ogg page is the fundamental unit of the Ogg container format.
// Each page contains a header and payload data.
type Page struct {
	Data            []byte
	Lacing          []byte // segment table; each value < 255 terminates a packet
	GranulePosition int64
	SerialNumber    uint32
	SequenceNumber  uint32
	HeaderType      byte
}

// readPage reads an Ogg page at the given offset.
//
// Returns the page, next offset, and any error encountered.
func readPage(sr *binary.SafeReader, offset int64) (*Page, int64, error) {
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

	// Calculate total data size
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

	page := &Page{
		HeaderType:      headerType,
		GranulePosition: int64(granule), //nolint:gosec // G115: RFC 3533 granule position is a signed field; all-ones (-1) means no packet ends on the page
		SerialNumber:    serial,
		SequenceNumber:  sequence,
		Data:            data,
		Lacing:          segments,
	}

	// Calculate next page offset
	nextOffset := dataOffset + int64(dataSize)

	return page, nextOffset, nil
}

// laceValues returns the page's segment table, synthesizing one (a single
// packet filling the page) for pages built without it.
func (p *Page) laceValues() []byte {
	if p.Lacing != nil {
		return p.Lacing
	}
	n := len(p.Data)
	lacing := make([]byte, 0, n/255+1)
	for ; n >= 255; n -= 255 {
		lacing = append(lacing, 255)
	}
	return append(lacing, byte(n)) //nolint:gosec // G115: the loop above leaves 0 <= n < 255
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
func (t *packetTracker) segments(page *Page) iter.Seq[segment] {
	return func(yield func(segment) bool) {
		if t.open && page.HeaderType&pageContinued == 0 {
			t.open = false
			if !yield(segment{ends: true}) {
				return
			}
		}
		data := page.Data
		for _, lace := range page.laceValues() {
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
func splitPackets(pages []*Page) (packets [][]byte, open bool) {
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
func extractPackets(pages []*Page) [][]byte {
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
