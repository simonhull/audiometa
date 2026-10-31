# Plan 017: Delete dead internal code and decide the fate of unused public API

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving on. If a
> STOP condition occurs, stop and report — do not improvise. When done, update
> the status row for this plan in `plans/README.md`.
>
> **This plan has two parts.** Part A (internal-only deletions) is safe — do it.
> Part B touches the **public API** of a pre-1.0 module; you must NOT delete or
> change any exported root-package symbol unless the "Maintainer decision" column of
> the Part B table has been filled in with `delete` / `wire` / `keep` for that row.
> If the column is empty, finish Part A, then stop and report the table back.
>
> **Drift check (run first)**:
> `git diff --stat aca9750..HEAD -- internal/binary/ internal/m4a/atoms.go internal/m4a/atoms_test.go internal/types/artwork.go internal/types/errors.go internal/types/audio.go internal/types/audio_test.go internal/types/format.go internal/types/format_test.go artwork.go errors.go errors_test.go file.go format_test.go`
> If any in-scope file changed since this plan was written, compare the excerpts below
> against the live code; on a mismatch, STOP.

## Status

- **Priority**: P3
- **Effort**: S (Part A) – M (Part B, if all deletes/wires are chosen)
- **Risk**: LOW for Part A (internal packages only); MED for Part B (exported-symbol deletions are breaking changes for any importer, even pre-1.0)
- **Depends on**: none
- **Category**: tech-debt
- **Planned at**: commit `aca9750`, 2026-10-30 (replaces the 2026-07-01 stub written at `7b22897`)

## Why this matters

`internal/binary/reader.go` carries a ~110-line sequential-reader abstraction
(`Reader`, `ChainReader`, …) that no parser ever adopted — only its own tests call it —
plus `ReadBE` (unused) and a `Read[T]` that copy-pastes `ReadEndian[T]`'s size switch.
`internal/m4a/atoms.go` has an `Atom.IsContainer` method nobody calls that allocates a map on
every call. Dead code costs reading time, misleads contributors about "the house way" to read
binary data, and inflates coverage numbers with tests of nothing. Separately, the root package
exports several symbols that nothing constructs or calls (`RawTag`, `OutOfBoundsError`,
`RegisterParser`, a few `AudioInfo` helpers) — each is a promise the library makes to users for
free. Pre-1.0 is the cheapest time to either delete them or make them real; this plan does the
safe half and gives the maintainer a decision table for the other half.

## Current state

All excerpts verified at `aca9750`. "No non-test callers" was checked with
`grep -rn --include='*.go' <symbol> . | grep -v '^./plans/' | grep -v '^./tmp/'`.

### Part A facts (internal packages — not importable by users)

- `internal/binary/reader.go` — `SafeReader` (lines 11-56, **live**, used everywhere), `Read[T]`
  (lines 58-97, **live**, ~60 call sites), and the dead sequential layer (lines 99-208):

  ```go
  // reader.go:99
  // Reader provides sequential reading with automatic offset tracking.
  type Reader struct {
  	*SafeReader
  	offset int64
  }
  // reader.go:106  func NewReader(sr *SafeReader, offset int64) *Reader
  // reader.go:114  func ReadValue[T ...](r *Reader, what string) (T, error)
  // reader.go:140  func (r *Reader) ReadString(length int, what string) (string, error)
  // reader.go:151  func (r *Reader) Skip(n int64)
  // reader.go:156  func (r *Reader) Offset() int64
  // reader.go:162  type ChainReader struct { *Reader; err error }
  // reader.go:168  func NewChainReader(r *Reader) *ChainReader
  // reader.go:174  func ReadChained[T ...](cr *ChainReader, what string) T
  // reader.go:191  func (cr *ChainReader) String(length int, what string) string
  // reader.go:206  func (cr *ChainReader) Error() error
  ```

  Only callers: `internal/binary/reader_test.go` — `TestReader_Sequential` (:126),
  `TestReader_Skip` (:155), `TestReader_ReadString` (:172), `TestChainReader_Success` (:192),
  `TestChainReader_ErrorAccumulation` (:212, ends :233), `BenchmarkReader_Sequential` (:250-262).
  (Every other `NewReader` hit in the repo is `bytes.NewReader` — not this function.)

