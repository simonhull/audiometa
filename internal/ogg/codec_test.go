package ogg

import (
	"testing"

	"github.com/simonhull/audiometa/internal/types"
)

func TestDetectCodec(t *testing.T) {
	t.Parallel()

	vorbisID := append([]byte{0x01}, "vorbis"...)
	tests := []struct {
		name          string
		packet        []byte
		headerPackets int // 0: no codec
		format        types.Format
	}{
		{"opus", []byte("OpusHead\x01\x02"), 2, types.FormatOpus},
		{"vorbis", vorbisID, 3, types.FormatOgg},
		{"short opus magic", []byte("OpusHea"), 0, 0},
		{"short vorbis magic", []byte("\x01vorbi"), 0, 0},
		{"empty", nil, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			c := detectCodec(tt.packet)
			if tt.headerPackets == 0 {
				if c != nil {
					t.Fatalf("detectCodec(%q) = %+v, want nil", tt.packet, c)
				}
				return
			}
			if c == nil {
				t.Fatalf("detectCodec(%q) = nil", tt.packet)
			}
			if c.headerPackets != tt.headerPackets || c.format != tt.format {
				t.Errorf("headerPackets, format = %d, %v; want %d, %v", c.headerPackets, c.format, tt.headerPackets, tt.format)
			}
		})
	}
}
