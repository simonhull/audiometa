package audiometa

// Option configures how [Open] and [OpenContext] read a file.
//
// Options are applied in order; the With functions in this package create
// them. Example:
//
//	file, err := audiometa.Open("song.flac",
//	    audiometa.WithStrictParsing(),
//	    audiometa.WithArtworkPreload(),
//	)
type Option func(*openOptions)

// openOptions holds configuration for opening files.
type openOptions struct {
	strictParsing  bool // Fail on any warning
	preloadArtwork bool // Load artwork immediately instead of lazily
	ignoreWarnings bool // Suppress all warnings
	maxArtworkSize int  // Maximum artwork size in bytes (0 = no limit)
}

// defaultOptions returns the default configuration.
func defaultOptions() *openOptions {
	return &openOptions{
		strictParsing:  false,
		preloadArtwork: false,
		ignoreWarnings: false,
		maxArtworkSize: 0, // No limit
	}
}

// WithStrictParsing makes any parse warning fatal.
//
// By default, audiometa keeps going when it meets damaged data, such as an
// undecodable tag frame, and reports the problem in File.Warnings. With strict
// parsing, Open fails instead if any warning was recorded, and returns no File.
// Warnings produced by [WithArtworkPreload] count too. Strict parsing takes
// precedence over [WithIgnoreWarnings].
//
// The error's message names the first warning. It unwraps (as a multi-error)
// to the underlying errors of all the warnings that have one, so [errors.As]
// and [errors.AsType] can find, for example, an [*OutOfBoundsError] behind a
// truncated block:
//
//	_, err := audiometa.Open("song.flac", audiometa.WithStrictParsing())
//	if oob, ok := errors.AsType[*audiometa.OutOfBoundsError](err); ok {
//		fmt.Println("truncated while reading", oob.What)
//	}
func WithStrictParsing() Option {
	return func(o *openOptions) {
		o.strictParsing = true
	}
}

// WithArtworkPreload reads the artwork during Open instead of on the first
// [File.ExtractArtwork] call.
//
// Use it when you know you will need the artwork. A failed extraction does
// not make Open fail; it is recorded as a StageArtwork [Warning] (and so does
// fail Open under [WithStrictParsing]). After a successful preload,
// [File.ExtractArtwork] returns the cached images without reading the file.
func WithArtworkPreload() Option {
	return func(o *openOptions) {
		o.preloadArtwork = true
	}
}

// WithIgnoreWarnings discards warnings, leaving File.Warnings empty.
//
// It covers warnings from parsing and from later [File.ExtractArtwork] calls.
// Parsing itself is unchanged, so it does not make Open faster. If
// [WithStrictParsing] is also set, a file with warnings still fails to open.
func WithIgnoreWarnings() Option {
	return func(o *openOptions) {
		o.ignoreWarnings = true
	}
}

// WithMaxArtworkSize sets the largest image, in bytes, that
// [File.ExtractArtwork] returns.
//
// Each larger image is left out of the result and a StageArtwork [Warning]
// is appended to File.Warnings (unless [WithIgnoreWarnings] is set). The
// limit applies per image and is checked after the image is read, so it
// bounds what the caller keeps, not what is read; a fixed internal cap on
// single allocations applies regardless. Zero or a negative value, the
// default, means no limit.
//
// Example:
//
//	// Limit artwork to 10MB
//	file, err := audiometa.Open("song.flac",
//	    audiometa.WithMaxArtworkSize(10*1024*1024),
//	)
func WithMaxArtworkSize(bytes int) Option {
	return func(o *openOptions) {
		o.maxArtworkSize = bytes
	}
}
