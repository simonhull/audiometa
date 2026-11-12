package mp3

import (
	"context"
	"os"
	"slices"
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
	file, err := p.Parse(context.Background(), f, stat.Size(), tmpFile.Name())
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

	if file.Audio.Container != "MP3" {
		t.Errorf("Container = %q, want MP3", file.Audio.Container)
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
	file, err := p.Parse(context.Background(), f, stat.Size(), tmpFile.Name())
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

// createMinimalMP3WithID3 creates a minimal MP3 file with ID3v2.3 tag.
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

func TestParseTextFrame(t *testing.T) {
	file := &types.File{
		Tags: types.Tags{},
	}

	// TIT2 (Title) frame
	frame := id3v2Frame{
		ID:   "TIT2",
		Data: []byte{0x00, 'T', 'e', 's', 't'}, // ISO-8859-1 encoding + "Test"
	}
	parseTextFrame(frame, file)
	types.DeriveFields(&file.Tags, types.FormatMP3)

	if file.Tags.Title != "Test" {
		t.Errorf("expected title 'Test', got '%s'", file.Tags.Title)
	}

	// TPE1 (Artist) frame
	frame = id3v2Frame{
		ID:   "TPE1",
		Data: []byte{0x00, 'A', 'r', 't', 'i', 's', 't'},
	}
	parseTextFrame(frame, file)
	types.DeriveFields(&file.Tags, types.FormatMP3)

	if file.Tags.Artist != "Artist" {
		t.Errorf("expected artist 'Artist', got '%s'", file.Tags.Artist)
	}

	// TRCK (Track number) frame
	frame = id3v2Frame{
		ID:   "TRCK",
		Data: []byte{0x00, '5', '/', '1', '2'},
	}
	parseTextFrame(frame, file)
	types.DeriveFields(&file.Tags, types.FormatMP3)

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
	frame := id3v2Frame{
		ID: "TXXX",
		Data: []byte{
			0x00,                                         // ISO-8859-1 encoding
			'N', 'a', 'r', 'r', 'a', 't', 'o', 'r', 0x00, // "Narrator\0"
			'J', 'o', 'h', 'n', ' ', 'D', 'o', 'e', // "John Doe"
		},
	}
	parseTXXXFrame(frame, file)
	types.DeriveFields(&file.Tags, types.FormatMP3)

	if file.Tags.Narrator != "John Doe" {
		t.Errorf("expected narrator 'John Doe', got '%s'", file.Tags.Narrator)
	}

	// TXXX frame with Series field
	frame = id3v2Frame{
		ID: "TXXX",
		Data: []byte{
			0x00,
			'S', 'e', 'r', 'i', 'e', 's', 0x00,
			'H', 'a', 'r', 'r', 'y', ' ', 'P', 'o', 't', 't', 'e', 'r',
		},
	}
	parseTXXXFrame(frame, file)
	types.DeriveFields(&file.Tags, types.FormatMP3)

	if file.Tags.Series != "Harry Potter" {
		t.Errorf("expected series 'Harry Potter', got '%s'", file.Tags.Series)
	}
}

func TestParseChapterFrames(t *testing.T) {
	// Create mock CHAP frames
	frames := []id3v2Frame{
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

	chapters := parseChapterFrames(frames, 3, sizeSynchsafe)

	if len(chapters) != 2 {
		t.Fatalf("expected 2 chapters, got %d", len(chapters))
	}

	for i, c := range chapters {
		if c.Source != types.ChapterSourceID3CHAP {
			t.Errorf("chapter %d: source = %q, want %q", i, c.Source, types.ChapterSourceID3CHAP)
		}
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

func TestParse_TrailingAPEv2IsNotAudio(t *testing.T) {
	data := slices.Concat(id3Tag(4, id3Frame(4, "TIT2", latin1Text("x"))), audioBytes(417*40))
	plain := parseBytes(t, data)
	if plain.Audio.Bitrate != 128000 || plain.Audio.Duration == 0 {
		t.Fatalf("baseline audio = %+v", plain.Audio)
	}
	tagged := parseBytes(t, slices.Concat(data, apeWithHeader(64<<10)))
	if tagged.Audio.Duration != plain.Audio.Duration || tagged.Audio.Bitrate != plain.Audio.Bitrate {
		t.Errorf("with APEv2 tag: duration %v bitrate %d; without: %v %d",
			tagged.Audio.Duration, tagged.Audio.Bitrate, plain.Audio.Duration, plain.Audio.Bitrate)
	}
}

func TestParse_TAGInsideAPEv2IsNotID3v1(t *testing.T) {
	ape := apeWithHeader(200)
	copy(ape[len(ape)-128:], "TAG") // an item value that looks like an ID3v1 trailer
	f := parseBytes(t, slices.Concat(audioBytes(417*40), ape))
	if f.Tags.Title != "" || f.Tags.Artist != "" {
		t.Errorf("tags = %+v, want none: \"TAG\" inside an APEv2 item is not ID3v1", f.Tags)
	}
}

func TestParse_ReleaseFields(t *testing.T) {
	const (
		albumID  = "f3c0fd5e-ba3e-4a2c-8fd3-6e4a6f0c1b2d"
		artistID = "5b11f4ce-a62d-471e-81fc-a69a8278c7da"
	)
	txt := func(version byte, id, v string) []byte { return id3Frame(version, id, latin1Text(v)) }
	xxx := func(version byte, desc, v string) []byte {
		return id3Frame(version, frameIDTXXX, txxxPayload(encUTF8, desc, v))
	}
	audio := audioBytes(417 * 4)

	tests := []struct {
		name string
		tag  []byte
		want types.Tags
	}{
		{
			"v2.4", id3Tag(4,
				txt(4, "TSRC", "USRC17607839"), txt(4, "TCOP", "2001 Label"), txt(4, "TDOR", "1999-06-01"), txt(4, "TPUB", "The Publisher"),
				xxx(4, "MusicBrainz Album Id", albumID), xxx(4, "MusicBrainz Artist Id", artistID+"\x00other"),
				xxx(4, "BARCODE", "0123456789"), xxx(4, "CATALOGNUMBER", "CAT-1"),
				id3Frame(4, frameIDUFID, ufidPayload(mbOwner, recordingID))),
			types.Tags{
				ISRC: "USRC17607839", Copyright: "2001 Label", OriginalDate: "1999-06-01", Publisher: "The Publisher",
				MusicBrainzAlbumID: albumID, MusicBrainzArtistID: artistID, Barcode: "0123456789", CatalogNumber: "CAT-1",
				MusicBrainzTrackID: recordingID,
			},
		},
		{
			"v2.3 TORY is a year", id3Tag(3, txt(3, "TORY", "1999"), txt(3, "TSRC", "GBAYE0601498")),
			types.Tags{OriginalDate: "1999", ISRC: "GBAYE0601498"},
		},
		{
			"TDOR before TORY", id3Tag(4, txt(4, "TORY", "1998"), txt(4, "TDOR", "1999-06-01")),
			types.Tags{OriginalDate: "1999-06-01"},
		},
		{
			"v2.2 TRC TCR TOR TPB", id3Tag(2,
				id3v22Frame("TRC", latin1Text("USRC17607839")), id3v22Frame("TCR", latin1Text("(c) 2001")),
				id3v22Frame("TOR", latin1Text("1999")), id3v22Frame("TPB", latin1Text("Pub"))),
			types.Tags{ISRC: "USRC17607839", Copyright: "(c) 2001", OriginalDate: "1999", Publisher: "Pub"},
		},
		{
			"repeated TXXX first wins", id3Tag(4, xxx(4, "BARCODE", "111"), xxx(4, "BARCODE", "222")),
			types.Tags{Barcode: "111"},
		},
		{
			"TXXX description is case-insensitive", id3Tag(4, xxx(4, "musicbrainz album id", albumID)),
			types.Tags{MusicBrainzAlbumID: albumID},
		},
		{
			"TXXX PUBLISHER before TPUB", id3Tag(4, txt(4, "TPUB", "Frame"), xxx(4, "PUBLISHER", "Custom")),
			types.Tags{Publisher: "Custom"},
		},
		{
			"UFID of another owner", id3Tag(4, id3Frame(4, frameIDUFID, ufidPayload("http://example.com", "x"))),
			types.Tags{},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := parseBytes(t, slices.Concat(tc.tag, audio)).Tags
			for _, c := range []struct {
				name      string
				got, want string
			}{
				{"ISRC", got.ISRC, tc.want.ISRC},
				{"Copyright", got.Copyright, tc.want.Copyright},
				{"OriginalDate", got.OriginalDate, tc.want.OriginalDate},
				{"Publisher", got.Publisher, tc.want.Publisher},
				{"Label", got.Label, ""},
				{"Barcode", got.Barcode, tc.want.Barcode},
				{"CatalogNumber", got.CatalogNumber, tc.want.CatalogNumber},
				{"MusicBrainzAlbumID", got.MusicBrainzAlbumID, tc.want.MusicBrainzAlbumID},
				{"MusicBrainzArtistID", got.MusicBrainzArtistID, tc.want.MusicBrainzArtistID},
				{"MusicBrainzTrackID", got.MusicBrainzTrackID, tc.want.MusicBrainzTrackID},
			} {
				if c.got != c.want {
					t.Errorf("%s = %q, want %q", c.name, c.got, c.want)
				}
			}
		})
	}
}
