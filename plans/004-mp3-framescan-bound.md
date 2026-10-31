# Plan 004: Bound and buffer the MP3 frame-sync scan

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving on. If a
> STOP condition occurs, stop and report — do not improvise. When done, update
> the status row for this plan in `plans/README.md`.
>
> **Drift check (run first)**: `git diff --stat aca9750..HEAD -- internal/mp3/technical.go`
> If it changed, compare against the excerpt below before proceeding; on a mismatch, STOP.

## Status

- **Priority**: P1
- **Effort**: M
- **Risk**: MED (touches frame-detection; must preserve correct detection for valid MP3s)
- **Depends on**: 001
- **Category**: performance / security (unbounded syscall loop → DoS)
- **Planned at**: commit `aca9750`, 2026-10-30 (refreshed from 7b22897, 2026-07-01)

## Why this matters

`parseTechnicalInfo` finds the first MP3 frame by scanning **one byte at a time**, and at
each offset issues a fresh 4-byte `ReadAt` — which on an `*os.File` is a separate `pread`
syscall plus a 4-byte allocation. The scan has **no upper bound**: `for frameOffset < fileSize-4 { ...; frameOffset++ }`.
When the first valid frame isn't immediately at `tagSize` — a slightly-wrong ID3 size, junk
or padding after the tag, or `parseID3v2` failing so `tagSize = 0` — the loop can scan the
entire file, one syscall per byte. On a large or crafted MP3 that is billions of syscalls:
pathological latency and a CPU/syscall DoS on the audiobook-scanning consumer. Real MP3 frame
data begins within a small distance of the tag, so a bounded, buffered scan is both correct
and dramatically faster.

## Current state

`internal/mp3/technical.go:22-65` (verified):

```go
func parseTechnicalInfo(sr *binutil.SafeReader, tagSize int64, fileSize int64, file *types.File) error {
	frameOffset := tagSize
	for frameOffset < fileSize-4 {
		header, err := findMP3FrameAt(sr, frameOffset)
		if err == nil {
			bitrate, sampleRate, channels := parseMP3FrameHeader(header)
			if bitrate > 0 && sampleRate > 0 {
				... // set fields, VBR/CBR, return nil
			}
		}
		frameOffset++
	}
	return errors.New("no valid MP3 frame found")
}

func findMP3FrameAt(sr *binutil.SafeReader, offset int64) (uint32, error) {
	buf := make([]byte, 4)                       // fresh 4-byte alloc per call
	if err := sr.ReadAt(buf, offset, "MP3 frame header"); err != nil { return 0, err }
	header := binary.BigEndian.Uint32(buf)
	if header&0xFFE00000 != 0xFFE00000 { return 0, errors.New("invalid frame sync") }
	... // version/layer checks
}
```

`SafeReader` wraps an `io.ReaderAt` and knows the file `size`. The repo already reads a bounded
window elsewhere and scans it in memory — `internal/ogg/container.go:146-158` reads the last 64 KB
in one `ReadAt` then scans the buffer. Use that as the pattern.

## Commands you will need

| Purpose | Command | Expected |
|---|---|---|
| Build | `go build ./...` | exit 0 |
| Tests | `go test ./internal/mp3/ ./...` | all pass |
| Bench (before/after) | `go test -bench BenchmarkOpen -benchmem -run '^$' ./...` | records ns/op, allocs/op |
| Fuzz | `go test -run '^$' -fuzz FuzzParseMP3 -fuzztime 30s ./internal/mp3/` | no crashers |
| Lint | `mise exec -- golangci-lint run ./...` | exit 0, prints `0 issues.` |

> **Lint note**: run lint via `mise exec -- golangci-lint run ./...` so the mise-pinned
> golangci-lint v2.15.0 is used. A stale global golangci-lint (e.g. 2.11.4) on PATH reports
> false "export data version" typecheck errors under Go 1.27.

## Scope

**In scope**:
- `internal/mp3/technical.go`
- `internal/mp3/technical_test.go` (create if absent) or `internal/mp3/parser_test.go` for regression tests
- `testdata/` — add a "no valid frame / junk after tag" fixture

**Out of scope**:
- Bitrate/sample-rate/duration correctness (MPEG-2 tables, Xing offset) — plans 012 and 013.
  Here, only bound and buffer the *search*; do not change what `parseMP3FrameHeader` computes.
- `internal/mp3/id3v2.go` — the frame walk there is plan 003's cancellation and plan 007's APIC laziness.

## Git workflow

- Branch: `advisor/004-mp3-framescan-bound`
- Commit style: gitmoji single-line, no body, no attribution (e.g. `⚡ Bound and buffer MP3 frame-sync scan`).
- Do NOT push or open a PR unless instructed.

## Steps

### Step 1: Read a bounded window once, scan it in memory

Introduce a maximum resync distance and read that window in a single `ReadAt`, then scan the
in-memory buffer for a valid frame sync instead of issuing a read per byte.

