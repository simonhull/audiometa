package types

import "testing"

// form builds a 12-byte IFF-style head: a container tag, a size and a form type.
func form(container, formType string) []byte {
	return []byte(container + "\x00\x00\x00\x00" + formType)
}

func TestDetectRIFF(t *testing.T) {
	tests := []struct {
		name string
		head []byte
		size int64
		want Format
		ok   bool
	}{
		{"RIFF WAVE", form("RIFF", "WAVE"), 12, FormatWAV, true},
		{"RIFX WAVE", form("RIFX", "WAVE"), 12, FormatWAV, true},
		{"RF64 WAVE", form("RF64", "WAVE"), 12, FormatWAV, true},
		{"RIFF with another form type", form("RIFF", "AVI "), 12, FormatUnknown, false},
		{"RIFF shorter than 12 bytes", []byte("RIFF\x00\x00\x00\x00"), 8, FormatUnknown, false},
		{"not RIFF", form("FORM", "WAVE"), 12, FormatUnknown, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := detectRIFF(tt.head, tt.size)
			if got != tt.want || ok != tt.ok {
				t.Errorf("detectRIFF() = (%v, %v), want (%v, %v)", got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestDetectFORM(t *testing.T) {
	tests := []struct {
		name string
		head []byte
		size int64
		want Format
		ok   bool
	}{
		{"AIFF", form("FORM", "AIFF"), 12, FormatAIFF, true},
		{"AIFC", form("FORM", "AIFC"), 12, FormatAIFF, true},
		{"FORM with another form type", form("FORM", "8SVX"), 12, FormatUnknown, false},
		{"FORM shorter than 12 bytes", []byte("FORM\x00\x00\x00\x00"), 8, FormatUnknown, false},
		{"not FORM", form("RIFF", "AIFF"), 12, FormatUnknown, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := detectFORM(tt.head, tt.size)
			if got != tt.want || ok != tt.ok {
				t.Errorf("detectFORM() = (%v, %v), want (%v, %v)", got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestDetectOgg_ShortFile(t *testing.T) {
	// Under 36 bytes there is no room for an OpusHead packet: plain Ogg.
	head := []byte("OggS" + "\x00\x00\x00\x00")
	if got, ok := detectOgg(head, 8); got != FormatOgg || !ok {
		t.Errorf("detectOgg() = (%v, %v), want (FormatOgg, true)", got, ok)
	}
	if got, ok := detectOgg([]byte("RIFF\x00\x00\x00\x00"), 8); got != FormatUnknown || ok {
		t.Errorf("detectOgg(non-Ogg) = (%v, %v), want (FormatUnknown, false)", got, ok)
	}
}

func TestDetectMPEGSync(t *testing.T) {
	for _, tt := range []struct {
		head []byte
		ok   bool
	}{
		{[]byte{0xFF, 0xFB, 0x00, 0x00}, true},
		{[]byte{0xFF, 0xE0, 0x00, 0x00}, true},
		{[]byte{0xFF, 0xDF, 0x00, 0x00}, false},
		{[]byte{0x00, 0xFF, 0x00, 0x00}, false},
	} {
		if _, ok := detectMPEGSync(tt.head); ok != tt.ok {
			t.Errorf("detectMPEGSync(% x) ok = %v, want %v", tt.head, ok, tt.ok)
		}
	}
}
