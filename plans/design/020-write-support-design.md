# 020: Write support design (spike)

Status: design only, contingent on the maintainer confirming GO in `020-write-support-decision.md`.
No file-mutating code exists or is proposed to land by this document. Nothing here was prototyped
(optional Step 7 skipped; see Prototype findings).

Facts verified at branch `fix/p2-spikes`: no serialization path exists
(`grep -rn --include='*.go' -E 'io\.Writer|os\.Create|os\.Rename|WriteAt|OpenFile'` over non-test code
returns nothing); `Open` uses read-only `os.Open`; parsers take `io.ReaderAt`; the corpus is in `testdata/`
with `audiometa_corpus_test.go` and `FuzzParseMP3/FLAC/M4A/Ogg`.

## First format

Compare M4B (ilst) and MP3 (ID3v2). FLAC is noted at the end as a candidate the plan did not list.

| Axis | M4B/M4A (ilst) | MP3 (ID3v2) |
|---|---|---|
| Audiobook share | Dominant container for commercial audiobooks | Common for ripped and podcast content; chapters via CHAP/CTOC |
| Where tags live | `moov/udta/meta/ilst`, in the middle of the file | File start, then optional padding, then frames |
| Growth cost | Growing `ilst` grows `moov`. If `moov` precedes `mdat` (faststart), every `stco`/`co64` entry in every `trak`, including the chapter text track (`internal/m4a/chapters.go`, `parseChunkOffsets`), shifts by the delta, or audio becomes unplayable. A `free` atom beside `ilst` absorbs growth without moving `mdat`. | A tag that fits in existing padding rewrites in place. Otherwise the file is re-streamed: new tag, then the audio bytes copied verbatim. No offset tables inside MP3 frames reference the tag. |
| Size encoding | 32-bit size or 64-bit extended size; nested sizes must all be fixed up (`ilst`, `meta`, `udta`, `moov`) | v2.3 plain 32-bit frame sizes vs v2.4 synchsafe (`decodeFrameSize`); tag header size is synchsafe; optional unsynchronisation |
| Existing read helpers | `readAtomHeader(sr, offset, parentEnd)`, `findAtom`, `extractIlstMetadata`, freeform `----` parsing | `parseID3v2Frames`, `readSingleFrame`, `decodeSynchsafe` |
| Corruption mode | Silent: file opens but audio offsets are wrong, many players seek to garbage | Loud: worst case tag unreadable; audio payload untouched |

**Pick: MP3 (ID3v2) first**, M4B second. Justification: the structural risk is an order of magnitude lower
(no chunk-offset tables, audio is an opaque suffix that can be hashed and compared), so it is the right place
to prove the harness, atomic-replace and byte-preservation machinery. The cost of the pick is that the
audiobook-dominant container comes second; the maintainer may choose to invert this, in which case v1 of M4B
must be restricted to "new `ilst` fits in adjacent `free` or full rewrite only when `moov` follows `mdat`;
otherwise return `ErrWriteNeedsRelayout`", with `stco`/`co64` shifting deferred to v2.

FLAC note: FLAC is arguably easiest of all (metadata blocks with a PADDING block, no offset tables, native
in-place edit when it fits). It is a strong third candidate; it shares the same machinery as MP3 and should
follow MP3 before M4B.

## API

Three options evaluated.

**A. `func (f *File) Save() error`**

```go
f, _ := audiometa.Open("book.mp3")
f.Tags.Set("title", "New")
err := f.Save() // mutates the path in place
```

Needs the file reopened read-write (today `File` holds a read-only `*os.File`), invites mutating the file we
are still reading from (the parser holds `io.ReaderAt` over it), and gives no way to test without touching
disk. Rejected.

**B. `func (f *File) WriteTo(w io.Writer) (int64, error)`**

```go
f, _ := audiometa.Open("book.mp3")
f.Tags.Set("title", "New")
tmp, _ := os.CreateTemp(filepath.Dir(path), ".audiometa-*")
_, err := f.WriteTo(tmp) // complete new file stream; original untouched
```

Produces a full new stream from the open reader; the caller decides destination. Safest: composes with
temp-file-then-rename, trivially testable with `bytes.Buffer`, original is never at risk. It cannot do
in-place padding reuse, which is fine (in-place is an optimisation, see next section). Implements
`io.WriterTo`, a standard interface.

**C. `func Write(path string, tags *Tags, opts ...WriteOption) error`**

