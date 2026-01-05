// Package registry manages format-specific parsers for audio file types.
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

// FormatWriter is the interface format writers implement.
type FormatWriter interface {
	// Write writes the file's metadata to w.
	// original provides read access to the source file for copying audio data.
	Write(w io.Writer, file *types.File, original io.ReaderAt, originalSize int64) error
}

// parsers maps formats to their parsers.
var parsers = make(map[types.Format]FormatParser)

// writers maps formats to their writers.
var writers = make(map[types.Format]FormatWriter)

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

// RegisterWriter registers a writer for a format.
// This is called by format packages during initialization (init functions).
func RegisterWriter(format types.Format, writer FormatWriter) {
	writers[format] = writer
}

// GetWriter returns the writer for a given format.
// Returns nil if no writer is registered for the format.
func GetWriter(format types.Format) FormatWriter {
	return writers[format]
}
