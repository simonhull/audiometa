package mp3

import (
	"bytes"
	"context"
	"encoding/binary"
	"testing"
	"unicode/utf16"

	"github.com/simonhull/audiometa/internal/types"
)

// synchsafe encodes n as a 4-byte ID3v2 synchsafe integer.
func synchsafe(n int) []byte {
	return []byte{byte(n >> 21 & 0x7F), byte(n >> 14 & 0x7F), byte(n >> 7 & 0x7F), byte(n & 0x7F)}
}

// id3Frame builds one ID3v2.3/2.4 frame: v2.4 sizes are synchsafe, v2.3 plain big-endian.
func id3Frame(version byte, id string, payload []byte) []byte {
	h := make([]byte, 10)
	copy(h, id)
	if version == 4 {
		copy(h[4:8], synchsafe(len(payload)))
	} else {
		binary.BigEndian.PutUint32(h[4:8], uint32(len(payload)))
	}
	return append(h, payload...)
}

// id3Tag wraps frames in an ID3v2 header of the given major version (no flags, no padding).
func id3Tag(version byte, frames ...[]byte) []byte {
	body := bytes.Join(frames, nil)
	h := append([]byte{'I', 'D', '3', version, 0, 0}, synchsafe(len(body))...)
	return append(h, body...)
}

// mpegFrame is a minimal MPEG-1 Layer III frame header (128 kbps, 44.1 kHz) plus filler.
var mpegFrame = []byte{0xFF, 0xFB, 0x90, 0x00, 0, 0, 0, 0}

// latin1Text is a text-frame payload with encoding byte 0.
func latin1Text(s string) []byte { return append([]byte{0x00}, s...) }

// utf16Text is a text-frame payload with encoding byte 1, LE BOM, UTF-16LE units.
func utf16Text(s string) []byte {
	out := []byte{0x01, 0xFF, 0xFE}
	for _, u := range utf16.Encode([]rune(s)) {
		out = append(out, byte(u), byte(u>>8))
	}
	return out
}

// chapFrameBody builds a CHAP payload: elementID\0, start/end ms, 0xFFFFFFFF offsets, subframes.
func chapFrameBody(elementID string, startMS, endMS uint32, subframes ...[]byte) []byte {
	b := append([]byte(elementID), 0)
	b = binary.BigEndian.AppendUint32(b, startMS)
	b = binary.BigEndian.AppendUint32(b, endMS)
	b = append(b, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF)
	return append(b, bytes.Join(subframes, nil)...)
}

// parseBytes runs the full MP3 parser (dispatch included) over data.
func parseBytes(t *testing.T, data []byte) *types.File {
	t.Helper()
	f, err := (&parser{}).Parse(context.Background(), bytes.NewReader(data), int64(len(data)), "test.mp3")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return f
}

// id3v22Frame builds an ID3v2.2 frame: 3-char ID, 3-byte big-endian size, payload.
func id3v22Frame(id string, payload []byte) []byte {
	n := len(payload)
	return append([]byte{id[0], id[1], id[2], byte(n >> 16), byte(n >> 8), byte(n)}, payload...)
}

// id3v1Trailer builds a 128-byte ID3v1 tag. track == 0 writes the v1.0 30-byte
// comment; otherwise the v1.1 layout with a track number.
func id3v1Trailer(title, artist, album, year, comment string, track byte) []byte {
	b := make([]byte, id3v1Size)
	copy(b, "TAG")
	copy(b[3:33], title)
	copy(b[33:63], artist)
	copy(b[63:93], album)
	copy(b[93:97], year)
	if track == 0 {
		copy(b[97:127], comment)
	} else {
		copy(b[97:125], comment)
		b[125], b[126] = 0, track
	}
	return b
}
