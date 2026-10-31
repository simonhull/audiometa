# Plan 003: Honor context cancellation inside parse loops

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving on. If a
> STOP condition occurs, stop and report — do not improvise. When done, update
> the status row for this plan in `plans/README.md`.
>
> **Drift check (run first)**: `git diff --stat aca9750..HEAD -- internal/ file.go`
> If any file cited below changed, compare against the excerpts before proceeding;
> on a mismatch, STOP.

## Status

- **Priority**: P1
- **Effort**: M
- **Risk**: LOW (adds periodic cancellation checks; only changes behavior under an already-canceled context)
- **Depends on**: 001
- **Category**: bug / security (makes the documented timeout contract real)
- **Planned at**: commit `aca9750`, 2026-10-30 (refreshed from 7b22897, 2026-07-01)

## Why this matters

The public docs promise that parsers "yield to cancellation at major parse boundaries
(block headers, atom boundaries, frame walks)" — `file.go:227-231` and
`internal/registry/registry.go:16-18`. **This is false.** Each parser calls `ctx.Err()`
exactly once at entry and never again; no parse loop re-checks it (verified by grep — the
only `ctx.Err()` calls in production are at `*/parser.go` entry points and `file.go:246`).
So `OpenContext`/`OpenMany` with a timeout **cannot** interrupt a long or malicious parse —
the goroutine runs to completion regardless. This is the mitigation an operator reaches for
against the pathological inputs plans 002 and 004 address, and today it does nothing. This
plan threads `ctx` into the hot loops and checks it periodically, making the contract true.

## Current state

Entry-only checks (verified): `internal/flac/parser.go:35`, `internal/mp3/parser.go:18`,
`internal/m4a/parser.go:58`, `internal/ogg/parser.go:26`. The documented promise:

```go
// file.go:226-231
// OpenContext opens a file with context-aware cancellation.
//
// The context is checked before opening the file and is threaded into each
// parser, which yields to cancellation at major parse boundaries (block
// headers, atom boundaries, frame walks). ...
```

The loops that need checks (none reference `ctx` today):
- **FLAC**: `internal/flac/parser.go:66` `for offset < size {` (metadata-block walk).
- **MP3**: `internal/mp3/id3v2.go:149` `for offset < tagEnd {` (frame walk), and
  `internal/mp3/technical.go:28` `for frameOffset < fileSize-4 {` (frame sync scan — also plan 004).
- **M4A**: `internal/m4a/parser.go` atom lookups plus the chapter table loops in
  `internal/m4a/chapters.go` (`parseChapterTimings`, `parseSampleSizes`, `parseChunkOffsets`).
- **Ogg**: `internal/ogg/parser.go:60` page-read loop (only 3 pages — low priority, but check once).

`Parse` signatures already carry `ctx context.Context` (see `internal/registry/registry.go:18`).
The lint config enables `containedctx` and `fatcontext` — do **not** store ctx in a struct;
pass it as a function argument (Go convention, and `context-as-argument` revive rule is on).

## Commands you will need

| Purpose | Command | Expected |
|---|---|---|
| Build | `go build ./...` | exit 0 |
| Tests | `go test ./...` | all pass |
| Race | `go test -race ./...` | all pass |
| Lint | `mise exec -- golangci-lint run ./...` | exit 0, prints `0 issues.` (watch for `containedctx`/`fatcontext`) |

> **Lint note**: run lint via `mise exec -- golangci-lint run ./...` so the mise-pinned
> golangci-lint v2.15.0 is used. A stale global golangci-lint (e.g. 2.11.4) on PATH reports
> false "export data version" typecheck errors under Go 1.27.

## Scope

**In scope**:
- `internal/flac/parser.go`
- `internal/mp3/parser.go`, `internal/mp3/id3v2.go`, `internal/mp3/technical.go`
- `internal/m4a/parser.go`, `internal/m4a/chapters.go`
- `internal/ogg/parser.go`
- New test file: `file_cancel_test.go` already exists at root — **add** a mid-parse
  cancellation test there (or a new `*_cancel_test.go` in an internal package).

**Out of scope**:
- `internal/vorbis/*`, `internal/parsing/*` — pure in-memory helpers over already-read
  bytes; no long loops over file I/O. Leave them.
- Do not change the `Parse` interface signature (it already has `ctx`).
- Do not alter the MP3 scan's *bounding* logic — that's plan 004. Here you only add a
  cancellation check to the existing loop.

## Git workflow

- Branch: `advisor/003-context-cancellation`
- Commit style: gitmoji single-line, no body, no attribution (e.g. `♻️ Check context inside parse loops`).
- Do NOT push or open a PR unless instructed.

## Steps

### Step 1: Add a cheap periodic-check helper

Add a small helper (in `internal/binary` or a new `internal/parsing` function — pick
`internal/binary` since every parser already imports it). It checks `ctx.Err()` only every
N iterations to avoid per-iteration overhead:

