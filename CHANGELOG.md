# Changelog

All notable changes to this project are documented here.

## Unreleased

### Breaking-ish

- Minimum Go version is now 1.27.
- `cmd/atom-dump-tool` is replaced by `audiometa atoms`.
- `audiometa.Version` is now a deprecated variable (was a constant); use `GetVersion()`.

### Added

- `audiometa` CLI with `inspect`, `chapters`, `artwork`, `atoms` and `version` subcommands.
- Tagged release binaries for linux, darwin and windows (amd64 and arm64).
- Real-file corpus, public API integration tests and parser fuzz targets.

### Fixed

- MP3: decode ID3 text correctly and prefer the real COMM frame; route GRP1/MVNM/MVIN frames; read CHAP titles with version-aware sizes.
- MP3: decode MPEG-2 frames with version-correct tables; probe the Xing header at the correct offset.
- M4A: parse duration and chapters when iTunes tags are absent; decode typed ilst atoms and map missing fields; read AAC profile from AudioSpecificConfig; honor compatible ftyp brands; read QuickTime chapter-track tables per spec and honor chpl version; avoid int64 overflow in mvhd duration.
- FLAC: fix CUESHEET reserved-byte skip; warn on unknown total samples and preserve all picture types; exclude metadata from bitrate; stop artwork scan at the last metadata block.
- FLAC and Ogg: fix chapters (cuesheet lead-out, CHAPTER comments, end times).
- Vorbis comment keys are matched case-insensitively.
- Opus: informational warnings no longer trip strict mode.
- Library version is derived from build info instead of a stale constant.

### Changed

- Series-parsing regexes are precompiled.
- Batch example handles partial failure correctly.

### Security

- MP4 atoms that overrun their container are rejected.

### Deprecated

- `audiometa.Version`; use `GetVersion()`.
