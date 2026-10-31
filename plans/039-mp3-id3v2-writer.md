# Plan 039: Write MP3 metadata (ID3v2 frame-list writer with byte-preserving re-emit)

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report. Do not improvise. When done, update the status row for this plan
> in `plans/README.md`, unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Precondition**: plan 038 is DONE, which means the **maintainer signed off on the API**.
> Check that `mise exec -- go doc . File.WriteTo` prints a signature and that
> `ls internal/registry/writer.go internal/writeutil/writeutil.go` succeeds. If either
> fails, or `plans/README.md` still shows 038 as `awaiting API sign-off`, STOP.
>
> **Drift check (run first)**: `git diff --stat 0d60738..HEAD -- internal/mp3/ internal/testcorpus/generate.sh audiometa_corpus_test.go write_roundtrip_test.go example_write_test.go`
> Expected drift:
> - plan 038 created `write_roundtrip_test.go` and `example_write_test.go`;
> - **plan 016** moved series and narrator fallbacks out of `internal/mp3/id3v2.go` and
>   `parser.go` into `internal/parsing/fallbacks.go`. A **no-inference pass** then deletes those
>   fallbacks (`ResolveSeriesFallbacks`, `ApplyNarratorFallback`) entirely; do not rely on them.
>   Plan 016 also replaced `detectMIMEType` and
>   `detectImageDimensions` in `internal/mp3/artwork.go` with `internal/imagemeta`;
> - a **Go 1.27 idioms pass** may have turned `binary.Read[T](sr, off, what)` into
>   `sr.Read[T](off, what)`.
>
> Write new code in whatever form is live (`grep -n 'Read\[' internal/binary/*.go`). Beyond that,
> compare the "Current state" excerpts. A mismatch in the ID3 header, frame-header or flag code
> is a STOP.

## Status

- **Priority**: P2
- **Effort**: L
- **Risk**: HIGH. This is the first code that produces audio files users keep. It is mitigated by
  three things: the output is a new stream (the original is read-only), the audio region is
  copied verbatim and digest-checked, and every untouched frame is re-emitted byte-for-byte.
- **Depends on**: plans/038-write-api-foundations.md (DONE, with maintainer API sign-off)
- **Category**: direction
- **Planned at**: commit `0d60738` (branch `fix/improve-plans`), 2026-10-31

## Why this matters

MP3 is the first format the maintainer approved for writing (`docs/design/020-write-support-decision.md`,
"Decision"). It has the lowest structural risk: the ID3v2 tag sits at the start of the file and no
offset tables point into the audio. That makes it the right place to prove three things: the
frame-list model, the "re-encode only what was edited" rule, and the round-trip harness that the
FLAC and M4A writers will reuse. The cost of getting it wrong is a user's only copy of an
audiobook, so the rules below are strict. When something can't be represented safely, the writer
refuses with a typed error instead of guessing.

## Current state

Excerpts are from `0d60738`.

**The reader decodes frames and keeps no bytes.** `internal/mp3/id3v2.go:30-36`:

```go
type ID3v2Frame struct {
	ID    string
	Data  []byte
	Size  uint32
	Flags uint16
}
```

`readSingleFrame` (`:313-359`) skips the payload of every frame `processFrame` does not consume
(`isMetadataFrame`, `:430-432`). So a separate **writer-side frame list** is needed. Do not modify
the reader to keep bytes.

**Header and totals** (`:22-28`, `:40-46`):

```go
type ID3v2Header struct {
	Version      byte // Major version (3 or 4)
	Revision     byte // Minor version
	Flags        byte
	Size         uint32 // Tag size (excluding header), synchsafe
	ExtendedSize uint32 // Extended header size if present
}

func (h ID3v2Header) totalSize() int64 {
	n := 10 + int64(h.Size)
	if h.Version == 4 && h.Flags&0x10 != 0 {
		n += 10
	}
	return n
}
```

`parseID3v2Header` (`:223-256`) accepts versions 2–4 and returns the populated header together
with the error for other versions. `skipExtendedHeader` (`:259-283`) handles the v2.3 and v2.4
extended header sizes.

**Flag constants** (`:100-116`): `flagUnsync = 0x80` (tag header), `flagV22Compression = 0x40`,
v2.4 frame flags `v24FrameGrouping 0x0040`, `v24FrameCompression 0x0008`,
`v24FrameEncryption 0x0004`, `v24FrameUnsync 0x0002`, `v24FrameDataLength 0x0001`, v2.3 frame
flags `v23FrameCompression 0x0080`, `v23FrameEncryption 0x0040`, `v23FrameGrouping 0x0020`.
`deunsync` (`:119-128`) and `deunsyncTag` (`:133-153`) exist. `frameBody` (`:159-197`) shows
how flags are interpreted. Reuse these helpers.

**Frame headers** (`:382-410`): v2.2 headers are 6 bytes (3-char ID and a 3-byte size), and the
ID is mapped through `v22FrameIDs` (`:372-377`). v2.3/v2.4 headers are 10 bytes. A frame-ID first
byte of `0` means padding. `decodeFrameSize` is synchsafe for v2.4 and plain big-endian
otherwise.

**Which raw key is which frame.** `parseTextFrame` (`:468-528`) stores `AddRaw(frame.ID, v)` for
every `isTextFrameID` frame (`T***` except `TXXX`, plus `GRP1`, `MVNM` and `MVIN`).
`parseTXXXFrame` (`:584-608`) stores `AddRaw(strings.ToUpper(description), value)`. **Only the
first value is kept** (`firstValue`). `parseCommentFrame` (`:615-640`) sets `Tags.Comment` from the
first COMM with an empty description and does not touch `raw`.

