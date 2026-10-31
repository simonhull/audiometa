package m4a

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"slices"
	"testing"

	audiobinary "github.com/simonhull/audiometa/internal/binary"
	"github.com/simonhull/audiometa/internal/types"
)

// createDataAtom creates a data atom with string content.
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

// For iTunes tags, itemType should be the 4-byte tag code as bytes (e.g., []byte{0xA9, 'n', 'a', 'm'} for ©nam).
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
	atom, err := readAtomHeader(sr, 0, int64(len(data)))
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
	atom, _ := readAtomHeader(sr, 0, int64(len(data)))

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
	ilstAtom, _ := readAtomHeader(sr, 0, int64(len(ilst)))

	file := &types.File{}
	err := extractIlstMetadata(context.Background(), sr, ilstAtom, file)
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
		{"\xA9nam", "Title", func(f *types.File) string { return f.Tags.Title }, "Title"},    // ©nam
		{"\xA9ART", "Artist", func(f *types.File) string { return f.Tags.Artist }, "Artist"}, // ©ART
		{"\xA9alb", "Album", func(f *types.File) string { return f.Tags.Album }, "Album"},    // ©alb
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

// createTrackDataAtom creates a data atom with track number content.
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
			trknAtom, err := readAtomHeader(sr, 0, int64(len(trkn)))
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
	trknAtom, _ := readAtomHeader(sr, 0, int64(len(trkn)))

	_, err := parseTrackNumber(sr, trknAtom)
	if err == nil {
		t.Error("expected error for missing data atom, got nil")
	}
}

// createTypedMetadataItem builds an ilst item whose data atom has the given type indicator.
func createTypedMetadataItem(itemType []byte, typeIndicator uint32, payload []byte) []byte {
	data := &bytes.Buffer{}
	binary.Write(data, binary.BigEndian, typeIndicator) // version 0 + type indicator
	binary.Write(data, binary.BigEndian, uint32(0))     // reserved
	data.Write(payload)
	return createAtomWithType(itemType, createMockAtom("data", data.Bytes()))
}

func runIlst(t *testing.T, items ...[]byte) *types.File {
	t.Helper()
	ilst := createMockAtom("ilst", bytes.Join(items, nil))
	sr := audiobinary.NewSafeReader(bytes.NewReader(ilst), int64(len(ilst)), "test.m4b")
	ilstAtom, err := readAtomHeader(sr, 0, int64(len(ilst)))
	if err != nil {
		t.Fatalf("readAtomHeader: %v", err)
	}
	file := &types.File{}
	if err := extractIlstMetadata(context.Background(), sr, ilstAtom, file); err != nil {
		t.Fatalf("extractIlstMetadata: %v", err)
	}
	return file
}

