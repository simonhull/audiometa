package m4a

import (
	"bytes"
	"context"
	"encoding/binary"
	"slices"
	"strings"
	"testing"

	audiobinary "github.com/simonhull/audiometa/internal/binary"
	"github.com/simonhull/audiometa/internal/types"
)

// createCustomAtom creates a custom (----) atom with mean, name, and data.
func createCustomAtom(fieldName, value string) []byte {
	const namespace string = "com.apple.iTunes"
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

// parseAudiobookAndDerive records the freeform atoms of ilstAtom and derives the
// typed fields from the raw tags, as Parse does.
func parseAudiobookAndDerive(ctx context.Context, sr *audiobinary.SafeReader, ilstAtom *Atom, file *types.File) error {
	err := parseAudiobookTags(ctx, sr, ilstAtom, file)
	types.DeriveFields(&file.Tags, types.FormatM4B)
	return err
}

func TestParseAudiobookTags_Narrator(t *testing.T) {
	// Create ilst with custom narrator atom
	narratorAtom := createCustomAtom("Narrator", "Wil Wheaton")
	ilst := createMockAtom("ilst", narratorAtom)

	sr := audiobinary.NewSafeReader(bytes.NewReader(ilst), int64(len(ilst)), "test.m4b")
	ilstAtom, _ := readAtomHeader(sr, 0, int64(len(ilst)))

	file := &types.File{}
	err := parseAudiobookAndDerive(context.Background(), sr, ilstAtom, file)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if file.Tags.Narrator != "Wil Wheaton" {
		t.Errorf("expected narrator 'Wil Wheaton', got '%s'", file.Tags.Narrator)
	}
}

// createTypedCustomAtom creates a ---- atom whose data atom has the given type indicator.
func createTypedCustomAtom(fieldName string, typeIndicator uint32, payload []byte) []byte {
	meanBuf := binary.BigEndian.AppendUint32(nil, 0)
	meanBuf = append(meanBuf, "com.apple.iTunes"...)
	nameBuf := binary.BigEndian.AppendUint32(nil, 0)
	nameBuf = append(nameBuf, fieldName...)
	dataBuf := binary.BigEndian.AppendUint32(nil, typeIndicator)
	dataBuf = binary.BigEndian.AppendUint32(dataBuf, 0)
	dataBuf = append(dataBuf, payload...)

	body := bytes.Join([][]byte{
		createAtomWithType([]byte("mean"), meanBuf),
		createAtomWithType([]byte("name"), nameBuf),
		createAtomWithType([]byte("data"), dataBuf),
	}, nil)
	return createAtomWithType([]byte("----"), body)
}

func TestParseAudiobookTags_FreeformByDataType(t *testing.T) {
	tests := []struct {
		name     string
		field    string
		typ      uint32
		payload  []byte
		rawKey   string
		wantRaw  []string
		narrator string
	}{
		{name: "integer", field: "Rating", typ: 21, payload: []byte{0x2A}, rawKey: "RATING", wantRaw: []string{"42"}},
		{name: "jpeg skipped", field: "Cover", typ: 13, payload: []byte{0xFF, 0xD8, 0xFF, 0xE0}, rawKey: "COVER"},
		{name: "text", field: "Narrator", typ: 1, payload: []byte("Wil Wheaton"), rawKey: "NARRATOR", wantRaw: []string{"Wil Wheaton"}, narrator: "Wil Wheaton"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ilst := createMockAtom("ilst", createTypedCustomAtom(tt.field, tt.typ, tt.payload))
			sr := audiobinary.NewSafeReader(bytes.NewReader(ilst), int64(len(ilst)), "test.m4b")
			ilstAtom, _ := readAtomHeader(sr, 0, int64(len(ilst)))

			file := &types.File{}
			if err := parseAudiobookAndDerive(context.Background(), sr, ilstAtom, file); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := file.Tags.Get(tt.rawKey); !slices.Equal(got, tt.wantRaw) {
				t.Errorf("Get(%q) = %q, want %q", tt.rawKey, got, tt.wantRaw)
			}
			if file.Tags.Narrator != tt.narrator {
				t.Errorf("Narrator = %q, want %q", file.Tags.Narrator, tt.narrator)
			}
		})
	}
}

