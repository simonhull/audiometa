package m4a

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/simonhull/audiometa/internal/types"
	audiobinary "github.com/simonhull/audiometa/internal/binary"
)

// createDataAtom creates a data atom with string content
func createDataAtom(value string) []byte {
	buf := &bytes.Buffer{}

	// data atom size
	dataSize := uint32(8 + 8 + len(value)) // header + version/flags/reserved + value
	binary.Write(buf, binary.BigEndian, dataSize)

	// data atom type
	buf.WriteString("data")

	// version (1) + flags (3) + reserved (4)
	binary.Write(buf, binary.BigEndian, uint32(1)) // version=0, flags=1 (UTF-8 text)
	binary.Write(buf, binary.BigEndian, uint32(0)) // reserved

	// value
	buf.WriteString(value)

	return buf.Bytes()
}

// createMetadataItem creates an iTunes metadata item (e.g., ©nam for title)
// For iTunes tags, itemType should be the 4-byte tag code as bytes (e.g., []byte{0xA9, 'n', 'a', 'm'} for ©nam)
func createMetadataItem(itemType []byte, value string) []byte {
	if len(itemType) != 4 {
		panic("itemType must be exactly 4 bytes")
	}

	buf := &bytes.Buffer{}

	dataAtom := createDataAtom(value)

	// item size (header + data atom)
	itemSize := uint32(8 + len(dataAtom))
	binary.Write(buf, binary.BigEndian, itemSize)

	// item type (4 bytes)
	buf.Write(itemType)

	// data atom
	buf.Write(dataAtom)

	return buf.Bytes()
}

func TestParseMetadataTag_String(t *testing.T) {
	// Create ©nam (title) item - 0xA9 = © in MP4 tags
	data := createMetadataItem([]byte{0xA9, 'n', 'a', 'm'}, "Test Title")

	sr := audiobinary.NewSafeReader(bytes.NewReader(data), int64(len(data)), "test.m4b")
	atom, err := readAtomHeader(sr, 0)
	if err != nil {
		t.Fatalf("failed to read atom header: %v", err)
	}

	t.Logf("Atom type: %s, size: %d, data size: %d", atom.Type, atom.Size, atom.DataSize())

	value, err := parseMetadataTag(sr, atom)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if value != "Test Title" {
		t.Errorf("expected 'Test Title', got '%s'", value)
	}
}

func TestParseMetadataTag_EmptyData(t *testing.T) {
	// Create item with no data atom
	buf := &bytes.Buffer{}
	itemSize := uint32(8) // just header
	binary.Write(buf, binary.BigEndian, itemSize)
	buf.Write([]byte{0xA9, 'n', 'a', 'm'})

	data := buf.Bytes()
	sr := audiobinary.NewSafeReader(bytes.NewReader(data), int64(len(data)), "test.m4b")
	atom, _ := readAtomHeader(sr, 0)

	value, err := parseMetadataTag(sr, atom)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if value != "" {
		t.Errorf("expected empty string, got '%s'", value)
	}
}

func TestExtractIlstMetadata(t *testing.T) {
	// Create ilst atom with multiple items
	titleItem := createMetadataItem([]byte{0xA9, 'n', 'a', 'm'}, "My Book")      // ©nam
	artistItem := createMetadataItem([]byte{0xA9, 'A', 'R', 'T'}, "Author Name") // ©ART
	albumItem := createMetadataItem([]byte{0xA9, 'a', 'l', 'b'}, "Album Name")   // ©alb

	var ilstData []byte
	ilstData = append(ilstData, titleItem...)
	ilstData = append(ilstData, artistItem...)
	ilstData = append(ilstData, albumItem...)

	ilst := createMockAtom("ilst", ilstData)

	sr := audiobinary.NewSafeReader(bytes.NewReader(ilst), int64(len(ilst)), "test.m4b")
	ilstAtom, _ := readAtomHeader(sr, 0)

	file := &types.File{}
	err := extractIlstMetadata(sr, ilstAtom, file)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if file.Tags.Title != "My Book" {
		t.Errorf("expected title 'My Book', got '%s'", file.Tags.Title)
	}

	if file.Tags.Artist != "Author Name" {
		t.Errorf("expected artist 'Author Name', got '%s'", file.Tags.Artist)
	}

	if file.Tags.Album != "Album Name" {
		t.Errorf("expected album 'Album Name', got '%s'", file.Tags.Album)
	}
}

