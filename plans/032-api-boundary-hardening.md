# Plan 032: Harden the Open/ExtractArtwork boundary (option ordering, panic recovery, warning cap, artwork cache)

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving on. If a
> STOP condition occurs, stop and report — do not improvise. When done, update
> the status row for this plan in `plans/README.md`.
>
> **Drift check (run first)**: `git diff --stat aca9750..HEAD -- file.go options.go internal/types/errors.go internal/flac/parser.go internal/m4a/metadata.go internal/m4a/parser.go internal/mp3/id3v2.go internal/mp3/parser.go internal/ogg/opus.go internal/ogg/parser.go internal/ogg/vorbis.go`
> If any of these changed, compare the excerpts below against the live code; on a mismatch,
> STOP. (Plans 003, 008 and 015 also touch `file.go` / warning sites — if one of them landed
> first, see "STOP conditions".)

## Status

- **Priority**: P2
- **Effort**: M
- **Risk**: LOW (public signatures unchanged; two documented behaviors become true; one
  observable change — strict mode now also fails on artwork-preload warnings, see Step 2)
- **Depends on**: none (001's corpus is nice-to-have, not required — all fixtures here are synthetic)
- **Category**: bug / security
- **Planned at**: commit `aca9750`, 2026-10-30

## Why this matters

The public entry points (`Open`, `OpenContext`, `OpenMany`, `File.ExtractArtwork[Context]`) are
the only surface a consumer — an audiobook server scanning large, untrusted libraries — touches,
and four defects sit right on it:

1. **Options interact wrongly.** `WithStrictParsing()` + `WithIgnoreWarnings()` never fails
   (warnings are cleared before the strict check), and artwork-preload warnings bypass both
   strict mode and the "file.Warnings will always be empty" promise of `WithIgnoreWarnings`.
2. **A parser panic kills the host process.** There is no `recover()` anywhere in the repo.
   In `OpenMany` parses run on errgroup goroutines, where an unrecovered panic terminates the
   whole server, not just the request.
3. **Warnings are unbounded.** A 1 MB file of `fLaC` followed by zero bytes produces 250,000
   warnings and retains ~52 MB of heap (measured at `aca9750`: `retained=52449368`,
   `total=117886944` bytes allocated) — ~50× amplification from a trivially crafted file.
4. **The artwork cache doesn't cache "no artwork" and is racy.** Every `ExtractArtwork` call on
   an artwork-less file re-runs extraction (for Ogg that is a full re-`Parse`), and the
   unsynchronized cache write is a data race if a `*File` is shared between goroutines.

After this plan: option semantics are deterministic and documented, panics surface as
`*CorruptedFileError`, per-file warnings are capped, and artwork is extracted at most once per
`*File` under a mutex.

## Current state

Files and roles:
- `file.go` — public `Open`/`OpenContext`/`OpenMany`, `File` wrapper, `openWithContext`,
  `openReader`, `ExtractArtwork[Context]`.
- `options.go` — `With*` functional options and their godoc promises.
- `internal/types/errors.go` — `Warning`, `CorruptedFileError`, stage constants.
- 26 warning-append sites in `internal/{flac,m4a,mp3,ogg}` (listed in Step 3).

**`File` struct** — `file.go:28-34`:

```go
type File struct {
	types.File

	reader  io.ReaderAt
	parser  FormatParser
	artwork []Artwork
}
```

**Strict check runs on already-cleared warnings, and before preload** — `file.go:86-118`:

```go
	typesFile, err := openReader(ctx, f, size, path, options)
	...
	file := &File{
		File:   *typesFile.file,
		reader: f,
		parser: typesFile.parser,
	}

	// Check strict parsing mode
	if options.strictParsing && len(file.Warnings) > 0 {
		_ = f.Close()
		return nil, fmt.Errorf("strict parsing failed: %s", file.Warnings[0].Message)
	}

	// Preload artwork if requested
	if options.preloadArtwork {
		if _, err := file.ExtractArtwork(); err != nil {
			// Don't fail completely, just add warning
			file.Warnings = append(file.Warnings, Warning{
				Stage:   StageArtwork,
				Message: fmt.Sprintf("preload artwork failed: %v", err),
				Err:     err,
			})
		}
	}

	return file, nil
```

`file.go:146-160` (inside `openReader`):

```go
	// Parse metadata; parsers check ctx at major boundaries.
	file, err := parser.Parse(ctx, r, size, path)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", format, err)
	}
	...
	// Apply option: ignore warnings
	if options.ignoreWarnings {
		file.Warnings = nil
	}
```

Verified behavior at `aca9750` (8-byte file `fLaC 80 00 00 00` = a last-block STREAMINFO of
length 0 → exactly one warning):
- default → 1 warning `failed to parse STREAMINFO: invalid STREAMINFO size: 0 (expected 34)`
- `WithStrictParsing()` → error `strict parsing failed: failed to parse STREAMINFO: ...`
- `WithStrictParsing(), WithIgnoreWarnings()` → **no error**, file returned (bug)

Godoc promises — `options.go:33-49` ("With strict parsing enabled, any warning becomes a fatal
error.") and `options.go:69-85` (`// file.Warnings will always be empty` at line 80).

**No recovery** — `grep -rn "recover()" --include='*.go' .` → no matches. `OpenMany`
(`file.go:296-321`) runs `openWithContext` inside `g.Go(func() error { ... })` (lines 307-317).
`ExtractArtworkContext` calls the extractor directly at `file.go:215`.

**Artwork cache** — `file.go:201-224`:

```go
func (f *File) ExtractArtworkContext(ctx context.Context) ([]Artwork, error) {
	// Return cached artwork if already loaded
	if f.artwork != nil {
		return f.artwork, nil
	}

	// Check if parser supports artwork extraction
	extractor, ok := f.parser.(ArtworkExtractor)
	if !ok {
		// Format doesn't support artwork
		return nil, nil
	}

	// Extract artwork
	artwork, err := extractor.ExtractArtwork(ctx, f.reader, f.Size, f.Path)
	if err != nil {
		return nil, fmt.Errorf("extract artwork: %w", err)
	}

	// Cache for future calls
	f.artwork = artwork

	return artwork, nil
}
```

Extractors return a nil slice when nothing is found (e.g. `internal/ogg/artwork.go:33`
`var artwork []types.Artwork` … `return artwork, nil`; Ogg's extractor also re-runs the full
`p.Parse` at line 28). The godoc (`file.go:181`) says "Returns an empty slice if the file
contains no artwork."

**Warning flood** — `internal/flac/parser.go:66-142`: the metadata-block loop advances
`offset += 4` (header) `+ blockLength` (0 for zero bytes); header `0x00000000` = non-last
STREAMINFO of length 0 → `parseStreamInfo` errors (`parser.go:218-220`) → one formatted warning
appended per 4 input bytes (`parser.go:89-96`). Same shape in the Vorbis comment loops: FLAC
`internal/flac/parser.go:288-313` (`numComments` from the file, each empty comment →
`missing '='` warning), Ogg `internal/ogg/vorbis.go:112-149`, Opus `internal/ogg/opus.go:125-163`.

Conventions:
- Warnings: `types.Warning{Stage: types.StageX, Message, Err, Offset}`; root package re-exports
  `Warning` and `StageX` (`errors.go`). Errors: `*types.CorruptedFileError{Path, Reason, Offset}`.
- Lint enables `errname` (error types must end in `Error`), `godot` (comments end with a
  period), `errorlint`, `govet` enable-all (incl. `copylocks`), `containedctx`.
- Tests: stdlib `testing` only, table-driven, `t.Run`. Internal (package `audiometa`) tests
  exist: `format_test.go`, `errors_test.go`. No test uses `t.Parallel()` (verified), so a test
  may temporarily swap a registry entry with `registry.Register` and restore it in `t.Cleanup`.

## Commands you will need

| Purpose | Command | Expected on success |
|---|---|---|
| Build | `go build ./...` | exit 0 |
| Vet | `go vet ./...` | exit 0 |
| Tests | `go test ./...` | all pass |
| Race | `go test -race ./...` | all pass |
| Focused | `go test -race -run 'WarningPolicy\|Panic\|WarningCap\|ExtractArtwork' -v .` | new tests PASS |
| Lint | `mise exec -- golangci-lint run ./...` | `0 issues.` |
| Format | `gofmt -l .` | no output |

> Lint MUST be run via `mise exec --`: a stale global golangci-lint 2.11.4 earlier on PATH
> reports false typecheck errors under Go 1.27. mise pins 2.15.0. Do not use APIs newer than
> Go 1.26 (`go.mod` says `go 1.26.0`).

## Scope

**In scope**:
- `file.go`
- `options.go` (godoc only)
- `internal/types/errors.go` (add `MaxWarnings` + `AppendWarning`)
- `internal/types/warning_test.go` (create)
- The 26 warning-append sites — mechanical one-token change only — in
  `internal/flac/parser.go`, `internal/m4a/metadata.go`, `internal/m4a/parser.go`,
  `internal/mp3/id3v2.go`, `internal/mp3/parser.go`, `internal/ogg/opus.go`,
  `internal/ogg/parser.go`, `internal/ogg/vorbis.go`
- `file_boundary_test.go` (create, package `audiometa`)

**Out of scope**:
- `OpenMany`'s signature. It takes no `...Option` (`file.go:296`) so batch callers cannot use
  strict/ignore/preload. Adding options is an API decision for the operator — record it in your
  report; do not change the signature.
- `WithMaxArtworkSize` wiring — plan 008 owns it.
- Removing the informational Opus warnings — plan 015 owns it.
- Making loops stop early or check ctx — plan 003 (ctx) and plan 002 (allocation caps). The
  cap here bounds *retained memory*; the loops still iterate (CPU linear in file size).
- The `FormatParser`/`ArtworkExtractor` interfaces (`internal/registry/registry.go`) — public
  via aliases at `file.go:325,329`; do not change.

## Git workflow

- Branch: `advisor/032-api-boundary-hardening`
- Commit style: gitmoji single-line, no body, no attribution. One commit per step, e.g.
  `🐛 Apply warning policy after artwork preload`, `🥅 Recover parser panics as CorruptedFileError`,
  `🔒 Cap per-file warnings`, `🐛 Cache empty artwork and guard it with a mutex`.
- Do NOT push or open a PR unless instructed.

## Steps

### Step 1: Recover panics at every parser/extractor boundary

In `file.go` add:

```go
// maxPanicDetail bounds how much of a recovered panic value is copied into the error.
const maxPanicDetail = 256

// panicError converts a recovered panic value into a CorruptedFileError.
//
// recover cannot catch Go runtime fatal errors such as out-of-memory or
// concurrent map writes; those still terminate the process. Bounding
// allocations from untrusted length fields is a separate safeguard.
func panicError(path string, rec any) error {
	detail := fmt.Sprint(rec)
	if len(detail) > maxPanicDetail {
		detail = detail[:maxPanicDetail] + "..."
	}
	return &types.CorruptedFileError{Path: path, Reason: "parser panic: " + detail}
}

// parseSafely calls p.Parse, converting a panic into a CorruptedFileError.
func parseSafely(ctx context.Context, p FormatParser, r io.ReaderAt, size int64, path string) (file *types.File, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			file, err = nil, panicError(path, rec)
		}
	}()
	return p.Parse(ctx, r, size, path)
}

// extractSafely calls e.ExtractArtwork, converting a panic into a CorruptedFileError.
func extractSafely(ctx context.Context, e ArtworkExtractor, r io.ReaderAt, size int64, path string) (art []Artwork, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			art, err = nil, panicError(path, rec)
		}
	}()
	return e.ExtractArtwork(ctx, r, size, path)
}
```

Wire them in:
- `file.go:147`: `file, err := parser.Parse(ctx, r, size, path)` → `file, err := parseSafely(ctx, parser, r, size, path)`.
  The existing `fmt.Errorf("parse %s: %w", format, err)` wrap stays, so
  `errors.As(err, &*CorruptedFileError)` works on the result.
- `file.go:215`: `extractor.ExtractArtwork(...)` → `extractSafely(ctx, extractor, f.reader, f.Size, f.Path)`
  (Step 4 rewrites this function; carry the change over).
- `OpenMany` goroutine (`file.go:308-316`): add a belt-and-braces recover as the first statement
  of the closure, so any future non-parser panic in `openWithContext` is still contained:

  ```go
  g.Go(func() (goErr error) {
  	defer func() {
  		if rec := recover(); rec != nil {
  			errs[i] = fmt.Errorf("%s: %w", path, panicError(path, rec))
  		}
  	}()
  	...existing body unchanged...
  })
  ```
  (If lint objects to the named result, use an unnamed `error` result — the recover only
  writes `errs[i]`, it doesn't need to change the return value.)

**Verify**: `go build ./...` → exit 0; `go test ./...` → all pass;
`grep -c "recover()" file.go` → `3`.

### Step 2: Apply the warning policy once, after preload

1. Delete the ignore block from `openReader` (`file.go:157-160`). `openReader` no longer reads
   `options` — if its `options` parameter becomes unused, remove the parameter and update the
   single caller (`file.go:87`); `revive unused-parameter` will flag it otherwise.
2. In `openWithContext`, delete the strict block (`file.go:100-104`) and, **after** the preload
   block, add a single policy call:

   ```go
   	if err := applyWarningPolicy(&file.File, options); err != nil {
   		_ = f.Close()
   		return nil, err
   	}
   	return file, nil
   ```

3. Add the helper and error type to `file.go`:

   ```go
   // strictParsingError reports that WithStrictParsing rejected a file. Its message
   // names the first warning; Unwrap exposes every warning's underlying error so
   // callers can use errors.Is/As.
   type strictParsingError struct {
   	msg  string
   	errs []error
   }

   func (e *strictParsingError) Error() string   { return e.msg }
   func (e *strictParsingError) Unwrap() []error { return e.errs }

   // applyWarningPolicy enforces WithStrictParsing, then WithIgnoreWarnings.
   // Strict takes precedence: a file with any warning fails even when
   // warnings are also being ignored.
   func applyWarningPolicy(file *types.File, options *openOptions) error {
   	if options.strictParsing && len(file.Warnings) > 0 {
   		msg := "strict parsing failed: " + file.Warnings[0].Message
   		if n := len(file.Warnings) - 1; n > 0 {
   			msg += fmt.Sprintf(" (and %d more warnings)", n)
   		}
   		var errs []error
   		for _, w := range file.Warnings {
   			if w.Err != nil {
   				errs = append(errs, w.Err)
   			}
   		}
   		return &strictParsingError{msg: msg, errs: errs}
   	}
   	if options.ignoreWarnings {
   		file.Warnings = nil
   	}
   	return nil
   }
   ```

   Message decision: the prefix `strict parsing failed: <first message>` is unchanged for the
   single-warning case (existing callers matching on it keep working); a ` (and N more
   warnings)` suffix is added only when there are several. Keep `strictParsingError`
   unexported — exporting it is an API decision (mention in report).
4. Change the preload-failure append (`file.go:110`) to use `types.AppendWarning` after Step 3
   lands (or now, if you do Step 3 first).
5. Godoc in `options.go`:
   - `WithStrictParsing`: add "Warnings from WithArtworkPreload count too. Strict parsing
     takes precedence over WithIgnoreWarnings."
   - `WithIgnoreWarnings`: keep "file.Warnings will always be empty"; add "(unless
     WithStrictParsing is also set, in which case a file with warnings fails to open)."

Behavior change to call out in the PR/report: a preload artwork failure under
`WithStrictParsing()` now fails `Open` (previously it was a warning that slipped past the strict
check). This matches `WithArtworkPreload`'s own godoc ("want to fail fast if artwork extraction
has issues", `options.go:56-57`).

**Verify**: `go build ./...` and `go vet ./...` → exit 0; `go test ./...` → all pass;
`grep -n "ignoreWarnings" file.go` → only inside `applyWarningPolicy`.

### Step 3: Cap per-file warnings at one choke point

In `internal/types/errors.go` (below `Warning`), add:

```go
// MaxWarnings is the most warnings a single file accumulates. Once reached, one
// final warning notes the truncation and further warnings are dropped, so a
// crafted file cannot grow File.Warnings without bound.
const MaxWarnings = 100

// AppendWarning appends w to ws unless the per-file cap has been reached.
func AppendWarning(ws []Warning, w Warning) []Warning {
	switch {
	case len(ws) < MaxWarnings:
		return append(ws, w)
	case len(ws) == MaxWarnings:
		return append(ws, Warning{
			Stage:   w.Stage,
			Message: fmt.Sprintf("further warnings suppressed after %d", MaxWarnings),
			Offset:  w.Offset,
		})
	default:
		return ws
	}
}
```

(A package-level function, not a method on `types.File`: a method would be promoted onto the
public `audiometa.File` through embedding and become public API.)

Then replace every parser append. All 26 internal sites share one exact spelling (verified):

```sh
grep -rln 'file.Warnings = append(file.Warnings, types.Warning{' internal/ --include='*.go' \
  | grep -v _test.go \
  | xargs sed -i 's/file\.Warnings = append(file\.Warnings, types\.Warning{/file.Warnings = types.AppendWarning(file.Warnings, types.Warning{/'
```

Expected sites (file: count): `internal/flac/parser.go: 5`, `internal/m4a/metadata.go: 1`,
`internal/m4a/parser.go: 4`, `internal/mp3/id3v2.go: 1`, `internal/mp3/parser.go: 2`,
`internal/ogg/opus.go: 5`, `internal/ogg/parser.go: 5`, `internal/ogg/vorbis.go: 3`.
In `file.go` change the preload append to
`file.Warnings = types.AppendWarning(file.Warnings, Warning{`.

What remains (say so in the report): the loops still run to the end of the input, formatting a
message per iteration that is then dropped — CPU stays linear in file size (bounded by file
size, and cancellable once plan 003 lands). Early-exit on repeated identical failures is a
follow-up, not this plan.

**Verify**: `grep -rn "Warnings = append(" --include='*.go' . | grep -v _test.go` → no output;
`grep -rn "types.AppendWarning(" --include='*.go' . | grep -v _test.go | wc -l` → `27`;
`go build ./...` → exit 0; `go test ./...` → all pass.

### Step 4: Cache artwork once, including "none", under a mutex

1. Extend `File` (`file.go:28-34`): keep `types.File`, `reader`, `parser`, `artwork` and add:

   ```go
   	artworkMu     sync.Mutex // guards artwork and artworkLoaded
   	artworkLoaded bool
   ```
   (`import "sync"`.) `File` becomes non-copyable; `go vet` (copylocks) will flag any value
   copy — none exist today (verified: `File` is only ever handled as `*File`).
2. Rewrite `ExtractArtworkContext` (`file.go:201-224`):

   ```go
   func (f *File) ExtractArtworkContext(ctx context.Context) ([]Artwork, error) {
   	f.artworkMu.Lock()
   	defer f.artworkMu.Unlock()

   	if f.artworkLoaded {
   		return f.artwork, nil
   	}

   	artwork := []Artwork{}
   	if extractor, ok := f.parser.(ArtworkExtractor); ok {
   		extracted, err := extractSafely(ctx, extractor, f.reader, f.Size, f.Path)
   		if err != nil {
   			return nil, fmt.Errorf("extract artwork: %w", err) // not cached: a later call may retry
   		}
   		if extracted != nil {
   			artwork = extracted
   		}
   	}

   	f.artwork = artwork
   	f.artworkLoaded = true
   	return artwork, nil
   }
   ```

   Errors are deliberately not cached (a canceled ctx must not poison later calls). The lock is
   held across extraction so concurrent callers wait for the one extraction instead of racing.
3. Godoc: add to `ExtractArtwork`'s comment "Safe for concurrent use; extraction runs at most
   once per File (failed attempts are retried on the next call)." and keep "Returns an empty
   slice if the file contains no artwork." (now true for formats without an extractor too).

**Verify**: `go build ./...`, `go vet ./...` → exit 0; `go test -race ./...` → all pass.

### Step 5: Tests

Create `file_boundary_test.go` (package `audiometa`, internal so it can build `File` values and
swap registry entries). Helpers:

```go
// writeTemp writes data to a temp file and returns its path.
func writeTemp(t *testing.T, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// oneWarningFLAC is "fLaC" + a last-block STREAMINFO header of length 0: exactly one warning.
var oneWarningFLAC = []byte{'f', 'L', 'a', 'C', 0x80, 0, 0, 0}
```

Tests to write:
1. `TestWarningPolicy` (table over options, opening `oneWarningFLAC`):
   none → nil error, 1 warning; `WithIgnoreWarnings()` → nil error, 0 warnings;
   `WithStrictParsing()` → error whose message starts with
   `strict parsing failed: failed to parse STREAMINFO`; `WithStrictParsing(), WithIgnoreWarnings()`
   → error (the bug fixed by Step 2); for the strict cases also assert the returned `*File` is nil.
2. `TestApplyWarningPolicy_JoinsErrors`: a `types.File` with two warnings whose `Err` are
   `errA`, `errB` sentinels → strict error; `errors.Is(err, errA)` and `errors.Is(err, errB)`
   both true; message ends with `(and 1 more warnings)`.
3. `TestWarningCap`: open `"fLaC"` + 4000 zero bytes (1000 would-be warnings) →
   `len(f.Warnings) == types.MaxWarnings+1` and the last message contains `suppressed`.
4. `TestOpen_RecoversParserPanic`: save `orig := registry.Get(types.FormatFLAC)`,
   `registry.Register(types.FormatFLAC, panicParser{})`, `t.Cleanup(func() { registry.Register(types.FormatFLAC, orig) })`
   where `panicParser.Parse` does `panic("boom")`. `Open(oneWarningFLAC path)` → error,
   `errors.As(err, &cfe)` with `cfe *CorruptedFileError` true, `cfe.Reason` contains `boom`.
   Then `OpenMany(ctx, path, path)` → both slots nil, non-nil joined error, test process alive.
5. `TestExtractArtwork_RecoversPanic`: `f := &File{parser: panicExtractor{}, reader: bytes.NewReader(nil)}`
   (`panicExtractor` implements both `Parse` and `ExtractArtwork`, the latter panics) →
   `ExtractArtwork()` returns a non-nil error wrapping `*CorruptedFileError`.
6. `TestExtractArtwork_ConcurrentExtractOnce`: `countingExtractor` with an `atomic.Int32`
   counter whose `ExtractArtwork` increments and returns `nil, nil`. Start 16 goroutines
   (`sync.WaitGroup`) each calling `f.ExtractArtwork()`; assert counter == 1, every result is
   non-nil with `len == 0`, no error. Must pass under `-race`.
7. `TestExtractArtwork_ErrorNotCached`: extractor returns an error on the first call and
   `[]Artwork{{}}` on the second → first call errors, second returns 1 image.
8. `TestExtractArtwork_NoExtractor`: parser without `ExtractArtwork` → returns non-nil empty slice.

Create `internal/types/warning_test.go`: `AppendWarning` with 0, `MaxWarnings-1`, `MaxWarnings`
and `MaxWarnings+1` existing entries → lengths `1`, `MaxWarnings`, `MaxWarnings+1`,
`MaxWarnings+1`.

**Verify**: `go test -race -run 'WarningPolicy|ApplyWarningPolicy|WarningCap|Panic|ExtractArtwork' -v .`
→ all new tests PASS; `go test -race ./...` → all pass.

## Test plan

- New tests listed in Step 5 (8 in `file_boundary_test.go`, 1 table test in
  `internal/types/warning_test.go`).
- Structural pattern: internal tests like `format_test.go`; open-and-assert shape from
  `file_cancel_test.go:82-118` (`TestOpenMany_PartialFailure`, incl. its `t.Cleanup` close loop).
- `go test -race ./...` → all pass.

## Done criteria

- [ ] `go build ./...`, `go vet ./...` exit 0; `gofmt -l .` prints nothing
- [ ] `mise exec -- golangci-lint run ./...` → `0 issues.`
- [ ] `go test -race ./...` exits 0 including the new tests
- [ ] `grep -c "recover()" file.go` → `3`
- [ ] `grep -rn "Warnings = append(" --include='*.go' . | grep -v _test.go` → no output
- [ ] `Open(oneWarningFLAC, WithStrictParsing(), WithIgnoreWarnings())` returns an error (test 1)
- [ ] No files outside the Scope list modified (`git status`)
- [ ] `plans/README.md` status row for 032 updated; report mentions the `OpenMany` options gap
      and the remaining CPU-linear loops

## STOP conditions

- Plan 008 landed first and already rewrote `ExtractArtworkContext` (look for `maxArtworkSize`
  in `file.go`): merge Step 4 into its version — keep its size filtering, add the mutex/loaded
  flag around it. Its "append a size warning to `f.Warnings` from inside ExtractArtwork" would
  then be a race on `Warnings` for shared `*File`s — STOP and report that conflict rather than
  choosing a resolution.
- Plan 003 landed first and changed warning-append spellings so the Step 3 `sed` matches fewer
  than 26 sites — list the unmatched sites and convert them by hand only if the change is the
  same one-token replacement; otherwise STOP.
- Any existing test asserts a strict-mode message that now differs, or expects artwork-preload
  failures not to fail strict mode — STOP and report the test name.
- `go vet` reports `copylocks` on a `File` value copy you cannot remove without touching an
  out-of-scope file — STOP.
- `errname`/`revive` lint refuses `strictParsingError` and the fix would require exporting it —
  STOP (exporting is an API decision).

## Maintenance notes

- `recover` cannot catch runtime fatal errors (OOM, concurrent map writes, stack exhaustion is
  fatal too). Plan 002's allocation caps remain required; this plan only stops ordinary panics
  (index out of range, nil deref) from killing a scanning server.
- New parser code must use `types.AppendWarning`; a reviewer should reject raw
  `append(file.Warnings, ...)`. Consider a lint rule (forbidigo) as a follow-up.
- `MaxWarnings = 100` is arbitrary; if consumers need more, make it an option rather than raising
  the constant.
- Deferred API decisions for the operator: `OpenMany(ctx, paths...)` has no `...Option`
  (batch callers can't use strict/ignore/preload); whether to export `strictParsingError`.
- Reviewer focus: Step 2 ordering (preload → strict → ignore), that errors are not cached in
  Step 4, and that the panic `Reason` is truncated.
