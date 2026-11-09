# Changelog

All notable changes to this project are documented here.

## Unreleased

### Changed

- `Write` with `WithInPlace` falls back to the atomic rewrite when the file's permissions do not allow opening it for writing, as it already did when the edit does not fit. It used to fail with a permission error, although the same `Write` without the option replaces the file.
- FLAC: a metadata block chain that ends before a block flagged as last now always adds a `StageMetadata` warning, so `Open` fails on it under `WithStrictParsing`. That used to happen only when the file ended inside a block header; a chain that stopped exactly at the end of the file, or whose last block ran past it, was accepted without a warning. A header cut short by the end of the file still wraps an `*OutOfBoundsError`.
- `DetectFormat` reports an ID3v2 tag followed by `fLaC` as `FormatFLAC`.

### Fixed

- M4A/M4B: writing refuses, with `ErrUnsupportedWrite`, a file whose `stco` or `co64` chunk offsets point into `moov` or the `free`/`skip` atoms right after it, whatever the size of the edit. The check used to run only when `moov` outgrew its space; a smaller edit overwrote those bytes, and an edit to a file ending in `moov` dropped the free atoms after it, without an error. A chunk-offset table whose entry count does not fit its atom is now refused in every case too.
- FLAC: a FLAC file with an ID3v2 tag in front of the stream, which some taggers write and libFLAC, `metaflac` and ffmpeg accept, is read as FLAC instead of MP3: its Vorbis comments, pictures and audio properties are read, and a `StageMetadata` warning notes the ID3v2 tag, whose frames are ignored (so `WithStrictParsing` rejects the file). Writing edits the Vorbis comments and keeps the ID3v2 tag byte for byte, in place or not, and `AudioDigest` is the same as without the tag. Such files used to open as MP3 with wrong or missing audio properties, and writing edited the ID3v2 tag, which FLAC players ignore.
- MP3, WAV and AIFF: `ExtractArtwork` now reads ID3v2 pictures the way `Open` reads every other frame. It undoes unsynchronisation (of the whole tag in ID3v2.2 and ID3v2.3, per frame in ID3v2.4), strips an ID3v2.4 data-length indicator or group byte, skips compressed and encrypted pictures and every picture of a compressed ID3v2.2 tag, steps over empty frames instead of stopping at them, and checks a frame's size against the file before allocating it. Pictures in such tags used to come back corrupted or as garbage, or not at all, and a crafted file of a few bytes could make it allocate 100 MB.
- FLAC: Vorbis comments and pictures are read only from inside their own metadata block. A length field that ran past the end of its block used to be followed into the next blocks or the audio, so a crafted file could make `Open` take seconds and gigabytes of memory and make `ExtractArtwork` return audio bytes as images. Now the comments decoded before such a length are kept and the block adds a `StageMetadata` warning saying the length runs past the end of the block; such a picture is skipped.
- WAV and AIFF: writing a file with many chunks is no longer quadratic. A WAV of 65,000 small chunks took over 30 seconds to write and verify; it now takes well under a second.
- WAV (RF64): a ds64 chunk whose table claims more entries than the file holds, or more than 1024, is refused with a CorruptedFileError at once. It used to be read entry by entry to the end of the file, taking seconds and hundreds of megabytes on a crafted file, and then fail with an OutOfBoundsError.
- WAV (RF64): writing a file whose ds64 chunk has an odd size keeps the chunk's pad byte. The output used to lose it, shifting every later chunk, so WriteTo returned a file no reader could parse and Write failed verification.
- WAV and AIFF: a sample rate, bit rate or duration too large to report is left at 0 with a StageTechnical warning (so Open fails on it under WithStrictParsing) instead of overflowing. A crafted fmt chunk gave a negative bit rate, and on 32-bit platforms a sample rate above 2,147,483,647 Hz read as negative.
- `Open`, `OpenContext`, `OpenMany` and `Write` refuse a path that is not a regular file (a FIFO, socket, device or directory) with an `*UnsupportedFormatError` whose Reason is "not a regular file", before reading it. On a FIFO without a writer they used to block forever, whatever the context; a directory or device used to fail with "failed to read file header".
- MP3: writing a file whose tags come from its ID3v1 trailer keeps them. The new ID3v2 tag now holds every value the reader took from ID3v1 (title, artist, album, year, comment, track) unless the edit changed it. Writing used to keep only the edited fields, so an edit of the title alone left the artist and album empty.
- Ogg: when a page does not continue the header packet the previous page left open, `AudioDigest` now ends that packet there, as `Open` already did. It used to count the next page's first packet as part of the headers and leave it out of the digest, so the digest of such a damaged file changes.
- CLI: audiometa artwork --force no longer writes through a symlink at the destination. The image goes to a temporary file in the output directory that is renamed over the destination, so a link there is replaced and its target left alone. Two inputs whose pictures would get the same output name (a/song.mp3 and b/song.flac both make song.jpg) now fail for the second input instead of the second picture silently replacing the first under --force.
- `Write`, `File.WriteTo`, `File.AudioDigest` and `File.ExtractArtwork` no longer use the exported `File.Size`, `File.Format` and `File.Path` fields, which callers can change: they use the values `Open` found. An edit callback that changed `f.Size` used to make `Write` replace the file with a truncated copy and report success, and changing `f.Format` sent the file to another format's writer.
- `Write` no longer fails with "file name too long" on files whose names are longer than about 229 bytes. Its temporary file is named after at most the first 100 bytes of the original's name (`.<name>.audiometa-<digits>.tmp`), which the `Write` documentation now states so leftovers from a crash can be found.
- WAV and AIFF: writing no longer rewrites native text it does not change. An edit that touched only the ID3v2 chunk used to re-encode the whole LIST/INFO chunk or the AIFF text chunks: Latin-1 text became UTF-8, anything after a value's first NUL was dropped, and every INFO subchunk after an unreadable one was lost, with no error. Native text is now copied byte for byte unless an edit changes it; when one does, the entries it leaves alone keep their stored bytes. A write that has to change a LIST/INFO chunk holding an unreadable subchunk is refused with a CorruptedFileError.
- `Write` with `WithInPlace`: when verification fails and the original bytes are written back, the file's modification time is restored too, so the file is left as it was. It used to keep the time of the failed write.
- `Write` returns `ErrConcurrentModification` when the file was replaced by another file while it ran (for example another program's atomic save), or, on Linux and macOS, rewritten without changing its size and with its modification time put back, as another `Write` with `WithInPlace` and `WithPreserveModTime` does. Both used to go unnoticed, and the other program's change was overwritten. A program that only changes the file's permissions, owner or extended attributes during the Write now also causes the error.

## v0.14.0 — 2026-11-08

### Added

- `Tags.Name` reports how a TXXX description or M4A freeform name is spelled in the file: `MusicBrainz Album Id` for the raw key `MUSICBRAINZ ALBUM ID` of a file MusicBrainz Picard tagged. Raw keys stay upper-cased, so every lookup that works today still does.
- `Tags.SetNamed` sets a tag by its name and spells a new TXXX frame or freeform item that way, so `SetNamed("MusicBrainz Album Id", id)` writes the mixed-case names Picard writes, which some readers (beets' MP4 freeform lookup among them) match case-sensitively. A tag that exists keeps its spelling. `Tags.AddRawNamed` is its counterpart for parsers.
- MP3, WAV and AIFF: `UFID` frames are read and written under the raw key `UFID:<owner>`, so the MusicBrainz recording ID is `UFID:http://musicbrainz.org`, where Picard stores it. A UFID whose identifier is not text is kept but not reported. Writing takes one identifier of 1 to 64 bytes and a printable ASCII owner, replaces that owner's frame in place and leaves other owners' frames alone. ID3v2.2 `UFI` frames are read too.

### Changed

- Writing a raw key whose TXXX frames or freeform items differ only in case (`MUSICBRAINZ ALBUM ID` and `MusicBrainz Album Id`) makes them one frame or item, named as the first in the file and put where it was. It already did; it is now documented and tested.
- `Tags.Equal` compares how names are spelled, and `Tags.Clone` and `Tags.Merge` carry the spelling.
- MP3: the raw key `UFID:<owner>` writes a UFID frame. It used to write a TXXX frame of that description, which reads back under its upper-cased key.

## v0.13.0 — 2026-11-08

### Added

- WAV and AIFF files are read and written. WAV covers RIFF, RIFX and RF64 (the 64-bit sizes of its `ds64` chunk); AIFF covers AIFF and AIFF-C. Audio properties come from `fmt ` (PCM, IEEE float, `WAVE_FORMAT_EXTENSIBLE`, A-law, mu-law; a `fact` chunk for compressed formats) or `COMM`. Tags come from an ID3v2 chunk (`id3 ` or `ID3 `), with its artwork and chapters, and from the native text: WAV `LIST`/`INFO` subchunks and AIFF `NAME`, `AUTH`, `(c) ` and `ANNO` chunks, kept under their IDs as raw keys. ID3 wins where both hold a field. Chunk walking honours pad bytes; a last chunk cut short, a wrong or unset form size, chunks after the form's end and junk after it are warnings.
- Writing WAV and AIFF: edits go to the ID3v2 chunk (an ID3v2.4 chunk is appended to a file without one), raw keys that are native text go to the native text, and a typed-field edit rewrites the native text holding that field when the file has some. Every other chunk is copied byte for byte. A file whose chunks are cut short is refused with a `CorruptedFileError`.
- `File.AudioDigest` for WAV (the `data` chunk) and AIFF (the `SSND` samples).

### Changed

- `DetectFormat` reports RIFX and RF64 files as `FormatWAV`.
- `Open` no longer returns an `UnsupportedFormatError` for WAV and AIFF files.

## v0.12.2 — 2026-11-05

### Added

- MP3: `USLT` lyrics are read into `Tags.Lyrics` (the first frame with text) and the raw key `USLT` (every text), including ID3v2.2 `ULT`. `Tags.Lyrics` is writable: it rewrites that one frame and keeps its language and description, a new frame gets the language `XXX` (unknown), and other `USLT` frames stay. Clearing `Tags.Lyrics` or deleting the raw key removes every `USLT` frame. A compressed `USLT` frame is skipped with a `StageMetadata` warning, like any other compressed frame, so `Open` fails on it under `WithStrictParsing`.
- MP3: `TSOT`, `TSOA` and `TSOP` can be written to ID3v2.3 tags, as iTunes and other taggers do. Other ID3v2.4-only frames are still refused there.

### Changed

- `Tags.Get` on a multi-valued MP3 `TXXX` frame or M4A freeform item now returns every value. It used to return only the first value of a `TXXX` frame and only the last of a freeform item. A typed field filled from a freeform item now takes the first value, not the last, so `Narrator`, `Series` and the others, and `Tags.GetFirst` and ReplayGain, change on an M4A file whose item holds several values.
- MP3: writing refuses, with `ErrUnsupportedWrite`, a raw key that names an ID3v2 frame other than a writable text frame (`USLT`, `COMM`, `APIC`, `UFID`, `PRIV`, `POPM`, `WOAR` and the like). Frame IDs are upper-case, so `text` or `user` are still ordinary keys, unless the file already has a `TXXX` frame of that description. These keys used to write a `TXXX` frame of that name without complaint. Other custom keys, including four-letter ones such as `MOOD` and `ASIN`, still write as `TXXX`.

### Fixed

- MP3: an empty raw key returns `ErrUnsupportedWrite`, as it does on FLAC and M4A, instead of writing a `TXXX` frame no reader reports.

## v0.12.1 — 2026-11-05

### Fixed

- M4A: a `©mvi` integer stored with a text or implicit data type now reads as its number, not as a control character (#3).

## v0.12.0 — 2026-11-01

### Added

- `File.AudioDigest` and `File.AudioDigestContext`: a SHA-256 of a file's audio data that metadata edits never change, for MP3, FLAC, M4A/M4B, Ogg Vorbis and Opus. Trailing ID3v1, APEv2, Lyrics3v2 and footer-terminated ID3v2 tags on MP3 and FLAC are left out, so tools such as mp3gain do not change it.
- ReplayGain is read from MP3 TXXX frames, M4A freeform items and Opus R128 gain tags, not only Vorbis comments.

### Changed

- A ReplayGain value that does not parse is reported as a `StageMetadata` warning (so `Open` fails under `WithStrictParsing`) and left unset, instead of being read as 0. `AudioInfo.ReplayGain` is nil, not a zeroed struct, when no value parses.

### Fixed

- The FLAC write verifier reported any read failure on the first four bytes as "invalid FLAC magic bytes"; the read error is now returned.

## v0.11.0 — 2026-10-31

### Breaking-ish

- Tags are now reported exactly as stored in the file; no field is inferred from another. `Narrator` is no longer derived from the first `Composer` (MP3 and M4A), `Series` is no longer parsed out of `Grouping`, and `SeriesPart` is no longer guessed from `Grouping`, the title, the album or the file path. Apps that want these heuristics should derive them from `Tags.Grouping`, `Tags.Composers`, `Tags.Title`, `Tags.Album` and the path. Explicit tags (`NARRATOR`, `SERIES`, `SERIESPART`, TXXX descriptions, `MVNM`/`MVIN`, M4A freeform atoms) are unchanged.
- `SHOW`, `EPISODE_ID`, generic `PART` (Vorbis, ID3 `TXXX`) and the M4A freeform `Part` and `Volume` atoms no longer populate `Series`/`SeriesPart`, because they do not mean "series". They remain available through `Tags.Get`/`Tags.All` under their own keys.
- Minimum Go version is now 1.27.
- `cmd/atom-dump-tool` is replaced by `audiometa atoms`.
- `audiometa.Version` is now a deprecated variable (was a constant); use `GetVersion()`.

### Added

- Write API: `File.WriteTo`, `File.WriteToContext`, `Write`, `WriteContext`, `WithInPlace` and `WithPreserveModTime`; artwork editing with `File.AddArtwork`, `ReplaceArtwork`, `RemoveArtwork` and `RemoveAllArtwork`; sentinel errors `ErrUnsupportedWrite`, `ErrDRMProtected`, `ErrTagTooLarge`, `ErrConflictingEdits`, `ErrInvalidArtwork`, `ErrConcurrentModification` and `ErrVerifyFailed`. Writing is supported for MP3, FLAC and M4A/M4B; Ogg Vorbis and Opus return `ErrUnsupportedWrite`. The write API follows the same compatibility promise as the read API.
- CI `write-gate` job: corpus round-trip, `ffprobe`/`ffmpeg`/`flac -t` oracle checks of edited files, and the `FuzzWriteRoundTrip` fuzz target.
- MP3 writing through `File.WriteTo` and `Write`: edited keys, typed fields and APIC artwork are rewritten; every other ID3v2 frame (including CHAP/CTOC chapters, which are not editable) is preserved byte for byte, the ID3v2 version is kept (ID3v2.2 and untagged files get ID3v2.4), and `WithInPlace` works when the new tag fits in the old one. The ID3v1 trailer is copied verbatim and never updated, so after deleting `TIT2` a stale ID3v1 title can show through in readers that fall back to ID3v1 (audiometa does when both title and artist are gone).
- FLAC writing through `File.WriteTo` and `Write`: edited Vorbis comments and PICTURE artwork are rewritten; every other metadata block (STREAMINFO, SEEKTABLE, APPLICATION, CUESHEET, unknown types) and the vendor string are preserved byte for byte, the PADDING block absorbs growth (when it runs out, 8192 bytes of fresh padding are added), and `WithInPlace` works when the edit fits in the existing padding. `CHAPTERnnn` comments and the CUESHEET are not editable.
- M4A/M4B writing through `File.WriteTo` and `Write`: only the edited `ilst` items (one `data` atom per value; freeform `----` items under `com.apple.iTunes`) and `covr` change, and every other atom, including the `chpl` and QuickTime chapter track (not editable), is re-emitted byte for byte. The new `moov` reuses adjacent `free` space when it fits (then `WithInPlace` works); otherwise the audio moves and every `stco`/`co64` chunk offset is shifted. An `stco` offset that would pass 4 GiB, fragmented MP4 and DRM-protected tracks (FairPlay, CENC, AAX) are refused. Freeform keys must be upper-case, as the reader reports them.
- M4A: read every `data` atom of a multi-value `ilst` item (`©ART`, `©gen`, `©wrt` fill `Artists`, `Genres`, `Composers`), and record a `gnre` item as raw key `gnre` holding its ID3v1 genre index (it is not mapped into `Genres`).
- `audiometa` CLI with `inspect`, `chapters`, `artwork`, `atoms` and `version` subcommands.
- Tagged release binaries for linux, darwin and windows (amd64 and arm64).
- MP3: read ID3v2.2 tags; fall back to an ID3v1 tag when ID3v2 gives no title or artist.
- MP3: read chapters from the OverDrive MediaMarkers `TXXX` frame when there are no `CHAP` frames.
- MP3 and M4A: populate raw tags (`Tags.Get`/`Tags.All`) and `Tags.Artists`.
- `Chapter.Source` and the `ChapterSource*` constants record which structure each chapter came from (also in the CLI's JSON output).
- `StageMetadata`, `StageTechnical`, `StageChapters` and `StageArtwork` constants for `Warning.Stage`.
- Strict-parsing errors unwrap to the underlying errors of every warning, so `errors.Is`/`errors.As` work on them.
- Reads past the end of a file now surface as a typed `*OutOfBoundsError` that `errors.As` can match.
- Runnable godoc examples (`example_test.go`) covering `Open`, `OpenContext`, `OpenMany`, artwork, chapters, strict parsing, typed errors and raw tags; rewritten package and API documentation.
- Real-file corpus, public API integration tests and parser fuzz targets.

### Fixed

- Vorbis (FLAC, Ogg, Opus): a typed field with several candidate keys is now filled from the highest-precedence key present, matching the writers and `Set`/`Delete`, instead of whichever key came first in the file. Affects `TrackTotal` (`TRACKTOTAL` over `TOTALTRACKS`), `DiscTotal` (`DISCTOTAL` over `TOTALDISCS`), `Series` (`SERIES` over `MVNM`), `SeriesPart` (`SERIESPART`, `SERIES-PART`, `SERIES_PART`, `SERIES PART`, then `MVIN`), `ASIN` (`ASIN` over `AUDIBLE_ASIN`) and `Language` (`LANGUAGE` over `LANG`). A file holding both keys no longer reads a stale value after a write. MP3 and M4A already followed the precedence.
- Vorbis, M4A and MP3: single-valued fields (`Artist`, `Title`, `Album`, ...) take the first occurrence of a repeated tag instead of the last, so `Artist` is the first value and `Artists` lists all values in file order in every format.
- MP3: decode ID3 text correctly and prefer the real COMM frame; route GRP1/MVNM/MVIN frames; read CHAP titles with version-aware sizes.
- MP3: decode MPEG-2 frames with version-correct tables; probe the Xing header at the correct offset.
- M4A: parse duration and chapters when iTunes tags are absent; decode typed ilst atoms and map missing fields; read AAC profile from AudioSpecificConfig; honor compatible ftyp brands; read QuickTime chapter-track tables per spec and honor chpl version; avoid int64 overflow in mvhd duration.
- FLAC: fix CUESHEET reserved-byte skip; warn on unknown total samples and preserve all picture types; exclude metadata from bitrate; stop artwork scan at the last metadata block.
- FLAC and Ogg: fix chapters (cuesheet lead-out, CHAPTER comments, end times).
- Vorbis comment keys are matched case-insensitively.
- Opus: informational warnings no longer trip strict mode.
- MP3: undo ID3v2 unsynchronisation, honor frame flags, and skip compressed or encrypted frames with a warning.
- Ogg: validate page headers, split packets on lacing values, and read headers that span several pages.
- M4A: read technical info from the sound track and honor the sample entry version.
- `WithMaxArtworkSize` is now enforced: oversized images are dropped from `ExtractArtwork` with a warning.
- `WithStrictParsing` and `WithIgnoreWarnings` now also apply to warnings from `WithArtworkPreload`.
- Hostile size and time fields can no longer wrap integers: FLAC cue offsets, M4A chpl start times, chapter time sums, 64-bit chunk offsets and extended atom sizes are bounds-checked (gosec G115 enabled).
- Tests now also pass on 32-bit targets (`GOARCH=386`), and CI runs them.
- `ExtractArtwork` is safe for concurrent use, and a failed extraction is retried instead of cached.
- `AudioInfo.ApplyReplayGain`: correct clipping limit, and no limit when the peak is unknown.
- Library version is derived from build info instead of a stale constant.

### Changed

- `Tags.Set` on tags returned by `Open` now also updates the corresponding typed field (for example `Set("TIT2", ...)` updates `Title`). Tags built by the caller are unaffected.
- Faster parsing with far fewer syscalls: `SafeReader` buffers small reads, MP3 skips artwork payloads during `Open`, and the MP3 frame-sync scan is bounded.
- Parse loops check the context, so `OpenContext` and `OpenMany` stop promptly on large files.
- Series-parsing regexes are precompiled.
- Batch example handles partial failure correctly.
- README and godoc corrected to match current behavior (formats, chapter sources, options, errors, measured benchmarks).

### Security

- MP4 atoms that overrun their container are rejected.
- Allocations driven by length fields are capped by the file size and a 256 MiB ceiling.
- Parser panics are recovered and returned as `*CorruptedFileError`; a file records at most 100 warnings.

### Deprecated

- `audiometa.Version`; use `GetVersion()`.
- `RawTag`, `RawTagType` and the `RawTag*` constants (never produced); use `Tags.Get` and `Tags.All`.
- `RegisterParser`; parsers register themselves.
- `AudioInfo.IsModernAudiobookCodec`; inspect `Codec` and `CodecProfile` instead.
