# Changelog

All notable changes to this project are documented here.

## Unreleased

### Breaking-ish

- Tags are now reported exactly as stored in the file; no field is inferred from another. `Narrator` is no longer derived from the first `Composer` (MP3 and M4A), `Series` is no longer parsed out of `Grouping`, and `SeriesPart` is no longer guessed from `Grouping`, the title, the album or the file path. Apps that want these heuristics should derive them from `Tags.Grouping`, `Tags.Composers`, `Tags.Title`, `Tags.Album` and the path. Explicit tags (`NARRATOR`, `SERIES`, `SERIESPART`, TXXX descriptions, `MVNM`/`MVIN`, M4A freeform atoms) are unchanged.
- `SHOW`, `EPISODE_ID`, generic `PART` (Vorbis, ID3 `TXXX`) and the M4A freeform `Part` and `Volume` atoms no longer populate `Series`/`SeriesPart`, because they do not mean "series". They remain available through `Tags.Get`/`Tags.All` under their own keys.
- Minimum Go version is now 1.27.
- `cmd/atom-dump-tool` is replaced by `audiometa atoms`.
- `audiometa.Version` is now a deprecated variable (was a constant); use `GetVersion()`.

### Added

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
