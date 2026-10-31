# Plan 007: Stop eagerly reading APIC artwork during the MP3 metadata pass

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving on. If a
> STOP condition occurs, stop and report — do not improvise. When done, update
> the status row for this plan in `plans/README.md`.
>
> **Drift check (run first)**: `git diff --stat aca9750..HEAD -- internal/mp3/id3v2.go internal/mp3/artwork.go`
> If either changed, compare against the excerpts below before proceeding; on a mismatch, STOP.

## Status

- **Priority**: P2
- **Effort**: S
- **Risk**: LOW–MED (must keep reading payloads for the frames metadata actually consumes, esp. CHAP)
- **Depends on**: 001
- **Category**: performance
- **Planned at**: commit `aca9750`, 2026-10-30 (refreshed from 7b22897, 2026-07-01)

## Why this matters

`Open()` on an MP3 reads the **entire payload of every ID3 frame** into memory, including
multi-megabyte APIC (cover art) frames — then throws the bytes away, because the metadata
pass only consumes text/TXXX/COMM/CHAP frames. A 5 MB embedded cover costs a 5 MB allocation
and read on every metadata-only `Open()`. This directly contradicts the documented lazy-loading
contract (`file.go:175`: "Artwork is lazily loaded - it is not parsed during Open()") — the
bytes are not *parsed*, but they are *read*. The lazy artwork path already demonstrates the fix:
`internal/mp3/artwork.go:85` skips payloads for non-APIC frames "to save memory." This plan
applies the inverse skip in the metadata pass.

## Current state

`internal/mp3/id3v2.go:166-201` `readSingleFrame` (verified) reads every frame body:

```go
frameID := string(frameHeaderBuf[0:4])
frameSize := decodeFrameSize(header.Version, frameHeaderBuf[4:8])
frameFlags := binary.BigEndian.Uint16(frameHeaderBuf[8:10])

// Read frame data
frameData := make([]byte, frameSize)          // <-- happens for APIC/GEOB/PRIV too
if err := sr.ReadAt(frameData, offset+10, fmt.Sprintf("frame %s data", frameID)); err != nil { ... }
```

`internal/mp3/id3v2.go:212-223` `processFrame` (verified) consumes only:

```go
switch {
case strings.HasPrefix(frame.ID, "T") && frame.ID != "TXXX":  // text frames
	parseTextFrame(frame, file)
case frame.ID == "TXXX":
	parseTXXXFrame(frame, file)
case frame.ID == "COMM":
	parseCommentFrame(frame, file)
case frame.ID == "CHAP":
	*chapters = append(*chapters, frame)
}
```

The lazy-artwork exemplar `internal/mp3/artwork.go:84-87` (verified):

```go
// Only read data for APIC frames to save memory
if frameID != "APIC" {
	return nil, 10 + int64(frameSize), false
}
```

Note: `fmt.Sprintf("frame %s data", frameID)` at id3v2.go:184 also allocates per frame even on
success; while editing this function, replace it with a constant label (e.g. `"frame data"`) —
the frame ID already appears in the warning message construction on the error path.

## Commands you will need

| Purpose | Command | Expected |
|---|---|---|
| Build | `go build ./...` | exit 0 |
| Tests | `go test ./internal/mp3/ ./...` | all pass |
| Bench | `go test -bench BenchmarkOpen -benchmem -run '^$' ./...` | records allocs/op |
| Fuzz | `go test -run '^$' -fuzz FuzzParseMP3 -fuzztime 30s ./internal/mp3/` | no crashers |
| Lint | `mise exec -- golangci-lint run ./...` | exit 0, prints `0 issues.` |

> **Lint note**: run lint via `mise exec -- golangci-lint run ./...` so the mise-pinned
> golangci-lint v2.15.0 is used. A stale global golangci-lint (e.g. 2.11.4) on PATH reports
> false "export data version" typecheck errors under Go 1.27.

## Scope

**In scope**:
- `internal/mp3/id3v2.go` (`readSingleFrame` and, if needed, `processFrame`)
- `internal/mp3/parser_test.go` or a new test file for the regression test
- `testdata/` — a fixture with a large-ish APIC frame (can be built synthetically)

**Out of scope**:
- `internal/mp3/artwork.go` — the lazy path is already correct; don't touch it.
- Which frames map to which fields — no mapping changes.
- The allocation cap (plan 002) — if 002 landed first, keep its `CheckLen` guard in place around
  the reads that remain.

## Git workflow

- Branch: `advisor/007-lazy-apic-metadata-pass`
- Commit style: gitmoji single-line, no body, no attribution (e.g. `⚡ Skip artwork payloads in MP3 metadata pass`).
- Do NOT push or open a PR unless instructed.

## Steps

### Step 1: Skip payloads for frames the metadata pass doesn't consume

