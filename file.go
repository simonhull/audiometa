package audiometa

import (
	"context"
	"fmt"
	"io"
	"os"
	"runtime"

	"golang.org/x/sync/errgroup"

	"github.com/simonhull/audiometa/internal/registry"
	"github.com/simonhull/audiometa/internal/types"

	// Register built-in format parsers
	_ "github.com/simonhull/audiometa/internal/flac"
	_ "github.com/simonhull/audiometa/internal/m4a"
	_ "github.com/simonhull/audiometa/internal/mp3"
	_ "github.com/simonhull/audiometa/internal/ogg"
)

// File represents an opened audio file with parsed metadata.
// Embeds types.File and adds methods.
type File struct {
	types.File
}

// Open opens an audio file and reads its metadata.
//
// Supported formats: FLAC, MP3, M4A, M4B
//
// Open performs lazy loading - audio content is not read into memory,
// only metadata is parsed. Use ExtractArtwork() to retrieve embedded images.
//
// If the file is corrupted or has invalid tags, Open may return a partial
// File with warnings instead of an error. Check File.Warnings for details.
//
// Options can be provided to customize parsing behavior:
//
//	file, err := audiometa.Open("song.flac",
//	    audiometa.WithStrictParsing(),
//	    audiometa.WithArtworkPreload(),
//	)
//
// Example:
//
//	file, err := audiometa.Open("song.flac")
//	if err != nil {
//		return err
//	}
//	defer file.Close()
//	fmt.Printf("%s - %s\n", file.Tags.Artist, file.Tags.Title)
func Open(path string, opts ...Option) (*File, error) {
	// Apply options
	options := defaultOptions()
	for _, opt := range opts {
		opt(options)
	}

	// Open file
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open file: %w", err)
	}

	// Get file size
	stat, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("stat file: %w", err)
	}
	size := stat.Size()

	// Parse with the reader
	typesFile, err := openReader(f, size, path, options)
	if err != nil {
		f.Close()
		return nil, err
	}

	// Wrap in File struct
	file := &File{File: *typesFile}

	// Keep the file handle for lazy operations (artwork, etc.)
	file.Reader_ = f

	// Check strict parsing mode
	if options.strictParsing && len(file.Warnings) > 0 {
		f.Close()
		return nil, fmt.Errorf("strict parsing failed: %s", file.Warnings[0].Message)
	}

	// Preload artwork if requested
	if options.preloadArtwork {
		if _, err := file.ExtractArtwork(); err != nil {
			// Don't fail completely, just add warning
			file.Warnings = append(file.Warnings, Warning{
				Stage:   "artwork",
				Message: fmt.Sprintf("preload artwork failed: %v", err),
			})
		}
	}

	return file, nil
}

// openReader opens from an io.ReaderAt (internal, for testing)
func openReader(r io.ReaderAt, size int64, path string, options *openOptions) (*types.File, error) {
	// Detect format
	format, err := types.DetectFormat(r, size, path)
	if err != nil {
		return nil, err
	}

	// Find parser for this format
	parser := findParser(format)
	if parser == nil {
		return nil, &types.UnsupportedFormatError{
			Path:   path,
			Reason: fmt.Sprintf("no parser available for format %s", format),
		}
	}

	// Create SafeReader for bounds-checked access
	// (SafeReader will be created by parser from internal/binary package)

	// Parse metadata
	file, err := parser.Parse(r, size, path)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", format, err)
	}

	// Set file-level fields
	file.Path = path
	file.Format = format
	file.Size = size
	file.Parser_ = parser

	// Apply option: ignore warnings
	if options.ignoreWarnings {
		file.Warnings = nil
	}

	return file, nil
}

