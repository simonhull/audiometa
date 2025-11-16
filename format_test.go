package audiometa

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// createMockM4B creates a minimal valid M4B/M4A file header.
func createMockM4B(brand string) []byte {
	buf := &bytes.Buffer{}

	// ftyp atom size (28 bytes)
	binary.Write(buf, binary.BigEndian, uint32(28))
	// ftyp atom type
	buf.WriteString("ftyp")
	// major brand
	buf.WriteString(brand)
	// minor version
	binary.Write(buf, binary.BigEndian, uint32(0))
	// compatible brands (just repeat the brand)
	buf.WriteString(brand)
	buf.WriteString(brand)

	return buf.Bytes()
}

// createInvalidFile creates a file with invalid ftyp.
func createInvalidFile() []byte {
	buf := &bytes.Buffer{}
	// Invalid atom size
	binary.Write(buf, binary.BigEndian, uint32(8))
	// Wrong type
	buf.WriteString("XXXX")
	return buf.Bytes()
}

func TestDetectFormat_M4B(t *testing.T) {
	data := createMockM4B("M4B ")

	format, err := DetectFormat(bytes.NewReader(data), int64(len(data)), "test.m4b")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if format != FormatM4B {
		t.Errorf("expected FormatM4B, got %v", format)
	}
}

func TestDetectFormat_M4A(t *testing.T) {
	data := createMockM4B("M4A ")

	format, err := DetectFormat(bytes.NewReader(data), int64(len(data)), "test.m4a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if format != FormatM4A {
		t.Errorf("expected FormatM4A, got %v", format)
	}
}

func TestDetectFormat_MP42(t *testing.T) {
	// mp42 is also valid M4A
	data := createMockM4B("mp42")

	format, err := DetectFormat(bytes.NewReader(data), int64(len(data)), "test.m4a")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if format != FormatM4A {
		t.Errorf("expected FormatM4A for mp42, got %v", format)
	}
}

func TestDetectFormat_TooSmall(t *testing.T) {
	// File too small to contain ftyp
	data := []byte{0x00, 0x00}

	_, err := DetectFormat(bytes.NewReader(data), int64(len(data)), "tiny.m4b")
	if err == nil {
		t.Fatal("expected error for file too small")
	}

	if _, ok := err.(*UnsupportedFormatError); !ok {
		t.Errorf("expected UnsupportedFormatError, got %T", err)
	}
}

func TestDetectFormat_InvalidFtyp(t *testing.T) {
	data := createInvalidFile()

	_, err := DetectFormat(bytes.NewReader(data), int64(len(data)), "invalid.m4b")
	if err == nil {
		t.Fatal("expected error for invalid ftyp")
	}

	if _, ok := err.(*UnsupportedFormatError); !ok {
		t.Errorf("expected UnsupportedFormatError, got %T", err)
	}
}

func TestDetectFormat_UnsupportedBrand(t *testing.T) {
	// Create file with unsupported brand
	data := createMockM4B("XXXX")

	_, err := DetectFormat(bytes.NewReader(data), int64(len(data)), "unsupported.mp4")
	if err == nil {
		t.Fatal("expected error for unsupported brand")
	}

	if _, ok := err.(*UnsupportedFormatError); !ok {
		t.Errorf("expected UnsupportedFormatError, got %T", err)
	}
}

func TestFormat_String(t *testing.T) {
	tests := []struct {
		format   Format
		expected string
	}{
		{FormatFLAC, "FLAC"},
		{FormatMP3, "MP3"},
		{FormatM4A, "M4A"},
		{FormatM4B, "M4B"},
		{FormatOgg, "Ogg Vorbis"},
		{FormatOpus, "Opus"},
		{FormatWAV, "WAV"},
		{FormatAIFF, "AIFF"},
		{FormatUnknown, "Unknown"},
	}

	for _, tt := range tests {
		if got := tt.format.String(); got != tt.expected {
			t.Errorf("Format(%d).String() = %s, want %s", tt.format, got, tt.expected)
		}
	}
}

