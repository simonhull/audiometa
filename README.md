# audiometa

[![Go Reference](https://pkg.go.dev/badge/github.com/simonhull/audiometa.svg)](https://pkg.go.dev/github.com/simonhull/audiometa)
[![Go Report Card](https://goreportcard.com/badge/github.com/simonhull/audiometa)](https://goreportcard.com/report/github.com/simonhull/audiometa)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

audiometa is a high-performance, format-agnostic audio metadata library showcasing modern Go 1.25 patterns. When you need to read audio tags, this is the library you reach for.

## Why audiometa?
There are several really great tagging libraries for Go. With special thanks to David Howden and his excellent [Tag](https://github.com/dhowden/tag) Library.
Initially this project began as a fork of that repository designed to add Audiobook tag support for another application I was writing. 
As work continued I wanted to add some more modern language features that had been introduced in the later versions of Go which turned a simple project
into something far more complicated.  Eventually culminating in a full rewrite.

AudioMeta includes the following:
- **🚀 Instant**: <1ms avg per file, lazy loading, zero-copy where possible
- **🎯 Simple API**: `audiometa.Open()` just works - no format detection needed
- **🎨 Modern Go**: Iterators, generics, structured concurrency - taking advantage of the latest language features.
- **💪 Robust**: Graceful degradation with warnings, not crashes
- **📦 Format Support**: FLAC, MP3, M4A/M4B, Ogg Vorbis, Opus
- **🔍 Complete Metadata**: Tags, technical info, artwork, chapters (all formats)

## Quick Start

```bash
go get github.com/simonhull/audiometa
```

```go
package main

import (
    "fmt"
    "log"

    "github.com/simonhull/audiometa"
)

func main() {
    // Open any audio file
    file, err := audiometa.Open("song.flac")
    if err != nil {
        log.Fatal(err)
    }
    defer file.Close()

    // Access metadata
    fmt.Printf("%s - %s\n", file.Tags.Artist, file.Tags.Title)
    fmt.Printf("Album: %s (%d)\n", file.Tags.Album, file.Tags.Year)
    fmt.Printf("Duration: %s\n", file.Audio.Duration)
    fmt.Printf("Format: %s\n", file.Audio) // e.g., "FLAC 44.1kHz 16-bit stereo lossless"
}
```

## Features

### Format-Agnostic API

All formats get called using the same API. 

```go
file, _ := audiometa.Open("mystery_file")
// Works for FLAC, MP3, M4A, M4B, Ogg Vorbis, Opus automatically
```

### Lazy Loading

Large data assets (like artwork) are lazy-loaded on request:

```go
file, _ := audiometa.Open("album.flac")  // Fast - only reads metadata

// Later, if needed:
artwork, _ := file.ExtractArtwork()       // Now load images
if len(artwork) > 0 {
    os.WriteFile("cover.jpg", artwork[0].Data, 0644)
}
```

### Graceful Error Handling

Corrupted tags won't get in the way of scanning.

```go
file, _ := audiometa.Open("partially_corrupted.mp3")

// Got what we could parse
fmt.Println(file.Tags.Title)

// Check for issues
if len(file.Warnings) > 0 {
    for _, w := range file.Warnings {
        fmt.Printf("Warning: %s\n", w)
    }
}
```

### Complete Metadata Access

```go
// Standard tags
file.Tags.Title
file.Tags.Artist
file.Tags.Album
file.Tags.Year

// Multi-value tags
file.Tags.Artists    // []string
file.Tags.Genres     // []string
file.Tags.Composers  // []string

// Technical audio info
file.Audio.Duration
file.Audio.SampleRate
file.Audio.BitDepth
file.Audio.Bitrate
file.Audio.Lossless

// Audiobook-specific
file.Tags.Narrator
file.Tags.Series
file.Tags.SeriesPart

// Cataloging
file.Tags.MusicBrainzTrackID
file.Tags.ISRC
file.Tags.Barcode

// Raw format-specific tags
for key, values := range file.Tags.All() {
    // Access any tag, even unknown ones
}
```

## Supported Formats

| Format      | Read | Write | Artwork | Chapters | Technical Info |
|-------------|------|-------|---------|----------|----------------|
| FLAC        | ✓    | 🚧    | ✓       | ✓        | ✓              |
| MP3         | ✓    | 🚧    | ✓       | ✓        | ✓              |
| M4A         | ✓    | 🚧    | ✓       | ✓        | ✓              |
| M4B         | ✓    | 🚧    | ✓       | ✓        | ✓              |
| Ogg Vorbis  | ✓    | 🚧    | -       | ✓        | ✓              |
| Opus        | ✓    | 🚧    | -       | ✓        | ✓              |

🚧 = Planned for future release

### Chapter Support

All formats support chapter markers for audiobooks and podcasts:
- **MP3**: ID3v2 CHAP frames
- **M4A/M4B**: QuickTime chapter tracks, Nero CHPL format
- **FLAC**: CUESHEET metadata block (CD-style track markers)
- **Ogg Vorbis/Opus**: CHAPTER Vorbis comments

```go
file, _ := audiometa.Open("audiobook.m4b")
for _, chapter := range file.Chapters {
    fmt.Printf("[%d] %s: %s → %s\n",
        chapter.Index,
        chapter.Title,
        chapter.StartTime,
        chapter.EndTime)
}
```

## Examples

See the [`examples/`](examples/) directory for complete, runnable examples:

- **[basic](examples/basic/)**: Simple metadata reading
- **[batch](examples/batch/)**: Process multiple files concurrently
- **[artwork](examples/artwork/)**: Extract and save cover art
- **[chapters](examples/chapters/)**: Extract and display chapter markers

## Performance

audiometa is designed for speed:

- **File open + metadata**: <1ms per file
- **Artwork extraction**: <5ms (depends on size)
- **Memory**: <1MB per File object (excluding artwork)
- **Concurrent**: Safe for parallel parsing

Benchmark on modern hardware (AMD Ryzen):
```
BenchmarkOpen/FLAC-16    2000    550 µs/op    892 B/op    18 allocs/op
BenchmarkOpen/MP3-16     3000    420 µs/op    764 B/op    15 allocs/op
BenchmarkOpen/M4A-16     2500    480 µs/op    834 B/op    16 allocs/op
```

## Command-Line Tools

### audiometa

Display metadata for audio files:

```bash
go install github.com/simonhull/audiometa/cmd/audiometa@latest
audiometa song.flac
```

### atom-dump-tool

Debug M4A/M4B atom structure:

```bash
go install github.com/simonhull/audiometa/cmd/atom-dump-tool@latest
atom-dump-tool audiobook.m4b
```

## API Documentation

Full API documentation is available at [pkg.go.dev/github.com/simonhull/audiometa](https://pkg.go.dev/github.com/simonhull/audiometa).

## Go Version

Requires **Go 1.25+** for modern language features:
- Iterators (`iter.Seq2`)
- Enhanced generics
- New `slices`, `maps`, `cmp` packages
- Built-in `min`, `max`, `clear`

## Project Structure

```
audiometa/
├── *.go              # Public API (file.go, tags.go, audio.go, etc.)
├── internal/         # Private implementation
│   ├── binary/       # Binary reading primitives
│   ├── flac/         # FLAC parser
│   ├── mp3/          # MP3 parser
│   ├── m4a/          # M4A/M4B parser
│   ├── ogg/          # Ogg Vorbis/Opus parser
│   ├── vorbis/       # Shared Vorbis comment parsing
│   └── parsing/      # Parsing utilities
├── cmd/              # Command-line tools
├── examples/         # Example programs
└── docs/             # Documentation
```

## Architecture

audiometa follows modern Go best practices:

- **Layered API**: Simple things simple, complex things possible
- **Zero surprises**: Predictable, well-documented behavior
- **Performance first**: Optimized hot paths, lazy loading
- **Graceful degradation**: Warnings instead of errors where possible

### Format Parser Registration

All supported format parsers (FLAC, MP3, M4A, M4B, Ogg Vorbis, Opus) are automatically
registered when you import the audiometa package. No additional imports or initialization
code is required.

This differs from Go's `database/sql` pattern where drivers must be explicitly imported.
For audiometa, supporting all common audio formats is considered a core feature rather
than an optional dependency, following the same pattern as Go's `image` package.

## Development

```bash
# Clone the repository
git clone https://github.com/simonhull/audiometa.git
cd audiometa

# Run all tests
go test ./...

# Run tests with race detector
go test -race ./...

# Run tests with coverage
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out

# Run benchmarks
go test -bench=. -benchmem ./...

# Run benchmarks with profiling
go test -bench=BenchmarkOpen -cpuprofile=cpu.prof -memprofile=mem.prof
go tool pprof cpu.prof

# Generate code (stringer for enums)
go generate ./...

# Format code
gofmt -s -w .
goimports -w .

# Run linters (install golangci-lint first: https://golangci-lint.run/usage/install/)
golangci-lint run ./...

# Install CLI tools locally
go install ./cmd/audiometa
go install ./cmd/atom-dump-tool

# Build all binaries
go build -v ./cmd/...
```

**Helper Scripts**: For convenience, use the scripts in `scripts/`:
- `./scripts/test.sh` - Full test suite with coverage
- `./scripts/bench.sh` - Benchmarks with profiling
- `./scripts/lint.sh` - Run all linters

## License

[MIT License](LICENSE)

## Acknowledgments

- Inspired by [Tag](https://github.com/dhowden/tag), [TagLib](https://taglib.org/) and [Mutagen](https://github.com/quodlibet/mutagen)
- Built with modern Go patterns and performance in mind

---

**Made with ❤️ using Go 1.25**
