# Plan 001: Establish a real-file test corpus, fuzz targets, and malformed-input regression tests

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `plans/README.md` — unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Drift check (run first)**: `git diff --stat 7b22897..HEAD -- internal/ testdata/ .github/workflows/ci.yml`
> If any in-scope file changed since this plan was written, compare the
> "Current state" excerpts against the live code before proceeding; on a
> mismatch, treat it as a STOP condition.

## Status

- **Priority**: P1
- **Effort**: L
- **Risk**: LOW (purely additive — new tests and fixtures only)
- **Depends on**: none
- **Category**: tests
- **Planned at**: commit `7b22897`, 2026-07-01

## Why this matters

audiometa is a library whose entire job is parsing untrusted binary audio files, yet it has **zero fuzz targets**, **no `testdata/` corpus of real encoder output**, and 47.4% statement coverage concentrated on happy paths. Every existing fixture is built in memory by hand, encoding the same assumptions the parser makes (e.g. Ogg fixtures write CRC=0 and pass because the parser never validates CRC). This plan creates the safety net that every subsequent hardening/fix plan (002, 004, 008, 009, 011, 012) will use for verification. It is deliberately first in the execution order.

## Current state

- `go test ./...` passes for all packages. CI (`.github/workflows/ci.yml:34`) runs `go test -v -race -coverprofile=coverage.txt -covermode=atomic ./...`.
- There is **no** `testdata/` directory anywhere in the repo. `grep -r "func Fuzz" .` returns nothing.
- Parser entrypoints all have the shape `func (p *parser) Parse(ctx context.Context, r io.ReaderAt, size int64, path string) (*types.File, error)`:
  - `internal/flac/parser.go:34`
  - `internal/mp3/parser.go:17`
  - `internal/m4a/parser.go:57`
  - `internal/ogg/parser.go:25`
- Existing synthetic fixture builders to seed fuzzing from:
  - `internal/mp3/parser_test.go:99` — `createMinimalMP3WithID3() []byte`
  - `internal/flac/parser_test.go` — `createMinimalFLAC` (top of file)
  - `internal/ogg/parser_test.go` — `createMinimalOgg`
  - `file_cancel_test.go:17` — `createTestM4BFile(t)` builds an ftyp+empty-moov M4B
- Test style convention (match it): plain `testing`, no testify, temp files via `os.CreateTemp`, direct `&parser{}` construction inside internal packages. Example: `internal/mp3/parser_test.go:12-55` (`TestParse_ValidMP3`).
- The public entry is `audiometa.Open(path, opts...)` in `file.go:61`; the internal seam used by tests is the package-level parser `Parse`.
- Tools available on this machine (verified): `ffmpeg` at `/usr/bin/ffmpeg`, `flac` at `/usr/bin/flac`. `opusenc`/`oggenc` are NOT installed — use ffmpeg's `libopus`/`libvorbis` encoders instead.
- Linter note: `.golangci.yml` relaxes most linters for `_test.go` files, so table-test verbosity is fine. `godot` (comments end in period) applies to non-test files only in practice — but keep comments tidy anyway.

## Commands you will need

| Purpose | Command | Expected on success |
|---|---|---|
| Tests | `go test ./...` | all packages `ok` |
| Race tests | `go test -race ./...` | all packages `ok` |
| One fuzz target, smoke | `go test -run=^$ -fuzz=FuzzParse -fuzztime=30s ./internal/flac/` | exits 0, no crashers |
| Coverage | `go test -coverprofile=/tmp/cover.out ./... && go tool cover -func=/tmp/cover.out \| tail -1` | total ≥ 60% |
| Lint | `golangci-lint run ./...` | exit 0 |
| Build | `go build ./...` | exit 0 |

## Scope

**In scope** (files you may create/modify):
- `testdata/` (new, at repo root) — corpus files + `generate.sh`
- `internal/flac/fuzz_test.go`, `internal/mp3/fuzz_test.go`, `internal/m4a/fuzz_test.go`, `internal/ogg/fuzz_test.go`, `internal/vorbis/fuzz_test.go` (new)
- `internal/*/corrupt_test.go` (new — truncation/oversized-length regression tests)
- `integration_test.go` (new, repo root, package `audiometa_test`)
- `.github/workflows/ci.yml` — add a short fuzz smoke step ONLY (do not touch existing jobs)
- `plans/README.md` (status row)

**Out of scope** (do NOT touch):
- Any non-test `.go` file. **This plan fixes nothing** — if a fuzzer or corrupt-input test finds a panic/hang, record it as a skipped test with a comment naming the crasher input and report it (see STOP conditions). Plans 002/004 do the fixing.
- `examples/`, `cmd/`.

## Git workflow

- Branch: `advisor/001-test-baseline` (repo commits straight to main historically; use a branch anyway since fuzzing may surface crashers).
- Commit style: gitmoji prefix, single line, no body — e.g. `✅ Add real-file corpus and fuzz targets` (matches `git log` style: `✨ Achieve series metadata parity with Audiobookshelf`).
- Do NOT push or open a PR unless the operator instructed it.

## Steps

### Step 1: Create the corpus generator and commit small real-encoder files

