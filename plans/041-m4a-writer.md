# Plan 041: Write M4A/M4B metadata (ilst rebuild, free-atom reuse, chunk-offset shifting)

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report. Do not improvise. When done, update the status row for this plan
> in `plans/README.md`, unless a reviewer dispatched you and told you they
> maintain the index.
>
> **Precondition**: plans 038 (with maintainer API sign-off), 039 and 040 are DONE.
> `grep -rn 'RegisterWriter(types.Format' internal/ | wc -l` → `2`. Otherwise STOP.
>
> **Drift check (run first)**: `git diff --stat 0d60738..HEAD -- internal/m4a/ internal/testcorpus/generate.sh audiometa_corpus_test.go write_roundtrip_test.go`
> Expected drift:
> - plans 038–040 edited `write_roundtrip_test.go`, and 039 edited `generate.sh` and
>   `audiometa_corpus_test.go`;
> - **plan 016** replaced the narrator/series fallbacks in `internal/m4a/audiobook.go`
>   (`parseAudiobookTags`) with `internal/parsing` helpers. A **no-inference pass** then deletes
>   those fallbacks entirely, including `resolveSeriesPart`'s title/album/path guessing. Do not
>   rely on them; the SeriesPart bindings must match whatever direct key mappings remain. It also moved the image sniffing in
>   `internal/m4a/artwork.go` to `internal/imagemeta`, and moved chapter end times into
>   `types.FillChapterEndTimes`;
> - a **Go 1.27 idioms pass** may have turned `binary.Read[T](sr, off, what)` into
>   `sr.Read[T](off, what)`.
>
> Write new code in whatever form is live. Beyond that, a mismatch in the `readAtomHeader`,
> `findIlst`, `parseMetadataTag`/`mapTagToField` or `parseChunkOffsets` excerpts is a STOP.

## Status

- **Priority**: P2
- **Effort**: L
- **Risk**: HIGH. A wrong chunk offset produces a file that opens but plays garbage, and that
  failure is silent. The mitigations:
  - offsets only ever shift upward, by one delta, and only for offsets past the edited region;
  - any offset that points into the edited region is a refusal;
  - stco overflow is a refusal;
  - the audio digest (all `mdat` payloads) and the chapters are verified;
  - an ffmpeg decode oracle runs on faststart and non-faststart fixtures.
- **Depends on**: plans/038-write-api-foundations.md, plans/039-mp3-id3v2-writer.md, plans/040-flac-writer.md
- **Category**: direction
- **Planned at**: commit `0d60738` (branch `fix/improve-plans`), 2026-10-31

## Why this matters

M4B is the dominant commercial audiobook container, and it is the last approved format
(`docs/design/020-write-support-decision.md`, "Decision"). It is also the riskiest. The tags live
in `moov/udta/meta/ilst`, in the middle of the file. Growing them grows `moov`, and when `moov`
precedes `mdat` ("faststart") every chunk offset in every track has to move. That includes the
chapter text track, whose samples hold the chapter titles. The design doc puts it this way: "If
`moov` precedes `mdat` (faststart), every `stco`/`co64` entry in every `trak`, including the
chapter text track, … shifts by the delta, or audio becomes unplayable."

This plan rebuilds only `ilst`, preserves every other atom, and keeps the audio offsets
unchanged whenever adjacent free space allows. Otherwise it shifts all offsets by one exact delta.

## Current state

Excerpts are from `0d60738`.

**Atom header and bounds** (`internal/m4a/atoms.go:53-104`). `readAtomHeader(sr, offset,
parentEnd)` handles `size32 == 1` (a 64-bit size at `+8`, header 16 bytes). It rejects sizes
below the header and atoms that overrun `parentEnd`. `size32 == 0` (to EOF) is rejected.
`Atom.DataOffset()`/`DataSize()` account for the 8- or 16-byte header (`:25-44`).
`findAtom`/`findAtomContext` scan siblings (`:107-135`).

**ilst path** (`internal/m4a/parser.go:121-137`, `findIlst`): moov → udta → meta → ilst. `meta`
is treated as a full atom, so its children start at `DataOffset()+4`:

```go
	// meta is a full atom: 4 bytes of version+flags precede its children.
	metaDataOffset := metaAtom.DataOffset() + 4
	metaDataEnd := metaAtom.DataOffset() + int64(metaAtom.DataSize())

	return findAtomContext(ctx, sr, metaDataOffset, metaDataEnd, "ilst")
```

**How ilst is read** (`internal/m4a/metadata.go`):
- `parseMetadataTag` (`:41-…`) reads the **first** `data` child only. Data payload layout:
  `version(1) type(3) locale(4) value`. Type 0/1 is text; 21/22 is a big-endian
  signed/unsigned integer of 1–8 bytes, decoded by `decodeBEInteger` (`:23`). Other types are
  ignored.
