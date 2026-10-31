# Plan 031: Read QuickTime chapter-track sample tables per spec (stsz default, stsc, chpl version)

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving on. If a
> STOP condition occurs, stop and report — do not improvise. When done, update
> the status row for this plan in `plans/README.md` (add a row for 031 if none
> exists) — unless a reviewer dispatched you and told you they maintain the index.
>
> **Drift check (run first)**: `git diff --stat aca9750..HEAD -- internal/m4a/chapters.go internal/m4a/chapters_test.go`
> If either changed, compare the "Current state" excerpts against the live code. If the only
> drift is plan 002's `CheckLen` guards or plan 003's `ctx` parameters (and `plans/README.md`
> marks that plan DONE), keep their additions and apply this plan around them. Any other
> mismatch is a STOP condition.

## Status

- **Priority**: P2
- **Effort**: M
- **Risk**: LOW–MED (changes chapter output for files whose tables were previously misread; spec-conformant files that already parsed correctly must produce identical output)
- **Depends on**: none
- **Category**: bug
- **Planned at**: commit `aca9750`, 2026-10-30

## Why this matters

Chapters are the most visible metadata for an audiobook server, and M4B chapters usually live
in a QuickTime text track. `internal/m4a/chapters.go` reads that track's sample tables with
shortcuts that break on valid files: (1) when `stsz` uses a single default sample size (which
ffmpeg emits when every chapter-title sample has the same byte length), the parser ignores the
default and reads non-existent table entries out of the following atoms, so titles come back
empty or chapters vanish; (2) when a chunk holds more than one sample, the parser assumes one
sample per chunk and points later samples at file offset 0, producing empty titles — the
correct mapping needs the `stsc` (sample-to-chunk) table, which is never read; (3) the Nero
`chpl` fallback always skips 4 "reserved" bytes, but those exist only when the atom's version
is non-zero, so version-0 `chpl` atoms are parsed 4 bytes out of alignment. After this plan,
all three layouts produce correct titles and timings.

## Current state

File: `internal/m4a/chapters.go` — QuickTime text-track chapters (`parseQuickTimeChapters` →
`parseTextTrackChapters`) with Nero `chpl` fallback (`parseChplChapters`).
Tests: `internal/m4a/chapters_test.go` — covers **only** `chpl`; there is no fixture for a
QuickTime chapter track.

**(1) `stsz` default size discarded** — `chapters.go:374-407`:

```go
	offset := stszAtom.DataOffset() + 4 // Skip version + flags

	// Read default sample size (if 0, sizes are in table)
	_, err = binary.Read[uint32](sr, offset, "default sample size")
	if err != nil {
		return nil, err
	}
	offset += 4

	sampleCount, err := binary.Read[uint32](sr, offset, "sample count")
	...
	sampleSizes := make([]uint32, sampleCount)
	for i := range sampleCount {
		size, err := binary.Read[uint32](sr, offset, "sample size")
		...
```

Per ISO/IEC 14496-12 §8.7.3 (`SampleSizeBox`): `sample_size` (the "default") — if non-zero,
every sample has that size and **no table follows**; the per-sample `entry_size` table is
present only when `sample_size == 0`.

**(2) One-sample-per-chunk assumption** — `chapters.go:458-476`, inside `buildChaptersFromText`:

```go
	sampleOffsets := make([]uint64, len(sampleSizes))

	if len(chunkOffsets) == 1 {
		// All samples are in a single chunk - calculate offsets by summing sizes
		currentOffset := chunkOffsets[0]
		for i := range sampleSizes {
			sampleOffsets[i] = currentOffset
			currentOffset += uint64(sampleSizes[i])
		}
	} else {
		// Multiple chunks - assume 1:1 mapping (one sample per chunk)
		// This is a simplification; proper handling would parse stsc atom
		for i := 0; i < len(chunkOffsets) && i < len(sampleSizes); i++ {
			sampleOffsets[i] = chunkOffsets[i]
		}
	}
```

