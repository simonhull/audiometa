# Plan 025: Parse M4A technical info and chapters even when udta/meta/ilst is absent

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving on. If a
> STOP condition occurs, stop and report — do not improvise. When done, update
> the status row for this plan in `plans/README.md`.
>
> **Drift check (run first)**: `git diff --stat aca9750..HEAD -- internal/m4a/parser.go internal/m4a/atoms.go internal/m4a/artwork.go`
> If any of these changed, compare the "Current state" excerpts below against the live code. On a
> mismatch, STOP. (If plan 023 has landed, `atoms.go`/`artwork.go` will show its changes:
> `readAtomHeader` takes a third `parentEnd` argument. That is expected and compatible; see Step 1.)

## Status

- **Priority**: P1
- **Effort**: S
- **Risk**: LOW (only adds work on files that currently return early; tagged files follow the same path as before)
- **Depends on**: none
- **Category**: bug
- **Planned at**: commit `aca9750`, 2026-10-30

## Why this matters

`m4a.Parse` returns as soon as it fails to find `udta`, `meta`, or `ilst` inside `moov`. That
happens before it ever calls `parseTechnicalInfo` or `parseChapters`. Any M4A/M4B without iTunes
tags therefore comes back with `Duration == 0`, no codec, no sample rate or channels, no chapters,
and no warning. Untagged files are common: freshly ripped or encoded audiobooks, `ffmpeg` output
without `-metadata`, files stripped by tools. Nero `chpl` chapters live in `udta` with no
`meta`/`ilst` at all, so a chaptered-but-untagged M4B loses its chapters too. For an audiobook
server, a zero duration breaks progress tracking and chapter navigation. After this plan, metadata
lookup is best-effort: technical info and chapters always run once `moov` is found.

## Current state

- `internal/m4a/parser.go` — `detectM4Format` (`:15-54`), `Parse` (`:56-151`), `ExtractArtwork` (`:153-160`).
- `internal/m4a/artwork.go` — `extractArtwork` (`:16-88`) repeats the same moov→udta→meta→ilst walk (`:28-48`).
- `internal/m4a/atoms.go` — `findAtom` (`:102-129`); "not found" is an unwrapped `fmt.Errorf` at `:128`.

`internal/m4a/parser.go:77-151` (verified). Note the three early returns:

```go
	// Find moov atom (movie container)
	moovAtom, err := findAtom(sr, 0, size, "moov")
	if err != nil {
		// No moov atom - return basic file info
		return file, nil
	}

	// Find udta atom (user data) inside moov
	udtaAtom, err := findAtom(sr, moovAtom.DataOffset(), moovAtom.DataOffset()+int64(moovAtom.DataSize()), "udta")
	if err != nil {
		// No udta - return basic file info
		return file, nil                                   // <-- :88, skips technical + chapters
	}

	// Find meta atom inside udta
	metaAtom, err := findAtom(sr, udtaAtom.DataOffset(), udtaAtom.DataOffset()+int64(udtaAtom.DataSize()), "meta")
	if err != nil {
		// No meta - return basic file info
		return file, nil                                   // <-- :95
	}

	// meta atom has 4 bytes of version+flags before the data
	metaDataOffset := metaAtom.DataOffset() + 4
	metaDataEnd := metaAtom.DataOffset() + int64(metaAtom.DataSize())

	// Find ilst atom (iTunes metadata list) inside meta
	ilstAtom, err := findAtom(sr, metaDataOffset, metaDataEnd, "ilst")
	if err != nil {
		// No ilst - return basic file info
		return file, nil                                   // <-- :106
	}

	// Extract metadata from ilst
	if err := extractIlstMetadata(sr, ilstAtom, file); err != nil { ...StageMetadata warning... }

	// Parse technical info (duration, bitrate, codec, sample rate, channels)
	if err := parseTechnicalInfo(sr, moovAtom, file); err != nil { ...StageTechnical warning... }   // :119

	// Parse chapters
	chapters, err := parseChapters(sr, moovAtom, file.Audio.Duration)                                // :128
	...
	// Parse audiobook-specific tags (narrator, series, publisher, etc.)
	if ilstAtom != nil {                                   // :140, always true today (dead guard)
		if err := parseAudiobookTags(sr, ilstAtom, file); err != nil { ...StageMetadata warning... }
	}

	return file, nil
```

