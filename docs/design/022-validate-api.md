# 022: Validate() / Issue QA surface with stable warning codes (design spike)

Status: design only. No `.go` change accompanies this document; the optional prototype (Step 9) was skipped.
Written against branch `fix/p2-spikes` (Go 1.27 base). The library is a general-purpose public tool, so
every proposal is additive and source-compatible; the only behavior change (strict mode) is called out and
opt-in.

Premise checks: `Warning` has no `Code`/`Severity` (`internal/types/errors.go`); `Stage` is a plain `string`;
`WithMaxArtworkSize` stores `maxArtworkSize` but nothing reads it (plan 008 not landed);
`OutOfBoundsError` exists (`errors.go` alias) but is not wired into warning sites. The inventory grep
(`grep -rn 'Warning{' --include='*.go' internal file.go | grep -v _test`) prints 27 lines, matching the plan.
The two informational Opus warnings from the original inventory are gone (plan 015); the live set still totals 27 sites, listed below.

## Inventory

All 27 non-test `Warning{` sites, read from live code. "Recoverable" = parsing continues and other data is
still returned. Offsets are only set at `ogg/parser.go:68` today.

| # | Site | Stage | Message pattern | Proposed Code | Severity | Recoverable? |
|---|---|---|---|---|---|---|
| 1 | `internal/flac/parser.go:72` | metadata | `failed to read metadata block header at offset %d: %v` | `metadata.block_header_unreadable` | error | yes (stops walking blocks) |
| 2 | `internal/flac/parser.go:92` | metadata | `failed to parse STREAMINFO: %v` | `technical.streaminfo_invalid` | error | yes (no audio info) |
| 3 | `internal/flac/parser.go:104` | metadata | `failed to parse Vorbis comments: %v` | `metadata.comments_unparseable` | error | yes |
| 4 | `internal/flac/parser.go:127` | chapters | `failed to parse CUESHEET: %v` | `chapters.cuesheet_invalid` | warning | yes |
| 5 | `internal/flac/parser.go:266` | technical | `STREAMINFO total samples is 0 (unknown); duration not available` | `technical.duration_unknown` | warning | yes |
| 6 | `internal/flac/parser.go:324` | metadata | `invalid Vorbis comment: %s` | `metadata.comment_invalid` | warning | yes (one comment skipped) |
| 7 | `internal/m4a/parser.go:58` | metadata | `err.Error()` from `findIlst` (not "atom not found") | `metadata.ilst_unreadable` | error | yes |
| 8 | `internal/m4a/parser.go:68` | metadata | `err.Error()` from `extractIlstMetadata` | `metadata.ilst_parse_failed` | error | partial |
| 9 | `internal/m4a/parser.go:78` | technical | `err.Error()` from `parseTechnicalInfo` | `technical.parse_failed` | error | yes |
| 10 | `internal/m4a/parser.go:88` | chapters | `err.Error()` from `parseChapters` | `chapters.parse_failed` | warning | yes |
| 11 | `internal/m4a/parser.go:100` | metadata | `err.Error()` from `parseAudiobookTags` | `metadata.audiobook_tags_failed` | warning | yes |
| 12 | `internal/m4a/metadata.go:132` | metadata | `failed to parse tag %s: %v` | `metadata.tag_invalid` | warning | yes (one atom skipped) |
| 13 | `internal/mp3/parser.go:38` | metadata | `ID3v2 parsing failed: %v` | `metadata.id3v2_failed` | error | yes |
| 14 | `internal/mp3/parser.go:48` | technical | `failed to parse MP3 technical info: %v` | `technical.parse_failed` | error | yes |
| 15 | `internal/mp3/id3v2.go:188` | metadata | `failed to read frame %s: %v` | `metadata.frame_unreadable` | warning | yes (frame skipped) |
| 16 | `internal/ogg/opus.go:118` | metadata | `truncated comment %d (missing length field)` | `metadata.comment_truncated` | warning | yes |
| 17 | `internal/ogg/opus.go:130` | metadata | `truncated comment %d data (expected %d bytes)` | `metadata.comment_truncated` | warning | yes |
| 18 | `internal/ogg/opus.go:147` | metadata | `invalid Opus tag: %s` | `metadata.comment_invalid` | warning | yes |
| 19 | `internal/ogg/parser.go:68` | metadata | `failed to read Ogg page %d: %v` (has Offset) | `metadata.page_unreadable` | error | yes (stops paging) |
| 20 | `internal/ogg/parser.go:108` | technical | `failed to calculate duration: %v` | `technical.duration_unknown` | warning | yes |
| 21 | `internal/ogg/parser.go:121` | metadata | `failed to parse Vorbis comment header: %v` | `metadata.comments_unparseable` | error | yes |
| 22 | `internal/ogg/parser.go:140` | technical | `failed to calculate duration: %v` | `technical.duration_unknown` | warning | yes |
| 23 | `internal/ogg/parser.go:152` | metadata | `failed to parse OpusTags header: %v` | `metadata.comments_unparseable` | error | yes |
| 24 | `internal/ogg/vorbis.go:115` | metadata | `truncated comment %d` | `metadata.comment_truncated` | warning | yes |
| 25 | `internal/ogg/vorbis.go:127` | metadata | `truncated comment %d data` | `metadata.comment_truncated` | warning | yes |
| 26 | `internal/ogg/vorbis.go:143` | metadata | `invalid Vorbis comment: %s` | `metadata.comment_invalid` | warning | yes |
| 27 | `file.go:110` | artwork | `preload artwork failed: %v` | `artwork.preload_failed` | warning | yes |