**Text helpers**: `decodeText(data, encoding)` (`:737-748`), `findNullTerminator` (`:835-851`),
`terminatorSize` (`:854-863`), `splitTextValues` (`:769-777`, splits on NUL and drops a BOM per
value).

**ID3v1 and the audio region** (`internal/mp3/parser.go:48-56`):

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

`readID3v1` (`internal/mp3/id3v1.go:301-326`) decodes the 128-byte trailer. It uses `buf[93:97]`
for the year, `buf[125]==0 && buf[126]!=0` for ID3v1.1 (28-byte comment plus track), and ignores
the genre byte `buf[127]`.

**APIC layout** (`internal/mp3/artwork.go:113-141`):
`[enc][MIME latin1 \0][picture type][description \0 per encoding][data]`. v2.2 PIC
(`:191-200`) is `[enc][3-char format][type][desc \0][data]`.

**Test builders already exist** (`internal/mp3/id3_fixture_test.go`): `synchsafe(n)`,
`id3Frame(version, id, payload)`, `id3Tag(version, frames...)` (no padding, no flags),
`mpegFrame`, `latin1Text`, `utf16Text`, `chapFrameBody`, `parseBytes`, `id3v22Frame`,
`id3v1Trailer`. Reuse them, and extend them in a new `_test.go` file instead of editing that
file.

**Foundations from plan 038** (read them before you start):
- `internal/types/edits.go`: `EditSet{Keys []KeyEdit; Fields []FieldEdit; Artwork []ArtworkOp; Orig, Final *Tags}`.
- `internal/types/fieldmap.go`: `FieldBindings(types.FormatMP3)`.
- `internal/types/write_errors.go`: `ErrUnsupportedWrite`, `ErrDRMProtected`, `ErrTagTooLarge`.
- `internal/registry/writer.go`: `TagWriter{WriteFile, AudioDigest}`, `InPlaceWriter{InPlacePatch}`, `RegisterWriter`.
- `internal/writeutil`: `CopyRange`, `DigestRange`, `CountingWriter`.
- `write_roundtrip_test.go`: `roundTripCorpus` (MP3 rows `writable: false`), `roundTripEdits`, `roundTripArtworkEdits`.

**Corpus**: `testdata/sample.mp3` is ID3v2.3 with APIC and CHAP/CTOC
(`internal/testcorpus/generate.sh`, "MP3, ID3v2.3, cover art and chapters"). Its header bytes
are `ID3\x03\x00\x00`. No v2.4 or ID3v1 fixture exists yet.

## Decisions (binding)

**W0. No cross-field inference (maintainer rule, 2026-10-31).** In the maintainer's words: "our
job is to do the hard part of reading the tags and passing them up. The above application
decides what to do with them." The writer never derives or fills one field from another, so only
the frames representing fields the caller set or cleared change. In particular:
- writing Narrator never touches `TCOM`;
- writing Series or SeriesPart never touches `GRP1`, `TIT1`, `TIT2` or `TALB`;
- writing Title never touches the ID3v1 trailer (W12).

Every rule below is subject to W0.

**W1. Output version.** v2.3 input produces v2.3 output, and v2.4 input produces v2.4. A v2.2
input, or a file with no ID3v2 tag, produces v2.4. An unsupported version (2.0, 2.1, 2.5+) or a
v2.2 tag with the compression flag returns `ErrUnsupportedWrite`.

**W2. Frame list.** The writer parses the tag into
`[]wframe{id string; flags uint16; payload []byte; edited bool}`. `payload` holds the on-disk
bytes after the frame header. The order is the file's order. The walk mirrors `readFrameHeader`:
it stops at a zero first byte (padding) or at the tag end. A frame whose declared size overruns
the tag end returns `*types.CorruptedFileError`, because preserving it is impossible. Every frame
is kept, including unknown IDs, PRIV, CHAP, CTOC and APIC.

**W3. Re-emit rules for a frame that was not edited.**
- When the output version equals the input version, re-emit the header (same ID, same size
  encoding, same flags) and the payload byte-for-byte. Compressed and grouped frames are included.
- **Unsynchronisation is never emitted.**
  - v2.2/v2.3 tags with the tag-level unsync flag: de-unsync the whole body first (as
    `deunsyncTag` does), so payloads are decoded. The output header flag is 0. This is lossless.
  - v2.4: for any frame with `v24FrameUnsync`, or every frame when the tag header has
    `flagUnsync`, replace the payload with `deunsync(payload)` and clear bit `0x0002`. A
    data-length indicator (`0x0001`) and its 4-byte prefix stay as they are.
- **v2.2 → v2.4 conversion.** Rebuild the header with the v2.4 ID from the W8 table and flags 0.
  The body is unchanged, except PIC, which is converted to APIC per W8. A v2.2 frame with no
  entry in the W8 table returns `ErrUnsupportedWrite` naming the frame.

**W4. Edited frames** are always written fresh: flags 0, no unsync, no compression, no grouping.
- v2.4: text encoding 3 (UTF-8).
- v2.3: encoding 0 (ISO-8859-1) when every rune is ≤ U+00FF, else encoding 1 (UTF-16 with BOM
  `FF FE`, little-endian).
- Multiple values are joined with a NUL terminator of the encoding's width (`00` or `00 00`), in
  both versions. The reader's `splitTextValues` splits on NUL for every version.
- A value that is not valid UTF-8, or that contains U+0000, returns `ErrUnsupportedWrite` naming
  the key.

**W5. Which frames an edit touches.**
- **KeyEdit, text frame**: the key is in `standardTextFrameIDs` (W9). Remove every frame with
  that ID. If values are non-empty, insert one new frame at the index of the first removed frame,
  or at the end of the list when none existed.
