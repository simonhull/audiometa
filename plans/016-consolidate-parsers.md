# Plan 016: Consolidate duplicated per-format tag/series/chapter logic

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving on. If a
> STOP condition occurs, stop and report — do not improvise. When done, update
> the status row for this plan in `plans/README.md`.
>
> **Drift check (run first)**: `git diff --stat aca9750..HEAD -- internal/`
> This plan touches many files; verify each excerpt just before editing its file.
> On a material mismatch, STOP.

## Status

- **Priority**: P3
- **Effort**: L
- **Risk**: MED (behavior-preserving refactor across all four parsers; wide test surface)
- **Depends on**: 001 (corpus/integration tests), 005 (raw-tag parity — do NOT consolidate before it)
- **Category**: tech-debt
- **Planned at**: commit `aca9750`, 2026-10-30 (refreshed from 7b22897, 2026-07-01)

## Why this matters

The same logic is re-implemented per format and has already diverged:

1. **Series-fallback block** (Grouping → Series, then path → SeriesPart) exists in four places
   with drift: `internal/flac/parser.go:149-163`, `internal/ogg/parser.go:166-180`,
   `internal/mp3/id3v2.go:59-80` (`resolveSeriesFallbacks`), `internal/m4a/audiobook.go:45-61` —
   and M4A alone adds title/album parsing via `resolveSeriesPart` (`audiobook.go:168-199`).
2. **Chapter end-time calculation** ("end = next chapter's start; last = file duration") is coded
   five times: `internal/vorbis/chapters.go:111-120`, `internal/m4a/chapters.go` (two places:
   `parseChplChapters` and `calculateChapterEndTimes`), `internal/flac/cuesheet.go` (~:247-259),
   and implicitly in `internal/mp3/id3v2.go` chapter building — with a divergent edge: the
   FLAC/Vorbis versions leave the last chapter's `EndTime` 0 when duration is unknown; M4A differs.
3. **Image dimension/MIME sniffing** is byte-for-byte duplicated:
   `internal/mp3/artwork.go:205-260` and `internal/m4a/artwork.go:154-209`
   (`detectImageDimensions`/`detectJPEGDimensions`/`detectPNGDimensions`).
4. **Narrator ← first Composer fallback** exists in MP3 (`internal/mp3/parser.go:56-59`) and M4A
   (`internal/m4a/audiobook.go:40-43`) but not FLAC/Ogg — decide and unify.

Every audiobook-tag change currently means editing 4–5 files that are quietly inconsistent.
This plan extracts shared helpers and routes all parsers through them, pinning current behavior
with characterization tests first.

## Current state — canonical excerpts

The duplicated series-fallback (FLAC flavor, `internal/flac/parser.go:149-163`, verified —
ogg/mp3 are near-identical):

```go
// Post-parse fallbacks for audiobook series metadata.
if file.Tags.Series == "" && file.Tags.Grouping != "" {
	series, part := parsing.ParseGrouping(file.Tags.Grouping)
	if series != "" {
		file.Tags.Series = series
		if file.Tags.SeriesPart == "" && part != "" {
			file.Tags.SeriesPart = part
		}
	}
}
if file.Tags.Series != "" && file.Tags.SeriesPart == "" {
	if part := parsing.ExtractSeriesPartFromPath(path); part != "" {
		file.Tags.SeriesPart = part
	}
}
```