- `Read[T]` duplicates `ReadEndian[T]` (`internal/binary/endian.go:50-97`). `Read` today:

  ```go
  // reader.go:60-97 (abridged)
  func Read[T uint8 | uint16 | uint32 | uint64](sr *SafeReader, off int64, what string) (T, error) {
  	var zero T
  	var size int
  	switch any(zero).(type) { case uint8: size = 1 ... default: return zero, errors.New("unsupported type for Read") }
  	buf := make([]byte, size)
  	if err := sr.ReadAt(buf, off, what); err != nil { return zero, err }
  	// Convert bytes to value (big-endian, MP4 standard)
  	...
  }
  ```

  `ReadEndian[T](sr, off, what, BigEndian)` produces identical results (same size switch, same
  `sr.ReadAt` call, same big-endian decode). The `default:` arm is unreachable — the type
  constraint admits only the four listed types.

- `internal/binary/endian.go:30-40` — `ReadBE[T]`: zero non-test callers. Test callers in
  `internal/binary/endian_test.go`: `TestReadBE` (:73-135), `TestEndianness_RealWorldExample`
  (:190), `TestReadEndian_Uint8` (:205), `BenchmarkReadBE_Uint32` (:235-243). `ReadLE` **is** live
  (`internal/flac/parser.go:271,281,290`, `internal/ogg/container.go:51,56,61,173`) — keep it.

- `internal/m4a/atoms.go:40-56`:

  ```go
  // atomTrak is the track container atom type.
  const atomTrak = "trak"

  // IsContainer returns true if this atom type can contain other atoms.
  func (a *Atom) IsContainer() bool {
  	containerTypes := map[string]bool{
  		"moov":   true, // Movie container
  		...
  		atomTrak: true, // Track container
  		...
  	}
  	return containerTypes[a.Type]
  }
  ```

  `IsContainer` has no non-test caller (test: `internal/m4a/atoms_test.go:185-210`,
  `TestAtom_IsContainer`, the last function in that file). **Keep `atomTrak`** — it is live in
  `internal/m4a/technical.go:26` and `internal/m4a/chapters.go:173,217`.
  (`cmd/atom-dump-tool/atom-dump.go:82` has its own unrelated `isContainer` — leave it.)

### Part B facts (exported from the root package `github.com/simonhull/audiometa`)

The root package re-exports `internal/types` via type aliases, so every exported method on an
aliased type is public API.

| # | Symbol(s) | Where | Evidence |
|---|---|---|---|
| B1 | `RawTag`, `RawTagType`, `RawTagText/Binary/Image/Counter/URL`, `RawTag.String()` | `internal/types/artwork.go:126-168`; root aliases `artwork.go:40-55` | Never constructed anywhere. Raw tags are stored as `raw map[string][]string` (`internal/types/tags.go:18`), not `RawTag`. (The test name `TestParseComment_StoresRawTag` in `internal/vorbis/comments_test.go:257` refers to the map, not this type.) |
| B2 | `OutOfBoundsError` | `internal/types/errors.go:5-21`; root alias `errors.go:7-9`; test `errors_test.go:8-` | Never constructed. `SafeReader.ReadAt` (`internal/binary/reader.go:35-43`) returns `fmt.Errorf` strings with the **same wording**, so `errors.As(err, &OutOfBoundsError{})` can never succeed today. |
| B3 | `Format.Extensions()` | `internal/types/format.go:35-59`; tests `internal/types/format_test.go:114-` and root `format_test.go:146-` | No non-test caller. Plausibly useful to a CLI walking directories (plan 021). |
| B4 | `AudioInfo.ApplyReplayGain` (+ private `pow10`) | `internal/types/audio.go:133-180`; test `internal/types/audio_test.go:165-` | No caller. **Suspected formula bug**, see below. Out of a metadata *reader*'s remit (it's playback DSP). |
| B5 | `AudioInfo.ShortCodecName`, `AudioInfo.FullCodecName` | `internal/types/audio.go:182-202`; tests `audio_test.go:248-312` | No caller. Harmless display helpers; a CLI (plan 021) could use `FullCodecName`. |
| B6 | `AudioInfo.IsModernAudiobookCodec` | `internal/types/audio.go:204-220`; test `audio_test.go:314-` | No caller. Opinionated policy; its comment `case "mhm1", "mhm2": // xHE-AAC` is wrong — `mhm1`/`mhm2` are MPEG-H 3D Audio sample entries; xHE-AAC is `mp4a` with the USAC object type. |
| B7 | `RegisterParser`, `FormatParser`, `ArtworkExtractor` | `file.go:323-345` | Godoc of `RegisterParser` (file.go:338-342) says "not intended for external use. Do not call this function." Parsers self-register via `registry.Register` in their `init()` (e.g. `internal/mp3/parser.go:73-75`). `FormatParser.Parse` returns `*types.File` (`internal/registry/registry.go:18`), and `types.File` is **not** re-exported, so code outside this module cannot even write an implementing method. `FormatParser`/`ArtworkExtractor` aliases are used internally by `file.go:32,126,208,334`. |