```go
const maxFrameSearch = 1 << 16 // 64 KiB — real frame data begins within this of the tag

func parseTechnicalInfo(sr *binutil.SafeReader, tagSize int64, fileSize int64, file *types.File) error {
	start := tagSize
	if start < 0 { start = 0 }
	if start >= fileSize { return errors.New("no valid MP3 frame found") }

	windowEnd := start + maxFrameSearch
	if windowEnd > fileSize { windowEnd = fileSize }
	window := make([]byte, windowEnd-start)
	if err := sr.ReadAt(window, start, "MP3 frame search window"); err != nil {
		return fmt.Errorf("read frame search window: %w", err)
	}

	for i := 0; i+4 <= len(window); i++ {
		header := binary.BigEndian.Uint32(window[i : i+4])
		if header&0xFFE00000 != 0xFFE00000 {
			continue
		}
		if !validFrameHeader(header) { // version/layer checks extracted from findMP3FrameAt
			continue
		}
		bitrate, sampleRate, channels := parseMP3FrameHeader(header)
		if bitrate > 0 && sampleRate > 0 {
			frameOffset := start + int64(i)
			file.Audio.Bitrate = bitrate
			file.Audio.SampleRate = sampleRate
			file.Audio.Channels = channels
			file.Audio.Codec = "MP3"
			duration, vbr := parseVBRHeader(sr, frameOffset, sampleRate, fileSize, tagSize)
			if vbr {
				file.Audio.Duration = duration
				file.Audio.VBR = true
			} else {
				file.Audio.Duration = estimateCBRDuration(bitrate, fileSize, tagSize)
				file.Audio.VBR = false
			}
			return nil
		}
	}
	return errors.New("no valid MP3 frame found")
}
```

Extract the version/layer validation from the current `findMP3FrameAt` into a
`validFrameHeader(header uint32) bool` so both the in-memory scan and any remaining caller use
one implementation. You may keep `findMP3FrameAt` if something else calls it (grep first:
`grep -rn findMP3FrameAt internal/mp3/`); if nothing else calls it, replace it.

**Verify**: `go build ./...` → exit 0; `go test ./internal/mp3/` → all pass.

### Step 2: Add regression + performance guard tests

- **Correctness preserved**: existing `TestParse_ValidMP3` and plan 001's `TestOpen_Corpus`
  (`testdata/sample.mp3`) must still detect the frame and set codec/bitrate/samplerate.
- **Bounded on junk**: build (or add to `testdata/`) an MP3-ish file that is an ID3v2 tag
  followed by 200 KB of non-frame bytes. Assert `parseTechnicalInfo` returns the
  "no valid MP3 frame found" error **quickly** (run the subtest under `go test -timeout 10s`)
  and does not hang. This proves the 64 KB window bounds the scan.
- Optional: capture `go test -bench BenchmarkOpen -benchmem` before and after; note the
  allocs/op improvement in your report (the per-byte 4-byte allocs are gone).

**Verify**: `go test -race -timeout 30s ./internal/mp3/` → all pass; fuzz 30s → no crashers.

## Test plan

- New tests: `TestParseTechnical_JunkAfterTag_Bounded` (returns error under timeout, no hang),
  and confirm `TestParse_ValidMP3` still passes.
- Pattern: `internal/mp3/parser_test.go` synthetic builders.
- Verification: `go test -race ./...` all pass.

## Done criteria

- [ ] `go build ./...`, `go vet ./...`, `mise exec -- golangci-lint run ./...` all exit 0
- [ ] `go test -race -timeout 60s ./...` exits 0 including the bounded-scan test
- [ ] `grep -n "frameOffset++" internal/mp3/technical.go` returns nothing (the per-byte read loop is gone)
- [ ] `TestParse_ValidMP3` and `TestOpen_Corpus` (MP3) still detect the frame and set codec/bitrate
- [ ] Fuzz `FuzzParseMP3` 30s: no crashers
- [ ] `plans/README.md` status row for 004 updated

## STOP conditions

- A valid MP3 in `testdata/` no longer has its frame detected after the change (the window is too
  small or the scan skips the real frame) — STOP and report the offset of the real first frame.
- `parseVBRHeader`/`estimateCBRDuration` signatures differ from the excerpt — STOP; do not guess.
- The real first frame in some fixture sits beyond 64 KB of the tag — STOP and report; the constant
  may need justification/raising, but that's a decision to surface, not to silently change.

## Maintenance notes

- 64 KiB is a generous resync window; if a real-world file legitimately needs more, raise
  `maxFrameSearch` with a comment citing the file, don't remove the bound.
- Plans 012 (MPEG-2 tables) and 013 (Xing offset) build on the detected frame; they assume the
  scan already found the correct first frame. Land 004 before or alongside them.
- Reviewer should confirm the scan reads exactly one window and never issues a per-byte read.