Samples with index ≥ `len(chunkOffsets)` keep offset 0. `stsc` layout (ISO/IEC 14496-12
§8.7.4, `SampleToChunkBox`): full-box header (version+flags, 4 bytes), `entry_count` (u32), then
`entry_count` × { `first_chunk` u32 (1-based), `samples_per_chunk` u32,
`sample_description_index` u32 }. An entry applies from `first_chunk` up to (not including)
the next entry's `first_chunk`; the last entry applies to all remaining chunks. Within a chunk,
samples are contiguous, starting at the chunk offset, each advancing by its `stsz` size.

**(3) `chpl` version ignored** — `chapters.go:59-73`:

```go
	// Read version (1 byte)
	_, err = binary.Read[uint8](sr, offset, "chpl version")
	if err != nil {
		return nil, err
	}
	offset++

	// Skip flags (3 bytes)
	offset += 3

	// Skip reserved (4 bytes)
	offset += 4
```

Reference implementation, ffmpeg `libavformat/mov.c` `mov_read_chpl`:
`version = avio_r8(pb); avio_rb24(pb); if (version) avio_rb32(pb); nb_chapters = avio_r8(pb);`
— the 4-byte field exists only when `version != 0`. ffmpeg's own muxer and mp4v2 write
version 1, which is why the current code works for most files; version-0 atoms are misparsed.
The existing test builder `createChplAtom` (`chapters_test.go:13-41`) writes version 1 + 4
reserved bytes, so existing tests remain valid.

**(4) Shadowed `readErr`** — `chapters.go:428-444`, in `parseChunkOffsets`:

```go
	for i := range chunkCount {
		var readErr error
		if stcoAtom.Type == "co64" {
			chunkOffsets[i], readErr = binary.Read[uint64](sr, offset, "chunk offset")
			offset += 8
		} else {
			offset32, readErr := binary.Read[uint32](sr, offset, "chunk offset")   // := shadows
			if readErr != nil {
				break // Return partial results
			}
			...
		}
		if readErr != nil {
			break // Return partial results
		}
	}
```

Harmless today (the inner `break` masks it) but misleading; `govet shadow` is disabled in
`.golangci.yml`, so lint won't catch it. This plan owns the fix; plan 017 must not touch it.

**Call site** — `parseTextTrackChapters`, `chapters.go:282-306`:

```go
	chapterTimes, err := parseChapterTimings(sr, stblAtom, timescale)
	...
	sampleSizes, err := parseSampleSizes(sr, stblAtom)
	...
	chunkOffsets, err := parseChunkOffsets(sr, stblAtom)
	...
	chapters := buildChaptersFromText(sr, chapterTimes, sampleSizes, chunkOffsets)
	calculateChapterEndTimes(chapters, fileDuration)
```

`buildChaptersFromText` (`chapters.go:451-497`) emits one chapter per sample index
`i < min(len(sampleOffsets), len(sampleSizes), len(chapterTimes))`, skipping samples with size
0 or ≥ 10000, and reads the title with `extractChapterTitle(sr, offset, size)` (2-byte BE length
prefix + UTF-8 text, `chapters.go:500-517`).

Atom layouts the test fixtures need (all big-endian; "full box" = 4 bytes version+flags first):
- `tkhd` v0 data: vf(4) creation(4) modification(4) **track_ID(4)** … — `readTrackID` reads the
  track ID at data offset +12 (`chapters.go:243-250`). 84 zero bytes with the ID at 12..16 is fine.
- `tref` → `chap` data: u32 referenced track ID (`chapters.go:187-203`).
- `mdhd` v0 data: vf(4) creation(4) modification(4) **timescale(4)** duration(4) lang(2) quality(2)
  — `parseTrackTimescale` reads +12 (`chapters.go:309-334`).
- `stts` data: vf(4) entry_count(4) then (sample_count u32, sample_delta u32) × n.
- `stco` data: vf(4) entry_count(4) then u32 absolute file offsets × n.
- Chapter tracks are found via `trak/tref/chap` in any track, then the `trak` whose `tkhd`
  has that ID; path to tables is `trak/mdia/minf/stbl` (`chapters.go:145-160, 259-275`).