```go
err := audiometa.Write("book.mp3", tags, audiometa.WithBackup())
```

Stateless convenience. Re-parses the file, so the caller's `Tags` may disagree with the file on disk
(stale edits). Fine as a thin wrapper, wrong as the primitive.

**Pick: B as the primitive, C as a convenience built on it** (`Write` = open, apply, `WriteTo` temp file,
fsync, rename). `Save()` is not offered. Context variants (`WriteToContext`) mirror `ExtractArtworkContext`.

**Writable fields in v1 (MP3):** the typed text fields (`Title`, `Artist`, `Album`, `AlbumArtist`, `Genre`,
`Year`/date, `Narrator`, `Series`, `SeriesPart`, `Comment`) and track/disc numbers. Not in v1: artwork
writes, chapters (CHAP/CTOC are preserved verbatim, not editable), ReplayGain, MusicBrainz IDs (preserved as
unknown frames, not editable).

**Source of truth.** Today `Tags.Set` writes only `raw`. v1 rule: **the `raw` map is the single source of
truth for writes, and typed fields are derived views.** Concretely, before the writer lands, `Set` must also
update the typed field (via the same key-to-field mapping the parsers use) so the two cannot diverge, and
the writer reads only `raw`. Reasoning: `raw` is lossless and key-addressed (multi-value, unknown keys),
typed fields are lossy projections. A writer driven by typed fields would drop everything the typed struct
cannot express. A design alternative ("typed fields win, `raw` is read-only") is rejected for that reason.
Implementation requires a round-trip test: `Set(k, v)` then reading the typed field returns `v`.

## In-place vs full rewrite

- **In place** only when all hold: the new serialised tag is no larger than old tag plus its padding, the
  file is a regular file the caller opened read-write via the `Write` convenience, and the audio offset does
  not change. Then overwrite the tag region and re-pad with zeros to the old size. No temp file is needed for
  the audio, but a crash mid-write can leave a half-written tag, so in-place is **opt-in** (`WithInPlace()`)
  and off by default.
- **Full rewrite** (default): write new tag, then copy the audio suffix with `io.Copy` from the original
  `ReaderAt` (`io.NewSectionReader`) into the destination. For MP3 add generous padding (e.g. 2 KiB or 10%)
  so a later edit can use in-place.
- ID3v2 output version: always write v2.4 (UTF-8 text, synchsafe sizes) unless the input was v2.3, in which
  case v1 preserves v2.3 to avoid breaking old players (see Open questions).

## Atomicity

For the `Write(path, ...)` convenience:

1. Create a temp file in the **same directory** (`os.CreateTemp(dir, ".audiometa-*")`), so `rename` is on one
   filesystem and atomic.
2. Copy the original's mode bits (`os.Chmod`), and where supported uid/gid and mtime policy (default: keep
   mtime off, bump it; `WithPreserveMTime()` opt-in).
3. Stream via `WriteTo`; `Sync()` the temp file; verify (read back with the library, compare audio hash).
4. `os.Rename(tmp, path)`; fsync the directory.
5. Never truncate or open the original for writing. On any error remove the temp file and return; the
   original is untouched.
6. `WithBackup()` hard-links or copies the original to `path.bak` first.

`WriteTo` alone has no atomicity responsibilities: the caller owns the destination.

## Failure modes

| Failure | Mitigation |
|---|---|
| Disk full mid-write | Write goes to temp file; error surfaces on `Write`/`Sync`; temp removed; original intact |
| Crash or power loss before rename | Original untouched; stale `.audiometa-*` temp left; documented, safe to delete; opt-in cleanup helper |
| Cross-device temp (temp dir on another filesystem) | Temp is always created beside the target; if the directory is not writable return `ErrDirNotWritable` rather than fall back to `/tmp` and a non-atomic copy |
| Concurrent reader | Rename is atomic: readers see old or new, never partial. A reader holding the old fd keeps the old inode |
| Concurrent writer | Not protected; document single-writer. Optional advisory lock (`flock`) on Unix in v2. Detect change: compare size+mtime captured at open against `stat` before rename, abort with `ErrConcurrentModification` |
| Unknown frames/atoms | Preserved byte-for-byte: parse into a frame list retaining raw bytes and order; only touched keys are re-encoded |
| Encrypted/DRM content (e.g. Audible AAX, DRM `drms`/`enca` atoms, ID3 encrypted frames) | Detect and refuse with `ErrDRMProtected`; never rewrite. Audio is not touched regardless |
| Symlinks | Resolve and replace the target, not the link (`filepath.EvalSymlinks`), preserving the link |
| Read-side misparse | Writer must re-emit the original frame bytes for any frame it did not intentionally edit; verify post-write audio hash and re-read equality before rename |
| Read-only filesystem / permissions | Fail before any write; temp creation is the probe |
| Tag larger than format limit (ID3v2 256 MiB, FLAC block 16 MiB) | Validate before writing; return `ErrTagTooLarge` |
| Non-UTF-8 or invalid input strings | Validated on `Set`/at write; reject, never emit invalid encoding |

