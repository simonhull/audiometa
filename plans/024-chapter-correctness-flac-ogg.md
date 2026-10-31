# Plan 024: Make FLAC and Ogg chapters correct (cuesheet layout, CHAPTER comments, end times)

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving on. If a
> STOP condition occurs, stop and report — do not improvise. When done, update
> the status row for this plan in `plans/README.md`.
>
> **Drift check (run first)**: `git diff --stat aca9750..HEAD -- internal/flac/cuesheet.go internal/flac/parser.go internal/ogg/parser.go internal/vorbis/chapters.go internal/types/chapter.go internal/flac/ internal/ogg/parser_test.go internal/vorbis/`
> If any file changed, compare the "Current state" excerpts below against the live code; on a
> mismatch, STOP.

## Status

- **Priority**: P1
- **Effort**: M
- **Risk**: LOW (chapters for FLAC are currently almost always empty; Ogg changes only fill a
  zero end time and drop phantom entries)
- **Depends on**: none. **Must land before plan 016** (016 extracts the chapter end-time loops this
  plan fixes; it must consolidate the corrected behavior, not the buggy one). If plan 034 is also
  pending, run this one first — both edit `internal/flac/parser.go`.
- **Category**: bug
- **Planned at**: commit `aca9750`, 2026-10-30

## Why this matters

The primary consumer is an audiobook server, and chapters are its navigation. Today:

1. **FLAC CUESHEET chapters are silently empty.** `parseCueSheet` skips one reserved byte too many,
   so it reads the track count from the first byte of track 1's 64-bit offset — almost always `0`
   — and returns zero chapters with no warning.
2. **FLAC `CHAPTERxxx=` comments are ignored.** The FLAC parser never calls
   `vorbis.ParseChapters`, although the same comments work in Ogg/Opus. Most audiobook taggers
   write chapters this way, with titles.
