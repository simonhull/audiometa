# Plan 026: Route GRP1/MVNM/MVIN frames and read CHAP titles correctly

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving on. If a
> STOP condition occurs, stop and report — do not improvise. When done, update
> the status row for this plan in `plans/README.md`.
>
> **Drift check (run first)**: `git diff --stat aca9750..HEAD -- internal/mp3/id3v2.go internal/mp3/parser_test.go`
> If either file changed, compare the "Current state" excerpts against the live code; on a
> mismatch, STOP. (Exception: if plan 007 landed, `readSingleFrame` will contain an
> `isMetadataFrame` skip — that is expected; see Step 1.)

## Status

- **Priority**: P1
- **Effort**: S
- **Risk**: LOW (adds frames that are currently dropped; CHAP change only affects which bytes become the title)
- **Depends on**: none
- **Category**: bug
- **Planned at**: commit `aca9750`, 2026-10-30

## Why this matters

The primary consumer is an audiobook server, and for MP3 audiobooks the series name and position
come from the iTunes movement frames `MVNM`/`MVIN` and the grouping frame `GRP1`. `parseTextFrame`
has `case` branches for all three — but `processFrame` only calls `parseTextFrame` for IDs that
start with `"T"`, so those branches are **unreachable**. Every MP3's series name, series position
and grouping are silently dropped, and the `Grouping → Series` fallback (`ParseGrouping`) never
sees GRP1. Separately, CHAP chapter titles are read by decoding the TIT2 subframe size as synchsafe
even in ID3v2.3 tags (where sizes are plain big-endian), and only the *first* subframe is checked:
v2.3 audiobooks with titles ≥ 128 bytes get **truncated** titles, and chapters whose first subframe
is not TIT2 (e.g. TIT3 first) fall back to the element ID (`"chp0"`). Both were reproduced at
`aca9750` with a full-Parse fixture: MVNM/MVIN/GRP1 → all empty; a 112-char UTF-16 v2.3 chapter
title → 48 chars.

## Current state

- `internal/mp3/id3v2.go` — the whole ID3v2 reader. Relevant parts:

`internal/mp3/id3v2.go:211-223` — the dispatch (the bug):

```go
// processFrame processes a single frame based on its ID.
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

`internal/mp3/id3v2.go:276-289` — branches inside `parseTextFrame` that can never run today:

```go
	case "GRP1": // Grouping (often contains series info for audiobooks)
		file.Tags.Grouping = text
	case "TIT1": // Content group description (alternative grouping)
		if file.Tags.Grouping == "" {
			file.Tags.Grouping = text
		}
	case "MVNM": // Movement name (series name in ID3v2.4)
		if file.Tags.Series == "" {
			file.Tags.Series = text
		}
	case "MVIN": // Movement number (series position in ID3v2.4)
		if file.Tags.SeriesPart == "" {
			file.Tags.SeriesPart = text
		}
```

Note: `MVIN` is defined (like `TRCK`) as `"number/count"`, e.g. `"3/7"`. Today's branch would
store `"3/7"` as `SeriesPart`; the fix below stores only the number.

`internal/mp3/id3v2.go:59-80` — `resolveSeriesFallbacks` (runs after all frames): if `Series == ""`
and `Grouping != ""` it calls `parsing.ParseGrouping` (`"Series Name #5"` → `("Series Name","5")`).
No change needed — it starts working once GRP1 reaches `parseTextFrame`.

`internal/mp3/id3v2.go:44-50` — chapters are collected then parsed; `header` is in scope here:

```go
	frameDataOffset := skipExtendedHeader(sr, header)
	chapters := parseID3v2Frames(sr, file, header, frameDataOffset)

	// Process chapters
	if len(chapters) > 0 {
		file.Chapters = parseChapterFrames(chapters, file.Audio.Duration)
	}
