# Plan 028: Match Vorbis comment keys case-insensitively and store them under a canonical key

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving on. If a
> STOP condition occurs, stop and report — do not improvise. When done, update
> the status row for this plan in `plans/README.md`.
>
> **Drift check (run first)**: `git diff --stat aca9750..HEAD -- internal/vorbis/comments.go internal/vorbis/comments_test.go internal/types/tags.go`
> If any file changed, compare the "Current state" excerpts against the live code. The one
> **expected** change is plan 005 having added `AddRaw` to `internal/types/tags.go` and switched
> `internal/vorbis/comments.go:153` to `tags.AddRaw(key, value)` — that is fine (see Step 0). Any
> other mismatch: STOP.

## Status

- **Priority**: P2
- **Effort**: S
- **Risk**: LOW (observable change: lower/mixed-case Vorbis keys now populate standard fields, and
  raw keys from FLAC/Ogg/Opus are stored upper-cased)
- **Depends on**: none. Coordinates with plan 005 (see Step 0) — either order works.
- **Category**: bug
- **Planned at**: commit `aca9750`, 2026-10-30

## Why this matters

The Vorbis comment spec says field names are case-insensitive, and the parser's own doc comments
say so — but `vorbis.ParseComment` switches on the raw key, so `title=`, `Artist=`, `album=`,
`replaygain_track_gain=` etc. map to nothing. Files tagged by tools that write lowercase keys
(several do) come back with empty Title/Artist/Album/ReplayGain from **both FLAC and Ogg/Opus**
(shared code). Raw keys are also stored as written, so `Tags.Get("TITLE")` misses a file tagged
`title=`, and the same logical tag spelled two ways lands under two raw keys. After this plan, keys
are matched case-insensitively and stored under one canonical (upper-case) spelling.

**Repeated keys** (two `ARTIST=` / `GENRE=` / `METADATA_BLOCK_PICTURE=` comments) currently keep
only the last value in the raw map because `Tags.Set` replaces. That bug is **owned by plan 005**
(Step 1 there adds `Tags.AddRaw` and switches the Vorbis call to it). This plan does not duplicate
it; it only uses `AddRaw` if 005 has already landed.

## Current state

- `internal/vorbis/comments.go` — `ParseComment`, shared by FLAC (`internal/flac/parser.go:305`)
  and Ogg/Opus (`internal/ogg/vorbis.go:141`, `internal/ogg/opus.go:155`).
- `internal/types/tags.go` — `Tags` with private `raw map[string][]string`; `Get` (lines 82–106)
  does an exact-match map lookup; `Set` (lines 141–164) replaces the slice.
- `internal/ogg/artwork.go:35-49` — reads pictures via `file.Tags.All()` and
  `strings.EqualFold(key, "METADATA_BLOCK_PICTURE")` (works around the raw-case issue; leave as is).

`internal/vorbis/comments.go:15-41` and `:150-155` (verified):

```go
// ParseComment parses a single Vorbis comment in "KEY=VALUE" format
// and populates the appropriate fields in the File struct.
//
// Vorbis comment field names are case-insensitive but typically uppercase.
// The comment is also stored in the raw tags map.
//
// Returns an error if the comment is not in valid "KEY=VALUE" format.
func ParseComment(comment string, file *types.File) error { //nolint:gocyclo // ...
	tags := &file.Tags
	// Find the '=' separator
	eq := -1
	...
	key := comment[:eq]
	value := comment[eq+1:]

	// Map Vorbis comments to standard Tags fields
	// Vorbis comment field names are case-insensitive, but typically uppercase
	switch key {
	case "TITLE":
	...
	}

	// Store in raw tags as well
	tags.Set(key, value)

	return nil
}
```

All `case` labels in the switch are already upper-case (verified, lines 42–149), and `strings` is
already imported.

`internal/types/tags.go:82-106` (verified):

```go
// Get retrieves all values for a tag key.
//
// Tag keys are format-specific (e.g., "TITLE", "TIT2", "©nam").
// Returns an empty slice if the key doesn't exist.
...
func (t *Tags) Get(key string) []string {
	...
	values := t.raw[key]
```

