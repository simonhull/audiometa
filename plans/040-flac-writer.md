# Plan 040: Write FLAC metadata (VORBIS_COMMENT and PICTURE blocks, padding reuse)

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report. Do not improvise. When done, update the status row for this plan
> in `plans/README.md`, unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Precondition**: plans 038 (with maintainer API sign-off) and 039 are DONE.
> `grep -n 'RegisterWriter(types.FormatMP3' internal/mp3/writer.go` → 1 match. If not, STOP.
>
> **Drift check (run first)**: `git diff --stat 0d60738..HEAD -- internal/flac/ internal/vorbis/ write_roundtrip_test.go`
> Expected drift:
> - plans 038/039 edited `write_roundtrip_test.go`;
> - **plan 016** replaced the inline series fallback in `internal/flac/parser.go:174-188` with a
>   call to `parsing.ResolveSeriesFallbacks`, and a **no-inference pass** then deletes that call
>   and the helper entirely (do not rely on it), and moved chapter end-time filling into
>   `types.FillChapterEndTimes`;
> - a **Go 1.27 idioms pass** may have turned `binary.Read[T](sr, off, what)` /
>   `binary.ReadLE[T](...)` into `sr.Read[T](off, what)`-style methods.
>
> Write new code in whatever form is live. Beyond that, a mismatch in the block-header parsing
> excerpt below is a STOP.

## Status

- **Priority**: P2
- **Effort**: M
- **Risk**: MED. FLAC has no offset tables into audio; the main risks are a wrong is-last flag
  or a malformed block, either of which makes the file unreadable by decoders. Both are covered
  by tests and by the audio-digest check.
- **Depends on**: plans/038-write-api-foundations.md, plans/039-mp3-id3v2-writer.md
- **Category**: direction
- **Planned at**: commit `0d60738` (branch `fix/improve-plans`), 2026-10-31

## Why this matters

FLAC is the second approved write format (`docs/design/020-write-support-decision.md`,
"Decision"). Its metadata is a chain of length-prefixed blocks ahead of the audio, with a
PADDING block intended for in-place edits. So it reuses the 039 machinery with little new risk.
This plan adds a writer that:
- edits Vorbis comments and pictures;
- keeps every other block (STREAMINFO, SEEKTABLE, CUESHEET, APPLICATION, unknown) byte-for-byte;
- keeps the vendor string;
- absorbs growth into existing padding, so the audio usually doesn't move.

## Current state

Excerpts are from `0d60738`.

**Block walk** (`internal/flac/parser.go:64-154`). Each header is a big-endian `uint32`:

```go
		isLast := (header >> 31) == 1
		blockType := uint8((header >> 24) & 0x7F)
		blockLength := int64(header & 0x00FFFFFF)

		offset += 4 // Move past header
		...
		// Move to next block
		offset += blockLength

		// If this was the last metadata block, we're done
		if isLast {
			audioStart = offset // first byte after the metadata blocks = first audio frame
			break
		}
```

Block type constants are at `:20-28`: `blockTypeStreamInfo 0`, `blockTypePadding 1`,
`blockTypeApplication 2`, `blockTypeSeekTable 3`, `blockTypeVorbisComment 4`,
`blockTypeCueSheet 5`, `blockTypePicture 6`. The magic `"fLaC"` is at offset 0 (`:43-53`).

**VORBIS_COMMENT layout** (`parseVorbisComment`, `:293-351`): a little-endian `uint32` vendor
length, then the vendor bytes, then a little-endian `uint32` count, then per comment a
little-endian `uint32` length followed by `KEY=VALUE` UTF-8 bytes. Every comment is passed to
`vorbis.ParseComment` (`internal/vorbis/comments.go:22-157`). That function upper-cases the key
for `raw` (`:38`) and maps fields. **Note:** for repeated `ARTIST` it sets `Artist` to the
**last** value (`:47-49`), while `Artists` collects all of them.

**Chapters live in comments.** `vorbis.ParseChapters` (`internal/vorbis/chapters.go:28-…`)
reads `CHAPTERxxx` and `CHAPTERxxxNAME` keys. Chapters are not editable in this wave.

**PICTURE layout** (`parsePicture`, `:354-445`), all big-endian `uint32`:
`type, mimeLen, mime, descLen, desc(UTF-8), width, height, depth, colors, dataLen, data`.

**Corpus layout** (verified with a block walk):
- `testdata/sample.flac`: STREAMINFO(34), PICTURE(266), VORBIS_COMMENT(106), PADDING(8192, last).
  The audio starts at 8618.