3. **Non-CD cuesheets produce a bogus final chapter** (lead-out track `255` is not recognised; only
   CD-DA's `170` is), and when no lead-out exists the last chapter's `EndTime` stays 0.
4. **Ogg/Opus last chapter always has `EndTime == 0`**: comments (and chapters) are parsed before the
   duration is computed.
5. **One bad `CHAPTERxxx` timestamp leaves a zero-value phantom chapter** (Index 0, empty title)
   in the output and zeroes the previous chapter's `EndTime`.

After this plan, FLAC and Ogg/Opus return contiguous, correctly-ended chapters, with a documented
precedence when both sources exist.

## Current state

Files:
- `internal/flac/cuesheet.go` — CUESHEET block parser + `cuesheetToChapters` (no tests exist).
- `internal/flac/parser.go` — FLAC metadata-block loop; `parseVorbisComment` (lines 266–316).
- `internal/vorbis/chapters.go` — `ParseChapters` for `CHAPTERxxx` / `CHAPTERxxxNAME` comments
  (shared by Ogg/Opus; no tests exist).
- `internal/ogg/parser.go` — Ogg Vorbis/Opus `Parse`; calls comment parsers and duration.
- `internal/ogg/vorbis.go:151-154` and `internal/ogg/opus.go:165-168` — both end with
  `file.Chapters = vorbis.ParseChapters(allComments, file.Audio.Duration)` (do **not** edit these).
- `internal/types/chapter.go:7-11` — doc comment listing chapter sources.

### FLAC CUESHEET layout bug — `internal/flac/cuesheet.go:39-77` (verified)

```go
func parseCueSheet(sr *binary.SafeReader, offset int64, length uint32, file *types.File) error {
	if length < 396 { // Minimum size: 128+8+1+259+1 = 397 bytes (but some fields can be smaller)
	...
	// Read flags (1 byte)
	flags, err := binary.Read[uint8](sr, offset, "cuesheet flags")
	...
	isCD := (flags & 0x80) != 0
	offset++

	// Skip reserved (259 bytes)
	offset += 259

	// Read track count (1 byte)
	trackCount, err := binary.Read[uint8](sr, offset, "track count")
```

FLAC spec (RFC 9639 §8.7, CUESHEET): media catalog number 128 bytes; lead-in u64 (8); **1 bit
is-CD + 7 bits reserved + 258 bytes reserved** (= the flags byte + 258 bytes); number of tracks u8.
Fixed header = 128 + 8 + 1 + 258 + 1 = **396** bytes. The code skips 259 after the flags byte, so
it is one byte late. The length check (`< 396`) is right; the comment is wrong.

Per track (36 bytes + 12 per index — the code at `cuesheet.go:110-181` is correct): offset u64
(samples from start of audio, pointing at the track's first index point), number u8, ISRC 12 bytes,
flags u8 (bit 7 = non-audio, bit 6 = pre-emphasis), 13 reserved bytes, index count u8; each index:
offset u64 (samples **relative to the track offset**), number u8, 3 reserved bytes.
Spec rules used below: the lead-out track is **always the last track**; its number is **170 for
CD-DA, 255 otherwise**. Index 01 is the track's actual start; index 00 (if present) is the pregap.

### `cuesheetToChapters` — `internal/flac/cuesheet.go:208-276` (verified, abridged)

```go
func cuesheetToChapters(cuesheet *CueSheet, sampleRate int) []types.Chapter {
	...
	for _, track := range cuesheet.Tracks {
		if track.IsAudio && track.Number != 170 {      // 255 lead-out slips through
			audioTracks = append(audioTracks, track)
		}
	}
	...
	for _, track := range cuesheet.Tracks {
		if track.Number == 170 { leadOutOffset = track.Offset; break }
	}
	chapters := make([]types.Chapter, len(audioTracks))
	for i, track := range audioTracks {
		seconds := float64(track.Offset) / float64(sampleRate)   // ignores INDEX 01 offset
		startTime := time.Duration(seconds * float64(time.Second))
		var endTime time.Duration
		if i < len(audioTracks)-1 {
			... next track's Offset ...
		} else if leadOutOffset > 0 {
			... lead-out ...
		}
		// If no lead-out and last track, endTime stays 0 (will be set by duration)   <-- nothing does
		title := fmt.Sprintf("Track %02d", track.Number)
		if track.ISRC != "" { title = fmt.Sprintf("Track %02d (%s)", track.Number, track.ISRC) }
		chapters[i] = types.Chapter{Index: i + 1, Title: title, StartTime: startTime, EndTime: endTime}
	}
	return chapters
}
```

Called at `cuesheet.go:103-104`: `file.Chapters = cuesheetToChapters(cuesheet, file.Audio.SampleRate)`.
STREAMINFO is mandated to be the first metadata block, so `file.Audio.SampleRate` and
`file.Audio.Duration` are already set when CUESHEET is parsed.

### FLAC never parses CHAPTER comments — `internal/flac/parser.go`

Block loop, `parser.go:98-106` and `:121-129` (verified):

```go
case blockTypeVorbisComment:
	if err := parseVorbisComment(sr, offset, blockLength, file); err != nil {
		file.Warnings = append(file.Warnings, types.Warning{ Stage: types.StageMetadata, ... })
	}
...
case blockTypeCueSheet:
	if err := parseCueSheet(sr, offset, uint32(blockLength), file); err != nil {
		file.Warnings = append(file.Warnings, types.Warning{ Stage: types.StageChapters, ... })
	}
```

`parseVorbisComment` (`parser.go:267-316`) loops over comments calling only
`vorbis.ParseComment(comment, file)` (line 305). It never collects the strings and never calls
`vorbis.ParseChapters`. Compare `internal/ogg/vorbis.go:137-154`, which appends each comment to
`allComments` and ends with `file.Chapters = vorbis.ParseChapters(allComments, file.Audio.Duration)`.
The loop ends at `parser.go:135-142` (`offset += blockLength; if isLast { break }`), followed by
`// Set container and codec` at line 144.

### Ogg ordering bug — `internal/ogg/parser.go:102-125` (Vorbis) and `:134-155` (Opus) (verified)

```go
	// Parse Vorbis comment header (packet 1)
	if err := parseVorbisComment(packets[1], file); err != nil { ...warning... }   // :103 → ParseChapters runs here, Duration == 0

	// Calculate duration from last page's granule position
	if file.Audio.SampleRate > 0 {
		duration, err := calculateDuration(sr, size, file.Audio.SampleRate)       // :114
		... file.Audio.Duration = duration
	}
```

The Opus branch is identical in shape: `parseOpusTags(packets[1], file)` at line 135, then
`calculateDuration(sr, size, 48000)` at line 145, then the bitrate estimate at 157–160.
`calculateDuration` only needs `sr`, `size` and the sample rate (set by the identification header
in packet 0), not the comments — so the duration block can move above the comment block.

### Phantom chapters — `internal/vorbis/chapters.go:102-135` (verified)

```go
	chapters := make([]types.Chapter, len(chapterList))
	for i, chap := range chapterList {
		startTime, err := parseChapterTimestamp(chap.timestamp)
		if err != nil {
			// Invalid timestamp - skip this chapter
			continue                                    // leaves chapters[i] as a zero Chapter
		}
		var endTime time.Duration
		if i < len(chapterList)-1 {
			endTime, _ = parseChapterTimestamp(chapterList[i+1].timestamp)   // 0 if next is invalid
		} else if fileDuration > 0 {
			endTime = fileDuration
		}
		title := chap.title
		if title == "" { title = fmt.Sprintf("Chapter %d", chap.number) }
		chapters[i] = types.Chapter{Index: i + 1, Title: title, StartTime: startTime, EndTime: endTime}
	}
	return chapters
```

The function carries `//nolint:gocyclo` (line 29) and `//nolint:nestif` (line 54).

### Conventions

- Warnings: `file.Warnings = append(file.Warnings, types.Warning{Stage: types.StageChapters, Message: ..., Err: err, Offset: offset})`.
- Binary reads: `binary.Read[T](sr, off, "what")` on `*binary.SafeReader` (big-endian).
- Lint config `.golangci.yml`: `gocyclo` min 15, `nestif` min 5, `makezero` (a `make([]T, n)`
  followed by `append` is flagged — use `make([]T, 0, n)`), `nolintlint` (an unused
  `//nolint:<linter>` directive is reported — delete it if your change makes it unnecessary).
- Test fixtures are synthetic bytes: `createMinimalFLAC` in `internal/flac/parser_test.go:15-105`,
  `createMinimalOgg` / `createMinimalOpus` in `internal/ogg/parser_test.go:15` / `:133`.
  `Parse` takes an `io.ReaderAt`, so tests may use `bytes.NewReader(data)` instead of temp files.

## Commands you will need

| Purpose | Command | Expected |
|---|---|---|
| Build | `go build ./...` | exit 0 |
| Vet | `go vet ./...` | exit 0 |
| Targeted tests | `go test ./internal/flac/ ./internal/ogg/ ./internal/vorbis/ -v -run 'Cue\|Chapter'` | all pass |
| All tests | `go test ./...` and `go test -race ./...` | all pass |
| Lint | `mise exec -- golangci-lint run ./...` | `0 issues.` |
| Format | `gofmt -l .` | no output |

Always run golangci-lint via `mise exec --` (pinned 2.15.0); a stale global binary on PATH gives
false typecheck errors. Go toolchain is 1.27.3 via mise but `go.mod` says `go 1.26.0` — use no API
newer than Go 1.26.

## Scope

**In scope** (the only files you should modify/create):
- `internal/flac/cuesheet.go`
- `internal/flac/parser.go` — `parseVorbisComment` signature + the VORBIS_COMMENT case + a
  post-loop chapter-precedence block only
- `internal/vorbis/chapters.go` — `ParseChapters` conversion loop only
- `internal/ogg/parser.go` — reorder duration vs. comment parsing only
- `internal/types/chapter.go` — doc comment only
- `internal/flac/fixtures_test.go` (create if absent — shared FLAC fixture builders)
- `internal/flac/chapters_test.go` (create)
- `internal/vorbis/chapters_test.go` (create)
- `internal/ogg/parser_test.go` (extend fixture builders + add tests)

**Out of scope** (do NOT touch):
- `internal/ogg/vorbis.go`, `internal/ogg/opus.go` — they keep calling `ParseChapters`; the fix is
  ordering in `parser.go`.
- `ExtractArtwork`, `parseStreamInfo`, bitrate, picture-type mapping in `internal/flac/parser.go`
  — that is plan 034.
- `internal/vorbis/comments.go` (case-insensitive keys) — plan 028.
- Consolidating the chapter end-time loops across formats into a shared helper — plan 016. Do not
  extract a cross-package helper here.
- Allocation caps on comment/track counts — plan 002. (Do **not** preallocate with the untrusted
  comment count.)
- Ogg packet lacing / granule scanning — plan 019.
- M4A and MP3 chapters.

## Git workflow

- Branch: `advisor/024-chapter-correctness`
- Commit per step, gitmoji single line, no body (e.g. `🐛 Fix FLAC CUESHEET reserved-byte skip`,
  `✨ Parse CHAPTER comments in FLAC`, `🐛 Set Ogg last chapter end time`).
- Do NOT push or open a PR unless instructed.

## Steps

### Step 1: Fix the CUESHEET header layout

In `internal/flac/cuesheet.go`:
- Change `offset += 259` to `offset += 258` and its comment to
  `// Skip reserved (7 bits after the is-CD flag were in the flags byte; 258 more bytes)`.
- Fix the line-40 comment to `// Minimum size: 128+8+1+258+1 = 396 bytes`. Keep the `< 396` check.

**Verify**: `go build ./...` → exit 0; `go test ./internal/flac/` → pass.

### Step 2: Fix `cuesheetToChapters` (lead-out, INDEX 01, last end time, exact math)

Change the signature to `cuesheetToChapters(cuesheet *CueSheet, sampleRate int, fileDuration time.Duration) []types.Chapter`
and the call at line 104 to `cuesheetToChapters(cuesheet, file.Audio.SampleRate, file.Audio.Duration)`.

Add constants and small helpers (keep `cuesheetToChapters` under gocyclo 15):

```go
// Lead-out track numbers (RFC 9639 §8.7): 170 for CD-DA cuesheets, 255 otherwise.
const (
	leadOutTrackCD    = 170
	leadOutTrackNonCD = 255
)

func isLeadOut(number byte) bool { return number == leadOutTrackCD || number == leadOutTrackNonCD }

// trackStartSample returns the sample where the track's audible content starts: the track
// offset plus the INDEX 01 offset (INDEX 00, if present, is the pregap). Falls back to the
// track offset when there is no INDEX 01.
func trackStartSample(t CueTrack) uint64 {
	for _, idx := range t.Indices {
		if idx.Number == 1 {
			return t.Offset + idx.Offset
		}
	}
	return t.Offset
}

// samplesToDuration converts a sample count to a Duration without float rounding.
// sampleRate is at most 2^20-1 (20-bit STREAMINFO field), so rem*1e9 cannot overflow uint64.
func samplesToDuration(samples uint64, sampleRate int) time.Duration {
	rate := uint64(sampleRate)
	return time.Duration(samples/rate)*time.Second + time.Duration(samples%rate*uint64(time.Second)/rate)
}
```

Rewrite the body:
1. Keep the existing early returns (`len(cuesheet.Tracks) == 0`, `sampleRate <= 0`).
2. Lead-out: if the **last** track satisfies `isLeadOut(number)`, remember it as the lead-out.
3. Chapter tracks: every track with `track.IsAudio && !isLeadOut(track.Number)` (preserve order).
   Return `nil` if none.
4. Build `chapters := make([]types.Chapter, 0, len(audioTracks))`; for each track,
   `StartTime: samplesToDuration(trackStartSample(track), sampleRate)`, same title logic as today,
   `Index: len(chapters)+1`.
5. End times in a second pass: `chapters[i].EndTime = chapters[i+1].StartTime` for all but the last;
   last chapter: `samplesToDuration(leadOut.Offset, sampleRate)` if a lead-out was found, else
   `fileDuration` (which may be 0 — then it stays 0).
6. Delete the stale comment `// If no lead-out and last track, endTime stays 0 (will be set by duration)`.

**Verify**: `go build ./...` → exit 0; `go test ./internal/flac/` → pass.

### Step 3: Drop phantom chapters in `vorbis.ParseChapters`

In `internal/vorbis/chapters.go` replace the conversion loop (lines 102–135) with a filter-then-fill
shape:

```go
	chapters := make([]types.Chapter, 0, len(chapterList))
	for _, chap := range chapterList {
		startTime, err := parseChapterTimestamp(chap.timestamp)
		if err != nil {
			continue // Invalid timestamp: drop the chapter entirely
		}
		title := chap.title
		if title == "" {
			title = fmt.Sprintf("Chapter %d", chap.number)
		}
		chapters = append(chapters, types.Chapter{
			Index:     len(chapters) + 1,
			Title:     title,
			StartTime: startTime,
		})
	}
	if len(chapters) == 0 {
		return nil
	}
	// End each chapter at the next one's start; the last at the file duration (0 if unknown).
	for i := range chapters[:len(chapters)-1] {
		chapters[i].EndTime = chapters[i+1].StartTime
	}
	chapters[len(chapters)-1].EndTime = fileDuration
	return chapters
```

(`fileDuration` ≤ 0 previously left EndTime 0; assigning `fileDuration` directly gives the same 0
for the 0 case. If you prefer to be explicit, guard with `if fileDuration > 0`.)
If lint then reports `//nolint:gocyclo` on line 29 as unused, delete that directive; leave the
`nestif` one unless it is reported too.

**Verify**: `go build ./...` → exit 0; `go test ./internal/vorbis/ ./internal/ogg/` → pass.

### Step 4: FLAC — collect comments and apply CHAPTER-comment precedence

In `internal/flac/parser.go`:
1. Change `parseVorbisComment` to `func parseVorbisComment(sr *binary.SafeReader, offset, _ int64, file *types.File) ([]string, error)`.
   Declare `var comments []string` (no preallocation — the count is untrusted), append each
   `comment` right before `vorbis.ParseComment`, and return `comments, nil` at the end. On the
   early error returns, return the comments collected so far: `return comments, err` /
   `return comments, fmt.Errorf(...)`.
2. In `Parse`, declare `var vorbisComments []string` before the block loop. In the
   `case blockTypeVorbisComment:` branch:

   ```go
   comments, err := parseVorbisComment(sr, offset, blockLength, file)
   vorbisComments = append(vorbisComments, comments...)
   if err != nil {
   	// existing warning, unchanged
   }
   ```
3. Immediately after the loop (before `// Set container and codec`), add:

   ```go
   // CHAPTERxxx= comments carry titles and are what audiobook taggers write; they take
   // precedence over CUESHEET track markers. Runs after the loop so block order doesn't matter
   // and STREAMINFO's duration is known.
   if chapters := vorbis.ParseChapters(vorbisComments, file.Audio.Duration); len(chapters) > 0 {
   	file.Chapters = chapters
   }
   ```

   (No warning when both sources exist — it is not an error, and warnings fail strict mode.)

**Verify**: `go build ./...` → exit 0; `go test ./internal/flac/` → pass.

### Step 5: Ogg — compute duration before parsing comments

In `internal/ogg/parser.go`:
- Vorbis branch: move the whole `// Calculate duration from last page's granule position` block
  (currently lines 112–125) so it sits **between** `parseVorbisIdentification` (98–100) and the
  `// Parse Vorbis comment header (packet 1)` block.
- Opus branch: move the `// Calculate duration (Opus always at 48kHz)` block (lines 144–155) to sit
  between `parseOpusHead` (130–132) and `// Parse OpusTags comment header (packet 1)`. Leave the
  `// Estimate bitrate for Opus` block after both.

Do not change any statement — only order. Add a one-line comment above each moved block:
`// Duration first: the comment parser derives the last chapter's end time from it.`

**Verify**: `go test ./internal/ogg/` → existing tests pass unchanged.

### Step 6: Update the Chapter doc

In `internal/types/chapter.go` change the FLAC bullet to
`//   - FLAC files (CHAPTER Vorbis comments, else the CUESHEET metadata block)`.

**Verify**: `gofmt -l .` → no output.

### Step 7: Tests (see Test plan), then full gates

**Verify**: `go test -race ./...` → all pass; `mise exec -- golangci-lint run ./...` → `0 issues.`;
`go vet ./...` → exit 0; `gofmt -l .` → no output.

## Test plan

**FLAC fixture builders** — `internal/flac/fixtures_test.go`. First run
`grep -n "func flacBlock" internal/flac/*_test.go`; if it already exists (plan 034 landed), reuse
it and skip creating. Otherwise create (package `flac`, imports `bytes`, `encoding/binary`):

```go
// flacBlock encodes a metadata block header (last flag, 7-bit type, 24-bit BE length) + payload.
func flacBlock(last bool, blockType byte, payload []byte) []byte {
	hdr := blockType
	if last {
		hdr |= 0x80
	}
	n := len(payload)
	return append([]byte{hdr, byte(n >> 16), byte(n >> 8), byte(n)}, payload...)
}

// streamInfoPayload returns a 34-byte STREAMINFO body for 16-bit stereo.
func streamInfoPayload(sampleRate, totalSamples uint64) []byte {
	buf := &bytes.Buffer{}
	binary.Write(buf, binary.BigEndian, uint16(4096)) // min block size
	binary.Write(buf, binary.BigEndian, uint16(4096)) // max block size
	buf.Write(make([]byte, 6))                        // min/max frame size
	packed := sampleRate<<44 | uint64(1)<<41 | uint64(15)<<36 | totalSamples
	binary.Write(buf, binary.BigEndian, packed)
	buf.Write(make([]byte, 16)) // MD5
	return buf.Bytes()
}

// vorbisCommentPayload returns a VORBIS_COMMENT block body (little-endian lengths).
func vorbisCommentPayload(comments ...string) []byte {
	buf := &bytes.Buffer{}
	vendor := "audiometa-test"
	binary.Write(buf, binary.LittleEndian, uint32(len(vendor)))
	buf.WriteString(vendor)
	binary.Write(buf, binary.LittleEndian, uint32(len(comments)))
	for _, c := range comments {
		binary.Write(buf, binary.LittleEndian, uint32(len(c)))
		buf.WriteString(c)
	}
	return buf.Bytes()
}

// buildFLAC returns "fLaC" followed by the given encoded blocks (and any trailing bytes).
func buildFLAC(parts ...[]byte) []byte {
	out := []byte("fLaC")
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}
```

**CUESHEET builder** — in `internal/flac/chapters_test.go`:

```go
type testIndex struct {
	offset uint64
	number byte
}
type testTrack struct {
	offset  uint64
	number  byte
	indices []testIndex
}

func cueSheetPayload(isCD bool, tracks []testTrack) []byte {
	b := &bytes.Buffer{}
	b.Write(make([]byte, 128))                    // media catalog number
	binary.Write(b, binary.BigEndian, uint64(0))  // lead-in
	var flags byte
	if isCD {
		flags = 0x80
	}
	b.WriteByte(flags)
	b.Write(make([]byte, 258)) // reserved
	b.WriteByte(byte(len(tracks)))
	for _, t := range tracks {
		binary.Write(b, binary.BigEndian, t.offset)
		b.WriteByte(t.number)
		b.Write(make([]byte, 12)) // ISRC
		b.WriteByte(0x00)         // audio, no pre-emphasis
		b.Write(make([]byte, 13)) // reserved
		b.WriteByte(byte(len(t.indices)))
		for _, ix := range t.indices {
			binary.Write(b, binary.BigEndian, ix.offset)
			b.WriteByte(ix.number)
			b.Write(make([]byte, 3))
		}
	}
	return b.Bytes()
}
```

Parse with `(&parser{}).Parse(context.Background(), bytes.NewReader(data), int64(len(data)), "test.flac")`.
Use sample rate 44100 and `totalSamples = 44100*600` (Duration exactly 600 s) everywhere.

Tests in `internal/flac/chapters_test.go` (assert exact `time.Duration` values):
1. `TestParse_CueSheetCD` — blocks: STREAMINFO, CUESHEET (last, `isCD=true`) with tracks
   `{0, 1, [{0,1}]}`, `{2646000, 2, [{0,0},{88200,1}]}` (2 s pregap), lead-out `{26460000, 170, nil}`.
   Expect exactly 2 chapters: `{Index 1, "Track 01", 0, 62s}`, `{Index 2, "Track 02", 62s, 600s}`,
   and no warnings. (Before Step 1 this returned 0 chapters — the regression.)
2. `TestParse_CueSheetNonCDLeadOut` — `isCD=false`, tracks `{0,1,[{0,1}]}`, `{4410000,2,[{0,1}]}`,
   lead-out `{26460000, 255, nil}`. Expect 2 chapters; last `{100s, 600s}`; no chapter titled
   `"Track 255"`.
3. `TestCuesheetToChapters_NoLeadOut` — call `cuesheetToChapters` directly with
   `&CueSheet{Tracks: []CueTrack{{Offset: 0, Number: 1, IsAudio: true}, {Offset: 441000, Number: 2, IsAudio: true}}}`,
   44100, `600*time.Second` → last `EndTime == 600s`; same with duration `0` → last `EndTime == 0`.
4. `TestParse_FLACChapterComments` — STREAMINFO, VORBIS_COMMENT (last) with
   `CHAPTER001=00:00:00.000`, `CHAPTER001NAME=Opening`, `CHAPTER002=00:05:00.000`,
   `CHAPTER002NAME=Middle` → 2 chapters, titles `Opening`/`Middle`, ends `5m`/`600s`.
5. `TestParse_ChapterCommentsWinOverCueSheet` — STREAMINFO, VORBIS_COMMENT (not last, CHAPTER
   comments as in 4), CUESHEET (**last**, CD layout from 1). Expect the comment chapters
   (`Opening`/`Middle`), proving precedence regardless of block order.

**Vorbis unit tests** — create `internal/vorbis/chapters_test.go` (model on
`internal/vorbis/comments_test.go`):
- three valid chapters, duration 10 min → contiguous ends, last = 10 min, Index 1..3;
- `CHAPTER002=garbage` among 001/002/003 → 2 chapters, Index 1 and 2, chapter 1's
  `EndTime` == chapter 003's start, no zero-value entry;
- duration 0 → last `EndTime == 0`;
- every timestamp invalid → `nil`;
- `CHAPTER005NAME=Only name` without timestamp → dropped.

**Ogg tests** — in `internal/ogg/parser_test.go`, extend the builders without breaking callers:
change signatures to `createMinimalOgg(title, artist, album string, extra ...string)` and
`createMinimalOpus(title, artist, album string, extra ...string)`, appending `extra...` to
`comments` right after the ALBUM line. Then add `TestParse_ChapterEndTimeUsesDuration` (Vorbis)
and `TestParseOpus_ChapterEndTimeUsesDuration`: extra comments
`CHAPTER001=00:00:00.000`, `CHAPTER001NAME=Intro`, `CHAPTER002=00:00:00.500` → 2 chapters;
`Chapters[0].EndTime == 500*time.Millisecond`; `file.Audio.Duration > 0` and
`Chapters[1].EndTime == file.Audio.Duration`.

## Done criteria

- [ ] `go build ./...`, `go vet ./...` exit 0; `gofmt -l .` prints nothing
- [ ] `mise exec -- golangci-lint run ./...` → `0 issues.`
- [ ] `go test -race ./...` passes, including the 5 FLAC, 5 vorbis and 2 Ogg tests above
- [ ] `grep -n "offset += 259" internal/flac/cuesheet.go` → no match
- [ ] `grep -n "ParseChapters" internal/flac/parser.go` → one match
- [ ] `git status` shows only in-scope files modified/created
- [ ] `plans/README.md` status row for 024 updated

## STOP conditions

- The excerpts above don't match the live code (drift) — especially if `parseCueSheet` already skips
  258 or `internal/flac/parser.go` already calls `ParseChapters`.
- An existing test asserts a zero last-chapter `EndTime` or a chapter with Index 0 — report it; do
  not edit the assertion to fit without reporting.
- Moving the Ogg duration block changes any existing Ogg test result (it should not: duration does
  not depend on comments).
- Lint requires splitting `Parse` in `internal/flac/parser.go` (it already carries
  `//nolint:gocyclo`) — report rather than refactoring the loop.
- A verification fails twice after a reasonable fix attempt.

## Maintenance notes

- **Plan 016** must run after this: its `types.FillChapterEndTimes` characterization should capture
  the corrected behavior (FLAC/Vorbis last chapter = lead-out or file duration). Its "historical
  behavior: keep zero" note about FLAC/Vorbis is superseded by this plan.
- Precedence decision (documented in code): CHAPTER comments > CUESHEET. A product owner may want
  "CUESHEET when comments have no titles" — revisit only if asked.
- Cuesheet chapter titles remain `Track NN`; a future improvement could pull `TITLE` from a
  `CUESHEET` Vorbis comment (embedded .cue text), deliberately not done here.
- Reviewer: check the 258 math against the spec, that `isLeadOut` is only applied positionally to
  the last track for end-time purposes, and that the Ogg move is a pure reorder.
- Plans 002/008 cite line numbers in `internal/flac/parser.go`; they will shift after this lands
  (their drift checks will flag it — compare by content).