func TestParseAudiobookTags_Series(t *testing.T) {
	seriesAtom := createCustomAtom("Series", "The Expanse")
	ilst := createMockAtom("ilst", seriesAtom)

	sr := audiobinary.NewSafeReader(bytes.NewReader(ilst), int64(len(ilst)), "test.m4b")
	ilstAtom, _ := readAtomHeader(sr, 0, int64(len(ilst)))

	file := &types.File{}
	err := parseAudiobookAndDerive(context.Background(), sr, ilstAtom, file)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if file.Tags.Series != "The Expanse" {
		t.Errorf("expected series 'The Expanse', got '%s'", file.Tags.Series)
	}
}

func TestParseAudiobookTags_MultipleFields(t *testing.T) {
	// Create ilst with multiple custom atoms
	ilstData := slices.Concat(
		createCustomAtom("Narrator", "Andy Serkis"),
		createCustomAtom("Publisher", "Hachette Audio"),
		createCustomAtom("Series", "Lord of the Rings"),
		createCustomAtom("Series Part", "1"),
	)

	ilst := createMockAtom("ilst", ilstData)

	sr := audiobinary.NewSafeReader(bytes.NewReader(ilst), int64(len(ilst)), "test.m4b")
	ilstAtom, _ := readAtomHeader(sr, 0, int64(len(ilst)))

	file := &types.File{}
	err := parseAudiobookAndDerive(context.Background(), sr, ilstAtom, file)
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
	err := parseAudiobookAndDerive(context.Background(), sr, ilstAtom, file)
	// Should not error, just no audiobook tags extracted
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}

	if file.Tags.Narrator != "" || file.Tags.Series != "" || file.Tags.Publisher != "" {
		t.Error("expected no audiobook tags to be set")
	}
}

func TestParseAudiobookTags_SeriesPartCaseInsensitive(t *testing.T) {
	ilst := createMockAtom("ilst", append(
		createCustomAtom("Series", "Dune"),
		createCustomAtom("SERIES PART", "3")...))
	sr := audiobinary.NewSafeReader(bytes.NewReader(ilst), int64(len(ilst)), "")
	ilstAtom, _ := readAtomHeader(sr, 0, int64(len(ilst)))

	file := &types.File{}
	if err := parseAudiobookAndDerive(context.Background(), sr, ilstAtom, file); err != nil {
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
	if err := parseAudiobookAndDerive(context.Background(), sr, ilstAtom, file); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if file.Tags.Narrator != "" || file.Tags.Series != "" || file.Tags.SeriesPart != "" {
		t.Errorf("narrator/series/part = %q/%q/%q, want all empty", file.Tags.Narrator, file.Tags.Series, file.Tags.SeriesPart)
	}
	if file.Tags.Grouping != "Foo, Book 3" {
		t.Errorf("Grouping = %q, want preserved", file.Tags.Grouping)
	}
}

// TestParseAudiobookTags_NonSeriesFreeformStaysRaw verifies the generic Part
// and Volume atoms do not fill SeriesPart but remain in the raw tags.
func TestParseAudiobookTags_NonSeriesFreeformStaysRaw(t *testing.T) {
	for _, name := range []string{"Part", "Volume"} {
		t.Run(name, func(t *testing.T) {
			ilst := createMockAtom("ilst", createCustomAtom(name, "5"))
			sr := audiobinary.NewSafeReader(bytes.NewReader(ilst), int64(len(ilst)), "")
			ilstAtom, _ := readAtomHeader(sr, 0, int64(len(ilst)))

			file := &types.File{}
			if err := parseAudiobookAndDerive(context.Background(), sr, ilstAtom, file); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if file.Tags.SeriesPart != "" {
				t.Errorf("SeriesPart = %q, want empty", file.Tags.SeriesPart)
			}
			if got := file.Tags.Get(strings.ToUpper(name)); len(got) != 1 || got[0] != "5" {
				t.Errorf("raw %s = %q, want [5]", name, got)
			}
		})
	}
}
