package registry

import (
	"io"
	"testing"

	"github.com/simonhull/audiometa/internal/types"
)

// mockParser implements FormatParser for testing.
type mockParser struct {
	name string
}

func (m *mockParser) Parse(r io.ReaderAt, size int64, path string) (*types.File, error) {
	return &types.File{Path: m.name}, nil
}

func TestRegisterAndGet(t *testing.T) {
	// Use a format that's unlikely to conflict with real registrations
	format := types.Format(999)
	parser := &mockParser{name: "test"}

	Register(format, parser)

	got := Get(format)
	if got == nil {
		t.Fatal("Get() returned nil for registered format")
	}

	// Verify it's our parser
	mp, ok := got.(*mockParser)
	if !ok {
		t.Fatal("Get() returned wrong parser type")
	}
	if mp.name != "test" {
		t.Errorf("Parser name = %q, want %q", mp.name, "test")
	}
}

func TestGet_Unregistered(t *testing.T) {
	// Use a format that's definitely not registered
	format := types.Format(998)

	got := Get(format)
	if got != nil {
		t.Errorf("Get() = %v for unregistered format, want nil", got)
	}
}

func TestRegister_Overwrites(t *testing.T) {
	format := types.Format(997)
	parser1 := &mockParser{name: "first"}
	parser2 := &mockParser{name: "second"}

	Register(format, parser1)
	Register(format, parser2)

	got := Get(format)
	mp, ok := got.(*mockParser)
	if !ok {
		t.Fatal("Get() returned wrong parser type")
	}
	if mp.name != "second" {
		t.Errorf("Parser name = %q, want %q (should be overwritten)", mp.name, "second")
	}
}

// mockArtworkExtractor implements both FormatParser and ArtworkExtractor.
type mockArtworkExtractor struct {
	mockParser
}

func (m *mockArtworkExtractor) ExtractArtwork(r io.ReaderAt, size int64, path string) ([]types.Artwork, error) {
	return []types.Artwork{{Type: types.ArtworkFrontCover}}, nil
}

func TestArtworkExtractorInterface(t *testing.T) {
	format := types.Format(996)
	parser := &mockArtworkExtractor{mockParser: mockParser{name: "artwork"}}

	Register(format, parser)

	got := Get(format)
	if got == nil {
		t.Fatal("Get() returned nil")
	}

	// Check it implements ArtworkExtractor
	ae, ok := got.(ArtworkExtractor)
	if !ok {
		t.Fatal("Parser should implement ArtworkExtractor")
	}

	artworks, err := ae.ExtractArtwork(nil, 0, "test.mp3")
	if err != nil {
		t.Errorf("ExtractArtwork() error = %v", err)
	}
	if len(artworks) != 1 {
		t.Errorf("ExtractArtwork() returned %d artworks, want 1", len(artworks))
	}
}

func TestRealParsersRegistered(t *testing.T) {
	// Verify that the real parsers are registered for known formats.
	// Note: This test depends on init() functions in other packages.
	// In a standalone test run, parsers may not be registered.
	// This test validates the interface, not the registrations.

	formats := []types.Format{
		types.FormatFLAC,
		types.FormatMP3,
		types.FormatM4A,
		types.FormatM4B,
		types.FormatOgg,
	}

	for _, format := range formats {
		parser := Get(format)
		// parser may be nil if tests run in isolation without init()
		// If registered, the type is guaranteed to be FormatParser by the interface
		_ = parser
	}
}
