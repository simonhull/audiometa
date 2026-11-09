package types

import (
	"bytes"
	"testing"

	"github.com/simonhull/audiometa/internal/binary"
)

func TestLeadingID3v2Size(t *testing.T) {
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
			sr := binary.NewSafeReader(bytes.NewReader(tt.head), int64(len(tt.head)), "x")
			if got := LeadingID3v2Size(sr); got != tt.want {
				t.Errorf("LeadingID3v2Size = %d, want %d", got, tt.want)
			}
		})
	}
}