Facts that make the fix safe (verified):
- `parseTechnicalInfo(sr, moovAtom, file)` (`technical.go:12`) needs only `moov`. It finds
  `mvhd`/`trak` itself.
- `parseChapters(sr, moovAtom, dur)` (`chapters.go:11`) needs only `moov`. QuickTime chapters come
  from `trak→tref→chap`. Nero chapters come from `udta→chpl` (`chapters.go:42-58`), which does its
  own `udta` lookup and treats absence as "no chapters".
- `extractIlstMetadata` and `parseAudiobookTags` are the only consumers of `ilstAtom`.
- `findAtom` returns `fmt.Errorf("atom '%s' not found", atomType)` (`atoms.go:128`) when the type is
  simply absent. It returns a `*types.CorruptedFileError` or a `SafeReader` read error when the
  structure is broken. Today, callers cannot tell these apart.

Warning convention (used throughout `parser.go`):
`file.Warnings = append(file.Warnings, types.Warning{Stage: types.StageMetadata, Message: err.Error(), Err: err})`.

Test fixtures: `createMockAtom` (`atoms_test.go:12`), `createMvhdAtom(version, timescale, duration)`
(`technical_test.go:14`, version 0 only), `createChplAtom(...)` (`chapters_test.go:13`), and the
whole-file builder `createMinimalM4B` (`parser_test.go:14-56`), which shows the ftyp + nested
layout.

## Commands you will need

| Purpose | Command | Expected |
|---|---|---|
| Build | `go build ./...` | exit 0 |
| Vet | `go vet ./...` | exit 0 |
| Targeted tests | `go test ./internal/m4a/ -run 'Untagged\|Parse_\|ExtractArtwork' -v` | all PASS |
| All tests | `go test ./...` and `go test -race ./...` | all `ok` |
| Lint | `mise exec -- golangci-lint run ./...` | `0 issues.` |
| Format | `gofmt -l .` | no output |

Lint **must** go through `mise exec --`. A stale global golangci-lint 2.11.4 on PATH gives false
typecheck errors; the pinned version is 2.15.0. `go.mod` says `go 1.26.0`, so use no newer APIs.

## Scope

**In scope**:
- `internal/m4a/parser.go`
- `internal/m4a/atoms.go` (add a not-found sentinel only)
- `internal/m4a/artwork.go` (Step 4, optional: reuse the shared helper)
- `internal/m4a/untagged_test.go` (create)

**Out of scope**:
- Anything about how `ilst` items map to tag fields: that is plan 029.
- mvhd duration overflow: that is plan `014-mvhd-overflow.md`.
- Which `trak` is chosen for technical info: that is plan `019-container-correctness.md`, part D.
- `detectM4Format` and format/brand detection: that is plan 030.
- Changing what `Parse` does when `moov` itself is missing (it returns basic info; keep that).

## Git workflow

- Branch: `advisor/025-m4a-untagged-technical`
- Commit style: gitmoji, single line, no body, no attribution (repo example:
  `♻️ Modernize for Go 1.26, add context plumbing, refresh tooling`). Suggested:
  `🐛 Parse M4A duration and chapters when iTunes tags are absent`.
- Do NOT push or open a PR.

## Steps

### Step 1: Write the failing tests

Create `internal/m4a/untagged_test.go`:

```go
package m4a

import (
	"bytes"
	"context"
	"encoding/binary"
	"testing"
	"time"

	"github.com/simonhull/audiometa/internal/types"
)

func m4bFtyp() []byte {
	ftyp := &bytes.Buffer{}
	ftyp.WriteString("M4B ")
	binary.Write(ftyp, binary.BigEndian, uint32(0))
	ftyp.WriteString("M4B ")
	return createMockAtom("ftyp", ftyp.Bytes())
}

func parseBytes(t *testing.T, data []byte) *types.File {
	t.Helper()
	file, err := (&parser{}).Parse(context.Background(), bytes.NewReader(data), int64(len(data)), "untagged.m4b")
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	return file
}

func TestParse_UntaggedStillParsesTechnicalAndChapters(t *testing.T) {
	mvhd := createMvhdAtom(0, 1000, 180_000) // 180 s
	chpl := createChplAtom([]struct {
		time  int64
		title string
	}{{0, "One"}, {600_000_000, "Two"}})
	metaNoIlst := createMockAtom("meta", []byte{0, 0, 0, 0}) // version+flags only

	tests := []struct {
		name         string
		moovChildren [][]byte
		wantChapters int
	}{
		{"no udta", [][]byte{mvhd}, 0},
		{"udta without meta", [][]byte{mvhd, createMockAtom("udta", chpl)}, 2},
		{"meta without ilst", [][]byte{mvhd, createMockAtom("udta", append(append([]byte{}, chpl...), metaNoIlst...))}, 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			moov := createMockAtom("moov", bytes.Join(tt.moovChildren, nil))
			data := append(m4bFtyp(), moov...)

			file := parseBytes(t, data)

			if file.Audio.Duration != 180*time.Second {
				t.Errorf("Duration = %v, want 3m0s", file.Audio.Duration)
			}
			if len(file.Chapters) != tt.wantChapters {
				t.Errorf("len(Chapters) = %d, want %d", len(file.Chapters), tt.wantChapters)
			}
			if len(file.Warnings) != 0 {
				t.Errorf("expected no warnings for an untagged file, got %v", file.Warnings)
			}
		})
	}
}
```

