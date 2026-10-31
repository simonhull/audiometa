# Plan 037: Read ID3v2.2 tags and fall back to ID3v1

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving on. If a
> STOP condition occurs, stop and report — do not improvise. When done, update
> the status row for this plan in `plans/README.md`.
>
> **Drift check (run first)**: `git diff --stat aca9750..HEAD -- internal/mp3/id3v2.go internal/mp3/parser.go internal/mp3/artwork.go internal/mp3/technical.go`
> Plans 026 and 027 are prerequisites and WILL appear in this diff (they edit
> `processFrame`, `parseTextFrame`, the CHAP functions, `decodeText`, `parseCommentFrame`).
> Compare only the functions excerpted below (`parseID3v2Header`, `parseID3v2`,
> `skipExtendedHeader`, `readSingleFrame`, `readFrameForArtwork`, `parseAPICFrame`,
> `Parse`, `parseTechnicalInfo`, `estimateCBRDuration`) against the live code; on a mismatch, STOP.

## Status

- **Priority**: P3
- **Effort**: M
- **Risk**: LOW (new code paths only run for files that today yield no tags; Step 1 changes only where the audio scan starts for unsupported tags)
- **Depends on**: plans/026-id3-audiobook-frames.md (`isTextFrameID`, test helpers), plans/027-id3-text-decoding.md (Latin-1-safe `decodeText`, `frameState`)
- **Category**: direction
- **Planned at**: commit `aca9750`, 2026-10-30

## Why this matters

Two classes of real-world MP3s come back with **no metadata at all**: files tagged with ID3v2.2
(older iTunes rips and some audiobook tools still write it) and files carrying only an ID3v1
trailer. dhowden/tag — the library the README credits as this project's inspiration — reads both,
so users migrating from it lose data. Worse, an unsupported ID3v2 version is demoted to a warning
with `tagSize = 0`, so the MPEG frame-sync scan starts at byte 0 and crawls through the whole tag,
including megabytes of artwork, where any `FF Fx` byte pair can be mistaken for an audio frame and
produce a wrong bitrate/duration. Finally, CBR duration counts a trailing 128-byte ID3v1 tag as
audio. This plan makes the scan always skip a recognised tag, adds ID3v2.2 frame reading on top of
the existing (fixed) frame dispatch, and adds an ID3v1 fallback.

## Current state

Files:
- `internal/mp3/id3v2.go` — ID3v2 header + frame reader and dispatch.
- `internal/mp3/parser.go` — `Parse`: runs `parseID3v2`, then `parseTechnicalInfo`.
- `internal/mp3/artwork.go` — lazy `ExtractArtwork` path; re-reads the header and walks frames for `APIC`.
- `internal/mp3/technical.go` — frame-sync scan and duration.

`internal/mp3/id3v2.go:83-115` — `parseID3v2Header` rejects everything but 3 and 4 and returns a
zero header on error:

```go
	header := ID3v2Header{
		Version: buf[3],
		Flags:   buf[5],
		Size:    decodeSynchsafe(buf[6:10]),
	}

	// Only support ID3v2.3 and ID3v2.4
	if header.Version != 3 && header.Version != 4 {
		return ID3v2Header{}, &types.UnsupportedFormatError{
			Path:   sr.Path(),
			Reason: fmt.Sprintf("unsupported ID3v2 version: 2.%d", header.Version),
		}
	}
```

`internal/mp3/id3v2.go:38-57` — `parseID3v2` returns `0, err` on header error and
`int64(10 + header.Size)` on success. **There is no footer handling**: an ID3v2.4 tag with header
flag `0x10` has a 10-byte footer after the frames that is not counted.

`internal/mp3/id3v2.go:118-141` — `skipExtendedHeader` tests `header.Flags&0x40`. In ID3v2.2 bit
`0x40` means *compression* (the whole tag is compressed and, per spec, must be ignored); there is
no extended header in v2.2.