Conventions:
- Binary reads: `binary.Read[T](sr, off, "what")`, `sr.ReadAt(buf, off, "what")` on
  `*binary.SafeReader` (`internal/binary/reader.go:33-80`).
- Fixtures: `createMockAtom(type, data)` (`internal/m4a/atoms_test.go:12`) wraps bytes in an
  8-byte atom header. Model new tests on `TestParseChapters_Success` (`chapters_test.go:43-102`).

## Commands you will need

| Purpose | Command | Expected on success |
|---|---|---|
| Build | `go build ./...` | exit 0 |
| Vet | `go vet ./...` | exit 0 |
| Targeted tests | `go test ./internal/m4a/ -run '<Name>' -v` | named tests PASS |
| All tests | `go test ./...` | all `ok` |
| Race | `go test -race ./...` | all `ok` |
| Lint | `mise exec -- golangci-lint run ./...` | `0 issues.` |
| Format | `gofmt -l .` | no output |

Lint MUST run via `mise exec --` (a stale global golangci-lint on PATH gives false typecheck
errors). `go.mod` declares `go 1.26.0`: use no standard-library API newer than Go 1.26
(`slices.Repeat`, `min`, `max`, range-over-int are all fine). `gocyclo` limit is 15
(`.golangci.yml:70-71`) and `nestif` is 5 — prefer small helper functions.

## Scope

**In scope** (the only files you should modify):
- `internal/m4a/chapters.go`
- `internal/m4a/chapters_test.go`

**Out of scope** (do NOT touch):
- Count/allocation caps on `stts`/`stsz`/`stco` (`make([]T, count)`) — plan 002. (This plan
  does bound the new default-size expansion, see Step 3, because it introduces it.)
- Context-cancellation checks in the table loops — plan 003.
- Consolidating `calculateChapterEndTimes` / the `chpl` end-time loop with other formats — plan 016.
- The `currentTime * 1_000_000_000` overflow in `parseChapterTimings` (`chapters.go:365`) —
  noted as a follow-up in plan 014.
- `sample_description_index` handling, text-sample `encd`/style boxes, and UTF-16 titles.
- `internal/m4a/technical.go`, `atoms.go`, `parser.go`.

## Git workflow

- Branch: `advisor/031-m4a-chapter-track-tables`
- One commit per step; gitmoji single-line, no body, no attribution
  (e.g. `🐛 Honor stsz default sample size in chapter tracks`; repo example:
  `♻️ Modernize for Go 1.26, add context plumbing, refresh tooling`).
- Do NOT push or open a PR.

## Steps

### Step 1: Add QuickTime chapter-track fixture builders (tests first)

In `internal/m4a/chapters_test.go` add helpers (all return `[]byte`):

```go
func u32be(vs ...uint32) []byte                               // concatenated big-endian uint32s
func fullAtom(typ string, body []byte) []byte                 // createMockAtom(typ, append(u32be(0), body...))
func textSample(s string) []byte                              // u16 BE len(s) + s
func sttsAtom(entries ...[2]uint32) []byte                    // fullAtom("stts", count + pairs)
func stszAtom(defaultSize, count uint32, sizes ...uint32) []byte // fullAtom("stsz", default, count, sizes...)
func stscAtom(entries ...[3]uint32) []byte                    // fullAtom("stsc", count + triples)
func stcoAtom(offsets ...uint32) []byte                       // fullAtom("stco", count + offsets)

// buildQTChapterFile returns a file laid out as [mdat(payload)][moov] and the moov offset.
// Chunk offsets passed to stco must be absolute: payload byte k is at file offset 8+k.
// moov = trak#1{tkhd(id=1), tref{chap{2}}} + trak#2{tkhd(id=2), mdia{mdhd(timescale), minf{stbl{stbl children}}}}
func buildQTChapterFile(timescale uint32, payload []byte, stblChildren ...[]byte) ([]byte, int64)
```