Row count: 27, equal to the grep output. Observations:

- Many sites share a code (`metadata.comment_truncated`, `technical.duration_unknown`, `metadata.comments_unparseable`): the code names the *condition*, the `Message` carries the per-site detail. Format is derivable from the file, so it is not repeated in the code.
- M4A sites 7-11 and 9/10 use `err.Error()` verbatim as the message. They need a sentinel or typed error per cause before codes can be assigned beyond the coarse ones above (implementation outline step 2).
- `errors.As(w.Err, *OutOfBoundsError)` should map to `metadata.truncated`/`technical.truncated` once plan 017 wires that error.
- Site 5 and sites 20/22 are the same user-visible fact ("duration unknown") and also feed rule `technical.duration_unknown` in Validate. Validate must de-duplicate: when a parse warning already carries that code, do not also emit the rule-based issue.

## Taxonomy

```go
// Severity grades an Issue. Higher is worse.
type Severity uint8

const (
	SeverityInfo Severity = iota + 1 // zero value is deliberately invalid/unset
	SeverityWarning
	SeverityError
)

func (s Severity) String() string // "info" | "warning" | "error" | "unknown"
func (s Severity) MarshalText() ([]byte, error)
func (s *Severity) UnmarshalText(b []byte) error
```

- **error**: data that should have been there is missing or corrupt (truncated tag, unreadable header, unparseable stream info). The file is partially unusable.
- **warning**: data is present but suspicious, partially recovered, or skipped (one bad comment, unknown duration).
- **info**: notable but correct (Opus output gain, non-default sample rate). No site emits info today (plan 015 removed them); the level exists for rules and future notes.

Starting `iota + 1` keeps the zero value distinguishable as "not set", which is how an un-coded legacy `Warning`
is detected.

```go
// Code is a stable, machine-readable identifier of an issue.
type Code string

const (
	CodeMetadataCommentInvalid   Code = "metadata.comment_invalid"
	CodeMetadataFrameUnreadable  Code = "metadata.frame_unreadable"
	CodeTechnicalDurationUnknown Code = "technical.duration_unknown"
	CodeChaptersParseFailed      Code = "chapters.parse_failed"
	CodeArtworkPreloadFailed     Code = "artwork.preload_failed"
	CodeArtworkTooLarge          Code = "artwork.too_large"
	CodeIngestNarratorMissing    Code = "ingest.narrator_missing"
	CodeUnknown                  Code = "unknown"
)
```

Format: `<namespace>.<snake_case>`, namespaces `metadata`, `technical`, `chapters`, `artwork`, `ingest`.
`Code` is string-typed so JSON and logs are readable and new codes are additive.

**Compatibility promise** (documented on the type): a code, once released, is never renamed and never reused
for a different condition; a code may be retired, in which case it stays documented as "no longer emitted".
Messages are *not* covered by the promise and may change any release.

