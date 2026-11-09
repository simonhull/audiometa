// Package ogg parses Ogg Vorbis and Opus files.
//
// It walks Ogg pages, reassembles the identification and comment header
// packets, reads Vorbis comments (including CHAPTER comments and
// METADATA_BLOCK_PICTURE artwork) and derives duration from the last
// granule position. It registers itself for [types.FormatOgg] and
// [types.FormatOpus].
package ogg

import (
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
func splitPackets(pages []*Page) (packets [][]byte, open bool) {
	var t packetTracker
	var cur []byte
	for _, page := range pages {
		for seg := range t.segments(page) {
			cur = append(cur, seg.data...)
			if seg.ends {
				packets = append(packets, cur)
				cur = nil
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

// findLastGranulePosition searches backwards from the end of file
// to find the last Ogg page's granule position.
//
// This is used to calculate the duration of the audio stream.
func findLastGranulePosition(sr *binary.SafeReader, fileSize int64) (int64, error) {
	// Search last 64KB for final page (typical max page size)
	searchStart := max(fileSize-65536, 0)

	searchSize := fileSize - searchStart
	buf := make([]byte, searchSize)
	if err := sr.ReadAt(buf, searchStart, "search region"); err != nil {
		return 0, err
	}

	// Find last "OggS" marker
	lastOggPos := int64(-1)
	// Payload bytes can contain "OggS", so only accept a plausible page header.
	for i := len(buf) - 4; i >= 0; i-- {
		if plausiblePageAt(buf, i) {
			lastOggPos = searchStart + int64(i)
			break
		}
	}

	if lastOggPos < 0 {
		return 0, errors.New("could not find last Ogg page")
	}

	// Read granule position from last page (at offset 6 from "OggS")
	granule, err := sr.ReadLE[uint64](lastOggPos+6, "granule position")
	if err != nil {
		return 0, err
	}

	return int64(granule), nil //nolint:gosec // G115: RFC 3533 granule position is a signed field; all-ones (-1) means no packet ends on the page
}
