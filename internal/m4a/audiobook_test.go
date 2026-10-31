package m4a

import (
	"bytes"
	"context"
	"encoding/binary"
	"testing"

	audiobinary "github.com/simonhull/audiometa/internal/binary"
	"github.com/simonhull/audiometa/internal/types"
)

// createCustomAtom creates a custom (----) atom with mean, name, and data.
func createCustomAtom(namespace, fieldName, value string) []byte {
	buf := &bytes.Buffer{}

	// Create mean atom (namespace)
	// mean atom has version+flags (4 bytes) + data
	meanBuf := &bytes.Buffer{}
	binary.Write(meanBuf, binary.BigEndian, uint32(0)) // version+flags
	meanBuf.WriteString(namespace)
	meanAtom := createAtomWithType([]byte("mean"), meanBuf.Bytes())
	buf.Write(meanAtom)

	// Create name atom (field name)
	// name atom has version+flags (4 bytes) + data
	nameBuf := &bytes.Buffer{}
	binary.Write(nameBuf, binary.BigEndian, uint32(0)) // version+flags
	nameBuf.WriteString(fieldName)
	nameAtom := createAtomWithType([]byte("name"), nameBuf.Bytes())
	buf.Write(nameAtom)

	// Create data atom (value)
	dataAtom := createDataAtom(value)
	buf.Write(dataAtom)

	// Wrap in ---- atom
	customType := []byte{0x2D, 0x2D, 0x2D, 0x2D} // "----"
	return createAtomWithType(customType, buf.Bytes())
}

// createAtomWithType creates an atom with a specific type (as bytes).
func createAtomWithType(atomType []byte, data []byte) []byte {
	buf := &bytes.Buffer{}
	size := uint32(8 + len(data))
	binary.Write(buf, binary.BigEndian, size)
	buf.Write(atomType)
	buf.Write(data)
	return buf.Bytes()
}

func TestParseAudiobookTags_Narrator(t *testing.T) {
	// Create ilst with custom narrator atom
	narratorAtom := createCustomAtom("com.apple.iTunes", "Narrator", "Wil Wheaton")
	ilst := createMockAtom("ilst", narratorAtom)

	sr := audiobinary.NewSafeReader(bytes.NewReader(ilst), int64(len(ilst)), "test.m4b")
	ilstAtom, _ := readAtomHeader(sr, 0, int64(len(ilst)))

	file := &types.File{}
	err := parseAudiobookTags(context.Background(), sr, ilstAtom, file)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if file.Tags.Narrator != "Wil Wheaton" {
		t.Errorf("expected narrator 'Wil Wheaton', got '%s'", file.Tags.Narrator)
	}
}

func TestParseAudiobookTags_Series(t *testing.T) {
	seriesAtom := createCustomAtom("com.apple.iTunes", "Series", "The Expanse")
	ilst := createMockAtom("ilst", seriesAtom)

	sr := audiobinary.NewSafeReader(bytes.NewReader(ilst), int64(len(ilst)), "test.m4b")
	ilstAtom, _ := readAtomHeader(sr, 0, int64(len(ilst)))

	file := &types.File{}
	err := parseAudiobookTags(context.Background(), sr, ilstAtom, file)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if file.Tags.Series != "The Expanse" {
		t.Errorf("expected series 'The Expanse', got '%s'", file.Tags.Series)
	}
}

func TestParseAudiobookTags_MultipleFields(t *testing.T) {
	// Create ilst with multiple custom atoms
	var ilstData []byte
	ilstData = append(ilstData, createCustomAtom("com.apple.iTunes", "Narrator", "Andy Serkis")...)
	ilstData = append(ilstData, createCustomAtom("com.apple.iTunes", "Publisher", "Hachette Audio")...)
	ilstData = append(ilstData, createCustomAtom("com.apple.iTunes", "Series", "Lord of the Rings")...)
	ilstData = append(ilstData, createCustomAtom("com.apple.iTunes", "Series Part", "1")...)

	ilst := createMockAtom("ilst", ilstData)

	sr := audiobinary.NewSafeReader(bytes.NewReader(ilst), int64(len(ilst)), "test.m4b")
	ilstAtom, _ := readAtomHeader(sr, 0, int64(len(ilst)))

	file := &types.File{}
	err := parseAudiobookTags(context.Background(), sr, ilstAtom, file)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if file.Tags.Narrator != "Andy Serkis" {
		t.Errorf("expected narrator 'Andy Serkis', got '%s'", file.Tags.Narrator)
	}
	if file.Tags.Publisher != "Hachette Audio" {
		t.Errorf("expected publisher 'Hachette Audio', got '%s'", file.Tags.Publisher)
	}
	if file.Tags.Series != "Lord of the Rings" {
		t.Errorf("expected series 'Lord of the Rings', got '%s'", file.Tags.Series)
	}
	if file.Tags.SeriesPart != "1" {
		t.Errorf("expected series part '1', got '%s'", file.Tags.SeriesPart)
	}
}