- **KeyEdit, TXXX**: every other key. Remove every TXXX whose decoded description, upper-cased,
  equals the key. Write one new TXXX at the first removed index (or at the end). Its description
  is the **original case** of the first matched description, or the key when there was no match.
- **Version-specific IDs.** A KeyEdit on a v2.4-only ID (`TDRC TDOR TDRL TDEN TDTG TIPL TMCL
  TMOO TPRO TSOA TSOP TSOT TSST`) for v2.3 output, or on a v2.3-only ID (`TYER TDAT TIME TORY
  TRDA TSIZ IPLS`) for v2.4 output, returns `ErrUnsupportedWrite` naming the key.
- **Compressed frames.** If a frame that would be removed under these rules is compressed or
  grouped, it is removed like any other. **Exception**: when a KeyEdit targets TXXX and the tag
  holds a compressed TXXX whose description cannot be read without decompressing, return
  `ErrUnsupportedWrite`. The writer never decompresses.
- **FieldEdit**: compute the target keys with plan 038's **"source key"** rule. Setting a field
  writes only the highest-precedence candidate present in the original, or the canonical key when
  none is present. Clearing a field removes every present candidate of that field.
  - The candidates are `FieldBindings(FormatMP3)` keys for the field, and a candidate is
    "present" when `edits.Orig.Get(key) != nil`.
  - If no candidate is present, write the canonical key: `Title→TIT2`, `Subtitle→TIT3`,
    `Artist/Artists→TPE1`, `Album→TALB`, `AlbumArtist→TPE2`, `Genres→TCON`, `Composers→TCOM`,
    `Year→TDRC` (v2.4 output) or `TYER` (v2.3 output), `TrackNumber/TrackTotal→TRCK`,
    `DiscNumber/DiscTotal→TPOS`, `Grouping→GRP1`, `Narrator→NARRATOR`, `Series→SERIES`,
    `SeriesPart→SERIES PART`, `Publisher→PUBLISHER`, `ISBN→ISBN`, `ASIN→ASIN`,
    `Language→LANGUAGE`, `Description→DESCRIPTION`. Each is then applied as the matching KeyEdit.
  - Value encoding:
    - **Year**: when the present frame value is longer than 4 characters and starts with 4
      digits, replace those 4 digits (`"2021-05-03"` becomes `"1999-05-03"`). Otherwise use
      `fmt.Sprintf("%04d")`. 0 deletes the frame.
    - **Track and Disc**: take `n` and `t` from `edits.Final` (`TrackNumber`/`TrackTotal`). The
      value is `"n"` when `t==0` and `"n/t"` otherwise. Both 0 deletes the frame.
    - **Artists**: `edits.Final.Artists`.
    - **Artist-only edit**: `[Final.Artist]` followed by `Orig.Artists[1:]`. An empty Artist
      deletes TPE1.
  - **Comment** (typed-only): replace the text of every COMM frame with an empty description,
    keeping each frame's language. If there is none, append COMM with language `eng`. Clearing
    the comment removes those frames.
  - Any other field (`Date`, `OriginalDate`, `Lyrics`, `Performers`, `Copyright`, `Label`,
    `CatalogNumber`, `Barcode`, `ISRC`, `MusicBrainz*`) returns
    `fmt.Errorf("%w: %s cannot be written to MP3", types.ErrUnsupportedWrite, field)`.
- Apply KeyEdits first, then FieldEdits, each in slice order (both are sorted by plan 038's
  `Diff`). The output is therefore deterministic.
- **Chapter carriers are not editable.** A KeyEdit on `OVERDRIVE MEDIAMARKERS` returns
  `ErrUnsupportedWrite` ("chapters are not editable"). That TXXX holds OverDrive chapters
  (`internal/mp3/mediamarkers.go:15`, `mediaMarkersDescription`, compared lower-cased). CHAP and
  CTOC never appear in `raw`, so they cannot be targeted.

**W6. Artwork (APIC).** For each existing APIC, read its picture-type byte (skip the encoding
byte, the MIME string up to `\0`, then 1 byte). Apply the ops in order to the APIC sub-list:
- `Add` appends a new APIC built from the Artwork: MIME (ASCII only, else
  `ErrUnsupportedWrite`), type byte = `uint8(a.Type)`, description encoded per W4, then data.
  The new frame goes after the last remaining APIC, or at the end of the list.
- `Remove(t)` drops every APIC, original or added, with
  `types.ArtworkTypeFromPictureType(byte) == t`.
- `RemoveAll` drops every APIC.

Descriptor uniqueness is **not** enforced. Untouched APIC frames are re-emitted per W3.

**W7. DRM and encryption.** Return `ErrDRMProtected` in any of these cases:
- any frame has the encryption flag (v2.3 `0x0040`, v2.4 `0x0004`);
- any `AENC` frame exists;
- a v2.2 tag contains `CRA` or `CRM`.

Check before any output is produced.

**W8. v2.2 → v2.4 ID table** (the body is identical unless noted):
- `BUF RBUF`, `CNT PCNT`, `COM COMM`, `ETC ETCO`, `GEO GEOB`, `MCI MCDI`, `MLL MLLT`,
  `POP POPM`, `REV RVRB`, `SLT SYLT`, `STC SYTC`, `UFI UFID`, `ULT USLT`;
- `TAL TALB`, `TBP TBPM`, `TCM TCOM`, `TCO TCON`, `TCR TCOP`, `TDY TDLY`, `TEN TENC`, `TFT TFLT`,
  `TKE TKEY`, `TLA TLAN`, `TLE TLEN`, `TMT TMED`, `TOA TOPE`, `TOF TOFN`, `TOL TOLY`, `TOT TOAL`;
