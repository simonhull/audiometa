package mp3

import (
	"os"
	"testing"
	"time"

	"github.com/simonhull/audiometa/internal/types"
)

func TestParse_ValidMP3(t *testing.T) {
	// Create a minimal valid MP3 with ID3v2 tag
	data := createMinimalMP3WithID3()

	tmpFile, err := os.CreateTemp("", "test*.mp3")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())

	tmpFile.Write(data)
	tmpFile.Close()

	// Open file for parsing
	f, err := os.Open(tmpFile.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}

	// Parse the file
	p := &parser{}
	file, err := p.Parse(f, stat.Size(), tmpFile.Name())
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if file.Format != types.FormatMP3 {
		t.Errorf("expected FormatMP3, got %v", file.Format)
	}

	if file.Size == 0 {
		t.Error("expected non-zero file size")
	}

	if file.Audio.Codec != "MP3" {
		t.Errorf("expected codec MP3, got %s", file.Audio.Codec)
	}
}

func TestParse_FileNotFound(t *testing.T) {
	_, err := os.Open("/nonexistent/path.mp3")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
}

func TestParse_EmptyFile(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "test*.mp3")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())
	tmpFile.Close()

	// Open file for parsing
	f, err := os.Open(tmpFile.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}

	// Parse the file
	p := &parser{}
	file, err := p.Parse(f, stat.Size(), tmpFile.Name())
	// Empty file doesn't necessarily error - it just returns minimal metadata with warnings
	if err != nil {
		// Error is acceptable
		return
	}
	// If no error, should have warnings
	if file != nil && len(file.Warnings) == 0 {
		t.Error("expected warnings for empty file")
	}
}

// createMinimalMP3WithID3 creates a minimal MP3 file with ID3v2.3 tag
func createMinimalMP3WithID3() []byte {
	data := make([]byte, 0, 1024)

	// ID3v2.3 header (10 bytes)
	data = append(data, []byte{
		'I', 'D', '3', // ID3 magic
		0x03, 0x00, // Version 2.3.0
		0x00,                   // Flags
		0x00, 0x00, 0x00, 0x10, // Size (synchsafe) = 16 bytes
	}...)

	// TIT2 frame (Title)
	data = append(data, []byte{
		'T', 'I', 'T', '2', // Frame ID
		0x00, 0x00, 0x00, 0x0B, // Size = 11 bytes
		0x00, 0x00, // Flags
		0x00,                                             // Encoding (ISO-8859-1)
		'T', 'e', 's', 't', ' ', 'T', 'i', 't', 'l', 'e', // Text
	}...)

	// Padding to match declared size
	for len(data) < 26 { // 10 (header) + 16 (declared size)
		data = append(data, 0)
	}

	// Add minimal MP3 frame (for technical info parsing)
	// Frame sync (11 bits): 0xFFE
	// MPEG Version 1, Layer III, 128 kbps, 44.1 kHz, mono
	data = append(data, []byte{
		0xFF, 0xFB, // Frame sync + version + layer
		0x90, 0x00, // Bitrate index (128kbps) + sample rate (44.1kHz) + padding
		// Add some audio data
		0x00, 0x00, 0x00, 0x00,
	}...)

	return data
}

func TestDecode_Synchsafe(t *testing.T) {
	tests := []struct {
		input    []byte
		expected uint32
	}{
		{[]byte{0x00, 0x00, 0x00, 0x00}, 0},
		{[]byte{0x00, 0x00, 0x00, 0x7F}, 127},
		{[]byte{0x00, 0x00, 0x01, 0x00}, 128},
		{[]byte{0x00, 0x00, 0x02, 0x00}, 256},
		{[]byte{0x7F, 0x7F, 0x7F, 0x7F}, 0x0FFFFFFF},
	}

	for _, tt := range tests {
		result := decodeSynchsafe(tt.input)
		if result != tt.expected {
			t.Errorf("decodeSynchsafe(%v) = %d, expected %d", tt.input, result, tt.expected)
		}
	}
}

func TestParseYear(t *testing.T) {
	tests := []struct {
		input    string
		expected int
	}{
		{"2023", 2023},
		{"2023-11-08", 2023},
		{"invalid", 0},
		{"", 0},
		{"1899", 0}, // Out of range
		{"2101", 0}, // Out of range
	}

	for _, tt := range tests {
		result := parseYear(tt.input)
		if result != tt.expected {
			t.Errorf("parseYear(%q) = %d, expected %d", tt.input, result, tt.expected)
		}
	}
}

func TestParseTrackNumber(t *testing.T) {
	tests := []struct {
		input         string
		expectedNum   int
		expectedTotal int
	}{
		{"5", 5, 0},
		{"5/12", 5, 12},
		{"1/1", 1, 1},
		{"invalid", 0, 0},
	}

	for _, tt := range tests {
		num, total := parseTrackNumber(tt.input)
		if num != tt.expectedNum || total != tt.expectedTotal {
			t.Errorf("parseTrackNumber(%q) = (%d, %d), expected (%d, %d)",
				tt.input, num, total, tt.expectedNum, tt.expectedTotal)
		}
	}
}

