# Plan 002: Cap allocations from untrusted length/count fields

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving on. If a
> STOP condition occurs, stop and report — do not improvise. When done, update
> the status row for this plan in `plans/README.md`.
>
> **Drift check (run first)**: `git diff --stat aca9750..HEAD -- internal/binary/ internal/mp3/ internal/m4a/ internal/flac/`
> If any file cited below changed since this plan was written, compare the
> excerpts against the live code before proceeding; on a mismatch, STOP.

## Status

- **Priority**: P1
- **Effort**: M
- **Risk**: LOW (adds sanity caps that only reject pathological inputs; valid files use realistic sizes)
- **Depends on**: 001 (fuzz targets + corpus are the regression net for this change)
- **Category**: security (defensive: malformed-input robustness)
- **Planned at**: commit `aca9750`, 2026-10-30 (refreshed from 7b22897, 2026-07-01)

## Why this matters

The parsers allocate byte slices and integer slices sized **directly from length and
count fields read out of the file**, *before* validating those numbers against the
file size or the enclosing block. A crafted file of a few hundred bytes can declare a
multi-gigabyte length or count and force that allocation immediately. For the library's
stated consumer — an audiobook server scanning user-uploaded files — a handful of such
files exhausts memory and triggers an OOM kill: a cheap, remote, unauthenticated
denial-of-service. Worse, `internal/m4a/chapters.go` has a loop that appends one element
per `sampleCount` with **no I/O inside the loop**, so a single 8-byte `stts` entry
declaring ~4.29 billion samples spins billions of iterations and grows a slice to tens of
gigabytes — an amplification bomb that the usual "the read will fail" backstop cannot stop.

The codebase already knows how to do this correctly in some places (`internal/mp3/artwork.go:80`
caps a frame at 100 MB; `internal/m4a/codecs.go` caps an `esds`). This plan applies that
discipline uniformly via one shared helper.

## Current state

**The bounds check happens too late.** `internal/binary/reader.go:33-56` — `SafeReader.ReadAt`
does validate the read against `sr.size`, but callers allocate *before* calling it:

```go
// internal/mp3/id3v2.go:179-184
frameSize := decodeFrameSize(header.Version, frameHeaderBuf[4:8]) // raw uint32, up to ~4 GiB for v2.3
frameFlags := binary.BigEndian.Uint16(frameHeaderBuf[8:10])
// Read frame data
frameData := make([]byte, frameSize)             // <-- allocates BEFORE the ReadAt below
if err := sr.ReadAt(frameData, offset+10, ...); err != nil { ... }
```

Sites with the same allocate-then-check shape (all verified):
- `internal/mp3/id3v2.go:183` — `make([]byte, frameSize)`, `frameSize` raw uint32 (v2.3 non-synchsafe).
- `internal/m4a/chapters.go:396` — `make([]uint32, sampleCount)` (stsz).
- `internal/m4a/chapters.go:427` — `make([]uint64, chunkCount)` (stco/co64).
- `internal/m4a/metadata.go:37` — `make([]byte, valueSize)`, `valueSize = dataAtom.DataSize()-8`.
- `internal/m4a/audiobook.go:87,99,111` — `make([]byte, dataSize)` from `atom.DataSize()-4`/`-8`.
- `internal/flac/parser.go:297` (comment), `:337` (MIME), `:352` (description), `:385` (picture data) — each `make([]byte, <uint32 length>)`.

**The amplification loop** (`internal/m4a/chapters.go:350-372`, verified):

```go
var currentTime uint64
chapterTimes := []time.Duration{}
for range entryCount {
	sampleCount, err := binary.Read[uint32](sr, offset, "sample count")   // attacker-controlled
	if err != nil { break }
	sampleDuration, err := binary.Read[uint32](sr, offset+4, "sample duration")
	if err != nil { break }
	offset += 8
	for range sampleCount {                                                // NO I/O inside — nothing fails
		timeNs := (currentTime * 1_000_000_000) / uint64(timescale)
		chapterTimes = append(chapterTimes, time.Duration(timeNs))
		currentTime += uint64(sampleDuration)
	}
}
```