func TestParseMetadataTag_Integer(t *testing.T) {
	tests := []struct {
		name    string
		typ     uint32
		payload []byte
		want    string
	}{
		{"signed 2 bytes", 21, []byte{0x00, 0x05}, "5"},
		{"signed -1", 21, []byte{0xFF, 0xFF}, "-1"},
		{"signed 3 bytes negative", 21, []byte{0xFF, 0xFF, 0xFE}, "-2"},
		{"unsigned 2 bytes", 22, []byte{0xFF, 0xFF}, "65535"},
		{"unsigned 4 bytes", 22, []byte{0, 0, 0, 7}, "7"},
		{"unsupported width", 21, []byte{1, 2, 3, 4, 5}, ""},
		{"image type", 13, make([]byte, 100), ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			item := createTypedMetadataItem([]byte{0xA9, 'm', 'v', 'i'}, tt.typ, tt.payload)
			sr := audiobinary.NewSafeReader(bytes.NewReader(item), int64(len(item)), "test.m4b")
			atom, _ := readAtomHeader(sr, 0, int64(len(item)))
			got, err := parseMetadataTag(sr, atom)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIlstSeriesPartInteger(t *testing.T) {
	file := runIlst(t,
		createMetadataItem([]byte{0xA9, 'm', 'v', 'n'}, "The Expanse"),
		createTypedMetadataItem([]byte{0xA9, 'm', 'v', 'i'}, 21, []byte{0x00, 0x05}),
	)
	if file.Tags.Series != "The Expanse" || file.Tags.SeriesPart != "5" {
		t.Errorf("Series=%q SeriesPart=%q", file.Tags.Series, file.Tags.SeriesPart)
	}
}

func TestIlstSeriesPartZeroIgnored(t *testing.T) {
	file := runIlst(t, createTypedMetadataItem([]byte{0xA9, 'm', 'v', 'i'}, 21, []byte{0x00, 0x00}))
	if file.Tags.SeriesPart != "" {
		t.Errorf("SeriesPart = %q, want empty", file.Tags.SeriesPart)
	}
}

func TestIlstDate(t *testing.T) {
	tests := []struct {
		value    string
		wantYear int
		wantDate string
	}{
		{"2015-03-01T07:00:00Z", 2015, "2015-03-01T07:00:00Z"},
		{"1999", 1999, "1999"},
		{"unknown", 0, "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			file := runIlst(t, createMetadataItem([]byte{0xA9, 'd', 'a', 'y'}, tt.value))
			if file.Tags.Year != tt.wantYear || file.Tags.Date != tt.wantDate {
				t.Errorf("Year=%d Date=%q, want %d %q", file.Tags.Year, file.Tags.Date, tt.wantYear, tt.wantDate)
			}
		})
	}
}

func TestIlstNewFields(t *testing.T) {
	file := runIlst(t,
		createMetadataItem([]byte("aART"), "Album Artist"),
		createMetadataItem([]byte("cprt"), "(c) Someone"),
		createMetadataItem([]byte{0xA9, 'l', 'y', 'r'}, "la la"),
		createTypedMetadataItem([]byte("disk"), 0, []byte{0, 0, 0, 2, 0, 3}),
	)
	if file.Tags.AlbumArtist != "Album Artist" || file.Tags.Copyright != "(c) Someone" || file.Tags.Lyrics != "la la" {
		t.Errorf("unexpected tags: %+v", file.Tags)
	}
	if file.Tags.DiscNumber != 2 || file.Tags.DiscTotal != 3 {
		t.Errorf("disc = %d/%d, want 2/3", file.Tags.DiscNumber, file.Tags.DiscTotal)
	}
}

func TestIlstDescriptionPriority(t *testing.T) {
	item := func(typ, v string) []byte { return createMetadataItem([]byte(typ), v) }
	des := []byte{0xA9, 'd', 'e', 's'}
	tests := []struct {
		name  string
		items [][]byte
		want  string
	}{
		{"ldes first", [][]byte{item("ldes", "long"), item("desc", "short"), createMetadataItem(des, "des")}, "long"},
		{"des before desc", [][]byte{createMetadataItem(des, "des"), item("desc", "short")}, "des"},
		{"desc before ldes", [][]byte{item("desc", "short"), item("ldes", "long")}, "long"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := runIlst(t, tt.items...).Tags.Description; got != tt.want {
				t.Errorf("Description = %q, want %q", got, tt.want)
			}
		})
	}
}

type maxReadReaderAt struct {
	r   io.ReaderAt
	max int
}

func (m *maxReadReaderAt) ReadAt(p []byte, off int64) (int, error) {
	m.max = max(m.max, len(p))
	return m.r.ReadAt(p, off)
}

func TestIlstSkipsCoverData(t *testing.T) {
	ilst := createMockAtom("ilst", bytes.Join([][]byte{
		createMetadataItem([]byte{0xA9, 'n', 'a', 'm'}, "Title"),
		createTypedMetadataItem([]byte("covr"), 13, make([]byte, 65536)),
		createMetadataItem([]byte("aART"), "AA"),
	}, nil))
	rr := &maxReadReaderAt{r: bytes.NewReader(ilst)}
	sr := audiobinary.NewSafeReader(rr, int64(len(ilst)), "test.m4b")
	ilstAtom, err := readAtomHeader(sr, 0, int64(len(ilst)))
	if err != nil {
		t.Fatal(err)
	}
	rr.max = 0
	file := &types.File{}
	if err := extractIlstMetadata(context.Background(), sr, ilstAtom, file); err != nil {
		t.Fatal(err)
	}
	if file.Tags.Title != "Title" || file.Tags.AlbumArtist != "AA" {
		t.Errorf("walk did not continue past covr: %+v", file.Tags)
	}
	if rr.max >= 1024 {
		t.Errorf("largest read was %d bytes; covr payload should not be read", rr.max)
	}
}

func TestExtractIlstMetadata_RepeatedAtomsFirstWins(t *testing.T) {
	var ilstData []byte
	for _, it := range []struct{ tag, val string }{
		{"\xA9ART", "Artist A"}, {"\xA9ART", "Artist B"},
		{"\xA9nam", "Title 1"}, {"\xA9nam", "Title 2"},
		{"\xA9alb", "Album 1"}, {"\xA9alb", "Album 2"},
		{"desc", "short"}, {"ldes", "long 1"}, {"ldes", "long 2"},
	} {
		ilstData = append(ilstData, createMetadataItem([]byte(it.tag), it.val)...)
	}
	ilst := createMockAtom("ilst", ilstData)
	sr := audiobinary.NewSafeReader(bytes.NewReader(ilst), int64(len(ilst)), "test.m4b")
	ilstAtom, _ := readAtomHeader(sr, 0, int64(len(ilst)))

	file := &types.File{}
	if err := extractIlstMetadata(context.Background(), sr, ilstAtom, file); err != nil {
		t.Fatal(err)
	}
	tg := file.Tags
	if tg.Artist != "Artist A" || !slices.Equal(tg.Artists, []string{"Artist A", "Artist B"}) {
		t.Errorf("Artist = %q, Artists = %v", tg.Artist, tg.Artists)
	}
	if tg.Title != "Title 1" || tg.Album != "Album 1" || tg.Description != "long 1" {
		t.Errorf("first value did not win: title=%q album=%q desc=%q", tg.Title, tg.Album, tg.Description)
	}
}
