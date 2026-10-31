# Plan 019: Ogg/ID3 container correctness — granule scan, packet lacing, multi-page headers, unsync, stsd

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving on. If a
> STOP condition occurs, stop and report — do not improvise. When done, update
> the status row for this plan in `plans/README.md`.
>
> **Drift check (run first)**: `git diff --stat aca9750..HEAD -- internal/ogg/container.go internal/ogg/parser.go internal/mp3/id3v2.go internal/m4a/technical.go internal/binary/reader.go`
> On a mismatch with the excerpts below, STOP for the affected part (parts A, C, D are
> independent; E builds on B — a drift in one does not block the others). The five parts
> are independent — a drift in one does not block the others).

## Status

- **Priority**: P3
- **Effort**: M (five small fixes bundled by theme; A/C/D independent, E builds on B)
- **Risk**: MED (parts A/B/E touch packet framing and duration; corpus + fuzz coverage required)
- **Depends on**: 001
- **Category**: bug
- **Planned at**: commit `aca9750`, 2026-10-30 (refreshed from 7b22897, 2026-07-01)

## Why this matters

Five verified spec-conformance gaps that produce wrong output on less-common but legitimate
files. They're bundled because all five are "the parser trusts a shortcut the container format
doesn't guarantee."

- **A. Ogg duration can read a granule from payload bytes.** The last-page search scans
  backward for the literal `OggS` and reads the granule at +6 **without validating it's a real
  page header**. Audio payload bytes can contain `OggS`; the backward scan then hits that
  payload occurrence *before* the true final header → garbage granule → wrong duration.
- **B. Ogg packet extraction ignores the segment lacing table.** `readPage` sums the segment
  table then discards it; `extractPackets` treats each page as one packet, splitting only on the
  continuation flag. Pages carrying multiple packets (or a packet ending exactly on a 255-byte
  lacing boundary) are mis-framed. Today's files usually put the comment header on its own page,
  so it *happens* to work — but `len(packets) < 2` (`parser.go:87`) can spuriously reject valid
  files whose header packets share a page, and any non-canonical muxing mis-parses.
- **C. ID3v2 unsynchronization is never undone; frame flags ignored.** The header flag byte's
  unsync bit (0x80) and per-frame flags (compression/encryption/data-length-indicator) are read
  and dropped. Tags written with unsync (or v2.4 DLI frames) yield corrupted text or a
  desynchronized frame walk.
- **D. M4A technical info assumes the first `trak` is audio and a version-0 sample entry.**
  Files whose first track is the chapter text track (or use v1/v2 sound sample entries) get
  channels/sample-rate read from the wrong bytes. (Investigate-grade: needs a real file to
  confirm frequency, but the code assumption is verifiable.)
- **E. Ogg header packets spanning more than two pages are truncated.** `Parse` reads at most
  three pages (`i < 3`). The identification header owns page 0, so the comment header gets at
  most pages 1–2 (≈2 × 64 KB). A Vorbis/Opus comment packet carrying embedded cover art
  (`METADATA_BLOCK_PICTURE`, base64) routinely spans more pages: the packet is cut off, every
  comment after the cut is lost (with "truncated comment" warnings), and `ExtractArtwork` —
  which re-runs `Parse` and base64-decodes the picture value — silently returns no artwork.

## Current state

**A** — `internal/ogg/container.go:159-178` (verified):

```go
// Find last "OggS" marker
lastOggPos := int64(-1)
for i := len(buf) - 4; i >= 0; i-- {
	if buf[i] == 'O' && buf[i+1] == 'g' && buf[i+2] == 'g' && buf[i+3] == 'S' {
		lastOggPos = searchStart + int64(i)
		break
	}
}
...
granule, err := binary.ReadLE[uint64](sr, lastOggPos+6, "granule position")
```

Ogg page header layout (RFC 3533): `OggS`(4) `version`(1, must be 0) `header_type`(1)
`granule`(8) `serial`(4) `sequence`(4) `crc`(4) `segment_count`(1) `lacing[segment_count]`.
Page length = 27 + segment_count + sum(lacing).

