# Plan 027: Decode ID3 text correctly (Latin-1, terminators, multi-values, COMM descriptions)

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving on. If a
> STOP condition occurs, stop and report — do not improvise. When done, update
> the status row for this plan in `plans/README.md`.
>
> **Drift check (run first)**: `git diff --stat aca9750..HEAD -- internal/mp3/id3v2.go internal/mp3/artwork.go`
> Plan 026 is a prerequisite and WILL show up in this diff (it edits `processFrame`,
> `parseTextFrame`'s MVIN branch and the CHAP functions). Compare only the functions
> excerpted below against the live code; on a mismatch in those, STOP.

## Status

- **Priority**: P2
- **Effort**: S
- **Risk**: LOW (changes string output only for non-ASCII Latin-1, trailing NULs, NUL-separated values and multi-COMM files)
- **Depends on**: plans/026-id3-audiobook-frames.md (shared test helpers in `internal/mp3/id3_fixture_test.go`; both edit `processFrame`)
- **Category**: bug
- **Planned at**: commit `aca9750`, 2026-10-30

## Why this matters

Four text-decoding defects corrupt MP3 metadata that users see and that the audiobook server
matches on. (1) ISO-8859-1 frames are returned as raw bytes, so `é`/`ü`/`ñ` become invalid UTF-8
(`"Caf\xe9"`). (2) Trailing NUL terminators are kept (`"Caf\xe9\x00"`), giving invisible
characters in titles that break equality and dedup. (3) ID3v2.4 multi-value frames
(`"Fantasy\x00Science Fiction"`) come through as one string with an embedded NUL. (4) Every COMM
frame overwrites `Tags.Comment`, and iTunes writes several COMM frames with descriptions
`iTunNORM`/`iTunSMPB`/`iTunPGAP` holding hex blobs — so the real comment is replaced by
`" 00000A2B 00000B3C ..."`. All four were reproduced at `aca9750` through a full `Parse`
(TALB `"Caf\xe9\x00"` → `"Caf\xe9\x00"`; COMM `""`→`"real"` then COMM `iTunNORM` → `" 0000"`).

## Current state

- `internal/mp3/id3v2.go` — ID3v2 frame parsing and all text decoders.
- `internal/mp3/artwork.go:151` — `parseAPICFrame` also calls `decodeText` for the picture
  description (benefits automatically; no edit).

`internal/mp3/id3v2.go:479-505` — `decodeText`:

```go
func decodeText(data []byte, encoding byte) string {
	if len(data) == 0 {
		return ""
	}

	switch encoding {
	case 0: // ISO-8859-1
		return string(data)

	case 1: // UTF-16 with BOM
		return decodeUTF16(data)

	case 2: // UTF-16BE (ID3v2.4)
		return decodeUTF16BE(data)

	case 3: // UTF-8 (ID3v2.4)
		if utf8.Valid(data) {
			return string(data)
		}
		return string(data) // Return as-is even if invalid

	default:
		// Unknown encoding - try as ISO-8859-1
		return string(data)
	}
}
```

`internal/mp3/id3v2.go:507-552` — `decodeUTF16` (BOM `FF FE` → LE, `FE FF` → BE, no BOM → BE,
`len < 2` → `""`), `decodeUTF16LE`, `decodeUTF16BE` (both drop a trailing odd byte, then
`utf16.Decode`). These three functions have **no tests** today.

`internal/mp3/id3v2.go:236-245` — `parseTextFrame` head (after plan 026 the switch also has an
updated MVIN branch; the head is unchanged):

```go
func parseTextFrame(frame ID3v2Frame, file *types.File) { //nolint:gocyclo // ...
	if len(frame.Data) < 1 {
		return
	}

	encoding := frame.Data[0]
	text := decodeText(frame.Data[1:], encoding)

	switch frame.ID {
	case "TIT2": // Title
		file.Tags.Title = text
	...
	case "TCON": // Genre
		if text != "" {
			file.Tags.Genres = append(file.Tags.Genres, text)
		}
	...
	case "TCOM": // Composer (often used for Narrator)
		if text != "" {
			file.Tags.Composers = append(file.Tags.Composers, text)
		}
```

`internal/mp3/id3v2.go:329-349` — `parseTXXXFrame`: `value := decodeText(data[nullIdx+terminatorSize(encoding):], encoding)`
then dispatches through `txxxFieldHandlers` (keyed by lower-cased description).

`internal/mp3/id3v2.go:351-372` — `parseCommentFrame` ignores the short description and always
overwrites:

```go
// Format: [encoding][language(3)][short description\0][text].
func parseCommentFrame(frame ID3v2Frame, file *types.File) {
	if len(frame.Data) < 4 {
		return
	}

	encoding := frame.Data[0]
	// Skip language (3 bytes)
	data := frame.Data[4:]

	// Find null terminator separating short description from text
	nullIdx := findNullTerminator(data, encoding)
	if nullIdx < 0 {
		// No null terminator - treat all as comment
		file.Tags.Comment = decodeText(data, encoding)
		return
	}

	// Extract comment text (after short description)
	comment := decodeText(data[nullIdx+terminatorSize(encoding):], encoding)
	file.Tags.Comment = comment
}
```

`processFrame` (`internal/mp3/id3v2.go:211-223` at `aca9750`; plan 026 replaces its first case
with `isTextFrameID(frame.ID)`) takes `chapters *[]ID3v2Frame` and calls
`parseCommentFrame(frame, file)`. `parseID3v2Frames` (:143-163) creates `chapters` and returns it;
`parseID3v2` (:38-57) consumes it.

**Decisions this plan makes (so the executor doesn't have to):**
- **Latin-1 vs. mislabelled UTF-8**: many taggers write UTF-8 bytes into frames labelled
  ISO-8859-1. Today those display correctly by accident (raw passthrough). A blind Latin-1 decode
  would turn them into `"CafÃ©"`. Rule: for encodings 0, 3 and unknown — if the bytes are valid
  UTF-8, use them as-is (pure ASCII is both); otherwise decode as ISO-8859-1 (byte value = code
  point). A genuine Latin-1 string is almost never valid UTF-8, so this is safe both ways and the
  output is always valid UTF-8.
- **Terminators**: trimmed once, centrally, in `decodeText` (`strings.TrimRight(s, "\x00")`). A
  UTF-16 terminator decodes to U+0000, so one trim covers every encoding.
- **Multi-values**: plan 005 (raw-tag parity) does **not** split NUL-separated values — it only
  records raw frames. So this plan splits: single-value fields take the first value; `TCON` and
  `TCOM` append every value. If plan 005 has landed, its `AddRaw` call in `parseTextFrame` should
  record each value (adapt it to loop over `values`).
- **COMM selection**: ignore any COMM whose short description starts with `"iTun"` (iTunNORM,
  iTunSMPB, iTunPGAP, iTunes_CDDB_*). The first COMM with an **empty** description wins; if there
  is none, the first COMM with any other description is used.

**Conventions**: gofmt; Go directive `go 1.26.0` (no newer APIs). Tests that exercise frame
handling go through the full parser using the helpers plan 026 created in
`internal/mp3/id3_fixture_test.go`: `id3Tag(version, frames...)`, `id3Frame(version, id, payload)`,
`latin1Text(s)`, `utf16Text(s)`, `mpegFrame`, `parseBytes(t, data)`. Pure decoder functions
(`decodeText`, `decodeUTF16*`) may be unit-tested directly.

## Commands you will need

| Purpose | Command | Expected |
|---|---|---|
| Build | `go build ./...` | exit 0 |
| Vet | `go vet ./...` | exit 0 |
| Targeted tests | `go test ./internal/mp3/ -run 'TestDecode|TestParse_Text|TestParse_Comment' -v` | all PASS |
| All tests | `go test ./...` and `go test -race ./...` | all pass |
| Lint | `mise exec -- golangci-lint run ./...` | `0 issues.` (must be via `mise exec --`) |
| Format | `gofmt -l .` | no output |

## Scope

**In scope**:
- `internal/mp3/id3v2.go` — `decodeText`, new `decodeSingleByteText`, new `splitTextValues`,
  `parseTextFrame`, `parseTXXXFrame`, `parseCommentFrame`, `processFrame`, `parseID3v2Frames`,
  `parseID3v2`
- `internal/mp3/id3_text_test.go` (create)

**Out of scope**:
- `decodeUTF16*` logic itself — only add tests. If a test reveals a real bug beyond what's listed
  here, STOP and report rather than widening scope.
- TCON numeric genre references like `"(17)"` / `"17"` → `"Rock"`: deferred (needs the ID3v1
  genre table; pairs naturally with plan 037's ID3v1 support). Leave values verbatim.
- Unsynchronisation / frame flags — plan 019. Raw-tag map — plan 005.
- `internal/mp3/artwork.go` — no edit (inherits the `decodeText` fix).

## Git workflow

- Branch: `advisor/027-id3-text-decoding`
- Commit per step; gitmoji single-line, no body, no attribution
  (e.g. `🐛 Decode ISO-8859-1 ID3 text and trim terminators`, `🐛 Prefer the real COMM over iTunes blobs`).
- Do NOT push or open a PR.

## Steps

### Step 1: Fix `decodeText`

Replace `decodeText` with:

```go
// decodeText decodes ID3v2 text for the given encoding byte and trims NUL
// terminators. Encodings: 0 ISO-8859-1, 1 UTF-16 with BOM, 2 UTF-16BE, 3 UTF-8.
func decodeText(data []byte, encoding byte) string {
	var s string
	switch encoding {
	case 1:
		s = decodeUTF16(data)
	case 2:
		s = decodeUTF16BE(data)
	default: // 0, 3 and unknown encodings
		s = decodeSingleByteText(data)
	}
	return strings.TrimRight(s, "\x00")
}

// decodeSingleByteText decodes ISO-8859-1 or UTF-8 text. Valid UTF-8 is used
// as-is (ASCII is both, and many taggers write UTF-8 into frames labelled
// ISO-8859-1); anything else is decoded as ISO-8859-1, where each byte is the
// code point of the same value. The result is always valid UTF-8.
func decodeSingleByteText(data []byte) string {
	if utf8.Valid(data) {
		return string(data)
	}
	var b strings.Builder
	b.Grow(len(data) * 2)
	for _, c := range data {
		b.WriteRune(rune(c))
	}
	return b.String()
}
```

(`decodeUTF16` already returns `""` for `len < 2`, and an empty slice yields `""` from both other
paths, so the old `len(data) == 0` early return is not needed.)

**Verify**: `go build ./...` → exit 0; `go test ./internal/mp3/` → existing tests pass.

### Step 2: Split multi-value text frames

Add:

```go
// splitTextValues splits decoded text-frame content into its values. ID3v2.4
// separates multiple values with NUL; UTF-16 values may each carry their own
// BOM, which decodes to U+FEFF and is dropped. Empty values are removed.
func splitTextValues(s string) []string {
	var out []string
	for _, p := range strings.Split(s, "\x00") {
		if p = strings.TrimPrefix(p, "﻿"); p != "" {
			out = append(out, p)
		}
	}
	return out
}
```

In `parseTextFrame`, replace `text := decodeText(...)` with:

```go
	values := splitTextValues(decodeText(frame.Data[1:], encoding))
	var text string
	if len(values) > 0 {
		text = values[0]
	}
```

Change the `TCON` and `TCOM` branches to append every value
(`file.Tags.Genres = append(file.Tags.Genres, values...)`, same for `Composers`); the `if text != ""`
guards become unnecessary because `values` has no empties. All other branches keep using `text`.

In `parseTXXXFrame`, take the first value of the value part:
`value := firstValue(decodeText(data[nullIdx+terminatorSize(encoding):], encoding))` — add a tiny
`firstValue(s string) string` helper that returns `splitTextValues(s)[0]` or `""` (bounds-checked),
and reuse it in Step 3.

**Verify**: `go build ./...` → exit 0; `go test ./internal/mp3/` → pass.

### Step 3: Choose the right COMM frame

1. Introduce a per-tag state struct next to `processFrame` and thread it through instead of the
   bare chapters slice:

```go
// frameState accumulates results that are resolved after the whole tag is walked.
type frameState struct {
	chapters []ID3v2Frame
	// describedComment is the first COMM text with a non-empty, non-iTunes
	// short description; used only when no COMM has an empty description.
	describedComment string
}
```

   - `processFrame(frame ID3v2Frame, file *types.File, st *frameState)`; the CHAP case becomes
     `st.chapters = append(st.chapters, frame)`; the COMM case calls
     `parseCommentFrame(frame, file, st)`.
   - `parseID3v2Frames` creates `st := &frameState{}`, passes it, and returns `st` (`*frameState`).
   - `parseID3v2` uses `st.chapters` where it used `chapters`, and right after the frame walk:
     `if file.Tags.Comment == "" { file.Tags.Comment = st.describedComment }`.

2. Rewrite `parseCommentFrame`:

```go
// parseCommentFrame handles COMM: [encoding][language(3)][short description\0][text].
// iTunes stores internal data (iTunNORM, iTunSMPB, iTunPGAP, iTunes_CDDB_*) in
// described COMM frames; those are ignored. The first COMM with an empty
// description is the user comment; otherwise the first described one is kept
// as a fallback.
func parseCommentFrame(frame ID3v2Frame, file *types.File, st *frameState) {
	if len(frame.Data) < 4 {
		return
	}
	encoding := frame.Data[0]
	data := frame.Data[4:] // skip encoding + language

	var desc, text string
	if nullIdx := findNullTerminator(data, encoding); nullIdx < 0 {
		text = firstValue(decodeText(data, encoding)) // malformed: no description
	} else {
		desc = decodeText(data[:nullIdx], encoding)
		text = firstValue(decodeText(data[nullIdx+terminatorSize(encoding):], encoding))
	}

	switch {
	case text == "", strings.HasPrefix(desc, "iTun"):
		return
	case desc == "":
		if file.Tags.Comment == "" {
			file.Tags.Comment = text
		}
	case st.describedComment == "":
		st.describedComment = text
	}
}
```

**Verify**: `go build ./...` → exit 0; `go test ./internal/mp3/` → pass;
`grep -n 'chapters \*\[\]ID3v2Frame' internal/mp3/id3v2.go` → no match.

### Step 4: Tests

Create `internal/mp3/id3_text_test.go`:

1. `TestDecodeText` (table, direct call):
   - enc 0 `"Caf\xe9"` → `"Café"`; enc 0 `"Café"` as UTF-8 bytes → `"Café"`;
   - enc 3 `"\xe9t\xe9"` (invalid UTF-8) → `"été"`; enc 3 `"Café\x00"` → `"Café"`;
   - enc 0 `"Title\x00"` → `"Title"`; enc 0 `"Title\x00\x00"` → `"Title"`;
   - enc 7 (unknown) `"\xfc"` → `"ü"`; enc 0 empty → `""`;
   - enc 1 `FF FE 41 00 00 00` → `"A"` (UTF-16 terminator trimmed);
   - assert `utf8.ValidString(result)` for every row.
2. `TestDecodeUTF16` (table, direct calls to `decodeUTF16`, `decodeUTF16LE`, `decodeUTF16BE`):
   LE BOM `"Hé"`; BE BOM `"Hé"`; no BOM → treated as BE; `len < 2` → `""`; odd length (trailing
   byte dropped); surrogate pair U+1F600 in LE and BE; BOM only → `""`.
3. `TestParse_TextDecoding` (full Parse via `parseBytes`), v2.3 and v2.4:
   - `TALB` payload `0x00 "Caf\xe9\x00"` → `Album == "Café"`.
   - `TIT2` `utf16Text("Book") + 00 00` → `Title == "Book"` (no NUL).
   - v2.4 `TCON` payload `0x03 "Fantasy\x00Science Fiction"` → `Genres == ["Fantasy","Science Fiction"]`.
   - v2.4 `TPE1` payload `0x03 "A\x00B"` → `Artist == "A"`.
   - `TXXX` payload `0x00 "NARRATOR\x00Jane Doe\x00"` → `Narrator == "Jane Doe"`.
4. `TestParse_CommentSelection` (full Parse). Build COMM payloads as
   `0x00 + "eng" + desc + 0x00 + text`:
   - [`""`→`"real"`, `iTunNORM`→`" 0000"`] → `"real"`; same in reverse order → `"real"`.
   - only [`iTunSMPB`→`" 00000000"`] → `""`.
   - only [`Description`→`"x"`] → `"x"`.
   - [`Description`→`"x"`, `""`→`"y"`] → `"y"`.
   - [`""`→`"first"`, `""`→`"second"`] → `"first"`.

**Verify**: `go test ./internal/mp3/ -run 'TestDecode|TestParse_Text|TestParse_Comment' -v` → all
PASS; `go test -race ./...` → all pass.

## Test plan

Listed in Step 4: decoder tables (incl. the previously-0%-covered UTF-16 decoders), full-Parse text
decoding (Latin-1, terminators, multi-values, TXXX), full-Parse COMM selection. Pattern: helpers in
`internal/mp3/id3_fixture_test.go` (from plan 026); frame-level behaviour must be asserted through
`parseBytes`, never by calling `parseTextFrame`/`parseCommentFrame` directly.

## Done criteria

- [ ] `go build ./...`, `go vet ./...` exit 0; `gofmt -l .` prints nothing
- [ ] `go test -race ./...` exits 0 including the four new tests
- [ ] `mise exec -- golangci-lint run ./...` → `0 issues.`
- [ ] `grep -n 'return string(data)' internal/mp3/id3v2.go` → exactly one match (inside `decodeSingleByteText`)
- [ ] `go test ./internal/mp3/ -cover` shows `decodeUTF16`, `decodeUTF16LE`, `decodeUTF16BE` covered (check with `go test ./internal/mp3/ -coverprofile=/tmp/c.out && go tool cover -func=/tmp/c.out | grep -i utf16` → none at 0.0%)
- [ ] `git status` shows only in-scope files changed
- [ ] `plans/README.md` status row for 027 updated (add the row if absent)

## STOP conditions

- Plan 026 has not landed (`internal/mp3/id3_fixture_test.go` missing or `isTextFrameID` absent) —
  STOP; execute 026 first.
- `processFrame`/`parseID3v2Frames` were restructured by another plan (e.g. 016 or 019) so the
  `frameState` threading no longer fits — STOP and report.
- An existing test asserts raw Latin-1 bytes, a trailing NUL, or a NUL-joined multi-value string —
  STOP and report (it was pinning the bug; confirm before changing it).
- `golangci-lint` reports `gocyclo` on `parseTextFrame` despite its existing `//nolint:gocyclo` — STOP
  rather than adding new suppressions elsewhere.

## Maintenance notes

- Multi-value UTF-16 with mixed BOM endianness within one frame is not handled (the second value
  would decode byte-swapped). No known writer does this; revisit only with a real file.
- Plan 005 (raw tags), when executed after this one, should record every element of `values` in
  `parseTextFrame` and both description and text for COMM.
- Deferred: TCON `"(17)"`-style numeric genre resolution — do it together with plan 037's ID3v1
  genre byte using one shared genre table.
- Reviewer: confirm the UTF-8-first heuristic is applied for encodings 0/3/unknown only — never for
  UTF-16 — and that `Comment` is set from `describedComment` only when empty.