- `TP1 TPE1`, `TP2 TPE2`, `TP3 TPE3`, `TP4 TPE4`, `TPA TPOS`, `TPB TPUB`, `TRC TSRC`, `TRK TRCK`,
  `TSS TSSE`, `TT1 TIT1`, `TT2 TIT2`, `TT3 TIT3`, `TXT TEXT`, `TXX TXXX`, `TYE TDRC`, `TOR TDOR`;
- `WAF WOAF`, `WAR WOAR`, `WAS WOAS`, `WCM WCOM`, `WCP WCOP`, `WPB WPUB`, `WXX WXXX`;
- iTunes: `TCP TCMP`, `TST TSOT`, `TSP TSOP`, `TSA TSOA`, `TS2 TSO2`, `TSC TSOC`, `GP1 GRP1`,
  `MVN MVNM`, `MVI MVIN`;
- `PIC APIC`. The body is converted: the 3-character format becomes MIME `image/jpeg` for
  `JPG`, `image/png` for `PNG`, and `image/` plus the lower-cased format otherwise, followed by a
  NUL.

Not convertible (→ `ErrUnsupportedWrite`): `TDA TIM TRD TSI IPL EQU RVA LNK`. `CRA CRM` are covered
by W7.

**W9. `standardTextFrameIDs`**:
- `TALB TBPM TCOM TCON TCOP TDAT TDEN TDLY TDOR TDRC TDRL TDTG TENC TEXT TFLT TIME TIPL TIT1 TIT2
  TIT3 TKEY TLAN TLEN TMCL TMED TMOO TOAL TOFN TOLY TOPE TORY TOWN TPE1 TPE2 TPE3 TPE4 TPOS TPRO
  TPUB TRCK TRDA TRSN TRSO TSIZ TSO2 TSOA TSOC TSOP TSOT TSRC TSSE TSST TYER TCMP GRP1 MVNM MVIN`.

A raw key outside this set becomes a TXXX description. For example, `Set("WOAR", ...)` writes
`TXXX:WOAR`, which is documented behavior.

**W10. Tag assembly and padding.**
- Let `oldCap = oldTotalSize - 10` (0 when untagged), and let `F` be the serialized frame length.
- When `F <= oldCap`, padding = `oldCap - F`. The tag keeps its exact on-disk size, so a full
  rewrite and an in-place write produce identical bytes.
- Otherwise padding = `min(max(F/10, 2048), 1<<20)`.
- The header is `"ID3"`, the version byte, `0`, flags `0`, then `synchsafe(F+padding)`.
- The extended header and the v2.4 footer are **dropped**: the CRC would be stale and the footer
  carries no data.
- When `F+padding > 0x0FFFFFFF`, or a single v2.4 frame is over `0x0FFFFFFF` (v2.3: over
  `0xFFFFFFFF`), return `ErrTagTooLarge`.

**W11. Output stream.** The new tag, then `src[oldTotalSize : audioEnd]` copied verbatim with
`writeutil.CopyRange`, then the ID3v1 trailer copied verbatim (W12) when the input had one. `audioEnd` is
`size - 128` when the last 128 bytes start with `TAG`, and `size` otherwise. APEv2, Lyrics3, a
second ID3v2 tag, or junk between the tag and the first MPEG frame all lie inside the copied
range, so they are preserved.

**W12. ID3v1 policy: preserve verbatim; never update, add or remove.** The 128-byte trailer is
copied byte-for-byte. A v1 trailer is a separate tag. Under W0 the writer changes only what the
caller set, and `edits.Keys`/`edits.Fields` address the ID3v2 tag. Rewriting v1 from the v2 edit
would be the writer filling one tag from another. Stale v1 data is the application's call; it is
listed as an open question for the maintainer in Maintenance notes.

**W13. In-place.** `InPlacePatch` returns `ok == true` only when all of these hold:
- the input has an ID3v2 tag;
- `F <= oldCap`.

The patch is then the full new tag (`oldTotalSize` bytes) at `off == 0`. Everything else returns
`ok == false, nil`.

**W14. `AudioDigest`.** The SHA-256 of `src[tagTotal : audioEnd]`. `tagTotal` is
`header.totalSize()` when an ID3v2 tag with a supported or recognized version is present, and 0
otherwise. This must match plan 038's `mp3AudioDigest` oracle.

## Commands you will need

| Purpose | Command | Expected |
|---|---|---|
| Build | `mise exec -- go build ./...` | exit 0 |
| Vet | `mise exec -- go vet ./...` | exit 0 |
| Package tests | `mise exec -- go test -race ./internal/mp3/ -v -run 'Write\|Frame\|ID3v1\|InPlace\|Digest'` | all PASS |
| Harness | `mise exec -- go test -race -run 'WriteRoundTrip\|AudioDigest\|Example' -v .` | all PASS |
| All | `mise exec -- go test -race ./...` | all `ok` |
| Fuzz smoke | `mise exec -- go test -run '^$' -fuzz FuzzParseMP3 -fuzztime 30s ./internal/mp3/` | no crash |
| Lint | `mise exec -- golangci-lint run ./...` | `0 issues.` |
| Format | `gofmt -l .` | no output |
| Oracle (local) | `ffprobe -v error -show_entries format_tags -of json <file>` | exit 0 |

## Scope

**In scope**:
- `internal/mp3/id3write.go` (create): the frame list, W2/W3/W8/W9, and serialization W4/W10
- `internal/mp3/id3write_edits.go` (create): W5/W6
- `internal/mp3/writer.go` (create): the `TagWriter` + `InPlaceWriter` implementation, W7, W11,
  W13, W14, and `init()` registration via `registry.RegisterWriter(types.FormatMP3, &writer{})`