func TestParseAudiobookTags_NoCustomAtoms(t *testing.T) {
	// ilst with only standard atoms, no custom atoms
	titleItem := createMetadataItem([]byte{0xA9, 'n', 'a', 'm'}, "Test Book")
	ilst := createMockAtom("ilst", titleItem)

	sr := audiobinary.NewSafeReader(bytes.NewReader(ilst), int64(len(ilst)), "test.m4b")
	ilstAtom, _ := readAtomHeader(sr, 0, int64(len(ilst)))

	file := &types.File{}
	err := parseAudiobookTags(context.Background(), sr, ilstAtom, file)

	// Should not error, just no audiobook tags extracted
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	if file.Tags.Narrator != "" || file.Tags.Series != "" || file.Tags.Publisher != "" {
		t.Error("expected no audiobook tags to be set")
	}
}

func TestApplyExplicitSeriesPart(t *testing.T) {
	tests := []struct {
		name       string
		customTags map[string]string
		file       *types.File
		expected   string
	}{
		{
			name:       "Series Part custom atom",
			customTags: map[string]string{"Series Part": "2"},
			file:       &types.File{},
			expected:   "2",
		},
		{
			name:       "Series Position custom atom",
			customTags: map[string]string{"Series Position": "3"},
			file:       &types.File{},
			expected:   "3",
		},
		{
			name:       "Part custom atom",
			customTags: map[string]string{"Part": "4"},
			file:       &types.File{},
			expected:   "4",
		},
		{
			name:       "Volume custom atom",
			customTags: map[string]string{"Volume": "5"},
			file:       &types.File{},
			expected:   "5",
		},
		{
			name:       "Priority: Series Part wins",
			customTags: map[string]string{"Series Part": "2", "Part": "99"},
			file:       &types.File{Tags: types.Tags{TrackNumber: 3, TrackTotal: 4}},
			expected:   "2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			applyExplicitSeriesPart(tt.file, tt.customTags)
			result := tt.file.Tags.SeriesPart
			if result != tt.expected {
				t.Errorf("expected '%s', got '%s'", tt.expected, result)
			}
		})
	}
}

func TestParseAudiobookTags_SeriesPartCaseInsensitive(t *testing.T) {
	ilst := createMockAtom("ilst", append(
		createCustomAtom("com.apple.iTunes", "Series", "Dune"),
		createCustomAtom("com.apple.iTunes", "SERIES PART", "3")...))
	sr := audiobinary.NewSafeReader(bytes.NewReader(ilst), int64(len(ilst)), "")
	ilstAtom, _ := readAtomHeader(sr, 0, int64(len(ilst)))

	file := &types.File{}
	if err := parseAudiobookTags(context.Background(), sr, ilstAtom, file); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if file.Tags.SeriesPart != "3" {
		t.Errorf("SeriesPart = %q, want 3", file.Tags.SeriesPart)
	}
}

// TestParseAudiobookTags_NoInference verifies Narrator, Series and SeriesPart
// are never derived from Composer, Grouping, title, album or the path.
func TestParseAudiobookTags_NoInference(t *testing.T) {
	ilst := createMockAtom("ilst", createMetadataItem([]byte{0xA9, 'w', 'r', 't'}, "Stephen Fry"))
	sr := audiobinary.NewSafeReader(bytes.NewReader(ilst), int64(len(ilst)), "/books/Foo/Book 03/x.m4b")
	ilstAtom, _ := readAtomHeader(sr, 0, int64(len(ilst)))

	file := &types.File{Tags: types.Tags{
		Composers: []string{"Stephen Fry"},
		Grouping:  "Foo, Book 3",
		Title:     "Foo Book 4",
		Album:     "Foo Book 5",
	}}
	if err := parseAudiobookTags(context.Background(), sr, ilstAtom, file); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if file.Tags.Narrator != "" || file.Tags.Series != "" || file.Tags.SeriesPart != "" {
		t.Errorf("narrator/series/part = %q/%q/%q, want all empty", file.Tags.Narrator, file.Tags.Series, file.Tags.SeriesPart)
	}
	if file.Tags.Grouping != "Foo, Book 3" {
		t.Errorf("Grouping = %q, want preserved", file.Tags.Grouping)
	}
}