- `extractIlstMetadata` (`:105-…`) dispatches by type:
  - `trkn`/`disk` go to `parseTrackNumber` (`:224`), which skips 2 bytes and then reads
    `uint16 number` and `uint16 total`;
  - `covr` and `----` are skipped (`:133`);
  - everything else goes to `mapTagToField(type, value, file)` (`:182`), which calls
    `AddRaw(type, value)` and assigns via `ilstTextFields` (`:159`). For example `©ART` also
    appends to `Artists`.
- Freeform `----` items (`internal/m4a/audiobook.go:80-142`, `parseCustomAtomWithTags`) have
  `mean`, `name` (each a full atom: 4 bytes of version/flags, then the string) and `data`.
  **The last** `data` wins. The raw key is `strings.ToUpper(name)` (`:71-77`).
- `covr` (`internal/m4a/artwork.go:19-…`, `parseCovrData` `:74`): each `data` child is one image.
  The type byte maps 13 → JPEG, 14 → PNG, 27 → BMP (`flagsToMIMEType` `:124`). **Every image is
  reported as `ArtworkFrontCover`** (`:116`).

**Chunk offsets** (`internal/m4a/chapters.go:554-…`, `parseChunkOffsets`): `stco` (version/flags
4 bytes, `uint32` count, `uint32` entries) or `co64` (`uint64` entries) inside
`trak/mdia/minf/stbl`. Chapter titles come from the chapter text track's samples
(`parseQuickTimeChapters` via `tref/chap`, `:158-…`). Nero `chpl` lives in `moov/udta`
(`:50-…`).

**Corpus layouts** (verified by walking the atoms):
- `testdata/sample.m4b`: `ftyp(28) free(8) mdat(601) moov(2061)`. **moov is after mdat (not
  faststart).**
  - moov holds trak(audio) with `tref/chap`, trak(text, chapter track), and udta.
  - udta holds meta(hdlr 33 bytes, ilst with `©nam ©ART ©alb ©too covr`) and `chpl(57)`.
  - Each trak's stbl has `stco`.
- `testdata/sample.m4a`: `ftyp free mdat moov`. Not faststart, no chapters, no covr.
- ffmpeg 9 with `-movflags +faststart` produces `ftyp(28) moov free(8) mdat`: an 8-byte top-level
  `free` directly after `moov` (verified).

**Test builders**: `createMockAtom(type, data)` (`internal/m4a/atoms_test.go:12-26`) writes an
8-byte header plus data. `audiometa_test.go:18` has `createSimpleM4B()`.

**Foundations** (plans 038–040):
- `types.EditSet`, `types.FieldBindings(types.FormatM4A)`;
- `types.ErrUnsupportedWrite`, `ErrDRMProtected`, `ErrTagTooLarge`;
- `registry.TagWriter` / `InPlaceWriter` / `RegisterWriter`;
- `writeutil`;
- the "source key" field rule (plan 038 D2);
- the writer structure in `internal/mp3/writer.go` and `internal/flac/writer.go`.

## Decisions (binding)

**M0. No cross-field inference (maintainer rule, 2026-10-31).** The application decides what tags
mean, and the library never derives one field from another. Only the items representing fields
the caller set or cleared may change. For example, writing Narrator never touches `©wrt`, and
writing Series never touches `©grp`, `©nam` or `©alb`.