- `internal/mp3/id3write_test.go`,
  `internal/mp3/writer_test.go`, `internal/mp3/write_fixture_test.go` (create)
- `internal/testcorpus/generate.sh` (add the one `sample_v24.mp3` command)
- `testdata/sample_v24.mp3` (create, generated)
- `audiometa_corpus_test.go` (add the new fixture to `TestOpen_Corpus`)
- `write_roundtrip_test.go` (flip the MP3 rows to `writable: true`; add the v24 row and
  MP3-specific edits)
- `example_write_test.go` (make `ExampleWrite` runnable, with `// Output:`)

**Out of scope** (do NOT touch):
- `internal/mp3/id3v2.go`, `parser.go`, `artwork.go`, `id3v1.go` (the reader). Reader gaps
  found while writing (for example TPUB not mapped to Publisher, or TXXX keeping only the first
  value) are follow-ups, not part of this plan. The writer writes the keys the reader reads.
- Any root `.go` file other than the two test files listed. The API is frozen by 038; if you
  believe it must change, STOP.
- Chapters: CHAP and CTOC are re-emitted verbatim and are never edited.
- `README.md`, `CHANGELOG.md`, CI: plan 042.

## Git workflow

- Branch: `advisor/039-mp3-id3v2-writer`
- Commits: gitmoji, single line, no body, no attribution. Suggested:
  - `✨ Add ID3v2 frame-list writer for MP3`
  - `✅ Add v2.4 MP3 fixture and enable MP3 round-trip`
- Do NOT push or open a PR.

## Steps

### Step 1: Test builders for the writer

Create `internal/mp3/write_fixture_test.go` (package `mp3`) with:
- `id3TagFlags(version, flags byte, padding int, frames ...[]byte) []byte`: like `id3Tag`, plus
  header flags and trailing zero padding;
- `id3FrameFlags(version byte, id string, flags uint16, payload []byte) []byte`;
- `txxxPayload(enc byte, desc, value string) []byte`;
- `apicPayload(mime string, picType byte, desc string, data []byte) []byte`;
- `commPayload(lang, desc, text string) []byte`;
- `writeBytes(t, src []byte, edits *types.EditSet) []byte`: calls `(&writer{}).WriteFile` into a
  `bytes.Buffer` and fails the test on error;
- `boundTags(t, src []byte) *types.Tags`: parses with `parseBytes` and calls `types.BindFormat`.

**Verify**: `mise exec -- go vet ./internal/mp3/` → exit 0. (The file references `writer`, which
does not exist yet. If vet fails only because of that, leave the helpers that reference `writer`
in a comment until Step 3, then restore them.)

### Step 2: Frame-list parse and verbatim serialization (W1–W3, W8–W10)

In `internal/mp3/id3write.go`, implement:
- `type wframe struct { id string; flags uint16; payload []byte; edited bool }`
- `type wtag struct { inVersion, outVersion byte; frames []wframe; oldTotal int64 }`
- `func readWTag(ctx context.Context, sr *binutil.SafeReader, size int64) (*wtag, error)`:
  - When `"ID3"` is absent, return a tag with `oldTotal 0` and `outVersion 4`.
  - Otherwise use `parseID3v2Header`. Reject the versions listed in W1.
  - Read the whole body (`sr.CheckLen` first). For v2.2/v2.3 with `flagUnsync`, deunsync it.
  - Skip the extended header (v2.3/v2.4: reuse the `skipExtendedHeader` arithmetic on the body).
  - Walk the frames per W2 into `[]wframe`:
    - v2.2 frames are converted to their v2.4 ID per W8 (PIC body converted);
    - v2.4 unsync is decoded per W3;
    - check `binutil.Canceled(ctx, i)` every iteration.
- `func (t *wtag) serialize() ([]byte, error)`: the frames per W3/W4 and the header per W10, with
  padding computed against `t.oldTotal`. Frame size encoding: synchsafe for v2.4, big-endian for
  v2.3. Return `ErrTagTooLarge` per W10.
- `func checkTagSize(bodyLen int, version byte) error` (separate, so it can be tested without
  allocating 256 MiB).

Tests in `internal/mp3/id3write_test.go`:
- `TestWTag_VerbatimRoundTrip`: a table of v2.3 and v2.4 tags. Each holds TIT2, a TXXX, an
  unknown `XYZW` frame, PRIV, a CHAP (built with `chapFrameBody`), a CTOC (raw bytes), and an
  APIC with 64 bytes of padding. Assert that `readWTag` followed by `serialize` gives:
  - an identical header except flags 0;
  - identical frame bytes in the same order;
  - total size equal to the input's total size.
- `TestWTag_V23TagUnsync`: a v2.3 tag with header flag `0x80` whose TIT2 payload contains
  `FF 00` → the output flag is 0, the payload is de-unsynced, and `parseBytes` of the output has
  the same Title.
- `TestWTag_V24FrameUnsync`: a v2.4 frame with flag `0x0002` → the output frame flags are `0x0000`
  and the payload is de-unsynced.
- `TestWTag_V24CompressedPreserved`: a v2.4 `XYZW` frame with flag `0x0008|0x0001` → re-emitted
  byte-identical.
- `TestWTag_V22ConvertsToV24`: a v22 tag (`v22Tag` in `id3_v22_v1_test.go`) with TT2, TP1 and PIC
  → the output version byte is 4, the IDs are TIT2, TPE1 and APIC, and `parseBytes` and
  `extractArtwork` of the output give the same Title, Artist and image data.