**Stage: named type or plain string?** Recommendation: keep plain `string` for `Warning.Stage` and the
existing untyped constants. Changing the field to `type Stage string` is source-compatible for code that
uses `StageMetadata` (untyped constants convert) and for `w.Stage == audiometa.StageMetadata`, but breaks
code that assigns a `string` variable to `Stage`, passes it to `func(string)`, or switches over `string`
keys. With external users, the type-safety win is small against that breakage. `Issue.Stage` is new and
may be typed `Stage` safely; the proposal is `type Stage string` introduced for `Issue` only, with
`Stage(w.Stage)` conversion in `Validate`. Revisit at a major version.

## Data model

**(A) Fields on `Warning`:** add `Code Code` and `Severity Severity`. Additive; zero values mean unset.
Parsers set them at the 27 sites. Pro: parse problems are typed at the source, `file.Warnings` consumers
benefit immediately without calling `Validate`. Con: touches every parser; `Warning` gets two more fields.

**(B) New `Issue` returned only by `Validate`:**

```go
type Issue struct {
	Code     Code     `json:"code"`
	Severity Severity `json:"severity"`
	Stage    Stage    `json:"stage"`
	Message  string   `json:"message"`
	Offset   int64    `json:"offset,omitempty"`
	Err      error    `json:"-"`
}
```

Pro: one JSON-serialisable shape that unifies parse warnings and ingest rules; `Warning` stays untouched.
Con alone: parse problems stay untyped until converted, so the code assignment must live in `Validate`
by string-matching messages, which is exactly the fragility this spike exists to remove.

**Recommendation: A and B.** Codes and severities on `Warning` (set at source, so there is no message
matching anywhere), and `Issue` as the unified output. `Validate` converts each `Warning` to an `Issue`
(`Issue{Code: w.Code, Severity: w.Severity, Stage: Stage(w.Stage), Message: w.Message, Offset: w.Offset,
Err: w.Err}`) and appends rule-based issues. An un-coded `Warning` (`Code == ""`, e.g. from a future parser
or older code path) maps to `Code: "unknown"`, `Severity: SeverityWarning`. `Warning.Severity == 0` also maps
to `SeverityWarning`. A `String()` on `Issue` mirrors `Warning.String()`.

## Rules

Default thresholds apply when `ValidateOptions` fields are zero. "Artwork I/O" means the rule calls
`ExtractArtwork` (lazy, reads file) and is opt-in via `CheckArtwork`.