**Verify**: `go test ./internal/m4a/ -run Untagged -v` → all three subtests FAIL with
`Duration = 0s, want 3m0s`. The two `udta` cases also report `len(Chapters) = 0, want 2`. This was
confirmed at `aca9750`.

### Step 2: Make "not found" distinguishable from "corrupt"

In `internal/m4a/atoms.go`, add `"errors"` to the imports and a sentinel near the top:

```go
// errAtomNotFound is wrapped by findAtom when the container holds no atom of
// the requested type. Callers use errors.Is to tell "absent" from "corrupt".
var errAtomNotFound = errors.New("atom not found")
```

Change the final return of `findAtom` (`:128`) to wrap it:

```go
	return nil, fmt.Errorf("atom '%s': %w", atomType, errAtomNotFound)
```

**Verify**: `go build ./...` → exit 0; `go test ./internal/m4a/` → the same failures as Step 1,
nothing new. No existing test asserts the old message text; this was checked.

### Step 3: Extract `findIlst` and make metadata lookup non-terminal in `Parse`

In `internal/m4a/parser.go`, add `"errors"` to the imports and add this helper, for example just
above `ExtractArtwork`:

```go
// findIlst navigates moov → udta → meta → ilst and returns the ilst atom.
// The returned error wraps errAtomNotFound when any level is simply absent.
func findIlst(sr *binary.SafeReader, moovAtom *Atom) (*Atom, error) {
	udtaAtom, err := findAtom(sr, moovAtom.DataOffset(), moovAtom.DataOffset()+int64(moovAtom.DataSize()), "udta")
	if err != nil {
		return nil, err
	}

	metaAtom, err := findAtom(sr, udtaAtom.DataOffset(), udtaAtom.DataOffset()+int64(udtaAtom.DataSize()), "meta")
	if err != nil {
		return nil, err
	}

	// meta is a full atom: 4 bytes of version+flags precede its children.
	metaDataOffset := metaAtom.DataOffset() + 4
	metaDataEnd := metaAtom.DataOffset() + int64(metaAtom.DataSize())

	return findAtom(sr, metaDataOffset, metaDataEnd, "ilst")
}
```

Then, in `Parse`, replace everything from `// Find udta atom (user data) inside moov` (`:84`) down
to the end of the `extractIlstMetadata` block (`:116`) with:

```go
	// Locate the iTunes metadata list. Its absence is normal (untagged file)
	// and must not stop technical info or chapters from being parsed.
	ilstAtom, err := findIlst(sr, moovAtom)
	if err != nil && !errors.Is(err, errAtomNotFound) {
		file.Warnings = append(file.Warnings, types.Warning{
			Stage:   types.StageMetadata,
			Message: err.Error(),
			Err:     err,
		})
	}

	// Extract metadata from ilst
	if ilstAtom != nil {
		if err := extractIlstMetadata(sr, ilstAtom, file); err != nil {
			file.Warnings = append(file.Warnings, types.Warning{
				Stage:   types.StageMetadata,
				Message: err.Error(),
				Err:     err,
			})
		}
	}
```

Leave the `parseTechnicalInfo`, `parseChapters`, and `if ilstAtom != nil { parseAudiobookTags … }`
blocks exactly as they are. The `ilstAtom != nil` guard is now meaningful rather than dead. Keep the
`moov` not-found early return; it is correct.

Rationale for the warning rule: a missing level is a valid untagged file and gets no warning. A
corrupt `udta`/`meta`/`ilst` (bad size, read past end of file) is a real defect worth surfacing at
`StageMetadata`, and parsing still continues.