- `TestWTag_V22UnconvertibleRefused`: a v22 tag with `TDA` → `ErrUnsupportedWrite`.
- `TestWTag_FrameOverrunsTag` → `*types.CorruptedFileError`.
- `TestWTag_Untagged` → `oldTotal == 0`, `outVersion == 4`.
- `TestCheckTagSize`: `0x0FFFFFFF` passes; `0x10000000` → `ErrTagTooLarge`.
- `TestWTag_Padding`:
  - frames shrink → the total size is unchanged;
  - frames grow beyond the capacity → padding is at least 2048;
  - untagged → padding is exactly 2048 for a small tag.

**Verify**: `mise exec -- go test -race ./internal/mp3/ -run WTag -v` → all PASS.

### Step 3: Apply edits (W4–W7)

In `internal/mp3/id3write_edits.go`, implement `func (t *wtag) apply(edits *types.EditSet) error`
per W5 (key edits, then field edits, then Comment) and W6 (artwork). Add the encoders:
- `encodeText(version byte, values []string) ([]byte, error)`;
- `encodeTXXX(version byte, desc string, values []string) ([]byte, error)`;
- `encodeCOMM(version byte, lang string, text string) ([]byte, error)`;
- `encodeAPIC(version byte, a types.Artwork) ([]byte, error)`.

Add `func (t *wtag) drmCheck() error` per W7. Keep every function under `gocyclo` 15 by
splitting into `applyKeyEdit`, `applyFieldEdit`, `fieldTargetKeys`, `applyComment` and
`applyArtwork`.

Tests (add to `id3write_test.go`). Each builds a source with the Step 1 builders, gets the
original tags with `boundTags`, builds the `EditSet` with `types.Diff`, calls `writeBytes`, then
parses the output with `parseBytes` (and `extractArtwork` for images):
- `TestApply_TextKeyEdit`: `Set("TIT2","Nouveau ✓")`:
  - v2.4 → encoding byte 3;
  - v2.3 → encoding byte 1 (non-Latin-1);
  - v2.3 with `"Cafe"` → encoding byte 0;
  - all other frames are byte-identical.
- `TestApply_TXXXPreservesDescriptionCase`: the source has TXXX `"Narrator"`, then
  `Set("NARRATOR","X")` → the output TXXX description is `"Narrator"` and the value is `"X"`.
- `TestApply_FieldEdits`, a table:
  - `Title` → TIT2;
  - `Narrator` → TXXX;
  - `Year=1999` on v2.4 `TDRC "2021-05-03"` → `"1999-05-03"`;
  - `Year=1999` on v2.3 → `TYER "1999"`;
  - `TrackNumber=3, TrackTotal=12` → `TRCK "3/12"`;
  - `Comment` → a COMM with an empty description;
  - `Date` → `ErrUnsupportedWrite`.
- `TestApply_SourceKeyOnly`: the source has both `GRP1` and `TIT1`. Then `Grouping = "G"` →
  `GRP1` holds `"G"` and the `TIT1` frame is byte-identical. Clearing Grouping removes both
  frames.
- `TestApply_NoCrossFieldWrites` (**W0**): a source with `TCOM "C"`, `GRP1 "Saga #5"`,
  `TIT2 "Book 3"` and a v1 trailer. Then:
  - `Narrator = "N"` → `TCOM` is byte-identical, and the only changed or added frame is
    `TXXX:NARRATOR`;
  - `Series = "S"` → `GRP1` and `TIT2` are byte-identical;
  - `Title = "T"` → the v1 trailer is byte-identical.

  For each case, compare every frame other than the targeted one by ID and payload.
- `TestApply_MultiValue`: `Artists = ["A","B"]` on v2.3 and v2.4 → `Tags.Artists` re-reads as
  `[A B]`.
- `TestApply_VersionSpecificKeyRefused`: `Set("TDRC","2020")` on v2.3 → `ErrUnsupportedWrite`.
- `TestApply_CompressedTXXXRefused`.
- `TestApply_Artwork`, starting from a source with front (type 3) and back (type 4) APIC:
  - `ReplaceArtwork(front PNG)` → front is the PNG and back is byte-identical;
  - `RemoveArtwork(BackCover)` → only front remains;
  - `RemoveAllArtwork` → none;
  - `AddArtwork(type 8)` → three images.
- `TestApply_DRM`: an encrypted-flag frame and an `AENC` frame each → `ErrDRMProtected`.
- `TestApply_InvalidUTF8Refused`.
- `TestApply_MediaMarkersRefused`: `Set("OVERDRIVE MEDIAMARKERS", "x")` → `ErrUnsupportedWrite`.

**Verify**: `mise exec -- go test -race ./internal/mp3/ -run 'Apply|WTag' -v` → all PASS.

### Step 4: ID3v1 preservation (W12)

Add `TestWriter_ID3v1Verbatim` to `writer_test.go`. The source has a v1 trailer, and the edit is a
Title plus Artist change. The last 128 bytes of the output must equal the source's last 128
bytes.

**Verify**: `mise exec -- go test -race ./internal/mp3/ -run ID3v1 -v` → PASS.

### Step 5: The `TagWriter` (W11, W13, W14) and registration

`internal/mp3/writer.go`:

```go
type writer struct{}

func (w *writer) WriteFile(ctx context.Context, out io.Writer, src io.ReaderAt, size int64, edits *types.EditSet) (int64, error)
func (w *writer) AudioDigest(ctx context.Context, src io.ReaderAt, size int64) ([]byte, error)
func (w *writer) InPlacePatch(ctx context.Context, src io.ReaderAt, size int64, edits *types.EditSet) (int64, []byte, bool, error)

func init() { registry.RegisterWriter(types.FormatMP3, &writer{}) }
```

`WriteFile`:
1. `readWTag`, then `drmCheck`, then `apply`, then `serialize`.
2. Write the tag through a `writeutil.CountingWriter`.
3. `CopyRange(src, oldTotal, audioEnd-oldTotal)`.
4. Append the v1 trailer verbatim, when present.

