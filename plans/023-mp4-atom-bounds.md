# Plan 023: Reject MP4 atom sizes that overrun their parent (fixes infinite loop)

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving on. If a
> STOP condition occurs, stop and report — do not improvise. When done, update
> the status row for this plan in `plans/README.md`.
>
> **Drift check (run first)**: `git diff --stat aca9750..HEAD -- internal/m4a/`
> If any in-scope file changed, compare the "Current state" excerpts below against the live
> code; on a mismatch, STOP.

## Status

- **Priority**: P1
- **Effort**: S
- **Risk**: LOW (malformed atoms that used to be walked past now produce a `CorruptedFileError`; every well-formed fixture in the suite still parses — verified on a scratch copy)
- **Depends on**: none
- **Category**: security
- **Planned at**: commit `aca9750`, 2026-10-30

## Why this matters

A 32-byte crafted M4A file hangs `audiometa.Open` forever, burning a full CPU core. The atom walker
accepts any 64-bit extended size, then advances with `offset += int64(atom.Size)`. A size of
`0xFFFFFFFFFFFFFFF0` is `-16` as `int64`, so the offset moves *backwards* to an earlier atom and the
loop never ends. The context passed to `OpenContext` is never checked inside this loop, so even a
caller's timeout does not stop it. The advisor reproduced it: `audiometa.OpenContext` with a 2 s
timeout was still running after 5 s. The primary consumer is an audiobook server that scans
user-supplied files, so one hostile upload can pin a worker for good. After this plan,
every atom header is checked against the end of its enclosing container. The walk then provably
moves strictly forward and never passes the container's end.

## Current state

All files are in `internal/m4a/` (package `m4a`).

- `atoms.go` — `Atom` type, `readAtomHeader`, `findAtom`. This is where the bug lives.
- `metadata.go`, `audiobook.go`, `chapters.go`, `artwork.go` — five more loops that walk sibling
  atoms with the same `offset += int64(x.Size)` pattern.
- `*_test.go` — 23 direct calls to `readAtomHeader(sr, 0)` that must be updated for the new signature.

`atoms.go:58-100` (verified). The only size check is `< 8`:

```go
// readAtomHeader reads an atom header at the given offset.
func readAtomHeader(sr *binary.SafeReader, offset int64) (*Atom, error) {
	// Read size (4 bytes)
	size32, err := binary.Read[uint32](sr, offset, "atom size")
	...
	// Handle extended size (size == 1 means 64-bit size follows)
	if size32 == 1 {
		size64, err := binary.Read[uint64](sr, offset+8, "extended atom size")
		...
		atom.Size = size64
		atom.Extended = true
	} else {
		atom.Size = uint64(size32)
		atom.Extended = false
	}

	// Validate atom size
	if atom.Size < 8 {
		return nil, &types.CorruptedFileError{
			Offset: offset,
			Reason: fmt.Sprintf("invalid atom size %d (minimum is 8)", atom.Size),
		}
	}

	return atom, nil
}
```

`atoms.go:102-129` (verified). The advance can go backwards, and the `Size == 0` guard can never
run because `readAtomHeader` already rejects sizes below 8:

```go
func findAtom(sr *binary.SafeReader, start, end int64, atomType string) (*Atom, error) {
	offset := start

	for offset < end {
		atom, err := readAtomHeader(sr, offset)
		if err != nil {
			return nil, err
		}

		if atom.Type == atomType {
			return atom, nil
		}

		// Move to next atom
		offset += int64(atom.Size)

		// Prevent infinite loop on corrupted files
		if atom.Size == 0 {
			return nil, &types.CorruptedFileError{
				Offset: offset,
				Reason: "atom with zero size",
			}
		}
	}

	return nil, fmt.Errorf("atom '%s' not found", atomType)
}
```

The other sibling-walk loops (verified). Each one has a local `end` variable for its container and
calls `readAtomHeader(sr, offset)`:

