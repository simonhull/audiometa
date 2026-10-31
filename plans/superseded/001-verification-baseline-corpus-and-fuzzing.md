# Plan 001: Establish a real-file test corpus, fuzz targets, and malformed-input regression tests

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `plans/README.md`.
>
> **Drift check (run first)**: `git diff --stat 7b22897..HEAD -- internal/ testdata/ file_bench_test.go audiometa_test.go .github/workflows/ci.yml`
> If any in-scope file changed since this plan was written, compare the
> "Current state" excerpts against the live code before proceeding; on a
> mismatch, treat it as a STOP condition.

## Status

- **Priority**: P1
- **Effort**: L
- **Risk**: LOW (additive tests only — no production code changes)
- **Depends on**: none
- **Category**: tests
- **Planned at**: commit `7b22897`, 2026-07-01

## Why this matters

audiometa is a library whose entire job is parsing untrusted binary audio files, yet it has **zero fuzz targets**, **no `testdata/` corpus of real encoder output**, and 47.4% statement coverage concentrated on happy paths. Every fixture is built in-memory by hand, encoding the same assumptions the parser makes — e.g. the Ogg tests write page CRCs as `uint32(0)` and pass, because the parser never validates CRCs. Several confirmed bugs (unbounded allocations from crafted length fields, wrong MPEG-2 duration math) would have been caught by fuzzing or a real corpus. This plan is the safety net that plans 002, 004, 008, 011, 012, 015, and 016 verify against — it must land first.

## Current state

- `go test ./...` passes for all packages; CI (`.github/workflows/ci.yml:34`) runs `go test -v -race -coverprofile=coverage.txt -covermode=atomic ./...`.
- **No `testdata/` directory exists anywhere** (`find . -name testdata -type d` → empty).
- **No fuzz targets**: `grep -rn "testing.F" --include="*.go" .` → no matches.
- Existing synthetic fixture builders to reuse as fuzz seeds:
  - `internal/flac/parser_test.go` — `createMinimalFLAC` (~line 15)
  - `internal/mp3/parser_test.go` — `createMinimalMP3WithID3` (~line 99)
  - `internal/ogg/parser_test.go` — `createMinimalOgg` (~line 15)
  - `internal/m4a/chapters_test.go`, `internal/m4a/metadata_test.go` — moov/ilst builders
  - Root: `file_cancel_test.go:17` — `createTestM4BFile(t)` writes a minimal ftyp+moov file to a temp path
- Parser entry points fuzzing will target (all take `(ctx, io.ReaderAt, size, path)`):
  - `internal/flac/parser.go:34` — `func (p *parser) Parse(ctx context.Context, r io.ReaderAt, size int64, path string) (*types.File, error)`
  - `internal/mp3/parser.go:17` — same signature
  - `internal/m4a/parser.go:57` — same signature
  - `internal/ogg/parser.go:25` — same signature
  - Artwork: `internal/mp3/parser.go:65`, `internal/m4a/parser.go:154`, `internal/ogg/artwork.go:20` — `ExtractArtwork(ctx, r, size, path)`
- The public API integration surface (currently untested end-to-end with real content): `Open` at `file.go:61`, `ExtractArtwork` at `file.go:193`.
- Repo conventions: table-driven tests, `t.Helper()` on builders, package-internal tests (`package m4a` etc.) for parsers, `package audiometa_test` for the root. Match `internal/m4a/chapters_test.go` structurally.

**KNOWN BUGS — do not "fix" while writing tests.** The following are real, confirmed defects with their own plans. Your fuzz targets and corpus tests may hit them. When they do, record the reproducer as a skipped test or seed-corpus entry and move on; do NOT modify production code in this plan:
- Crafted `stts`/`stsz`/`stco` counts cause huge allocations/loops (`internal/m4a/chapters.go:353-368,396,427`) → plan 002.
- ID3v2.3 frame size up to ~4 GiB allocated unchecked (`internal/mp3/id3v2.go:183`) → plan 002.
- MP3 sync scan is O(fileSize) on garbage input (`internal/mp3/technical.go:28-55`) → plan 004. **Cap fuzz inputs to ≤1 MiB to keep fuzzing fast despite this.**
- MPEG-2 frames decoded with MPEG-1 tables (`internal/mp3/technical.go:13-20,79`) → plan 008.

## Commands you will need

| Purpose | Command | Expected on success |
|---------|---------|---------------------|
| Build | `go build ./...` | exit 0 |
| Tests | `go test ./...` | all packages `ok` |
| Race | `go test -race ./...` | all packages `ok` |
| One fuzz target (smoke) | `go test -fuzz=FuzzParse -fuzztime=10s ./internal/flac/` | exits 0, no crashers |
| Lint | `golangci-lint run ./...` | exit 0 (v2.11.4 — install via `mise install` or match `.golangci.yml`) |
| Coverage | `go test -coverprofile=/tmp/cover.out ./... && go tool cover -func=/tmp/cover.out \| tail -1` | total ≥ 55% |
| Corpus generation | `which ffmpeg` | ffmpeg available (see STOP conditions if not) |

## Scope