`tkhd`: `fullAtom("tkhd", body)` where body is 80 zero bytes with the track ID written at
body bytes 8..12 (i.e. data offset 12 once the 4 vf bytes are counted). `mdhd`:
`fullAtom("mdhd", u32be(0, 0, timescale, 0, 0))` (creation, modification, timescale, duration,
lang+quality). Plain containers (`trak`, `tref`, `mdia`, `minf`, `stbl`, `moov`, `mdat`) use
`createMockAtom`; `chap` is `createMockAtom("chap", u32be(2))`.

Then add a **baseline** test that already passes on current code, to prove the builder:
`TestQTChapters_OneSamplePerChunk` — samples `textSample("Intro")`, `textSample("Middle")`,
`textSample("End")` concatenated in payload; `stts (3, 1000)`; timescale 1000;
`stsz(0, 3, 7, 8, 5)`; `stsc([1,1,1])`; `stco` = 8, 15, 23. Read the moov header with
`readAtomHeader(sr, moovOffset)` and call `parseChapters(sr, moovAtom, 10*time.Second)`.
Expect 3 chapters, titles `Intro`/`Middle`/`End`, starts 0/1s/2s, ends 1s/2s/10s.

**Verify**: `go test ./internal/m4a/ -run TestQTChapters_OneSamplePerChunk -v` → PASS.
If it fails, the builder is wrong (current code handles this layout) — fix the builder, do not
change `chapters.go`.

### Step 2: Fix the shadowed `readErr`

In `parseChunkOffsets`, replace the `else` branch so it assigns the outer `readErr`:

```go
		} else {
			var offset32 uint32
			offset32, readErr = binary.Read[uint32](sr, offset, "chunk offset")
			chunkOffsets[i] = uint64(offset32)
			offset += 4
		}
		if readErr != nil {
			break // Return partial results
		}
```

(Removes the inner `break`; behavior is identical.)

**Verify**: `go test ./internal/m4a/` → `ok`; `grep -n 'readErr :=' internal/m4a/chapters.go` → no matches.

### Step 3: Honor the `stsz` default sample size

Change `parseSampleSizes` to `parseSampleSizes(sr *binary.SafeReader, stblAtom *Atom, maxSamples int) ([]uint32, error)`:

- Keep the default size: `defaultSize, err := binary.Read[uint32](...)`.
- `n := min(int(sampleCount), maxSamples)` — only samples that have a timing can become
  chapters, so never materialize more (this also bounds the allocation below by what `stts`
  already produced).
- If `defaultSize != 0`: `return slices.Repeat([]uint32{defaultSize}, n), nil` — **do not read
  a table**.
- Else: read `n` table entries as today (`make([]uint32, n)`, partial results on read error).

In `parseTextTrackChapters`, pass `len(chapterTimes)` as `maxSamples`.

Add test `TestQTChapters_StszDefaultSize`: samples `textSample("Ch 1")`, `"Ch 2"`, `"Ch 3"`
(6 bytes each) in payload; `stts (3, 1000)`; `stsz(6, 3)` (no sizes); `stsc([1,3,1])`;
`stco(8)`. stbl order: stts, stsz, stsc, stco (stsz must be followed by other atoms so the old
code would read them as sizes). Expect titles `Ch 1`/`Ch 2`/`Ch 3`, starts 0/1s/2s.

**Verify**: `go test ./internal/m4a/ -run 'TestQTChapters' -v` → both tests PASS. (Confirm
the new test fails if you temporarily `git stash` the `chapters.go` change — optional.)

### Step 4: Map samples to chunks with `stsc`

1. Add a type and parser:
   ```go
   // stscEntry is one sample-to-chunk run (ISO/IEC 14496-12 §8.7.4).
   type stscEntry struct {
   	firstChunk      uint32 // 1-based
   	samplesPerChunk uint32
   }

   // parseSampleToChunk reads stsc; returns nil, nil when the atom is absent.
   func parseSampleToChunk(sr *binary.SafeReader, stblAtom *Atom) ([]stscEntry, error)
   ```
   Read `entry_count` at data offset +4; then for each entry read `first_chunk` and
   `samples_per_chunk` (skip the 4-byte `sample_description_index`; stride 12). Append
   entries; on a read error stop and return what was read (same partial-result policy as the
   other table parsers). Do **not** `make` with `entry_count` capacity (untrusted count).
