package m4a

import "testing"

// TestImageSniffing_Characterization pins image MIME and dimension sniffing.
func TestImageSniffing_Characterization(t *testing.T) {
	jpeg := func(marker byte) []byte {
		return []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x04, 0x00, 0x00, 0xFF, marker, 0x00, 0x11, 0x08, 0x01, 0x2C, 0x02, 0x58, 0x03, 0x00, 0x00}
	}
	png := append([]byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A, 0, 0, 0, 13, 'I', 'H', 'D', 'R', 0, 0, 2, 0, 0, 0, 1, 0x80}, make([]byte, 8)...)

	tests := []struct {
		name         string
		data         []byte
		mime         string
		wantW, wantH int
	}{
		{"jpeg SOF0", jpeg(0xC0), "image/jpeg", 600, 300},
		{"jpeg SOF1", jpeg(0xC1), "image/jpeg", 600, 300},
		{"jpeg SOF2", jpeg(0xC2), "image/jpeg", 600, 300},
		{"jpeg without SOF", []byte{0xFF, 0xD8, 0xFF, 0xE0, 0, 0, 0, 0, 0, 0, 0, 0}, "image/jpeg", 0, 0},
		{"png IHDR", png, "image/png", 512, 384},
		{"png bad signature", append([]byte{0x00}, png[1:]...), "image/png", 0, 0},
		{"png truncated", png[:20], "image/png", 0, 0},
		{"gif has no dimensions", []byte("GIF89a\x01\x00\x01\x00"), "image/gif", 0, 0},
		{"empty", nil, "image/jpeg", 0, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w, h := detectImageDimensions(tc.data, tc.mime)
			if w != tc.wantW || h != tc.wantH {
				t.Errorf("dimensions = %dx%d, want %dx%d", w, h, tc.wantW, tc.wantH)
			}
		})
	}
}