**B4 bug detail** (do not fix in this plan): `audio.go:160-172`

```go
if peak == 0 {
	return amplitude          // gain silently NOT applied when peak is unknown/0
}
linearGain := pow10(gain / 20.0)
adjusted := amplitude * linearGain
// Prevent clipping
if peak > 0 && adjusted > 1.0/peak {   // compares a sample amplitude to a gain limit (mixed units)
	adjusted = 1.0 / peak
}
```

The correct clip guard limits the *gain* (`linearGain = min(linearGain, 1/peak)`), then
multiplies; and a zero/absent peak should still apply the gain. Unknown `mode` values also
silently fall back to track gain.

**Import-cycle constraint for B2**: `internal/types` imports `internal/binary`
(`internal/types/format.go:6`), so `internal/binary` cannot import `internal/types`. To have
`SafeReader.ReadAt` return a typed error, the struct must be **defined in `internal/binary`** and
aliased upward: `types.OutOfBoundsError = binary.OutOfBoundsError`, root alias unchanged.

**B2 message mismatch to fix if wired**: `OutOfBoundsError.Error()` picks the "offset … out of
bounds" wording only when `Offset >= Size`; `ReadAt` also rejects `off < 0` with that wording.
The wired version must use `if e.Offset < 0 || e.Offset >= e.Size`.

### Conventions

- Generic binary reads use `binary.Read[T](sr, off, "what")` — see `internal/m4a/technical.go:80`.
- Errors that callers may inspect are struct types with `Error()` on a pointer receiver
  (`internal/types/errors.go:23-42`); public ones are re-exported by alias in root `errors.go`.

## Commands you will need

| Purpose | Command | Expected on success |
|---|---|---|
| Build | `go build ./...` | exit 0 |
| Vet | `go vet ./...` | exit 0 |
| Tests | `go test ./...` | all `ok` |
| Race | `go test -race ./...` | all `ok` |
| Lint | `mise exec -- golangci-lint run ./...` | `0 issues.` (must go through `mise exec --`; a stale global golangci-lint on PATH reports false typecheck errors) |
| Format | `gofmt -l .` | no output |
| Dead-symbol check | `grep -rn --include='*.go' '<symbol>' . \| grep -v '^./plans/' \| grep -v '^./tmp/'` | as stated per step |

## Scope

**In scope** (Part A):
- `internal/binary/reader.go`, `internal/binary/reader_test.go`
- `internal/binary/endian.go`, `internal/binary/endian_test.go`
- `internal/m4a/atoms.go`, `internal/m4a/atoms_test.go`

**In scope** (Part B — only rows the maintainer marked):
- `internal/types/artwork.go`, `artwork.go` (B1)
- `internal/binary/reader.go`, `internal/binary/reader_test.go`, `internal/types/errors.go`, `errors.go`, `errors_test.go` (B2)
- `internal/types/format.go`, `internal/types/format_test.go`, `format_test.go` (B3)
- `internal/types/audio.go`, `internal/types/audio_test.go` (B4–B6)
- `file.go` (B7)

**Out of scope**:
- The shadowed `readErr` in `internal/m4a/chapters.go` `parseChunkOffsets` — owned by plan 031.
- `cmd/atom-dump-tool/` — plan 021 decides its fate.
- `SafeReader`, `Read`, `ReadLE`, `ReadEndian` signatures — live, keep them.
- Fixing `ApplyReplayGain` — if the maintainer keeps it, the fix is a separate plan.
- Any other exported symbol not in the Part B table.

## Git workflow

