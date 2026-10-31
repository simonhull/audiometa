# Plan 008: Implement WithMaxArtworkSize enforcement

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving on. If a
> STOP condition occurs, stop and report — do not improvise. When done, update
> the status row for this plan in `plans/README.md`.
>
> **Drift check (run first)**: `git diff --stat aca9750..HEAD -- options.go file.go internal/registry/ internal/mp3/artwork.go internal/m4a/artwork.go internal/ogg/artwork.go internal/flac/parser.go`
> If any changed, compare against the excerpts below; on a mismatch, STOP.

## Status

- **Priority**: P2
- **Effort**: M
- **Risk**: MED (requires threading options into the artwork path; public behavior gains a documented limit)
- **Depends on**: 001
- **Category**: bug / security-adjacent (documented option is a silent no-op)
- **Planned at**: commit `aca9750`, 2026-10-30 (refreshed from 7b22897, 2026-07-01)

## Why this matters

`WithMaxArtworkSize` is a public, documented option whose godoc promises: *"If artwork exceeds
this size (in bytes), it will be skipped with a warning. This protects against excessively large
embedded images."* (`options.go:87-92`). **The field is never read anywhere** — verified by grep:
`maxArtworkSize` appears only in `options.go` (field at :20, default at :29, setter at :102).
`ExtractArtworkContext` (`file.go:201-224`) never receives the options, and no extractor consults
a limit. A caller who sets this option to bound memory gets no protection at all. This plan wires
the option through to artwork extraction and enforces it, making the documented contract true.

## Current state

**The no-op option** — `options.go:16-21, 100-104` (verified):

```go
type openOptions struct {
	strictParsing  bool
	preloadArtwork bool
	ignoreWarnings bool
	maxArtworkSize int  // Maximum artwork size in bytes (0 = no limit)
}
...
func WithMaxArtworkSize(bytes int) Option {
	return func(o *openOptions) { o.maxArtworkSize = bytes }
}
```

**Where options die** — `file.go:94-98`: `openWithContext` builds the `File` but does not retain
`options`; `file.go:201-224` `ExtractArtworkContext` has no access to them:

```go
file := &File{
	File:   *typesFile.file,
	reader: f,
	parser: typesFile.parser,
}
```

**The extractor interface** — `internal/registry/registry.go:22-26`:

```go
type ArtworkExtractor interface {
	ExtractArtwork(ctx context.Context, r io.ReaderAt, size int64, path string) ([]types.Artwork, error)
}
```

Extractors: `internal/mp3/parser.go:65` (delegates to `internal/mp3/artwork.go`),
`internal/m4a/parser.go:154`, `internal/ogg/artwork.go:20`. FLAC pictures are parsed via
`internal/flac/parser.go:319` `parsePicture` on the lazy path.

**Caching quirk to fix while here** — `file.go:201-224`: cache check is `if f.artwork != nil`,
so a file with *no* artwork re-runs extraction on every call. When you touch this function,
also cache the empty result (e.g. store a non-nil empty slice after a successful extraction).

## Commands you will need

| Purpose | Command | Expected |
|---|---|---|
| Build | `go build ./...` | exit 0 |
| Tests | `go test ./...` | all pass |
| Race | `go test -race ./...` | all pass |
| Lint | `mise exec -- golangci-lint run ./...` | exit 0, prints `0 issues.` |

> **Lint note**: run lint via `mise exec -- golangci-lint run ./...` so the mise-pinned
> golangci-lint v2.15.0 is used. A stale global golangci-lint (e.g. 2.11.4) on PATH reports
> false "export data version" typecheck errors under Go 1.27.

## Scope

**In scope**:
- `options.go` (doc tweaks only if behavior detail changes)
- `file.go` — retain the limit on `File`, enforce in `ExtractArtworkContext`, fix empty-result caching
- `internal/mp3/artwork.go`, `internal/m4a/artwork.go`, `internal/ogg/artwork.go`,
  `internal/flac/parser.go` — only if you choose per-extractor enforcement (see Step 1 decision)
- Tests: options behavior tests (currently 0% coverage on all four options)

**Out of scope**:
- Changing the exported `ArtworkExtractor` interface (`registry.go`) — it is aliased publicly in
  `file.go:328`; changing its signature is a breaking API change. The chosen design must avoid this.
- `WithStrictParsing`/`WithIgnoreWarnings` semantics (plan 015 touches strict-mode inputs).

## Git workflow

- Branch: `advisor/008-maxartworksize-enforcement`
- Commit style: gitmoji single-line, no body, no attribution (e.g. `🐛 Enforce WithMaxArtworkSize`).
- Do NOT push or open a PR unless instructed.

## Steps

### Step 1: Enforce centrally in `ExtractArtworkContext` (chosen design)

