package audiometa

// Option configures behavior when opening audio files.
//
// Options use the functional options pattern for clean, extensible APIs.
//
// Example:
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

// WithStrictParsing treats any warning as a fatal error.
//
// By default, audiometa continues parsing when it encounters issues
// like invalid tag encodings or corrupted artwork, returning warnings
// alongside the parsed data.
//
// With strict parsing enabled, any warning becomes a fatal error.
//
// Example:
//
//	file, err := audiometa.Open("song.flac", audiometa.WithStrictParsing())
//	// err != nil if ANY issue is encountered
func WithStrictParsing() Option {
	return func(o *openOptions) {
		o.strictParsing = true
	}
}

// WithArtworkPreload loads artwork immediately instead of lazily.
//
// By default, artwork is only loaded when ExtractArtwork() is called.
// This option loads it during Open() for convenience.
//
// Use this when you know you'll need the artwork and want to fail fast
// if artwork extraction has issues.
//
// Example:
//
//	file, err := audiometa.Open("song.flac", audiometa.WithArtworkPreload())
//	// file.ExtractArtwork() will return cached data
func WithArtworkPreload() Option {
	return func(o *openOptions) {
		o.preloadArtwork = true
	}
}

// WithIgnoreWarnings suppresses all warnings.
//
// By default, warnings about non-fatal issues (invalid encodings, etc.)
// are collected in File.Warnings. This option discards them.
//
// Use this for performance-critical code where you don't care about
// data quality issues.
//
// Example:
//
//	file, err := audiometa.Open("song.flac", audiometa.WithIgnoreWarnings())
//	// file.Warnings will always be empty
func WithIgnoreWarnings() Option {
	return func(o *openOptions) {
		o.ignoreWarnings = true
	}
}

// WithMaxArtworkSize sets a maximum size limit for artwork extraction.
//
// If artwork exceeds this size (in bytes), it will be skipped with a warning.
// This protects against excessively large embedded images.
//
// Default is 0 (no limit).
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