`SafeReader` already exposes its size: `internal/binary/reader.go` has a `size int64` field
and a `Path()` method. There is a `types.CorruptedFileError{Path, Reason, Offset}` for
signalling malformed structure (`internal/types/errors.go:34`).

Convention: parsers return warnings or `CorruptedFileError` on malformed data, never panic;
missing-optional returns `(nil, nil)` (documented in `.golangci.yml:165-170`).

## Commands you will need

| Purpose | Command | Expected |
|---|---|---|
| Build | `go build ./...` | exit 0 |
| Tests | `go test ./...` | all pass |
| Race | `go test -race ./...` | all pass |
| Fuzz MP3 | `go test -run '^$' -fuzz FuzzParseMP3 -fuzztime 30s ./internal/mp3/` | no crashers |
| Fuzz M4A | `go test -run '^$' -fuzz FuzzParseM4A -fuzztime 30s ./internal/m4a/` | no crashers |
| Fuzz FLAC | `go test -run '^$' -fuzz FuzzParseFLAC -fuzztime 30s ./internal/flac/` | no crashers |
| Lint | `mise exec -- golangci-lint run ./...` | exit 0, prints `0 issues.` |

> **Lint note**: run lint via `mise exec -- golangci-lint run ./...` so the mise-pinned
> golangci-lint v2.15.0 is used. A stale global golangci-lint (e.g. 2.11.4) on PATH reports
> false "export data version" typecheck errors under Go 1.27.

## Scope

**In scope**:
- `internal/binary/reader.go` — add the shared validation helper
- `internal/binary/reader_test.go` — test the helper
- `internal/mp3/id3v2.go`
- `internal/m4a/chapters.go`
- `internal/m4a/metadata.go`
- `internal/m4a/audiobook.go`
- `internal/flac/parser.go`
- The corresponding `_test.go` files for regression tests
- `testdata/fuzz/` — commit any crashers found

**Out of scope**:
- `internal/ogg/*` — Ogg comment/artwork parsing already bounds every length against
  `len(data)` (in-memory slices, not `make`-from-length); do not modify.
- `internal/mp3/artwork.go` — already caps at 100 MB; leave it.
- Changing the `SafeReader.ReadAt` bounds guard (`internal/binary/reader.go:40`) — it cannot
  overflow (`off < size` is checked first and `len(b)` is a real slice length); leave it.
- Do not change parse *semantics* for valid files (same fields, same values).

## Git workflow

- Branch: `advisor/002-allocation-caps`
- Commit style: gitmoji single-line, no body, no attribution (e.g. `🔒 Cap allocations from untrusted length fields`).
- Do NOT push or open a PR unless instructed.

## Steps

### Step 1: Add a shared length-validation helper to `internal/binary`

In `internal/binary/reader.go`, add an exported method on `SafeReader` and an exported
absolute cap constant. The helper validates a requested element count/length against the
remaining bytes from an offset and against an absolute ceiling, returning an error before
any allocation.

```go
// MaxAllocBytes is the absolute ceiling on any single length/count-driven
// allocation, regardless of file size. Real audio metadata blocks are far
// smaller; this rejects crafted files that declare implausible sizes.
const MaxAllocBytes = 256 << 20 // 256 MiB

// CheckLen validates that `count` elements of `elemSize` bytes each, starting
// at `off`, fit within the file and within MaxAllocBytes. It allocates nothing.
// Use it immediately before make([]T, count) when count/length came from file data.
func (sr *SafeReader) CheckLen(off int64, count int, elemSize int, what string) error {
	if count < 0 || elemSize <= 0 {
		return fmt.Errorf("%s: invalid length %d", sr.path, count)
	}
	total := int64(count) * int64(elemSize)
	if total > MaxAllocBytes {
		return fmt.Errorf("%s: %s length %d exceeds max %d", sr.path, what, total, int64(MaxAllocBytes))
	}
	if off < 0 || off > sr.size || total > sr.size-off {
		return fmt.Errorf("%s: %s length %d at offset %d exceeds file size %d", sr.path, what, total, off, sr.size)
	}
	return nil
}
```