func TestFormat_Extensions(t *testing.T) {
	tests := []struct {
		format Format
		want   []string
	}{
		{FormatFLAC, []string{".flac"}},
		{FormatMP3, []string{".mp3"}},
		{FormatM4A, []string{".m4a", ".mp4", ".m4p"}},
		{FormatM4B, []string{".m4b"}},
		{FormatOgg, []string{".ogg", ".oga"}},
		{FormatOpus, []string{".opus"}},
		{FormatWAV, []string{".wav"}},
		{FormatAIFF, []string{".aiff", ".aif"}},
		{FormatUnknown, nil},
	}

	for _, tt := range tests {
		got := tt.format.Extensions()
		if len(got) != len(tt.want) {
			t.Errorf("Format(%s).Extensions() = %v, want %v", tt.format, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("Format(%s).Extensions()[%d] = %q, want %q", tt.format, i, got[i], tt.want[i])
			}
		}
	}
}

func TestDetectFormat_FLAC(t *testing.T) {
	// FLAC magic bytes: "fLaC"
	data := []byte("fLaC")
	data = append(data, make([]byte, 100)...) // Add some padding

	format, err := DetectFormat(bytes.NewReader(data), int64(len(data)), "test.flac")
	if err != nil {
		t.Fatalf("DetectFormat failed: %v", err)
	}

	if format != FormatFLAC {
		t.Errorf("DetectFormat() = %v, want %v", format, FormatFLAC)
	}
}

func TestDetectFormat_MP3_WithID3(t *testing.T) {
	// MP3 with ID3v2 tag
	data := []byte("ID3")
	data = append(data, make([]byte, 100)...) // Add some padding

	format, err := DetectFormat(bytes.NewReader(data), int64(len(data)), "test.mp3")
	if err != nil {
		t.Fatalf("DetectFormat failed: %v", err)
	}

	if format != FormatMP3 {
		t.Errorf("DetectFormat() = %v, want %v", format, FormatMP3)
	}
}

func TestDetectFormat_MP3_WithoutID3(t *testing.T) {
	// MP3 frame sync: 0xFF 0xFB (common MP3 header)
	data := []byte{0xFF, 0xFB, 0x00, 0x00}
	data = append(data, make([]byte, 100)...) // Add some padding

	format, err := DetectFormat(bytes.NewReader(data), int64(len(data)), "test.mp3")
	if err != nil {
		t.Fatalf("DetectFormat failed: %v", err)
	}

	if format != FormatMP3 {
		t.Errorf("DetectFormat() = %v, want %v", format, FormatMP3)
	}
}

func TestDetectFormat_Ogg(t *testing.T) {
	// Ogg magic bytes: "OggS"
	data := []byte("OggS")
	data = append(data, make([]byte, 100)...) // Add some padding

	format, err := DetectFormat(bytes.NewReader(data), int64(len(data)), "test.ogg")
	if err != nil {
		t.Fatalf("DetectFormat failed: %v", err)
	}

	if format != FormatOgg {
		t.Errorf("DetectFormat() = %v, want %v", format, FormatOgg)
	}
}

func TestDetectFormat_WAV(t *testing.T) {
	// WAV header: RIFF....WAVE
	buf := &bytes.Buffer{}
	buf.WriteString("RIFF")
	binary.Write(buf, binary.LittleEndian, uint32(100)) // File size
	buf.WriteString("WAVE")
	buf.Write(make([]byte, 100)) // More data

	data := buf.Bytes()
	format, err := DetectFormat(bytes.NewReader(data), int64(len(data)), "test.wav")
	if err != nil {
		t.Fatalf("DetectFormat failed: %v", err)
	}

	if format != FormatWAV {
		t.Errorf("DetectFormat() = %v, want %v", format, FormatWAV)
	}
}

func TestDetectFormat_AIFF(t *testing.T) {
	// AIFF header: FORM....AIFF
	buf := &bytes.Buffer{}
	buf.WriteString("FORM")
	binary.Write(buf, binary.BigEndian, uint32(100)) // File size
	buf.WriteString("AIFF")
	buf.Write(make([]byte, 100)) // More data

	data := buf.Bytes()
	format, err := DetectFormat(bytes.NewReader(data), int64(len(data)), "test.aiff")
	if err != nil {
		t.Fatalf("DetectFormat failed: %v", err)
	}

	if format != FormatAIFF {
		t.Errorf("DetectFormat() = %v, want %v", format, FormatAIFF)
	}
}
