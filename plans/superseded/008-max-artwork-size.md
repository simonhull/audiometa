# Plan 008: Implement WithMaxArtworkSize enforcement

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving on. If a
> STOP condition occurs, stop and report — do not improvise. When done, update
> the status row for this plan in `plans/README.md`.
>
> **Drift check (run first)**: `git diff --stat 7b22897..HEAD -- options.go file.go internal/registry/ internal/mp3/artwork.go internal/m4a/artwork.go internal/flac/parser.go internal/ogg/artwork.go`
> If any changed, compare against the excerpts below before proceeding; on a mismatch, STOP.

## Status

- **Priority**: P2
- **Effort**: M
- **Risk**: MED (threads a new parameter through the artwork extraction path; public behavior change for oversized art)
- **Depends on**: 001
- **Category**: bug / security (documented option is a silent no-op; it is the intended defense against oversized embedded images)
- **Planned at**: commit `7b22897`, 2026-07-01

## Why this matters

`WithMaxArtworkSize` is a public, documented option whose godoc promises: "If artwork exceeds
this size (in bytes), it will be skipped with a warning. This protects against excessively
large embedded images." **It does nothing.** The field is written in `options.go` and read
nowhere — `File.ExtractArtworkContext` never sees the options, and no extractor takes a size
limit (verified by grep: `maxArtworkSize` appears only in `options.go` lines 20/29/102). A
caller who sets the limit believes extraction is bounded; it is not. This plan wires the limit
through `Open` → `File` → the four extractors, making the documented contract true.

## Current state

**The option (write-only)** — `options.go:16-21, 100-104` (verified):

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

**The extraction path (options never reach it)** — `file.go:193-224` (verified): `File` struct
(`file.go:28-34`) holds `reader`, `parser`, `artwork` — no options. `ExtractArtworkContext` calls
`extractor.ExtractArtwork(ctx, f.reader, f.Size, f.Path)`.

**The extractor interface** — `internal/registry/registry.go:22-26`:

```go
type ArtworkExtractor interface {
	ExtractArtwork(ctx context.Context, r io.ReaderAt, size int64, path string) ([]types.Artwork, error)
}
```

Implementations: `internal/mp3/parser.go:65`, `internal/m4a/parser.go:154`, `internal/ogg/artwork.go:20`.
FLAC pictures are parsed via `internal/flac/parser.go:319` `parsePicture` reached from FLAC's
`ExtractArtwork` (find it with `grep -n "func (p \*parser) ExtractArtwork" internal/flac/`).

**Cache quirk to fix while here** — `file.go:201-224`: `if f.artwork != nil` means a file with
no artwork (`nil` result) re-runs extraction on every call. Cache a non-nil empty slice.

## Design decision (made for you — do not re-litigate)

Do **not** change the exported `ArtworkExtractor` interface (it is aliased publicly in `file.go:327-329`).
Instead:
1. Store the artwork limit on the `File` struct at open time.
2. Enforce the limit centrally in `ExtractArtworkContext`: after extraction, drop any
   `Artwork` whose `len(Data)` exceeds the limit and append a `Warning{Stage:"artwork", ...}` per drop.
3. Additionally pass the limit into extractors that can avoid even *reading* oversized data
   cheaply — only where it does not require an interface change: the MP3 extractor already has a
   100 MB hard cap (`internal/mp3/artwork.go:80`); leave format-internal caps as they are.

Central post-filtering does not prevent the read itself, but combined with plan 002's
`MaxAllocBytes` cap the read is already bounded; the option's documented contract ("skipped with
a warning") is about which artwork the caller receives. This keeps the public interface stable.
Record this trade-off in the code comment.

## Commands you will need

| Purpose | Command | Expected |
|---|---|---|
| Build | `go build ./...` | exit 0 |
| Tests | `go test ./...` | all pass |
| Race | `go test -race ./...` | all pass |
| Lint | `golangci-lint run ./...` | exit 0 |

## Scope

**In scope**:
- `file.go` — store limit on `File`, enforce in `ExtractArtworkContext`, fix the nil-cache quirk
- `options.go` — align the godoc with the implemented behavior
- Root test files (`audiometa_test.go` or new `options_test.go`) — option behavior tests

**Out of scope**:
- The `ArtworkExtractor` interface and its four implementations (no signature changes).
- `Warnings` semantics (plan 015 handles warning severity).

## Git workflow

- Branch: `advisor/008-max-artwork-size`
- Commit style: gitmoji single-line, no body, no attribution (e.g. `🐛 Enforce WithMaxArtworkSize`).
- Do NOT push or open a PR unless instructed.

## Steps

### Step 1: Carry the limit on the File struct

In `file.go`, add a field to `File` (unexported): `maxArtworkSize int`. In `openWithContext`
(after building `file := &File{...}` at `file.go:94-98`), set `file.maxArtworkSize = options.maxArtworkSize`.

**Verify**: `go build ./...` → exit 0.

### Step 2: Enforce centrally in ExtractArtworkContext + fix the cache

Replace the body of `ExtractArtworkContext` (`file.go:201-224`) with logic that (a) uses a
non-nil cached slice so no-artwork files don't re-extract, (b) filters oversized artwork with
a warning:

```go
func (f *File) ExtractArtworkContext(ctx context.Context) ([]Artwork, error) {
	if f.artwork != nil {
		return f.artwork, nil
	}
	extractor, ok := f.parser.(ArtworkExtractor)
	if !ok {
		f.artwork = []Artwork{} // cache "no support" as empty, not nil
		return nil, nil
	}
	artwork, err := extractor.ExtractArtwork(ctx, f.reader, f.Size, f.Path)
	if err != nil {
		return nil, fmt.Errorf("extract artwork: %w", err)
	}
	if f.maxArtworkSize > 0 {
		kept := artwork[:0]
		for _, a := range artwork {
			if len(a.Data) > f.maxArtworkSize {
				f.Warnings = append(f.Warnings, Warning{
					Stage:   "artwork",
					Message: fmt.Sprintf("artwork %d bytes exceeds max %d; skipped", len(a.Data), f.maxArtworkSize),
				})
				continue
			}
			kept = append(kept, a)
		}
		artwork = kept
	}
	if artwork == nil {
		artwork = []Artwork{}
	}
	f.artwork = artwork
	return f.artwork, nil
}
```

Behavior notes to preserve: `ExtractArtwork()` still returns `(nil, nil)`-equivalent (empty slice,
nil error) for unsupported formats; callers checking `len(artwork) > 0` are unaffected. Note the
empty-slice return changes `artwork == nil` checks to `len(artwork) == 0` semantics — grep root
tests and examples for `!= nil` checks on the result (`grep -rn "ExtractArtwork" examples/ *_test.go`)
and confirm none break.

**Verify**: `go test ./...` → all pass.

### Step 3: Align the godoc

Update `options.go:87-99` godoc to state exactly what happens: "Artwork larger than this limit is
omitted from ExtractArtwork results and a warning is appended to File.Warnings. The data may still
be read during extraction; the limit bounds what is returned and cached." Also update
`ExtractArtwork`'s doc (`file.go:175-192`) to mention the option.

**Verify**: `go vet ./...` → exit 0 (godoc formatting), `golangci-lint run ./...` → exit 