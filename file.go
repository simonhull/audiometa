package audiometa

import (
	"context"
	"fmt"
	"io"
	"os"
	"runtime"

	"golang.org/x/sync/errgroup"
)

// File represents an opened audio file with parsed metadata.
//
// File provides access to format-agnostic metadata (Tags), technical
// audio properties (AudioInfo), and optional embedded artwork.
//
// File uses lazy loading - opening a file reads only metadata, not
// audio content or artwork. Call ExtractArtwork() to load images.
//
// Always call Close() when done to release file resources:
//
//	file, err := audiometa.Open("song.flac")
//	if err != nil {
//		return err
//	}
//	defer file.Close()
type File struct {
	// Path to the audio file
	Path string

	// Detected format (FLAC, MP3, M4A, M4B, etc.)
	Format Format

	// File size in bytes
	Size int64

	// Parsed metadata (format-agnostic)
	Tags Tags

	// Audio technical properties
	Audio AudioInfo

	// Chapters (for audiobooks, CD tracks, etc.)
	Chapters []Chapter

	// Warnings encountered during parsing (non-fatal issues)
	Warnings []Warning

	// Internal state (unexported)
	reader  io.ReaderAt // File handle or other reader
	parser  FormatParser // Format-specific parser
	artwork []Artwork // Cached artwork (nil until ExtractArtwork called)
	rawTags map[string][]RawTag // Format-specific raw tags
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
	file, err := openReader(f, size, path, options)
	if err != nil {
		f.Close()
		return nil, err
	}

	// Keep the file handle for lazy operations (artwork, etc.)
	file.reader = f

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
func openReader(r io.ReaderAt, size int64, path string, options *openOptions) (*File, error) {
	// Detect format
	format, err := DetectFormat(r, size, path)
	if err != nil {
		return nil, err
	}

	// Find parser for this format
	parser := findParser(format)
	if parser == nil {
		return nil, &UnsupportedFormatError{
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
	file.parser = parser

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
	if closer, ok := f.reader.(io.Closer); ok {
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
	if f.artwork != nil {
		return f.artwork, nil
	}

	// Check if parser supports artwork extraction
	extractor, ok := f.parser.(ArtworkExtractor)
	if !ok {
		// Format doesn't support artwork
		return nil, nil
	}

	// Extract artwork
	artwork, err := extractor.ExtractArtwork(f.reader, f.Size, f.Path)
	if err != nil {
		return nil, fmt.Errorf("extract artwork: %w", err)
	}

	// Cache for future calls
	f.artwork = artwork

	return artwork, nil
}

// RawTags returns format-specific raw tag data.
//
// This provides access to tags that may not be mapped to the standard
// Tags fields. Useful for preserving unknown or custom tags.
//
// The returned map should not be modified.
func (f *File) RawTags() map[string][]RawTag {
	return f.rawTags
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

// FormatParser is the interface all format parsers implement.
//
// This interface is public to allow internal format packages to implement it,
// but it's not intended for external use. Do not implement custom parsers.
type FormatParser interface {
	// Parse extracts metadata from an audio file.
	// Returns a partially initialized File (Path, Format, Size set by caller).
	Parse(r io.ReaderAt, size int64, path string) (*File, error)
}

// ArtworkExtractor is an optional interface for parsers that support artwork extraction.
type ArtworkExtractor interface {
	// ExtractArtwork extracts embedded artwork from the file.
	ExtractArtwork(r io.ReaderAt, size int64, path string) ([]Artwork, error)
}

// findParser returns the parser for a given format.
//
// Returns nil if no parser is registered for the format.
func findParser(format Format) FormatParser {
	// Parser registry will be populated as formats are implemented
	// For now, return nil (Phase 1A foundation)
	return parsers[format]
}

// parsers maps formats to their parsers.
// This will be populated in each format package's init() function.
var parsers = make(map[Format]FormatParser)

// RegisterParser registers a parser for a format.
// This is called by format packages during initialization (init functions).
//
// This function is public to allow internal format packages to register themselves,
// but it's not intended for external use. Do not call this function.
func RegisterParser(format Format, parser FormatParser) {
	parsers[format] = parser
}