Because the `ArtworkExtractor` interface is public and must not change, enforce the limit
**after** extraction in `file.go` — filter out oversized images and add a warning. This honors
the documented "skipped with a warning" contract without touching the interface. (It does not
avoid the transient allocation inside the extractor — plan 002's `MaxAllocBytes` cap bounds the
worst case there; note this tradeoff in a code comment.)

1. Retain the limit on `File`: add field `maxArtworkSize int` to the `File` struct in `file.go`,
   set it in `openWithContext`:
   ```go
   file := &File{
       File:           *typesFile.file,
       reader:         f,
       parser:         typesFile.parser,
       maxArtworkSize: options.maxArtworkSize,
   }
   ```
2. Enforce in `ExtractArtworkContext` after a successful extraction:
   ```go
   artwork, err := extractor.ExtractArtwork(ctx, f.reader, f.Size, f.Path)
   if err != nil {
       return nil, fmt.Errorf("extract artwork: %w", err)
   }
   if f.maxArtworkSize > 0 {
       kept := artwork[:0:0]
       for _, a := range artwork {
           if len(a.Data) > f.maxArtworkSize {
               f.Warnings = append(f.Warnings, Warning{
                   Stage:   StageArtwork,
                   Message: fmt.Sprintf("artwork %q (%d bytes) exceeds max size %d; skipped", a.Description, len(a.Data), f.maxArtworkSize),
               })
               continue
           }
           kept = append(kept, a)
       }
       artwork = kept
   }
   if artwork == nil {
       artwork = []Artwork{} // cache the empty result too (fixes re-extract-on-every-call)
   }
   f.artwork = artwork
   return artwork, nil
   ```

**Verify**: `go build ./...` → exit 0.

### Step 2: Update the option's godoc to match the implementation

`options.go:87-99` — keep the promise but make it precise: the artwork is skipped from the
returned slice and a warning is appended to `File.Warnings`; the limit applies per image.
If plan 002 landed, mention that a global hard cap (`binary.MaxAllocBytes`) bounds parsing
regardless of this option.

**Verify**: `go vet ./...` → exit 0 (godoc formatting), `mise exec -- golangci-lint run ./...` → exit 0.

### Step 3: Test all four options (they are currently 0% covered)

Create `options_test.go` (package `audiometa_test`) using plan 001's corpus:

- `WithMaxArtworkSize(1)`: open an artwork-bearing fixture, `ExtractArtwork()` → returned slice
  excludes the oversized image AND `File.Warnings` contains an "exceeds max size" entry.
- `WithMaxArtworkSize(0)` (default): image is returned; no size warning.
- `WithArtworkPreload()`: after `Open`, a second `ExtractArtwork()` returns cached data
  (and with the Step 1 caching fix, so does a no-artwork file — assert extraction is not re-run
  by asserting the same slice length and nil error twice; exact identity is fine too).
- `WithIgnoreWarnings()`: `File.Warnings` is empty even for a fixture that produces warnings.
- `WithStrictParsing()`: a warning-producing fixture returns an error. (If no fixture produces
  a warning naturally, craft one from the synthetic builders — e.g. the truncated-frame MP3 from
  plan 002's tests.)

**Verify**: `go test -race ./...` → all pass.

## Test plan

- New file `options_test.go` covering the five cases above.
- Pattern: `audiometa_corpus_test.go` (plan 001) for public-API usage.
- Verification: `go test -race ./...` all pass; per-function coverage for `options.go` functions > 0%.

## Done criteria

- [ ] `go build ./...`, `go vet ./...`, `mise exec -- golangci-lint run ./...` all exit 0
- [ ] `go test -race ./...` exits 0 including the new options tests
- [ ] `grep -n "maxArtworkSize" file.go` shows the field consumed in `ExtractArtworkContext`
- [ ] A no-artwork file's second `ExtractArtwork()` call does not re-run extraction (empty result cached)
- [ ] Godoc for `WithMaxArtworkSize` matches actual behavior
- [ ] `plans/README.md` status row for 008 updated

## STOP conditions

- Enforcing centrally requires changing the public `ArtworkExtractor` alias or `registry.go`
  interface — STOP; that is an API break this plan explicitly avoids.
- An existing test depends on `f.artwork == nil` semantics after extraction — STOP and report.
- `Warning`/`Artwork` struct fields differ from those used in the snippet (e.g. no `Description`
  field) — check `internal/types/artwork.go` and adapt the message fields; if the struct has
  drifted materially, STOP.

## Maintenance notes

- The limit filters *after* the extractor allocates; the hard parse-time cap is plan 002's
  `MaxAllocBytes`. If a future refactor threads options into extractors (e.g. when write support
  changes the interfaces anyway), move enforcement inside to avoid the transient allocation.
- Reviewer: check the warning wording is stable (tests will pin it) and that `OpenMany` — which
  passes no options (`file.go:309`) — still behaves (limit 0 = no filtering).
- Deferred follow-up: `OpenMany` accepting per-call options is a small API addition worth
  considering with plan 020's API review; noted, not in scope.
