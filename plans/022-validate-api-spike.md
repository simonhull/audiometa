# Plan 022: Design a Validate()/Issue QA surface with warning codes (spike)

> **Executor instructions**: This is a **design spike**. The deliverable is a design document
> with an API sketch (and optionally a throwaway prototype branch that is never merged). There
> must be **no behavior change on main**. Follow the steps in order, run every verification
> command, and if a STOP condition occurs, stop and report — do not improvise. When done, update
> the status row for this plan in `plans/README.md`.
>
> **Drift check (run first)**: `git diff --stat aca9750..HEAD -- internal/types/errors.go errors.go file.go options.go internal/ogg/opus.go internal/types/artwork.go internal/types/chapter.go`
> If `Warning` gained a `Code`/`Severity` field, or plan 015 already removed the Opus info
> warnings, update the inventory in Step 1 to match the live code before continuing.

## Status

- **Priority**: direction
- **Effort**: M
- **Risk**: LOW (docs-only on main)
- **Depends on**: none (reads better after plans 008 and 015; coordinates with 021 and 036)
- **Category**: direction
- **Planned at**: commit `aca9750`, 2026-10-30

## Why this matters

Today every parse problem is a `Warning{Stage, Message, Err, Offset}` with a free-text message.
A consumer that wants to react — "quarantine files with truncated tags, but ignore Opus output-gain
notes" — has to string-match English messages that can change any release. `WithStrictParsing` is
an all-or-nothing gate that treats an informational note the same as a corrupt header. And the
questions an audiobook server actually asks at ingest — *is the narrator missing? do chapters
overrun the duration? is the cover art 40 MB?* — are not parse errors at all; every consumer
re-implements them. A small, typed `Validate()` surface with stable codes and severities would
fix all three, and gives plan 021's CLI a `validate --json` command for free. This spike designs
that surface without changing any behavior.

## Current state

- `internal/types/errors.go:44-87` (verified):

  ```go
  // Parsing stages reported in Warning.Stage.
  const (
  	StageMetadata  = "metadata"
  	StageTechnical = "technical"
  	StageChapters  = "chapters"
  	StageArtwork   = "artwork"
  )

  type Warning struct {
  	// Stage where the warning occurred
  	Stage string // one of StageMetadata, StageTechnical, StageChapters, StageArtwork
  	// Warning message
  	Message string
  	// Err is the underlying error, if any. ...
  	Err error
  	// File offset where the issue occurred (0 if not applicable)
  	Offset int64
  }
  func (w Warning) String() string { ... }
  func (w Warning) Unwrap() error { return w.Err }
  ```

  Root `errors.go:19-29` re-exports `Warning` and the four `Stage*` constants. `Stage` is a plain
  `string`, not a named type. There is no code, kind or severity field.