- `testdata/sample_upper.flac`: STREAMINFO, SEEKTABLE(18), VORBIS_COMMENT(115), PADDING(8192, last).

**Test builders** (`internal/flac/fixtures_test.go:9-60`): `flacBlock(last, type, payload)`,
`streamInfoPayload(rate, samples)`, `vorbisCommentPayload(comments...)` (vendor
`"audiometa-test"`), `buildFLAC(parts...)`.

**Foundations** (plans 038/039):
- `types.EditSet`, `types.FieldBindings(types.FormatFLAC)` (the Vorbis table);
- `types.ErrUnsupportedWrite`, `ErrTagTooLarge`;
- `registry.TagWriter` / `InPlaceWriter` / `RegisterWriter`;
- `writeutil.CopyRange` / `DigestRange` / `CountingWriter`;
- the "source key" field rule (plan 038 D2);
- the MP3 writer in `internal/mp3/writer.go`, which is the structural pattern to follow.

## Decisions (binding)

**F0. No cross-field inference (maintainer rule, 2026-10-31).** The application decides what tags
mean, and the library never derives one field from another. The writer changes only the comments
representing fields the caller set or cleared. For example, writing `NARRATOR` never touches
`COMPOSER`, and writing `SERIES` never touches `GROUPING` or `TITLE`.

**F1. Block model.** Parse `"fLaC"`, then blocks into `[]fblock{typ uint8; payload []byte}`,
until the is-last bit.
- Use `sr.CheckLen` before allocating, and call `binutil.Canceled(ctx, i)` per block.
- STREAMINFO must be the first block. If it is not, or the chain never reaches is-last before
  EOF, return `*types.CorruptedFileError`.
- Record `audioStart` (the offset after the last block) and
  `oldMetaLen = audioStart - 4`.

**F2. Verbatim blocks.** STREAMINFO, SEEKTABLE, APPLICATION, CUESHEET, reserved types (7–126)
and every PICTURE not removed by an op are re-emitted with identical payload bytes, in their
original relative order. The is-last bit is the only header change.

**F3. VORBIS_COMMENT edits.**
- Parse into `vendor []byte` plus `comments [][]byte` (raw `KEY=VALUE`). A malformed block
  returns `*types.CorruptedFileError`.
- More than one VORBIS_COMMENT block with any tag edit pending returns `ErrUnsupportedWrite`.
  With only artwork edits pending, every VORBIS_COMMENT block is left verbatim.
- A **KeyEdit** `K` (raw keys are upper-case):
  - remove every comment whose key part (the text before the first `=`) `strings.EqualFold` `K`;
  - insert one `key=value` comment per value at the index of the first removed comment, or
    append at the end when none was removed. `key` is the **original spelling** of the first
    removed comment's key, or `K`;
  - validate the key: non-empty, every byte in 0x20–0x7D, and no `=`. Validate each value as UTF-8.
    Either failure returns `ErrUnsupportedWrite` naming the key;
  - a key matching `CHAPTER` followed by a digit returns `ErrUnsupportedWrite` ("chapters are not
    editable").
- A **FieldEdit** uses plan 038's **"source key"** rule over `FieldBindings(types.FormatFLAC)`.
  Setting writes only the highest-precedence present candidate. Clearing removes every present
  candidate of that field. When no candidate is present, the canonical key is the
  binding's **first** key (for example `TITLE`, `TRACKTOTAL`, `DISCTOTAL`, `SERIES`,
  `SERIESPART`, `ASIN`, `LANGUAGE`). Value encoding:
  - **Year**: when `DATE` is present, longer than 4 characters and starts with 4 digits, replace
    those 4 digits. Otherwise use `%04d`. 0 deletes the comment.
  - **Track and Disc numbers and totals**: decimal, each in its own key. 0 deletes.
  - **Artists**: `Final.Artists`.
  - **Artist-only edit**: `[Final.Artist]` followed by `Orig.Artists[1:]`. An empty Artist
    deletes `ARTIST`.
  - Every other field: `FieldEdit.Values` as given.

  All 35 fields are writable for FLAC.
- The **vendor string** is copied verbatim. The comment count is recomputed.
- **No VORBIS_COMMENT block and tag edits pending**: create one with vendor `audiometa`, inserted
  directly after STREAMINFO, or after a SEEKTABLE that directly follows STREAMINFO.