| File:lines | Function | Advance line |
|---|---|---|
| `metadata.go:51-90` | `extractIlstMetadata` | `:86` `offset += int64(tagAtom.Size)` |
| `audiobook.go:12-37` | `parseAudiobookTags` | `:36` `offset += int64(atom.Size)` |
| `audiobook.go:67-120` | `parseCustomAtomWithTags` | `:119` `offset += int64(atom.Size)` |
| `chapters.go:163-183` | `findChapterTrackReference` | `:180` `offset += int64(trakAtom.Size)` |
| `chapters.go:207-227` | `findTrackByID` | `:224` `offset += int64(trakAtom.Size)` |
| `artwork.go:57-85` | `extractArtwork` (covr loop) | `:73` and `:79` `offset += int64(dataAtom.Size)`; unreachable `if dataAtom.Size == 0 { break }` at `:82-84` |

Every `findAtom` call passes a correct container end: either the file `size`, or
`X.DataOffset()+int64(X.DataSize())` of the parent atom (for `meta`, the start is `+4` for its
version/flags). So "parent end" is already available at every call site. No other package calls
`readAtomHeader` or `findAtom`.

**Size 0 (decided: keep rejecting).** Per ISO/IEC 14496-12, `size32 == 0` means "this atom extends to
the end of the file". It is only legal for the last top-level atom, in practice a streamed `mdat`.
Today `size32 == 0` gives `Size == 0`, which is rejected by `< 8`. This plan keeps that behavior
unchanged and does not implement the spec meaning. See Maintenance notes.

**Conventions.** Structural corruption returns `*types.CorruptedFileError{Offset, Reason}`
(`internal/types/errors.go`). Binary reads go through `binary.Read[T](sr, off, "what")` and
`sr.ReadAt(buf, off, "what")` on `*binary.SafeReader`. Tests build synthetic atoms with
`createMockAtom(atomType string, data []byte) []byte` (`atoms_test.go:12-26`), which writes an
8-byte header plus data.

**The reproduction (32 bytes, big-endian):**

```
00 00 00 10  'f' 't' 'y' 'p'  'i' 's' 'o' 'm'  00 00 00 00     ftyp, size 16, brand isom
00 00 00 01  'f' 'r' 'e' 'e'  FF FF FF FF FF FF FF F0          size32=1 → extended size 0xFFFFFFFFFFFFFFF0
```

`findAtom(sr, 0, 32, "moov")` skips `ftyp` to offset 16. It then reads `free` with size -16 as
int64, goes back to offset 0, and repeats forever.

## Commands you will need

| Purpose | Command | Expected |
|---|---|---|
| Build | `go build ./...` | exit 0 |
| Vet | `go vet ./...` | exit 0 |
| Targeted tests | `go test ./internal/m4a/ -run 'ReadAtomHeader\|FindAtom\|HugeExtended' -v` | all PASS |
| All tests | `go test ./...` and `go test -race ./...` | all `ok` |
| Lint | `mise exec -- golangci-lint run ./...` | `0 issues.` |
| Format | `gofmt -l .` | no output |

Lint **must** go through `mise exec --`. A stale global golangci-lint 2.11.4 earlier on PATH reports
false typecheck errors; the pinned version is 2.15.0 in `mise.toml`. The `go.mod` directive is
`go 1.26.0`, so do not use APIs newer than Go 1.26.

## Scope

**In scope** (the only files you should modify):
- `internal/m4a/atoms.go`
- `internal/m4a/metadata.go`, `internal/m4a/audiobook.go`, `internal/m4a/chapters.go`, `internal/m4a/artwork.go` (call-site argument only, plus removing the dead guard in `artwork.go`)
- `internal/m4a/atoms_test.go`, `metadata_test.go`, `audiobook_test.go`, `chapters_test.go`, `technical_test.go` (signature update)
- `internal/m4a/bounds_test.go` (create)

**Out of scope** (do NOT touch):
- Allocation caps on length fields (`make([]byte, n)` from untrusted sizes): that is plan
  `002-allocation-caps.md`.