In `readSingleFrame`, decide **before** reading the payload whether this frame is consumed.
The consumed set (must exactly mirror `processFrame`): IDs starting with `"T"` (which includes
`TXXX`), `"COMM"`, and `"CHAP"`. For anything else (APIC, GEOB, PRIV, UFID, …), skip the payload
and advance:

```go
frameID := string(frameHeaderBuf[0:4])
frameSize := decodeFrameSize(header.Version, frameHeaderBuf[4:8])
frameFlags := binary.BigEndian.Uint16(frameHeaderBuf[8:10])

if !isMetadataFrame(frameID) {
	// Payload not needed by the metadata pass (e.g. APIC artwork — read lazily
	// by ExtractArtwork). Skip the bytes entirely.
	return nil, 10 + int64(frameSize), false
}

frameData := make([]byte, frameSize)
if err := sr.ReadAt(frameData, offset+10, "frame data"); err != nil { ... }
```

With a helper kept adjacent to `processFrame` so the two stay in sync:

```go
// isMetadataFrame reports whether processFrame consumes this frame's payload.
// Keep in sync with processFrame.
func isMetadataFrame(id string) bool {
	return strings.HasPrefix(id, "T") || id == "COMM" || id == "CHAP"
}
```

**Verify**: `go build ./...` → exit 0; `go test ./internal/mp3/` → all pass (text/TXXX/COMM/CHAP
behavior unchanged; skipped frames were already ignored by `processFrame`).

### Step 2: Add a regression test proving the skip

Build a synthetic ID3v2.3 tag containing: one `TIT2` frame, one large `APIC` frame (e.g. 1 MB of
0xFF), then one `TALB` frame **after** the APIC. Assert:
- `Parse` returns Title *and* Album (proves the walk correctly skips over the APIC payload and
  keeps going — the offset arithmetic is right), and
- (optional, if plan 001's benchmarks exist) `allocs/op` for opening this fixture drops vs. the
  eager version — record numbers in the report.

Also assert `ExtractArtwork` on the same fixture still returns the APIC image (the lazy path is
untouched and must still find it).

**Verify**: `go test -race ./internal/mp3/` → all pass; fuzz 30s → no crashers.

## Test plan

- New test: `TestParse_SkipsArtworkPayload` (Title + Album parsed across a large APIC; artwork
  still extractable lazily).
- Pattern: synthetic tag builders in `internal/mp3/parser_test.go:99` (`createMinimalMP3WithID3`).
- Verification: `go test -race ./...` all pass.

## Done criteria

- [ ] `go build ./...`, `go vet ./...`, `mise exec -- golangci-lint run ./...` all exit 0
- [ ] `go test -race ./...` exits 0 including `TestParse_SkipsArtworkPayload`
- [ ] `readSingleFrame` no longer allocates/reads payloads for frames `processFrame` doesn't consume (inspect the function; if plan 026 landed, `GRP1`/`MVNM`/`MVIN` must still be read — the existing 026 tests for them pass)
- [ ] `ExtractArtwork` on an APIC-bearing fixture still returns the image
- [ ] The per-frame `fmt.Sprintf` label at the old id3v2.go:184 is replaced with a constant
- [ ] `plans/README.md` status row for 007 updated

## STOP conditions

- `processFrame` consumes more frame IDs than the excerpt shows (drift) — STOP; the skip set must
  exactly mirror it or metadata silently disappears.
- Any existing test loses a parsed field after the skip — STOP; a consumed frame is being skipped.
- Frame flags indicate compression/encryption handling was added since planning (grep `frameFlags`)
  — STOP and reconcile with plan 019.

## Maintenance notes

- If `processFrame` ever gains a new frame ID, `isMetadataFrame` must gain it too — the comment
  pins them together; a reviewer should check both on any frame-handling PR.
- Plan 019 (unsync/frame flags) touches the same function; whichever lands second reconciles.
- **Plan 026 interaction (added 2026-10-30):** plan 026 makes `processFrame` dispatch `GRP1`,
  `MVNM` and `MVIN` (non-`T*` IDs) through a new `isTextFrameID` helper. If 026 has landed,
  `isMetadataFrame` must be `isTextFrameID(id) || id == "TXXX" || id == "COMM" || id == "CHAP"`
  (or whatever `processFrame` dispatches at that point) — a bare `HasPrefix(id, "T")` would
  silently skip the series frames 026 fixed. The STOP condition about `processFrame` drift
  applies: re-derive the set from the live `processFrame`, never from this plan's excerpt.
- The same eager-read pattern exists for Ogg (base64 `METADATA_BLOCK_PICTURE` retained in
  `Tags.raw` — see plan 016's maintenance notes); Ogg is structurally different (comments live in
  one packet) and is deliberately not addressed here.