**B** — `internal/ogg/container.go:78-101` discards lacing (`dataSize := sum(segments)`), and
`extractPackets` (`:113-140`) splits only on `page.HeaderType&0x01` continuation.

**C** — `internal/mp3/id3v2.go:100-104` reads `Flags: buf[5]` but no code consults bit 0x80;
`readSingleFrame` (`:180`) stores `frameFlags` unused. ID3v2.3 spec: when the tag-level unsync
flag is set, every `FF 00` byte pair in the tag body was written as `FF 00` → must collapse to
`FF`. ID3v2.4 moves unsync to per-frame flag bit 0x0002 and adds a data-length-indicator frame
flag 0x0001 (4 synchsafe bytes prepended to the frame body).

**D** — `internal/m4a/technical.go:26-33` takes the **first** `trak` under `moov`; the correct
selection walks `trak → mdia → hdlr` and picks the track whose handler type is `soun`.
`parseStsd` (`:154+`) skips the sample-entry version field without branching (v1 adds 16 bytes
of extension fields before the ones it reads; v2 is a different layout).
*Refresh note (aca9750):* per the QuickTime SoundDescription layout, v1's 16 extension bytes
(samplesPerPacket, bytesPerPacket, bytesPerFrame, bytesPerSample) come **after** the sample-rate
field, so v0 and v1 share the channel/sample-rate offsets the code reads; only v2 moves them.
Step D is written to that layout.

**E** — `internal/ogg/parser.go:55-89` (verified at aca9750):

```go
// Read up to 3 pages (identification, comment, setup headers)
for i := 0; i < 3 && offset < size; i++ {
	page, nextOffset, err := readPage(sr, offset)
	...
	pages = append(pages, page)
	offset = nextOffset
}
...
packets := extractPackets(pages)
if len(packets) < 2 {
	return nil, fmt.Errorf("not enough packets found (need at least 2, got %d)", len(packets))
}
```

`internal/ogg/artwork.go:20-55` `ExtractArtwork` calls `p.Parse(...)` and scans
`file.Tags.All()` for `METADATA_BLOCK_PICTURE`; a value that fails base64/picture decoding is
skipped with no warning (`continue`). RFC 3533 / Vorbis I §4.2.1: the identification header is
alone on the first page; the comment and setup headers may span any number of pages. Opus
(RFC 7845 §3): `OpusHead` alone on page 0, `OpusTags` begins on page 1 and may span pages.
Plan 002 introduces `binary.MaxAllocBytes` (256 MiB) as the global per-allocation ceiling.

## Commands you will need

| Purpose | Command | Expected |
|---|---|---|
| Build | `go build ./...` | exit 0 |
| Tests | `go test ./internal/ogg/ ./internal/mp3/ ./internal/m4a/ ./...` | all pass |
| Fuzz Ogg | `go test -run '^$' -fuzz FuzzParseOgg -fuzztime 60s ./internal/ogg/` | no crashers |
| Fuzz MP3 | `go test -run '^$' -fuzz FuzzParseMP3 -fuzztime 60s ./internal/mp3/` | no crashers |
| Corpus check | `go test -run TestOpen_Corpus ./...` | durations/tags still correct |
| Vet | `go vet ./...` | exit 0 |
| Race | `go test -race ./...` | all pass |
| Lint | `mise exec -- golangci-lint run ./...` | exit 0, prints `0 issues.` |
| Format | `gofmt -l .` | no output |

> **Lint note**: run lint via `mise exec -- golangci-lint run ./...` so the mise-pinned
> golangci-lint v2.15.0 is used. A stale global golangci-lint (e.g. 2.11.4) on PATH reports
> false "export data version" typecheck errors under Go 1.27.

## Scope

**In scope**:
- `internal/ogg/container.go` (+ tests) — parts A and B
- `internal/ogg/parser.go` — part E (header-page loop), and anything B's `Page` change requires
- `internal/mp3/id3v2.go` (+ tests) — part C
- `internal/m4a/technical.go` (+ tests) — part D
- `testdata/` additions for regression fixtures

**Out of scope**:
- Opus pre-skip duration adjustment (recorded as deferred in plans/README.md — include only if
  trivial while editing `calculateDuration`, and say so in the report).