**M1. Structure checks (refuse before writing).**
- Exactly one top-level `moov`. Zero or several → `*types.CorruptedFileError`.
- A top-level `moof`, or a `moov/mvex` (fragmented MP4) → `ErrUnsupportedWrite` ("fragmented
  MP4").
- **DRM**: for every `trak/mdia/minf/stbl/stsd`, read the entry count and walk every sample
  entry's fourcc. If any is in `{drms, drmi, enca, encv, enct, encs, aavd}` → `ErrDRMProtected`.
  These are Apple FairPlay, the ISO CENC protected entries, and Audible AAX.

**M2. moov model.** Read the whole `moov` into memory (`sr.CheckLen` first; the existing 256 MiB
cap applies). Parse it into a tree of `matom{typ string; ext bool; full []byte; children
[]*matom; payload []byte}`:
- Only these types are containers: `moov trak mdia minf stbl udta edts dinf tref` and `meta`
  (`full` = its 4 version/flags bytes) and `ilst`.
- Every other atom is a leaf whose `payload` is kept byte-for-byte.
- `ext` records a 16-byte (64-bit) header.
- Serialization recomputes every size bottom-up. It keeps a 64-bit header when `ext` was set, or
  when a 32-bit size would exceed `0xFFFFFFFF`.

So untouched atoms (`mvhd`, `tkhd`, `hdlr`, `chpl`, `tref`, `sgpd`, unknown `uuid`, …) come out
byte-identical, and every enclosing size (`ilst`, `meta`, `udta`, `moov`) is fixed up.

**M3. ilst edits.** Find `moov/udta/meta/ilst`. Missing levels are created when edits need them:
- `udta` is appended as the last moov child;
- `meta` is a full atom with version/flags 0, holding this exact 33-byte `hdlr`
  (`00000021 'hdlr' 00000000 00000000 'mdir' 'appl' 00000000 00000000 00`) followed by `ilst`.

ilst items keep their order. Edited items are replaced in place; new items are appended.

- **Item kinds**:
  - A **known item type** is in `knownTextItems = {©nam ©ART ©alb ©cmt ©grp ©lyr aART cprt ldes
    desc ©des ©gen ©wrt ©day ©mvn ©too ©st3 ©wrk soal soar soaa sonm soco sosn tvsh tven tvnn
    catg keyw purl}`, written as bytes with `©` = `0xA9`. Any 4-byte type of an existing
    non-`----` item in the original ilst also counts.
  - Any other raw key is **freeform**.
- **KeyEdit on an item type `K`**:
  - Remove the item(s) of type `K`.
  - If there are values, write one item with **one `data` child per value**. For text the data
    type is 1, locale 0, value UTF-8. When the existing item's first data atom had type 21 or 22
    (an integer), encode each value as a big-endian integer of the original width. A non-integer
    or out-of-range value → `ErrUnsupportedWrite`.
  - Data types other than 0, 1, 21 or 22 on an existing item being edited → `ErrUnsupportedWrite`.
- **KeyEdit on a freeform key `K`**:
  - Remove every `----` item whose `name` `strings.EqualFold` `K`. The `mean` does not matter.
  - Write one `----` item with mean `com.apple.iTunes` (full atom), name = the original spelling
    of the first removed name or `K` (full atom), and one `data` (type 1) per value.
- **FieldEdit**: use plan 038's **"source key"** rule over `FieldBindings(types.FormatM4A)`.
  Setting writes only the highest-precedence present candidate, or the canonical key when none is
  present. Clearing removes every present candidate. The canonical keys are:
  - `Title ©nam`, `Artist/Artists ©ART`, `Album ©alb`, `Comment ©cmt`, `Grouping ©grp`,
    `Lyrics ©lyr`, `AlbumArtist aART`, `Copyright cprt`, `Description ldes`, `Genres ©gen`,
    `Composers ©wrt`;
  - `Date/Year ©day`. For Year, replace the leading 4 digits when the present value is longer;
    otherwise use `%04d`;
  - `Series ©mvn`;
  - `SeriesPart`: the target is chosen by the source-key rule. When the target is `©mvi` (an
    integer item), the value must be an integer 1–65535, written as data type 21 with 2 bytes.
    Otherwise return `ErrUnsupportedWrite`. Never silently move the value to another key (M0).
    When no candidate is present, the canonical key is `©mvi` for an integer value and freeform
    `SERIES PART` otherwise;
  - freeform `SUBTITLE`, `NARRATOR`, `PUBLISHER`, `ISBN`, `ASIN`, `LANGUAGE`;
  - typed-only `TrackNumber/TrackTotal → trkn`: data type 0, 8 bytes `00 00 nn nn tt tt 00 00`;
  - typed-only `DiscNumber/DiscTotal → disk`: data type 0, 6 bytes `00 00 nn nn tt tt`.
    For trkn and disk, both values 0 removes the item, and a value over 65535 →
    `ErrUnsupportedWrite`;
  - `Genres` edits do **not** touch a `gnre` item (an ID3v1 genre index). Under M0 it is a
    separate tag the caller did not edit; see the maintenance notes;
  - not writable for M4A (→ `ErrUnsupportedWrite`): `MusicBrainz*`, `Label`, `CatalogNumber`,
    `Barcode`, `OriginalDate`, `ISRC`, `Performers`.
- **Chapter carriers** (`chpl`, the chapter text track, `tref/chap`) never appear in `raw`, so
  they cannot be targeted. They are re-emitted per M2, with only M5's offset shift.
- **Values** must be valid UTF-8 → otherwise `ErrUnsupportedWrite`.

**M4. covr.** Picture types do not exist in `covr`:
- `Add`: `a.Type` must be `ArtworkFrontCover` or `ArtworkOther`, otherwise
  `ErrUnsupportedWrite` ("M4A cover art has no picture type"). The MIME must be `image/jpeg` (13),
  `image/png` (14) or `image/bmp` (27), otherwise `ErrUnsupportedWrite`. Append one `data` child
  to `covr`, creating `covr` at the end of ilst if needed.
- `Remove(ArtworkFrontCover)` and `RemoveAll` remove every covr image. `Remove` of any other type
  is a no-op, because no covr image is reported with another type.
- A `covr` left with no `data` children is removed. Untouched `data` children are byte-identical.

**M5. Layout, free-space reuse and offset shifting.**
- Let `moovStart` be moov's offset. Let `F` be the run of top-level `free`/`skip` atoms directly
  after moov (contiguous, possibly empty). `oldRegion = moov.size + sum(F)`, and
  `rEnd = moovStart + oldRegion`.
- The rebuilt moov drops every `free`/`skip` child of the edited `meta` and of `udta`; that space
  joins the slack. Let `N = len(serialized new moov)`.
  1. **Fits**: `N == oldRegion`, or `oldRegion - N >= 8` → write the new moov, then one top-level
     `free` of `oldRegion - N` bytes (none when 0). `delta = 0`. Nothing after `rEnd` moves.
  2. **moov is last**: nothing after `rEnd` except EOF → write the new moov with no free.
     `delta` is irrelevant, because no offsets point past it.
  3. **Grow**: otherwise, write the new moov, then a `free` of 2048 bytes.
     `delta = N + 2048 - oldRegion`, which is always > 0.
- **Shift**: when `delta != 0`, then **before serializing** (stco/co64 sizes don't change), go
  through every `trak` in moov, and for every `stco`/`co64` entry `o`:
  - `o >= rEnd` → `o + delta`;
  - `o < moovStart` → unchanged;
  - `moovStart <= o < rEnd` (an offset pointing into moov or the free run) →
    `ErrUnsupportedWrite`.
- **stco overflow**: an `stco` entry where `o + delta > 0xFFFFFFFF` → `ErrUnsupportedWrite`
  ("chunk offset overflow; co64 upgrade not supported"). **Decision: refuse; do not upgrade to
  co64 in v1.** Justification: the upgrade changes `stco` sizes, which changes moov, which changes
  delta. That fixed-point iteration is a bug farm, for files that need faststart plus more than
  4 GiB, which audiobooks rarely do. It is listed as a follow-up.
- **Output**: `src[0:moovStart]` verbatim, then the new moov, then the free (if any), then
  `src[rEnd:size]` verbatim.

**M6. In-place.** `InPlacePatch` returns `ok` only in M5 case 1 (`delta == 0`). The patch is the
new moov plus the free atom: exactly `oldRegion` bytes at `moovStart`. Everything else returns
`ok == false, nil`.

**M7. AudioDigest.** One SHA-256 over the payload of every top-level `mdat`, in file order
(the header is 8 or 16 bytes). This matches plan 038's `m4aAudioDigest`. In addition, the
top-level atom sequence excluding `moov`/`free`/`skip` must be unchanged. The harness checks
that with `m4aTopLevelTypes`.

**M8. Reader change, scoped to multi-value items.** `parseMetadataTag` reads only the first
`data` atom, so a multi-value `©ART` written per M3 would re-read as one value. Add
`parseMetadataValues(sr, tagAtom) ([]string, error)`, which returns every text or integer `data`
child, in order. In `extractIlstMetadata`'s default branch:
- call `mapTagToField(type, values[0], file)` exactly as today;
- for each further value, call a new `mapExtraValue(type, value, file)`. It only calls `AddRaw`
  and appends to the slice fields (`©ART`→Artists, `©gen`→Genres, `©wrt`→Composers).

Single-value files parse exactly as before, so the bench goldens must not change. Freeform `----`
multi-values stay "last wins" (unchanged; noted below).

## Commands you will need

| Purpose | Command | Expected |
|---|---|---|
| Build / vet | `mise exec -- go build ./... && mise exec -- go vet ./...` | exit 0 |
| Package tests | `mise exec -- go test -race ./internal/m4a/ -v -run 'Write\|MAtom\|Ilst\|Covr\|Shift\|DRM\|Digest\|MultiValue'` | all PASS |
| Harness | `mise exec -- go test -race -run 'WriteRoundTrip' -v .` | all PASS |
| Goldens | `mise exec -- go test -run TestBenchFixturesParse -v .` | PASS (no `-update`) |
| All | `mise exec -- go test -race ./...` | all `ok` |
| Fuzz smoke | `mise exec -- go test -run '^$' -fuzz FuzzParseM4A -fuzztime 30s ./internal/m4a/` | no crash |
| Decode oracle | `ffmpeg -v error -i <file> -f null - ; echo $?` | prints nothing, then `0` |
| Probe oracle | `ffprobe -v error -show_chapters -show_entries format_tags -of json <file>` | exit 0, chapters intact |
| Lint / format | `mise exec -- golangci-lint run ./...` / `gofmt -l .` | `0 issues.` / empty |

## Scope

**In scope**:
- `internal/m4a/writer.go` (create): M1, M5–M7, the `TagWriter` + `InPlaceWriter`
  implementation, and `init()` with `registry.RegisterWriter` for **both** `types.FormatM4A`
  and `types.FormatM4B`
- `internal/m4a/moovtree.go` (create): M2
- `internal/m4a/ilstwrite.go` (create): M3, M4
- `internal/m4a/metadata.go`: M8 only (add `parseMetadataValues` and `mapExtraValue`, and switch
  the default branch of `extractIlstMetadata`)
- `internal/m4a/writer_test.go`, `internal/m4a/moovtree_test.go`, `internal/m4a/ilstwrite_test.go`,
  `internal/m4a/write_fixture_test.go` (create); `internal/m4a/metadata_test.go` (add M8 tests)
- `internal/testcorpus/generate.sh` (two faststart commands)
- `testdata/sample_faststart.m4a`, `testdata/sample_faststart.m4b` (create, generated)
- `audiometa_corpus_test.go` (add the two fixtures)
- `write_roundtrip_test.go` (flip the M4A/M4B rows, add the faststart rows and M4A-specific
  checks)

**Out of scope**:
- Every other `internal/m4a` reader file (`atoms.go`, `parser.go`, `audiobook.go`,
  `chapters.go`, `technical.go`, `codecs.go`, `artwork.go`). Copy small helpers instead of
  editing them.
- Upgrading `stco` to `co64` (M5 refuses), and moving `moov` (no layout changes other than M5).
- Chapter editing (`chpl`, the text track): a future plan.
- Fragmented MP4, QuickTime `.mov`-style `meta` without version/flags (the reader doesn't
  support it either), and AAX/DRM (refused).
- Root `.go` files other than the two test files. README, CHANGELOG and CI belong to plan 042.

## Git workflow

- Branch: `advisor/041-m4a-writer`
- Commits: gitmoji, single line, no body, no attribution. Suggested:
  - `✨ Read every data atom of multi-value ilst items`
  - `✨ Add M4A/M4B ilst writer with chunk-offset shifting`
  - `✅ Add faststart M4A fixtures and enable M4A round-trip`
- Do NOT push or open a PR.

## Steps

### Step 1: M8, reading multi-value items

In `internal/m4a/metadata.go`, add `parseMetadataValues` (same per-atom rules as
`parseMetadataTag`, looped over every `data` child with `readAtomHeader(sr, off, end)`) and
`mapExtraValue`. Switch the default branch of `extractIlstMetadata` as M8 describes. Keep
`parseMetadataTag`, because tests call it (`metadata_test.go:70,91,307`).

Tests (`metadata_test.go`):
- `TestExtractIlst_MultiValueArtist`: a `©ART` item with two `data` children (`"A"`, `"B"`) →
  `Artist == "A"`, `Artists == [A B]`, `Get("\xA9ART") == [A B]`.
- `TestExtractIlst_MultiValueGenre`: likewise for `Genres`.

**Verify**:
- `mise exec -- go test -race ./internal/m4a/ -run 'MultiValue|Metadata' -v` → PASS;
- `mise exec -- go test -run TestBenchFixturesParse -v .` → PASS, with goldens unchanged. If the
  golden differs, STOP.

### Step 2: The moov tree (M2)

`internal/m4a/moovtree.go`:
- `readMoovTree(sr, moovAtom *Atom) (*matom, error)`;
- `(*matom).bytes() ([]byte, error)`, which recomputes sizes;
- `findPath(root, "udta", "meta", "ilst") *matom`;
- `traks(root) []*matom`.

Tests in `moovtree_test.go`:
- `TestMAtom_RoundTripCorpus`: for `testdata/sample.m4b` and `sample.m4a`, read moov and call
  `bytes()` → identical to the source moov bytes.
- `TestMAtom_ExtendedHeaderPreserved`: a synthetic moov whose `udta` uses a 16-byte header. Grow
  a leaf inside → `udta` still has `size32 == 1`, and its 64-bit size is correct.
- `TestMAtom_SizesFixedUp`: add 10 bytes to an ilst leaf → `ilst`, `meta`, `udta` and `moov`
  sizes each grow by 10.

**Verify**: `mise exec -- go test -race ./internal/m4a/ -run MAtom -v` → PASS.

### Step 3: ilst and covr edits (M3, M4)

`internal/m4a/ilstwrite.go`: `applyIlstEdits(root *matom, edits *types.EditSet) error` and
`applyCovr(ilst *matom, ops []types.ArtworkOp) error`. Split them so each function stays under
`gocyclo` 15. Add a synthetic-MP4 builder in `write_fixture_test.go`:

```go
// buildMP4(faststart bool, ilstItems ...[]byte) []byte
```

It builds `ftyp`, a moov with one sound trak whose `stco` points at the mdat payload, plus
`udta/meta(hdlr,ilst)`, plus an `mdat` holding 256 recognizable bytes. The order depends on
`faststart`, and faststart adds an 8-byte `free` after moov. Also add `ilstText(type, values...)`,
`ilstFreeform(name, value)` and `covrItem(images...)`.

Tests (`ilstwrite_test.go`). Each re-parses the output with `(&parser{}).Parse` and
`ExtractArtwork`:
- `TestIlst_TextKeyEdit`: `Set("\xA9nam","New")` → Title is New, and every other item is
  byte-identical.
- `TestIlst_MultiValue`: `Artists = [A B]` → two `data` children, and the re-read gives `[A B]`.
- `TestIlst_Freeform`:
  - `Set("NARRATOR","X")` with an existing `----`/`Narrator` → the name keeps its case `Narrator`;
  - with none → a new item with mean `com.apple.iTunes` and name `NARRATOR`.
- `TestIlst_FieldEdits`, a table:
  - Track 3/12 → `trkn` bytes `00 00 00 03 00 0C 00 00`;
  - Disc 1/2 → `disk` 6 bytes;
  - Year on `©day "2021-05-03"` → `"1999-05-03"`;
  - SeriesPart `"4"` with no candidate present → `©mvi` int16 4;
  - SeriesPart `"4.5"` with no candidate present → freeform `SERIES PART` = 4.5;
  - SeriesPart `"4.5"` with `©mvi` present → `ErrUnsupportedWrite`;
  - Genres with a `gnre` present → the `gnre` item is byte-identical;
  - `ISRC` → `ErrUnsupportedWrite`.
- `TestIlst_CreatesMissingPath`: moov without udta plus a Title edit → `udta/meta/hdlr/ilst`
  created with the exact 33-byte hdlr, and the re-read Title is correct.
- `TestCovr_Ops`:
  - ReplaceArtwork(front PNG) → one image, the PNG;
  - RemoveAllArtwork → no `covr`;
  - AddArtwork(BackCover) → `ErrUnsupportedWrite`;
  - AddArtwork(`image/gif`) → `ErrUnsupportedWrite`;
  - untouched covr data is byte-identical when only text changes.
- `TestIlst_NoCrossFieldWrites` (**M0**): the source has `©wrt "C"`, `©grp "Saga #5"` and
  `©nam "Book 3"`. Then:
  - `Narrator="N"` → `©wrt` is byte-identical, and the only change is an added freeform
    `NARRATOR`;
  - `Series="S"` → `©grp`/`©nam` are byte-identical.
- `TestIlst_IntegerItemEdit`: a `tmpo` item with type 21 and 2 bytes, then `Set("tmpo","120")` →
  type 21, 2 bytes `00 78`. `Set("tmpo","fast")` → `ErrUnsupportedWrite`.

**Verify**: `mise exec -- go test -race ./internal/m4a/ -run 'Ilst|Covr' -v` → PASS.

### Step 4: Layout, shifting, DRM, and the writer (M1, M5–M7)

`internal/m4a/writer.go`:
- `type writer struct{}` with `WriteFile`, `AudioDigest` and `InPlacePatch`;
- helpers `structureCheck` (M1), `planLayout` (M5: returns the case, `delta` and the free size),
  and `shiftChunkOffsets(root, moovStart, rEnd, delta) error`;
- `func init() { w := &writer{}; registry.RegisterWriter(types.FormatM4A, w); registry.RegisterWriter(types.FormatM4B, w) }`.

Tests (`writer_test.go`):
- `TestLayout_FitsInFree`: faststart, Title shortened by 3 bytes → `delta == 0`, the output
  length equals the input length, the mdat offset is unchanged, and the free grew by 3.
- `TestLayout_ExactFit`: faststart, grow by exactly 8 (free consumed) → no free, `delta == 0`.
- `TestLayout_GrowShiftsOffsets`: faststart, grow by 100 → for every trak, every chunk offset
  equals the old offset plus `delta`. Also, the 256 mdat bytes read at the new stco offset equal
  the original.
- `TestLayout_Remainder1to7Grows`: grow so that `0 < oldRegion-N < 8` → the grow branch,
  with `delta == 2048 - (oldRegion-N)`.
- `TestLayout_MoovLast`: non-faststart, grow by 100 → no free, and the offsets are unchanged.
- `TestShift_Co64`: a synthetic trak using `co64` → its entries shift.
- `TestShift_StcoOverflowRefused`: an stco entry `0xFFFFFF00` past moov, plus growth → 
  `ErrUnsupportedWrite`.
- `TestShift_OffsetIntoMoovRefused`: an stco entry pointing into moov, plus growth →
  `ErrUnsupportedWrite`.
- `TestStructure_DRMRefused`: stsd entries `drms`, `enca` and `aavd` → `ErrDRMProtected`.
- `TestStructure_FragmentedRefused`: a top-level `moof` → `ErrUnsupportedWrite`.
- `TestWriter_InPlaceEqualsFullRewrite`: case 1 → applying the patch at `moovStart` to a copy
  gives `WriteFile`'s output.
- `TestWriter_InPlaceNotOK`: the grow case → `ok == false`.
- `TestWriter_AudioDigest`: equals the oracle for `sample.m4a` and `sample.m4b`.
- `TestWriter_Deterministic`.
- `TestWriter_ChapterTrackPreserved`: on `testdata/sample.m4b`, a Title edit → the re-parse has
  the same 2 chapters (titles and times), and the `chpl` and `tref` payloads are byte-identical.

**Verify**: `mise exec -- go test -race ./internal/m4a/ -v 2>&1 | grep -c '^--- FAIL'` → `0`.

### Step 5: Faststart fixtures

Append to `internal/testcorpus/generate.sh`, before `echo "Generated:"`:

```sh
# Faststart variants (moov before mdat) for write-path offset shifting.
# shellcheck disable=SC2086
$FF $SILENCE -i "$TMP/cover.jpg" -i "$TMP/chapters.txt" -map 0:a -map 1:v -map_metadata 2 \
	-c:a aac -c:v copy -disposition:v attached_pic -movflags +faststart \
	-metadata title="Test Title" -metadata artist="Test Artist" \
	-metadata album="Test Album" -f ipod "$OUT/sample_faststart.m4b"

# shellcheck disable=SC2086
$FF $SILENCE -c:a aac -movflags +faststart \
	-metadata title="Test Title" -metadata artist="Test Artist" \
	-metadata album="Test Album" "$OUT/sample_faststart.m4a"
```

**Run only these two commands**, not the whole script. Set up `OUT`, `TMP`, `FF` and `SILENCE`,
`cover.jpg` and `chapters.txt` exactly as the script's header does. The `chapters.txt` heredoc
must be copied verbatim. Add both files to `TestOpen_Corpus` with
`wantFormat: audiometa.FormatM4A`, which matches `sample.m4b`'s existing row: ffmpeg's ipod
brand is detected as M4A. If `Open` reports `FormatM4B` for the faststart m4b, use that instead
and note it. Add `sample_faststart.m4b` to `TestOpen_Corpus_Chapters`.

**Verify**:
- `git status --short testdata/` → exactly the two new files;
- a top-level atom walk (the Python one-liner below) prints `ftyp moov free mdat` for both:
  ```sh
  python3 -I -c 'import struct,sys
  d=open(sys.argv[1],"rb").read();o=0
  while o<len(d):
    s,t=struct.unpack(">I4s",d[o:o+8]);print(t.decode(),end=" ");o+=s
  print()' testdata/sample_faststart.m4b
  ```
- `mise exec -- go test -run 'TestOpen_Corpus' -v .` → PASS.

### Step 6: Enable the round-trip harness for M4A/M4B

In `write_roundtrip_test.go`:
- Set `writable: true` on `sample.m4a` and `sample.m4b`. Add
  `{"testdata/sample_faststart.m4a", true}` and `{"testdata/sample_faststart.m4b", true}`.
- The seeded `AddArtwork(back cover)` edit already lists M4A/M4B as unsupported (plan 038).
- Add M4A-only edits (use the `formats` gate):
  - `Set("NARRATOR","Reader")` → its check is `Narrator == "Reader"`;
  - typed `TrackNumber=3, TrackTotal=12` → its check is `TrackNumber == 3 && TrackTotal == 12`.
- Add `TestWriteRoundTrip_M4AFaststartGrowth`: on `sample_faststart.m4b`, set `Comment` to 4 KiB
  (forces growth), then:
  - the audio digest is equal;
  - the chapters are equal;
  - `m4aTopLevelTypes` is equal;
  - when `ffmpeg` is on PATH, `ffmpeg -v error -i <out> -f null -` exits 0 with empty stderr.
    Skip with `t.Skip` when it is absent.

**Verify**: `mise exec -- go test -race -run 'WriteRoundTrip' -v .` → PASS, and the four M4A rows
run the edit loop.

### Step 7: External oracle (local)

For every M4A/M4B edited output (all four fixtures; a Title edit; the 4 KiB Comment growth edit;
`ReplaceArtwork`), run:
- `ffmpeg -v error -i <out> -f null -` → no output, exit 0;
- `ffprobe -v error -show_chapters -of json <out>` → the same chapter titles as the source.

Record the results in your report.

**Verify**: every command exits 0, and the chapters match.

### Step 8: Full gates

**Verify**:
- `mise exec -- go test -race ./...` → all `ok`;
- `mise exec -- go test -run '^$' -fuzz FuzzParseM4A -fuzztime 30s ./internal/m4a/` → no crash;
- `mise exec -- golangci-lint run ./...` → `0 issues.`;
- `gofmt -l .` → empty.

## Test plan

- New test files:
  - `internal/m4a/write_fixture_test.go` (synthetic MP4 builder);
  - `moovtree_test.go` (tree round-trip, sizes, extended headers);
  - `ilstwrite_test.go` (text, freeform, integer items, field mapping, path creation, covr);
  - `writer_test.go` (layout cases, shifting, overflow, DRM, fragmented, in-place, digest,
    chapter preservation).
- Extended:
  - `internal/m4a/metadata_test.go` (M8 multi-value read);
  - `write_roundtrip_test.go` (four M4A rows writable, faststart growth with the ffmpeg decode
    check);
  - `audiometa_corpus_test.go` (two faststart fixtures).
- Patterns:
  - `internal/m4a/atoms_test.go` (`createMockAtom`) and `internal/m4a/bounds_test.go`
    (synthetic atom files);
  - `internal/m4a/chapters_test.go` (chapter-track fixtures);
  - plan 039/040 writer tests.
- Command: `mise exec -- go test -race ./...` → all pass.

## Done criteria

- [ ] `mise exec -- go build ./...`, `go vet ./...` exit 0; `gofmt -l .` empty
- [ ] `mise exec -- go test -race ./...` all `ok`; `TestBenchFixturesParse` passes with goldens unchanged
- [ ] `TestWriteRoundTrip_Corpus` runs the edit loop for `sample.m4a`, `sample.m4b`, `sample_faststart.m4a`, `sample_faststart.m4b`
- [ ] `mise exec -- golangci-lint run ./...` → `0 issues.`
- [ ] `grep -rn 'RegisterWriter(types.Format' internal/ | wc -l` → `4` (MP3, FLAC, M4A, M4B)
- [ ] `TestIlst_NoCrossFieldWrites` passes (M0)
- [ ] ffmpeg decode and ffprobe chapter checks pass on every edited M4A output (recorded in report)
- [ ] `git diff 0d60738 --stat -- internal/m4a/` shows changes from this plan only in `metadata.go`, `metadata_test.go` and the new files (plus plan 016 and idioms-pass changes)
- [ ] `plans/README.md` row for 041 updated

## STOP conditions

- `readMoovTree` followed by `bytes()` of either existing corpus file is not byte-identical.
- Step 1 changes any bench golden.
- `ffmpeg -f null` reports any error on an edited output. That is a silent-corruption class bug.
  Do not ship.
- The chapters of an edited `sample.m4b` or `sample_faststart.m4b` differ from the source.
- A chunk offset points between `moovStart` and `rEnd` in a corpus file. That would mean a
  layout this plan did not anticipate.
- Generating the faststart fixtures modifies any existing `testdata/` file.

## Maintenance notes

- **Follow-ups deliberately deferred**:
  - stco → co64 upgrade (M5 refuses on overflow);
  - chapter editing (`chpl`, the text track, `tref`);
  - freeform multi-value read (the reader keeps the last `data`; the writer writes one `data` per
    value, which Mutagen and ffmpeg read as multi-value);
  - `©nrt` as a Narrator source (not read today);
  - writing MusicBrainz/ISRC/Label freeform atoms (needs the reader to map them first).
- **`gnre`**: a file can hold both `gnre` (an ID3v1 genre index) and `©gen`. Some players
  prefer `gnre`, so a Genres edit may look ineffective there. Under the no-inference rule the
  writer leaves it alone, and the caller can remove it explicitly with `Set("gnre")` (no values).
  This requires the reader to record `gnre` in `raw`, which it does not today; that is a
  follow-up.
- **Picture types**: M4A cannot store them. Only FrontCover/Other are accepted on add (M4),
  and every covr image re-reads as FrontCover. The maintainer may decide to accept any type and
  silently store it as a cover. That is a one-line change in `applyCovr` plus a test update.
- If a later plan adds in-moov chapter writing, the text track's samples live in `mdat`. Writing
  new titles means appending to (or relocating) mdat data and rewriting that track's
  `stsz`/`stco`. That is a much larger change than this plan.
- Reviewer focus:
  - `planLayout`'s three cases;
  - `shiftChunkOffsets`'s three-way comparison (`< moovStart`, inside, `>= rEnd`);
  - that `rEnd` is computed from the **original** layout, before any rebuild.

## Maintainer addendum (2026-10-31): tag deletion

Applications must be able to delete any tag, including keys that map to no typed field. Plan 038 adds `Tags.Delete(keys ...string)`. A deletion arrives at this writer as a `KeyEdit` with `Values == nil`, so it uses the existing remove path with no insert. Add these tests and treat them as done criteria:

- **Typed key:** deleting a key that fills a typed field removes it from the file (MP3 `TIT2`, FLAC `TITLE`, M4A `©nam`). After re-open the field is empty, every other tag is unchanged, and the audio digest is unchanged.
- **Key with no typed field:** deleting such a key removes it from the file. The examples are an MP3 `TXXX` with a custom description, a FLAC/Vorbis `SHOW` comment, and an M4A `----` freeform item, plus `gnre` once the reader records it.
- **Every key:** deleting every key in `Tags.All()` produces a valid file. Re-open gives zero raw tags and the audio digest is unchanged. For MP3 this means an empty tag body with padding, or no ID3v2 tag at all; pick one, document it in godoc, and test it. FLAC keeps a VORBIS_COMMENT block with only the vendor string. M4A keeps an empty `ilst`.
- **Chapters:** deletion never touches chapter structures. MP3 CHAP/CTOC and the OverDrive MediaMarkers TXXX, FLAC `CHAPTERnnn`, and M4A `chpl` and the chapter track stay as they are. Deleting a chapter-bearing key returns `ErrUnsupportedWrite`, matching the read-only chapter rule.
- **`gnre` is now in scope** (maintainer, 2026-10-31). Before the writer steps, make the reader record `gnre` in raw tags under the key `gnre`. Store the decoded ID3v1 genre index as a decimal string. Do not map it into `Genres`, because that would be inference. This lets apps see the item and delete it, and the deletion test above covers it.