Create `testdata/generate.sh` (bash, `set -euo pipefail`) that produces ~1-second files with a fixed sine tone and a rich, known tag set. Use these exact invocations (all verified tools):

```bash
# Shared tag set — every file gets:
# title="Corpus Title" artist="Corpus Artist" album="Corpus Album"
# date=2024 genre=Audiobook composer="Corpus Narrator"
SRC="-f lavfi -i sine=frequency=440:duration=1"

ffmpeg $SRC -metadata title="Corpus Title" -metadata artist="Corpus Artist" \
  -metadata album="Corpus Album" -metadata date=2024 -metadata genre=Audiobook \
  -metadata composer="Corpus Narrator" -c:a flac testdata/sine.flac
ffmpeg $SRC <same -metadata flags> -c:a libmp3lame -b:a 64k -id3v2_version 3 testdata/sine-id3v23.mp3
ffmpeg $SRC <same -metadata flags> -c:a libmp3lame -b:a 64k -id3v2_version 4 testdata/sine-id3v24.mp3
ffmpeg $SRC <same -metadata flags> -c:a aac -b:a 64k -f mp4 testdata/sine.m4a
ffmpeg $SRC <same -metadata flags> -c:a libvorbis -q:a 2 testdata/sine.ogg
ffmpeg $SRC <same -metadata flags> -c:a libopus -b:a 32k testdata/sine.opus
```

Also generate one **M4B with chapters**: write an ffmetadata file with two `[CHAPTER]` blocks (0–500ms "Intro", 500ms–1s "Chapter One") and run `ffmpeg $SRC -i chapters.txt -map_metadata 1 -c:a aac -f mp4 testdata/sine-chapters.m4b`. And one **MP3 with embedded cover**: create a 32x32 PNG with ffmpeg (`-f lavfi -i color=red:size=32x32 -frames:v 1 cover.png`), then mux it as an attached picture (`-i sine.mp3 -i cover.png -map 0:a -map 1:v -c copy -id3v2_version 3 -metadata:s:v comment="Cover (front)" testdata/sine-cover.mp3`).

Run the script once and commit both the script and the generated files. Files should each be under ~40KB.

**Verify**: `ls testdata/*.flac testdata/*.mp3 testdata/*.m4a testdata/*.m4b testdata/*.ogg testdata/*.opus | wc -l` → `8`; `du -sh testdata` → under 1MB.

### Step 2: Root-level integration test over the corpus

Create `integration_test.go` (package `audiometa_test`) with `TestOpenCorpus`: table-driven over every `testdata/` file, calling `audiometa.Open("testdata/<name>")` and asserting for each file:

- `err == nil`
- `file.Tags.Title == "Corpus Title"`, `file.Tags.Artist == "Corpus Artist"`, `file.Tags.Album == "Corpus Album"`, `file.Tags.Year == 2024`
- `file.Audio.Duration` within [900ms, 1100ms]
- `file.Audio.SampleRate > 0`, `file.Audio.Channels > 0`
- correct `file.Format` per extension (`FormatFLAC`, `FormatMP3`, `FormatM4A`, `FormatM4B`, `FormatOgg`, `FormatOpus`)
- for `sine-chapters.m4b`: `len(file.Chapters) == 2`, first title `"Intro"`
- for `sine-cover.mp3`: `art, err := file.ExtractArtwork()` → `err == nil && len(art) == 1 && art[0].MIMEType == "image/png"`; call it twice and assert the second call returns the same slice length (cache path)
- `defer file.Close()` for every case

Use `file_cancel_test.go:52` (`TestOpenMany_Cancellation`) as the structural pattern for package/name/style. Note the `Artwork` struct fields: check `internal/types/artwork.go` for the exact field name (`MIMEType` vs `MimeType`) before writing assertions.

If a specific assertion fails because the parser genuinely can't extract that field from real encoder output (e.g. ffmpeg writes the year as `TDRC` and the parser misses it), do NOT weaken the assertion to pass — mark that single case with `t.Skipf("KNOWN GAP: <detail>")` and list it in your final report. These skips are audit findings, not noise.

**Verify**: `go test -run TestOpenCorpus -v ./` → PASS (possibly with documented skips).

### Step 3: Fuzz targets, one per parser package

In each of `internal/flac`, `internal/mp3`, `internal/m4a`, `internal/ogg`, create `fuzz_test.go` with:

```go
func FuzzParse(f *testing.F) {
	// Seed with the package's synthetic builder output and the real corpus.
	f.Add(createMinimalFLAC())            // package's existing builder(s)
	for _, p := range corpusSeeds(f) {    // read ../../testdata/*.<ext> via os.ReadFile
		f.Add(p)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		p := &parser{}
		// Must not panic or hang; errors and warnings are fine.
		file, _ := p.Parse(context.Background(), bytes.NewReader(data), int64(len(data)), "fuzz")
		if file != nil {
			_ = file.Tags.Title // touch results so they aren't optimized away
		}
	})
}
```

Adapt seeds per package (`createMinimalMP3WithID3()`, `createMinimalOgg()`, the M4B builder bytes from `file_canc