**In scope** (the only files you should create/modify):
- `testdata/` (create, at repo root) — real-encoder corpus + a `README.md` describing each file
- `internal/flac/fuzz_test.go`, `internal/mp3/fuzz_test.go`, `internal/m4a/fuzz_test.go`, `internal/ogg/fuzz_test.go` (create)
- `internal/vorbis/fuzz_test.go` (create — fuzz `ParseComment` and `ParseChapters` directly, they take plain strings)
- `corpus_test.go` (create, repo root, `package audiometa_test`) — integration tests over `testdata/`
- `truncation_test.go` in each of `internal/flac`, `internal/mp3`, `internal/m4a`, `internal/ogg` (create) — deterministic malformed-input tables
- `options_test.go` (create, repo root) — behavior tests for each `Option`
- `.github/workflows/ci.yml` — add a short fuzz smoke step ONLY (see step 6)

**Out of scope** (do NOT touch):
- Any file under `internal/` that is not a new `*_test.go` — production code changes belong to plans 002+.
- `file_bench_test.go` — benchmark realism is deferred (see Maintenance notes).
- Committing audio files with unclear licensing — generate all corpus files yourself with ffmpeg/flac tools (step 1).

## Git workflow

- Branch: `advisor/001-test-baseline`
- Commit style: single-line gitmoji, no body (repo convention — see `git log`: `✅ Add fuzz targets for parser entry points`)
- Do NOT push or open a PR unless the operator instructed it.

## Steps

### Step 1: Generate the real-file corpus

Create `testdata/` at the repo root. Generate small (≤ ~200 KB each) files with real encoders. Use ffmpeg's built-in `sine` generator so no source media is needed:

```bash
mkdir -p testdata
# 2-second sine wave source
ffmpeg -f lavfi -i "sine=frequency=440:duration=2" -y /tmp/src.wav
# FLAC with tags
ffmpeg -i /tmp/src.wav -metadata title="Test Title" -metadata artist="Test Artist" \
  -metadata album="Test Album" -metadata date=2024 -metadata genre=Audiobook \
  -y testdata/tagged.flac
# MP3 (CBR) with ID3v2.3 tags
ffmpeg -i /tmp/src.wav -codec:a libmp3lame -b:a 128k -id3v2_version 3 \
  -metadata title="Test Title" -metadata artist="Test Artist" -y testdata/tagged_cbr.mp3
# MP3 VBR (produces a Xing header)
ffmpeg -i /tmp/src.wav -codec:a libmp3lame -q:a 4 -id3v2_version 3 \
  -metadata title="VBR Test" -y testdata/tagged_vbr.mp3
# M4A/M4B with tags
ffmpeg -i /tmp/src.wav -codec:a aac -metadata title="Test Title" \
  -metadata artist="Test Artist" -y testdata/tagged.m4a
cp testdata/tagged.m4a testdata/tagged.m4b   # then re-brand if ffmpeg supports -brand; otherwise keep .m4a only and note it
# Ogg Vorbis + Opus with tags
ffmpeg -i /tmp/src.wav -codec:a libvorbis -metadata title="Test Title" -y testdata/tagged.ogg
ffmpeg -i /tmp/src.wav -codec:a libopus  -metadata title="Test Title" -y testdata/tagged.opus
# Cover art variants (use any generated PNG, e.g. via ffmpeg color source)
ffmpeg -f lavfi -i "color=c=blue:s=64x64:d=1" -frames:v 1 -y /tmp/cover.png
ffmpeg -i testdata/tagged_cbr.mp3 -i /tmp/cover.png -map 0:a -map 1:v -codec copy \
  -id3v2_version 3 -metadata:s:v title="Album cover" -y testdata/artwork.mp3
ffmpeg -i /tmp/src.wav -i /tmp/cover.png -map 0:a -map 1:v -codec:a flac \
  -disposition:v attached_pic -y testdata/artwork.flac
```

Also write `testdata/README.md` listing each file, how it was generated (exact command), and its expected key metadata (title/artist/duration±0.5s). If a specific ffmpeg incantation fails on the executor's ffmpeg build, note it in `testdata/README.md` and skip that one file — do not hunt for alternative encoders.

**Verify**: `ls testdata/*.flac testdata/*.mp3 testdata/*.m4a testdata/*.ogg testdata/*.opus | wc -l` → ≥ 6 files, each under 300 KB (`du -sh testdata` sanity check).

### Step 2: Root integration tests over the corpus

Create `corpus_test.go` (`package audiometa_test`). For each corpus file, call `audiometa.Open(path)` and assert:
- no error; `file.Format` matches the extension
- `file.Tags.Title` / `file.Tags.Artist` equal the values from step 1
- `file.Audio.Duration` within ±500ms of 2s; `file.Audio.SampleRate > 0`
- `file.Close()` returns nil
- For the artwork files: `file.ExtractArtwork()` returns ≥1 artwork whose `Data` is non-empty and whose `MIMEType` is `image/png`; calling it a second time returns the same result (cache path, `file.go:203`).

Use a table keyed by filename with expected values; model the structure after `TestOpenMany_PartialFailure` in `file_cancel_test.go:82`.

**Verify**