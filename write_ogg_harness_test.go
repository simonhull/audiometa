package audiometa_test

// Independent Ogg oracles for the write round-trip harness: a page walker, the
// page checksum and the audio digest, none of which use the library or its
// internal packages.

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"testing"
)

// oggPage is one Ogg page found by oggPages.
type oggPage struct {
	start, end int // the page's bytes
	flags      byte
	serial     uint32
	seq        uint32
	crc        uint32
	lacing     []byte
	payload    []byte
}

// oggPages splits data into pages the way RFC 3533 describes them. It stops at
// the first byte that does not start a whole page and returns what follows.
func oggPages(data []byte) (pages []oggPage, tail []byte) {
	for off := 0; off < len(data); {
		if len(data)-off < 27 || string(data[off:off+4]) != "OggS" || data[off+4] != 0 {
			return pages, data[off:]
		}
		n := int(data[off+26])
		if len(data)-off < 27+n {
			return pages, data[off:]
		}
		lacing := data[off+27 : off+27+n]
		body := 0
		for _, l := range lacing {
			body += int(l)
		}
		end := off + 27 + n + body
		if end > len(data) {
			return pages, data[off:]
		}
		pages = append(pages, oggPage{
			start: off, end: end, flags: data[off+5],
			serial: binary.LittleEndian.Uint32(data[off+14:]), seq: binary.LittleEndian.Uint32(data[off+18:]),
			crc: binary.LittleEndian.Uint32(data[off+22:]), lacing: lacing, payload: data[off+27+n : end],
		})
		off = end
	}
	return pages, nil
}

// oggCRC is the Ogg page checksum, bit by bit: polynomial 0x04C11DB7, most
// significant bit first, initial value 0, no final XOR, over the page with its
// CRC field zeroed.
func oggCRC(page []byte) uint32 {
	var crc uint32
	for i, c := range page {
		if i >= 22 && i < 26 {
			c = 0
		}
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

// chainedOgg returns the Ogg file at path followed by a copy of it with another
// stream serial number: a chained file, which holds two logical streams.
func chainedOgg(t *testing.T, path string) []byte {
	t.Helper()
	data := readTestdata(t, path)
	second := bytes.Clone(data)
	pages, _ := oggPages(second)
	for _, p := range pages {
		binary.LittleEndian.PutUint32(second[p.start+14:], 0xC0FFEE)
		binary.LittleEndian.PutUint32(second[p.start+22:], oggCRC(second[p.start:p.end]))
	}
	return append(bytes.Clone(data), second...)
}

// oggAudioDigest hashes the packet bytes of the first logical stream after its
// header packets (three for Vorbis, two for Opus).
func oggAudioDigest(t *testing.T, data []byte) [32]byte {
	t.Helper()
	pages, _ := oggPages(data)
	if len(pages) == 0 {
		t.Fatal("ogg: no pages")
	}
	serial := pages[0].serial
	remaining := 2
	if bytes.HasPrefix(pages[0].payload, []byte("\x01vorbis")) {
		remaining = 3
	}
	h := sha256.New()
	for _, p := range pages {
		if p.serial != serial {
			continue
		}
		body := p.payload
		for _, l := range p.lacing {
			if remaining == 0 {
				break
			}
			body = body[l:]
			if l < 255 {
				remaining--
			}
		}
		if remaining == 0 {
			h.Write(body)
		}
		if p.flags&0x04 != 0 {
			break
		}
	}
	return [32]byte(h.Sum(nil))
}

// checkOggPages checks what every Ogg reader relies on and the oracles of
// libogg and ffmpeg enforce: each page's CRC is right, sequence numbers run on
// without a gap, BOS is on the first page only and EOS on the last only.
func checkOggPages(t *testing.T, data []byte) {
	t.Helper()
	pages, _ := oggPages(data)
	for i, p := range pages {
		if got := oggCRC(data[p.start:p.end]); got != p.crc {
			t.Errorf("ogg page %d: crc %#x, stored %#x", i, got, p.crc)
		}
		if p.seq != pages[0].seq+uint32(i) {
			t.Errorf("ogg page %d: sequence %d, want %d", i, p.seq, pages[0].seq+uint32(i))
		}
		if bos := p.flags&0x02 != 0; bos != (i == 0) {
			t.Errorf("ogg page %d: BOS %v", i, bos)
		}
		if p.flags&0x04 != 0 && i != len(pages)-1 {
			t.Errorf("ogg page %d: EOS before the last page", i)
		}
	}
}
