# Plan 014: Prevent int64 overflow in M4B duration math

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving on. If a
> STOP condition occurs, stop and report — do not improvise. When done, update
> the status row for this plan in `plans/README.md`.
>
> **Drift check (run first)**: `git diff --stat aca9750..HEAD -- internal/m4a/technical.go`
> On a mismatch with the excerpt below, STOP.

## Status

- **Priority**: P2
- **Effort**: S
- **Risk**: LOW (pure arithmetic reordering; identical results in the non-overflow range)
- **Depends on**: 001
- **Category**: bug
- **Planned at**: commit `aca9750`, 2026-10-30 (refreshed from 7b22897, 2026-07-01)

## Why this matters

M4B duration is computed as `(duration * 1e9) / timescale` with the multiply performed first in
`int64`. When the mvhd `timescale` equals the sample rate (e.g. 44100 — common) and the file is
long, `duration` (in timescale units) reaches ~9.2×10⁹ at ~58 hours, and `duration * 1_000_000_000`
overflows `int64` — producing a garbage or negative `Audio.Duration`. The corrupted duration then
poisons the derived bitrate (`technical.go:65-70` divides file size by duration seconds) and the
last chapter's end time. Very long audiobooks are squarely this library's target use case
(version-1 mvhd exists precisely because 32-bit durations overflow). Note the version-0 path is
safe (duration is uint32, max ~4.3e9 × 1e9 < MaxInt64); the overflow needs a **version-1** mvhd —
also note `int64(duration)` for a uint64 above MaxInt64 is itself already negative.

## Current state

`internal/m4a/technical.go:89-109` (verified):

```go
var timescale uint32
var duration uint64

if version == 1 {
	timescale, duration, err = parseMvhdVersion1(sr, offset)
} else {
	timescale, duration, err = parseMvhdVersion0(sr, offset)
}
if err != nil { return err }

// Calculate duration: duration / timescale
if timescale > 0 {
	durationNs := (int64(duration) * 1_000_000_000) / int64(timescale)   // <-- overflows
	file.Audio.Duration = time.Duration(durationNs)
}
```

`time.Duration` is an int64 nanosecond count (max ~292 years — the *result* always fits; only the
intermediate multiply overflows).

## Commands you will need

| Purpose | Command | Expected |
|---|---|---|
| Build | `go build ./...` | exit 0 |
| Tests | `go test ./internal/m4a/ ./...` | all pass |
| Lint | `mise exec -- golangci-lint run ./...` | exit 0, prints `0 issues.` |

> **Lint note**: run lint via `mise exec -- golangci-lint run ./...` so the mise-pinned
> golangci-lint v2.15.0 is used. A stale global golangci-lint (e.g. 2.11.4) on PATH reports
> false "export data version" typecheck errors under Go 1.27.

## Scope

**In scope**:
- `internal/m4a/technical.go` (the duration computation only)
- `internal/m4a/technical_test.go`

**Out of scope**:
- `parseMvhdVersion0/1` reading logic (correct as-is).
- The first-track/stsd assumptions (plan 019's investigate item).
- Bitrate estimation formula (only benefits indirectly from a sane duration).

## Git workflow

- Branch: `advisor/014-mvhd-overflow`
- Commit style: gitmoji single-line, no body, no attribution (e.g. `🐛 Avoid int64 overflow in mvhd duration`).
- Do NOT push or open a PR unless instructed.

## Steps

### Step 1: Split the division to avoid the overflowing intermediate

Replace the computation with quotient/remainder form, staying in uint64 until the final
conversion:

```go
if timescale > 0 {
	ts := uint64(timescale)
	secs := duration / ts
	rem := duration % ts
	file.Audio.Duration = time.Duration(secs)*time.Second +
		time.Duration(rem*uint64(time.Second)/ts)
}
```

Why this is safe: `secs` fits easily (even 2^63 units at timescale 1 gives ~292-year Duration
ceiling — beyond that `time.Duration(secs)*time.Second` would overflow, but such values are
non-physical; if you want belt-and-braces, clamp `secs` to `math.MaxInt64/int64(time.Second)`
first and add a warning). `rem < ts ≤ 2^32`, so `rem * 1e9 < 2^32 * 2^30 = 2^62` — no overflow.

**Verify**: `go build ./...` → exit 0; `go test ./internal/m4a/` → existing tests pass (the
existing mvhd tests use small values; results are bit-identical in the non-overflow range).

### Step 2: Regression tests at the boundary

Add table tests for the duration math (extract it into a small function
`mvhdDuration(duration uint64, timescale uint32) time.Duration` if that makes testing cleaner):
- `duration=44100*3600*70, timescale=44100` (a 70-hour book) → exactly `70h`. (This case
  overflows the old code — assert the precise value.)
- `duration=2646000, timescale=44100` → `60s` (normal case, matches old code).
- `duration=1000, timescale=1000` → `1s`.
- `duration=1, timescale=3` → `333333333ns` (rounding behavior pinned).
- `timescale=0` → duration remains 0 (guard already present — keep it).

**Verify**: `go test -race ./...` → all pass.

## Test plan

- Unit table tests above, in `internal/m4a/technical_test.go` (file exists — match its style,
  it builds `moov` bytes with `NewSafeReader`; for pure math tests a direct function call is fine).

## Done criteria

- [ ] `go build ./...`, `go vet ./...`, `mise exec -- golangci-lint run ./...` exit 0
- [ ] `go test -race ./...` exits 0 including the 70-hour boundary test
- [ ] `grep -n "1_000_000_000" internal/m4a/technical.go` → the naked multiply-first expression is gone
- [ ] Existing mvhd tests pass unchanged
- [ ] `plans/README.md` status row for 014 updated

## STOP conditions

- Existing tests pin a rounding behavior that differs from quotient/remainder form — STOP and
  report the expected vs. actual values (they shouldn't: integer division order only differs
  at sub-nanosecond rounding).

## Maintenance notes

- The same multiply-first pattern exists in the chapter timing loop
  (`internal/m4a/chapters.go:365`: `currentTime * 1_000_000_000 / timescale` in uint64 —
  overflow wraps rather than going negative, same class of bug at ~584 years of accumulated
  `currentTime` at timescale 1, or ~13 years at 44100/ns scale... practically: it overflows for
  `currentTime > ~1.8e10` at any timescale, i.e. ~116 hours at 44100). If plan 002 landed, its
  cap limits accumulation; still, apply the same quotient/remainder form there as a follow-up —
  note it in your report if you don't do it here.
- Reviewer: check the math derivation in Step 1's "why this is safe" against the final code.
