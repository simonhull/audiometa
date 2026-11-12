package types

import (
	"bytes"
	"slices"
	"testing"

	"github.com/simonhull/audiometa/internal/binary"
)

func TestLeadingID3v2Size(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		head []byte
		want int64
	}{
		{"no tag", []byte("fLaC\x00\x00\x00\x00\x00\x00"), 0},
		{"too short", []byte("ID3\x03"), 0},
		{"ID3v2.3, 300-byte body", []byte{'I', 'D', '3', 3, 0, 0, 0, 0, 0x02, 0x2C}, 310},
		{"ID3v2.4 with footer", []byte{'I', 'D', '3', 4, 0, 0x10, 0, 0, 0, 20}, 40},
		{"ID3v2.3 ignores the footer flag", []byte{'I', 'D', '3', 3, 0, 0x10, 0, 0, 0, 20}, 30},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sr := binary.NewSafeReader(bytes.NewReader(tt.head), int64(len(tt.head)), "x")
			if got := LeadingID3v2Size(sr); got != tt.want {
				t.Errorf("LeadingID3v2Size = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestDetectFormat_ID3Prefix(t *testing.T) {
	t.Parallel()
	tag := func(version, flags byte, body int) []byte {
		h := []byte{'I', 'D', '3', version, 0, flags, 0, 0, byte(body >> 7 & 0x7F), byte(body & 0x7F)}
		return slices.Concat(h, make([]byte, body))
	}
	footer := []byte{'3', 'D', 'I', 4, 0, 0x10, 0, 0, 0, 20}
	frames := []byte{0xFF, 0xFB, 0x90, 0x00, 0, 0, 0, 0}
	tests := []struct {
		name string
		data []byte
		want Format
	}{
		{"ID3v2.3 then fLaC", append(tag(3, 0, 300), "fLaC\x00\x00\x00\x22"...), FormatFLAC},
		{"ID3v2.4 with footer then fLaC", slices.Concat(tag(4, 0x10, 20), footer, []byte("fLaC")), FormatFLAC},
		{"ID3v2.3 then MPEG frames", append(tag(3, 0, 300), frames...), FormatMP3},
		{"ID3v2.3 whose size runs past the end", tag(3, 0, 300)[:100], FormatMP3},
		{"fLaC inside the tag only", append(append(tag(3, 0, 4)[:10], "fLaC"...), frames...), FormatMP3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := DetectFormat(bytes.NewReader(tt.data), int64(len(tt.data)), "x")
			if err != nil || got != tt.want {
				t.Errorf("DetectFormat = %v, %v; want %v", got, err, tt.want)
			}
		})
	}
}