Note: `total := int64(count) * int64(elemSize)` cannot overflow for `count` up to ~2^31
(uint32 range) and small `elemSize`; the `MaxAllocBytes` check fires long before any concern.
If `count` is a `uint32`, convert with `int(count)` at the call site (uint32 fits in int on
64-bit; this repo targets 64-bit — the benchmarks assume amd64). Guard the conversion is fine
because `CheckLen` re-validates.

**Verify**: `go build ./internal/binary/` → exit 0.

### Step 2: Route every flagged allocation through `CheckLen`

For each site, insert a `CheckLen` call immediately before the `make`, returning/handling
the error per that function's existing convention (return the error, or `break` in a loop
that returns partial results). Concrete edits:

- **`internal/mp3/id3v2.go:183`** — before `frameData := make([]byte, frameSize)`:
  ```go
  if err := sr.CheckLen(offset+10, int(frameSize), 1, "ID3 frame"); err != nil {
      file.Warnings = append(file.Warnings, types.Warning{Stage: types.StageMetadata, Message: err.Error(), Err: err})
      return nil, 10 + int64(frameSize), false
  }
  frameData := make([]byte, frameSize)
  ```
- **`internal/m4a/chapters.go:396`** (stsz) — before `make([]uint32, sampleCount)`:
  `if err := sr.CheckLen(offset, int(sampleCount), 4, "stsz table"); err != nil { return nil, err }`
- **`internal/m4a/chapters.go:427`** (stco/co64) — before `make([]uint64, chunkCount)`, use
  `elemSize` 4 for `stco` and 8 for `co64` (branch on `stcoAtom.Type == "co64"`):
  `if err := sr.CheckLen(offset, int(chunkCount), elemSize, "chunk table"); err != nil { return nil, err }`
- **`internal/m4a/metadata.go:37`** — before `make([]byte, valueSize)`:
  `if err := sr.CheckLen(valueOffset, int(valueSize), 1, "metadata value"); err != nil { return "", err }`
- **`internal/m4a/audiobook.go:87,99,111`** — before each `make([]byte, dataSize)`, guard with
  `CheckLen(dataOffset, int(dataSize), 1, "...")` and skip that child atom on error (the loop
  already tolerates missing values — `continue`/leave the field empty; do NOT panic).
- **`internal/flac/parser.go:297,337,352,385`** — before each `make([]byte, <length>)`, guard with
  `CheckLen(currentOffset, int(<length>), 1, "<what>")` and return the existing error type on failure.

**Verify**: `go build ./...` → exit 0; `go test ./...` → all pass (existing valid-file tests
must still pass — the caps never trip on realistic sizes).

### Step 3: Bound the stts amplification loop

In `internal/m4a/chapters.go` `parseChapterTimings` (the loop at lines ~350-372), the total
number of accumulated chapter times must be bounded. Chapters number in the thousands, never
billions. Add a hard cap on the accumulated slice and validate each `sampleCount` against the
`stts` atom's remaining bytes (each entry is 8 bytes, so `entryCount*8` must fit; and the sum
of `sampleCount` across entries for a text/chapter track is bounded by the sample count of the
track). Minimal robust fix:

```go
const maxChapterSamples = 1 << 20 // 1,048,576 — far above any real audiobook chapter count
...
for range entryCount {
	sampleCount, err := binary.Read[uint32](sr, offset, "sample count")
	if err != nil { break }
	sampleDuration, err := binary.Read[uint32](sr, offset+4, "sample duration")
	if err != nil { break }
	offset += 8
	for range sampleCount {
		if len(chapterTimes) >= maxChapterSamples {
			return chapterTimes, nil // bounded: stop accumulating on pathological input
		}
		timeNs := (currentTime * 1_000_000_000) / uint64(timescale)
		chapterTimes = append(chapterTimes, time.Duration(timeNs))
		currentTime += uint64(sampleDuration)
	}
}
```

