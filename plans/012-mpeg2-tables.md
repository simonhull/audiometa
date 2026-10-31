# Plan 012: Decode MPEG-2 MP3 frames with correct tables

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving on. If a
> STOP condition occurs, stop and report — do not improvise. When done, update
> the status row for this plan in `plans/README.md`.
>
> **Drift check (run first)**: `git diff --stat aca9750..HEAD -- internal/mp3/technical.go`
> On a mismatch with the excerpts below, STOP. (Plan 004 rewrites the scan in this
> file — if 004 landed, the line numbers moved but the tables/functions remain; locate
> them by name.)

## Status

- **Priority**: P2
- **Effort**: M
- **Risk**: LOW (adds version-correct tables; MPEG-1 paths unchanged)
- **Depends on**: 001 (corpus for regression); coordinates with 004 (same file)
- **Category**: bug
- **Planned at**: commit `aca9750`, 2026-10-30 (refreshed from 7b22897, 2026-07-01)

## Why this matters

The frame validator **accepts** MPEG-2 frames but then decodes them with MPEG-1 tables.
`findMP3FrameAt` (`internal/mp3/technical.go:79`) allows `version == 3` (MPEG-1) **and**
`version == 2` (MPEG-2), yet:
- `bitrateTable` (`technical.go:12-15`) holds only MPEG-1 Layer III bitrates,
- `sampleRateTable` (`technical.go:17-20`) holds only MPEG-1 rates (44100/48000/32000),
- `calculateDurationFromFrames` (`technical.go:158-164`) hardcodes `samplesPerFrame = 1152`
  (MPEG-1 Layer III; MPEG-2 Layer III uses **576**).

So any MPEG-2 file (16/22.05/24 kHz — common for low-bitrate spoken word) silently reports a
wrong sample rate (e.g. 22050 reported as 44100), wrong bitrate, and roughly **2× wrong duration**.
Silently-wrong metadata is worse than an error for a library whose one job is metadata.

## Current state

`internal/mp3/technical.go` (verified excerpts):

```go
// MP3 bitrate table (MPEG1 Layer III) in kbps.
var bitrateTable = []int{
	0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 0,
}

// MP3 sample rate table (MPEG1) in Hz.
var sampleRateTable = []int{
	44100, 48000, 32000, 0,
}
```

```go
// findMP3FrameAt — version check
version := (header >> 19) & 0x3
layer := (header >> 17) & 0x3
// MPEG1 (version 11) or MPEG2 (version 10)
if version != 3 && version != 2 {
	return 0, errors.New("unsupported MPEG version")
}
// Layer III (01)
if layer != 1 { return 0, errors.New("unsupported layer") }
```

```go
// parseMP3FrameHeader — no version branch at all
bitrateIdx := (header >> 12) & 0xF
if bitrateIdx < uint32(len(bitrateTable)) { bitrate = bitrateTable[bitrateIdx] * 1000 }
sampleRateIdx := (header >> 10) & 0x3
if sampleRateIdx < uint32(len(sampleRateTable)) { sampleRate = sampleRateTable[sampleRateIdx] }
```

```go
// calculateDurationFromFrames
samplesPerFrame := 1152 // MPEG1 Layer III
```

Reference values (MPEG audio spec, Layer III):
- **MPEG-2 bitrate table (kbps)**: `0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160, 0`
- **MPEG-2 sample rates (Hz)**: `22050, 24000, 16000, 0`
- **Samples per frame, Layer III**: MPEG-1 → 1152; MPEG-2 (and 2.5) → 576
- Version bits (header >> 19) & 3: `3` = MPEG-1, `2` = MPEG-2, `0` = MPEG-2.5, `1` = reserved.
  MPEG-2.5 halves the MPEG-2 sample rates (11025/12000/8000). The current code rejects 2.5 —
  keep rejecting it (out of scope) but document that in a comment.

## Commands you will need

| Purpose | Command | Expected |
|---|---|---|
| Build | `go build ./...` | exit 0 |
| Tests | `go test ./internal/mp3/ ./...` | all pass |
| Make an MPEG-2 fixture | `ffmpeg -y -f lavfi -i anullsrc=r=22050:cl=mono -t 2 -b:a 32k testdata/sample_mpeg2.mp3` | file created |
| Inspect fixture truth | `ffprobe -v error -show_entries stream=sample_rate,bit_rate -show_entries format=duration testdata/sample_mpeg2.mp3` | prints 22050, ~32000, ~2.0 |
| Lint | `mise exec -- golangci-lint run ./...` | exit 0, prints `0 issues.` |

> **Lint note**: run lint via `mise exec -- golangci-lint run ./...` so the mise-pinned
> golangci-lint v2.15.0 is used. A stale global golangci-lint (e.g. 2.11.4) on PATH reports
> false "export data version" typecheck errors under Go 1.27.