func TestParseTextFrame(t *testing.T) {
	file := &types.File{
		Tags: types.Tags{},
	}

	// TIT2 (Title) frame
	frame := ID3v2Frame{
		ID:   "TIT2",
		Data: []byte{0x00, 'T', 'e', 's', 't'}, // ISO-8859-1 encoding + "Test"
	}
	parseTextFrame(frame, file)

	if file.Tags.Title != "Test" {
		t.Errorf("expected title 'Test', got '%s'", file.Tags.Title)
	}

	// TPE1 (Artist) frame
	frame = ID3v2Frame{
		ID:   "TPE1",
		Data: []byte{0x00, 'A', 'r', 't', 'i', 's', 't'},
	}
	parseTextFrame(frame, file)

	if file.Tags.Artist != "Artist" {
		t.Errorf("expected artist 'Artist', got '%s'", file.Tags.Artist)
	}

	// TRCK (Track number) frame
	frame = ID3v2Frame{
		ID:   "TRCK",
		Data: []byte{0x00, '5', '/', '1', '2'},
	}
	parseTextFrame(frame, file)

	if file.Tags.TrackNumber != 5 || file.Tags.TrackTotal != 12 {
		t.Errorf("expected track 5/12, got %d/%d", file.Tags.TrackNumber, file.Tags.TrackTotal)
	}
}

func TestParseTXXXFrame(t *testing.T) {
	file := &types.File{
		Tags: types.Tags{},
	}

	// TXXX frame with Narrator field
	// Format: [encoding][description\0][value]
	frame := ID3v2Frame{
		ID: "TXXX",
		Data: []byte{
			0x00,                                         // ISO-8859-1 encoding
			'N', 'a', 'r', 'r', 'a', 't', 'o', 'r', 0x00, // "Narrator\0"
			'J', 'o', 'h', 'n', ' ', 'D', 'o', 'e', // "John Doe"
		},
	}
	parseTXXXFrame(frame, file)

	if file.Tags.Narrator != "John Doe" {
		t.Errorf("expected narrator 'John Doe', got '%s'", file.Tags.Narrator)
	}

	// TXXX frame with Series field
	frame = ID3v2Frame{
		ID: "TXXX",
		Data: []byte{
			0x00,
			'S', 'e', 'r', 'i', 'e', 's', 0x00,
			'H', 'a', 'r', 'r', 'y', ' ', 'P', 'o', 't', 't', 'e', 'r',
		},
	}
	parseTXXXFrame(frame, file)

	if file.Tags.Series != "Harry Potter" {
		t.Errorf("expected series 'Harry Potter', got '%s'", file.Tags.Series)
	}
}

func TestParseChapterFrames(t *testing.T) {
	// Create mock CHAP frames
	frames := []ID3v2Frame{
		{
			ID: "CHAP",
			// [element_id\0][start(4)][end(4)][start_offset(4)][end_offset(4)]
			// Note: CHAP frames do NOT have an encoding byte at the start
			Data: []byte{
				'c', 'h', '0', '1', 0x00, // element ID "ch01\0"
				0x00, 0x00, 0x00, 0x00, // start time = 0ms
				0x00, 0x00, 0x27, 0x10, // end time = 10000ms
				0xFF, 0xFF, 0xFF, 0xFF, // start offset (unused)
				0xFF, 0xFF, 0xFF, 0xFF, // end offset (unused)
				// No TIT2 subframe - will use element ID as title
			},
		},
		{
			ID: "CHAP",
			Data: []byte{
				'c', 'h', '0', '2', 0x00, // element ID "ch02\0"
				0x00, 0x00, 0x27, 0x10, // start = 10000ms
				0x00, 0x00, 0x4E, 0x20, // end = 20000ms
				0xFF, 0xFF, 0xFF, 0xFF, // start offset (unused)
				0xFF, 0xFF, 0xFF, 0xFF, // end offset (unused)
			},
		},
	}

	chapters := parseChapterFrames(frames, 20*time.Second)

	if len(chapters) != 2 {
		t.Fatalf("expected 2 chapters, got %d", len(chapters))
	}

	// Check first chapter
	if chapters[0].Index != 1 {
		t.Errorf("chapter 0: expected index 1, got %d", chapters[0].Index)
	}
	if chapters[0].StartTime != 0 {
		t.Errorf("chapter 0: expected start time 0, got %v", chapters[0].StartTime)
	}
	if chapters[0].EndTime != 10*time.Second {
		t.Errorf("chapter 0: expected end time 10s, got %v", chapters[0].EndTime)
	}

	// Check second chapter
	if chapters[1].Index != 2 {
		t.Errorf("chapter 1: expected index 2, got %d", chapters[1].Index)
	}
	if chapters[1].StartTime != 10*time.Second {
		t.Errorf("chapter 1: expected start time 10s, got %v", chapters[1].StartTime)
	}
}
