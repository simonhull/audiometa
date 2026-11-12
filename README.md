# audiometa

[![Go Reference](https://pkg.go.dev/badge/github.com/simonhull/audiometa.svg)](https://pkg.go.dev/github.com/simonhull/audiometa)
[![Go Report Card](https://goreportcard.com/badge/github.com/simonhull/audiometa)](https://goreportcard.com/report/github.com/simonhull/audiometa)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

audiometa is a fast, format-agnostic audio metadata library for Go. One call reads tags,
technical audio properties, chapters and embedded artwork from FLAC, MP3, M4A/M4B,
Ogg Vorbis, Opus, WAV and AIFF files, and it writes tags and artwork back to every one of
those formats, with first-class support for audiobooks.

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
- **📦 Format Support**: FLAC, MP3, M4A/M4B, Ogg Vorbis, Opus, WAV, AIFF
- **🔍 Complete Metadata**: tags, raw tags, technical info, artwork and chapters in every supported format
- **✍️ Writing**: edit tags and artwork with `Write` (verified atomic replace, or in place with `WithInPlace`) or stream an edited copy with `File.WriteTo`
- **🔐 Audio digest**: a SHA-256 of the audio alone that survives retagging and renames
- **🔊 ReplayGain**: track and album gain and peak from every format's tags, including Opus R128

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
// Works for FLAC, MP3, M4A, M4B, Ogg Vorbis, Opus, WAV and AIFF
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
| `*audiometa.UnsupportedFormatError` | unknown format, unreadable header, or a path that is not a regular file |
| `*audiometa.CorruptedFileError` | invalid container structure, or a recovered parser panic |
| `*audiometa.OutOfBoundsError` | a required read runs past the end of the file (truncation, corrupt length) |
| `context.Canceled` / `context.DeadlineExceeded` | the context passed to `OpenContext` / `OpenMany` is done |

### Open options

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

Write options are listed under [Write options](#write-options).

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
file.Audio.Container // "MP4", "Ogg", "FLAC", "MP3", "WAV", "RF64", "AIFF" or "AIFF-C"
file.Audio.ReplayGain // *ReplayGainInfo, nil if absent

// Audiobook-specific (filled only from explicit tags, never inferred)
file.Tags.Narrator   // NARRATOR (Vorbis), TXXX NARRATOR (ID3), "Narrator" atom (M4A)
file.Tags.Series     // SERIES / MVNM / SHOW (Vorbis), TXXX SERIES / frame MVNM (ID3), "Series" atom / ©mvn (M4A)
file.Tags.SeriesPart // SERIESPART / MVIN (Vorbis), TXXX SERIES PART / frame MVIN (ID3), "Series Part" atom / ©mvi (M4A)
file.Tags.Grouping   // GROUPING / GRP1 / ©grp, as stored

// Cataloging
// MP3 and M4A: read from the keys MusicBrainz Picard writes (TXXX/freeform "MusicBrainz Album Id", UFID, TSRC, TCOP, ...)
file.Tags.MusicBrainzTrackID  // MP3: UFID "http://musicbrainz.org"; M4A: "MusicBrainz Track Id"
file.Tags.MusicBrainzAlbumID
file.Tags.MusicBrainzArtistID
file.Tags.ISRC                // TSRC (MP3)
file.Tags.Barcode
file.Tags.CatalogNumber
file.Tags.Copyright           // TCOP (MP3), cprt (M4A)
file.Tags.Label               // M4A "LABEL"; MP3 stores it in TPUB, which fills Publisher
file.Tags.OriginalDate        // TDOR or TORY (MP3)
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
| MP3 | ID3v2 frame ID (`TIT2`, `TSSE`); TXXX frames under their upper-cased description; UFID frames under `UFID:` and their owner (`UFID:http://musicbrainz.org`) |
| M4A/M4B | ilst atom name (`"\xa9nam"` for ©nam, `aART`); freeform `----` atoms under their upper-cased name |

TXXX descriptions and freeform names keep the spelling the file has: `Tags.Name` reports it, so
a file Picard tagged gives `MusicBrainz Album Id` for the key `MUSICBRAINZ ALBUM ID`.

### Which tags a file has

`File.TagSources` lists the tag containers a file stores, so an editor does not have to parse the
file again to learn them:

```go
for _, s := range file.TagSources {
    fmt.Println(s.Kind, s.Version, s.Used) // e.g. "id3v2 4 true", then "id3v1 0 false"
}
```

Each `TagSource` has a `Kind` (`TagID3v2`, `TagID3v1`, `TagAPEv2`, `TagLyrics3`, `TagVorbisComment`,
`TagMP4`, `TagRIFFInfo`, `TagAIFFText`), the ID3v2 major `Version` (0 for other kinds) and `Used`:
whether audiometa read the container into `Tags`. The list is in the order the reader consults the
containers; those it does not read (APEv2, Lyrics3, an ID3v2 tag in front of a FLAC stream or after
the audio) come last with `Used` false. An ID3v1 trailer is used only when the ID3v2 tag left both
title and artist empty and the trailer is the last block of the file. `Used` is per container, not
per field. A file without tags has an empty list.

`CanWrite(format)` says whether audiometa has a tag writer for a format, and `File.Writable()`
whether the file itself would be refused by `Write` (DRM, fragmented MP4, a damaged layout),
without writing anything. It stops the writer at its first output byte, so it costs the metadata
and structure, not the audio. A nil result does not cover edit-specific failures such as
`ErrTagTooLarge`.

## Supported Formats

| Format      | Read | Write | Artwork | Chapters | Technical Info |
|-------------|------|-------|---------|----------|----------------|
| FLAC        | ✓    | ✓     | ✓       | ✓        | ✓              |
| MP3         | ✓    | ✓     | ✓       | ✓        | ✓              |
| M4A         | ✓    | ✓     | ✓       | ✓        | ✓              |
| M4B         | ✓    | ✓     | ✓       | ✓        | ✓              |
| Ogg Vorbis  | ✓    | ✓     | ✓       | ✓        | ✓              |
| Opus        | ✓    | ✓     | ✓       | ✓        | ✓              |
| WAV         | ✓    | ✓     | ✓       | ✓        | ✓              |
| AIFF        | ✓    | ✓     | ✓       | ✓        | ✓              |

Ogg Vorbis and Opus writing replaces the comment header and re-pages it; chained and multiplexed
files (more than one logical stream) are refused. WAV covers
RIFF, its big-endian twin RIFX, and RF64; AIFF covers AIFF and AIFF-C. Their artwork and
chapters come from the ID3v2 chunk. See [Writing metadata](#writing-metadata) for what writing does
and does not touch.

Tag formats:

- **FLAC**: Vorbis comments, PICTURE blocks (a non-standard ID3v2 tag in front of the stream is skipped with a warning, and kept on write)
- **WAV and AIFF**: an ID3v2 chunk (`id3 ` or `ID3 `), read as MP3's ID3v2 tag is, and the
  format's own text: WAV's `LIST`/`INFO` chunk (`INAM`, `IART`, `IPRD`, `ICRD`, `ITRK`, `ICMT`,
  `ISFT`, ...) and AIFF's `NAME`, `AUTH`, `(c) ` and `ANNO` chunks, each under its ID as a raw
  key. Where both hold a field, ID3 wins. Audio properties come from `fmt ` (PCM, IEEE float,
  `WAVE_FORMAT_EXTENSIBLE`, A-law, mu-law) or `COMM`. A last chunk cut short, a wrong form size
  and junk after the form are warnings, not failures.
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

Every supported format can be edited. `Write` opens the file, hands it to your
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

`WriteContext` and `File.WriteToContext` are the same calls bounded by a `context`: when it is
done they stop and return an error wrapping `ctx.Err()`, and the original file is untouched.
`Write` and `Open` refuse anything but a regular file.

### Write options

| Option | Effect |
|--------|--------|
| `WithInPlace()` | overwrite the metadata region of the file when the edit fits the space already reserved for it (for example ID3v2 or FLAC padding), instead of rewriting the whole file; a hint that falls back to the atomic rewrite when the edit does not fit, the format cannot patch in place, or the file is read-only; not crash-atomic (the audio is never touched), and a failed verification restores the overwritten bytes and the modification time |
| `WithID3v1(mode)` | what to do with an MP3's ID3v1 trailer: `ID3v1Keep` (the default) copies it, `ID3v1Update` rewrites an existing trailer from the edited tags, `ID3v1Remove` drops it; a trailer is never added; a write that only changes the trailer still happens, and `WithInPlace` falls back to the atomic rewrite for it; other formats ignore it |
| `WithPreserveModTime()` | keep the file's modification time instead of setting the time of the write |

### Write errors

Match these with `errors.Is`; they are always wrapped with detail.

| Error | When |
|-------|------|
| `ErrUnsupportedWrite` | an edit cannot be represented in the format, chapters were changed, or the file cannot be rewritten (a chained or multiplexed Ogg file, a fragmented MP4) |
| `ErrDRMProtected` | the file is encrypted or DRM-protected; such files are never rewritten |
| `ErrTagTooLarge` | the metadata would exceed a limit of the format (16 MiB for a FLAC metadata block, 256 MiB for an ID3v2 tag) |
| `ErrConflictingEdits` | a typed field and the raw key it is read from were changed to different values, `Artist` disagrees with the first of `Artists`, or `Year` disagrees with the year of `Date` |
| `ErrInvalidArtwork` | `AddArtwork` or `ReplaceArtwork` got an image with no data, an invalid type or description, or an undetectable MIME type |
| `ErrConcurrentModification` | the file changed on disk between `Write` opening it and replacing it |
| `ErrVerifyFailed` | the new file did not pass verification (it must parse, with identical audio and chapters); the original is left as it was |

`WriteContext`, `File.WriteToContext` and `File.AudioDigestContext` return errors wrapping
`ctx.Err()` (`context.Canceled` or `context.DeadlineExceeded`) when their context is done.

- **Only what you change is written.** audiometa never infers one field from another, on
  read or on write: only the keys and fields you set or delete change. Writing `Narrator`
  leaves the composer alone, and setting `Series` does not rewrite the grouping.
- **Safety.** The new file is written to a temporary file in the same directory, synced,
  verified (it must parse, with bit-identical audio and unchanged chapters) and renamed over
  the original. On any error the original is untouched. A crash can leave a temporary file named
  `.<name>.audiometa-<digits>.tmp` (the name cut to 100 bytes) next to the original; delete it.
- **Preservation.** Unknown frames, atoms and blocks, and every tag you did not edit, are kept
  byte for byte.
- **Chapters** are preserved but not editable: they stay as they are, and an edit that
  targets chapter data returns `ErrUnsupportedWrite`.
- **ID3.** A `Year` outside 1900-2100 is refused with `ErrUnsupportedWrite` (an ID3 tag cannot
  hold it, and it would read back as 0). v2.3 stays v2.3, v2.4 stays v2.4, and v2.2 or untagged files get v2.4. An existing
  ID3v1 trailer is copied verbatim by default. `WithID3v1(ID3v1Update)` rewrites it from the edited
  tags (30-byte Latin-1 fields; the genre is kept) and `WithID3v1(ID3v1Remove)` drops it; a trailer is
  never added. A file whose tags come from ID3v1 gets the ID3v1 title, artist, album, comment, year
  and track copied into its ID3v2 tag (a new ID3v2.4 tag when it had none), so no field disappears.
  v2.3 accepts the
  sort frames `TSOT`, `TSOA` and `TSOP`, as iTunes writes them; other v2.4-only frames are
  refused there.
- **Editing.** Change `f.Tags` fields directly, or use `Tags.Set` and `Tags.Delete` with
  format-native keys. Editing a field and its native key to different values returns
  `ErrConflictingEdits`. `Tags.Merge` fills the gaps in a `Tags` from another and never
  overwrites a value the file already has. An empty key returns `ErrUnsupportedWrite`, and so does an MP3 key
  naming an ID3v2 frame that is not a writable text frame (`APIC`, `COMM`, `USLT`, lower-case
  `tit2`), unless the file already has a `TXXX` of that description. Write lyrics through
  `Tags.Lyrics`: on MP3 that keeps the frame's language and description, and a new frame gets
  the language `XXX` (unknown).
- **Tag names.** A TXXX frame or freeform item that exists keeps its name as spelled, whatever
  case the key is given in. A new one is named by its key, or as `Tags.SetNamed` spells it:
  `SetNamed("MusicBrainz Album Id", id)` writes the name Picard and other taggers use. Setting
  the typed field (`Tags.MusicBrainzAlbumID`, `MusicBrainzArtistID`, `MusicBrainzTrackID` on M4A)
  names a new frame or item that way without `SetNamed`. Frames or items whose names differ only
  in case are read as one key, and writing it makes them one frame or item, named and placed as
  the first was. `UFID:<owner>` writes a UFID frame with one identifier of up to 64 bytes;
  `Tags.MusicBrainzTrackID` on MP3, WAV and AIFF is the `UFID:http://musicbrainz.org` frame, so
  a longer value is refused, not cut. Writing a typed MusicBrainz ID replaces every value of its
  key with the one value.
- **Release fields.** `OriginalDate` is written to `TDOR` on ID3v2.4 as it is, and to `TORY` on
  ID3v2.3, which holds only a year: the value's first four digits are stored, and a value
  without them returns `ErrUnsupportedWrite`. The frame of the other version is removed.
  `TPUB` is the ID3 Publisher frame, so it fills `Publisher` (after TXXX `PUBLISHER`), not
  `Label`.
- **WAV and AIFF.** Edits go to the ID3v2 chunk, which keeps its version; a file without one
  gets an ID3v2.4 chunk at the end. A raw key that is native text (an INFO ID, or an AIFF text
  chunk) is written there instead. Setting or clearing a typed field also rewrites the native
  text holding it (`INAM`, `IART`, `IPRD`, `IGNR`, `ICRD`, `ITRK`/`IPRT`, `ICOP`, `ICMT`; `NAME`,
  `AUTH`, `(c) `, `ANNO`) when the file has native text, so no reader sees a stale value. Every other
  chunk, the audio included, is copied byte for byte. A file whose chunks are cut short is
  refused with a `CorruptedFileError`.
- **Artwork.** `AddArtwork`, `ReplaceArtwork`, `RemoveArtwork` and `RemoveAllArtwork`.
  M4A cover art has no picture type (only front cover or other).
- **DRM.** Encrypted files (FairPlay, CENC, AAX, ID3 encrypted frames) return
  `ErrDRMProtected`.
- **Ogg Vorbis and Opus.** Edits go to the comment header (the `OpusTags` or Vorbis comment
  packet), which is laid out on fresh pages together with the unchanged Vorbis setup packet.
  Every audio page keeps its payload, granule position and flags byte for byte; when the header
  takes a different number of pages the later pages are renumbered and checksummed, nothing else.
  The vendor string and the data after the comments (Opus padding and binary data) are kept, and no
  padding is added. Pictures are `METADATA_BLOCK_PICTURE` comments. An edit that leaves the comment
  packet the same length (including Opus edits absorbed by existing padding) is a candidate for
  `WithInPlace`: only the header pages are rewritten, in place. A file that holds more than one
  logical stream (chained or multiplexed) returns `ErrUnsupportedWrite`.
- **Not writable:** fragmented MP4 and faststart M4A files whose chunk offsets would pass 4 GiB
  return `ErrUnsupportedWrite`.

Typed fields a format has no native place for return `ErrUnsupportedWrite`:

| Format | Typed fields that cannot be written |
|--------|-------------------------------------|
| MP3, WAV, AIFF | `Date`, `Performers`, `Label` (TPUB is `Publisher`) |
| M4A/M4B | `OriginalDate`, `Performers` |
| FLAC | none |

On ID3v2.3, `OriginalDate` is written to `TORY`, which holds only a year.

Every writable format is gated in CI: a round-trip over the test corpus (audio bit-identical,
untouched tags unchanged), an external check of edited files with `ffprobe`, `ffmpeg` and
`flac -t`, and fuzz targets that apply random edits (`FuzzWriteRoundTrip`, `FuzzWriteFLAC`).

## Robustness

The parsers are written to read untrusted files:

- every read is bounds-checked against the file size;
- length fields from the file cannot drive allocations past the file size or a fixed
  256 MiB ceiling;
- a file records at most 100 warnings;
- a file records at most 65,536 tag values, and an OverDrive MediaMarkers document is read
  only up to 1 MiB;
- a panic inside a parser is recovered and returned as a `*CorruptedFileError`;
- parse loops check the `context`, so `OpenContext` and `OpenMany` stop promptly;
- every parser, the writers, format detection, trailing-tag detection and the CLI atom walker
  have fuzz targets that check results and allocation budgets
  (`FuzzParseMP3`, `FuzzParseFLAC`, `FuzzParseM4A`, `FuzzParseOgg`, `FuzzParseIFF`,
  `FuzzWriteRoundTrip`, `FuzzWriteFLAC`, `FuzzDetectFormat`, `FuzzStart`, `FuzzWalkAtoms`),
  and CI fuzzes each on every push.

## Examples

Runnable examples with verified output are rendered on
[pkg.go.dev](https://pkg.go.dev/github.com/simonhull/audiometa#pkg-examples) (source in
[`example_test.go`](example_test.go) and [`example_write_test.go`](example_write_test.go)). The [`examples/`](examples/) directory has complete
programs:

- **[basic](examples/basic/)**: Simple metadata reading
- **[batch](examples/batch/)**: Process multiple files concurrently
- **[artwork](examples/artwork/)**: Extract and save cover art
- **[chapters](examples/chapters/)**: Extract and display chapter markers

## Performance

Measured with `go test -run '^$' -bench 'BenchmarkParse|BenchmarkOpenFile' -benchmem -count 6 .`
on an AMD Ryzen 9 5950X (Linux, Go 1.27.3). The fixtures in `testdata/bench/` are
10-second audiobook files with a full tag set and 50 chapters. Medians of six runs. The `Open` and memory columns come from `BenchmarkOpenFile`;
the parse-only and `ReadAt` columns come from `BenchmarkParse`:

| Fixture | `Open` (real file) | Parse only (in memory) | `ReadAt` calls | Memory / allocs per open |
|---------|-------------------:|-----------------------:|---------------:|-------------------------:|
| `book.flac` | 19 µs | 12 µs | 1 | 39 KiB / 108 |
| `book.m4b`  | 26 µs | 19 µs | 1 | 45 KiB / 543 |
| `book.mp3`  | 39 µs | 30 µs | 4 | 98 KiB / 440 |
| `book.opus` | 58 µs | 50 µs | 1 | 86 KiB / 440 |

The fixtures carry modest tags, so a file with larger tags can need more `ReadAt` calls.
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
├── example_test.go   # Runnable godoc examples for reading
├── example_write_test.go # Runnable godoc examples for writing
├── internal/         # Private implementation
│   ├── atomicfile/   # Atomic temp-file replace
│   ├── binary/       # Bounds-checked binary reading (SafeReader)
│   ├── registry/     # Format parser registry
│   ├── types/        # Shared data types (Tags, AudioInfo, Chapter, ...)
│   ├── flac/         # FLAC parser and writer
│   ├── mp3/          # MP3 parser and writer (ID3v2, ID3v1, MPEG frames)
│   ├── iff/          # WAV and AIFF parser and writer
│   ├── m4a/          # M4A/M4B parser and writer
│   ├── ogg/          # Ogg Vorbis/Opus parser and writer
│   ├── vorbis/       # Shared Vorbis comment parsing, editing and chapters
│   ├── imagemeta/    # Image type and dimension sniffing
│   ├── picture/      # FLAC PICTURE block encoding and decoding
│   ├── replaygain/   # ReplayGain values from raw tags
│   ├── trailer/      # Trailing APEv2, Lyrics3 and ID3v1 tag detection
│   ├── writeutil/    # Helpers shared by the format writers
│   ├── testbudget/   # Allocation budgets for tests and fuzz targets
│   ├── testcorpus/   # Test corpus generator scripts
│   └── testctx/      # Contexts for testing cancellation
├── cmd/audiometa/    # Command-line tool
├── examples/         # Example programs
└── testdata/         # Sample files and benchmark fixtures
```

## Architecture

- **Layered API**: `Open` for the common case; options, contexts and raw tags when you need them
- **Lazy loading**: artwork is read only when requested
- **Graceful degradation**: warnings instead of errors wherever partial data is still useful

### Format Parser Registration

All supported format parsers (FLAC, MP3, M4A, M4B, Ogg Vorbis, Opus, WAV, AIFF) are automatically
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

# Install the pinned toolchain (Go, golangci-lint, stringer, goimports, govulncheck, goreleaser)
mise install

# Fast gate: build, race and 32-bit tests, lint, vet, gofmt, tidy, generated code
mise run check

# Everything CI runs (needs network, ffmpeg, ffprobe and flac; CI also installs ogginfo and opusinfo)
mise run ci

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
scripts/fuzz.sh 30s   # every target

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
