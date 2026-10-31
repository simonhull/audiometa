// Package audiometa reads metadata from audio files through one
// format-agnostic API.
//
// It parses tags, technical audio properties, chapters and embedded artwork
// from FLAC, MP3, M4A/M4B, Ogg Vorbis and Opus files. The format is detected
// from the file's contents, so callers never pick a parser. [File.WriteTo] and
// [Write] save edited tags and artwork; MP3 and FLAC are currently the
// writable formats.
//
// # Quick Start
//
// [Open] detects the format, parses the metadata and returns a [File]:
//
//	file, err := audiometa.Open("song.flac")
//	if err != nil {
//		log.Fatal(err)
//	}
//	defer file.Close()
//
//	fmt.Printf("%s - %s\n", file.Tags.Artist, file.Tags.Title)
//	fmt.Println(file.Audio.Duration) // e.g. 3m25s
//	fmt.Println(file.Audio)          // e.g. FLAC 44.1kHz 16-bit stereo lossless
//
// A [File] carries the parsed data in its embedded fields:
//
//   - Tags: format-agnostic fields such as Title, Artist, Album, Narrator and
//     Series, plus the multi-value Artists, Genres and Composers.
//     Tags.Get and Tags.All expose every raw tag under its native key.
//     Tags are reported as stored: Narrator, Series and SeriesPart come only
//     from explicit tags and are never inferred from Composers, Grouping, the
//     title, the album or the file path.
//   - Audio: an [AudioInfo] with duration, sample rate, bit depth, channels,
//     bitrate, codec and ReplayGain.
//   - Chapters: a slice of [Chapter]; each records the [ChapterSource] it was
//     read from.
//   - Warnings: non-fatal problems found while parsing, as [Warning] values.
//   - Format, Path and Size.
//
// # Supported Formats
//
//   - FLAC: Vorbis comments, PICTURE blocks, and chapters from CHAPTER
//     comments or the CUESHEET block. Writing rewrites only the edited
//     comments and pictures; every other metadata block and the vendor string
//     are kept byte for byte, and the PADDING block absorbs growth so the
//     audio usually stays in place. CHAPTER comments and the CUESHEET are
//     never edited.
//   - MP3: ID3v2.2, ID3v2.3 and ID3v2.4 tags with an ID3v1 fallback, APIC
//     artwork, chapters from CHAP frames or OverDrive MediaMarkers, and
//     Xing/VBRI-aware duration. Writing keeps the ID3v2 version (ID3v2.2 tags
//     and untagged files get ID3v2.4), re-emits untouched frames byte for byte,
//     and never edits chapters. An ID3v1 trailer is copied unchanged, never
//     updated from ID3v2 edits, so after deleting TIT2 a reader that falls
//     back to ID3v1 can show the stale ID3v1 title (audiometa itself does so
//     when both TIT2 and TPE1 are gone).
//   - M4A/M4B: iTunes ilst atoms including freeform (----) atoms, covr
//     artwork, and QuickTime or Nero chapters.
//   - Ogg Vorbis and Opus: Vorbis comments, METADATA_BLOCK_PICTURE artwork
//     and CHAPTER comments.
//
// WAV and AIFF are recognized by [DetectFormat] but have no parser, so [Open]
// returns an [*UnsupportedFormatError] for them.
//
// # Artwork
//
// Artwork is not read during [Open]. [File.ExtractArtwork] reads it on first
// use and caches the result. [WithArtworkPreload] loads it during Open instead,
// and [WithMaxArtworkSize] drops oversized images.
//
// # Warnings and Errors
//
// audiometa degrades gracefully. Damaged but readable data, such as a bad tag
// frame or an unreadable chapter table, becomes a [Warning] in File.Warnings
// and parsing continues. Open returns an error only when no useful result is
// possible:
//
//   - the file cannot be opened (an [*os.PathError] wrapping, for example,
//     [io/fs.ErrNotExist]);
//   - the format is not recognized or has no parser ([*UnsupportedFormatError]);
//   - the container is structurally broken (often a [*CorruptedFileError], or
//     an [*OutOfBoundsError] when a required read runs past the end of the
//     file);
//   - the context is done ([context.Canceled] or [context.DeadlineExceeded],
//     from [OpenContext] and [OpenMany]).
//
// Errors are wrapped, so match them with [errors.Is] and [errors.As] (or
// [errors.AsType]).
// [WithStrictParsing] turns any warning into an error that unwraps to the
// warnings' underlying errors; [WithIgnoreWarnings] discards warnings.
//
// # Robustness
//
// The parsers are written for untrusted input and have fuzz tests:
//
//   - every read is bounds-checked against the file size;
//   - length fields read from the file cannot trigger allocations larger than
//     the file itself or a fixed ceiling;
//   - a file accumulates at most 100 warnings;
//   - a panic inside a parser is recovered and returned as a
//     [*CorruptedFileError] instead of crashing the program;
//   - parse loops check the context, so [OpenContext] and [OpenMany] stop
//     promptly when it is canceled.
//
// # Concurrency
//
// Distinct [File] values are independent, and [OpenMany] parses files in
// parallel. [File.ExtractArtwork] is safe for concurrent use. The exported
// fields are plain data and are not synchronized: do not modify a File while
// other goroutines read it.
//
// # Command-Line Tool
//
// The audiometa command (cmd/audiometa) is built on this package. It prints
// metadata and chapters, extracts artwork and dumps MP4 atom trees:
//
//	go install github.com/simonhull/audiometa/cmd/audiometa@latest
package audiometa
