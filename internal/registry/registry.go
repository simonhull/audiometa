package registry

import (
	"io"

	"github.com/simonhull/audiometa/internal/types"
)

// FormatParser is the interface all format parsers implement.
type FormatParser interface {
	// Parse extracts metadata from an audio file.
	// Returns a partially initialized File (Path, Format, Size set by caller).
	Parse(r io.ReaderAt, size int64, path string) (*types.File, error)
}

// ArtworkExtractor is an optional interface for parsers that support artwork extraction.
type ArtworkExtractor interface {
	// ExtractArtwork extracts embedded artwork from the file.
	ExtractArtwork(r io.ReaderAt, size int64, path string) ([]types.Artwork, error)
}

// parsers maps formats to their parsers.
var parsers = make(map[types.Format]FormatParser)

// Register registers a parser for a format.
// This is called by format packages during initialization (init functions).
func Register(format types.Format, parser FormatParser) {
	parsers[format] = parser
}

// Get returns the parser for a given format.
// Returns nil if no parser is registered for the format.
func Get(format types.Format) FormatParser {
	return parsers[format]
}