- Branch: `advisor/017-dead-code-pruning`
- Commit per logical unit; gitmoji, single line, no body, no attribution. Examples:
  `🔥 Remove unused binary Reader and ChainReader`, `♻️ Make binary.Read delegate to ReadEndian`,
  `🔥 Remove unused Atom.IsContainer`, `💥 Remove unexported-in-practice RegisterParser`.
- Do NOT push or open a PR.

## Steps — Part A (do these)

### Step A1: Delete the sequential Reader / ChainReader layer

In `internal/binary/reader.go`, delete lines 99-208 (from `// Reader provides sequential
reading…` through the end of `func (cr *ChainReader) Error()`). In
`internal/binary/reader_test.go`, delete `TestReader_Sequential`, `TestReader_Skip`,
`TestReader_ReadString`, `TestChainReader_Success`, `TestChainReader_ErrorAccumulation`, and
`BenchmarkReader_Sequential`. Keep `mockReader`, the `TestSafeReader_*`, `TestRead_*` tests and
`BenchmarkRead_Uint32`.

**Verify**:
- `grep -rn --include='*.go' -E 'ChainReader|ReadChained|ReadValue|binary\.NewReader\(|func NewReader' . | grep -v '^./plans/' | grep -v '^./tmp/'` → no output
- `go build ./... && go test ./internal/binary/` → `ok`

### Step A2: Make `Read[T]` delegate to `ReadEndian[T]`

Replace the body of `Read` (`internal/binary/reader.go`, the function after `ReadAt`) with:

```go
// Read reads a big-endian value of type T from the given offset.
// It is the default for MP4/ID3v2/FLAC-header fields; use ReadLE for little-endian data.
func Read[T uint8 | uint16 | uint32 | uint64](sr *SafeReader, off int64, what string) (T, error) {
	return ReadEndian[T](sr, off, what, BigEndian)
}
```

Remove imports that become unused (`errors`, `encoding/binary` — the compiler will tell you).
In `internal/binary/endian.go`, update the `ReadEndian` doc line "This is the low-level function
used by Read, ReadLE, and ReadBE." to drop `ReadBE` (after Step A3).

**Verify**: `go build ./... && go test ./internal/binary/ ./internal/m4a/ ./internal/flac/ ./internal/ogg/ ./internal/types/` → all `ok`

### Step A3: Delete `ReadBE`

Delete `ReadBE` (`internal/binary/endian.go:30-40`). In `internal/binary/endian_test.go`:
- `TestReadBE` → rename to `TestRead_BigEndian` and replace `ReadBE[` with `Read[`; update the
  `t.Fatalf`/`t.Errorf` strings from `ReadBE` to `Read`.
- In `TestEndianness_RealWorldExample` and `TestReadEndian_Uint8`, replace `ReadBE[` with `Read[`
  (and the messages likewise).
- Delete `BenchmarkReadBE_Uint32` (it duplicates `BenchmarkRead_Uint32_Legacy`).

**Verify**:
- `grep -rn --include='*.go' 'ReadBE' . | grep -v '^./plans/' | grep -v '^./tmp/'` → no output
- `go test ./internal/binary/` → `ok`

### Step A4: Delete `Atom.IsContainer`

Delete the `IsContainer` method and its doc comment from `internal/m4a/atoms.go` (lines 43-56).
**Keep** the `atomTrak` constant and its comment (lines 40-41). Delete `TestAtom_IsContainer`
from `internal/m4a/atoms_test.go` (lines 185-210).

