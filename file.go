package audiometa

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"sync"

	"golang.org/x/sync/errgroup"

	"github.com/simonhull/audiometa/internal/registry"
	"github.com/simonhull/audiometa/internal/types"

	// Register built-in format parsers.
	_ "github.com/simonhull/audiometa/internal/flac"
	_ "github.com/simonhull/audiometa/internal/m4a"
	_ "github.com/simonhull/audiometa/internal/mp3"
	_ "github.com/simonhull/audiometa/internal/ogg"
)

// File represents an opened audio file with parsed metadata.
//
// Embeds types.File for the data payload (Path, Tags, Audio, Chapters, ...)
// and adds runtime state (the underlying file handle, parser reference, and
// artwork cache) used by methods like Close and ExtractArtwork.
type File struct {
	types.File

	reader  io.ReaderAt
	parser  FormatParser
	artwork []Artwork

	maxArtworkSize int  // from WithMaxArtworkSize; 0 = no limit
	ignoreWarnings bool // from WithIgnoreWarnings; also suppresses artwork-stage warnings

	artworkMu     sync.Mutex // guards artwork and artworkLoaded
	artworkLoaded bool
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
	return openWithContext(context.Background(), path, opts...)
}

func openWithContext(ctx context.Context, path string, opts ...Option) (*File, error) {
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
		_ = f.Close()
		return nil, fmt.Errorf("stat file: %w", err)
	}
	size := stat.Size()

	// Parse with the reader
	typesFile, err := openReader(ctx, f, size, path)
	if err != nil {
		_ = f.Close()
		return nil, err
	}

	// Wrap in File struct
	file := &File{
		File:   *typesFile.file,
		reader: f,
		parser: typesFile.parser,

		maxArtworkSize: options.maxArtworkSize,
		ignoreWarnings: options.ignoreWarnings,
	}

	// Preload artwork if requested
	if options.preloadArtwork {
		if _, err := file.ExtractArtwork(); err != nil {
			// Don't fail completely, just add warning
			file.Warnings = types.AppendWarning(file.Warnings, Warning{
				Stage:   StageArtwork,
				Message: fmt.Sprintf("preload artwork failed: %v", err),
				Err:     err,
			})
		}
	}

	if err := applyWarningPolicy(&file.File, options); err != nil {
		_ = f.Close()
		return nil, err
	}
	return file, nil
}

// strictParsingError reports that WithStrictParsing rejected a file. Its message
// names the first warning; Unwrap exposes every warning's underlying error so
// callers can use errors.Is/As.
type strictParsingError struct {
	msg  string
	errs []error
}

func (e *strictParsingError) Error() string   { return e.msg }
func (e *strictParsingError) Unwrap() []error { return e.errs }

// applyWarningPolicy enforces WithStrictParsing, then WithIgnoreWarnings.
// Strict takes precedence: a file with any warning fails even when
// warnings are also being ignored.
func applyWarningPolicy(file *types.File, options *openOptions) error {
	if options.strictParsing && len(file.Warnings) > 0 {
		msg := "strict parsing failed: " + file.Warnings[0].Message
		if n := len(file.Warnings) - 1; n > 0 {
			msg += fmt.Sprintf(" (and %d more warnings)", n)
		}
		var errs []error
		for _, w := range file.Warnings {
			if w.Err != nil {
				errs = append(errs, w.Err)
			}
		}
		return &strictParsingError{msg: msg, errs: errs}
	}
	if options.ignoreWarnings {
		file.Warnings = nil
	}
	return nil
}

// maxPanicDetail bounds how much of a recovered panic value is copied into the error.
const maxPanicDetail = 256

// panicError converts a recovered panic value into a CorruptedFileError.
//
// recover cannot catch Go runtime fatal errors such as out-of-memory or
// concurrent map writes; those still terminate the process. Bounding
// allocations from untrusted length fields is a separate safeguard.
func panicError(path string, rec any) error {
	detail := fmt.Sprint(rec)
	if len(detail) > maxPanicDetail {
		detail = detail[:maxPanicDetail] + "..."
	}
	return &types.CorruptedFileError{Path: path, Reason: "parser panic: " + detail}
}

// parseSafely calls p.Parse, converting a panic into a CorruptedFileError.
func parseSafely(ctx context.Context, p FormatParser, r io.ReaderAt, size int64, path string) (file *types.File, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			file, err = nil, panicError(path, rec)
		}
	}()
	return p.Parse(ctx, r, size, path)
}

// extractSafely calls e.ExtractArtwork, converting a panic into a CorruptedFileError.
func extractSafely(ctx context.Context, e ArtworkExtractor, r io.ReaderAt, size int64, path string) (art []Artwork, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			art, err = nil, panicError(path, rec)
		}
	}()
	return e.ExtractArtwork(ctx, r, size, path)
}

// parsedFile bundles the parsed data payload with the parser that produced it,
// so openReader can hand both back to its caller without exposing the parser
// reference on the public types.File data type.
type parsedFile struct {
	file   *types.File
	parser FormatParser
}

