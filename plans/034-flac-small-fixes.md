# Plan 034: Fix FLAC artwork loop, bitrate and picture types

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving on. If a
> STOP condition occurs, stop and report — do not improvise. When done, update
> the status row for this plan in `plans/README.md`.
>
> **Drift check (run first)**: `git diff --stat aca9750..HEAD -- internal/flac/parser.go internal/ogg/artwork.go internal/types/artwork.go internal/flac/ internal/ogg/artwork_test.go`
> If any file changed, compare the "Current state" excerpts against the live code (compare by
> content — plan 024 legitimately shifts line numbers in `internal/flac/parser.go`). On a content
> mismatch in the excerpted regions, STOP.

## Status

- **Priority**: P3
- **Effort**: S
- **Risk**: LOW (four independent, local fixes; observable: smaller FLAC bitrate values, richer
  artwork types, one new warning)
- **Depends on**: none logically. If plan 024 is pending, run it first (both edit
  `internal/flac/parser.go`; 024 also creates `internal/flac/fixtures_test.go`, reused here).
- **Category**: bug
- **Planned at**: commit `aca9750`, 2026-10-30

## Why this matters

Four small FLAC defects, each producing wrong data silently:

1. `ExtractArtwork` skips the "last metadata block" check when a PICTURE fails to parse, so a bad
   final PICTURE makes it walk into audio frames, decoding them as block headers (garbage or bogus
   "pictures").
2. Bitrate is `file size × 8 / duration`, and file size includes metadata — audiobook FLACs often
   carry multi-MB cover art, inflating bitrate.
3. STREAMINFO `total samples == 0` means "unknown" per spec; Duration is then 0 with no warning, so
   consumers can't distinguish "unknown" from "empty".