**Verify**:
- `grep -rn --include='*.go' 'IsContainer' . | grep -v '^./plans/' | grep -v '^./tmp/'` → no output (`cmd/atom-dump-tool`'s lower-case `isContainer` does not match this case-sensitive grep)
- `grep -n 'atomTrak' internal/m4a/*.go` → still matches in `atoms.go`, `technical.go`, `chapters.go`
- `go test ./internal/m4a/` → `ok`

### Step A5: Full gate

**Verify**: `go build ./... && go vet ./... && go test -race ./... && mise exec -- golangci-lint run ./... && gofmt -l .`
→ all exit 0, lint prints `0 issues.`, gofmt prints nothing.

## Part B — decision table (maintainer fills the last column)

Advisor recommendation in column 4. A "delete" of any row is a **breaking change** for
importers; record it in the commit message with the `💥` gitmoji and list it in your final report
so it can go in release notes.

| # | Symbol(s) | Options | Advisor recommends | Maintainer decision |
|---|---|---|---|---|
| B1 | `RawTag` family | delete / keep | **delete** — no producer exists; plan 005 uses the `map[string][]string` raw store, not this type | |
| B2 | `OutOfBoundsError` | delete / **wire** | **wire** — non-breaking, makes `errors.As` work as the type's existence promises | |
| B3 | `Format.Extensions` | delete / keep | **keep** — plan 021's CLI can use it for directory walking | |
| B4 | `ApplyReplayGain` | delete / keep(+fix plan) | **delete** — buggy, playback DSP, no caller | |
| B5 | `ShortCodecName`, `FullCodecName` | delete / keep | **keep** — harmless; CLI display | |
| B6 | `IsModernAudiobookCodec` | delete / keep(+fix comment) | **delete** — opinionated and its codec table is wrong | |
| B7 | `RegisterParser`, `FormatParser`, `ArtworkExtractor` | delete / keep | **delete** — cannot be used externally (see Current state) | |

## Steps — Part B (each step runs ONLY if its row says so)

### Step B1 (if B1 = delete): Remove the RawTag family

Delete `internal/types/artwork.go:126-168` (the `RawTag` struct, `RawTagType`, its constants and
`RawTag.String`) and root `artwork.go:40-55` (aliases and constants). If `fmt` becomes unused in
`internal/types/artwork.go`, the compiler will say so — it is still used by `Artwork.String`, so
it should not.

**Verify**: `grep -rn --include='*.go' -E '\bRawTag(Type|Text|Binary|Image|Counter|URL)?\b' . | grep -v '^./plans/' | grep -v '^./tmp/' | grep -v 'StoresRawTag'` → no output; `go build ./... && go test ./...` → ok.

### Step B2 (if B2 = wire): Return a typed OutOfBoundsError from SafeReader.ReadAt

1. Move the `OutOfBoundsError` struct and its `Error()` method from `internal/types/errors.go` into
   `internal/binary/reader.go` (exported, same fields: `Path, What string; Offset int64;
   Length int; Size int64`). Change the condition to `if e.Offset < 0 || e.Offset >= e.Size`.
2. In `internal/types/errors.go` replace the struct with
   `type OutOfBoundsError = binary.OutOfBoundsError` (add the import
   `"github.com/simonhull/audiometa/internal/binary"`). Root `errors.go` stays as is.
3. In `SafeReader.ReadAt`, replace the two bounds-check `fmt.Errorf` returns with
   `return &OutOfBoundsError{Path: sr.path, What: what, Offset: off, Length: len(b), Size: sr.size}`.
   Leave the I/O-error and short-read returns as `fmt.Errorf` (they are not bounds errors).
4. Add tests to `internal/binary/reader_test.go`:
   - offset ≥ size → `errors.As(err, &oob)` true, message contains `out of bounds`.
   - offset < 0 → same, message contains `out of bounds` (not "would exceed").
   - off+len > size → `errors.As` true, message contains `would exceed file size`.
5. Add one root-level test in `errors_test.go` asserting `errors.As` works through `%w`
   wrapping: `err := fmt.Errorf("parse: %w", &OutOfBoundsError{...})`, then
   `var target *OutOfBoundsError; errors.As(err, &target)` → true.

**Verify**: `go test ./internal/binary/ ./internal/types/ .` → ok; existing
`TestSafeReader_ReadAt_OutOfBounds` and `TestOutOfBoundsError_Error` still pass unchanged.

### Step B3 (if B3 = delete): Remove Format.Extensions

Delete `internal/types/format.go:35-59` and both `TestFormat_Extensions` functions
(`internal/types/format_test.go:114-`, root `format_test.go:146-`).

**Verify**: `grep -rn --include='*.go' 'Extensions()' . | grep -v '^./plans/' | grep -v '^./tmp/'` → no output; tests pass.

### Step B4 (if B4 = delete): Remove ApplyReplayGain

Delete `ApplyReplayGain` and `pow10` (`internal/types/audio.go:133-180`) and
`TestAudioInfo_ApplyReplayGain`. Drop the `math` import if unused.
(If B4 = keep: do nothing here; add a line to your report "ApplyReplayGain kept — needs a fix
plan: gain-clip formula mixes units; peak==0 skips gain".)

**Verify**: `grep -rn --include='*.go' -E 'ApplyReplayGain|pow10' . | grep -v '^./plans/' | grep -v '^./tmp/'` → no output; tests pass.

### Step B5 (if B5 = delete): Remove ShortCodecName / FullCodecName

Delete `internal/types/audio.go:182-202` and their tests (`audio_test.go:248-312`).

**Verify**: grep for both names → no output; tests pass.

### Step B6 (if B6 = delete): Remove IsModernAudiobookCodec

Delete `internal/types/audio.go:204-220` and `TestAudioInfo_IsModernAudiobookCodec`.
(If keep: correct the comment on `"mhm1", "mhm2"` to `// MPEG-H 3D Audio` — do not change logic.)

**Verify**: grep → no output; tests pass.

### Step B7 (if B7 = delete): Remove RegisterParser and the parser-interface aliases

In `file.go`: delete `RegisterParser` (:338-345) and the `FormatParser` / `ArtworkExtractor`
aliases (:323-329). Replace internal uses with the registry types:
`parser FormatParser` (:32, :126) → `parser registry.FormatParser`;
`func findParser(...) FormatParser` (:334) → `registry.FormatParser`;
`f.parser.(ArtworkExtractor)` (:208) → `f.parser.(registry.ArtworkExtractor)`.
The comments `// parser implements the audiometa.FormatParser interface` in
`internal/{m4a,mp3,flac,ogg}/parser.go` become stale — change them to
`registry.FormatParser` (comment-only edits; add these four files to the commit).

**Verify**: `grep -rn --include='*.go' -E 'RegisterParser|audiometa\.FormatParser' . | grep -v '^./plans/' | grep -v '^./tmp/'` → no output; `go build ./... && go test -race ./...` → ok.

### Step B8: Full gate (same as A5)

## Test plan

- Part A removes tests of deleted code only; no behavior change, so the existing suite is the
  regression net (it exercises `Read[T]` through every parser — Step A2's delegation is covered).
- Part B2 adds three `internal/binary` tests (out-of-bounds offset, negative offset, overrun) and
  one root `errors.As`-through-wrapping test. Model them on `TestSafeReader_ReadAt_OutOfBounds`
  (`internal/binary/reader_test.go:42-61`).
- Final: `go test -race ./...` → all `ok`.

## Done criteria

- [ ] `go build ./...`, `go vet ./...`, `go test -race ./...` exit 0
- [ ] `mise exec -- golangci-lint run ./...` → `0 issues.`; `gofmt -l .` → empty
- [ ] `grep -rn --include='*.go' -E 'ChainReader|ReadChained|ReadValue|ReadBE|IsContainer\(' . | grep -v '^./plans/' | grep -v '^./tmp/'` → no output
- [ ] `grep -n 'atomTrak' internal/m4a/atoms.go` → still defined
- [ ] For each Part B row: either the maintainer's decision is implemented and its grep check passes, or the row was undecided and is listed in your report as "pending decision"
- [ ] `git status` shows changes only in in-scope files
- [ ] `plans/README.md` status row for 017 updated (DONE, or "PARTIAL — Part B pending decision")

## STOP conditions

- Any Part A symbol has gained a non-test caller since `aca9750` (the grep in the step returns a
  production file) — stop; the code is no longer dead.
- Step A2: any test fails after delegation — the two functions were assumed byte-identical;
  report the failing test rather than patching around it.
- Part B: the decision column is empty for a row — do not guess; skip it and report.
- B2: adding the `internal/binary` import to `internal/types/errors.go` produces an import cycle
  (it should not — `types` already imports `binary` in `format.go`). Report the cycle path.
- B7: removing the aliases requires touching any file outside `file.go` and the four
  `internal/*/parser.go` comment lines.

## Maintenance notes

- Part B deletions are breaking. If any were made, the next tag should be a minor bump with a
  "Removed" section in release notes listing them; plan 011 (Version constant) governs the version.
- If B4 or B6 were kept, file a follow-up plan: `ApplyReplayGain` clip formula and `peak==0`
  behavior; `IsModernAudiobookCodec` codec table.
- If B2 was wired, plan 022 (Validate/warning codes) can map `*OutOfBoundsError` in
  `Warning.Err` to a "truncated file" code.
- Reviewer: confirm `atomTrak` survived, and that `Read` still decodes big-endian (Step A2).