- Strict mode, `file.go:100-104`:

  ```go
  // Check strict parsing mode
  if options.strictParsing && len(file.Warnings) > 0 {
  	_ = f.Close()
  	return nil, fmt.Errorf("strict parsing failed: %s", file.Warnings[0].Message)
  }
  ```

  Observed quirks to record in the design: (a) only the first warning's message reaches the error
  and the error does not wrap anything (`errors.As` can't find the cause); (b) `ignoreWarnings`
  is applied earlier (`file.go:157-160`, inside `openReader`) and clears `file.Warnings`, so
  `WithIgnoreWarnings()` + `WithStrictParsing()` silently disables strict mode; (c) the
  artwork-preload warning (`file.go:106-116`) is appended **after** the strict check, so strict
  mode never sees it.
- Options (`options.go:16-21`): `strictParsing`, `preloadArtwork`, `ignoreWarnings`,
  `maxArtworkSize` (the last is stored but never read at `aca9750`; plan 008 implements it).
- Warning emission inventory at `aca9750` — 27 `types.Warning{` construction sites outside tests
  (`grep -rn 'Warning{' --include='*.go' internal file.go | grep -v _test`):
  - metadata: `internal/m4a/parser.go:112,143`, `internal/m4a/metadata.go:75`,
    `internal/mp3/parser.go:39`, `internal/mp3/id3v2.go:186`, `internal/flac/parser.go:71,91,101,308`,
    `internal/ogg/parser.go:69,106,138`, `internal/ogg/vorbis.go:116,128,144`, `internal/ogg/opus.go:129,141,158`
  - technical: `internal/m4a/parser.go:121`, `internal/mp3/parser.go:49`, `internal/ogg/parser.go:118,149`,
    `internal/ogg/opus.go:59,67`
  - chapters: `internal/m4a/parser.go:131`, `internal/flac/parser.go:124`
  - artwork: `file.go:111`
  The two `internal/ogg/opus.go:59,67` warnings are **informational** ("original sample rate was
  N Hz", "output gain: X dB") — plan 015 removes them and explicitly defers a severity field to
  this plan.
- Data available for ingest rules: `Tags` fields `Narrator`, `Series`, `SeriesPart`, `Title`,
  `Artist`, `Album`, `ISBN`, `ASIN` (`internal/types/tags.go:17-53`); `Chapter{Title, Index,
  StartTime, EndTime}` (`internal/types/chapter.go:23-28`); `AudioInfo.Duration`;
  `Artwork{MIMEType, Description, Data, Type, Width, Height}` (`internal/types/artwork.go:9-16`)
  — artwork is lazy (`(*File).ExtractArtwork`), so artwork rules cost I/O.
- Consumer context: ListenUp's server stores a `hasScanWarning` flag per book
  (`~/Code/listenup/ListenUp/contract/src/commonMain/kotlin/com/calypsan/listenup/api/sync/BookSyncPayload.kt`,
  field `hasScanWarning`) — a real use for a severity-filtered issue list. Verify before citing.

## Commands you will need

| Purpose | Command | Expected on success |
|---|---|---|
| Inventory | `grep -rn 'Warning{' --include='*.go' internal file.go \| grep -v _test` | 27 lines at `aca9750` |
| Build/Test | `go build ./... && go test -race ./...` | exit 0 / all `ok` |
| Lint | `mise exec -- golangci-lint run ./...` | `0 issues.` (must use `mise exec --`) |
| Main untouched | `git diff --stat main -- '*.go'` (on the docs branch) | no output |

## Scope

**In scope (docs branch `advisor/022-validate-api-spike`)**:
- `plans/design/022-validate-api.md` (create)

**In scope (optional throwaway branch `spike/022-validate-prototype`, never merged)**:
- a prototype `validate.go` + test in the root package, to check the API compiles and reads well

**Out of scope**:
- Any `.go` change on main/the docs branch — including adding `Code` to `Warning`. That is the
  follow-up implementation plan this spike produces.
- Removing the Opus info warnings (plan 015).
- Implementing `WithMaxArtworkSize` (plan 008).
- The CLI command (plan 021 adds `validate` after implementation).

## Git workflow

- Branch: `advisor/022-validate-api-spike`; prototype `spike/022-validate-prototype` (delete locally after reporting).
- gitmoji single-line, no body, no attribution, e.g. `📝 Add Validate API design spike`.
- Do NOT push or open a PR.

## Steps

### Step 1: Warning inventory table

In `plans/design/022-validate-api.md`, section `## Inventory`: a table with one row per site from
"Current state" — `file:line | Stage | message pattern | proposed Code | proposed Severity |
recoverable?`. Read each site; do not guess messages. Classify every row.

**Verify**: the table has exactly as many rows as the Inventory grep prints (27 at `aca9750`).

### Step 2: Severity taxonomy and codes

Section `## Taxonomy`:
- `type Severity uint8` with `SeverityInfo < SeverityWarning < SeverityError`, plus `String()` and
  `MarshalText` (lowercase `info|warning|error`). Define each in one sentence: *error* = data that
  should have been there is missing or corrupt (truncated tag, unreadable header); *warning* =
  data present but suspicious or partially recovered; *info* = notable but correct (Opus output gain).
- `type Code string` with stable, namespaced, lowercase constants, e.g. `metadata.frame_truncated`,
  `metadata.comment_invalid`, `technical.duration_unknown`, `chapters.parse_failed`,
  `artwork.preload_failed`, `artwork.too_large`, `ingest.narrator_missing`. String-typed so JSON and
  logs are readable and new codes are additive. Document the compatibility promise: codes are never
  renamed; removed codes are documented.
- Decide and justify: `type Stage string` (named) vs keep plain string constants. Changing the field
  type is source-compatible for code using the constants but breaks code that assigns arbitrary
  strings — note it.

### Step 3: Where codes live — Warning vs Issue

Section `## Data model`. Compare at least:
- (A) add `Code Code` and `Severity Severity` fields to `Warning` (additive struct change, zero
  values = unknown); parsers set them at the 27 sites.
- (B) a new `Issue` type returned only by `Validate`, which wraps parse warnings + ingest rules:
  ```go
  type Issue struct {
  	Code     Code     `json:"code"`
  	Severity Severity `json:"severity"`
  	Stage    string   `json:"stage"`
  	Message  string   `json:"message"`
  	Offset   int64    `json:"offset,omitempty"`
  	Err      error    `json:"-"`
  }
  ```
Recommend one (advisor leaning: A **and** B — codes on `Warning` so parse issues are typed at
source, `Issue` as the unified JSON-serializable output; `Validate` converts `Warning`→`Issue`).
State how an un-coded `Warning` (code `""`) maps (e.g. `unknown`, severity warning).

### Step 4: Audiobook ingest rules

Section `## Rules`. A table: `Code | Severity default | Condition | Needs artwork I/O?`. At least:
- `ingest.title_missing` — `Tags.Title == ""`
- `ingest.narrator_missing` — `Tags.Narrator == ""` (note: MP3 falls back to the first composer, `internal/mp3/parser.go:56-59`)
- `ingest.series_part_without_series` — `SeriesPart != "" && Series == ""`
- `chapters.none` — `len(Chapters) == 0 && Duration > 1h` (info)
- `chapters.overlap` — `Chapters[i].EndTime > Chapters[i+1].StartTime`
- `chapters.unsorted` — start times not non-decreasing
- `chapters.exceeds_duration` — any `EndTime > Duration + tolerance` (state the tolerance, e.g. 1s)
- `chapters.zero_length` — `EndTime <= StartTime`
- `artwork.missing` — no front cover
- `artwork.too_large` — `len(Data) > MaxArtworkBytes` (align the default with plan 008's `maxArtworkSize`)
- `artwork.too_small` — `Width/Height` below a threshold when known (state that `0` means unknown, not small)
- `technical.duration_unknown` — `Duration == 0`

### Step 5: API sketch

Section `## API`:

```go
// ValidateOptions selects rules and thresholds. The zero value runs the
// default rule set with documented defaults.
type ValidateOptions struct {
	Rules           []Code   // nil = defaults
	MinSeverity     Severity // drop issues below this
	CheckArtwork    bool     // opt-in: triggers ExtractArtwork I/O
	MaxArtworkBytes int
	MinArtworkPx    int
	DurationSlack   time.Duration
}

func (f *File) Validate(opts ValidateOptions) []Issue
```

Address: should it take `context.Context` (artwork I/O → probably `ValidateContext` alongside, matching
`ExtractArtwork`/`ExtractArtworkContext` in `file.go:182-224`); deterministic ordering of results
(by severity desc, then stage, then offset); whether rules are pluggable (advisor: not in v1).

### Step 6: Reconcile strict mode

Section `## Strict mode`. Propose `WithStrictParsing()` ≡ "fail if any parse warning has severity
≥ error", returning an error type that wraps the first offending `Warning` (`errors.As` works), and
fixing quirks (b) and (c) from Current state (order ignore-vs-strict explicitly; decide whether the
artwork-preload warning is subject to strict). Spell out the behavior change: files that today fail
strict mode only because of info-level warnings will start passing. Propose an optional
`WithStrictSeverity(Severity)` instead of changing semantics silently — pick one and justify.

### Step 7: JSON and CLI

Section `## JSON`: the exact JSON for an `Issue` (from Step 3's tags), `Severity`/`Code` as strings,
`Err` omitted but its `Error()` text optionally in `"error"`. Show one full example output for a file
with three issues. Note plan 021's DTO convention (snake_case, `_ms` integer milliseconds) and match it.

### Step 8: Open questions and follow-up plan outline

Section `## Open questions` (≥ 6 items: e.g. per-format rules; i18n of messages; whether chapter
`Source` from plan 036 affects rules — e.g. OverDrive markers are per-file; multi-file books;
thresholds from config; performance budget). Section `## Implementation plan outline`: ordered
list of the follow-up implementation steps (add types → set codes at 27 sites → Validate → strict
reconciliation → CLI).

**Verify**:
- `grep -c '^## ' plans/design/022-validate-api.md` → ≥ 9 (Inventory, Taxonomy, Data model, Rules, API, Strict mode, JSON, Open questions, Implementation plan outline)
- `awk '/^## Open questions/{f=1;next}/^## /{f=0}f && /^- /' plans/design/022-validate-api.md | wc -l` → ≥ 6
- `git diff --stat main -- '*.go'` → no output

### Step 9 (optional): Prototype

On `spike/022-validate-prototype` only, implement `Severity`, `Code`, `Issue` and `Validate` with
three rules (`ingest.narrator_missing`, `chapters.exceeds_duration`, `chapters.overlap`) plus a table
test, to check the API reads well. Record findings in `## Prototype findings` on the docs branch.
Never merge the prototype branch.

## Test plan

- None on main (docs-only). Docs branch: `go build ./... && go test -race ./...` still pass (nothing changed).
- Prototype branch (optional): one table test per prototype rule.

## Done criteria

- [ ] `plans/design/022-validate-api.md` exists with ≥ 9 `##` sections as listed in Step 8
- [ ] Inventory table row count equals the inventory grep line count
- [ ] Rules table has ≥ 12 rows; Open questions ≥ 6 items
- [ ] `git diff --stat main -- '*.go'` on the docs branch → empty
- [ ] `plans/README.md` row for 022 updated

## STOP conditions

- The inventory grep count differs from 27 and you can't reconcile it with drift since `aca9750` — report the diff.
- Any step seems to require a `.go` change on the docs branch.
- `Warning` already has a `Code`/`Severity` field (someone implemented part of this) — stop and report.

## Maintenance notes

- Implementing this touches every parser's warning sites; schedule it after plan 016 (parser
  consolidation) if that is in flight, or the 27 sites will move under you.
- Plan 015 removes the two Opus info warnings; if this design reintroduces them as `SeverityInfo`,
  say so explicitly in the follow-up plan so the two plans don't fight.
- If plan 017 wired `OutOfBoundsError`, map `errors.As(w.Err, *OutOfBoundsError)` to a
  `metadata.truncated` / `technical.truncated` code.
