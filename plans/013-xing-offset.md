# Plan 013: Read the Xing/Info header at the channel-correct offset

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving on. If a
> STOP condition occurs, stop and report — do not improvise. When done, update
> the status row for this plan in `plans/README.md`.
>
> **Drift check (run first)**: `git diff --stat aca9750..HEAD -- internal/mp3/technical.go`
> On a mismatch with the excerpt below (functions may have moved if plans 004/012
> landed — locate `parseVBRHeader` by name), STOP only if the function is gone or
> its logic differs materially.

## Status

- **Priority**: P2
- **Effort**: S
- **Risk**: LOW (only changes where the Xing probe looks; VBRI probe unchanged)
- **Depends on**: 001; land after 012 (reuses the version helper)
- **Category**: bug
- **Planned at**: commit `aca9750`, 2026-10-30 (refreshed from 7b22897, 2026-07-01)

## Why this matters

The Xing/Info VBR header sits immediately after the frame's side-information block, whose size
depends on MPEG version and channel mode: **MPEG-1 stereo = 32 bytes, MPEG-1 mono = 17,
MPEG-2 stereo = 17, MPEG-2 mono = 9** (plus the 4-byte frame header). The code probes a fixed
`frameOffset + 36` — correct **only** for MPEG-1 stereo. For mono files the probe misses the
`Xing` marker, `parseVBRHeader` returns `(0, false)`, and the file falls back to CBR estimation —
producing an inaccurate duration for precisely the mono, VBR, spoken-word MP3s that audiobooks
are made of.

## Current state

`internal/mp3/technical.go:116-131` (verified):

```go
// parseVBRHeader checks for Xing/VBRI VBR headers and calculates accurate duration.
func parseVBRHeader(sr *binutil.SafeReader, frameOffset int64, sampleRate int, _ int64, _ int64) (time.Duration, bool) {
	// Xing/Info header is 36 bytes after frame header for MPEG1
	xingOffset := frameOffset + 36
	buf := make([]byte, 120)
	if err := sr.ReadAt(buf, xingOffset, "VBR header"); err != nil { return 0, false }
	isXing := string(buf[0:4]) == "Xing" || string(buf[0:4]) == "Info"
	if !isXing {
		// Try VBRI header (different location)
		vbriOffset := frameOffset + 36
		...
	}
	...
}
```

Facts to encode:
- Xing offset from frame start = `4 (frame header) + sideInfoSize`, where `sideInfoSize` is:
  MPEG-1: 32 (stereo/joint/dual) or 17 (mono); MPEG-2/2.5: 17 (stereo-ish) or 9 (mono).
- **VBRI is different**: it is always at a fixed 32 bytes after the 4-byte header
  (`frameOffset + 36`) regardless of channel mode — the current VBRI probe location is correct;
  leave it.
- The caller (`parseTechnicalInfo`) has the frame `header` word in hand: version is
  `(header >> 19) & 3`, channel mode is `(header >> 6) & 3` (mode 3 = mono) — see
  `parseMP3FrameHeader` (`technical.go:92-114`).

## Commands you will need

| Purpose | Command | Expected |
|---|---|---|
| Build | `go build ./...` | exit 0 |
| Tests | `go test ./internal/mp3/ ./...` | all pass |
| Mono VBR fixture | `ffmpeg -y -f lavfi -i "sine=frequency=440:duration=4" -ac 1 -c:a libmp3lame -q:a 6 testdata/sample_mono_vbr.mp3` | file created |
| Fixture truth | `ffprobe -v error -show_entries format=duration testdata/sample_mono_vbr.mp3` | ~4.0 |
| Lint | `mise exec -- golangci-lint run ./...` | exit 0, prints `0 issues.` |

(Use a sine tone, not silence — LAME needs real audio to produce a meaningful VBR stream, and it
writes a Xing/Info header by default.)

> **Lint note**: run lint via `mise exec -- golangci-lint run ./...` so the mise-pinned
> golangci-lint v2.15.0 is used. A stale global golangci-lint (e.g. 2.11.4) on PATH reports
> false "export data version" typecheck errors under Go 1.27.