```go
// internal/binary/ctx.go (new file)
package binary

import "context"

// CtxCheckInterval is how often loops should test for cancellation.
const CtxCheckInterval = 1024

// Canceled returns ctx.Err() when i is a multiple of CtxCheckInterval, else nil.
// Use in tight parse loops: if err := binary.Canceled(ctx, i); err != nil { return ..., err }.
func Canceled(ctx context.Context, i int) error {
	if i%CtxCheckInterval == 0 {
		return ctx.Err()
	}
	return nil
}
```

**Verify**: `go build ./internal/binary/` → exit 0.

### Step 2: Thread `ctx` into the loop-bearing functions and check it

For each in-scope loop, pass `ctx` down from `Parse` and add the check. Because the current
helper functions don't take `ctx`, add the parameter and update call sites. Example for the
MP3 frame walk:

- `internal/mp3/id3v2.go`: change `parseID3v2Frames(sr, file, header, offset)` to
  `parseID3v2Frames(ctx, sr, file, header, offset)`; in the `for offset < tagEnd {` loop add a
  counter `i` and `if err := binutil.Canceled(ctx, i); err != nil { return chapters }` (or plumb
  the error out — simplest is to stop the walk and return what's parsed so far, matching the
  existing graceful-degradation style). Thread `ctx` from `parseID3v2(ctx, sr, file)` and from
  `parser.Parse` (`internal/mp3/parser.go:35`).
- `internal/mp3/technical.go:28`: add `ctx` param to `parseTechnicalInfo`, check `Canceled(ctx, i)`
  in the scan loop, return `ctx.Err()` on cancellation.
- `internal/flac/parser.go:66`: the loop is inside `Parse`, which already has `ctx` — add the
  counter and `if err := binary.Canceled(ctx, i); err != nil { return nil, err }`.
- `internal/m4a/chapters.go`: thread `ctx` into `parseChapterTimings`/`parseSampleSizes`/
  `parseChunkOffsets` and check in each loop. These are called from `parseChapters` → thread
  `ctx` from `parser.Parse` (`internal/m4a/parser.go:128`).
- `internal/ogg/parser.go:60`: only 3 iterations, but add one `ctx.Err()` check at loop top for
  consistency.

Keep each change mechanical; do not restructure the functions.

**Verify**: `go build ./...` → exit 0; `go test ./...` → all pass.

### Step 3: Add a mid-parse cancellation test

Add a test that constructs (or takes from `testdata/`) an input large enough that the parse
loop iterates many times, cancels the context *before* calling `Parse`, and asserts `Parse`
returns `context.Canceled`. The already-canceled case is the deterministic, race-free way to
prove the loop observes cancellation (a mid-flight cancel is inherently timing-dependent).

Model on `file_cancel_test.go` (already in the repo). Add e.g.:

```go
func TestParse_CanceledMidWalk(t *testing.T) {
	// A file with many ID3 frames (or many FLAC blocks) so the loop would iterate.
	data := createManyFrameMP3(t) // build enough frames that i crosses CtxCheckInterval, or lower the interval in a test build tag
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p := &parser{}
	_, err := p.Parse(ctx, bytes.NewReader(data), int64(len(data)), "x.mp3")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
}
```

Because `Canceled` checks at `i % 1024 == 0` (which includes `i == 0`), an already-canceled
context is caught on the **first** iteration — so the test does not need thousands of frames.
Confirm the first-iteration check by keeping `i` starting at 0.

**Verify**: `go test -run Cancel ./...` → PASS; `go test -race ./...` → all pass.

## Test plan

- New test(s): already-canceled context returns `context.Canceled` from `Parse` for at least
  MP3 and M4A (the two with the deepest loops). Reuse `file_cancel_test.go` helpers where possible.
- Pattern: `file_cancel_test.go:52` `TestOpenMany_Cancellation`.
- Verification: `go test -race ./...` all pass.

## Done criteria

- [ ] `go build ./...`, `go vet ./...`, `mise exec -- golangci-lint run ./...` all exit 0 (no `containedctx`/`fatcontext` violations)
- [ ] `go test -race ./...` exits 0 including the new cancellation test(s)
- [ ] `grep -rn "Canceled(ctx" internal/` shows checks in flac, mp3 (id3v2 + technical), m4a chapters
- [ ] The doc claims in `file.go:227-231` and `registry.go:16-18` are now true (a reviewer can trace ctx into each loop)
- [ ] `plans/README.md` status row for 003 updated

## STOP conditions

- A loop function's signature can't take `ctx` without also changing an out-of-scope caller
  chain you can't see — STOP and report the call graph.
- `containedctx`/`fatcontext` lint fires — you likely stored ctx in a struct; revert to passing
  it as an argument. If you cannot satisfy the linter, STOP.
- Adding the check changes behavior for a non-canceled context (a valid file now errors) — STOP.

## Maintenance notes

- `CtxCheckInterval = 1024` trades responsiveness against overhead; if a single iteration can be
  very expensive (e.g. a huge allocation — but plan 002 caps those), lower it or check every iteration there.
- Any new file-walking loop added later should use `binary.Canceled(ctx, i)`; a reviewer should
  flag new `for` loops over file offsets that don't.
- This plan makes cancellation *observable*; plans 002/004 make the underlying work *bounded*.
  Both are needed — keep them in sync.