// Close releases resources held by the file.
//
// After Close is called, the File should not be used.
func (f *File) Close() error {
	if closer, ok := f.Reader_.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

// ExtractArtwork extracts embedded artwork from the file.
//
// Artwork is lazily loaded - it is not parsed during Open(). The first
// call to ExtractArtwork() reads and caches the artwork. Subsequent
// calls return the cached data.
//
// Returns an empty slice if the file contains no artwork.
//
// Example:
//
//	artwork, err := file.ExtractArtwork()
//	if err != nil {
//		return err
//	}
//	if len(artwork) > 0 {
//		cover := artwork[0] // First image (usually front cover)
//		os.WriteFile("cover.jpg", cover.Data, 0644)
//	}
func (f *File) ExtractArtwork() ([]Artwork, error) {
	// Return cached artwork if already loaded
	if f.Artwork_ != nil {
		return f.Artwork_, nil
	}

	// Check if parser supports artwork extraction
	extractor, ok := f.Parser_.(ArtworkExtractor)
	if !ok {
		// Format doesn't support artwork
		return nil, nil
	}

	// Extract artwork
	artwork, err := extractor.ExtractArtwork(f.Reader_, f.Size, f.Path)
	if err != nil {
		return nil, fmt.Errorf("extract artwork: %w", err)
	}

	// Cache for future calls
	f.Artwork_ = artwork

	return artwork, nil
}

// RawTags returns format-specific raw tag data.
//
// This provides access to tags that may not be mapped to the standard
// Tags fields. Useful for preserving unknown or custom tags.
//
// The returned map should not be modified.
func (f *File) RawTags() map[string][]RawTag {
	return f.RawTags_
}

// OpenContext opens a file with context support for cancellation.
//
// This is a thin wrapper around Open() that checks context before starting.
// Future enhancements (streaming, network files) will use context throughout
// the parsing process.
//
// Options can be provided just like with Open():
//
//	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
//	defer cancel()
//
//	file, err := audiometa.OpenContext(ctx, "song.flac",
//	    audiometa.WithStrictParsing(),
//	)
//	if err != nil {
//		return err
//	}
//	defer file.Close()
func OpenContext(ctx context.Context, path string, opts ...Option) (*File, error) {
	// Check context before starting
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// TODO: In future, pass context through parsing for incremental cancellation
	return Open(path, opts...)
}

// OpenMany opens multiple audio files concurrently.
//
// Files are parsed in parallel using up to runtime.NumCPU() goroutines.
// Results are returned in the same order as the input paths.
//
// If any file fails to open, all successfully opened files are closed
// and an error is returned.
//
// Example:
//
//	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
//	defer cancel()
//
//	files, err := audiometa.OpenMany(ctx, paths...)
//	if err != nil {
//		log.Fatal(err)
//	}
//	defer func() {
//		for _, f := range files {
//			f.Close()
//		}
//	}()
//
//	for _, f := range files {
//		fmt.Printf("%s: %s - %s\n", f.Format, f.Tags.Artist, f.Tags.Title)
//	}
func OpenMany(ctx context.Context, paths ...string) ([]*File, error) {
	if len(paths) == 0 {
		return nil, nil
	}

	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(runtime.NumCPU()) // Limit concurrent operations

	results := make([]*File, len(paths))

	for i, path := range paths {
		i, path := i, path // Capture loop variables
		g.Go(func() error {
			// Check for cancellation
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			// Open file
			file, err := Open(path)
			if err != nil {
				return fmt.Errorf("%s: %w", path, err)
			}

			results[i] = file
			return nil
		})
	}

	// Wait for all to complete
	if err := g.Wait(); err != nil {
		// Close any successfully opened files
		for _, file := range results {
			if file != nil {
				file.Close()
			}
		}
		return nil, err
	}

	return results, nil
}

// FormatParser is an alias to registry.FormatParser for backwards compatibility.
// Re-exporting from internal/registry to maintain public API.
type FormatParser = registry.FormatParser

// ArtworkExtractor is an alias to registry.ArtworkExtractor for backwards compatibility.
// Re-exporting from internal/registry to maintain public API.
type ArtworkExtractor = registry.ArtworkExtractor

// findParser returns the parser for a given format.
//
// Returns nil if no parser is registered for the format.
func findParser(format types.Format) FormatParser {
	return registry.Get(format)
}

// RegisterParser registers a parser for a format.
// This is called by format packages during initialization (init functions).
//
// This function is public to allow internal format packages to register themselves,
// but it's not intended for external use. Do not call this function.
func RegisterParser(format types.Format, parser FormatParser) {
	registry.Register(format, parser)
}
