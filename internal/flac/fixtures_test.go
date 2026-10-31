package flac

import (
	"bytes"
	"encoding/binary"
)

// flacBlock encodes a metadata block header (last flag, 7-bit type, 24-bit BE length) + payload.
func flacBlock(last bool, blockType byte, payload []byte) []byte {
	hdr := blockType
	if last {
		hdr |= 0x80
	}
	n := len(payload)
	return append([]byte{hdr, byte(n >> 16), byte(n >> 8), byte(n)}, payload...)
}

// streamInfoPayload returns a 34-byte STREAMINFO body for 16-bit stereo.
func streamInfoPayload(sampleRate, totalSamples uint64) []byte {
	buf := &bytes.Buffer{}
	_ = binary.Write(buf, binary.BigEndian, uint16(4096)) // min block size
	_ = binary.Write(buf, binary.BigEndian, uint16(4096)) // max block size
	buf.Write(make([]byte, 6))                            // min/max frame size
	packed := sampleRate<<44 | uint64(1)<<41 | uint64(15)<<36 | totalSamples
	_ = binary.Write(buf, binary.BigEndian, packed)
	buf.Write(make([]byte, 16)) // MD5
	return buf.Bytes()
}

// vorbisCommentPayload returns a VORBIS_COMMENT block body (little-endian lengths).
func vorbisCommentPayload(comments ...string) []byte {
	buf := &bytes.Buffer{}
	vendor := "audiometa-test"
	_ = binary.Write(buf, binary.LittleEndian, uint32(len(vendor)))
	buf.WriteString(vendor)
	_ = binary.Write(buf, binary.LittleEndian, uint32(len(comments)))
	for _, c := range comments {
		_ = binary.Write(buf, binary.LittleEndian, uint32(len(c)))
		buf.WriteString(c)
	}
	return buf.Bytes()
}

// buildFLAC returns "fLaC" followed by the given encoded blocks.
func buildFLAC(parts ...[]byte) []byte {
	out := []byte("fLaC")
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}
