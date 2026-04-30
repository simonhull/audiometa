// Package registry manages format-specific parsers for audio file types.
package registry

import (
	"context"
	"io"
	"sync"

	"github.com/simonhull/audiometa/internal/types"
)

// FormatParser is the interface all format parsers implement.
type FormatParser interface {
	// Parse extracts metadata from an audio file.
	// Returns a partially initialized File (Path, Format, Size set by caller).
	// The context is checked at major parse boundaries so callers can cancel
	// long-running parses (large M4B chapter scans, dense MP3 frame walks).
	Parse(ctx context.Context, r io.ReaderAt, size int64, path string) (*types.File, error)
}

// ArtworkExtractor is an optional interface for parsers that support artwork extraction.
type ArtworkExtractor interface {
	// ExtractArtwork extracts embedded artwork from the file.
	// The context is checked at entry; long extractions will yield to cancellation.
	ExtractArtwork(ctx context.Context, r io.ReaderAt, size int64, path string) ([]types.Artwork, error)
}

var (
	mu      sync.RWMutex
	parsers = make(map[types.Format]FormatParser)
)

// Register registers a parser for a format. Safe for concurrent use, though
// in practice it is only called from init() in format packages.
func Register(format types.Format, parser FormatParser) {
	mu.Lock()
	defer mu.Unlock()
	parsers[format] = parser
}

// Get returns the parser for a given format.
// Returns nil if no parser is registered for the format.
func Get(format types.Format) FormatParser {
	mu.RLock()
	defer mu.RUnlock()
	return parsers[format]
}