4. Picture types other than 3/4 collapse to `ArtworkOther`, though `types.ArtworkType` already uses
   the exact ID3v2/FLAC numbering 0–20 (MP3 already casts directly). The same lossy mapping is
   duplicated in the Ogg `METADATA_BLOCK_PICTURE` decoder; this plan fixes both via one helper
   (decision: fix Ogg here — it's the same two-line bug; plan 016 later dedupes the parsers).

## Current state

Files:
- `internal/flac/parser.go` — FLAC `Parse`, `ExtractArtwork`, `parseStreamInfo`, `parsePicture`.
- `internal/ogg/artwork.go` — `parseMetadataBlockPicture` (base64 FLAC picture block).
- `internal/types/artwork.go` — `ArtworkType` constants 0–20.

### (1) `internal/flac/parser.go:180-210` — ExtractArtwork loop (verified)

```go
	for offset < size {
		header, err := binary.Read[uint32](sr, offset, "metadata block header")
		if err != nil {
			break
		}
		isLast := (header >> 31) == 1
		blockType := uint8((header >> 24) & 0x7F)
		blockLength := int64(header & 0x00FFFFFF)
		offset += 4
		if blockType == blockTypePicture {
			pic, err := parsePicture(sr, offset, blockLength)
			if err != nil {
				// Skip this picture but continue
				offset += blockLength
				continue                 // <-- bypasses the isLast check below
			}
			artwork = append(artwork, pic)
		}
		offset += blockLength
		if isLast {
			break
		}
	}
```

Note `parsePicture` ignores the block length (`func parsePicture(sr *binary.SafeReader, offset, _ int64)`)
and reads MIME/description/data lengths from the payload.

### (2) and (3) `internal/flac/parser.go:244-262` — in `parseStreamInfo` (verified)

```go
	// Calculate duration
	if sampleRate > 0 {
		durationSeconds := float64(totalSamples) / float64(sampleRate)
		file.Audio.Duration = time.Duration(durationSeconds * float64(time.Second))
	}
	...
	// Calculate approximate bitrate (FLAC is variable bitrate)
	// Use file size and duration for a rough estimate
	if file.Audio.Duration > 0 {
		durationSeconds := file.Audio.Duration.Seconds()
		bitsPerSecond := (float64(file.Size) * 8) / durationSeconds
		file.Audio.Bitrate = int(bitsPerSecond)
	}
```

`totalSamples` is `packed & 0xFFFFFFFFF` (line 242). `parseStreamInfo` receives `offset` (start of
the block payload) — use it as the warning `Offset`.

The metadata loop in `Parse` (`parser.go:64-142`) ends with:

```go
		// Move to next block
		offset += blockLength

		// If this was the last metadata block, we're done
		if isLast {
			break
		}
	}
```

When `isLast` is hit, `offset` is exactly the first byte of audio frames.

### (4) `internal/flac/parser.go:390-399` and `internal/ogg/artwork.go:143-152` (verified, identical)

```go
	// Map FLAC picture type to types.ArtworkType
	var artType types.ArtworkType
	switch pictureType {
	case 3:
		artType = types.ArtworkFrontCover
	case 4:
		artType = types.ArtworkBackCover
	default:
		artType = types.ArtworkOther
	}
```

`pictureType` is a `uint32` in both. `internal/types/artwork.go:26-69`: `ArtworkOther ArtworkType = iota`
(0) … `ArtworkPublisherLogotype` (20) — the ID3v2 APIC / FLAC PICTURE numbering. MP3 does
`Type: types.ArtworkType(pictureType)` (`internal/mp3/artwork.go:176`).

### Conventions

- Warnings: `file.Warnings = append(file.Warnings, types.Warning{Stage: types.StageTechnical, Message: ..., Offset: offset})`.
  Note `WithStrictParsing()` fails on **any** warning (`file.go:100-104`).
- Lint (`.golangci.yml`): `gocyclo` 15, `nestif` 5; `Parse` already has `//nolint:gocyclo`.
  `revive` `exported` rule requires a doc comment on new exported identifiers; `godot` requires
  comments to end with a period.
- Test fixtures: synthetic bytes (see `createMinimalFLAC`, `internal/flac/parser_test.go:15-105`;
  `createTestPictureBlock`, `internal/ogg/artwork_test.go:118`). `Parse`/`ExtractArtwork` take an
  `io.ReaderAt`, so tests can pass `bytes.NewReader(data)`.

## Commands you will need

| Purpose | Command | Expected |
|---|---|---|
| Build | `go build ./...` | exit 0 |
| Vet | `go vet ./...` | exit 0 |
| Targeted | `go test ./internal/flac/ ./internal/ogg/ ./internal/types/ -v -run 'Artwork\|Picture\|Bitrate\|TotalSamples'` | all pass |
| All tests | `go test ./...` and `go test -race ./...` | all pass |
| Lint | `mise exec -- golangci-lint run ./...` | `0 issues.` |
| Format | `gofmt -l .` | no output |

Always run golangci-lint via `mise exec --` (pinned 2.15.0). `go.mod` is `go 1.26.0`.

## Scope

**In scope**:
- `internal/flac/parser.go` — `ExtractArtwork` loop, `parseStreamInfo`, bitrate placement in
  `Parse`, picture-type mapping in `parsePicture`
- `internal/ogg/artwork.go` — picture-type mapping only
- `internal/types/artwork.go` — add one helper function
- `internal/flac/fixtures_test.go` (create only if absent), `internal/flac/artwork_test.go` (create)
- `internal/ogg/artwork_test.go`, `internal/types/artwork_test.go` (create)

**Out of scope**:
- Allocation caps on `make([]byte, mimeLength/descLength/dataLength)` — plan 002.
- `WithMaxArtworkSize` enforcement — plan 008.
- Deduplicating the FLAC/Ogg picture parsers — plan 016.
- `internal/mp3/artwork.go` — its direct cast of a byte (0–255) is not range-checked; leave for a
  follow-up (note it in your report).
- Chapters / CUESHEET / VORBIS_COMMENT handling — plan 024.
- The Opus bitrate heuristic (`estimateOpusBitrate`) and Vorbis nominal bitrate.

## Git workflow

- Branch: `advisor/034-flac-small-fixes`
- One commit per fix, gitmoji single line, no body (e.g. `🐛 Stop FLAC artwork scan at last metadata block`,
  `🐛 Exclude metadata from FLAC bitrate`, `🐛 Warn on unknown FLAC total samples`,
  `🐛 Preserve all FLAC picture types`).
- Do NOT push or open a PR unless instructed.

## Steps

### Step 1: Always honor `isLast` in `ExtractArtwork`

Replace the PICTURE branch so a failed parse falls through to the shared advance/`isLast` logic:

```go
		if blockType == blockTypePicture {
			// An unparseable picture is skipped; the block chain is still followed normally.
			if pic, err := parsePicture(sr, offset, blockLength); err == nil {
				artwork = append(artwork, pic)
			}
		}

		offset += blockLength

		if isLast {
			break
		}
```

**Verify**: `go test ./internal/flac/` → pass.

### Step 2: Compute bitrate from audio bytes only

1. Delete the bitrate block (`// Calculate approximate bitrate ...` through its closing `}`) from
   `parseStreamInfo`.
2. In `Parse`, declare `audioStart := int64(-1)` before the block loop, and change the loop tail to:
   ```go
		if isLast {
			audioStart = offset // first byte after the metadata blocks = first audio frame
			break
		}
   ```
3. After the loop (before `// Set container and codec`; if plan 024 landed, after its
   `ParseChapters` block), add:
   ```go
	// Approximate bitrate from audio frames only (metadata such as cover art excluded).
	// Unknown when the metadata chain never reached its last block.
	if audioStart > 0 && audioStart < size && file.Audio.Duration > 0 {
		file.Audio.Bitrate = int(float64(size-audioStart) * 8 / file.Audio.Duration.Seconds())
	}
   ```

**Verify**: `go test ./internal/flac/` → pass (no existing test asserts FLAC bitrate — verified).

### Step 3: Warn when total samples is unknown

In `parseStreamInfo`, after the duration block:

```go
	if totalSamples == 0 {
		file.Warnings = append(file.Warnings, types.Warning{
			Stage:   types.StageTechnical,
			Message: "STREAMINFO total samples is 0 (unknown); duration not available",
			Offset:  offset,
		})
	}
```

**Verify**: `go test ./internal/flac/` → pass.

### Step 4: One helper for picture-type mapping, used by FLAC and Ogg

In `internal/types/artwork.go`, after the const block:

```go
// ArtworkTypeFromPictureType maps an ID3v2 APIC / FLAC PICTURE type code to an ArtworkType.
// Codes 0–20 map one-to-one; anything else is ArtworkOther.
func ArtworkTypeFromPictureType(code uint32) ArtworkType {
	if code > uint32(ArtworkPublisherLogotype) {
		return ArtworkOther
	}
	return ArtworkType(code)
}
```

Replace the 9-line `var artType ... switch ... }` block in **both** `internal/flac/parser.go`
(`parsePicture`) and `internal/ogg/artwork.go` (`parseMetadataBlockPicture`) with
`artType := types.ArtworkTypeFromPictureType(pictureType)` (keep the `// Map FLAC picture type to
types.ArtworkType` comment).

Do **not** run `go generate` — `artworktype_string.go` is unaffected (no new constants).

**Verify**: `go build ./...` → exit 0; `go test ./internal/ogg/ ./internal/flac/` → pass (existing
`TestParseMetadataBlockPicture_OtherType` uses type 0 → still `ArtworkOther`).

### Step 5: Tests, then full gates

Add the tests below.

**Verify**: `go test -race ./...` → all pass; `mise exec -- golangci-lint run ./...` → `0 issues.`;
`go vet ./...` → exit 0; `gofmt -l .` → no output.

## Test plan

**Fixtures.** Run `grep -n "func flacBlock" internal/flac/*_test.go`. If it exists (plan 024
landed), reuse `flacBlock`, `streamInfoPayload`, `vorbisCommentPayload`, `buildFLAC`. Otherwise
create `internal/flac/fixtures_test.go` (package `flac`; imports `bytes`, `encoding/binary`) with:

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
	binary.Write(buf, binary.BigEndian, uint16(4096))
	binary.Write(buf, binary.BigEndian, uint16(4096))
	buf.Write(make([]byte, 6))
	packed := sampleRate<<44 | uint64(1)<<41 | uint64(15)<<36 | totalSamples
	binary.Write(buf, binary.BigEndian, packed)
	buf.Write(make([]byte, 16))
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

Add to `internal/flac/artwork_test.go`:

```go
// picturePayload returns a FLAC PICTURE block body.
func picturePayload(picType uint32, mime string, data []byte) []byte {
	buf := &bytes.Buffer{}
	for _, v := range []uint32{picType, uint32(len(mime))} {
		binary.Write(buf, binary.BigEndian, v)
	}
	buf.WriteString(mime)
	// description length, width, height, color depth, colors used, data length
	for _, v := range []uint32{0, 1, 1, 24, 0, uint32(len(data))} {
		binary.Write(buf, binary.BigEndian, v)
	}
	buf.Write(data)
	return buf.Bytes()
}
```

Call the parser as `(&parser{}).ExtractArtwork(context.Background(), bytes.NewReader(data), int64(len(data)), "t.flac")`
/ `.Parse(...)`.

Tests (`internal/flac/artwork_test.go` unless noted):
1. `TestExtractArtwork_BadLastPictureStopsScan` — build:
   ```go
   bad := make([]byte, 8)
   binary.BigEndian.PutUint32(bad[0:], 3)
   binary.BigEndian.PutUint32(bad[4:], 4096) // MIME length runs past EOF → parsePicture fails
   data := buildFLAC(
   	flacBlock(false, blockTypeStreamInfo, streamInfoPayload(44100, 44100)),
   	flacBlock(true, blockTypePicture, bad),
   	// "audio" that happens to look like a valid PICTURE block:
   	flacBlock(true, blockTypePicture, picturePayload(3, "image/png", []byte{1, 2, 3})),
   )
   ```
   Expect `len(artwork) == 0`, no error. (Before Step 1 this returned 1 artwork — the regression.)
2. `TestExtractArtwork_AllPictureTypes` — STREAMINFO, PICTURE type 3, PICTURE type 8 (last) → 2
   artworks, `Type` `ArtworkFrontCover` and `ArtworkArtist`.
3. `TestParse_BitrateExcludesMetadata` — STREAMINFO `(44100, 441000)` (10 s), PADDING block
   (`blockTypePadding`, 50 000 zero bytes, not last), VORBIS_COMMENT (last, `TITLE=x`), then
   10 000 trailing zero bytes as audio. Expect `file.Audio.Bitrate == 8000`
   (10 000 B × 8 / 10 s). Old code gave > 48 000.
4. `TestParse_UnknownTotalSamplesWarns` — STREAMINFO `(44100, 0)` (last) → `Duration == 0` and
   exactly one warning with `Stage == types.StageTechnical` whose `Message` contains `"total samples"`.
   Also assert the existing 1-second fixture (`createMinimalFLAC`) produces **no** warnings.
5. `internal/ogg/artwork_test.go` → `TestParseMetadataBlockPicture_ExtendedTypes` — table using the
   existing `createTestPictureBlock`: type 6 → `ArtworkMedia`, 20 → `ArtworkPublisherLogotype`,
   21 → `ArtworkOther`, `0xFFFFFFFF` → `ArtworkOther`.
6. `internal/types/artwork_test.go` (create, package `types`) → `TestArtworkTypeFromPictureType`:
   0 → `ArtworkOther`, 3 → `ArtworkFrontCover`, 4 → `ArtworkBackCover`, 20 →
   `ArtworkPublisherLogotype`, 21 → `ArtworkOther`.

## Done criteria

- [ ] `go build ./...`, `go vet ./...` exit 0; `gofmt -l .` prints nothing
- [ ] `mise exec -- golangci-lint run ./...` → `0 issues.`
- [ ] `go test -race ./...` passes including the 6 new tests
- [ ] `grep -n "file.Size" internal/flac/parser.go` → no match in the bitrate computation
- [ ] `grep -n "case 3:" internal/flac/parser.go internal/ogg/artwork.go` → no match
- [ ] `git status` shows only in-scope files modified/created
- [ ] `plans/README.md` status row for 034 updated

## STOP conditions

- An existing test (anywhere, including root `audiometa_test.go`) asserts a FLAC bitrate computed
  from total file size, or asserts no warnings for a STREAMINFO with total samples 0 — report it.
- `fixtures_test.go` exists but its helpers have different signatures from those above — STOP (don't
  create duplicates; a duplicate function name won't compile).
- Plan 002 has landed and `parsePicture` now rejects the oversized MIME length **before** reading
  (still an error → test 1 still valid); if instead it clamps and *succeeds*, test 1's premise breaks
  — STOP and report.
- A verification fails twice after a reasonable fix attempt.

## Maintenance notes

- The new warning in Step 3 makes `WithStrictParsing()` reject FLACs with unknown total samples —
  intended (duration genuinely unavailable). If plan 015 introduces warning severities, classify
  this one as a real (non-informational) warning.
- `internal/mp3/artwork.go:176` should adopt `types.ArtworkTypeFromPictureType(uint32(pictureType))`
  in a follow-up so out-of-range APIC bytes (21–255) also map to `ArtworkOther`.
- Plan 016 should route both picture parsers through one implementation and keep using the helper.
- Bitrate is still an average including frame headers — fine for display; it is not a nominal rate.
- Reviewer: check that the `isLast` path is the only place `audioStart` is set and that the
  ExtractArtwork loop has a single `offset += blockLength`.
