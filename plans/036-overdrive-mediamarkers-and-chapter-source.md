# Plan 036: Read OverDrive MediaMarkers chapters and record each chapter's source

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving on. If a
> STOP condition occurs, stop and report — do not improvise. When done, update
> the status row for this plan in `plans/README.md` (add a row for 036 if none exists).
>
> **Drift check (run first)**: `git diff --stat aca9750..HEAD -- internal/types/chapter.go chapter.go internal/mp3/ internal/m4a/chapters.go internal/flac/cuesheet.go internal/vorbis/chapters.go`
> Plan 026 (ID3 frame dispatch fix) is recommended first and **will** change
> `internal/mp3/id3v2.go`. If it landed, re-locate `processFrame`, `parseTXXXFrame` and
> `txxxFieldHandlers` by name and confirm they still behave as the excerpts below describe
> (TXXX frames reach a description-keyed handler map; CHAP frames are collected and turned into
> chapters after all frames are read). If they no longer do, STOP.

## Status

- **Priority**: direction
- **Effort**: S–M
- **Risk**: LOW–MED (adds a public struct field and a public type — additive; new parsing path only runs on files carrying the marker frame)
- **Depends on**: plans/026 (ID3 frame dispatch fix) recommended first; not a hard dependency
- **Category**: direction
- **Planned at**: commit `aca9750`, 2026-10-30

## Why this matters

