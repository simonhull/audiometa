# Plan 005: Populate the raw-tag map and multi-value fields for all formats

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving on. If a
> STOP condition occurs, stop and report — do not improvise. When done, update
> the status row for this plan in `plans/README.md`.
>
> **Drift check (run first)**: `git diff --stat aca9750..HEAD -- internal/mp3/id3v2.go internal/m4a/metadata.go internal/m4a/audiobook.go internal/vorbis/comments.go internal/types/tags.go`
> If any file cited changed, compare against the excerpts before proceeding; on a mismatch, STOP.

## Status

- **Priority**: P1
- **Effort**: M
- **Risk**: MED (changes observable output of `Tags.All()`/`Get()` and multi-value slices for MP3/M4A)
- **Depends on**: 001
- **Category**: bug / tech-debt (documented public API returns nothing for two of five formats)
- **Planned at**: commit `aca9750`, 2026-10-30 (refreshed from 7b22897, 2026-07-01)

## Why this matters

`Tags.All()`, `Tags.Get(key)`, `Tags.GetFirst`, `Tags.GetBest`, and `Tags.Filter` all read
from the private `raw map[string][]string`. **Only the Vorbis path populates it** — `tags.Set`
is called from exactly one place, `internal/vorbis/comments.go:153` (verified: it is the only
non-test caller in the repo). The MP3 and M4A parsers never call `Set` and never append to
`Artists`/`Performers`. Consequences for a consumer:

- `file.Tags.Get("TIT2")` / `Get("©nam")` → empty for MP3/M4A, even though the docs
  (`internal/types/tags.go:84`) give exactly those keys as examples.
- `file.Tags.All()` → yields nothing for MP3/M4A files.
- `file.Tags.Artists` → empty for MP3/M4A (only `Artist` is set); `Performers` empty for all
  formats except Vorbis.

So the public raw-tag surface silently works for FLAC/Ogg and silently fails for MP3/M4A. This
plan makes every parser populate the raw map and the multi-value slices consistently.

## Current state

**Vorbis (correct, the model to match)** — `internal/vorbis/comments.go`:
```go
case "ARTIST":
	tags.Artist = value
	tags.Artists = append(tags.Artists, value)   // multi-value populated
...
tags.Set(key, value)                              // raw map populated (line 153, every comment)
```

**MP3 (missing)** — `internal/mp3/id3v2.go:245-291` `parseTextFrame` sets `file.Tags.Artist = text`
for `TPE1` but never appends `Artists` and never records the raw frame ID. `parseTXXXFrame`,
`parseCommentFrame` similarly don't touch `raw`.

**M4A (missing)** — `internal/m4a/metadata.go:93-124` `mapTagToField` sets `file.Tags.Artist = value`
for `\xA9ART` but never appends `Artists` and never records the raw atom name. `parseAudiobookTags`
(`internal/m4a/audiobook.go`) likewise.

`Tags.Set` (`internal/types/tags.go:153`) lazily creates the map and `slices.Clone`s the values;
it is safe to call repeatedly. Calling `Set(key, value)` twice for the same key **overwrites** —
so for multi-value tags accumulate into a slice and `Set(key, slice...)` once, OR add a raw-append
helper (see Step 1).

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
- `internal/types/tags.go` — add an `addRaw`/`AddRaw` append helper if needed (see Step 1)
- `internal/mp3/id3v2.go` — record raw frame IDs + populate `Artists`
- `internal/m4a/metadata.go`, `internal/m4a/audiobook.go` — record raw atom names + populate `Artists`
- The corresponding `_test.go` files
- `internal/vorbis/comments.go` — only if you add an append helper it should use (keep behavior identical)

**Out of scope**:
- Do not change the *standard field* mappings (Title/Artist/etc. keep their current values).
- Do not consolidate the mapping tables — that's plan 016 (which depends on this).
- FLAC uses the Vorbis path already; no FLAC-specific change needed.

## Git workflow

- Branch: `advisor/005-raw-tag-parity`
- Commit style: gitmoji single-line, no body, no attribution (e.g. `✨ Populate raw tags for MP3 and M4A`).
- Do NOT push or open a PR unless instructed.

## Steps

### Step 1: Decide the raw-append contract (add a helper if multi-value)

`Set(key, values...)` replaces. For multi-value tags (Genre, Composer appearing more than once)
you need append semantics. Add an internal append helper to `internal/types/tags.go`:

```go
// addRaw appends a value to the raw tag map under key, creating the entry if absent.
// Used by format parsers to record every raw tag occurrence (including repeats).
func (t *Tags) addRaw(key, value string) {
	if t.raw == nil {
		t.raw = make(map[string][]string)
	}
	t.raw[key] = append(t.raw[key], value)
}
```