**Verify**: `go test ./internal/m4a/ -run Untagged -v` → all three subtests PASS;
`grep -c 'return file, nil' internal/m4a/parser.go` → `2` (the `moov` not-found return and the
final return).

### Step 4 (optional, bounded): Reuse `findIlst` in `extractArtwork`

In `internal/m4a/artwork.go`, replace the udta→meta→ilst block (`:28-48`, from
`// Find udta atom (user data) inside moov` through the `ilst` lookup's `return nil, nil`) with:

```go
	ilstAtom, err := findIlst(sr, moovAtom)
	if err != nil {
		return nil, nil
	}
```

This must change nothing observable: artwork stays silent on any lookup failure, exactly as today.
Do not add warnings here, because `ExtractArtwork` returns no warnings channel. Skip this step if
plan 023 or another plan has restructured `extractArtwork` and the block no longer matches the
excerpt.

**Verify**: `go test ./internal/m4a/ -run 'ExtractArtwork' -v` → all PASS.

### Step 5: Full gates

**Verify**: `gofmt -l .` → no output; `go vet ./...` → exit 0; `go test -race ./...` → all `ok`;
`mise exec -- golangci-lint run ./...` → `0 issues.` (This was checked on a scratch copy: 0 issues.)

## Test plan

- New: `internal/m4a/untagged_test.go` with three cases: no `udta`; `udta` (with `chpl`) but no
  `meta`; `meta` but no `ilst`. Each asserts `Duration == 180s` from `mvhd`, the expected chapter
  count, and zero warnings.
- Recommended extra case: a tagged file still works. `TestParse_Success` (`parser_test.go:58`)
  already covers it and must keep passing unchanged.
- Optional: a corrupt `udta` (e.g. `udta` whose child declares a size larger than `udta`) yields
  exactly one `StageMetadata` warning while `Duration` is still populated. If plan 023 has landed,
  this produces a `CorruptedFileError`. If it has not, the result depends on the bytes, so only add
  this case after 023.
- Pattern: `parser_test.go` (`TestParse_NoMetadata`, `:164`) for driving `Parse`. An in-memory
  `bytes.NewReader` is fine in place of the temp-file dance.

## Done criteria

- [ ] `go build ./...`, `go vet ./...` exit 0; `gofmt -l .` empty
- [ ] `go test -race ./...` all `ok`, including `TestParse_UntaggedStillParsesTechnicalAndChapters` (3 subtests)
- [ ] `mise exec -- golangci-lint run ./...` → `0 issues.`
- [ ] `grep -n 'No udta - return basic file info\|No meta - return basic file info\|No ilst - return basic file info' internal/m4a/parser.go` → no matches
- [ ] `git status` shows only in-scope files changed
- [ ] `plans/README.md` status row for 025 added/updated

## STOP conditions

- The Step 1 tests already pass before Step 3: the early returns are already gone, so the plan is
  stale.
- An existing test fails after Step 3 because it expected an untagged file to have
  `Duration == 0` or no chapters. Report it. Such a test encodes the bug; do not "fix" it without
  confirmation.
- `parseTechnicalInfo` or `parseChapters` turns out to need `udta`/`ilst` (their signatures changed
  since planning).
- The Step 4 excerpt does not match `artwork.go`: skip Step 4 and say so in the report. Do not
  improvise a different refactor.

## Maintenance notes

- New observable behavior: untagged M4A/M4B files now report `Duration`, `Codec`,
  `SampleRate`/`Channels`, `Bitrate`, and chapters. A corrupt metadata container now yields a
  `StageMetadata` warning where it was silent before. Under `WithStrictParsing`, which documents
  "any warning becomes a fatal error", such files now fail to open. That is consistent with the
  option's contract, but call it out in the PR.
- `errAtomNotFound` is now the way to ask "absent or broken?" for any `findAtom` call. Plan 023's
  bounds check returns `CorruptedFileError`, which is correctly *not* `errAtomNotFound`. The two
  plans compose in either order; the only textual overlap is `atoms.go`, and it touches different
  lines.
- Plan 003 (context cancellation) adds `ctx` checks in `Parse`. If it lands first, keep its
  checkpoints between the stages; the ordering of stages here is unchanged.
- Reviewer: confirm that metadata, technical, chapters, and audiobook-tags each run independently,
  and that only `moov` absence short-circuits.