## Scope

**In scope**:
- `internal/mp3/technical.go`
- `internal/mp3/technical_test.go` (create if absent)
- `testdata/sample_mpeg2.mp3` (add via ffmpeg as above)

**Out of scope**:
- MPEG-2.5 support (keep rejected; add a comment).
- Layers I/II (keep rejected).
- The scan-bounding rewrite (plan 004) and Xing offset (plan 013) — same file; coordinate but
  don't do their work.

## Git workflow

- Branch: `advisor/012-mpeg2-tables`
- Commit style: gitmoji single-line, no body, no attribution (e.g. `🐛 Decode MPEG-2 frames with correct tables`).
- Do NOT push or open a PR unless instructed.

## Steps

### Step 1: Make the tables and samples-per-frame version-aware

Restructure so the version selects the tables. Suggested shape:

```go
var bitrateTableV1 = []int{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320, 0}
var bitrateTableV2 = []int{0, 8, 16, 24, 32, 40, 48, 56, 64, 80, 96, 112, 128, 144, 160, 0}
var sampleRateTableV1 = []int{44100, 48000, 32000, 0}
var sampleRateTableV2 = []int{22050, 24000, 16000, 0}

// mpegVersion extracts the version bits: 3=MPEG-1, 2=MPEG-2 (2.5 and reserved rejected upstream).
func mpegVersion(header uint32) uint32 { return (header >> 19) & 0x3 }
```

Change `parseMP3FrameHeader(header)` to branch on `mpegVersion(header)` when indexing bitrate and
sample-rate tables. Change `calculateDurationFromFrames(numFrames, sampleRate)` to also take the
version (or samples-per-frame) and use 576 for MPEG-2. Update its callers (`parseVBRHeader` passes
through — it will need the version or the header; simplest is to pass `samplesPerFrame int` computed
at the call site in `parseTechnicalInfo`, where the header is in hand).

Keep MPEG-1 results byte-identical to today (regression: existing tests).

**Verify**: `go build ./...` → exit 0; `go test ./internal/mp3/` → existing tests pass.

### Step 2: Unit tests from constructed headers

Add table tests in `internal/mp3/technical_test.go` that build 4-byte headers directly
(sync bits + version + layer + bitrate idx + samplerate idx + channel mode) and assert
`parseMP3FrameHeader` output for:
- MPEG-1: 128 kbps / 44100 Hz stereo (current behavior, must not change),
- MPEG-2: bitrate idx that means 32 kbps in V2 (would wrongly read as ~56k with the V1 table),
  sample rate idx 0 → 22050 (not 44100),
- and assert duration math: N frames at 22050 Hz MPEG-2 = `N*576/22050` seconds (not `N*1152/22050`).

### Step 3: Real-file regression via ffmpeg fixture

Generate `testdata/sample_mpeg2.mp3` (command above; 22.05 kHz mono forces MPEG-2). Open it via
the public API and assert: `SampleRate == 22050`, `Duration` within ±10% of 2 s, `Channels == 1`.
Compare against `ffprobe` ground truth if available.

**Verify**: `go test -race ./...` → all pass, including the new MPEG-2 assertions.

## Test plan

- Unit: constructed-header table tests (V1 unchanged, V2 correct, duration 576-based).
- Integration: `testdata/sample_mpeg2.mp3` via `audiometa.Open`.
- Pattern: table tests as in `internal/mp3/parser_test.go`.

## Done criteria

- [ ] `go build ./...`, `go vet ./...`, `mise exec -- golangci-lint run ./...` exit 0
- [ ] `go test -race ./...` exits 0, including MPEG-2 unit + fixture tests
- [ ] MPEG-1 fixtures (`testdata/sample.mp3`) report identical values to before the change
- [ ] `grep -n "1152" internal/mp3/technical.go` shows it only in the version-aware selection (not hardcoded in duration math)
- [ ] `plans/README.md` status row for 012 updated

## STOP conditions

- ffmpeg produces an MPEG-1 file at 22050 Hz (it shouldn't — 22.05k forces MPEG-2) — verify with
  ffprobe; if the fixture isn't MPEG-2, STOP and report the ffmpeg version/flags.
- Existing MPEG-1 test expectations change — STOP; V1 behavior must be untouched.
- Plan 004 landed and restructured `parseTechnicalInfo` beyond recognition — locate
  `parseMP3FrameHeader`/`calculateDurationFromFrames` by name; if they're gone, STOP and report.

## Maintenance notes

- MPEG-2.5 (`version == 0`) remains rejected; if real-world files surface, add
  `sampleRateTableV25` (11025/12000/8000) and 576 samples/frame — the structure from Step 1
  makes that a table addition.
- Plan 013 (Xing offset) also branches on version+channels; land 012 first so the side-info
  size calculation there can reuse `mpegVersion`.