`internal/mp3/id3v2.go:166-201` — `readSingleFrame` reads a 10-byte header
(`frameID := string(frameHeaderBuf[0:4])`, `decodeFrameSize(header.Version, frameHeaderBuf[4:8])`,
flags `[8:10]`), stops at a `0x00` first byte (padding). `internal/mp3/artwork.go:64-102`
`readFrameForArtwork` duplicates the same 10-byte header parsing (plus `frameSize == 0 ||
> 100 MiB` → stop, and reads the payload only for `"APIC"`).

`internal/mp3/parser.go:34-47`:

```go
	// Parse ID3v2 tag (if present)
	tagSize, err := parseID3v2(sr, file)
	if err != nil {
		// Not an ID3v2 file or parse error - try to find MP3 frames anyway
		file.Warnings = append(file.Warnings, types.Warning{
			Stage:   types.StageMetadata,
			Message: fmt.Sprintf("ID3v2 parsing failed: %v", err),
			Err:     err,
		})
		tagSize = 0
	}

	// Parse MP3 frame headers for technical info (bitrate, duration, etc.)
	if err := parseTechnicalInfo(sr, tagSize, size, file); err != nil {
```

`internal/mp3/technical.go:23-58` — `parseTechnicalInfo(sr, tagSize, fileSize, file)` scans
byte-by-byte from `tagSize` while `frameOffset < fileSize-4`; on CBR calls
`estimateCBRDuration(bitrate, fileSize, tagSize)` (:46). `parseVBRHeader` ignores its last two
params. `internal/mp3/technical.go:166-178`:

```go
func estimateCBRDuration(bitrate int, fileSize int64, tagSize int64) time.Duration {
	...
	// Audio data size (excluding ID3 tag)
	audioSize := fileSize - tagSize
```

So passing an "audio end" (file size minus a trailing ID3v1 tag) as `fileSize` fixes both the scan
bound and the CBR estimate.

`internal/mp3/artwork.go:112-180` — `parseAPICFrame(data)`: encoding byte, NUL-terminated MIME,
picture-type byte, encoding-terminated description, image bytes; already maps legacy MIME `"JPG"`/
`"PNG"` and sniffs magic bytes via `detectMIMEType`.

