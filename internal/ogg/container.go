// Package ogg implements Ogg Vorbis format parsing.
package ogg

import (
	"fmt"

	"github.com/simonhull/audiometa/internal/binary"
)

// Page represents an Ogg page.
//
// An Ogg page is the fundamental unit of the Ogg container format.
// Each page contains a header and payload data.
type Page struct {
	HeaderType      byte   // Bit flags: 0x01=continued, 0x02=BOS, 0x04=EOS
	GranulePosition int64  // Position in samples
	SerialNumber    uint32 // Logical bitstream identifier
	SequenceNumber  uint32 // Page sequence number
	Data            []byte // Page payload (one or more packets)
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
	if string(magic) != "OggS" {
		return nil, 0, fmt.Errorf("invalid Ogg page at offset %d", offset)
	}

	// Read stream structure version (should be 0x00)
	version, err := binary.Read[uint8](sr, offset+4, "version")
	if err != nil {
		return nil, 0, err
	}
	if version != 0 {
		return nil, 0, fmt.Errorf("unsupported Ogg version: %d", version)
	}

	// Read header fields
	headerType, err := binary.Read[uint8](sr, offset+5, "header type")
	if err != nil {
		return nil, 0, err
	}

	granule, err := binary.ReadLE[uint64](sr, offset+6, "granule position")
	if err != nil {
		return nil, 0, err
	}

	serial, err := binary.ReadLE[uint32](sr, offset+14, "serial number")
	if err != nil {
		return nil, 0, err
	}

	sequence, err := binary.ReadLE[uint32](sr, offset+18, "sequence number")
	if err != nil {
		return nil, 0, err
	}

	segmentCount, err := binary.Read[uint8](sr, offset+26, "segment count")
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
		GranulePosition: int64(granule),
		SerialNumber:    serial,
		SequenceNumber:  sequence,
		Data:            data,
	}

	// Calculate next page offset
	nextOffset := dataOffset + int64(dataSize)

	return page, nextOffset, nil
}

// extractPackets extracts complete packets from a series of pages.
//
// Ogg packets can span multiple pages. A packet ends when a segment
// has a size < 255 bytes.
//
// This function handles:
//   - Packets split across multiple pages (continued packets)
//   - Multiple packets in a single page
//   - Packets exactly 255 bytes (continued in next segment)
func extractPackets(pages []*Page) [][]byte {
	var packets [][]byte
	var currentPacket []byte

	for _, page := range pages {
		// If page is a continuation and we have a packet in progress,
		// append to current packet
		if page.HeaderType&0x01 != 0 && len(currentPacket) > 0 {
			currentPacket = append(currentPacket, page.Data...)
		} else {
			// Not a continuation page
			// Save any packet in progress
			if len(currentPacket) > 0 {
				packets = append(packets, currentPacket)
			}
			// Start new packet with this page's data
			currentPacket = make([]byte, len(page.Data))
			copy(currentPacket, page.Data)
		}
	}

	// Don't forget the last packet
	if len(currentPacket) > 0 {
		packets = append(packets, currentPacket)
	}

	return packets
}

// findLastGranulePosition searches backwards from the end of file
// to find the last Ogg page's granule position.
//
// This is used to calculate the duration of the audio stream.
func findLastGranulePosition(sr *binary.SafeReader, fileSize int64) (int64, error) {

	// Search last 64KB for final page (typical max page size)
	searchStart := fileSize - 65536
	if searchStart < 0 {
		searchStart = 0
	}

	searchSize := fileSize - searchStart
	buf := make([]byte, searchSize)
	if err := sr.ReadAt(buf, searchStart, "search region"); err != nil {
		return 0, err
	}

	// Find last "OggS" marker
	lastOggPos := int64(-1)
	for i := len(buf) - 4; i >= 0; i-- {
		if buf[i] == 'O' && buf[i+1] == 'g' && buf[i+2] == 'g' && buf[i+3] == 'S' {
			lastOggPos = searchStart + int64(i)
			break
		}
	}

	if lastOggPos < 0 {
		return 0, fmt.Errorf("could not find last Ogg page")
	}

	// Read granule position from last page (at offset 6 from "OggS")
	granule, err := binary.ReadLE[uint64](sr, lastOggPos+6, "granule position")
	if err != nil {
		return 0, err
	}

	return int64(granule), nil
}