- Adding `ctx` checks inside loops: that is plan `003-context-cancellation.md`. This plan makes the
  loop terminate on its own, which is the right fix whether or not 003 lands.
- `cmd/atom-dump-tool/atom-dump.go`: it has the same bug (`:74` `offset += int64(atomSize)`, with
  no upper bound). It is a developer tool, not library code. It is deferred; see Maintenance notes.
- `SafeReader` in `internal/binary/`: no accessor is needed, because callers already know the
  container end.
- Implementing `size32 == 0` = "to end of file": a deliberate non-change, see above.

## Git workflow

- Branch: `advisor/023-mp4-atom-bounds`
- Commit style: gitmoji, single line, no body, no attribution (repo examples:
  `⬆️ Bump Go 1.27, x/sync 0.24, golangci-lint 2.15 and CI actions`). Suggested:
  `🔒 Reject MP4 atoms that overrun their container`.
- Do NOT push or open a PR.

## Steps

### Step 1: Write the failing regression test first

Create `internal/m4a/bounds_test.go`. Run each probe in a goroutine with a timeout, so that a
regression makes the test fail instead of hanging the whole suite:

```go
package m4a

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"testing"
	"time"

	audiobinary "github.com/simonhull/audiometa/internal/binary"
	"github.com/simonhull/audiometa/internal/types"
)

// hugeExtendedAtomFile is a 32-byte file: a 16-byte ftyp atom followed by a
// "free" atom with size32=1 and extended size 0xFFFFFFFFFFFFFFF0 (-16 as int64).
func hugeExtendedAtomFile() []byte {
	buf := &bytes.Buffer{}
	binary.Write(buf, binary.BigEndian, uint32(16))
	buf.WriteString("ftyp")
	buf.WriteString("isom")
	binary.Write(buf, binary.BigEndian, uint32(0))
	binary.Write(buf, binary.BigEndian, uint32(1))
	buf.WriteString("free")
	binary.Write(buf, binary.BigEndian, uint64(0xFFFFFFFFFFFFFFF0))
	return buf.Bytes()
}

func TestFindAtom_RejectsSizeBeyondParent(t *testing.T) {
	data := hugeExtendedAtomFile()
	sr := audiobinary.NewSafeReader(bytes.NewReader(data), int64(len(data)), "huge.m4a")

	done := make(chan error, 1)
	go func() {
		_, err := findAtom(sr, 0, int64(len(data)), "moov")
		done <- err
	}()

	select {
	case err := <-done:
		var corrupted *types.CorruptedFileError
		if !errors.As(err, &corrupted) {
			t.Fatalf("expected *types.CorruptedFileError, got %T: %v", err, err)
		}
		if corrupted.Offset != 16 {
			t.Errorf("expected offset 16, got %d", corrupted.Offset)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("findAtom did not return within 2s: atom size overflow loops forever")
	}
}

func TestParse_HugeExtendedAtomTerminates(t *testing.T) {
	data := hugeExtendedAtomFile()

	done := make(chan error, 1)
	go func() {
		_, err := (&parser{}).Parse(context.Background(), bytes.NewReader(data), int64(len(data)), "huge.m4a")
		done <- err
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Parse should degrade gracefully, got error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Parse did not return within 2s")
	}
}
```

(`Parse` returns `file, nil` when `moov` cannot be found, so "no error" is the expected graceful
result.)

**Verify**: `go test ./internal/m4a/ -run 'HugeExtended|BeyondParent' -v` → both tests FAIL with
`did not return within 2s`. This was confirmed on a scratch copy at `aca9750`; the leaked goroutines
spin until the test binary exits, which is expected. If either test PASSES before your fix, STOP:
the bug has already been fixed elsewhere.

### Step 2: Give `readAtomHeader` the parent end and validate against it

In `internal/m4a/atoms.go`, change the signature and the validation block. Target shape:

```go
// readAtomHeader reads the atom header at offset. parentEnd is the exclusive end
// of the enclosing container (the file size for top-level atoms). An atom that
// does not fit inside its parent is rejected, so a caller that advances by
// atom.Size always moves strictly forward and never past parentEnd.
func readAtomHeader(sr *binary.SafeReader, offset, parentEnd int64) (*Atom, error) {
	// ... size32 / type / extended-size reads unchanged ...

	// Validate atom size: at least its own header, at most what the parent has left.
	minSize := uint64(8)
	if atom.Extended {
		minSize = 16
	}
	if atom.Size < minSize {
		return nil, &types.CorruptedFileError{
			Offset: offset,
			Reason: fmt.Sprintf("invalid atom size %d (minimum is %d)", atom.Size, minSize),
		}
	}
	if offset >= parentEnd || atom.Size > uint64(parentEnd-offset) {
		return nil, &types.CorruptedFileError{
			Offset: offset,
			Reason: fmt.Sprintf("atom %q size %d exceeds its container (%d bytes remain)", atom.Type, atom.Size, parentEnd-offset),
		}
	}

	return atom, nil
}
```

Why this is enough. The check `offset >= parentEnd` comes first, so `parentEnd-offset` is positive
and the `uint64` conversion is safe. After both checks, `8 ≤ Size ≤ parentEnd-offset ≤ MaxInt64`.
So `int64(atom.Size)` is positive, and `offset + Size ≤ parentEnd`. Every loop that advances by
`Size` therefore makes progress of at least 8 bytes and stops at `end`. The `minSize = 16` for
extended atoms also rejects an extended atom whose size cannot even hold its own 16-byte header.

In `findAtom`, pass `end` and delete the unreachable zero-size guard:

```go
	for offset < end {
		atom, err := readAtomHeader(sr, offset, end)
		if err != nil {
			return nil, err
		}

		if atom.Type == atomType {
			return atom, nil
		}

		// readAtomHeader guarantees 8 <= atom.Size <= end-offset.
		offset += int64(atom.Size)
	}
```

**Verify**: `go build ./internal/m4a/` → fails, but only with "not enough arguments in call to
readAtomHeader" errors in `metadata.go`, `audiobook.go`, `chapters.go`, and `artwork.go`. This is
expected; Step 3 fixes them.

### Step 3: Pass the container end at every loop site

At each of the six call sites in the table above, change `readAtomHeader(sr, offset)` to
`readAtomHeader(sr, offset, end)`. Each site already has a local `end` computed as
`offset + int64(<container>.DataSize())`, so no other change is needed. In `artwork.go`, also delete
the now-unreachable guard at `:81-84`:

```go
		// Prevent infinite loop
		if dataAtom.Size == 0 {
			break
		}
```

Do not change the per-site error handling. Some sites `return err`, others `break`; keep each as it
is.

**Verify**: `grep -n 'readAtomHeader(sr, offset)' internal/m4a/*.go` → no matches;
`grep -n 'Size == 0' internal/m4a/*.go | grep -v _test` → no matches; `go build ./...` → exit 0.

### Step 4: Update the existing test call sites

`go vet ./internal/m4a/` will now flag 23 calls of `readAtomHeader(sr, 0)` in `atoms_test.go`,
`metadata_test.go`, `audiobook_test.go`, `chapters_test.go`, and `technical_test.go`. In every case,
the line directly above builds the reader as
`audiobinary.NewSafeReader(bytes.NewReader(X), int64(len(X)), ...)`. Pass that same length, for
example `readAtomHeader(sr, 0, int64(len(moov)))`.

**One exception:** `TestReadAtomHeader_Extended` (`atoms_test.go:59-87`) builds a 16-byte buffer
that declares an `mdat` of size 1000. That test is about header decoding, not containment, so pass
`readAtomHeader(sr, 0, 1000)` and leave its assertions unchanged.

**Verify**: `go vet ./...` → exit 0; `go test ./internal/m4a/ -v 2>&1 | grep -c '^--- FAIL'` → `0`.
The two Step 1 tests now PASS.

