package ogg

import (
	ebinary "encoding/binary"
)

// pageFixedHeader is the length of an Ogg page header before its segment table.
const pageFixedHeader = 27

// maxSegments is how many lacing values one page holds.
const maxSegments = 255

// pageCRCOffset is where the four CRC bytes sit in a page.
const pageCRCOffset = 22

// noGranule is the granule position of a page on which no packet ends
// (RFC 3533 §6, field 4).
const noGranule = -1

// appendPage appends the encoding of p to dst, with its CRC computed. The
// caller guarantees what a parsed page already has: at most 255 lacing values,
// summing to len(p.Data).
func appendPage(dst []byte, p *oggPage) []byte {
	start := len(dst)
	dst = append(dst, capturePattern...)
	dst = append(dst, 0, p.HeaderType)
	dst = ebinary.LittleEndian.AppendUint64(dst, uint64(p.GranulePosition)) //nolint:gosec // G115: the granule position is a signed field; the cast is the encoding
	dst = ebinary.LittleEndian.AppendUint32(dst, p.SerialNumber)
	dst = ebinary.LittleEndian.AppendUint32(dst, p.SequenceNumber)
	dst = append(dst, 0, 0, 0, 0, byte(len(p.Lacing))) //nolint:gosec // G115: at most 255 lacing values; the four zeros are the CRC, filled in below
	dst = append(dst, p.Lacing...)
	dst = append(dst, p.Data...)
	ebinary.LittleEndian.PutUint32(dst[start+pageCRCOffset:], crcUpdate(0, dst[start:]))
	return dst
}

// appendLacing appends the lacing values of a packet of n bytes: n/255 values
// of 255 and a final n%255, which is 0 when n is a multiple of 255 (RFC 3533
// §6, "Ogg page"). A packet always ends with a value below 255.
func appendLacing(dst []byte, n int) []byte {
	for ; n >= 255; n -= 255 {
		dst = append(dst, 255)
	}
	return append(dst, byte(n)) //nolint:gosec // G115: n < 255 here
}

// paginate lays packets out back to back on pages of the stream serial,
// numbered from seq. A page takes as many lacing values as fit (255), so a
// packet may start on one page and continue on the next, and the last packet
// ends the last page. A page on which a packet ends has granule position 0, as
// the encoders write it on header pages; any other page has -1. The first page
// does not continue a packet. eos marks the last page as the end of the
// stream.
//
// The pages' Data aliases one copy of the packets.
func paginate(packets [][]byte, serial, seq uint32, eos bool) []*oggPage {
	var lacing []byte
	size := 0
	for _, p := range packets {
		lacing = appendLacing(lacing, len(p))
		size += len(p)
	}
	data := make([]byte, 0, size)
	for _, p := range packets {
		data = append(data, p...)
	}

	pages := make([]*oggPage, 0, (len(lacing)+maxSegments-1)/maxSegments)
	var continued byte
	for len(lacing) > 0 {
		page := &oggPage{
			HeaderType:      continued,
			GranulePosition: noGranule,
			SerialNumber:    serial,
			SequenceNumber:  seq,
			Lacing:          lacing[:min(len(lacing), maxSegments)],
		}
		n := 0
		for _, l := range page.Lacing {
			n += int(l)
			if l < 255 {
				page.GranulePosition = 0
			}
		}
		page.Data, data = data[:n], data[n:]
		lacing = lacing[len(page.Lacing):]
		continued = 0
		if page.Lacing[len(page.Lacing)-1] == 255 {
			continued = pageContinued
		}
		pages = append(pages, page)
		seq++
	}
	if eos && len(pages) > 0 {
		pages[len(pages)-1].HeaderType |= pageEOS
	}
	return pages
}