M4A's richer version (`internal/m4a/audiobook.go:56-61` + `resolveSeriesPart:168-199`) adds, between
the Grouping and path fallbacks: custom-atom candidates ("Series Part"/"Series Position"/"Part"/
"Volume"), then title parsing, then album parsing. **The consolidated helper must support the M4A
ordering as options/hooks — do not silently upgrade FLAC/Ogg/MP3 to M4A behavior or downgrade M4A**;
behavior per format stays exactly as today (that's what characterization tests pin).

Existing shared packages: `internal/parsing` (series text utilities — natural home for the series
fallback), `internal/types` (Chapter — natural home for end-time fill), and a new
`internal/imagemeta` for the sniffers.

## Commands you will need

| Purpose | Command | Expected |
|---|---|---|
| Build | `go build ./...` | exit 0 |
| Tests | `go test ./...` | all pass |
| Race | `go test -race ./...` | all pass |
| Coverage (mapping fns) | `go test -coverprofile=/tmp/c.out ./... && go tool cover -func=/tmp/c.out \| grep -E "resolveSeries\|mapTag\|mapAudiobook"` | shows improved % |
| Lint | `mise exec -- golangci-lint run ./...` | exit 0, prints `0 issues.` |

> **Lint note**: run lint via `mise exec -- golangci-lint run ./...` so the mise-pinned
> golangci-lint v2.15.0 is used. A stale global golangci-lint (e.g. 2.11.4) on PATH reports
> false "export data version" typecheck errors under Go 1.27.

## Scope

**In scope**:
- `internal/parsing/` — new `fallbacks.go` (series fallback helper) + tests
- `internal/types/chapter.go` or new `internal/types/chapters.go` — `FillChapterEndTimes` + tests
- New `internal/imagemeta/` package — dimension/MIME sniffing + tests
- Callers: `internal/flac/parser.go`, `internal/ogg/parser.go`, `internal/mp3/id3v2.go`,
  `internal/mp3/parser.go`, `internal/m4a/audiobook.go`, `internal/m4a/chapters.go`,
  `internal/mp3/artwork.go`, `internal/m4a/artwork.go`, `internal/vorbis/chapters.go`,
  `internal/flac/cuesheet.go`
- Characterization tests in each parser package (write these FIRST)

**Out of scope**:
- The tag-key → field mapping tables themselves (`mapTagToField`, `parseTextFrame` switch,
  Vorbis switch) — full normalization is a bigger refactor; this plan only extracts the four
  duplications above. Note tag-map consolidation as deferred follow-up.
- Any behavior change. If characterization reveals a divergence (e.g. last-chapter EndTime),
  **preserve each format's current behavior** via a parameter and record the divergence in the
  report for a human decision.
- Public API surface.

## Git workflow

- Branch: `advisor/016-consolidate-parsers`
- Commit style: gitmoji single-line, no body, no attribution (e.g. `♻️ Extract shared series-fallback helper`).
- Commit per extraction (fallbacks / chapters / imagemeta / narrator) so each is independently revertable.
- Do NOT push or open a PR unless instructed.

## Steps

### Step 1: Characterization tests first

Before moving any code, pin current behavior per format:
- Series fallback: table tests per parser feeding Tags with `{Series, Grouping, SeriesPart}`
  combinations + a path, asserting resulting `Series`/`SeriesPart`. Include an M4A case that
  exercises title/album resolution (custom tags → title → album → path priority).
- Chapter end-times: per format, chapters with/without known file duration; assert each
  chapter's `EndTime`, especially the last one (document today's 0-vs-duration divergence in
  the test names).