**F4. PICTURE ops** (in order), applied to the PICTURE sub-list. The type is the payload's first
4 bytes:
- `Add` builds a block with the following fields:
  - type `uint32(a.Type)`;
  - the MIME (ASCII only, else `ErrUnsupportedWrite`);
  - the description as UTF-8;
  - width and height from `a.Width`/`a.Height`. `AddArtwork` filled them from `imagemeta`
    where it could;
  - depth 0, colors 0, then the data.

  New blocks go after the last remaining PICTURE, or, when there is none, after the last
  non-PADDING block.
- `Remove(t)` drops pictures where `types.ArtworkTypeFromPictureType(type) == t`.
- `RemoveAll` drops every PICTURE.

Uniqueness of types 1 and 2 (file icons) is not enforced.

**F5. PADDING policy.** Drop every PADDING block, then compute `newLen`, the sum over the
remaining blocks of `4 + len(payload)`:
- `newLen == oldMetaLen` → no padding block;
- `newLen + 4 <= oldMetaLen` → one trailing PADDING whose payload is `oldMetaLen - newLen - 4`
  zero bytes (it can be 0);
- otherwise → one trailing PADDING of 8192 zero bytes (libFLAC's default).

The first two cases keep `audioStart` unchanged, and the full rewrite then equals the in-place
result.

**F6. Limits.** Any block payload over `0xFFFFFF` bytes returns `ErrTagTooLarge`. The limit is
16 MiB, which a large picture can exceed.

**F7. is-last flag.** Clear it on every block, then set it on the final block. Only the
top bit of the header byte changes.

**F8. Output.** `"fLaC"`, the blocks, then `src[audioStart:size]` copied verbatim
(`writeutil.CopyRange`). `AudioDigest` is the SHA-256 of `src[audioStart:size]`, which matches
plan 038's `flacAudioDigest` oracle.

**F9. In-place.** `InPlacePatch` returns `ok` when F5 lands in its first or second case. The
patch is the serialized blocks (exactly `oldMetaLen` bytes) at `off == 4`. Otherwise it returns
`ok == false, nil`.

**F10. DRM.** FLAC has no DRM mechanism. There is no refusal path.

## Commands you will need

| Purpose | Command | Expected |
|---|---|---|
| Build / vet | `mise exec -- go build ./... && mise exec -- go vet ./...` | exit 0 |
| Package tests | `mise exec -- go test -race ./internal/flac/ -v -run 'Write\|FBlock\|Picture\|Padding\|Digest'` | all PASS |
| Harness | `mise exec -- go test -race -run 'WriteRoundTrip' -v .` | all PASS |
| All | `mise exec -- go test -race ./...` | all `ok` |
| Fuzz smoke | `mise exec -- go test -run '^$' -fuzz FuzzParseFLAC -fuzztime 30s ./internal/flac/` | no crash |
| Decoder oracle | `flac -t -s <file>` (local) | exit 0 |
| Tag oracle | `metaflac --list <file>` (local) | exit 0 |
| Lint / format | `mise exec -- golangci-lint run ./...` / `gofmt -l .` | `0 issues.` / empty |

## Scope

**In scope**:
- `internal/flac/writer.go` (create): F1–F10, the `TagWriter` + `InPlaceWriter` implementation,
  and `init()` with `registry.RegisterWriter(types.FormatFLAC, &writer{})`
- `internal/flac/vorbiswrite.go` (create): F3
- `internal/flac/writer_test.go`, `internal/flac/vorbiswrite_test.go` (create)
- `write_roundtrip_test.go` (flip the FLAC rows; add FLAC-specific edits)

**Out of scope**:
- `internal/flac/parser.go` and `cuesheet.go` (the reader), and `internal/vorbis/` (shared with
  Ogg). If the writer needs a helper from them, copy the few lines instead of changing them.
- Ogg Vorbis and Opus writing: deferred by maintainer decision. Do not register a writer for
  `FormatOgg` or `FormatOpus`, even though they share Vorbis comments. Their re-pagination and
  CRC work is a separate future plan.
- FLAC files with a leading ID3v2 tag. The reader already rejects them (no `fLaC` at 0), so
  `Open` fails first.
- Chapter edits (`CHAPTERxxx` comments, CUESHEET).
- Root `.go` files other than `write_roundtrip_test.go`. README, CHANGELOG and CI belong to
  plan 042.

## Git workflow

- Branch: `advisor/040-flac-writer`
- Commits: gitmoji, single line, no body, no attribution. Suggested:
  - `✨ Add FLAC metadata block writer`
  - `✅ Enable FLAC round-trip`
- Do NOT push or open a PR.

## Steps

### Step 1: Block model and verbatim re-serialization (F1, F2, F5–F8)

Implement in `internal/flac/writer.go`:
- `type fblock struct{ typ uint8; payload []byte }`
- `type fmeta struct{ blocks []fblock; audioStart, oldMetaLen int64 }`
- `func readFMeta(ctx, sr, size) (*fmeta, error)`
- `func (m *fmeta) serialize() ([]byte, error)`, which applies F5 (padding), F6 (limit) and F7
  (is-last)

Tests in `internal/flac/writer_test.go`:
- `TestFBlock_VerbatimRoundTrip`: `buildFLAC` with STREAMINFO, SEEKTABLE (18 bytes), an
  APPLICATION block (type 2, payload `"TEST" + 4 bytes`), a CUESHEET (type 5, 396 arbitrary
  bytes), VORBIS_COMMENT and PADDING(100, last), plus 64 audio bytes. Then `readFMeta` and
  `serialize` give bytes identical to the input metadata (`input[4:audioStart]`).
- `TestFBlock_PaddingAbsorbsGrowth`: add a 50-byte comment → the output `audioStart` is unchanged
  and the padding shrinks by 54.
- `TestFBlock_PaddingExhausted`: padding 10, grow by 100 → the trailing padding is 8192.
- `TestFBlock_ExactFitNoPadding`: grow by exactly `4 + padLen` → no PADDING block, and the last
  real block has the is-last bit.
- `TestFBlock_IsLastOnlyOnFinal`: walk the output headers; exactly one has bit `0x80`, and it is
  the last.
- `TestFBlock_StreamInfoNotFirst` and `TestFBlock_TruncatedChain` → `*types.CorruptedFileError`.
- `TestFBlock_TooLarge`: a payload of `0x1000000` bytes → `ErrTagTooLarge`. To avoid allocating
  16 MiB in a unit test, test the size check as its own function, `checkBlockLen(n int) error`.

**Verify**: `mise exec -- go test -race ./internal/flac/ -run FBlock -v` → all PASS.

### Step 2: VORBIS_COMMENT edits (F3)

`internal/flac/vorbiswrite.go`:
- `type vcomment struct{ vendor []byte; comments [][]byte }`
- `parseVC(payload []byte) (*vcomment, error)`
- `(v *vcomment) bytes() []byte`
- `(v *vcomment) applyKey(KeyEdit) error`
- `applyField(v, FieldEdit, edits *types.EditSet) error`

Tests in `internal/flac/vorbiswrite_test.go`:
- `TestVC_VerbatimRoundTrip`: `parseVC` then `bytes()` gives identical bytes, including vendor
  `"audiometa-test"` and mixed-case keys (`title=x`).
- `TestVC_KeyEditPreservesOriginalKeyCase`: the source has `title=Old`, then `Set("TITLE","New")`
  → the output comment is `title=New`, at the same index.
- `TestVC_MultiValue`: `Set("ARTIST","A","B")` → two `ARTIST=` comments in order.
- `TestVC_DeleteKey` removes every case variant (`Genre=`, `GENRE=`).
- `TestVC_FieldEdits`, a table:
  - Title;
  - Year on `DATE=2021-05-03` → `DATE=1999-05-03`;
  - TrackTotal with only `TOTALTRACKS` present → updates `TOTALTRACKS`;
  - TrackTotal with both `TRACKTOTAL` and `TOTALTRACKS` present → updates `TRACKTOTAL` only,
    `TOTALTRACKS` is byte-identical, and clearing removes both;
  - Series with neither key present → `SERIES=`;
  - Lyrics → `LYRICS=`;
  - MusicBrainzTrackID → `MUSICBRAINZ_TRACKID=`.
- `TestVC_InvalidKeyRefused` (a key containing `=`, or a byte `0x7E`) → `ErrUnsupportedWrite`.
- `TestVC_ChapterKeyRefused`: `Set("CHAPTER001NAME","x")` → `ErrUnsupportedWrite`.
- `TestVC_VendorPreserved`, after any edit.
- `TestVC_NoCrossFieldWrites` (**F0**): the source has `COMPOSER=C`, `GROUPING=Saga #5` and
  `TITLE=Book 3`. Then:
  - `Narrator="N"` → the only change is an added `NARRATOR=N`;
  - `Series="S"` → only `SERIES=S` is added, and `GROUPING`/`TITLE` are byte-identical.

**Verify**: `mise exec -- go test -race ./internal/flac/ -run VC -v` → all PASS.

### Step 3: PICTURE ops (F4) and creating a missing VORBIS_COMMENT

Add `applyArtwork(m *fmeta, ops []types.ArtworkOp) error`, plus VORBIS_COMMENT creation when it
is missing. Tests (in `writer_test.go`). Each parses the output with `(&parser{}).ExtractArtwork`
and `Parse`:
- `TestPicture_ReplaceFront`: a source with front and back pictures → the front is replaced and
  the back block payload is byte-identical.
- `TestPicture_RemoveByType` and `TestPicture_RemoveAll`.
- `TestPicture_AddPlacement`: with no pictures, the new PICTURE comes after the last non-PADDING
  block and before PADDING.
- `TestPicture_NonASCIIMIMERefused`.
- `TestVC_CreatedWhenMissing`: a source with no VORBIS_COMMENT plus a Title edit → a new block
  directly after STREAMINFO, with vendor `audiometa`, and `Parse` reads the Title.
- `TestVC_MultipleBlocksRefused`: two VORBIS_COMMENT blocks plus a Title edit →
  `ErrUnsupportedWrite`. Two blocks plus an artwork-only edit → succeeds, and both blocks are
  byte-identical.

**Verify**: `mise exec -- go test -race ./internal/flac/ -run 'Picture|VC_' -v` → all PASS.

### Step 4: The `TagWriter` (F8, F9) and registration

`internal/flac/writer.go`:
- `type writer struct{}` with `WriteFile`, `AudioDigest` and `InPlacePatch`;
- `func init() { registry.RegisterWriter(types.FormatFLAC, &writer{}) }`.

Tests:
- `TestWriter_AudioVerbatim`: the bytes after the output `audioStart` equal the source audio.
- `TestWriter_Deterministic`.
- `TestWriter_InPlaceEqualsFullRewrite`: when it fits, patching a copy at offset 4 gives the
  `WriteFile` output byte-for-byte.
- `TestWriter_InPlaceNotOK`: padding is exhausted → `ok == false`.
- `TestWriter_AudioDigest`: equals `sha256(src[audioStart:])` for both corpus files.
- `TestWriter_Corpus`: for `testdata/sample.flac` and `sample_upper.flac`, apply a Title edit,
  `Parse` the output, and check that the Title changed, the Artist and Album did not, the
  artwork is byte-identical, and the SEEKTABLE payload is byte-identical (`sample_upper`).

**Verify**: `mise exec -- go test -race ./internal/flac/ -v 2>&1 | grep -c '^--- FAIL'` → `0`.

### Step 5: Enable the round-trip harness for FLAC

In `write_roundtrip_test.go`:
- Set `writable: true` on `sample.flac` and `sample_upper.flac`.
- Add FLAC-only edits (use the `formats` gate that 039 added):
  - `Set("REPLAYGAIN_TRACK_GAIN","-6.00 dB")`. Its check is
    `f.Audio.ReplayGain.TrackGain == -6.0`.
  - Typed `Lyrics = "la la"`. Its check is `Lyrics == "la la"`.
- Add `TestWriteRoundTrip_FLACKeepsAudioOffset`: after a Title edit on `sample.flac`, the
  output's audio start (walk the blocks) equals 8618, because the padding absorbed the change.

**Verify**: `mise exec -- go test -race -run 'WriteRoundTrip' -v .` → all PASS, and the FLAC
subtests run the edit loop.

### Step 6: Decoder oracle (local only)

For each edited FLAC output from Steps 4–5, write it to a temp file and run `flac -t -s <file>`
(decodes the whole stream and checks the STREAMINFO MD5) and `metaflac --list <file>`.
**Expected**: both exit 0, and `metaflac --show-tag=TITLE` prints the edited title. Record the
results in your report. Plan 042 adds the CI oracle.

**Verify**: all commands exit 0.

### Step 7: Full gates

**Verify**:
- `mise exec -- go test -race ./...` → all `ok`;
- `mise exec -- go test -run '^$' -fuzz FuzzParseFLAC -fuzztime 30s ./internal/flac/` → no
  crash;
- `mise exec -- golangci-lint run ./...` → `0 issues.`;
- `gofmt -l .` → empty.

## Test plan

- New test files:
  - `internal/flac/writer_test.go` (block model, padding, is-last, pictures, writer, corpus);
  - `internal/flac/vorbiswrite_test.go` (comment edits, field mapping, validation, chapters,
    vendor).
- Extended: `write_roundtrip_test.go` (FLAC rows writable, FLAC-only edits, audio-offset check).
- Patterns:
  - `internal/flac/fixtures_test.go` builders, used by `internal/flac/chapters_test.go` and
    `artwork_test.go`;
  - the MP3 writer tests from plan 039 (`internal/mp3/writer_test.go`) for writer-level
    structure.
- Command: `mise exec -- go test -race ./...` → all pass.

## Done criteria

- [ ] `mise exec -- go build ./...`, `go vet ./...` exit 0; `gofmt -l .` empty
- [ ] `mise exec -- go test -race ./...` all `ok`; FLAC rows in `TestWriteRoundTrip_Corpus` run the edit loop
- [ ] `mise exec -- golangci-lint run ./...` → `0 issues.`
- [ ] `grep -rn 'RegisterWriter(types.Format' internal/ | wc -l` → `2` (MP3, FLAC; no Ogg/Opus)
- [ ] `TestVC_NoCrossFieldWrites` passes (F0)
- [ ] `flac -t` and `metaflac --list` pass on every edited corpus output (recorded in report)
- [ ] No changes to `internal/flac/parser.go`, `cuesheet.go`, `internal/vorbis/` from this plan
- [ ] `plans/README.md` row for 040 updated

## STOP conditions

- `readFMeta` followed by `serialize` of either corpus file is not byte-identical in the
  metadata region.
- `flac -t` fails on any output. That is a structural bug. Do not ship.
- A seeded harness edit fails on FLAC because the reader does not read back what F3 writes.
  Report it; do not change `internal/vorbis`.
- You find yourself registering a writer for Ogg or Opus.

## Maintenance notes

- **Ogg Vorbis and Opus** share the Vorbis comment code, but writing them needs page
  re-segmentation, CRC32 recomputation and granule invariants. That is deliberately not
  attempted here. A future plan can reuse `vcomment` from `vorbiswrite.go`. Move it to
  `internal/vorbis` at that time.
- **Chapter writing** (`CHAPTERxxx` comments, CUESHEET) is a future plan. F3 refuses
  `CHAPTER<digit>…` keys so a raw `Set` cannot silently desynchronize chapters.
- **Reader quirk**: repeated `ARTIST` makes `Tags.Artist` the **last** value
  (`internal/vorbis/comments.go:47-49`). The Artist-only edit rule writes the new artist first,
  so on multi-artist files the re-read `Artist` may differ from what was set. Fix the reader
  (first value wins) in a follow-up, and then this caveat disappears.
- New PICTURE blocks carry depth/colors 0. If a strict consumer complains, compute the depth
  from the PNG IHDR or JPEG SOF (`internal/imagemeta`).
- Reviewer focus:
  - F5's padding arithmetic, in particular the case `0 < oldMetaLen - newLen < 4`, which must
    take the "grow" branch;
  - F7 (exactly one is-last).

## Maintainer addendum (2026-10-31): tag deletion

Applications must be able to delete any tag, including keys that map to no typed field. Plan 038 adds `Tags.Delete(keys ...string)`. A deletion arrives at this writer as a `KeyEdit` with `Values == nil`, so it uses the existing remove path with no insert. Add these tests and treat them as done criteria:

- **Typed key:** deleting a key that fills a typed field removes it from the file (MP3 `TIT2`, FLAC `TITLE`, M4A `©nam`). After re-open the field is empty, every other tag is unchanged, and the audio digest is unchanged.
- **Key with no typed field:** deleting such a key removes it from the file. The examples are an MP3 `TXXX` with a custom description, a FLAC/Vorbis `SHOW` comment, and an M4A `----` freeform item, plus `gnre` once the reader records it.
- **Every key:** deleting every key in `Tags.All()` produces a valid file. Re-open gives zero raw tags and the audio digest is unchanged. For MP3 this means an empty tag body with padding, or no ID3v2 tag at all; pick one, document it in godoc, and test it. FLAC keeps a VORBIS_COMMENT block with only the vendor string. M4A keeps an empty `ilst`.
- **Chapters:** deletion never touches chapter structures. MP3 CHAP/CTOC and the OverDrive MediaMarkers TXXX, FLAC `CHAPTERnnn`, and M4A `chpl` and the chapter track stay as they are. Deleting a chapter-bearing key returns `ErrUnsupportedWrite`, matching the read-only chapter rule.