Also guard the divide: `timescale` is already checked `> 0` by the caller path, but if
`parseChapterTimings` can be reached with `timescale == 0`, add `if timescale == 0 { return nil, nil }`
at the top (verify the caller — `parseTextTrackChapters` — passes a validated timescale; if not,
add the guard).

**Verify**: `go test ./internal/m4a/` → pass.

### Step 4: Add regression tests + re-run fuzzing

Add table tests (in the relevant `_test.go`) that build a minimal valid atom/frame with a
**length field larger than the buffer** and assert the parser returns an error (or partial
result) **without panicking and without allocating gigabytes**. Model the fixture construction
on the existing synthetic builders (`internal/m4a/chapters_test.go`, `internal/mp3/parser_test.go:99`).
At minimum:
- MP3: an ID3v2.3 frame whose size field says 2 GB in a 100-byte file → parser returns, warning present, no panic.
- M4A: an `stts` entry with `sampleCount = 0xFFFFFFFF` → `parseChapterTimings` returns within the cap, no hang. (Use a timeout: run this subtest under `go test -timeout 30s`.)
- M4A: an `stsz`/`stco` count exceeding file size → error, no panic.
- FLAC: a Vorbis comment length exceeding the block → error, no panic.

Then re-run the fuzz targets from plan 001 for 30s each (commands above); commit any crashers
under `testdata/fuzz/`.

**Verify**: `go test -race -timeout 60s ./...` → all pass; fuzz commands → no crashers.

## Test plan

- New tests: oversized-length regression per format (MP3, M4A stts/stsz/stco, FLAC), plus a
  `CheckLen` unit test in `internal/binary/reader_test.go` (cases: valid, exceeds file, exceeds
  MaxAllocBytes, negative count).
- Pattern to follow: existing synthetic builders in each package's `_test.go`.
- Verification: `go test -race ./...` all pass; the stts test completes in well under its timeout.

## Done criteria

- [ ] `go build ./...`, `go vet ./...`, `mise exec -- golangci-lint run ./...` all exit 0
- [ ] `go test -race ./...` exits 0, including new oversized-length regression tests
- [ ] `grep -n "make(\[\]byte, frameSize)" internal/mp3/id3v2.go` — the `make` is now preceded by a `CheckLen` guard (inspect the 6-line window)
- [ ] The four fuzz targets run 30s each with no new crashers (or crashers committed + reported)
- [ ] Valid-file tests (`TestParse_ValidMP3`, `TestOpen_Corpus` from plan 001) still pass unchanged
- [ ] `plans/README.md` status row for 002 updated

## STOP conditions

- A valid corpus file (plan 001's `testdata/`) fails to parse after the caps are added — the
  cap is too tight; report the actual sizes involved, do not just raise the constant blindly.
- The stts fix changes chapter output for a valid M4B fixture (chapter count/titles differ) —
  STOP; the cap must not affect legitimate files.
- A flagged `make` site's surrounding function has drifted from the excerpt (different variable
  names/offsets) — STOP and report rather than guessing the offset argument to `CheckLen`.

## Maintenance notes

- `MaxAllocBytes` (256 MiB) is deliberately generous; if a legitimate use case needs larger
  embedded data, prefer wiring `WithMaxArtworkSize` (plan 008) than raising this global.
- Any new `make([]T, n)` where `n` comes from file bytes must go through `CheckLen`. A reviewer
  should grep new parser code for `make(` with a variable length and confirm a preceding guard.
- The stts cap interacts with plan 031 (stsc-aware sample mapping): if that lands, re-check that
  the cap still reflects "max plausible chapters," not "max samples."
