# audiometa

[![Go Reference](https://pkg.go.dev/badge/github.com/simonhull/audiometa.svg)](https://pkg.go.dev/github.com/simonhull/audiometa)
[![Go Report Card](https://goreportcard.com/badge/github.com/simonhull/audiometa)](https://goreportcard.com/report/github.com/simonhull/audiometa)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

audiometa is a fast, format-agnostic audio metadata reader for Go. One call reads tags,
technical audio properties, chapters and embedded artwork from FLAC, MP3, M4A/M4B,
Ogg Vorbis and Opus files, with first-class support for audiobooks.

## Why audiometa?
There are several really great tagging libraries for Go. With special thanks to David Howden and his excellent [Tag](https://github.com/dhowden/tag) Library.
Initially this project began as a fork of that repository designed to add Audiobook tag support for another application I was writing.
As work continued I wanted to add some more modern language features that had been introduced in the later versions of Go which turned a simple project
into something far more complicated.  Eventually culminating in a full rewrite.

AudioMeta includes the following:
- **🚀 Fast**: tens of microseconds to open a typical file ([measured](#performance)); artwork is read only on request
- **🎯 Simple API**: `audiometa.Open()` just works - the format is detected from the file's contents
- **🎨 Modern Go**: iterators, generics, `context` cancellation throughout
- **💪 Robust**: graceful degradation with warnings; bounds-checked reads, allocation caps and panic recovery for hostile files
- **📦 Format Support**: FLAC, MP3, M4A/M4B, Ogg Vorbis, Opus
- **🔍 Complete Metadata**: tags, raw tags, technical info, artwork and chapters in every supported format

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

Runnable versions of this and the snippets below are on
[pkg.go.dev](https://pkg.go.dev/github.com/simonhull/audiometa#pkg-examples).

## Features

### Format-Agnostic API

All formats are read through the same API. The format is detected from the file's
signature bytes, not its extension.

```go
file, _ := audiometa.Open("mystery_file")
// Works for FLAC, MP3, M4A, M4B, Ogg Vorbis and Opus
fmt.Println(file.Format) // e.g. "Opus"
```

### Lazy Artwork

`Open` reads metadata only. Artwork is read on the first `ExtractArtwork` call and
cached; `ExtractArtwork` is safe for concurrent use.

```go
file, _ := audiometa.Open("album.flac") // reads metadata only

artwork, err := file.ExtractArtwork() // now reads the images
if err == nil && len(artwork) > 0 {
    os.WriteFile("cover.jpg", artwork[0].Data, 0o644)
}
```

### Audio Digest

`AudioDigest` returns a SHA-256 of the audio data alone. Renaming or moving a file, or
editing its tags and artwork with audiometa, never changes it, so it identifies the same
recording across all three. See the `AudioDigest` docs for exactly what each format hashes.

```go
sum, err := file.AudioDigest() // [32]byte; reads all of the audio
```

### Graceful Error Handling

Damaged tags don't stop a scan. What could be parsed is returned, and the problems are
listed in `file.Warnings`.

```go
file, err := audiometa.Open("partially_corrupted.mp3")
if err != nil {
    log.Fatal(err) // unreadable, unsupported or structurally broken
}

fmt.Println(file.Tags.Title) // got what we could parse

for _, w := range file.Warnings {
    fmt.Printf("Warning: %s\n", w) // e.g. "metadata: failed to read frame TPE1: ..."
}
```

`Open` fails only when there is nothing useful to return. Errors are wrapped; match them
with `errors.Is` / `errors.As` (or `errors.AsType`):

| Error | When |
|-------|------|
| `*os.PathError` (e.g. `fs.ErrNotExist`) | the file cannot be opened |
| `*audiometa.UnsupportedFormatError` | unknown format, unreadable header, or a detected format without a parser (WAV, AIFF) |
| `*audiometa.CorruptedFileError` | invalid container structure, or a recovered parser panic |
| `*audiometa.OutOfBoundsError` | a required read runs past the end of the file (truncation, corrupt length) |
| `context.Canceled` / `context.DeadlineExceeded` | the context passed to `OpenContext` / `OpenMany` is done |

### Options

```go
file, err := audiometa.Open("song.flac",
    audiometa.WithStrictParsing(),
    audiometa.WithMaxArtworkSize(10<<20),
)
```

| Option | Effect |
|--------|--------|
| `WithStrictParsing()` | any warning makes `Open` fail; the error unwraps to the warnings' causes (e.g. `*OutOfBoundsError`) |
| `WithIgnoreWarnings()` | discard warnings (`file.Warnings` stays empty) |
| `WithArtworkPreload()` | read artwork during `Open` instead of on first `ExtractArtwork` |
| `WithMaxArtworkSize(n)` | drop images larger than `n` bytes from `ExtractArtwork`, with a warning |

### Cancellation and Batches

```go
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

file, err := audiometa.OpenContext(ctx, "huge.m4b") // parse loops check ctx

// Parse many files in parallel; files[i] is nil if paths[i] failed,
// and err joins the individual failures.
files, err := audiometa.OpenMany(ctx, paths...)
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
file.Audio.ReplayGain // *ReplayGainInfo, nil if absent

// Audiobook-specific (filled only from explicit tags, never inferred)
file.Tags.Narrator   // NARRATOR (Vorbis), TXXX NARRATOR (ID3), "Narrator" atom (M4A)
file.Tags.Series     // SERIES / MVNM / SHOW (Vorbis), TXXX SERIES / frame MVNM (ID3), "Series" atom / ©mvn (M4A)
file.Tags.SeriesPart // SERIESPART / MVIN (Vorbis), TXXX SERIES PART / frame MVIN (ID3), "Series Part" atom / ©mvi (M4A)
file.Tags.Grouping   // GROUPING / GRP1 / ©grp, as stored

// Cataloging
file.Tags.MusicBrainzTrackID
file.Tags.ISRC
file.Tags.Barcode
```

### Raw Tags

Every tag is also kept under its native key, including ones audiometa has no field for:

```go
encoder := file.Tags.GetFirst("ENCODER") // first value, or ""
artists := file.Tags.Get("ARTIST")       // all values (a copy), or nil

for key, values := range file.Tags.All() { // iter.Seq2, unspecified order
    fmt.Println(key, values)
}
```

Raw keys are format-specific:

| Format | Raw key |
|--------|---------|
| FLAC, Ogg Vorbis, Opus | Vorbis comment name, upper-cased (`TITLE`, `ENCODER`) |
| MP3 | ID3v2 frame ID (`TIT2`, `TSSE`); TXXX frames under their upper-cased description |
| M4A/M4B | ilst atom name (`"\xa9nam"` for ©nam, `aART`); freeform `----` atoms under their upper-cased name |

## Supported Formats

| Format      | Read | Write | Artwork | Chapters | Technical Info |
|-------------|------|-------|---------|----------|----------------|
| FLAC        | ✓    | ✓     | ✓       | ✓        | ✓              |
| MP3         | ✓    | ✓     | ✓       | ✓        | ✓              |
| M4A         | ✓    | ✓     | ✓       | ✓        | ✓              |
| M4B         | ✓    | ✓     | ✓       | ✓        | ✓              |
| Ogg Vorbis  | ✓    | -     | ✓       | ✓        | ✓              |
| Opus        | ✓    | -     | ✓       | ✓        | ✓              |

Ogg Vorbis and Opus writing is not supported yet (see
[docs/design/020-write-support-design.md](docs/design/020-write-support-design.md)). WAV and AIFF
are detected but not parsed. See [Writing metadata](#writing-metadata) for what writing does
and does not touch.

Tag formats:

- **FLAC**: Vorbis comments, PICTURE blocks
- **MP3**: ID3v2.2, ID3v2.3 and ID3v2.4 (unsynchronisation undone; compressed or encrypted
  frames skipped with a warning), falling back to an ID3v1 tag when ID3v2 gives no title
  or artist
- **M4A/M4B**: iTunes `ilst` atoms, including freeform `----` atoms
- **Ogg Vorbis/Opus**: Vorbis comments, `METADATA_BLOCK_PICTURE` artwork

### Chapter Support

Every format supports chapter markers for audiobooks and podcasts. Each chapter's
`Source` records where it came from:

| Format | Chapter source (in order of preference) | `Chapter.Source` |
|--------|------------------------------------------|------------------|
| M4A/M4B | QuickTime chapter text track, else Nero `chpl` atom | `quicktime`, `nero` |
| MP3 | ID3v2 `CHAP` frames, else the OverDrive MediaMarkers `TXXX` frame | `id3_chap`, `overdrive` |
| FLAC | `CHAPTERxxx` Vorbis comments, else the `CUESHEET` block | `vorbis_comment`, `flac_cuesheet` |
| Ogg Vorbis/Opus | `CHAPTERxxx` Vorbis comments | `vorbis_comment` |

```go
file, _ := audiometa.Open("audiobook.m4b")
for _, chapter := range file.Chapters {
    fmt.Printf("[%d] %s: %s → %s (%s)\n",
        chapter.Index,
        chapter.Title,
        chapter.StartTime,
        chapter.EndTime,
        chapter.Source) // e.g. audiometa.ChapterSourceQuickTime
}
```

## Writing metadata

MP3, FLAC and M4A/M4B files can be edited. `Write` opens the file, hands it to your
callback, and atomically replaces it:

```go
err := audiometa.Write("book.m4b", func(f *audiometa.File) error {
	f.Tags.Title = "The Way of Kings"
	f.Tags.Narrator = "Michael Kramer"
	f.Tags.Delete("\xa9cmt") // remove a tag by its native key
	return f.ReplaceArtwork(audiometa.Artwork{Data: cover, Type: audiometa.ArtworkFrontCover})
})
```

To stream the edited file somewhere else (the original is never touched), edit a `File` from
`Open` and call `WriteTo`:

```go
f, _ := audiometa.Open("book.mp3")
defer f.Close()
f.Tags.Series = "The Stormlight Archive"
_, err := f.WriteTo(w) // any io.Writer; WriteToContext takes a context
```

- **Only what you change is written.** audiometa never infers one field from another, on
  read or on write: only the keys and fields you set or delete change. Writing `Narrator`
  leaves the composer alone, and setting `Series` does not rewrite the grouping.
- **Safety.** The new file is written to a temporary file in the same directory, synced,
  verified (it must parse, with bit-identical audio and unchanged chapters) and renamed over
  the original. On any error the original is untouched.
- **Preservation.** Unknown frames, atoms and blocks, and every tag you did not edit, are kept
  byte for byte.
- **Chapters** are preserved but not editable: they stay as they are, and an edit that
  targets chapter data returns `ErrUnsupportedWrite`.
- **ID3.** v2.3 stays v2.3, v2.4 stays v2.4, and v2.2 or untagged files get v2.4. An existing
  ID3v1 trailer is copied verbatim: it is never updated, added or removed. After deleting
  `TIT2`, a reader that falls back to ID3v1 can show the stale ID3v1 title.
- **Editing.** Change `f.Tags` fields directly, or use `Tags.Set` and `Tags.Delete` with
  format-native keys. Editing a field and its native key to different values returns
  `ErrConflictingEdits`.
- **Artwork.** `AddArtwork`, `ReplaceArtwork`, `RemoveArtwork` and `RemoveAllArtwork`.
  M4A cover art has no picture type (only front cover or other).
- **DRM.** Encrypted files (FairPlay, CENC, AAX, ID3 encrypted frames) return
  `ErrDRMProtected`.
- **Not writable yet:** Ogg Vorbis and Opus return `ErrUnsupportedWrite`, as do fragmented
  MP4 and faststart M4A files whose chunk offsets would pass 4 GiB.

Typed fields a format has no native place for return `ErrUnsupportedWrite`:

| Format | Typed fields that cannot be written |
|--------|-------------------------------------|
| MP3 | `Date`, `OriginalDate`, `Lyrics`, `Performers`, `Copyright`, `Label`, `CatalogNumber`, `Barcode`, `ISRC`, `MusicBrainz*` |
| M4A/M4B | `OriginalDate`, `Performers`, `Label`, `CatalogNumber`, `Barcode`, `ISRC`, `MusicBrainz*` |
| FLAC | none |

Every writable format is gated in CI: a round-trip over the test corpus (audio bit-identical,
untouched tags unchanged), an external check of edited files with `ffprobe`, `ffmpeg` and
`flac -t`, and a fuzz target that applies random edits.

## Robustness

The parsers are written to read untrusted files:

- every read is bounds-checked against the file size;
- length fields from the file cannot drive allocations past the file size or a fixed
  256 MiB ceiling;
- a file records at most 100 warnings;
- a panic inside a parser is recovered and returned as a `*CorruptedFileError`;
- parse loops check the `context`, so `OpenContext` and `OpenMany` stop promptly;
- each parser has a fuzz target (`FuzzParseMP3`, `FuzzParseFLAC`, `FuzzParseM4A`, `FuzzParseOgg`).

## Examples

Runnable examples with verified output are rendered on
[pkg.go.dev](https://pkg.go.dev/github.com/simonhull/audiometa#pkg-examples) (source in
[`example_test.go`](example_test.go)). The [`examples/`](examples/) directory has complete
programs:

- **[basic](examples/basic/)**: Simple metadata reading
- **[batch](examples/batch/)**: Process multiple files concurrently
- **[artwork](examples/artwork/)**: Extract and save cover art
- **[chapters](examples/chapters/)**: Extract and display chapter markers

## Performance

Measured with `go test -run '^$' -bench 'BenchmarkParse|BenchmarkOpenFile' -benchmem -count 3 .`
on an AMD Ryzen 9 5950X (Linux, Go 1.27.3). The fixtures in `testdata/bench/` are
10-second audiobook files with a full tag set and 50 chapters. Medians of three runs:

| Fixture | `Open` (real file) | Parse only (in memory) | `ReadAt` calls | Memory / allocs per open |
|---------|-------------------:|-----------------------:|---------------:|-------------------------:|
| `book.flac` | 21 µs | 13 µs | 2 | 41 KB / 158 |
| `book.m4b`  | 29 µs | 19 µs | 3 | 68 KB / 542 |
| `book.mp3`  | 38 µs | 26 µs | 5 | 101 KB / 491 |
| `book.opus` | 56 µs | 46 µs | 3 | 105 KB / 445 |

Numbers depend on the machine and on the files, so treat them as an order of magnitude and
run the benchmarks on your own hardware. Artwork is not included: it is read only by
`ExtractArtwork`.

## Command-Line Tool

### audiometa

#### Installation

With Go 1.27 or newer:

```bash
go install github.com/simonhull/audiometa/cmd/audiometa@latest
```

Or download a prebuilt binary (linux, macOS, Windows; amd64 and arm64) from the
[GitHub releases page](https://github.com/simonhull/audiometa/releases), verify it
against `checksums.txt`, and put `audiometa` on your `PATH`. See
[RELEASING.md](RELEASING.md) for how releases are built.

#### Usage

```bash
audiometa song.flac                      # same as: audiometa inspect song.flac
audiometa inspect --json *.m4b           # one JSON object per file
audiometa chapters audiobook.m4b         # [1] 00:00:00.000 → 00:12:31.500  Chapter 1
audiometa artwork --out covers/ *.mp3    # extract embedded artwork (add --force to overwrite)
audiometa atoms audiobook.m4b            # dump the MP4 atom tree (debugging)
audiometa version
```

`--json` output is NDJSON: one object per file, in argument order, so it streams
over any number of files and pipes straight into `jq`. Objects carry a
`schema_version` field, durations are integer milliseconds in `*_ms` fields, and
a file that cannot be read yields an object with an `error` field. Flags must
come before file names; run `audiometa help` for details.

Exit codes: `0` success, `1` at least one file failed (or a write failed), `2`
usage error.

The former `atom-dump-tool` is now `audiometa atoms`.

## API Documentation

Full API documentation is available at [pkg.go.dev/github.com/simonhull/audiometa](https://pkg.go.dev/github.com/simonhull/audiometa).

## Requirements

Requires **Go 1.27+** (the `go` directive in `go.mod`). The only dependency is
`golang.org/x/sync`.

## Project Structure

```
audiometa/
├── *.go              # Public API (file.go, options.go, tags.go, ...)
├── example_test.go   # Runnable godoc examples
├── internal/         # Private implementation
│   ├── binary/       # Bounds-checked binary reading (SafeReader)
│   ├── registry/     # Format parser registry
│   ├── types/        # Shared data types (Tags, AudioInfo, Chapter, ...)
│   ├── flac/         # FLAC parser
│   ├── mp3/          # MP3 parser (ID3v2, ID3v1, MPEG frames)
│   ├── m4a/          # M4A/M4B parser
│   ├── ogg/          # Ogg Vorbis/Opus parser
│   ├── vorbis/       # Shared Vorbis comment and chapter parsing
│   └── imagemeta/    # Image type and dimension sniffing
├── cmd/audiometa/    # Command-line tool
├── examples/         # Example programs
├── testdata/         # Sample files and benchmark fixtures
└── docs/design/      # Design documents
```

## Architecture

- **Layered API**: `Open` for the common case; options, contexts and raw tags when you need them
- **Lazy loading**: artwork is read only when requested
- **Graceful degradation**: warnings instead of errors wherever partial data is still useful

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

# Install the pinned toolchain (Go, golangci-lint, stringer, goimports)
mise install

# Everything CI checks: build, race tests, lint, vet, gofmt
mise run check

# Run all tests (including the godoc examples)
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

# Fuzz a parser
go test -fuzz=FuzzParseMP3 ./internal/mp3

# Generate code (stringer for enums)
go generate ./...

# Format code
gofmt -s -w .
goimports -w .

# Run linters (uses the mise-pinned golangci-lint)
mise exec -- golangci-lint run ./...

# Install the CLI locally
go install ./cmd/audiometa

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