## Scope

**In scope**:
- `internal/mp3/technical.go` (`parseVBRHeader` signature/callers within the file)
- `internal/mp3/technical_test.go`
- `testdata/sample_mono_vbr.mp3`

**Out of scope**:
- The VBRI probe location (already correct).
- Bitrate/sample-rate tables (plan 012), scan bounding (plan 004).

## Git workflow

- Branch: `advisor/013-xing-offset`
- Commit style: gitmoji single-line, no body, no attribution (e.g. `🐛 Compute Xing offset from side-info size`).
- Do NOT push or open a PR unless instructed.

## Steps

### Step 1: Compute the side-info size and use it for the Xing probe

Add a helper and pass the frame header into `parseVBRHeader` (it currently gets only
`sampleRate`; the two `_ int64` parameters are unused — tidy while here):

```go
// sideInfoSize returns the Layer III side-information size in bytes for the
// given frame header: MPEG-1: 32 (stereo modes) / 17 (mono); MPEG-2: 17 / 9.
func sideInfoSize(header uint32) int64 {
	mono := (header>>6)&0x3 == 3
	if mpegVersion(header) == 3 { // MPEG-1
		if mono { return 17 }
		return 32
	}
	if mono { return 9 }
	return 17
}
```

In `parseVBRHeader`, probe Xing/Info at `frameOffset + 4 + sideInfoSize(header)`; keep the VBRI
probe at `frameOffset + 36`. (If plan 012 hasn't landed, inline the version extraction
`(header >> 19) & 0x3` instead of calling `mpegVersion`.)

Update the call site in `parseTechnicalInfo` to pass `header`.

**Verify**: `go build ./...` → exit 0; existing tests pass (`go test ./internal/mp3/`) — the
stereo path still probes offset 36 (4+32), so MPEG-1-stereo behavior is unchanged.

### Step 2: Fixture regression test

Generate `testdata/sample_mono_vbr.mp3` (command above). Test that:
- `audiometa.Open("testdata/sample_mono_vbr.mp3")` reports `VBR == true` (the Xing header was
  found — this is the assertion that fails before the fix), and
- `Duration` within ±10% of ffprobe's value (~4 s).

Also add a constructed-bytes unit test: a fake MPEG-1 mono frame (header with mode bits = 3)
whose bytes contain `Xing` at offset 4+17=21 with the frames-count flag and a known frame count;
assert `parseVBRHeader` finds it and returns the expected duration. Mirror with a stereo frame and
`Xing` at 36.

**Verify**: `go test -race ./...` → all pass.

## Test plan

- Unit: constructed mono (offset 21) and stereo (offset 36) Xing placements.
- Integration: mono VBR fixture reports `VBR == true` and correct duration.
- Pattern: table tests in `internal/mp3/`.

## Done criteria

- [ ] `go build ./...`, `go vet ./...`, `mise exec -- golangci-lint run ./...` exit 0
- [ ] `go test -race ./...` exits 0 including the mono-Xing tests
- [ ] `grep -n "frameOffset + 36" internal/mp3/technical.go` matches only the VBRI probe (with a comment saying VBRI is fixed-offset)
- [ ] Stereo fixtures' reported values unchanged
- [ ] `plans/README.md` status row for 013 updated

## STOP conditions

- The generated mono fixture reports `VBR == true` **before** the fix — then LAME placed
  Xing at 36 for this file (unexpected); STOP and inspect the fixture bytes (`xxd | head`) to
  find the actual marker offset before changing code.
- `parseVBRHeader`'s callers pass different arguments than the excerpt shows (drift from plans
  004/012) — adapt mechanically if obvious; STOP if the header word is no longer available at
  the call site.

## Maintenance notes

- If MPEG-2.5 support is ever added (see plan 012 notes), `sideInfoSize` already handles it via
  the non-MPEG-1 branch (9/17) — confirm against the spec at that time.
- Reviewer: the easy mistake is conflating "offset from frame start" (4 + side info) with
  "offset after header" (side info only). The tests with constructed placements pin this.