```

`internal/mp3/id3v2.go:203-209` — the version-aware size decoder to reuse:

```go
func decodeFrameSize(version byte, sizeBytes []byte) uint32 {
	if version == 4 {
		return decodeSynchsafe(sizeBytes)
	}
	return binary.BigEndian.Uint32(sizeBytes)
}
```

`internal/mp3/id3v2.go:374-417` — `parseChapterFrames(frames []ID3v2Frame, _ time.Duration)`. Its
doc comment claims the layout starts with `[encoding]` — wrong; CHAP has no encoding byte (the code
itself is right, see the inline comment at :394). Upfront length check at :389-392:

```go
	for _, frame := range frames {
		if len(frame.Data) < 20 {
			continue
		}
```

then it finds the NUL ending the element ID (:399-405), checks `len(data) < 16` after it (:407), and
calls `extractChapterTitleFromSubframes(data[16:], elementID)` (:417).

CHAP minimum size arithmetic: element ID (≥1 byte) + NUL (1) + 4×uint32 (16) = **17 bytes**. A
2-char ID `"c1\0"` gives 19 bytes and is wrongly rejected by `< 20`. The later NUL and `< 16` checks
already guarantee safety, so the `< 20` guard is redundant and too strict.

`internal/mp3/id3v2.go:447-477` — the title extractor (the second bug):

```go
func extractChapterTitleFromSubframes(subframeData []byte, fallbackTitle string) string {
	if len(subframeData) < 10 {
		return fallbackTitle
	}

	// Try to parse TIT2 subframe
	subframeID := string(subframeData[0:4])
	if subframeID != "TIT2" {
		return fallbackTitle
	}

	// Try synchsafe size (CHAP subframes may use synchsafe)
	subframeSize := decodeSynchsafe(subframeData[4:8])
	if len(subframeData) < int(10+subframeSize) {
		return fallbackTitle
	}

	titleData := subframeData[10 : 10+subframeSize]
	...
	titleEncoding := titleData[0]
	title := decodeText(titleData[1:], titleEncoding)
	if title == "" {
		return fallbackTitle
	}
	return title
}
```

Per the ID3v2 chapter spec, CHAP subframes use the same frame header format as the enclosing tag
version: plain big-endian sizes in v2.3, synchsafe in v2.4. For sizes < 128 both decodings agree,
which is why short titles work.

**Tests today** — `internal/mp3/parser_test.go`: `TestParseTextFrame` (:199) calls
`parseTextFrame` directly, which is exactly how this bug hid (the direct call bypasses the
dispatch). `TestParseChapterFrames` (:275) calls `parseChapterFrames(frames, 20*time.Second)` —
its call must be updated when the signature changes. `createMinimalMP3WithID3` (:99) shows the
byte layout of a tag + a minimal MPEG frame. New tests in this plan MUST go through the full
`(&parser{}).Parse(...)` path.

**Conventions**: gofmt; warnings are `file.Warnings = append(file.Warnings, types.Warning{Stage:
types.StageMetadata, Message: ..., Err: ...})` (none needed here). In `internal/mp3`, the repo's
`internal/binary` package is imported as `binutil` because `encoding/binary` is also imported.
Go module directive is `go 1.26.0` — do not use APIs newer than Go 1.26.

## Commands you will need

| Purpose | Command | Expected |
|---|---|---|
| Build | `go build ./...` | exit 0 |
| Vet | `go vet ./...` | exit 0 |
| Targeted tests | `go test ./internal/mp3/ -run 'TestParse_|TestParseChapter' -v` | all PASS |
| All tests | `go test ./...` and `go test -race ./...` | all pass |
| Lint | `mise exec -- golangci-lint run ./...` | `0 issues.` (MUST use `mise exec --`; a stale global golangci-lint on PATH gives false typecheck errors) |
| Format | `gofmt -l .` | no output |

## Scope

**In scope** (the only files you should modify):
- `internal/mp3/id3v2.go` — `processFrame`, `parseTextFrame` (MVIN branch only),
  `parseChapterFrames`, `extractChapterTitleFromSubframes`, the call at :49
- `internal/mp3/parser_test.go` — update the `parseChapterFrames` call
- `internal/mp3/id3_fixture_test.go` (create) — shared fixture helpers
- `internal/mp3/id3_frames_test.go` (create) — the new tests

**Out of scope** (do NOT touch):
- ID3 unsynchronisation and per-frame flags (compression/DLI) — plan 019 owns them, including for
  CHAP subframes.
- APIC skipping in `readSingleFrame` — plan 007. But see Step 1: if 007 already landed you must
  extend its frame allowlist.
- Raw-tag map / `AddRaw` — plan 005.
- Text decoding (Latin-1, NUL terminators, COMM selection) — plan 027.
- **CTOC** (table-of-contents frame; defines chapter order and nesting). Not handled today and
  deliberately deferred: chapters are sorted by start time, which matches CTOC order for every
  linear audiobook. Record as deferred in your report; do not implement.
- The TXXX handler table (`txxxFieldHandlers`) — unchanged.

## Git workflow

- Branch: `advisor/026-id3-audiobook-frames`
- Commit per step; gitmoji single-line, no body, no attribution
  (e.g. `🐛 Route GRP1/MVNM/MVIN frames to the text parser`, `🐛 Read CHAP titles with version-aware subframe sizes`).
- Do NOT push or open a PR.

## Steps

### Step 1: Fix the frame dispatch

Add a helper next to `processFrame` and use it in the switch:

```go
// isTextFrameID reports whether parseTextFrame handles this frame ID: every
// standard T*** text frame except TXXX, plus the iTunes non-T text frames
// GRP1 (grouping), MVNM (movement name) and MVIN (movement number).
func isTextFrameID(id string) bool {
	switch id {
	case "TXXX":
		return false
	case "GRP1", "MVNM", "MVIN":
		return true
	}
	return strings.HasPrefix(id, "T")
}
```

```go
	switch {
	case isTextFrameID(frame.ID):
		parseTextFrame(frame, file)
	case frame.ID == "TXXX":
	...
```

**If plan 007 has landed** (`grep -n isMetadataFrame internal/mp3/id3v2.go` returns a match), its
`isMetadataFrame` decides which frames get their payload read at all. Change its body to
`return isTextFrameID(id) || id == "TXXX" || id == "COMM" || id == "CHAP"` — otherwise GRP1/MVNM/MVIN
payloads are skipped before dispatch and this fix does nothing.

In `parseTextFrame`, change the `MVIN` branch to keep only the number before any `/`:

```go
	case "MVIN": // Movement number "N" or "N/count" (series position)
		if file.Tags.SeriesPart == "" {
			part, _, _ := strings.Cut(text, "/")
			file.Tags.SeriesPart = strings.TrimSpace(part)
		}
```

Leave the GRP1/TIT1/MVNM branches as they are (GRP1 wins over TIT1; TXXX `SERIES` still wins over
MVNM because its handler assigns unconditionally).

**Verify**: `go build ./...` → exit 0; `go test ./internal/mp3/` → existing tests pass.

### Step 2: Create shared fixture helpers

Create `internal/mp3/id3_fixture_test.go` (package `mp3`). Plans 027 and 037 reuse these — keep
the names exactly:

```go
// synchsafe encodes n as a 4-byte ID3v2 synchsafe integer.
func synchsafe(n int) []byte {
	return []byte{byte(n >> 21 & 0x7F), byte(n >> 14 & 0x7F), byte(n >> 7 & 0x7F), byte(n & 0x7F)}
}

// id3Frame builds one ID3v2.3/2.4 frame: v2.4 sizes are synchsafe, v2.3 plain big-endian.
func id3Frame(version byte, id string, payload []byte) []byte {
	h := make([]byte, 10)
	copy(h, id)
	if version == 4 {
		copy(h[4:8], synchsafe(len(payload)))
	} else {
		binary.BigEndian.PutUint32(h[4:8], uint32(len(payload)))
	}
	return append(h, payload...)
}

// id3Tag wraps frames in an ID3v2 header of the given major version (no flags, no padding).
func id3Tag(version byte, frames ...[]byte) []byte {
	body := bytes.Join(frames, nil)
	h := append([]byte{'I', 'D', '3', version, 0, 0}, synchsafe(len(body))...)
	return append(h, body...)
}

// mpegFrame is a minimal MPEG-1 Layer III frame header (128 kbps, 44.1 kHz) plus filler.
var mpegFrame = []byte{0xFF, 0xFB, 0x90, 0x00, 0, 0, 0, 0}

// latin1Text is a text-frame payload with encoding byte 0.
func latin1Text(s string) []byte { return append([]byte{0x00}, s...) }

// utf16Text is a text-frame payload with encoding byte 1, LE BOM, UTF-16LE units.
func utf16Text(s string) []byte {
	out := []byte{0x01, 0xFF, 0xFE}
	for _, u := range utf16.Encode([]rune(s)) {
		out = append(out, byte(u), byte(u>>8))
	}
	return out
}

// chapFrameBody builds a CHAP payload: elementID\0, start/end ms, 0xFFFFFFFF offsets, subframes.
func chapFrameBody(elementID string, startMS, endMS uint32, subframes ...[]byte) []byte {
	b := append([]byte(elementID), 0)
	b = binary.BigEndian.AppendUint32(b, startMS)
	b = binary.BigEndian.AppendUint32(b, endMS)
	b = append(b, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF)
	return append(b, bytes.Join(subframes, nil)...)
}

// parseBytes runs the full MP3 parser (dispatch included) over data.
func parseBytes(t *testing.T, data []byte) *types.File {
	t.Helper()
	f, err := (&parser{}).Parse(context.Background(), bytes.NewReader(data), int64(len(data)), "test.mp3")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return f
}
```

Usage: `parseBytes(t, append(id3Tag(3, id3Frame(3, "MVNM", latin1Text("Dune"))), mpegFrame...))`.

**Verify**: `go vet ./internal/mp3/` → exit 0.

### Step 3: Tests for the dispatch

Create `internal/mp3/id3_frames_test.go` with a table test `TestParse_ITunesTextFrames`, run for
**both** version 3 and version 4 (loop over `[]byte{3, 4}` with `t.Run`):
- `MVNM "Dune"` + `MVIN "3/6"` → `Tags.Series == "Dune"`, `Tags.SeriesPart == "3"`.
- `MVIN "2"` alone → `SeriesPart == "2"`.
- `GRP1 "The Expanse #4"` (no MVNM, no TXXX series) → `Grouping == "The Expanse #4"`,
  `Series == "The Expanse"`, `SeriesPart == "4"` (via `resolveSeriesFallbacks`).
- `TIT1 "A"` then `GRP1 "B"` → `Grouping == "B"`.
- `TXXX` with description `SERIES`, value `"Real"` (payload `latin1Text("SERIES\x00Real")`) plus
  `MVNM "Other"` → `Series == "Real"` regardless of frame order (test both orders).

**Verify**: `go test ./internal/mp3/ -run TestParse_ITunesTextFrames -v` → all PASS. (For
reference: at `aca9750`, before Step 1, the same fixtures produce empty Series/SeriesPart/Grouping —
the test is a genuine regression test.)

Helper file imports: `bytes`, `context`, `encoding/binary`, `testing`, `unicode/utf16`,
`github.com/simonhull/audiometa/internal/types`.

### Step 4: Read CHAP titles with version-aware sizes across all subframes

1. Change the signature to `parseChapterFrames(frames []ID3v2Frame, version byte) []types.Chapter`
   (drop the unused `time.Duration` parameter), update the call at :49 to
   `parseChapterFrames(chapters, header.Version)` and the call in `TestParseChapterFrames`
   (`parser_test.go:303`) to `parseChapterFrames(frames, 3)`.
2. Remove the `if len(frame.Data) < 20 { continue }` guard (the NUL and `len(data) < 16` checks
   below it already make the slicing safe). Fix the doc comment to
   `[element_id\0][start_time(4)][end_time(4)][start_offset(4)][end_offset(4)][subframes...]`.
3. Rewrite `extractChapterTitleFromSubframes(subframeData []byte, version byte, fallbackTitle string) string`
   to walk subframes:

```go
	for len(subframeData) >= 10 {
		id := string(subframeData[0:4])
		if subframeData[0] == 0 { // padding
			break
		}
		size := uint64(decodeFrameSize(version, subframeData[4:8]))
		if size > uint64(len(subframeData)-10) { // truncated/corrupt: stop, don't guess
			break
		}
		body := subframeData[10 : 10+size]
		if id == "TIT2" && len(body) > 0 {
			if title := decodeText(body[1:], body[0]); title != "" {
				return title
			}
		}
		subframeData = subframeData[10+size:]
	}
	return fallbackTitle
```

The `uint64` comparison avoids an `int` overflow on 32-bit platforms when `size` is huge.

**Verify**: `go build ./...` → exit 0; `go test ./internal/mp3/` → all pass including
`TestParseChapterFrames`.

### Step 5: CHAP regression tests (full Parse)

Add `TestParse_ChapterTitles` to `id3_frames_test.go`; every case builds the tag with `id3Tag` +
`id3Frame(v, "CHAP", chapFrameBody(...))`, appends `mpegFrame`, and calls `parseBytes`:
- v2.3, one chapter whose subframes are `TIT3 "sub"` then `TIT2 "Opening"` → title `"Opening"`.
- v2.3, one chapter with a TIT2 of `utf16Text(s)` where `s` is 200 ASCII chars (payload 403 bytes,
  size `00 00 01 93` — synchsafe would misread it) → title `== s` exactly.
- v2.4, the same 200-char title with synchsafe subframe size → title `== s`.
- v2.3, element ID `"c1"` with no subframes (19-byte CHAP payload) → one chapter, title `"c1"`.
- v2.3, chapter with a TIT2 subframe whose declared size exceeds the remaining bytes → title falls
  back to the element ID, no panic.

**Verify**: `go test ./internal/mp3/ -run TestParse_ChapterTitles -v` → all PASS; then
`go test -race ./...` → all pass.

## Test plan

- `TestParse_ITunesTextFrames` (v2.3 + v2.4): MVNM/MVIN/GRP1 routing, MVIN `/count` stripping,
  GRP1→Series fallback, precedence with TIT1 and TXXX.
- `TestParse_ChapterTitles`: TIT3-before-TIT2, 200-char UTF-16 title in v2.3 and v2.4, 19-byte
  CHAP, truncated subframe.
- Pattern: byte layout of `createMinimalMP3WithID3` (`internal/mp3/parser_test.go:99`), but via the
  new helpers and `parseBytes` — never by calling `parseTextFrame`/`parseChapterFrames` directly in
  new tests.

## Done criteria

- [ ] `go build ./...`, `go vet ./...` exit 0; `gofmt -l .` prints nothing
- [ ] `go test -race ./...` exits 0 including `TestParse_ITunesTextFrames` and `TestParse_ChapterTitles`
- [ ] `mise exec -- golangci-lint run ./...` → `0 issues.`
- [ ] `grep -n 'HasPrefix(frame.ID, "T")' internal/mp3/id3v2.go` → no match (dispatch goes through `isTextFrameID`)
- [ ] `grep -n 'decodeSynchsafe(subframeData' internal/mp3/id3v2.go` → no match
- [ ] If plan 007 landed: `isMetadataFrame` calls `isTextFrameID`
- [ ] `git status` shows only in-scope files modified/created
- [ ] `plans/README.md` status row for 026 updated (add the row if absent)

## STOP conditions

- `processFrame` or `parseTextFrame` no longer match the excerpts (e.g. plan 005 or 016 restructured
  them) — STOP and report; the dispatch fix must be re-targeted.
- Plan 019 has landed and CHAP subframes are now de-unsynchronised/flag-processed somewhere else —
  STOP; reconcile so the subframe walk uses its helpers rather than duplicating.
- An existing test asserts `SeriesPart` contains a `/` (e.g. `"3/7"`) — STOP and report.
- Any existing test's output changes other than the intended fields.

## Maintenance notes

- `isTextFrameID` is now the single source of truth for "text frame". Plan 007's
  `isMetadataFrame` and plan 037's ID3v2.2 ID mapping must route through it.
- Deferred: **CTOC** support (ordering/nesting of chapters, top-level flag). Implement only if a
  real file shows CHAP start times disagreeing with CTOC order.
- Deferred: TXXX `mvin` handler still stores `"N/count"` verbatim; harmonise if a file needs it.
- Reviewer: check the subframe loop terminates on every input (each iteration consumes ≥ 10 bytes
  or breaks) and that the bounds check happens before slicing.