Put the `init` in `writer.go`; `parser.go`'s existing `init` stays as it is.

Tests in `writer_test.go`:
- `TestWriter_AudioRegionVerbatim`: the source is a tag, 4 KiB of `mpegFrame` repeats and a v1
  trailer. After a Title edit, the output bytes from `newTagEnd` to `len-128` equal the source's
  audio bytes.
- `TestWriter_Deterministic`: two writes are byte-identical.
- `TestWriter_InPlaceEqualsFullRewrite`: when it fits, apply the patch to a copy of the source;
  the result is byte-identical to `WriteFile`'s output.
- `TestWriter_InPlaceNotOK`:
  - it grows beyond the capacity → `ok == false`, `err == nil`.
- `TestWriter_AudioDigest`: equals the sha256 of the oracle range for a tagged file, for an
  untagged file, and for a file with a v1 trailer.
- `TestWriter_Corpus`: `testdata/sample.mp3` with an empty `EditSet` → the output re-parses, and
  every frame payload matches the source's.

**Verify**: `mise exec -- go test -race ./internal/mp3/ -v 2>&1 | grep -c '^--- FAIL'` → `0`.

### Step 6: The v2.4 fixture

Append to `internal/testcorpus/generate.sh`, just before `echo "Generated:"`:

```sh
# MP3, ID3v2.4 with an ID3v1 trailer, cover art (write-path coverage).
# shellcheck disable=SC2046,SC2086
$FF $SILENCE -i "$TMP/cover.jpg" -map 0:a -map 1:v \
	-c:a libmp3lame -c:v copy -id3v2_version 4 -write_id3v1 1 \
	-metadata title="Test Title" -metadata artist="Test Artist" \
	-metadata album="Test Album" -metadata date="2021" \
	-metadata:s:v title="Cover" -metadata:s:v comment="Cover (front)" \
	"$OUT/sample_v24.mp3"
```

**Do not run the whole script.** That would re-encode every existing fixture. Run only this
command, from the repo root, in a shell where you have set:
- `OUT=testdata`
- `TMP=$(mktemp -d)`
- `FF="ffmpeg -hide_banner -loglevel error -y"`
- `SILENCE="-f lavfi -t 2 -i anullsrc=r=44100:cl=stereo"`

and created the cover with `$FF -f lavfi -i color=c=red:s=16x16 -frames:v 1 "$TMP/cover.jpg"`.

Add `{path: "testdata/sample_v24.mp3", wantFormat: audiometa.FormatMP3}` to `TestOpen_Corpus` in
`audiometa_corpus_test.go`.

**Verify**:
- `git status --short testdata/` → only `?? testdata/sample_v24.mp3`;
- `head -c 4 testdata/sample_v24.mp3 | od -An -c` → `I D 3 004`;
- `tail -c 128 testdata/sample_v24.mp3 | head -c 3` → `TAG`;
- `mise exec -- go test -run TestOpen_Corpus -v .` → PASS.

### Step 7: Enable the round-trip harness for MP3

In `write_roundtrip_test.go`:
- Set `writable: true` on the three existing MP3 rows.
- Add `{"testdata/sample_v24.mp3", true}`.
- In `roundTripEdits`, add `audiometa.FormatMP3` to the `unsupported` list of any seeded edit
  that touches a field W5 rejects. The seeded edits (Title, clear Album, Artists, Comment, Year)
  are all supported, so the list should stay empty. If any one fails, STOP and report.
- Add MP3-specific edits:
  - `Set("NARRATOR","Reader")` → its check is `Narrator == "Reader"`;
  - `Set("TIT2")` (delete), gated to `testdata/sample.mp3`, which has no v1 trailer → its check
    is `Title == ""`.
  Gate both to MP3 with a `formats []audiometa.Format` field. Add that field to `tagEdit` if it is
  not there yet. It is a test-only change.
- Add a version-preservation test, `TestWriteRoundTrip_MP3VersionPreserved`: after a Title edit,
  the output byte 3 is `3` for `sample.mp3` and `4` for `sample_v24.mp3`.
- Add `TestWriteRoundTrip_MP3ID3v1Preserved`: on `sample_v24.mp3` set Title to `"New"` → the
  last 128 bytes of the output equal the source's.

In `example_write_test.go`, make `ExampleWrite` runnable:
1. copy `testdata/sample.mp3` to `os.MkdirTemp`;
2. `audiometa.Write(copy, func(f *audiometa.File) error { f.Tags.Title = "New Title"; return nil })`;
3. re-open the copy and `fmt.Println(f.Tags.Title)`;
4. add `// Output: New Title`.

**Verify**: `mise exec -- go test -race -run 'WriteRoundTrip|Example' -v .` → all PASS. The MP3
subtests run the full edit loop and no longer only assert `ErrUnsupportedWrite`.

### Step 8: External oracle (local only)

For a manual sanity check, write the edited output of each MP3 edit to a temp file and run
`ffprobe -v error -show_entries format_tags -of json <file>`. Expected: exit 0, and `title`
equals the edited title. Also run `mid3v2 -l <file>` if it is installed; it lists the frames
without error. This step does not add a test (plan 042 adds the CI oracle); just record the
outputs in your report.

**Verify**: both tools exit 0 on every edited output.

### Step 9: Full gates

**Verify**:
- `mise exec -- go test -race ./...` → all `ok`;
- `mise exec -- go test -run '^$' -fuzz FuzzParseMP3 -fuzztime 30s ./internal/mp3/` → no crash;
- `mise exec -- golangci-lint run ./...` → `0 issues.`;
- `gofmt -l .` → empty.

## Test plan