- MP3 tables/Xing (plans 012/013), allocation caps (002), ctx (003).
- Compressed/encrypted ID3 frames: **skip** them safely (advance by the declared size, emit a
  warning) — do not implement decompression.

## Git workflow

- Branch: `advisor/019-container-correctness`
- Commit per part (A/B/C/D/E) — gitmoji single-line, no body, no attribution
  (e.g. `🐛 Validate Ogg page header before reading granule`).
- Do NOT push or open a PR unless instructed.

## Steps

### Step A: Validate the candidate page header before trusting its granule

In `findLastGranulePosition`, when a backward scan hits `OggS`, verify it is a plausible page
header before accepting: `buf[i+4] == 0` (version), header_type ≤ 0x07, and
`27 + segment_count + sum(lacing)` fits within the remaining window or ends exactly at EOF.
If validation fails, continue scanning backward. All checks can run against the already-read
64 KB buffer — no extra I/O.

Regression test: craft a final page whose **payload contains the bytes `OggS`** followed by
plausible-looking garbage, with the true final page header earlier in the window; assert the
computed duration matches the true header's granule. Also keep a plain-fixture test
(`testdata/sample.ogg` duration ≈ 2 s) passing.

**Verify**: `go test ./internal/ogg/` → pass, including the new payload-collision test.

### Step B: Split packets on the lacing table

Retain the segment table on `Page` (add field `Lacing []byte`), then rewrite `extractPackets`
per RFC 3533: walk each page's lacing values; a value < 255 terminates the current packet;
255 continues it (possibly across pages via the continuation flag). Preserve the existing
behavior that packet data accumulates across pages when `HeaderType&0x01` is set.

Regression tests:
1. Two packets in one page (lacing `[..., <255-terminated>, <255-terminated>]`) → 2 packets out
   (this is the header-sharing case that `parser.go:87` currently rejects).
2. A packet of exactly 255 bytes (lacing `[255, 0]`) → one 255-byte packet.
3. A packet spanning two pages (first page ends with 255-lacing, next page has continuation
   flag) → one concatenated packet.
4. Existing fixtures (`testdata/sample.ogg`, `sample.opus`) parse identically (tags + duration).

**Verify**: `go test ./internal/ogg/` → pass; corpus test unchanged; fuzz 60s → no crashers.

### Step C: Undo unsynchronization; honor the frame flags you can't ignore

In `parseID3v2` / `readSingleFrame`:
1. If the **tag-level** unsync flag (`header.Flags & 0x80`, v2.3) is set: after reading the tag
   body region, collapse `FF 00` → `FF` before frame parsing. Practical approach: read the whole
   tag body (bounded by `header.Size`, which plan 002 caps) into memory once, de-unsync the
   buffer, and parse frames from the buffer. If that restructure is too invasive, de-unsync
   per-frame payload (v2.4 semantics) and document the v2.3 approximation — STOP only if neither
   is achievable without touching out-of-scope files.
2. For **v2.4 per-frame flags** (`frameFlags`): if unsync bit (0x0002) set → de-unsync the frame
   payload; if DLI bit (0x0001) set → skip the 4-byte synchsafe length prefix before decoding;
   if compression (0x0008) or encryption (0x0004) set → skip the frame with a warning.

Regression tests: build a v2.3 tag with unsync applied to a `TIT2` payload containing `FF 00`
sequences → title decodes correctly; a v2.4 frame with DLI → text decodes; a compressed frame →
skipped with warning, following frames still parsed (offset arithmetic correct).

**Verify**: `go test ./internal/mp3/` → pass; fuzz 60s → no crashers.

### Step D: Select the `soun` track; branch on stsd sample-entry version

In `parseTechnicalInfo` (`internal/m4a/technical.go:24-29`), instead of taking the first
`trak` via `findAtom(..., atomTrak)`:

1. Walk the children of `moov` the same way `findChapterTrackReference`
   (`internal/m4a/chapters.go:163-184`) does (`readAtomHeader`, advance by `Size`, match
   `Type == atomTrak`). For each `trak`, find `mdia` → `hdlr` with `findAtom`; `hdlr` is a full
   atom: `version/flags`(4) `pre_defined`(4) `handler_type`(4), so the handler type is the 4 bytes
   at `hdlrAtom.DataOffset()+8`. Select the first `trak` whose handler type is `soun`.