### Step 5: Full gates

**Verify**: `gofmt -l .` → no output; `go test -race ./...` → all `ok`;
`mise exec -- golangci-lint run ./...` → `0 issues.`

## Test plan

- New: `internal/m4a/bounds_test.go`, which drives the exact 32-byte reproduction through
  `findAtom` (asserting `*types.CorruptedFileError` at `Offset == 16`) and through `Parse`
  (asserting it returns, with nil error). Both tests use a 2 s goroutine timeout.
- Optional extra case, recommended: a `TestReadAtomHeader_ExceedsParent` table test in
  `atoms_test.go`, modelled on `TestReadAtomHeader_InvalidSize` (`atoms_test.go:101-115`). Cover
  (a) a 32-bit size larger than the remaining bytes, (b) an extended size of 12, which is below the
  16-byte header, and (c) `offset == parentEnd`. Each expects a non-nil error.
- Existing tests: all must pass with only the mechanical signature update from Step 4.
- Pattern: `atoms_test.go`, which uses `createMockAtom` with `audiobinary.NewSafeReader(bytes.NewReader(data), int64(len(data)), "test.m4b")`.

## Done criteria

- [ ] `go build ./...`, `go vet ./...` exit 0; `gofmt -l .` is empty
- [ ] `go test -race ./...` all `ok`, including `TestFindAtom_RejectsSizeBeyondParent` and `TestParse_HugeExtendedAtomTerminates`
- [ ] `mise exec -- golangci-lint run ./...` → `0 issues.`
- [ ] `grep -n 'readAtomHeader(sr, offset)' internal/m4a/*.go` → no matches
- [ ] `git status` shows only the in-scope files modified or added
- [ ] `plans/README.md` status row for 023 added/updated

## STOP conditions

- The Step 1 tests pass before any fix. That means the code drifted or the bug is already fixed.
- After Step 4, an existing test fails with "exceeds its container". That means a fixture declares
  an atom larger than its parent. Do not loosen the check. Report the test name and the fixture
  builder, because it may reveal a real-world layout this plan did not anticipate.
- Some call site has no local container end available, i.e. the drift check missed a new caller.
  Report it instead of passing a guessed bound such as `math.MaxInt64`.
- The fix seems to need changes in `internal/binary` or any file outside the in-scope list.

## Maintenance notes

- **Behavior change.** An atom that overruns its container used to be skipped past or returned. It
  now produces a `CorruptedFileError`. Callers treat `findAtom` errors as "not found" (for example
  `Parse` returns basic file info when `moov` is not found), so a truncated download whose `moov`
  sits before `mdat` still parses. If `moov` itself is truncated, it was already unreadable.
  Real-file corpus checks (plan 001, when it lands) should confirm that no well-formed sample now
  warns.
- **`size32 == 0` stays rejected.** If streamed MP4s ever need support (an `mdat` written with size
  0 as the last atom), implement it in `readAtomHeader` as `atom.Size = uint64(parentEnd - offset)`.
  This is now trivial because `parentEnd` is in hand. Note that such an atom is almost always `mdat`
  placed after `moov`, so the current rejection only matters when `moov` comes after it, which is
  impossible for a valid size-0 atom.
- **Deferred:** `cmd/atom-dump-tool/atom-dump.go:50-78` has the identical unbounded advance
  (`offset += int64(atomSize)` at `:74`). It is a debugging tool that is never imported by the
  library. It needs a two-line fix of the same shape; do it in a follow-up rather than widening this
  plan.
- Plans 002 (allocation caps) and 003 (context checkpoints) touch the same `internal/m4a` files.
  Expect trivial textual conflicts at the loop sites, not semantic ones. Plan 003's `ctx` checks
  complement this fix but do not replace it.
- Reviewer: confirm that every `offset +=` in `internal/m4a` follows a `readAtomHeader(…, end)` with
  the loop's own `end`. `grep -n 'offset += int64(' internal/m4a/*.go` lists them.