// openReader opens from an io.ReaderAt (internal, for testing).
func openReader(ctx context.Context, r io.ReaderAt, size int64, path string) (*parsedFile, error) {
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

	// Parse metadata; parsers check ctx at major boundaries.
	file, err := parseSafely(ctx, parser, r, size, path)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", format, err)
	}

	// Set file-level fields
	file.Path = path
	file.Format = format
	file.Size = size

	return &parsedFile{file: file, parser: parser}, nil
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
// Returns an empty slice if the file contains no artwork. Safe for concurrent
// use; extraction runs at most once per File (failed attempts are retried on
// the next call).
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
	return f.ExtractArtworkContext(context.Background())
}

// ExtractArtworkContext is the cancellable form of ExtractArtwork.
//
// Use this when you want to bound how long artwork extraction can take,
// for example when scanning many files in a worker pool.
func (f *File) ExtractArtworkContext(ctx context.Context) ([]Artwork, error) {
	f.artworkMu.Lock()
	defer f.artworkMu.Unlock()

	if f.artworkLoaded {
		return f.artwork, nil
	}

	artwork := []Artwork{}
	if extractor, ok := f.parser.(ArtworkExtractor); ok {
		extracted, err := extractSafely(ctx, extractor, f.reader, f.Size, f.Path)
		if err != nil {
			return nil, fmt.Errorf("extract artwork: %w", err) // Not cached: a later call may retry.
		}
		if extracted != nil {
			artwork = extracted
		}
	}
	artwork = f.filterOversized(artwork)

	f.artwork = artwork
	f.artworkLoaded = true
	return artwork, nil
}

// filterOversized drops images larger than the WithMaxArtworkSize limit and
// records a warning for each. The filter runs after extraction, so the image is
// still read transiently; binary.MaxAllocBytes bounds that read regardless.
// The caller must hold artworkMu.
func (f *File) filterOversized(artwork []Artwork) []Artwork {
	if f.maxArtworkSize <= 0 {
		return artwork
	}
	kept := make([]Artwork, 0, len(artwork))
	for _, a := range artwork {
		if len(a.Data) <= f.maxArtworkSize {
			kept = append(kept, a)
			continue
		}
		if !f.ignoreWarnings {
			f.Warnings = types.AppendWarning(f.Warnings, Warning{
				Stage:   StageArtwork,
				Message: fmt.Sprintf("artwork (%s, %d bytes) exceeds max size %d; skipped", a.MIMEType, len(a.Data), f.maxArtworkSize),
			})
		}
	}
	return kept
}

// OpenContext opens a file with context-aware cancellation.
//
// The context is checked before opening the file and is threaded into each
// parser, which yields to cancellation at major parse boundaries (block
// headers, atom boundaries, frame walks). Cancellation between files in a
// batch is therefore observed promptly even on large inputs.
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
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return openWithContext(ctx, path, opts...)
}

// OpenMany opens multiple audio files concurrently.
//
// Files are parsed in parallel using up to runtime.NumCPU() goroutines.
// The returned slice is parallel to the input paths: results[i] is either
// the parsed file for paths[i] or nil if that file failed to parse.
//
// One bad file does not abort the others. The returned error is non-nil if
// any file failed; it is the errors.Join of every individual failure, so
// callers can use errors.Is/As against it. The caller owns Close()ing any
// non-nil entries.
//
// Example:
//
//	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
//	defer cancel()
//
//	files, err := audiometa.OpenMany(ctx, paths...)
//	if err != nil {
//		// One or more files failed; successful files are still returned.
//		// Use errors.Is/As against the joined error to inspect individual failures.
//		log.Printf("partial failure: %v", err)
//	}
//	defer func() {
//		for _, f := range files {
//			if f != nil {
//				f.Close()
//			}
//		}
//	}()
//
//	for i, f := range files {
//		if f == nil {
//			continue // failed; see joined error
//		}
//		fmt.Printf("%s: %s - %s\n", f.Format, f.Tags.Artist, f.Tags.Title)
//	}
//
// The returned slice is parallel to the input paths: results[i] is either
// the parsed file for paths[i] or nil if that file failed to parse. The
// returned error is non-nil if any file failed; use errors.Is/As against
// the joined error to inspect individual failures.
//
// If the context is canceled, any in-flight parses observe the cancellation
// at their next checkpoint; already-parsed files are still returned.
func OpenMany(ctx context.Context, paths ...string) ([]*File, error) {
	if len(paths) == 0 {
		return nil, nil
	}

	results := make([]*File, len(paths))
	errs := make([]error, len(paths))

	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(runtime.NumCPU())

	for i, path := range paths {
		g.Go(func() error {
			defer func() {
				if rec := recover(); rec != nil {
					errs[i] = fmt.Errorf("%s: %w", path, panicError(path, rec))
				}
			}()
			file, err := openWithContext(ctx, path)
			if err != nil {
				errs[i] = fmt.Errorf("%s: %w", path, err)
				return nil // Don't cancel siblings on individual file failure.
			}
			results[i] = file
			return nil
		})
	}
	_ = g.Wait() // Per-file errors are collected in errs; g never errors.

	return results, errors.Join(errs...)
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
//
// Deprecated: parsers register themselves via internal/registry; external
// callers cannot supply a usable FormatParser.
func RegisterParser(format types.Format, parser FormatParser) {
	registry.Register(format, parser)
}
