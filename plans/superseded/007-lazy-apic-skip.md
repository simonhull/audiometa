# Plan 007: Stop eagerly reading APIC artwork during the MP3 metadata pass

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving on. If a
> STOP condition occurs, stop and report — do not improvise. When done, update
> the status row for this plan in `plans/README.md`.
>
> **Drift check (run first)**: `git diff --stat 7b22897..HEAD -- internal/mp3/id3v2.go internal/mp3/artwork.go`
> If either changed, compare against the excerpts below before proceeding; on a mismatch, STOP.

## Status

- **Priority**: P2
- **Effort**: S
- **Risk**: LOW–MED (must not break CHAP chapter subframe parsing, which does need frame data)
- **Depends on**: 001 (corpus + integration tests)
- **Category**: performance
- **Planned at**: commit `7b22897`, 2026-07-01

## Why this matters

`Open()` on an MP3 reads **every** ID3 frame's full payload into memory — including APIC
(attached picture) frames that can be multiple megabytes — and then throws the bytes away,
because the metadata pass only consumes `T*`/`TXXX`/`COMM`/`CHAP` frames. An MP3 with a 5 MB
embedded cover allocates and reads 5 MB on every metadata-only `Open()`, directly contradicting
the documented lazy-loading contract ("Artwork is lazily loaded - it is not parsed during
Open()", `file.go:176-179`). The lazy artwork path already demonstrates the right pattern:
skip the payload for frames you don't need. This plan applies that pattern to the metadata pass.

## Current state

**Eager path (the problem)** — `internal/mp3/id3v2.go:166-201` `readSingleFrame` (verified):

```go
frameID := string(frameHeaderBuf[0:4])
frameSize := decodeFrameSize(header.Version, frameHeaderBuf[4:8])
frameFlags := binary.BigEndian.Uint16(frameHeaderBuf[8:10])

// Read frame data
frameData := make([]byte, frameSize)                  // <-- reads EVERY frame's payload
if err := sr.ReadAt(frameData, offset+10, fmt.Sprintf("frame %s data", frameID)); err != nil { ... }
```

**Consumer of frames** — `internal/mp3/id3v2.go:212-223` `processFrame` (verified): only acts on
`strings.HasPrefix(frame.ID, "T")` (incl. `TXXX`), `COMM`, and `CHAP`. Everything else (APIC,
GEOB, PRIV, ...) is read then ignored.

**Lazy pattern to copy** — `internal/mp3/artwork.go:76-87` `readFrameForArtwork` (verified):

```go
// Sanity check frame size
if frameSize == 0 || frameSize > 100*1024*1024 { // 100MB max
	return nil, 0, true
}
// Only read data for APIC frames to save memory
if frameID != "APIC" {
	return nil, 10 + int64(frameSize), false          // <-- advances offset without reading payload
}
```

Note also: `fmt.Sprintf("frame %s data", frameID)` allocates a label string per frame even on
success — replace with a constant label while you're in this function (the frame ID still appears
in the warning message construction on the error path).

## Commands you will need

| Purpose | Command | Expected |
|---|---|---|
| Build | `go build ./...` | exit 0 |
| Tests | `go test ./internal/mp3/ ./...` | all pass |
| Bench | `go test -bench BenchmarkOpen -benchmem -run '^$' ./...` | records allocs/op |
| Fuzz | `go test -run '^$' -fuzz FuzzParseMP3 -fuzztime 30s ./internal/mp3/` | no crashers |

## Scope

**In scope**:
- `internal/mp3/id3v2.go` (`readSingleFrame`, and a needed-frame predicate)
- `internal/mp3/parser_test.go` or a new test file for the regression test

**Out of scope**:
- `internal/mp3/artwork.go` — already lazy; leave it.
- `processFrame`'s switch — do not change which frames are consumed.
- The allocation cap itself — plan 002 adds `CheckLen`; if 002 already landed, keep its guard in place.

## Git workflow

- Branch: `advisor/007-lazy-apic-skip`
- Commit style: gitmoji single-line, no body, no attribution (e.g. `⚡ Skip unneeded frame payloads in MP3 metadata pass`).
- Do NOT push or open a PR unless instructed.

## Steps

### Step 1: Add a needed-frame predicate and skip payloads for everything else

In `internal/mp3/id3v2.go`, mirror `processFrame`'s switch exactly:

```go
// metadataFrameNeeded reports whether the metadata pass consumes this frame's
// payload. Must stay in sync with processFrame.
func metadataFrameNeeded(frameID string) bool {
	return strings.HasPrefix(frameID, "T") || // text frames incl. TXXX
		frameID == "COMM" ||
		frameID == "CHAP"
}
```

In `readSingleFrame`, after computing `frameID`/`frameSize`/`frameFlags` and before the
`make([]byte, frameSize)`:

```go
if !metadataFrameNeeded(frameID) {
	// Skip payload (APIC, GEOB, PRIV, ...) — the metadata pass never uses it.
	return nil, 10 + int64(frameSize), false
}
```

Returning `nil` frame with the advanced offset matches the existing contract: the caller
(`parseID3v2Frames:149-160`) already handles `frame == nil` by just advancing.

**Verify**: `go build ./...` → exit 0; `go test ./internal/mp3/` → all pass.

### Step 2: Regression test — big APIC is not read during Open

Add a test that builds an ID3v2.3 tag containing (in order) a `TIT2` frame, a large APIC frame
(e.g. 1 MB of 0xAA bytes), then another text frame (`TALB`), followed by a valid MP3 frame header.
Assert:
- `Parse` returns Title **and** Album (proves the walk correctly skips over the APIC payload and
  keeps parsing frames after it), and
- `ExtractArtwork` on the same bytes still returns the APIC image (proves laziness didn't break
  the artwork path).

Optionally, measure with `testing.AllocsPerRun` or a benchmark that `Parse` allocations no longer
scale with APIC size (compare 1 KB vs 1 MB APIC: allocs and bytes should be ~flat).

**Verify**: `go test -race ./internal/mp3/` → all pass; fuzz 30s → no crashers.

## Test plan

- New test: `TestParse_SkipsAPICPayload` as described (frames before AND after the APIC).
- Pattern: synthetic tag builders in `internal/mp3/parser_test.go:99` (`createMinimalMP3WithID3`).
- Verification: `go test -race ./...` all pass; `BenchmarkOpen` allocs recorded before/after in the report.

## Done criteria

- [ ] `go build ./...`, `go vet ./...`, `golangci-lint run ./...` all exit 0
- [ ] `go test -race ./...` exits 0 including `TestParse_SkipsAPICPayload`
- [ ] In `readSingleFrame`, the payload read is guarded by `metadataFrameNeeded` (inspect)
- [ ] `ExtractArtwork` on an APIC-bearing fixture still returns the artwork
- [ ] CHAP-bearing test fixtures still produce chapters (existing chapter tests pass)
- [ ] `plans/README.md` status row for 007 updated

## STOP conditions

- Any existing chapter test fails — CHAP subframes need payloads; the predicate must include CHAP
  (it does above). If failures persist beyond that, STOP and report which frame the chapter path needed.
- `processFrame`'s switch differs from the excerpt (new frame types consumed since planning) — update
  the predicate to match it exactly, and note the drift in your report. If the switch has been
  restructured entirely, STOP.

## Maintenance notes

- `metadataFrameNeeded` must stay in sync with `processFrame`. A reviewer should check both when
  either changes; plan 016's consolidation may later merge them into one table.
- The same eager-read concern exists for Ogg (`METADATA_BLOCK_PICTURE` retained in raw tags) — noted
  in the audit as lower priority; not addressed here.