func TestMapTagToField(t *testing.T) {
	tests := []struct {
		tag      string
		value    string
		checkFn  func(*types.File) string
		expected string
	}{
		{"\xA9nam", "Title", func(f *types.File) string { return f.Tags.Title }, "Title"},       // ©nam
		{"\xA9ART", "Artist", func(f *types.File) string { return f.Tags.Artist }, "Artist"},    // ©ART
		{"\xA9alb", "Album", func(f *types.File) string { return f.Tags.Album }, "Album"},       // ©alb
		{"\xA9gen", "Genre", func(f *types.File) string {
			if len(f.Tags.Genres) > 0 {
				return f.Tags.Genres[0]
			}
			return ""
		}, "Genre"}, // ©gen
		{"\xA9cmt", "Comment", func(f *types.File) string { return f.Tags.Comment }, "Comment"}, // ©cmt
	}

	for _, tt := range tests {
		file := &types.File{}
		mapTagToField(tt.tag, tt.value, file)

		got := tt.checkFn(file)
		if got != tt.expected {
			t.Errorf("tag %s: expected '%s', got '%s'", tt.tag, tt.expected, got)
		}
	}
}

// createTrackDataAtom creates a data atom with track number content
func createTrackDataAtom(trackNum, trackTotal uint16) []byte {
	buf := &bytes.Buffer{}

	// data atom size: header(8) + version/flags(4) + reserved(4) + track data(8)
	dataSize := uint32(8 + 4 + 4 + 8)
	binary.Write(buf, binary.BigEndian, dataSize)

	// data atom type
	buf.WriteString("data")

	// version (1) + flags (3)
	binary.Write(buf, binary.BigEndian, uint32(0)) // version=0, flags=0

	// reserved (4)
	binary.Write(buf, binary.BigEndian, uint32(0))

	// Track number structure:
	// [2 bytes] reserved
	// [2 bytes] track number
	// [2 bytes] track total
	// [2 bytes] reserved
	binary.Write(buf, binary.BigEndian, uint16(0))  // reserved
	binary.Write(buf, binary.BigEndian, trackNum)   // track number
	binary.Write(buf, binary.BigEndian, trackTotal) // track total
	binary.Write(buf, binary.BigEndian, uint16(0))  // reserved

	return buf.Bytes()
}

func TestParseTrackNumber_Success(t *testing.T) {
	tests := []struct {
		name          string
		trackNum      uint16
		trackTotal    uint16
		expectedNum   int
		expectedTotal int
	}{
		{"Book 2 of 4", 2, 4, 2, 4},
		{"Book 1 of 1", 1, 1, 1, 1},
		{"Chapter 15 of 69", 15, 69, 15, 69},
		{"Track 0 of 10", 0, 10, 0, 10},
		{"Track 5 of 0", 5, 0, 5, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create trkn atom with data atom inside
			dataAtom := createTrackDataAtom(tt.trackNum, tt.trackTotal)

			// Create trkn container
			buf := &bytes.Buffer{}
			trknSize := uint32(8 + len(dataAtom))
			binary.Write(buf, binary.BigEndian, trknSize)
			buf.WriteString("trkn")
			buf.Write(dataAtom)

			trkn := buf.Bytes()
			sr := audiobinary.NewSafeReader(bytes.NewReader(trkn), int64(len(trkn)), "test.m4b")
			trknAtom, err := readAtomHeader(sr, 0)
			if err != nil {
				t.Fatalf("failed to read trkn atom header: %v", err)
			}

			result, err := parseTrackNumber(sr, trknAtom)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if result.Number != tt.expectedNum {
				t.Errorf("expected track number %d, got %d", tt.expectedNum, result.Number)
			}

			if result.Total != tt.expectedTotal {
				t.Errorf("expected track total %d, got %d", tt.expectedTotal, result.Total)
			}
		})
	}
}

func TestParseTrackNumber_NoDataAtom(t *testing.T) {
	// Create trkn atom with no data atom inside
	buf := &bytes.Buffer{}
	trknSize := uint32(8) // just header
	binary.Write(buf, binary.BigEndian, trknSize)
	buf.WriteString("trkn")

	trkn := buf.Bytes()
	sr := audiobinary.NewSafeReader(bytes.NewReader(trkn), int64(len(trkn)), "test.m4b")
	trknAtom, _ := readAtomHeader(sr, 0)

	_, err := parseTrackNumber(sr, trknAtom)
	if err == nil {
		t.Error("expected error for missing data atom, got nil")
	}
}
