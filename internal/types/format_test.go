package types

import (
	"bytes"
	"testing"
)

func TestDetectFormat_Opus(t *testing.T) {
	// Create minimal valid Opus header:
	// - OggS page header (27 bytes) + segment table + OpusHead packet
	data := createMinimalOggPage("OpusHead")

	r := bytes.NewReader(data)
	format, err := DetectFormat(r, int64(len(data)), "test.opus")
	if err != nil {
		t.Fatalf("DetectFormat() error = %v", err)
	}
	if format != FormatOpus {
		t.Errorf("DetectFormat() = %v, want FormatOpus", format)
	}
}

func TestDetectFormat_Vorbis(t *testing.T) {
	// Create minimal valid Vorbis header:
	// - OggS page header + segment table + Vorbis identification packet
	data := createMinimalOggPage("\x01vorbis")

	r := bytes.NewReader(data)
	format, err := DetectFormat(r, int64(len(data)), "test.ogg")
	if err != nil {
		t.Fatalf("DetectFormat() error = %v", err)
	}
	if format != FormatOgg {
		t.Errorf("DetectFormat() = %v, want FormatOgg", format)
	}
}

func TestDetectFormat_FLAC(t *testing.T) {
	data := []byte("fLaC" + "\x00\x00\x00\x00") // fLaC magic + minimal header

	r := bytes.NewReader(data)
	format, err := DetectFormat(r, int64(len(data)), "test.flac")
	if err != nil {
		t.Fatalf("DetectFormat() error = %v", err)
	}
	if format != FormatFLAC {
		t.Errorf("DetectFormat() = %v, want FormatFLAC", format)
	}
}

func TestDetectFormat_MP3_ID3(t *testing.T) {
	data := []byte("ID3\x04\x00\x00\x00\x00\x00\x00") // ID3v2.4 header

	r := bytes.NewReader(data)
	format, err := DetectFormat(r, int64(len(data)), "test.mp3")
	if err != nil {
		t.Fatalf("DetectFormat() error = %v", err)
	}
	if format != FormatMP3 {
		t.Errorf("DetectFormat() = %v, want FormatMP3", format)
	}
}

func TestDetectFormat_MP3_FrameSync(t *testing.T) {
	// MP3 frame sync: 0xFF 0xFB (MPEG1 Layer3)
	data := []byte{0xFF, 0xFB, 0x90, 0x00, 0x00, 0x00, 0x00, 0x00}

	r := bytes.NewReader(data)
	format, err := DetectFormat(r, int64(len(data)), "test.mp3")
	if err != nil {
		t.Fatalf("DetectFormat() error = %v", err)
	}
	if format != FormatMP3 {
		t.Errorf("DetectFormat() = %v, want FormatMP3", format)
	}
}

func TestDetectFormat_WAV(t *testing.T) {
	data := []byte("RIFF\x00\x00\x00\x00WAVE")

	r := bytes.NewReader(data)
	format, err := DetectFormat(r, int64(len(data)), "test.wav")
	if err != nil {
		t.Fatalf("DetectFormat() error = %v", err)
	}
	if format != FormatWAV {
		t.Errorf("DetectFormat() = %v, want FormatWAV", format)
	}
}

func TestDetectFormat_AIFF(t *testing.T) {
	data := []byte("FORM\x00\x00\x00\x00AIFF")

	r := bytes.NewReader(data)
	format, err := DetectFormat(r, int64(len(data)), "test.aiff")
	if err != nil {
		t.Fatalf("DetectFormat() error = %v", err)
	}
	if format != FormatAIFF {
		t.Errorf("DetectFormat() = %v, want FormatAIFF", format)
	}
}

func TestDetectFormat_TooSmall(t *testing.T) {
	data := []byte("abc")

	r := bytes.NewReader(data)
	_, err := DetectFormat(r, int64(len(data)), "test.bin")
	if err == nil {
		t.Error("DetectFormat() should return error for file too small")
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

	for _, tc := range tests {
		got := tc.format.Extensions()
		if len(got) != len(tc.want) {
			t.Errorf("%v.Extensions() = %v, want %v", tc.format, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("%v.Extensions()[%d] = %q, want %q", tc.format, i, got[i], tc.want[i])
			}
		}
	}
}

// createMinimalOggPage creates a minimal Ogg page with the given first packet content.
func createMinimalOggPage(packetContent string) []byte {
	// Ogg page header structure:
	// - 4 bytes: "OggS" magic
	// - 1 byte: version (0)
	// - 1 byte: header type (0x02 = BOS)
	// - 8 bytes: granule position (-1)
	// - 4 bytes: serial number
	// - 4 bytes: page sequence number
	// - 4 bytes: checksum
	// - 1 byte: segment count
	// - N bytes: segment table
	// - data

	packetLen := len(packetContent)

	// Build header
	header := make([]byte, 27)
	copy(header[0:4], "OggS")
	header[4] = 0    // version
	header[5] = 0x02 // BOS flag
	// granule position -1 (0xFF * 8)
	for i := 6; i < 14; i++ {
		header[i] = 0xFF
	}
	// serial number (arbitrary)
	header[14] = 0x01
	// page sequence = 0
	// checksum = 0 (we're not validating it)
	header[26] = 1 // one segment

	// Segment table: one entry with packet length
	segmentTable := []byte{byte(packetLen)}

	// Combine: header + segment table + packet
	result := make([]byte, 0, 27+1+packetLen)
	result = append(result, header...)
	result = append(result, segmentTable...)
	result = append(result, []byte(packetContent)...)

	return result
}
