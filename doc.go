// Package audiometa provides format-agnostic audio metadata extraction.
//
// audiometa is designed to be the inevitable choice for audio metadata reading
// in Go. It supports multiple formats (FLAC, MP3, M4A/M4B) with a unified API
// that makes simple things simple and complex things possible.
//
// # Quick Start
//
// Reading metadata from an audio file:
//
//	file, err := audiometa.Open("song.flac")
//	if err != nil {
//		log.Fatal(err)
//	}
//	defer file.Close()
//
//	fmt.Printf("%s - %s\n", file.Tags.Artist, file.Tags.Title)
//	fmt.Printf("Duration: %s\n", file.Audio.Duration)
//
// # Supported Formats
//
//   - FLAC: Lossless audio with Vorbis comments and embedded pictures
//   - MP3: ID3v2.3 and ID3v2.4 tags with full frame support
//   - M4A/M4B: iTunes metadata atoms, audiobook tags, and chapters
//
// # Philosophy
//
// audiometa embodies three core principles:
//
// 1. Performance: Every operation should feel instant. We optimize for
// correctness first, but we don't write slow code.
//
// 2. Graceful Degradation: Corrupted files return partial data + warnings,
// not errors. Missing optional fields don't stop parsing.
//
// 3. Zero Surprises: Behavior is predictable and well-documented. The API
// guides you toward correct usage.
//
// # Architecture
//
// The library uses a layered architecture:
//
//	[File]           - Entry point with Open()
//	  ├─ [Tags]      - Format-agnostic metadata
//	  ├─ [AudioInfo] - Technical properties
//	  └─ [Artwork]   - Embedded images (lazy loaded)
//
// Format-specific parsers implement a common interface, making it easy
// to add new formats without changing the public API.
//
// # Advanced Usage
//
// Extract artwork:
//
//	artwork, err := file.ExtractArtwork()
//	if err == nil && len(artwork) > 0 {
//		os.WriteFile("cover.jpg", artwork[0].Data, 0644)
//	}
//
// Parse multiple files concurrently:
//
//	ctx := context.Background()
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
// Iterate over raw tags:
//
//	for key, values := range file.Tags.All() {
//		fmt.Printf("%s: %v\n", key, values)
//	}
//
// # Error Handling
//
// audiometa distinguishes between fatal errors and warnings:
//
//   - Fatal errors prevent parsing entirely (file not found, unsupported format)
//   - Warnings indicate non-fatal issues (corrupted tags, missing optional fields)
//
// Always check file.Warnings for issues encountered during parsing:
//
//	if len(file.Warnings) > 0 {
//		for _, w := range file.Warnings {
//			log.Printf("Warning: %s", w)
//		}
//	}
//
// # Performance
//
// audiometa is designed for speed:
//
//   - Lazy loading: Artwork not loaded until ExtractArtwork() is called
//   - Zero-copy: Minimal memory allocation during parsing
//   - Concurrent: OpenMany() parses files in parallel
//   - Streaming: Reads only necessary portions of files
//
// Typical performance on modern hardware:
//   - File open + tag parsing: <1ms per file
//   - Artwork extraction: <5ms (depends on image size)
//   - Memory: <1MB per File object (excluding artwork)
//
// # Go 1.25 Features
//
// audiometa showcases modern Go patterns:
//
//   - Iterators: Range over tags with iter.Seq
//   - Generics: Type-safe binary reading
//   - Structured concurrency: Context-aware operations
//   - New stdlib: slices, maps, cmp packages
//   - slog: Structured logging support
//
// # Reference Implementation
//
// This library serves as a reference implementation for:
//   - Clean API design
//   - Modern Go patterns
//   - Performance optimization
//   - Comprehensive testing
//   - Living documentation
//
// See MODERN_GO_PATTERNS.md for details on the patterns used.
//
// # Contributing
//
// See CONTRIBUTING.md for guidelines.
//
// # License
//
// See LICENSE file.
package audiometa