2. Add a pure function:
   ```go
   // computeSampleOffsets lays samples out in chunks per stsc. Without stsc it falls back to the
   // legacy layout: one chunk => all samples contiguous; otherwise one sample per chunk.
   // The result may be shorter than sampleSizes when the tables describe fewer samples.
   func computeSampleOffsets(sampleSizes []uint32, chunkOffsets []uint64, stsc []stscEntry) []uint64
   ```
   Algorithm with stsc: `offsets := make([]uint64, 0, len(sampleSizes))`; `e := 0`; for each
   chunk index `c` (0-based) with `chunkNum := uint32(c+1)`: advance `e` while
   `e+1 < len(stsc) && stsc[e+1].firstChunk <= chunkNum`; `pos := chunkOffsets[c]`; repeat
   `stsc[e].samplesPerChunk` times: if `len(offsets) == len(sampleSizes)` return offsets;
   `offsets = append(offsets, pos)`; `pos += uint64(sampleSizes[len(offsets)-1])`.
   Return offsets. (The early return bounds the inner loop by `len(sampleSizes)` even if
   `samplesPerChunk` is huge.)
   Legacy (no stsc): single chunk → contiguous from `chunkOffsets[0]`; multiple chunks →
   `chunkOffsets[i]` for `i < min(len(chunkOffsets), len(sampleSizes))`.
3. Change `buildChaptersFromText` to take `sampleOffsets []uint64` instead of `chunkOffsets`
   and delete its internal offset computation (`chapters.go:458-476`). Iterate
   `i < min(len(sampleSizes), len(chapterTimes))`; keep the size-0/≥10000 skip; when
   `i >= len(sampleOffsets)` emit the chapter with an empty `Title` (keeps chapter count and
   timings driven by `stts`, as today) instead of reading offset 0.
4. In `parseTextTrackChapters`: after `parseChunkOffsets`, call `parseSampleToChunk` (an error
   from it is non-fatal: treat as `nil` entries), then
   `sampleOffsets := computeSampleOffsets(sampleSizes, chunkOffsets, stsc)`, and keep the
   existing early-out when `len(chunkOffsets) == 0 || len(sampleSizes) == 0` (now in
   `parseTextTrackChapters` or `computeSampleOffsets` — return empty).

Tests:
- `TestComputeSampleOffsets` (table, pure function):
  - no stsc, chunks `[100]`, sizes `[3,4,5]` → `[100,103,107]`
  - no stsc, chunks `[100,200,300]`, sizes `[3,4,5]` → `[100,200,300]`
  - stsc `[{1,2}]`, chunks `[100,200]`, sizes `[3,4,5,6]` → `[100,103,200,205]`
  - stsc `[{1,2},{2,1}]`, chunks `[100,200,300]`, sizes `[1,1,1,1]` → `[100,101,200,300]`
  - stsc `[{1,5}]`, chunks `[100]`, sizes `[2,2]` → `[100,102]` (truncated to sample count)
  - stsc `[{1,1}]`, chunks `[100]`, sizes `[2,2,2]` → `[100]` (tables describe fewer samples)
  - stsc `[{1,4294967295}]`, chunks `[0]`, sizes `[1]` → `[0]` (huge count, bounded)
- `TestQTChapters_MultipleSamplesPerChunk`: payload = `textSample("A")` + `textSample("Bb")` +
  9 zero bytes + `textSample("Ccc")` + `textSample("Dddd")` (sizes 3, 4, 5, 6; chunk 2 starts at
  payload byte 16); `stts (4, 1000)`; `stsz(0, 4, 3, 4, 5, 6)`; `stsc([1,2,1])`; `stco(8, 24)`.
  Expect titles `A`/`Bb`/`Ccc`/`Dddd`, starts 0/1s/2s/3s.

**Verify**: `go test ./internal/m4a/ -run 'TestQTChapters|TestComputeSampleOffsets' -v` → all PASS.

### Step 5: Honor the `chpl` version