Because it's unexported it's only callable from within `package types`. The parsers are in other
packages, so either (a) export it as `AddRaw`, or (b) have parsers accumulate and call the existing
exported `Set(key, values...)` once. **Choose (a) `AddRaw`** — simpler for the parsers and matches
`Set` being exported. Update `internal/vorbis/comments.go:153` to use `AddRaw(key, value)` too so
repeated Vorbis keys accumulate rather than overwrite (this is a latent Vorbis bug the change fixes;
verify existing Vorbis tests still pass — if a test asserts overwrite behavior, STOP and report).

**Verify**: `go build ./...` → exit 0.

### Step 2: MP3 — record raw frame IDs and populate Artists

In `internal/mp3/id3v2.go`:
- In `parseTextFrame` (after computing `text`), call `file.Tags.AddRaw(frame.ID, text)` for every
  text frame so `Get("TPE1")` etc. work.
- For `TPE1` (Artist), also `file.Tags.Artists = append(file.Tags.Artists, text)` when `text != ""`.
- In `parseTXXXFrame`, record the raw `TXXX:<description>` (or the description key) so custom tags are
  retrievable via `All()`. Use a stable key convention — recommend the uppercased description, matching
  how a consumer would look it up; document the choice in a one-line comment.

Keep it minimal: do not change which standard fields get set.

**Verify**: `go test ./internal/mp3/` → pass.

### Step 3: M4A — record raw atom names and populate Artists

In `internal/m4a/metadata.go` `mapTagToField`: after the switch, call
`file.Tags.AddRaw(tag, value)` (the atom name, e.g. `"\xA9ART"`) for every parsed tag. For `\xA9ART`
(Artist) also append to `file.Tags.Artists`. In `internal/m4a/audiobook.go`
`mapAudiobookField`/`parseCustomAtomWithTags`, record the custom field name → value into raw as well
(these are the `----`/`com.apple.iTunes` custom tags a consumer will want via `Get`).

**Verify**: `go test ./internal/m4a/` → pass.

### Step 4: Assert parity across formats

Add a test (in `audiometa_corpus_test.go` from plan 001, or a new `internal` test) that opens each
`testdata/` fixture and asserts:
- `len(f.Tags.All-materialized) > 0` for **every** format (materialize by iterating `All()` into a map),
- `f.Tags.Get("<title-key-for-format>")` returns the title (e.g. `"TIT2"` for MP3, `"\xA9nam"` for M4A,
  `"TITLE"` for Vorbis/FLAC),
- `f.Tags.Artists` is non-empty for every format that had an artist.

**Verify**: `go test -race ./...` → all pass.

## Test plan

- New tests: cross-format raw-tag presence + `Artists` population, using plan 001's corpus.
- Also unit tests: MP3 `parseTextFrame` records `TPE1` into raw and Artists; M4A `mapTagToField`
  records `\xA9ART` into raw and Artists.
- Pattern: existing `internal/mp3/parser_test.go` frame tests and `internal/m4a/metadata_test.go`.
- Verification: `go test -race ./...` all pass.

## Done criteria

- [ ] `go build ./...`, `go vet ./...`, `mise exec -- golangci-lint run ./...` all exit 0
- [ ] `go test -race ./...` exits 0 including cross-format parity tests
- [ ] `grep -rn "AddRaw\|\.Set(" internal/mp3 internal/m4a` shows raw population in both parsers
- [ ] Opening `testdata/sample.mp3` and `sample.m4a` yields non-empty `Tags.All()` and correct `Tags.Get(titleKey)`
- [ ] Existing Vorbis/FLAC tests still pass (no regression from the `Set`→`AddRaw` change)
- [ ] `plans/README.md` status row for 005 updated

## STOP conditions

- An existing test asserts that repeated Vorbis keys overwrite (so switching `Set`→`AddRaw` breaks it)
  — STOP and report; the append change may need to be Vorbis-scoped.
- Recording raw tags changes a *standard field* value in any existing test — STOP; standard mapping must
  be untouched.
- The raw-key convention for TXXX/custom atoms is ambiguous and no existing consumer test pins it — pick
  the documented convention, note it in the plan report, and proceed (this is a defensible default, not a blocker).

## Maintenance notes

- Plan 016 consolidates the per-format mapping into a shared normalizer; it assumes this parity exists.
  Land 005 first.
- A reviewer should confirm every format now populates `raw` (grep for `AddRaw` in each parser) and that
  `Artists`/`Performers`/`Genres`/`Composers` are appended consistently.
- Follow-up deferred: `Performers` is still only populated by Vorbis (no MP3/M4A source frame maps to it
  cleanly); leave as-is and note it, don't invent a mapping.