2. If no `trak` has a `soun` handler (or `hdlr` is missing), fall back to the first `trak` —
   today's behavior — so files that currently parse keep parsing.
3. In `parseStsd`, replace the blind `offset += 8` at `:204-206` with a read of the sample-entry
   version (`uint16` at entry start + 16; entry start = the offset of the entry-size field).
   - version 0 or 1: keep the current offsets (channels at entry start + 24, 16.16 sample rate
     at entry start + 32). v1's extra 16 bytes follow the sample rate and do not need skipping.
   - version 2 (QuickTime `SoundDescriptionV2`): the legacy fields hold constants (channels = 3,
     sample rate = 0x00010000); read `audioSampleRate` as a big-endian IEEE-754 `float64` at entry
     start + 40 and `numAudioChannels` as `uint32` at entry start + 48.
   - any other version: leave channels/sample rate unset and return nil (no guessing).
   Keep the `parseCodecDetails` call unchanged.

Regression tests (in `internal/m4a/technical_test.go`, building atoms in-memory like the existing
tests there and in `chapters_test.go`):
1. `moov` with a `text` (chapter) `trak` first and a `soun` `trak` second → channels and sample
   rate come from the second track.
2. `moov` with a single `trak` lacking `hdlr` → falls back to it (today's behavior).
3. v2 sample entry with `audioSampleRate = 48000.0`, `numAudioChannels = 2` → `SampleRate == 48000`,
   `Channels == 2` (today this yields `SampleRate == 1`, `Channels == 3`).
4. v1 sample entry → same values as the equivalent v0 entry.

**Verify**: `go test ./internal/m4a/` → pass; `go test -run TestOpen_Corpus ./...` → M4A fixture
unchanged.

### Step E: Read header pages until the comment packet completes (bounded)

Depends on Step B (packet boundaries must come from the lacing table, or "packet complete" can't
be detected). In `internal/ogg/parser.go`, replace the fixed `for i := 0; i < 3 && offset < size; i++`
loop with one that keeps reading pages until `extractPackets(pages)` yields **two complete
packets** (identification + comment; the setup header is not used), the file ends, or a byte cap
is hit:

1. Track the cumulative page-data bytes read. Define a package constant, e.g.
   `const maxHeaderBytes = 16 << 20 // 16 MiB: comment packets with embedded cover art`
   (if plan 002 has landed, additionally keep it ≤ `binary.MaxAllocBytes`). When the cap would
   be exceeded, stop reading and append a `types.StageMetadata` warning
   ("Ogg comment header exceeds N bytes; truncated") — parse with what you have, as today.
2. Expose packet completeness from Step B's `extractPackets` (e.g. return the completed packets
   plus whether the last one is still open, or add a small helper) — a packet is complete when
   its final lacing value is < 255. Do not count an unterminated trailing packet.
3. Keep today's behavior for a read error on page 0 (fatal) and later pages (warning + stop).
4. Only consider pages of the first logical stream: skip pages whose `SerialNumber` differs from
   page 0's (multiplexed/chained files), so another stream's pages can't be glued into the packet.

Regression tests (in `internal/ogg/parser_test.go`, extending the `createMinimalOgg` /
`createMinimalOpus` builders so the comment packet can be split across N pages):
1. Vorbis and Opus files whose comment packet spans 4 pages (e.g. a ~200 KB
   `METADATA_BLOCK_PICTURE` value followed by a `TITLE=` comment) → the title after the picture is
   parsed, no "truncated comment" warning, and `ExtractArtwork` returns one image with the
   expected bytes.
2. A comment packet larger than `maxHeaderBytes` (use a test-only smaller cap if the constant is
   unexported — e.g. make the loop take the cap as a parameter) → parse succeeds with the cap
   warning; no unbounded read.
3. Existing fixtures and `TestParse_Success` / `TestParseOpus_Success` unchanged.

**Verify**: `go test ./internal/ogg/` → pass; fuzz `FuzzParseOgg` 60s → no crashers.

### Step F: Full gates

Run every gate on the finished branch.