OverDrive/Libby MP3 audiobooks — a large share of library-borrowed audiobooks — carry no `CHAP`
frames. Their chapters live in an ID3 `TXXX` frame described `OverDrive MediaMarkers` as a small
XML document. audiometa ignores that frame, so these books show zero chapters, while
Audiobookshelf (and the maintainer's own ListenUp server, which re-implemented it) recover them.
Separately, `Chapter` carries no provenance: M4A silently prefers QuickTime text-track chapters
over Nero `chpl` and discards the loser, so when a user reports "wrong chapters", nobody can tell
which extraction path produced them. Adding a typed `Chapter.Source` fixes the diagnosis problem
and is the natural place to label the new OverDrive path.

## Current state

- `internal/types/chapter.go:23-28` (verified) — no provenance field:

  ```go
  type Chapter struct {
  	Title     string        `json:"title"`
  	Index     int           `json:"index"`
  	StartTime time.Duration `json:"start_time"`
  	EndTime   time.Duration `json:"end_time"`
  }
  ```

  Root `chapter.go:9`: `type Chapter = types.Chapter` (alias; root re-exports types this way —
  see `errors.go:23-29` for how constants are re-exported).
- Chapter construction sites (every one must set `Source`):
  - `internal/m4a/chapters.go:118` — Nero `chpl` (`parseChplChapters`)
  - `internal/m4a/chapters.go:488` — QuickTime text track (`buildChaptersFromText`)
  - `internal/mp3/id3v2.go:436` — ID3v2 `CHAP` (`parseChapterFrames`)
  - `internal/flac/cuesheet.go:267` — FLAC `CUESHEET` (`cuesheetToChapters`)
  - `internal/vorbis/chapters.go:127` — Vorbis `CHAPTERxxx` comments (`ParseChapters`, used by Ogg Vorbis and Opus: `internal/ogg/vorbis.go:153`, `internal/ogg/opus.go:167`)
- M4A precedence, `internal/m4a/chapters.go:10-39`: QuickTime first; if it yields chapters with no
  error, Nero is never consulted; the losing list is discarded silently.
- MP3 frame dispatch, `internal/mp3/id3v2.go:212-224`:

  ```go
  func processFrame(frame ID3v2Frame, file *types.File, chapters *[]ID3v2Frame) {
  	switch {
  	case strings.HasPrefix(frame.ID, "T") && frame.ID != "TXXX":
  		parseTextFrame(frame, file)
  	case frame.ID == "TXXX":
  		parseTXXXFrame(frame, file)
  	case frame.ID == "COMM":
  		parseCommentFrame(frame, file)
  	case frame.ID == "CHAP":
  		*chapters = append(*chapters, frame)
  	}
  }
  ```

  No `CTOC` handling. `parseTXXXFrame` (`id3v2.go:330-350`) decodes `[encoding][description\0][value]`
  and looks up `txxxFieldHandlers[strings.ToLower(description)]` (map at `id3v2.go:295-319`,
  type `map[string]func(*types.File, string)`); there is no MediaMarkers entry.
  `grep -rni 'mediamarker\|overdrive' --include='*.go' .` → no output.
- `parseID3v2` (`id3v2.go:38-58`) processes **all** frames first, then
  `if len(chapters) > 0 { file.Chapters = parseChapterFrames(chapters, file.Audio.Duration) }` —
  so anything a TXXX handler puts in `file.Chapters` is overwritten when CHAP frames exist. That
  gives "CHAP wins, MediaMarkers is the fallback" for free.
- Ordering problem: `internal/mp3/parser.go:35-53` runs `parseID3v2` **before**
  `parseTechnicalInfo`, so `file.Audio.Duration` is 0 while ID3 frames are parsed. The last
  MediaMarkers chapter's `EndTime` must be filled after the technical pass.
- `decodeText` (`id3v2.go:479-503`) does not strip trailing NULs; ID3v2.4 TXXX values may end in `\x00`.
- The marker format (from Audiobookshelf's `parseOverdriveMediaMarkers.js`, ported in ListenUp at
  `~/Code/listenup/ListenUp/server/src/commonMain/kotlin/com/calypsan/listenup/server/scanner/pipeline/OverdriveChapters.kt:15-31` — read it as reference data):

  ```xml
  <Markers>
    <Marker><Name>Chapter 1</Name><Time>0:00.000</Time></Marker>
    <Marker><Name>Chapter 2</Name><Time>15:51.000</Time></Marker>
  </Markers>
  ```

  `Time` is relative to the start of **this file**. Formats seen: `m:ss.fff` (minutes may exceed
  59 on long tracks) and `h:mm:ss.fff`. A multi-file book has one marker block per file;
  stitching files together is the consumer's job (ListenUp does it), not this library's.
- ListenUp's own chapter-source vocabulary, for alignment
  (`~/Code/listenup/ListenUp/contract/src/commonMain/kotlin/com/calypsan/listenup/domain/embeddedmeta/ChapterSource.kt`):
  `Mp4Chpl, Mp4TextTrack, Id3v2Chap, FlacCuesheet, OggVorbisComment, None`. Note its comment says
  its MP4 parser **prefers Nero over the text track** — the opposite of audiometa. Do not change
  audiometa's precedence here; `Source` makes the difference visible.
- Tests that build chapters: `internal/mp3/parser_test.go:275` `TestParseChapterFrames`,
  `internal/m4a/chapters_test.go:43-200` (`TestParseChapters_*`, chpl only). There are **no**
  tests for FLAC cuesheet chapters or Vorbis `ParseChapters`. TXXX test pattern:
  `internal/mp3/parser_test.go:238-273` `TestParseTXXXFrame` (builds `ID3v2Frame{ID:"TXXX", Data: …}`
  and calls `parseTXXXFrame`). No test compares whole `Chapter` structs (no `DeepEqual`), so adding
  a field breaks no existing assertion.

## Commands you will need

| Purpose | Command | Expected on success |
|---|---|---|
| Build | `go build ./...` | exit 0 |
| Vet | `go vet ./...` | exit 0 |
| Focused tests | `go test -race ./internal/mp3/ ./internal/m4a/ ./internal/flac/ ./internal/vorbis/ ./internal/ogg/ ./internal/types/` | all `ok` |
| All tests | `go test -race ./...` | all `ok` |
| Lint | `mise exec -- golangci-lint run ./...` | `0 issues.` (must use `mise exec --`) |
| Format | `gofmt -l .` | no output |

## Scope

**In scope**:
- `internal/types/chapter.go` (+ create `internal/types/chapter_test.go` if useful)
- `chapter.go` (root re-export)
- `internal/m4a/chapters.go`, `internal/m4a/chapters_test.go`
- `internal/mp3/id3v2.go`, `internal/mp3/parser.go`, `internal/mp3/mediamarkers.go` (create), `internal/mp3/mediamarkers_test.go` (create), `internal/mp3/parser_test.go`
- `internal/flac/cuesheet.go`, `internal/flac/parser_test.go` (or a new `cuesheet_test.go`)
- `internal/vorbis/chapters.go`, `internal/vorbis/chapters_test.go` (create)

**Out of scope**:
- M4A precedence (QuickTime vs Nero) — unchanged.
- Exposing the losing candidate lists (e.g. `File.ChapterCandidates`) — deferred; note it in your report.
- Multi-file aggregation of MediaMarkers.
- Filtering OverDrive "sub-chapters" (names containing `(` + digit or "continued"; ABS/ListenUp drop them) — that is consumer policy; return every marker.
- `CTOC` parsing.
- The Vorbis zero-entry bug (see Maintenance notes) and chapter end-time consolidation (plan 016).
- `README.md` (plan 009 owns it; mention MediaMarkers in your report so it can be added).

## Git workflow

- Branch: `advisor/036-overdrive-mediamarkers`
- gitmoji single-line commits, no body, no attribution, e.g. `✨ Record chapter source on every parser`,
  `✨ Parse OverDrive MediaMarkers chapters from TXXX`.
- Do NOT push or open a PR.

## Steps

### Step 1: Add the `ChapterSource` type and field

In `internal/types/chapter.go`:

```go
// ChapterSource identifies which container structure a chapter list was read from.
// Values are stable strings, safe to persist and compare.
type ChapterSource string

const (
	ChapterSourceQuickTime     ChapterSource = "quicktime"      // MP4 chapter text track (tref/chap)
	ChapterSourceNero          ChapterSource = "nero"           // MP4 udta/chpl
	ChapterSourceID3CHAP       ChapterSource = "id3_chap"       // ID3v2 CHAP frames
	ChapterSourceOverDrive     ChapterSource = "overdrive"      // ID3v2 TXXX "OverDrive MediaMarkers"
	ChapterSourceCueSheet      ChapterSource = "flac_cuesheet"  // FLAC CUESHEET block
	ChapterSourceVorbisComment ChapterSource = "vorbis_comment" // CHAPTERxxx comments (Ogg Vorbis/Opus)
)
```

Give each constant a full-sentence doc comment (the `godot`/`revive` linters require it — follow the
style of `internal/types/format.go:14-33`). Add the field **last** in `Chapter`:
`Source ChapterSource \`json:"source,omitempty"\`` with a doc comment ("empty means unknown").
Update the type's doc comment list (`chapter.go:7-11`) to mention OverDrive MediaMarkers for MP3.

In root `chapter.go`, add `type ChapterSource = types.ChapterSource` and a `const (...)` block
re-exporting the six constants (pattern: root `errors.go:23-29`).

**Verify**: `go build ./... && go vet ./...` → exit 0.

### Step 2: Set `Source` at every existing construction site

- `internal/m4a/chapters.go:118` → `Source: types.ChapterSourceNero`
- `internal/m4a/chapters.go:488` → `Source: types.ChapterSourceQuickTime`
- `internal/mp3/id3v2.go:436` → `Source: types.ChapterSourceID3CHAP`
- `internal/flac/cuesheet.go:267` → `Source: types.ChapterSourceCueSheet`
- `internal/vorbis/chapters.go:127` → `Source: types.ChapterSourceVorbisComment`

**Verify**: `grep -rn 'types.Chapter{' --include='*.go' internal | grep -v _test` → 5 lines, and
`grep -rn -A6 'types.Chapter{' --include='*.go' internal | grep -v _test | grep -c 'Source:'` → `5`.
`go test ./...` → ok.

### Step 3: MediaMarkers parser (pure function)

Create `internal/mp3/mediamarkers.go`:

```go
// mediaMarkersDescription is the TXXX description OverDrive/Libby use for chapter markers.
const mediaMarkersDescription = "overdrive mediamarkers" // compared lower-cased

// maxMediaMarkers bounds work on hostile input.
const maxMediaMarkers = 10_000

type mediaMarkersXML struct {
	XMLName xml.Name `xml:"Markers"`
	Markers []struct {
		Name string `xml:"Name"`
		Time string `xml:"Time"`
	} `xml:"Marker"`
}

// parseMediaMarkers converts an OverDrive MediaMarkers XML document into
// chapters with file-relative start times. EndTime of each chapter is the next
// chapter's start; the last chapter's EndTime is left 0 for the caller to fill.
func parseMediaMarkers(doc string) ([]types.Chapter, error)

// parseMarkerTime parses "M:SS[.f]" or "H:MM:SS[.f]" (1-3 fraction digits).
func parseMarkerTime(s string) (time.Duration, error)
```

Rules — implement exactly these; anything else is an error (do not guess):
- `doc = strings.TrimRight(doc, "\x00")`, then `xml.Unmarshal`. Root element must be `Markers`.
- More than `maxMediaMarkers` markers → error. Zero markers → `nil, nil` (no chapters, no error).
- `parseMarkerTime`: `strings.TrimSpace`; split on `:`.
  - 2 parts `M:SS[.f]`: `M` = one or more ASCII digits (no upper bound — minutes may exceed 59);
  - 3 parts `H:MM:SS[.f]`: `H` digits, `MM` exactly two digits `00`–`59`;
  - `SS` (both forms) = exactly two digits `00`–`59` (`0:5.000` is rejected), optionally followed by `.` and 1–3 digits, scaled to milliseconds (`.5` = 500 ms, `.05` = 50 ms).
  - Use integer arithmetic only (`strconv.Atoi` per component, then `time.Duration` math) — no floats.
  - Any other shape (1 part, 4+ parts, signs, spaces inside, letters, fraction > 3 digits) → error naming the input.
- Start times must be non-decreasing in document order; otherwise error (ListenUp also rejects non-monotonic markers).
- `Title` = `strings.TrimSpace(Name)`; if empty, `fmt.Sprintf("Chapter %d", i+1)`.
- `Index` = 1-based position; `Source = types.ChapterSourceOverDrive`; `EndTime[i] = StartTime[i+1]`, last = 0.
- Errors are all-or-nothing: on any error return `nil, err` (no partial chapter list).

**Verify**: `go test ./internal/mp3/ -run 'MediaMarkers|MarkerTime'` → ok (tests from the Test plan).

### Step 4: Wire it into TXXX handling, CHAP-wins precedence

Add a handler entry to `txxxFieldHandlers` (`id3v2.go:295`):
`mediaMarkersDescription: applyMediaMarkers,` where

```go
func applyMediaMarkers(f *types.File, value string) {
	chapters, err := parseMediaMarkers(value)
	if err != nil {
		f.Warnings = append(f.Warnings, types.Warning{
			Stage:   types.StageChapters,
			Message: fmt.Sprintf("ignoring OverDrive MediaMarkers: %v", err),
			Err:     err,
		})
		return
	}
	if len(chapters) > 0 {
		f.Chapters = chapters
	}
}
```

Leave `parseID3v2`'s `if len(chapters) > 0 { file.Chapters = parseChapterFrames(...) }` as is — CHAP
frames therefore override MediaMarkers. Add a one-line comment there: `// CHAP frames take precedence over OverDrive MediaMarkers (set by a TXXX handler).`

(If plan 026 replaced the handler map with another dispatch, add the equivalent case in the new
structure; the behavior requirements are: matched case-insensitively on the description, warning
on parse error, CHAP still wins.)

**Verify**: `go test ./internal/mp3/` → ok.

### Step 5: Fill the last OverDrive chapter's end after the technical pass

In `internal/mp3/parser.go`, after the `parseTechnicalInfo` block (currently ending at line 53):

```go
// OverDrive markers carry only start times; close the last chapter at the file's end.
if n := len(file.Chapters); n > 0 {
	last := &file.Chapters[n-1]
	if last.Source == types.ChapterSourceOverDrive && last.EndTime == 0 && file.Audio.Duration > last.StartTime {
		last.EndTime = file.Audio.Duration
	}
}
```

**Verify**: `go test -race ./internal/mp3/` → ok.

### Step 6: Full gate

**Verify**: `go build ./... && go vet ./... && go test -race ./... && mise exec -- golangci-lint run ./... && gofmt -l .`
→ all clean.

## Test plan

New tests (table-driven; model TXXX frame construction on `TestParseTXXXFrame`,
`internal/mp3/parser_test.go:238-273`):

`internal/mp3/mediamarkers_test.go`:
- `parseMarkerTime`: `0:00.000`→0; `15:51.000`→15m51s; `75:30.250`→75m30.25s; `1:02:03.456`→1h2m3.456s;
  `1:02:03`→1h2m3s; `0:05.5`→5.5s; `0:05.05`→5.05s. Errors: `""`, `5.000`, `1:2:3:4`, `0:60.000`,
  `1:60:00.000`, `0:5.000`, `-1:00.000`, `0:05.0001`, `a:bc`.
- `parseMediaMarkers`: the two-marker example → 2 chapters, titles, starts, `EndTime[0]=StartTime[1]`,
  `EndTime[1]=0`, all `Source==ChapterSourceOverDrive`, Index 1 and 2; empty `<Markers/>` → nil, nil;
  malformed XML → error; wrong root → error; non-monotonic times → error; one bad `Time` → error and nil chapters;
  trailing `\x00` accepted; empty `Name` → "Chapter N"; `maxMediaMarkers+1` markers → error.
- Through `parseTXXXFrame`: ISO-8859-1 TXXX with description `OverDrive MediaMarkers` sets
  `file.Chapters`; description `overdrive mediamarkers` (lower-case) also works; UTF-16 (encoding 1,
  BOM) value works; bad XML → no chapters, one `StageChapters` warning.

`internal/mp3/parser_test.go`:
- Full `Parse` on a synthetic file: ID3v2.3 tag with the TXXX MediaMarkers frame + one MPEG frame
  (extend `createMinimalMP3WithID3`, `parser_test.go:99-135`, into a variant; fix the declared
  synchsafe tag size to match). Assert chapters present with Source OverDrive and the last
  chapter's `EndTime == file.Audio.Duration` when Duration > its start, else 0.
- Precedence: a tag with both a CHAP frame and the MediaMarkers TXXX → chapters are the CHAP ones, `Source == ChapterSourceID3CHAP`.
- Extend `TestParseChapterFrames` to assert `Source == ChapterSourceID3CHAP` on every chapter.

Other parsers:
- `internal/m4a/chapters_test.go`: in `TestParseChapters_Success` assert every chapter's `Source == ChapterSourceNero`.
  (QuickTime text-track chapters have no existing fixture; add a direct `buildChaptersFromText` unit
  test with one sample if feasible, asserting `ChapterSourceQuickTime`; if building the inputs needs
  a real file, note it in your report instead.)
- `internal/flac`: a direct `cuesheetToChapters` test (construct a `*CueSheet` with two tracks) asserting `Source == ChapterSourceCueSheet`.
- `internal/vorbis/chapters_test.go` (create): `ParseChapters([]string{"CHAPTER001=00:00:00.000","CHAPTER001NAME=Intro","CHAPTER002=00:05:00.000","CHAPTER002NAME=Body"}, 10*time.Minute)` → 2 chapters with `Source == ChapterSourceVorbisComment`. (Check the exact comment-key format `ParseChapters` expects by reading `internal/vorbis/chapters.go:29-100` first.)

Verification: `go test -race ./...` → all `ok`, ≥ 25 new test cases.

## Done criteria

- [ ] `go build ./...`, `go vet ./...`, `go test -race ./...` exit 0
- [ ] `mise exec -- golangci-lint run ./...` → `0 issues.`; `gofmt -l .` empty
- [ ] `grep -rn -A6 'types.Chapter{' --include='*.go' internal | grep -v _test | grep -c 'Source:'` → equals the number of non-test `types.Chapter{` sites (5 existing + 1 in `mediamarkers.go` = 6)
- [ ] `grep -n 'ChapterSource' chapter.go` → alias and 6 constants re-exported
- [ ] Every parser's chapter tests assert `Source`
- [ ] No `float` in `internal/mp3/mediamarkers.go` (`grep -n 'float' internal/mp3/mediamarkers.go` → no output)
- [ ] `go.mod` unchanged (encoding/xml is stdlib)
- [ ] `plans/README.md` row for 036 updated

## STOP conditions

- A real-world sample (from plan 001's corpus or anywhere you look) uses a `Time` format not covered
  by Step 3's grammar — do not widen the grammar by guessing; report the exact string.
- After plan 026, TXXX frames no longer reach a description-keyed handler, or CHAP chapters are no
  longer assigned after all frames are read (the precedence trick in Step 4 would break).
- Adding `Source` breaks an existing test that compares `Chapter` values wholesale (none exist at `aca9750`).
- golangci-lint's `gocyclo`/`nestif` flags `parseMarkerTime` and it can't be simplified by splitting helpers.

## Maintenance notes

- **Found while planning (not fixed here)**: `internal/vorbis/chapters.go:103-108` allocates
  `make([]types.Chapter, len(chapterList))` and `continue`s on an invalid timestamp, leaving a
  zero-value `Chapter{}` (empty title, zero times, empty `Source`) in the result. Worth a small
  bug plan or folding into plan 016's chapter consolidation.
- Deferred: exposing losing candidates (`File.ChapterCandidates map[ChapterSource][]Chapter`) so
  consumers can override M4A precedence. Revisit if ListenUp (which prefers Nero) asks for it.
- If plan 016 introduces a shared `FillChapterEndTimes`, Step 5's special-case can become a call
  to it — keep the "only when EndTime is 0" guard so CHAP's explicit end times are not overwritten.
- If plan 021's CLI exists, add `source` to its chapter DTO (`omitempty`, no schema bump).
- Reviewer: check the time grammar against the tests, the all-or-nothing error behavior, and that
  CHAP still wins.