- New test files:
  - `internal/mp3/write_fixture_test.go` (builders);
  - `internal/mp3/id3write_test.go` (frame list, flags, conversions, edits, artwork, DRM, padding);
  - `internal/mp3/writer_test.go` (stream, determinism, in-place, digest, corpus).
- Extended: `write_roundtrip_test.go` (MP3 rows writable, MP3 edits, version and v1 checks),
  `audiometa_corpus_test.go`, `example_write_test.go`.
- Patterns:
  - `internal/mp3/id3_fixture_test.go` and `id3_v22_v1_test.go` (synthetic tags);
  - `internal/mp3/id3v2_flags_test.go` (flag cases);
  - `internal/mp3/lazy_apic_test.go` (artwork assertions).
- Command: `mise exec -- go test -race ./...` → all pass.

## Done criteria

- [ ] `mise exec -- go build ./...`, `go vet ./...` exit 0; `gofmt -l .` empty
- [ ] `mise exec -- go test -race ./...` all `ok`; `TestWriteRoundTrip_Corpus` runs the edit loop for 4 MP3 files
- [ ] `mise exec -- golangci-lint run ./...` → `0 issues.`
- [ ] `grep -n 'RegisterWriter(types.FormatMP3' internal/mp3/writer.go` → 1 match
- [ ] `git diff 0d60738 --stat -- internal/mp3/id3v2.go internal/mp3/parser.go internal/mp3/artwork.go internal/mp3/id3v1.go` shows only changes from plans 016 and the idioms pass (none from this plan)
- [ ] `git status --short testdata/` shows only `sample_v24.mp3` added
- [ ] `ExampleWrite` has an `// Output:` line and passes
- [ ] `TestApply_NoCrossFieldWrites`, `TestApply_SourceKeyOnly` and `TestWriter_ID3v1Verbatim` pass (W0 no-inference rule)
- [ ] `plans/README.md` row for 039 updated

## STOP conditions

- The 038 API sign-off is missing, or you believe a public signature must change.
- `readWTag` followed by `serialize` of `testdata/sample.mp3` is not frame-for-frame identical.
  That means the frame walk disagrees with the reader. Report the first differing frame index
  and ID.
- A seeded harness edit fails on an MP3 fixture for a reason other than a bug in your code (for
  example, the reader does not read back a key that W5 writes). Report it instead of changing
  the reader.
- `TestBenchFixturesParse` or any reader test changes result.
- Generating `sample_v24.mp3` modifies any other file in `testdata/`.
- `ffprobe` rejects an edited output (Step 8).

## Maintenance notes

- **Chapter writing is a follow-up plan.** CHAP and CTOC are only re-emitted verbatim. A future
  plan adds chapter edits (CHAP/CTOC rebuild, element-ID management, TIT2 subframes).
- **Reader gaps that limit the writer** (each a small follow-up):
  - TPUB is not read as Publisher, so the writer uses `TXXX:PUBLISHER`;
  - TXXX multi-values are read as the first value only;
  - USLT, TCOP and TSRC are not read, so Lyrics, Copyright and ISRC are not writable for MP3;
  - TDRC month and day are not exposed as `Date`.

  When the reader learns one of these, update `internal/types/fieldmap.go` and the W5 canonical
  list together.
- v2.3 multi-values are NUL-separated (W4), which this library reads but some v2.3 readers show
  as only the first value. If the maintainer prefers `/` (Mutagen's v2.3 default), change
  `encodeText` and the round-trip expectations together.
- **ID3v1 is preserved verbatim (W12).** That follows from the no-inference rule. If the
  maintainer wants a v1 trailer kept in sync with v2 edits, it must be an explicit, opt-in caller
  choice (for example a future `WithID3v1Sync()` option). It must not be an implicit side effect.
- - APIC descriptor uniqueness is not enforced (W6). Some players hide duplicate descriptors.
- The extended header (CRC, restrictions) and the v2.4 footer are dropped on write (W10).
- Reviewer focus:
  - W3 (byte-identical re-emit) and W5 (which frames an edit removes);
  - that the audio range copy uses `oldTotal`, the **on-disk** size. It is not the
    de-unsynchronised size: for a v2.3 tag with unsync, `header.Size` changes after
    `deunsyncTag`, so capture the size before decoding.

## Maintainer addendum (2026-10-31): tag deletion

Applications must be able to delete any tag, including keys that map to no typed field. Plan 038 adds `Tags.Delete(keys ...string)`. A deletion arrives at this writer as a `KeyEdit` with `Values == nil`, so it uses the existing remove path with no insert. Add these tests and treat them as done criteria:

- **Typed key:** deleting a key that fills a typed field removes it from the file (MP3 `TIT2`, FLAC `TITLE`, M4A `©nam`). After re-open the field is empty, every other tag is unchanged, and the audio digest is unchanged.
- **Key with no typed field:** deleting such a key removes it from the file. The examples are an MP3 `TXXX` with a custom description, a FLAC/Vorbis `SHOW` comment, and an M4A `----` freeform item, plus `gnre` once the reader records it.
- **Every key:** deleting every key in `Tags.All()` produces a valid file. Re-open gives zero raw tags and the audio digest is unchanged. For MP3 this means an empty tag body with padding, or no ID3v2 tag at all; pick one, document it in godoc, and test it. FLAC keeps a VORBIS_COMMENT block with only the vendor string. M4A keeps an empty `ilst`.
- **Chapters:** deletion never touches chapter structures. MP3 CHAP/CTOC and the OverDrive MediaMarkers TXXX, FLAC `CHAPTERnnn`, and M4A `chpl` and the chapter track stay as they are. Deleting a chapter-bearing key returns `ErrUnsupportedWrite`, matching the read-only chapter rule.