**Verify**: `go build ./...` → exit 0; `go vet ./...` → exit 0; `go test ./...` → all pass;
`go test -race ./...` → all pass; `mise exec -- golangci-lint run ./...` → `0 issues.`;
`gofmt -l .` → no output.

## Test plan

- Part A: payload-contains-`OggS` regression in `internal/ogg/parser_test.go` (or a new
  `internal/ogg/container_test.go`), plus the plain-fixture duration check.
- Part B: the four lacing cases in Step B, as table-driven tests on `extractPackets` with
  hand-built `Page` values (no file I/O needed).
- Part C: v2.3 tag-level unsync, v2.4 per-frame unsync + DLI, compressed-frame skip — in a new
  `internal/mp3/id3v2_test.go`, building tags in-memory the way `createMinimalMP3WithID3` in
  `internal/mp3/parser_test.go` does.
- Part D: the four cases in Step D in `internal/m4a/technical_test.go`.
- Part E: multi-page comment packet (Vorbis + Opus, with artwork) and over-cap packet in
  `internal/ogg/parser_test.go`.
- Structural pattern: table-driven, stdlib `testing` only, plain `if got != want { t.Errorf }`
  (see `internal/ogg/parser_test.go:235` `TestParse_Success`).
- Verification: `go test -race ./...` → all pass including the new tests; each fuzz target 60 s →
  no crashers.

## Done criteria

- [ ] `go build ./...` and `go vet ./...` exit 0
- [ ] `go test ./...` and `go test -race ./...` exit 0; new tests for parts A–E exist and pass
- [ ] `mise exec -- golangci-lint run ./...` prints `0 issues.`; `gofmt -l .` prints nothing
- [ ] `grep -n "i < 3" internal/ogg/parser.go` → no match
- [ ] `findLastGranulePosition` validates version byte and page length before reading the granule
- [ ] `Page` carries the lacing table and `extractPackets` splits on it
- [ ] `grep -n "0x80" internal/mp3/id3v2.go` shows the tag-level unsync flag consulted
- [ ] `parseTechnicalInfo` selects the `soun` track; `parseStsd` branches on sample-entry version
- [ ] `FuzzParseOgg` and `FuzzParseMP3` run 60 s each without crashers
- [ ] Only in-scope files modified (`git status`)
- [ ] `plans/README.md` status row for 019 updated

## STOP conditions

- The excerpts in "Current state" don't match the live code for a part — STOP that part only.
- Plan 001's fuzz targets / `testdata/` corpus don't exist yet — STOP (this plan depends on 001
  for its regression net).
- Step B changes the packet count or tag output for an existing fixture (`testdata/sample.ogg`,
  `sample.opus`, or the synthetic builders) — STOP and report the diff; that means today's
  output relied on the mis-framing and the operator should decide.
- Part C's buffer-based de-unsync can't be done without restructuring `parseID3v2Frames` beyond
  `internal/mp3/id3v2.go`, and the per-frame fallback also fails — STOP (as Step C says).
- Part D: a real M4B in the corpus changes channels/sample rate after the change — STOP and report
  the before/after values; the `soun` assumption may need refining.
- Part E would require changing `ExtractArtwork`'s signature or the public `ArtworkExtractor`
  interface — STOP; it should only need the `Parse` loop.
- A step's verification fails twice after a reasonable fix attempt.

## Maintenance notes

- Parts B and E change how many pages and packets the Ogg parser reads. Any future Ogg feature
  (e.g. reading the setup header, chained streams, Opus pre-skip) builds on the lacing-aware
  `extractPackets`; keep packet assembly in one place.
- `maxHeaderBytes` is a policy knob: if `WithMaxArtworkSize` (plan 008) ever moves enforcement
  into extractors, the Ogg comment cap is where an artwork limit would apply at parse time.
- Reviewers should scrutinize offset arithmetic in Part C (frames after a skipped
  compressed/encrypted frame, DLI prefix) and the v2 `stsd` offsets in Part D against a real
  file if one is available.
- Deferred: Opus pre-skip duration adjustment (see Scope); decompression of compressed ID3
  frames; full ISO-BMFF `AudioSampleEntryV1` (`srat` box for rates > 65535 Hz).