In `parseChplChapters` (`chapters.go:62-73`): keep the version byte
(`version, err := binary.Read[uint8](...)`), skip 3 flag bytes, and skip the 4-byte field
only `if version != 0`. Update the comment to cite ffmpeg `mov_read_chpl`.

Tests: refactor the builder to `createChplAtomVersion(version byte, chapters ...)` writing the
4 reserved bytes only when `version != 0`, and make `createChplAtom(chapters)` call it with
version 1 (existing tests unchanged). Add `TestParseChapters_ChplVersion0`: version 0,
chapters `{0,"One"}`, `{600_000_000,"Two"}` → 2 chapters, titles `One`/`Two`, starts 0/60s.

**Verify**: `go test ./internal/m4a/ -run 'TestParseChapters' -v` → all PASS (5 existing + 1 new).

## Test plan

Summary of new tests, all in `internal/m4a/chapters_test.go` (pattern: `TestParseChapters_Success`,
`chapters_test.go:43-102`):

1. `TestQTChapters_OneSamplePerChunk` — baseline (passes before and after).
2. `TestQTChapters_StszDefaultSize` — regression for (1).
3. `TestComputeSampleOffsets` — table of 7 cases for (2).
4. `TestQTChapters_MultipleSamplesPerChunk` — regression for (2), end-to-end.
5. `TestParseChapters_ChplVersion0` — regression for (3).

Verification: `go test ./internal/m4a/ -v 2>&1 | grep -E '^(--- FAIL|ok|FAIL)'` → no `FAIL`
lines; `go test -race ./...` → all `ok`.

## Done criteria

- [ ] `go build ./...` and `go vet ./...` exit 0
- [ ] `go test ./...` and `go test -race ./...` → all `ok`, including the 5 new tests
- [ ] `mise exec -- golangci-lint run ./...` → `0 issues.`
- [ ] `gofmt -l .` → no output
- [ ] `grep -n '_, err = binary.Read\[uint32\](sr, offset, "default sample size")' internal/m4a/chapters.go` → no matches
- [ ] `grep -n 'assume 1:1 mapping' internal/m4a/chapters.go` → no matches
- [ ] `grep -n '"stsc"' internal/m4a/chapters.go` → at least one match
- [ ] `grep -n 'readErr :=' internal/m4a/chapters.go` → no matches
- [ ] `git status --short` shows only `internal/m4a/chapters.go`, `internal/m4a/chapters_test.go` (plus `plans/README.md`)
- [ ] `plans/README.md` status row for 031 updated

## STOP conditions

Stop and report back (do not improvise) if:

- The excerpts above don't match the live code and the drift isn't plan 002/003 as described.
- The Step 1 baseline test fails and you cannot make it pass by fixing only the builder — that
  means current behavior differs from this plan's understanding of it.
- Any of the 5 existing `TestParseChapters_*` tests fails after Step 5.
- You find a real-world M4B (e.g. in a `testdata/` corpus added by plan 001) whose chapters
  were correct before and differ after — report the file and both outputs.
- A fix seems to require changing `parser.go`, `technical.go`, or `atoms.go`.

## Maintenance notes

- Plan 002 adds `CheckLen` guards at the `stsz`/`stco` `make` calls. After this plan the stsz
  `make` takes `n` (already bounded by `len(chapterTimes)`); 002's guard should still apply to
  the table branch only (the default-size branch reads no table). 002's maintenance note says
  "the stts cap interacts with plan 019 (stsc-aware sample mapping)" — that stsc work is this
  plan, not 019.
- Plan 003 will thread `ctx` into the table parsers; add `parseSampleToChunk` to its list.
- Plan 017 previously absorbed the shadowed-`readErr` finding (see `plans/README.md`
  "Findings considered and rejected"); this plan fixes it, so 017 must not.
- Reviewer: check `computeSampleOffsets` for the "stsc entry applies until the next
  `first_chunk`" rule and that no loop is bounded by an untrusted count alone.
- Deferred: `sample_description_index` (multiple text sample descriptions), `encd` boxes and
  UTF-16 (BOM-prefixed) chapter titles in text samples.