Multi-value mapped fields (verified): `ARTIST` sets `Artist` (last wins) and appends to `Artists`;
`GENRE`, `COMPOSER`, `PERFORMER` append to `Genres`/`Composers`/`Performers`. These already
accumulate correctly across repeats; this plan doesn't change them. Existing test
`TestParseComment_MultipleArtists` (`internal/vorbis/comments_test.go:199`) pins
"Artist = last value" — keep it passing.

**Design decision (made here, don't revisit):** canonicalize at storage time with
`strings.ToUpper(key)`; keep `Tags.Get` an exact-match lookup. Rationale: `Tags.Get` is shared with
MP3/M4A raw keys (`TIT2`, `©nam` — plan 005), where case-folding would be surprising, and upper-case
is the conventional Vorbis spelling, so `Get("TITLE")` now works for every FLAC/Ogg/Opus file.
Document it on `Get`.

Conventions: tests are table-driven in `internal/vorbis/comments_test.go` (see `TestParseComment`
at line 10, each case a `check func(*types.File) bool`). Lint (`.golangci.yml`): `nolintlint`
reports unused `//nolint` directives; `ParseComment` keeps its `//nolint:gocyclo`.

## Commands you will need

| Purpose | Command | Expected |
|---|---|---|
| Build | `go build ./...` | exit 0 |
| Vet | `go vet ./...` | exit 0 |
| Targeted tests | `go test ./internal/vorbis/ -run ParseComment -v` | all pass |
| All tests | `go test ./...` and `go test -race ./...` | all pass |
| Lint | `mise exec -- golangci-lint run ./...` | `0 issues.` |
| Format | `gofmt -l .` | no output |

Always run golangci-lint via `mise exec --` (pinned 2.15.0; a stale global on PATH gives false
errors). `go.mod` is `go 1.26.0` — no newer APIs.

## Scope

**In scope**:
- `internal/vorbis/comments.go` — key canonicalization + doc comments
- `internal/vorbis/comments_test.go` — new tests
- `internal/types/tags.go` — **doc comment on `Get` only**

**Out of scope**:
- Adding `AddRaw` / changing `Tags.Set` semantics — plan 005 owns repeated-key accumulation.
- Making `Tags.Get` case-insensitive (decided against above).
- `internal/ogg/artwork.go` — its `EqualFold` keeps working; simplifying it is churn.
- Trimming whitespace around keys (`"TITLE ="`) — not allowed by the spec; leave.
- `internal/vorbis/chapters.go` (already upper-cases keys) — plan 024.
- Changing "Artist = last value" to first value — product question; note it in your report if you
  think it matters, don't change it.

## Git workflow

- Branch: `advisor/028-vorbis-comment-keys`
- One commit, gitmoji single line, no body (e.g. `🐛 Match Vorbis comment keys case-insensitively`).
- Do NOT push or open a PR unless instructed.

## Steps

### Step 0: Detect whether plan 005 has landed

Run: `grep -n "func (t \*Tags) AddRaw" internal/types/tags.go`
- **Match** → 005 landed; the raw-store line in `ParseComment` is `tags.AddRaw(key, value)`. Use
  `AddRaw` in Step 1 and do Test 4 below.
- **No match** → the line is `tags.Set(key, value)`. Keep `Set` in Step 1 and skip Test 4.

Record which case applied in your report.

### Step 1: Canonicalize the key in `ParseComment`

In `internal/vorbis/comments.go`:
- Replace `key := comment[:eq]` with
  ```go
  // Field names are case-insensitive per the Vorbis comment spec; canonicalize to upper case so
  // "title=" maps like "TITLE=" and the raw map has one key per logical tag.
  key := strings.ToUpper(comment[:eq])
  ```
- Delete the now-redundant comment above the `switch` (`// Vorbis comment field names are
  case-insensitive, but typically uppercase`), keeping `// Map Vorbis comments to standard Tags fields`.
- Leave the `switch key` and all cases unchanged.
- The raw-store line stays `tags.Set(key, value)` (or `tags.AddRaw(key, value)` per Step 0) — it now
  receives the canonical key.
- Update the function doc: replace `// Vorbis comment field names are case-insensitive but typically
  uppercase.\n// The comment is also stored in the raw tags map.` with
  ```go
  // Field names are matched case-insensitively, and the comment is stored in the raw tags
  // map under its upper-cased name (e.g. "title=Foo" is retrievable as Get("TITLE")).
  ```

**Verify**: `go test ./internal/vorbis/ ./internal/flac/ ./internal/ogg/` → all existing tests pass
(including `TestParseComment_EmptyKey`: `strings.ToUpper("") == ""`).

### Step 2: Document the canonical spelling on `Tags.Get`

In `internal/types/tags.go`, after the line `// Tag keys are format-specific (e.g., "TITLE", "TIT2", "©nam").`
add:

```go
// Vorbis-comment keys (FLAC, Ogg Vorbis, Opus) are stored upper-cased, so look them up as
// "TITLE", not "title". Lookups are exact-match.
```

No code change in this file.

**Verify**: `go build ./...` → exit 0; `gofmt -l .` → no output.

### Step 3: Tests, then full gates

Add the tests below, then:

**Verify**: `go test -race ./...` → all pass; `mise exec -- golangci-lint run ./...` → `0 issues.`;
`go vet ./...` → exit 0.

## Test plan

In `internal/vorbis/comments_test.go`:

1. `TestParseComment_CaseInsensitiveKeys` — table, same shape as `TestParseComment`:
   - `"title=Lower"` → `Tags.Title == "Lower"`
   - `"Artist=Mixed"` → `Tags.Artist == "Mixed"` and `Artists == ["Mixed"]`
   - `"album=x"` → `Tags.Album == "x"`
   - `"tracknumber=7"` → `Tags.TrackNumber == 7`
   - `"Series=Discworld"` → `Tags.Series == "Discworld"`
   - `"replaygain_track_gain=-6.50 dB"` → `Audio.ReplayGain != nil && TrackGain == -6.5`
2. `TestParseComment_RawKeyCanonicalized` — `ParseComment("title=Foo")` →
   `Get("TITLE") == ["Foo"]` and `Get("title") == nil`; iterate `Tags.All()` and assert the only key
   is `"TITLE"`.
3. `TestParseComment_MixedCaseSameKey` — `"GENRE=Rock"` then `"genre=Jazz"` →
   `Genres == ["Rock", "Jazz"]`; and materializing `All()` yields exactly one key, `"GENRE"`.
4. **Only if Step 0 matched (`AddRaw` exists):** `TestParseComment_RepeatedRawValues` —
   `"METADATA_BLOCK_PICTURE=a"` then `"metadata_block_picture=b"` →
   `Get("METADATA_BLOCK_PICTURE") == ["a", "b"]` (this is what makes Ogg `ExtractArtwork`, which reads
   the raw map, return every embedded picture).

Existing tests that must keep passing unchanged: `TestParseComment_StoresRawTag`,
`TestParseComment_UnknownTag` (uses `CUSTOMTAG` — already upper), `TestParseComment_EmptyKey`,
`TestParseComment_MultipleArtists`.

## Done criteria

- [ ] `go build ./...`, `go vet ./...` exit 0; `gofmt -l .` prints nothing
- [ ] `mise exec -- golangci-lint run ./...` → `0 issues.`
- [ ] `go test -race ./...` passes, including tests 1–3 (and 4 if `AddRaw` exists)
- [ ] `grep -n "strings.ToUpper(comment\[:eq\])" internal/vorbis/comments.go` → one match
- [ ] `git status` shows only the three in-scope files modified
- [ ] `plans/README.md` status row for 028 updated

## STOP conditions

- An existing test asserts that a lowercase key is stored/retrievable in lowercase (e.g.
  `Get("title")`) — STOP and report; that pins the old behavior and needs a product call.
- The drift check shows `ParseComment` restructured beyond plan 005's `Set`→`AddRaw` swap (e.g.
  plan 016 moved the mapping table elsewhere) — STOP; the canonicalization point has moved.
- Anything outside `internal/vorbis/` or the `Get` doc comment seems to need changing.

## Maintenance notes

- **Observable change for consumers:** for FLAC/Ogg/Opus, `Tags.All()` now yields upper-cased keys
  and `Tags.Get` must be called with upper-case Vorbis names. Mention in the changelog.
- **If this lands before plan 005:** 005's drift check will flag `internal/vorbis/comments.go`; its
  Step 1 should then change `tags.Set(key, value)` → `tags.AddRaw(key, value)` (the `key` is now the
  canonical one) — same intent, no conflict.
- Plan 016 (consolidation) should keep canonicalization in the shared normalizer.
- Reviewer: confirm there is exactly one `ToUpper` on the key and the switch labels are unchanged.