## Verification

Round-trip harness over `testdata/`, run in CI as a **release gate** before any release mentions writes.
Designed here, to live in a `_test.go` in the root package when write code exists (not implemented now).

For each corpus file with a supported format:

1. Open, record `Tags`, `Chapters`, `Audio` (duration, bitrate), and SHA-256 of the audio payload region
   (MP3: bytes after the ID3v2 tag plus padding; M4B: the `mdat` payload; FLAC: after the last metadata block).
2. `WriteTo` into a buffer without changes.
3. Re-open the buffer: `Tags.Equal` true, chapters identical, duration equal, audio hash identical.
4. Edit loop: for a table of edits (set title, clear narrator, add multi-value artist, long Unicode, empty
   value removal), apply, write, re-open; assert the edit is visible, **all other tags unchanged**, audio hash
   identical.
5. Unchanged-write idempotence: writing twice yields byte-identical output (determinism).
6. External oracle (skipped if absent): `ffprobe -v error -show_format -show_streams out` exits 0, and
   ffprobe's tags equal ours; for MP3 additionally `id3v2 -l`/`mediainfo` smoke.
7. Unknown-frame preservation: inject a synthetic unknown frame into a copy of a corpus file, round-trip,
   assert the frame bytes are present and unchanged.

Fuzz idea (`FuzzWriteRoundTrip`): take corpus seeds and a random sequence of `Set`/`Delete` operations with
arbitrary key and value bytes; invariants: never panics; if `WriteTo` returns nil, the output re-opens
without error and **the audio hash equals the input's audio hash**; if it returns an error, no partial output
is required but the input must be unmodified (it is read-only by construction). Also fuzz the input file
(existing `FuzzParse*` seeds) through `WriteTo` to catch parser/writer disagreement.

## Prototype findings

Skipped. Step 7 is optional; no throwaway code was written. Nothing in the repository mutates files.
The first finding the prototype should produce, when someone builds it on a never-merged branch under
`internal/write/`, is whether the existing `parseID3v2Frames` can retain raw frame bytes cheaply or
whether it needs a second "frame list" parse pass (the current reader decodes and discards the raw bytes).

## Open questions

- ID3v2 output version: write v2.4 only, or preserve the input version (v2.3 is still the most compatible with older hardware and iTunes)?
- Preserving unknown frames and their order, including frames with unsynchronisation, compression, or grouping flags, when re-emitting under a different tag version.
- Writing chapters: ID3 CHAP/CTOC, MP4 `chpl` plus text-track, Vorbis `CHAPTERxx` keys; which are editable in v2, and how do chapter edits keep the text-track `stco` consistent?
- Artwork writes: size cap (align with `WithMaxArtworkSize`), format conversion (never), multiple images, picture types.
- FLAC/Vorbis: reuse the PADDING block, vendor string preservation, and `Ogg` page re-pagination plus CRC recomputation on rewrite; Opus header CRCs and granule-position invariants.
- How `WithStrictParsing` and any validation surface (plan 022) interact with write preconditions: should a file with error-level warnings be writable, or refused?
- `Tags.Set` semantic cleanup: typed-field update on `Set`, handling of keys that map to multiple frames (`TXXX`/`----` freeform), case rules across formats.
- Version/compat promise: is the write API marked experimental until 1.1, and how are on-disk output changes (padding sizes, frame order) versioned?
- ID3v1 and APEv2 footers: preserve, update or strip? Same for Lyrics3.
- Timestamp policy: preserve mtime or bump it, since library managers detect changes by mtime.
- Windows semantics: `os.Rename` over an open file, antivirus locks, and directory fsync equivalents.
- Large files (multi-GB M4B): streaming copy memory profile; progress callbacks and `context` cancellation granularity.