| Code | Default severity | Condition | Artwork I/O? |
|---|---|---|---|
| `ingest.title_missing` | warning | `Tags.Title == ""` | no |
| `ingest.narrator_missing` | warning | `Tags.Narrator == ""`. Note MP3 falls back to the first composer (`internal/mp3/parser.go:56-58`), so an MP3 with a composer never fires. | no |
| `ingest.series_part_without_series` | warning | `SeriesPart != "" && Series == ""` | no |
| `ingest.artist_missing` | info | `Artist == ""` and no album artist | no |
| `chapters.none` | info | `len(Chapters) == 0 && Audio.Duration > 1h` | no |
| `chapters.overlap` | warning | `Chapters[i].EndTime > Chapters[i+1].StartTime` for sorted neighbours | no |
| `chapters.unsorted` | warning | start times not non-decreasing in slice order | no |
| `chapters.exceeds_duration` | warning | any `EndTime > Duration + DurationSlack` (default slack 1s). Skipped when duration is 0/unknown. | no |
| `chapters.zero_length` | warning | `EndTime <= StartTime` (last chapter exempt when `EndTime` was derived from duration and equals it) | no |
| `chapters.duplicate_title` | info | two or more chapters share a title (case-insensitive, non-empty) | no |
| `artwork.missing` | info | no `ArtworkFrontCover` (falls back to "any image" when no typed cover exists) | yes |
| `artwork.too_large` | warning | `len(Data) > MaxArtworkBytes` (default aligned with plan 008's `maxArtworkSize`; 0 there means no limit, so here default 10 MiB) | yes |
| `artwork.too_small` | info | both `Width` and `Height` known (non-zero) and either below `MinArtworkPx` (default 300). `0` means unknown, never "small". | yes |
| `technical.duration_unknown` | warning | `Audio.Duration == 0` (suppressed if a parse warning with this code exists) | no |

14 rows. `chapters.*` rules require chapters sorted by index; `Validate` uses `Index` order, not time order,
for `unsorted`, so the rule is meaningful.

## API

```go
// ValidateOptions selects rules and thresholds. The zero value runs the default
// rule set with documented defaults.
type ValidateOptions struct {
	Rules           []Code        // nil = default set; non-nil = only these (parse-warning issues always included)
	MinSeverity     Severity      // drop issues below this; zero = SeverityInfo
	CheckArtwork    bool          // opt-in: triggers ExtractArtwork I/O
	MaxArtworkBytes int           // default 10 MiB
	MinArtworkPx    int           // default 300
	DurationSlack   time.Duration // default 1s
}

// Validate reports parse warnings and ingest-rule violations as a single ordered list.
func (f *File) Validate(opts ValidateOptions) []Issue

// ValidateContext is Validate with cancellation for the artwork rules.
func (f *File) ValidateContext(ctx context.Context, opts ValidateOptions) ([]Issue, error)
```

- **Context.** Artwork rules do I/O, so `ValidateContext` exists alongside, matching `ExtractArtwork`/`ExtractArtworkContext`. It returns an `error` only for cancellation (a rule that cannot run is reported as an issue, not an error). `Validate` is `ValidateContext(context.Background(), ...)` with the error discarded (cannot occur with a background context).
- **Ordering.** Deterministic: severity descending, then stage, then offset, then code, then message. Stable across runs so JSON can be diffed in tests.
- **Dedup.** Identical `(code, offset, message)` triples collapse to one.
- **Pluggable rules.** Not in v1: a `Rule` interface commits the public surface to the `Tags`/`Chapter` shapes. Revisit when a second consumer needs custom rules; `Rules []Code` already allows selection.
- **Does not mutate.** `Validate` never changes `f.Warnings`; with `WithIgnoreWarnings()` the parse-warning half is empty by construction (documented).

## Strict mode

Current behavior, with the three quirks verified in live code (`file.go` strict check after `openReader`,
`ignoreWarnings` cleared inside `openReader`, artwork-preload warning appended after the strict check):

(a) only the first warning's message reaches the error and nothing is wrapped (`fmt.Errorf` without `%w`);
(b) `WithIgnoreWarnings()` + `WithStrictParsing()` silently disables strict mode because warnings are
discarded before the check; (c) the artwork-preload warning is appended after the strict check, so strict
mode never sees it.

Proposal:

- `WithStrictParsing()` keeps its current meaning (fail on any warning) so existing callers are not silently changed; the new option below carries the severity-graded behavior.
- Add `WithStrictSeverity(min Severity) Option`: fail if any warning has `Severity >= min` (un-coded legacy warnings count as warning). `WithStrictParsing()` is documented as `WithStrictSeverity(SeverityInfo)` (every warning), which is exactly today's behavior. **Pick: the optional option, not a silent redefinition.** Justification: this is a public library; redefining strict to "errors only" would make files that fail today start passing, and the reverse risk (users relying on strict to quarantine anything odd) is real. A new, explicit option makes the new meaning discoverable and keeps old callers unchanged.
- Return a typed error: `type StrictError struct{ Warning Warning; Count int }` with `Error()` ("strict parsing failed: <msg> (and N more)") and `Unwrap() error { return w.Warning }`, so `errors.As(err, &StrictError{})` and `errors.As(err, &outOfBounds)` (via `Warning.Unwrap`) both work. This fixes quirk (a).
- Order explicitly: strict evaluation runs on the *unfiltered* warning list, before `ignoreWarnings` clears it. `WithIgnoreWarnings()` then only affects what lands in `File.Warnings` afterwards. This fixes quirk (b). Behavior change: `WithIgnoreWarnings()+WithStrictParsing()` now fails on warnings instead of silently passing; this is a bug fix but should be called out in the changelog.
- Artwork-preload warning (quirk c): make it subject to strict mode (move the check after preload). Behavior change: strict + preload files with bad artwork now fail at `Open` rather than succeed with a warning. Called out in the changelog.
- Behavior change summary: default users (no strict) see no change. `WithStrictParsing()` users see (b) and (c) tightened. Only users of the new `WithStrictSeverity` see severity-graded behavior.

## JSON

`Severity` and `Code` serialise as strings, `Err` is omitted from the structured form; its text is exposed as
an optional `error` field via a custom `MarshalJSON` (kept off the struct tag so `Err` stays non-serialised).
Field names are snake_case and any duration is integer `_ms`, matching the plan 021 DTO convention.

```go
type Issue struct { /* as above */ }
// MarshalJSON emits {"code","severity","stage","message","offset?","error?"}
```

Example output for a file with three issues:

```json
[
  {
    "code": "metadata.comments_unparseable",
    "severity": "error",
    "stage": "metadata",
    "message": "failed to parse Vorbis comments: unexpected EOF",
    "offset": 4096,
    "error": "unexpected EOF"
  },
  {
    "code": "chapters.exceeds_duration",
    "severity": "warning",
    "stage": "chapters",
    "message": "chapter 12 \"Epilogue\" ends at 9h30m4s, after duration 9h30m0s",
    "context": {"chapter_index": 12, "end_ms": 34204000, "duration_ms": 34200000}
  },
  {
    "code": "ingest.narrator_missing",
    "severity": "warning",
    "stage": "metadata",
    "message": "no narrator tag"
  }
]
```

The `context` object (optional, rule-specific numeric detail with `_ms` suffixes) is an extension the
example shows; it is optional in v1 and omitted for parse warnings. If the maintainer prefers a minimal
type, drop `context` and keep the numbers in `message`.

## Open questions

- Per-format rules: should `Validate` know that ID3 narrator falls back to composer, or that Ogg has no artwork support, so rules do not fire spuriously?
- i18n of messages: messages stay English and unstable; is a `MessageKey`/parameter map worth it for non-English UIs, or is `Code` enough?
- Chapter provenance (plan 036): OverDrive markers are per-file; should `chapters.*` rules be skipped or relaxed when `Source` says the chapters came from a heuristic?
- Multi-file books: `Validate` is per file; where do cross-file rules (continuous chapter numbering, mixed narrators) live, and is that this API or a caller concern?
- Thresholds from config: should `ValidateOptions` be loadable from a file/JSON for the CLI (`validate --config`)?
- Performance budget: artwork rules decode image headers for width/height when the container does not give them; what cap keeps `Validate` cheap on a large library scan?
- Should `Validate` ever mutate or auto-fix? (Assumed no.) How does it relate to write support (plan 020): is a file with error-level issues writable?
- Severity stability: can a code's default severity change in a minor release, or is severity part of the compatibility promise?
- Should info-level issues appear in `File.Warnings` at all, or only via `Validate`?

## Implementation plan outline

1. Add types: `Severity`, `Code`, constants, `Issue`, and `Code`/`Severity` fields on `Warning` (internal/types), re-export from the root package. Pure additions, no behavior change.
2. Introduce per-cause sentinel/typed errors in the M4A parser so sites 7-11 can be coded without message matching.
3. Set `Code` and `Severity` at the 27 sites, one parser package per commit, with a test per format asserting the code on a crafted corrupt input. Schedule after plan 016 (parser consolidation) if in flight, or the sites move.
4. Add `Validate`/`ValidateContext` with the rule table (CheckArtwork opt-in), deterministic ordering, dedup, and table tests per rule; coordinate default artwork limit with plan 008.
5. Add `Issue` JSON marshalling and golden-file tests.
6. Strict-mode reconciliation: `StrictError`, ordering fix (b), preload fix (c), `WithStrictSeverity`; changelog note for the tightening.
7. CLI: `audiometa validate --json` (plan 021) consuming `Issue`.
8. Wire `OutOfBoundsError` to `metadata.truncated`/`technical.truncated` once plan 017 lands.

## Prototype findings

Skipped (optional Step 9). The API sketch was not compiled against the tree. Risk to check first when
prototyping: `Issue.Stage` typed as `Stage` versus `Warning.Stage` as `string` needs one explicit conversion
in `Validate`, which is the only place the two meet.