- Image sniffing: JPEG SOF0/SOF2 and PNG IHDR byte fixtures → width/height; identical
  expectations for the MP3 and M4A copies (they're byte-identical today, so one table reused twice).

**Verify**: `go test ./...` → all new characterization tests pass against the *current* code.

### Step 2: Extract `parsing.ResolveSeriesFallbacks`

Create in `internal/parsing/fallbacks.go`:

```go
// SeriesSource provides optional extra series-part candidates, in priority order
// (used by M4A: explicit custom atoms, then title, then album).
type SeriesSource struct {
	ExplicitParts []string // e.g. values of "Series Part", "Series Position", "Part", "Volume"
	Title, Album  string   // parsed for part patterns when set
}

// ResolveSeriesFallbacks applies the shared post-parse series logic:
// Grouping → Series(+part); then src candidates; then path.
// Mutates tags in place. Matches the historical per-format behavior exactly.
func ResolveSeriesFallbacks(tags *types.Tags, path string, src SeriesSource) { ... }
```

`internal/parsing` currently doesn't import `internal/types` — check for an import cycle
(`types` must not import `parsing`; verify with `go build`). If a cycle exists, take
`*types.Tags`-shaped fields as plain `*string` params instead — keep it simple.

Switch the four call sites; FLAC/Ogg/MP3 pass a zero `SeriesSource`; M4A passes its custom-tag
candidates and title/album.

**Verify**: `go test ./...` → all characterization tests still pass, per format.

### Step 3: Extract `types.FillChapterEndTimes`

```go
// FillChapterEndTimes sets each chapter's EndTime to the next chapter's StartTime;
// the last chapter gets fileDuration when > 0, else keeps zero (historical behavior
// for FLAC/Vorbis; M4A callers that differ pass their own duration handling — see tests).
func FillChapterEndTimes(chapters []Chapter, fileDuration time.Duration) { ... }
```

> **Note (2026-10-30):** plan 024 must land before this step. It fixes the FLAC cuesheet lead-out,
> FLAC `CHAPTERxxx` comments, and the Ogg ordering so the last chapter's EndTime is the file
> duration instead of zero. After 024, the "keeps zero" case above only applies when the duration
> is genuinely unknown (0); characterize the post-024 behavior, not the pre-024 one. If 024 has
> not landed, STOP and execute 024 first.

Replace the five implementations. Where characterization shows divergence, either parameterize or
(if the divergence is a bug per the tests' documentation) keep both behaviors and flag in the report.

**Verify**: `go test ./...` → chapter characterization tests pass per format.

### Step 4: Extract `internal/imagemeta`

Move `detectImageDimensions`/`detectJPEGDimensions`/`detectPNGDimensions` (and MP3's
`detectMIMEType` if M4A can share it — compare both first) into `internal/imagemeta`, exported as
`Dimensions(data []byte) (w, h int)` / `MIMEType(data []byte) string`. Delete both copies; update
callers.

**Verify**: `go test ./...`; `grep -rn "detectJPEGDimensions" internal/mp3 internal/m4a` → no matches.

### Step 5: Unify the narrator fallback placement

Extract `Narrator ← Composers[0]` into the shared post-parse step (natural place: call it from
each parser exactly where it happens today — MP3 `parser.go:56-59` and M4A `audiobook.go:40-43`).
**Do not add it to FLAC/Ogg** (behavior change); instead add a one-line comment in each of those
parsers noting the deliberate asymmetry, and record it in your report as a product question.

**Verify**: full gates — `go test -race ./...`, `mise exec -- golangci-lint run ./...` → pass.

## Test plan

- Characterization tests (Step 1) are the core deliverable alongside the refactor; they live in
  each parser package plus the new shared packages' own unit tests.
- Verification: identical pass results before (Step 1) and after (Steps 2–5) the moves.

## Done criteria

- [ ] All Step 1 characterization tests exist and pass both before and after the refactor
- [ ] `grep -rn "ParseGrouping" internal/flac internal/ogg internal/mp3 internal/m4a` → only via the shared helper
- [ ] `grep -rn "detectJPEGDimensions\|detectPNGDimensions" internal/` → only `internal/imagemeta`
- [ ] Chapter end-time logic exists in exactly one exported function
- [ ] `go build ./...`, `go vet ./...`, `mise exec -- golangci-lint run ./...`, `go test -race ./...` all exit 0
- [ ] Report lists any behavior divergences found and preserved
- [ ] `plans/README.md` status row for 016 updated

## STOP conditions

- A characterization test's expectation is ambiguous because current behavior is
  nondeterministic (e.g. map iteration order affecting first-wins) — STOP and report; that's a
  bug to surface, not to bake into a helper.
- Import cycle between `internal/parsing` and `internal/types` that the plain-params fallback
  can't cleanly resolve — STOP with the dependency sketch.
- Any parser's corpus test (plan 001) output changes — STOP; this refactor must be invisible.

## Maintenance notes

- Deferred (record in report): full tag-key→field normalization (one canonical mapping table
  with per-format alias tables) — the natural next consolidation once this lands; and Ogg's
  eager retention of base64 `METADATA_BLOCK_PICTURE` in raw tags (memory cost) which a
  normalizer could filter.
- Reviewer: diff each extraction against the deleted copies line-by-line; the risk is a silent
  first-wins/ordering change, which the characterization tests are designed to catch.