**Format reference (inline — the executor has not read the specs):**
- ID3v2.2 tag header: identical 10-byte layout to v2.3 (`"ID3"`, major `2`, revision, flags,
  synchsafe size). Flags: `0x80` unsynchronisation, `0x40` compression (ignore tag's frames).
- ID3v2.2 frame header: **6 bytes** — 3-char ID, 3-byte big-endian size, **no flags**. Text frames
  use the same body layout as v2.3 (encoding byte 0 or 1, then text). `COM` = `COMM` layout,
  `TXX` = `TXXX` layout.
- ID3v2.2 `PIC` body: `[encoding][image format, 3 bytes e.g. "JPG"/"PNG"][picture type][description + terminator][image data]` — no NUL-terminated MIME.
- ID3v2.2 → v2.3 ID mapping to use: `TT1→TIT1, TT2→TIT2, TT3→TIT3, TP1→TPE1, TP2→TPE2, TAL→TALB,
  TYE→TYER, TRK→TRCK, TPA→TPOS, TCO→TCON, TCM→TCOM, COM→COMM, TXX→TXXX, GP1→GRP1, MVN→MVNM,
  MVI→MVIN, PIC→APIC` (GP1/MVN/MVI are the iTunes v2.2 forms, as defined by mutagen).
- ID3v2.4 footer: present when header flag `0x10` is set (v2.4 only); 10 bytes after the frames.
- ID3v1 (last 128 bytes of the file): `"TAG"`(0-2) title(3-32, 30) artist(33-62, 30) album(63-92,
  30) year(93-96, 4) comment(97-126, 30) genre(127, 1). **v1.1**: if byte 125 is `0` and byte 126 is
  non-zero, byte 126 is the track number and the comment is bytes 97-124. Fields are padded with NUL
  or spaces; encoding is ISO-8859-1.

**Conventions**: gofmt; warnings `file.Warnings = append(file.Warnings, types.Warning{Stage:
types.StageMetadata, Message: ..., Err: ...})`; in `internal/mp3` the repo's `internal/binary` is
imported as `binutil` (alongside `encoding/binary`); reads via `sr.ReadAt(buf, off, "what")` on
`*binutil.SafeReader`. Go directive `go 1.26.0` — no newer APIs. Test helpers from plan 026 live
in `internal/mp3/id3_fixture_test.go`: `synchsafe`, `id3Frame`, `id3Tag`, `mpegFrame`
(`FF FB 90 00` = MPEG-1 Layer III 128 kbps 44.1 kHz + 4 zero bytes), `latin1Text`, `utf16Text`,
`parseBytes(t, data)`. Format detection (`internal/types/format.go:89-98`) already recognises both
`"ID3"`-prefixed files and bare frame-sync files as MP3 — no change needed there.

## Commands you will need

| Purpose | Command | Expected |
|---|---|---|
| Build | `go build ./...` | exit 0 |
| Vet | `go vet ./...` | exit 0 |
| Targeted | `go test ./internal/mp3/ -run 'TestParse_ID3v22|TestParse_ID3v1|TestParse_Unsupported|TestID3v2Header' -v` | all PASS |
| All tests | `go test ./...` and `go test -race ./...` | all pass |
| Lint | `mise exec -- golangci-lint run ./...` | `0 issues.` (must be via `mise exec --`) |
| Format | `gofmt -l .` | no output |

## Scope

**In scope**:
- `internal/mp3/id3v2.go` — header parsing, tag size, v2.2 frame header reading, ID mapping
- `internal/mp3/artwork.go` — v2.2 frame walk + `PIC` parsing
- `internal/mp3/parser.go` — keep tag size on unsupported versions; ID3v1 fallback; audio end
- `internal/mp3/id3v1.go` (create) — ID3v1 reader
- `internal/mp3/id3_v22_v1_test.go` (create)
- `internal/mp3/id3_fixture_test.go` — add v2.2/v1 helpers only

**Out of scope**:
- `internal/mp3/technical.go` — not edited; the audio-end fix is done by what `parser.go` passes.
  (Plans 004/012/013 rewrite parts of it.)
- ID3v1 genre byte → genre name, and TCON `"(17)"` resolution — deferred together (needs a shared
  192-entry genre table; a natural small follow-up).
- Enhanced `"TAG+"` (227 bytes before ID3v1), APEv2, Lyrics3 trailers — not handled; their bytes
  stay inside the "audio" region as today.
- ID3v2.2 tag-level unsynchronisation and compressed v2.2 tags — unsync belongs to plan 019;
  compressed v2.2 tags are skipped with a warning (Step 2).
- ID3v2.2 `CRM`/encrypted frames, `PIC` with `"-->"` link format beyond what `parseAPICFrame`
  already does.

## Git workflow

- Branch: `advisor/037-id3v22-and-id3v1`
- Commit per step; gitmoji single-line, no body, no attribution
  (e.g. `🐛 Skip unsupported ID3v2 tags before scanning for audio`, `✨ Read ID3v2.2 tags`, `✨ Fall back to ID3v1 tags`).
- Do NOT push or open a PR.

## Steps

### Step 1: Always skip a recognised ID3v2 tag (and its v2.4 footer)

1. Add a method on `ID3v2Header`:

```go
// totalSize returns the on-disk size of the tag: 10-byte header, body and,
// for ID3v2.4 with the footer flag (0x10), the 10-byte footer.
func (h ID3v2Header) totalSize() int64 {
	n := 10 + int64(h.Size)
	if h.Version == 4 && h.Flags&0x10 != 0 {
		n += 10
	}
	return n
}
```

2. In `parseID3v2Header`, on an unsupported version return the **populated** header together with
   the error (`return header, &types.UnsupportedFormatError{...}`). Magic/read failures still return
   `ID3v2Header{}`.
3. In `parseID3v2`: on header error, `if header.Version != 0 { return header.totalSize(), err }`,
   else `return 0, err`. On success return `header.totalSize()` instead of `int64(10 + header.Size)`.
4. In `parser.go` `Parse`, delete the `tagSize = 0` line in the error branch (`parseID3v2` now
   returns 0 itself when there is no tag). Keep the warning.

`extractArtwork` (`artwork.go:30`) ignores the header on error, so returning it is harmless there.

**Verify**: `go build ./...` → exit 0; `go test ./internal/mp3/` → pass. Add and run
`TestParse_UnsupportedID3VersionSkipsTag`: `id3Tag(5, <body of 40 zero bytes with FF FB 50 00 at
offset 20>)` + `mpegFrame` → `Audio.Bitrate == 128000` (the false sync inside the tag would give
64000) and a warning whose `Message` contains `"unsupported ID3v2 version"`. Also
`TestID3v2Header_TotalSize`: `{Version:4, Flags:0x10, Size:100}` → 120; `{Version:3, Flags:0x10,
Size:100}` → 110; `{Version:4, Size:0}` → 10.

### Step 2: Read ID3v2.2 frames through the existing dispatch

1. Accept version 2 in `parseID3v2Header` (`header.Version < 2 || header.Version > 4` → unsupported).
2. In `skipExtendedHeader`, return `10` immediately when `header.Version == 2`.
3. In `parseID3v2`, after the header: if `header.Version == 2 && header.Flags&0x40 != 0`, append a
   `StageMetadata` warning `"ID3v2.2 tag is compressed; frames ignored"` and
   `return header.totalSize(), nil` without walking frames.
4. Add the mapping table and a shared header reader in `id3v2.go`:

```go
// v22FrameIDs maps ID3v2.2 three-character frame IDs to their ID3v2.3 equivalents.
var v22FrameIDs = map[string]string{
	"TT1": "TIT1", "TT2": "TIT2", "TT3": "TIT3", "TP1": "TPE1", "TP2": "TPE2",
	"TAL": "TALB", "TYE": "TYER", "TRK": "TRCK", "TPA": "TPOS", "TCO": "TCON",
	"TCM": "TCOM", "COM": "COMM", "TXX": "TXXX", "GP1": "GRP1", "MVN": "MVNM",
	"MVI": "MVIN", "PIC": "APIC",
}

// readFrameHeader reads the frame header at offset for the tag's version and
// returns the (v2.3-normalised) frame ID, payload size, flags and header length.
// ok is false at padding or when the header cannot be read.
func readFrameHeader(sr *binutil.SafeReader, version byte, offset int64) (id string, size uint32, flags uint16, headerLen int64, ok bool)
```

   - v2.2: read 6 bytes; stop (`ok=false`) if `buf[0] == 0`; `raw := string(buf[0:3])`;
     `id = v22FrameIDs[raw]`, or `raw` itself if unmapped (it then matches no handler, except that
     unmapped `T**` IDs reach `parseTextFrame` and hit no case — harmless);
     `size = uint32(buf[3])<<16 | uint32(buf[4])<<8 | uint32(buf[5])`; `flags = 0`; `headerLen = 6`.
   - v2.3/2.4: the existing 10-byte logic (`decodeFrameSize`, flags from `[8:10]`), `headerLen = 10`.
5. Rewrite `readSingleFrame` and `readFrameForArtwork` to call `readFrameHeader` and use
   `headerLen` everywhere they currently hard-code `10` (payload offset `offset+headerLen`, return
   `headerLen + int64(size)`). Keep every other behaviour (warnings, the artwork size sanity check,
   APIC-only payload reads, and plan 007's `isMetadataFrame` skip / plan 019's flag handling if they
   have landed — apply them to the normalised `id`).

**Verify**: `go build ./...` → exit 0; `go test ./internal/mp3/` → all existing tests pass (v2.3/2.4
paths are byte-for-byte equivalent).

### Step 3: Parse v2.2 `PIC` artwork

In `artwork.go`:
1. Split `parseAPICFrame` at the point after the MIME type is parsed: move everything from
   `if pos >= len(data) { return types.Artwork{}, errAPICTruncated }` to the end into
   `parsePictureBody(data []byte, pos int, encoding byte, mimeType string) (types.Artwork, error)`;
   `parseAPICFrame` calls it. Behaviour unchanged.
2. Add:

```go
// parsePICFrame parses an ID3v2.2 PIC frame:
// [encoding][image format(3), e.g. "JPG"][picture type][description\0][data].
func parsePICFrame(data []byte) (types.Artwork, error) {
	if len(data) < 6 {
		return types.Artwork{}, errAPICTooShort
	}
	mimeType := mimeJPEG // default; magic-byte sniffing in parsePictureBody overrides it
	if strings.EqualFold(string(data[1:4]), "PNG") {
		mimeType = mimePNG
	}
	return parsePictureBody(data, 4, data[0], mimeType)
}
```

(`artwork.go` will need `strings` added to its imports.)

3. In `extractArtwork`, for frames with ID `"APIC"` call `parsePICFrame` when
   `header.Version == 2`, else `parseAPICFrame`.

**Verify**: `go test ./internal/mp3/` → pass.

### Step 4: ID3v1 fallback and audio end

Create `internal/mp3/id3v1.go`:

```go
const id3v1Size = 128

// id3v1Tag holds the text fields of an ID3v1 / ID3v1.1 trailer.
type id3v1Tag struct {
	Title, Artist, Album, Comment string
	Year, Track                   int
}

// readID3v1 reads the ID3v1 trailer from the last 128 bytes of the file.
func readID3v1(sr *binutil.SafeReader, size int64) (id3v1Tag, bool)

// id3v1String decodes a fixed-width ID3v1 field: cut at the first NUL,
// decode as ISO-8859-1 (via decodeText encoding 0), trim surrounding spaces.
func id3v1String(b []byte) string

// applyID3v1 copies ID3v1 fields into tags that are still empty.
func applyID3v1(file *types.File, tag id3v1Tag)
```

- `readID3v1`: `size < id3v1Size` → false; `sr.ReadAt` the 128 bytes at `size-id3v1Size`; not
  `"TAG"` → false. Fill per the layout in "Current state"; `Year = parseYear(id3v1String(buf[93:97]))`;
  v1.1 when `buf[125] == 0 && buf[126] != 0` (`Track = int(buf[126])`, comment `buf[97:125]`),
  else comment `buf[97:127]`. Ignore the genre byte (deferred).
- `applyID3v1`: set `Title`, `Artist`, `Album`, `Comment` when empty, `Year` when 0, `TrackNumber`
  when 0.

In `parser.go` `Parse`, after the ID3v2 block and before `parseTechnicalInfo`:

```go
	v1, hasV1 := readID3v1(sr, size)
	if hasV1 && file.Tags.Title == "" && file.Tags.Artist == "" {
		applyID3v1(file, v1)
	}
	audioEnd := size
	if hasV1 {
		audioEnd -= id3v1Size
	}
```

and pass `audioEnd` instead of `size` to `parseTechnicalInfo`. (Its `fileSize` parameter is only
used as the scan bound and in `estimateCBRDuration` — both should exclude the trailer. The
`SafeReader` still knows the real size.)

**Verify**: `go build ./...` → exit 0; `go test ./internal/mp3/` → pass.

### Step 5: Tests

Add to `internal/mp3/id3_fixture_test.go`:
- `id3v22Frame(id string, payload []byte) []byte` — 3-char ID + 3-byte BE size + payload.
- `id3v1Trailer(title, artist, album, year, comment string, track byte) []byte` — 128 bytes,
  fields NUL-padded; `track == 0` writes a v1.0 30-byte comment, else v1.1 layout.

Create `internal/mp3/id3_v22_v1_test.go`:
1. `TestParse_ID3v22`: `id3Tag(2, id3v22Frame("TT2", latin1Text("Title")), id3v22Frame("TP1",
   utf16Text("Artist")), id3v22Frame("TAL", latin1Text("Alb\xe9")), id3v22Frame("TRK",
   latin1Text("3/10")), id3v22Frame("MVN", latin1Text("Saga")), id3v22Frame("COM",
   []byte("\x00eng\x00note")), id3v22Frame("PIC", <0x00 "JPG" 0x03 0x00 + FF D8 FF E0 ... + FF FB 50 00 ...>))`
   + `mpegFrame` → Title `"Title"`, Artist `"Artist"`, Album `"Albé"`, TrackNumber 3, TrackTotal
   10, Series `"Saga"`, Comment `"note"`, `Audio.Bitrate == 128000` (proves the scan starts after
   the tag, not at the false sync inside PIC), and no warning mentioning `"unsupported"`.
2. `TestExtractArtwork_ID3v22`: same bytes → `(&parser{}).ExtractArtwork(...)` returns one artwork
   with `MIMEType == "image/jpeg"` and `Type == types.ArtworkType(3)`.
3. `TestParse_ID3v22Compressed`: v2.2 tag with flag `0x40` → no Title, one warning containing
   `"compressed"`, `Audio.Bitrate == 128000`.
4. `TestParse_ID3v1Only`: `mpegFrame` + 15992 zero bytes (16000 bytes of audio) +
   `id3v1Trailer("T\xe9", "Artist", "Album", "1999", "c", 0)` → Title `"Té"`, Artist, Album, Year
   1999, Comment `"c"`, TrackNumber 0, and `Audio.Duration == time.Second` exactly (16000×8 /
   128000; it was 1.008 s before Step 4).
5. `TestParse_ID3v11Track`: same with track 7 → `TrackNumber == 7`, comment intact.
6. `TestParse_ID3v1IgnoredWhenV2HasTags`: `id3Tag(3, id3Frame(3, "TIT2", latin1Text("V2")))` +
   audio + `id3v1Trailer("V1", "V1Artist", ...)` → Title `"V2"`, Artist `""` (v1 not merged).

**Verify**: `go test ./internal/mp3/ -run 'TestParse_ID3v22|TestExtractArtwork_ID3v22|TestParse_ID3v1|TestParse_Unsupported|TestID3v2Header' -v`
→ all PASS; `go test -race ./...` → all pass.

## Test plan

Step 1 and Step 5 tests above: unsupported-version tag skip with a planted false sync, footer size
arithmetic, v2.2 text/COM/MVN/PIC through full Parse and ExtractArtwork, compressed v2.2, v1-only,
v1.1 track, v1 not merged over v2, CBR duration excluding the trailer. Pattern: helpers in
`internal/mp3/id3_fixture_test.go`; always through `parseBytes` / `ExtractArtwork`.

## Done criteria

- [ ] `go build ./...`, `go vet ./...` exit 0; `gofmt -l .` prints nothing
- [ ] `go test -race ./...` exits 0 including all tests named in Step 1 and Step 5
- [ ] `mise exec -- golangci-lint run ./...` → `0 issues.`
- [ ] `grep -n 'tagSize = 0' internal/mp3/parser.go` → no match
- [ ] `grep -n 'readFrameHeader(sr' internal/mp3/id3v2.go internal/mp3/artwork.go` → three matches: the definition, one call in `readSingleFrame`, one in `readFrameForArtwork`
- [ ] `git status` shows only in-scope files changed/created
- [ ] `plans/README.md` status row for 037 updated (add the row if absent)

## STOP conditions

- Plan 026 or 027 has not landed (`isTextFrameID`, `decodeSingleByteText` or
  `internal/mp3/id3_fixture_test.go` missing) — STOP; execute them first.
- Plan 004/012/013 changed `parseTechnicalInfo`'s signature or the meaning of its `fileSize`
  parameter — STOP and report; the audio-end fix must be re-targeted.
- Plan 019 has landed and its unsync handling assumes 10-byte frame headers in a way that
  `readFrameHeader` breaks — STOP and reconcile rather than special-casing.
- Any existing test changes `Audio.Duration` or `Audio.Bitrate` for a fixture without an ID3v1
  trailer or unsupported tag — STOP; Step 1/4 should be no-ops for those.

## Maintenance notes

- The v2.2 → v2.3 ID table is the only place v2.2 knowledge lives; extend it (e.g. `TCP`→`TCMP`,
  `TOA`, `TEN`) if a consumer needs more fields. Unmapped IDs are dropped, as unknown v2.3 IDs are.
- Deferred: genre names for the ID3v1 genre byte and TCON `"(n)"` references — add one shared
  table and use it in both places. `TAG+` / APEv2 / Lyrics3 trailers still count as audio bytes in
  the CBR estimate.
- Reviewer: check that every place that advanced by `10 + size` now uses `headerLen + size`, that a
  tag whose declared size exceeds the file simply yields a "no valid MP3 frame found" warning (no
  panic, no huge allocation), and that the ID3v1 fallback only fires when ID3v2 gave neither title
  nor artist.
