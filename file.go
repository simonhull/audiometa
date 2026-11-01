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

// File is an opened audio file with its parsed metadata.
//
// The parsed data lives in the promoted fields of the embedded data payload:
//
//   - Path, Format and Size describe the file.
//   - Tags holds the format-agnostic tags ([Tags]).
//   - Audio holds the technical properties ([AudioInfo]).
//   - Chapters lists the chapter markers ([Chapter]), in playback order.
//   - Warnings lists non-fatal problems found while parsing ([Warning]).
//
// A File also keeps the underlying file handle open for lazy artwork
// extraction, so call [File.Close] when done with it.
//
// Tags and artwork can be edited in memory and saved with [File.WriteTo] or
// [Write]; see Tags.Set and [File.AddArtwork].
//
// [File.ExtractArtwork], [File.ExtractArtworkContext], [File.AudioDigest],
// [File.AudioDigestContext] and the artwork edit methods are safe for
// concurrent use. The exported fields are not
// synchronized; do not modify them while other goroutines use the File.
type File struct {
	types.File

	reader  io.ReaderAt
	parser  FormatParser
	artwork []Artwork

	maxArtworkSize int  // from WithMaxArtworkSize; 0 = no limit
	ignoreWarnings bool // from WithIgnoreWarnings; also suppresses artwork-stage warnings

	artworkMu     sync.Mutex // guards artwork and artworkLoaded
	artworkLoaded bool

	editMu     sync.Mutex        // guards artworkOps
	artworkOps []types.ArtworkOp // pending artwork edits, applied by WriteTo
}

// Open opens the audio file at path and parses its metadata.
//
// The format is detected from the file's contents. Supported formats are FLAC,
// MP3, M4A, M4B, Ogg Vorbis and Opus. Only metadata is read: audio data is
// never loaded, and artwork is read later by [File.ExtractArtwork]. The
// caller must [File.Close] the returned File.
//
// Damaged but readable metadata does not make Open fail: it returns the data
// it could parse and records the problems in File.Warnings. Pass options such
// as [WithStrictParsing] or [WithIgnoreWarnings] to change that policy.
//
// # Errors
//
// Open returns an error, and no File, when:
//
//   - the file cannot be opened or stat'ed: the error wraps the [*os.PathError];
//   - the format is unknown or has no parser: [*UnsupportedFormatError];
//   - the container structure is unusable: often a [*CorruptedFileError],
//     or an [*OutOfBoundsError] for a required read past the end of the file;
//   - [WithStrictParsing] is set and parsing produced a warning.
//
// Errors are wrapped; use [errors.Is] and [errors.As] to inspect them.
// Use [OpenContext] to bound or cancel the parse.
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
	types.BindFormat(&file.Tags, format)

	return &parsedFile{file: file, parser: parser}, nil
}

// Close closes the underlying file handle.
//
// The parsed fields stay readable after Close, but artwork that has not been
// extracted yet can no longer be read. Close returns the error from closing
// the handle, if any.
func (f *File) Close() error {
	if closer, ok := f.reader.(io.Closer); ok {
		return closer.Close()
	}
	return nil
}

// ExtractArtwork returns the images embedded in the file, in file order.
//
// Artwork is not read by [Open]. The first successful call reads it and
// caches the result; later calls return the cached slice, which callers must
// not modify. A file without artwork yields an empty, non-nil slice. Each
// [Artwork] carries its MIME type, picture type and, when known, its pixel
// dimensions.
//
// Images larger than the [WithMaxArtworkSize] limit are left out, and each
// one adds a StageArtwork [Warning] to File.Warnings unless
// [WithIgnoreWarnings] is set.
//
// ExtractArtwork is safe for concurrent use. A failed extraction is not
// cached, so a later call retries it. The error wraps the parser's error,
// for example an [*OutOfBoundsError] when a picture runs past the end of the
// file, or a [*CorruptedFileError] if the parser panicked. Use
// [File.ExtractArtworkContext] to make extraction cancellable.
func (f *File) ExtractArtwork() ([]Artwork, error) {
	return f.ExtractArtworkContext(context.Background())
}

// ExtractArtworkContext is like [File.ExtractArtwork] but stops early when
// ctx is done, returning an error that wraps ctx.Err().
//
// Use it to bound how long artwork extraction can take, for example when
// scanning many files in a worker pool. It is safe for concurrent use.
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

// OpenContext is like [Open] but stops early when ctx is done.
//
// The context is checked before the file is opened and is passed to the
// parser, which checks it at major parse boundaries (metadata blocks, atoms,
// frames) and periodically inside long loops, so cancellation is observed
// promptly even on large or hostile inputs.
//
// If ctx is done, OpenContext returns an error that wraps ctx.Err(), so
// [errors.Is] reports [context.Canceled] or [context.DeadlineExceeded]. Other
// errors are as for [Open].
func OpenContext(ctx context.Context, path string, opts ...Option) (*File, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return openWithContext(ctx, path, opts...)
}

// OpenMany opens and parses several audio files concurrently, using up to
// [runtime.NumCPU] goroutines and the default options.
//
// The returned slice is parallel to paths: element i is the File for paths[i],
// or nil if that file failed. One bad file does not stop the others. The
// caller must [File.Close] every non-nil element.
//
// The error is nil when every file opened. Otherwise it is the [errors.Join]
// of the individual failures, each prefixed with its path and wrapping the
// same errors [Open] would return, so [errors.Is] and [errors.As] work on it.
//
// When ctx is done, files still being parsed fail with an error wrapping
// ctx.Err(); files that had already been parsed are still returned.
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

// FormatParser is the interface the built-in format parsers implement.
//
// It is exported only for backwards compatibility: it refers to internal
// types, so code outside this module cannot implement it usefully.
type FormatParser = registry.FormatParser

// ArtworkExtractor is the optional interface of format parsers that can read
// embedded artwork.
//
// It is exported only for backwards compatibility; see [FormatParser].
type ArtworkExtractor = registry.ArtworkExtractor

// findParser returns the parser for a given format.
//
// Returns nil if no parser is registered for the format.
func findParser(format types.Format) FormatParser {
	return registry.Get(format)
}

// RegisterParser registers parser as the parser for format, replacing any
// parser already registered for it.
//
// All built-in parsers are registered automatically when this package is
// imported. RegisterParser is not intended for external use.
//
// Deprecated: parsers register themselves via internal/registry; external
// callers cannot supply a usable FormatParser.
func RegisterParser(format types.Format, parser FormatParser) {
	registry.Register(format, parser)
}
