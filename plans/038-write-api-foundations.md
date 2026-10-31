# Plan 038: Add the stable write API, edit model, writer registry and atomic-replace helper (no format writer yet)

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report. Do not improvise. **This plan ends in a mandatory STOP (Step 14,
> API-freeze review).** Do not mark it DONE yourself. Set its row in
> `plans/README.md` to `IN PROGRESS (awaiting API sign-off)` and report. Only the
> maintainer moves it to DONE, and plan 039 must not start before that.
>
> **Drift check (run first)**:
> `git diff --stat 0d60738..HEAD -- file.go options.go errors.go tags.go artwork.go internal/types/ internal/registry/`
> Two passes are scheduled to land **before** this plan and will show up here.
> That is expected:
> - **Plan 016 (parser consolidation)** added `internal/parsing/fallbacks.go`
>   (`ResolveSeriesFallbacks`, `ApplyNarratorFallback`), `internal/imagemeta`
>   (`MIMEType`, `Dimensions`) and `types.FillChapterEndTimes` in `internal/types/chapter.go`.
> - **A no-inference pass (maintainer rule, see "No cross-field inference" below)** removes every
>   cross-field fallback from the readers: Narrator is no longer taken from Composer, and
>   Series/SeriesPart are no longer guessed from Grouping, path, title or album.
>   `ResolveSeriesFallbacks` and `ApplyNarratorFallback` are being **deleted**. Do not call them
>   or rely on them.
> - **A Go 1.27 idioms pass** turns `binary.Read[T](sr, off, what)` package functions into generic
>   methods `sr.Read[T](off, what)` on `*binary.SafeReader`, and converts benchmarks to `b.Loop`.
>
> Apart from those, compare the "Current state" excerpts against the live code. A mismatch in
> `file.go` (the `File` struct, `openReader`, `ExtractArtworkContext`), `internal/types/tags.go`
> (`Tags`, `Set`, `AddRaw`, `Clone`) or `internal/registry/registry.go` is a STOP condition.

## Status

- **Priority**: P2
- **Effort**: L
- **Risk**: MED. It adds new public API that is frozen from day one. It also makes one behavior
  change to an existing method: `Tags.Set` on a `Tags` obtained from `Open` now also updates
  typed fields. No file-mutating format code lands here.
- **Depends on**: plan 016 (must be DONE: this plan imports `internal/imagemeta`)
- **Category**: direction
- **Planned at**: commit `0d60738` (branch `fix/improve-plans`), 2026-10-31

## Why this matters

The maintainer approved metadata write support (decision recorded in
`docs/design/020-write-support-decision.md`, section "Decision"). The order is MP3, then FLAC, then
M4A/M4B. Ogg and Opus are deferred. Scope is text tags and artwork; chapters are preserved but not
editable. **The API is stable from day one**: the library is public with outside users, so every
exported symbol added here is a permanent promise, and no existing exported API may break.

This plan builds everything that is format-independent:
- the public surface (`File.WriteTo`, `Write`, options, sentinel errors, artwork edits);
- the rule that turns the caller's edited `Tags` into an unambiguous edit set;
- the internal writer registry that format writers plug into;
- the crash-safe file replacement;
- the round-trip test harness.

It ends with a review gate, so the surface can be frozen before plan 039 writes the first byte of
an MP3.

## Current state

All excerpts are from commit `0d60738`. Module `github.com/simonhull/audiometa`; `go.mod` says
`go 1.27.0`, so Go 1.27 APIs are allowed.

**`file.go`: the public `File` wraps a read-only reader and the parser** (`file.go:29-41`):

```go
type File struct {
	types.File

	reader  io.ReaderAt
	parser  FormatParser
	artwork []Artwork

	maxArtworkSize int  // from WithMaxArtworkSize; 0 = no limit
	ignoreWarnings bool // from WithIgnoreWarnings; also suppresses artwork-stage warnings

	artworkMu     sync.Mutex // guards artwork and artworkLoaded
	artworkLoaded bool
}
```

`openWithContext` (`file.go:72-127`) opens the path with `os.Open` (read-only), calls
`openReader`, and stores `reader: f` (the `*os.File`) and `parser: typesFile.parser`.
`openReader` (`file.go:207-236`) detects the format, finds the parser, parses it, and then sets the
file-level fields:

```go
	// Parse metadata; parsers check ctx at major boundaries.
	file, err := parseSafely(ctx, parser, r, size, path)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", format, err)
	}

	// Set file-level fields
	file.Path = path
	file.Format = format
	file.Size = size

	return &parsedFile{file: file, parser: parser}, nil
```

`parseSafely` (`file.go:180-187`) and `extractSafely` (`:190-197`) wrap parser calls in
`recover()` → `panicError`. New writer calls must get the same wrapper.

**Artwork is lazy and cached** (`file.go:276-299`). `ExtractArtworkContext` takes `artworkMu`. If
`artworkLoaded` is false it calls the parser's `ExtractArtwork` (through `extractSafely`), filters
by `maxArtworkSize`, and stores the result in `f.artwork`.

**`Tags`** (`internal/types/tags.go:17-54`) has one unexported field, `raw map[string][]string`,
and 35 exported typed fields. `Set` writes only `raw` (`:154-165`):

```go
func (t *Tags) Set(key string, values ...string) {
	if t.raw == nil {
		t.raw = make(map[string][]string)
	}

	if len(values) == 0 {
		delete(t.raw, key)
		return
	}

	t.raw[key] = slices.Clone(values)
}
```

`AddRaw` (`:170-175`) is what parsers use; it must keep its current behavior. `Clone`
(`:308-363`) copies every field. `Equal` (`:374-427`) compares every typed field plus `raw`.
The root package re-exports the type with `type Tags = types.Tags` (`tags.go:8`). Any exported
method added to `types.Tags` therefore becomes public API. **Add only unexported methods or
package-level functions in `internal/types`.**

No library code calls `Tags.Set`; the only callers are tests (`grep -rn '\.Set(' internal file.go`
prints only doc comments).

**Raw keys are format-native.** This is the fact the edit model is built on:
- MP3 (`internal/mp3/id3v2.go:480-482`, `:600-603`): text frames are stored by frame ID
  (`TIT2`, `TPE1`, …), and TXXX by the **upper-cased** description (`NARRATOR`).
  COMM, APIC and CHAP are not in `raw`.
- FLAC/Ogg (`internal/vorbis/comments.go:38`, `:154`): the upper-cased Vorbis field name
  (`TITLE`).
- M4A (`internal/m4a/metadata.go:474`, `internal/m4a/audiobook.go:73-77`): the 4-byte atom type
  (`"\xA9nam"`), or the upper-cased freeform `----` name (`NARRATOR`). `trkn`/`disk` are not in
  `raw`.

**Registry** (`internal/registry/registry.go:12-47`): `FormatParser`, the optional
`ArtworkExtractor`, and `Register`/`Get` over a mutex-guarded map. Format packages register
themselves in `init()`. The root aliases `FormatParser`/`ArtworkExtractor` publicly
(`file.go:427-433`). The new writer interfaces must **not** be aliased publicly.

**Options pattern** (`options.go:113`): `type Option func(*openOptions)`.

**Errors** (`errors.go`, `internal/types/errors.go`): the root re-exports
`types.CorruptedFileError` and others as aliases. There are no sentinel errors today.

**Design constraints you must honor** (quoted from `docs/design/020-write-support-design.md`):
- "Pick: B as the primitive, C as a convenience built on it (`Write` = open, apply, `WriteTo`
  temp file, fsync, rename). `Save()` is not offered. Context variants (`WriteToContext`) mirror
  `ExtractArtworkContext`."
- Atomicity: "Create a temp file in the **same directory** … Copy the original's mode bits …
  Stream via `WriteTo`; `Sync()` the temp file; verify (read back with the library, compare audio
  hash) … `os.Rename(tmp, path)`; fsync the directory … Never truncate or open the original for
  writing. On any error remove the temp file and return; the original is untouched."
- "Symlinks: Resolve and replace the target, not the link (`filepath.EvalSymlinks`)."
- "Concurrent writer: … compare size+mtime captured at open against `stat` before rename, abort
  with `ErrConcurrentModification`."
- In-place: "opt-in (`WithInPlace()`) and off by default."
- Source of truth: "`Set` must also update the typed field … so the two cannot diverge … A writer
  driven by typed fields would drop everything the typed struct cannot express." This plan keeps
  that intent but extends it, because direct assignment (`f.Tags.Title = "x"`) is the most natural
  Go edit and must not be silently ignored. See Decision D2.

**No cross-field inference (binding maintainer rule, 2026-10-31).** In the maintainer's words:
"our job is to do the hard part of reading the tags and passing them up. The above application
decides what to do with them." For this plan that means three things:
- A typed field is bound only to the native keys that **explicitly represent that field**. A key
  is never bound to a different field that it might imply. For example, `TCOM`/`COMPOSER` are
  bound to `Composers` only, never to `Narrator`; `GRP1`/`GROUPING` are bound to `Grouping` only,
  never to `Series`.
- `Tags.Set` syncs only through those bindings.
- Writers never fill or rewrite one field from another. Only the keys that represent the fields
  the caller set or cleared may change.

**Lint conventions** (`.golangci.yml`): `errname` (sentinels must be `ErrXxx`, types `XxxError`),
`gocyclo` at 15 (split long functions), `godot` (comments end with a period), `revive exported`
(every exported symbol has a doc comment starting with its name), `gosec` with G304/G301/G306
excluded.

## Design decisions (binding for this plan; the maintainer reviews them at Step 14)

**D1. Public surface.** This is the complete list. Do not add anything else.

```go
// write_errors.go (root). Each is a re-export of the identically named var in internal/types,
// so internal format writers can return them without an import cycle.
var (
	ErrUnsupportedWrite       = types.ErrUnsupportedWrite       // no writer for the format, or an edit the format cannot represent
	ErrDRMProtected           = types.ErrDRMProtected           // encrypted/DRM content; never rewritten
	ErrTagTooLarge            = types.ErrTagTooLarge            // serialized metadata exceeds a format limit
	ErrConflictingEdits       = types.ErrConflictingEdits       // a typed field and its raw key were edited to different values
	ErrInvalidArtwork         = types.ErrInvalidArtwork         // AddArtwork/ReplaceArtwork input rejected
	ErrConcurrentModification = types.ErrConcurrentModification // Write: file changed on disk between open and replace
	ErrVerifyFailed           = types.ErrVerifyFailed           // Write: the new file failed read-back verification
)

// write_options.go (root)
type WriteOption func(*writeOptions)
func WithInPlace() WriteOption        // hint: overwrite the metadata region in place when it fits; otherwise atomic rewrite
func WithPreserveModTime() WriteOption // keep the original modification time (default: the replaced file gets a new mtime)

// internal/types/tags.go: an exported method on types.Tags, public through `type Tags = types.Tags`.
// This is the ONE deliberate exception to "add only unexported methods to types.Tags" (maintainer decision 2026-10-31).
func (t *Tags) Delete(keys ...string) // removes each raw key (format-native) and, on bound Tags, zeroes or re-derives its typed field; unknown keys are a no-op

// write.go (root)
func (f *File) WriteTo(w io.Writer) (int64, error)                               // implements io.WriterTo
func (f *File) WriteToContext(ctx context.Context, w io.Writer) (int64, error)
func Write(path string, edit func(f *File) error, opts ...WriteOption) error
func WriteContext(ctx context.Context, path string, edit func(f *File) error, opts ...WriteOption) error

// artwork_edit.go (root)
func (f *File) AddArtwork(a Artwork) error     // append an image (a.Type is its picture type)
func (f *File) ReplaceArtwork(a Artwork) error // remove every image of a.Type, then add a
func (f *File) RemoveArtwork(t ArtworkType)    // remove every image of type t
func (f *File) RemoveAllArtwork()              // remove every image
```

Behavior change to an existing method: `Tags.Set` (doc and behavior, see D3).

**D2. How edits are supplied: diff at write time.** The caller edits `f.Tags` freely: with
`Set` (raw, format-native keys), with direct typed-field assignment, or both. `WriteTo` re-parses
the original source (`f.parser` over `f.reader`, which is read-only and unchanged) to get the
*original* `Tags`, then diffs:
1. **Key edits.** Every raw key whose values differ between the original and the current `Tags`
   becomes a `KeyEdit{Key, Values}`. Values are `nil` for a deleted key.
2. **Field edits.** Diff applies all key edits to a clone of the original (with the D3 sync
   rule), which gives the "derived" `Tags`. Then, for each typed field: if `current == derived`,
   there is no field edit. If `derived != original`, a raw key already changed this field, and
   `current` disagrees with it, so return `ErrConflictingEdits`. Otherwise emit
   `FieldEdit{Field, Values}`.
3. Two cross-field conflicts are also `ErrConflictingEdits`:
   - `Artist` and `Artists` both have field edits, `len(Artists) > 0` and `Artists[0] != Artist`;
   - `Year` and `Date` both have field edits, `len(Date) >= 4` and `Date[:4] != fmt.Sprintf("%04d", Year)`.
4. If the current `f.Chapters` differs from the re-parsed original, return `ErrUnsupportedWrite`
   ("chapter editing is not supported"). Changes to `f.Audio` are ignored (they are not metadata),
   as the godoc says.
5. Artwork edits are an ordered op list recorded on the `File` by the artwork methods.

Re-parsing at write time instead of snapshotting at `Open` costs read-only users nothing.
**Justification versus "raw is the single source of truth" (design doc):** `raw` stays the
lossless record, and the writer still preserves everything it does not edit. But a typed-field
assignment is honored instead of silently dropped, and the only ambiguous case (both views edited
differently) is an explicit error.

Each format writer maps a `FieldEdit` to native keys with the **"source key"** rule. The
candidate keys of a field are its bindings, which by the no-inference rule are all keys that
explicitly represent that field.
- **Setting** a field writes only the **highest-precedence candidate present in the original**
  (the key the value was read from). If no candidate is present, it writes the format's canonical
  key. Every other key is left untouched.
- **Clearing** a field (an empty value) removes **every** present candidate of that field.
  Otherwise a lower-precedence key for the same field would resurface on re-read, and the clear
  would not stick.

No other key is ever written.
A field the format cannot represent makes the writer return
`fmt.Errorf("%w: %s cannot be written to %s", ErrUnsupportedWrite, field, format)`. Plans
039–041 own the per-format details.

**D3. `Tags.Set` sync rule.** `Tags` gets an unexported `format Format` field.
`openReader` binds it after parsing (`types.BindFormat(&file.Tags, format)`). On a **bound**
`Tags`, `Set(key, …)` updates `raw` as today and then, for every binding whose candidate keys
contain `key`, recomputes that field from the first candidate key present in `raw` (zero value
if none is present). Bindings only ever map a key to the field it explicitly represents, so
`Set` never changes any other field. For example, `Set("TCOM", …)` changes `Composers` and never
`Narrator`. On an **unbound** `Tags` (zero value, or built by the caller), `Set` behaves
exactly as today. `AddRaw` never syncs. `Clone` copies `format`. `Equal` ignores `format`.
`Merge` keeps the receiver's `format`.

**D4. Write convenience takes an edit callback, not a `*Tags`.** `Write(path, edit, opts...)`
opens the file fresh, calls `edit(f)`, and writes atomically. The design doc's sketch,
`Write(path, tags *Tags, …)`, has the stale-edit problem the doc itself names ("the caller's
`Tags` may disagree with the file on disk"). A callback applies the edits against what is on disk
right now, and the same closure works for tags and artwork. `Write` with zero pending edits returns
nil and does not touch the file.

**D5. Verification in `Write`.** Verification happens before the rename (or after the in-place
patch). It re-parses the new bytes with the **original path string** as the parser's `path`
argument, so that any path-dependent behavior still in a parser sees the real name and not the
temp file's. It then requires all of the following:
- the parse returns no error;
- the new file's `AudioDigest` equals the source's;
- the chapters (count, titles, start and end times) equal the re-parsed source's.

Any failure returns an error wrapping `ErrVerifyFailed`.

**D6. Formats without a writer.** `WriteTo` and `Write` return an error wrapping
`ErrUnsupportedWrite` for any format with no registered writer, **even when there are no
edits**. After this plan, that is every format.

**D7. No-edit `WriteTo` is a verbatim copy.** If a writer exists and the edit set is empty,
`WriteTo` copies the source byte-for-byte. Plans 039–041 test their serializers by calling the
internal writer directly with an empty edit set.

## Commands you will need

| Purpose | Command | Expected on success |
|---|---|---|
| Build | `mise exec -- go build ./...` | exit 0 |
| Cross-build | `GOOS=windows GOARCH=amd64 mise exec -- go build ./...` and `GOOS=darwin GOARCH=arm64 mise exec -- go build ./...` | exit 0 |
| Vet | `mise exec -- go vet ./...` | exit 0 |
| Tests | `mise exec -- go test -race ./...` | all `ok` |
| Targeted | `mise exec -- go test -race -run 'Write\|Artwork\|Field\|Diff\|Bind\|Set' . ./internal/types/ ./internal/registry/ ./internal/atomicfile/ ./internal/writeutil/ -v` | all PASS |
| Lint | `mise exec -- golangci-lint run ./...` | `0 issues.` |
| Format | `gofmt -l .` | no output |
| Doc | `mise exec -- go doc . Write` (etc.) | prints the symbol |

Always run lint through `mise exec --`. Never use a bare global `golangci-lint`.

## Scope

**In scope** (the only files you should modify or create):
- `internal/types/write_errors.go` (create)
- `internal/types/fields.go` (create), `internal/types/fields_test.go` (create)
- `internal/types/fieldmap.go` (create), `internal/types/fieldmap_test.go` (create)
- `internal/types/edits.go` (create), `internal/types/edits_test.go` (create)
- `internal/types/tags.go` (modify: `format` field, `Set` sync, `Clone`, doc)
- `internal/types/tags_test.go` (modify: add Set-sync tests)
- `internal/registry/writer.go` (create), `internal/registry/writer_test.go` (create)
- `internal/writeutil/writeutil.go` (create), `internal/writeutil/writeutil_test.go` (create)
- `internal/atomicfile/atomicfile.go`, `atomicfile_unix.go`, `atomicfile_other.go`, `atomicfile_test.go` (create)
- `file.go` (modify: `BindFormat` call, new unexported fields on `File`)
- `write_errors.go`, `write_options.go`, `write.go`, `artwork_edit.go` (create, root)
- `write_internal_test.go` (create, `package audiometa`)
- `write_harness_test.go`, `write_roundtrip_test.go`, `example_write_test.go` (create, `package audiometa_test`)

**Out of scope** (do NOT touch):
- Any format package (`internal/mp3`, `internal/flac`, `internal/m4a`, `internal/ogg`,
  `internal/vorbis`): writers are plans 039–041. Ogg/Opus writing is deferred by maintainer
  decision.
- `README.md`, `CHANGELOG.md`, `doc.go`, `.github/workflows/*`: owned by plan 042. **Do not cut a
  release between this plan and 042.**
- `options.go` (`Option` is for Open; write options are a separate type on purpose, so they can't
  be passed to `Open` by mistake).
- Chapter editing (a future plan), `WithBackup`, progress callbacks, and a `Format.Writable()`
  helper. They are not part of the frozen surface; adding them later is additive.
- `cmd/audiometa`: no CLI `write` subcommand in this wave.

## Git workflow

- Branch: `advisor/038-write-api-foundations`
- One commit per step group. Gitmoji, single line, no body, no attribution. Examples from
  `git log`: `✨ Parse OverDrive MediaMarkers chapters from TXXX`,
  `♻️ Extract shared FillChapterEndTimes`. Suggested: `✨ Add write edit model and Tags.Set field
  sync`, `✨ Add writer registry, writeutil and atomic replace`, `✨ Add WriteTo, Write and artwork
  edit API`, `✅ Add write round-trip harness`.
- Do NOT push or open a PR.

## Steps

### Step 1: Sentinel errors in `internal/types`

Create `internal/types/write_errors.go`:

```go
package types

import "errors"

// Write-path sentinel errors. The root package re-exports each one; format
// writers return them wrapped with detail via fmt.Errorf("%w: ...").
var (
	// ErrUnsupportedWrite reports that the format has no writer, or that an
	// edit cannot be represented in the format (a field, a key, a picture type).
	ErrUnsupportedWrite = errors.New("audiometa: write not supported")
	// ErrDRMProtected reports encrypted or DRM-protected content. Such files are never rewritten.
	ErrDRMProtected = errors.New("audiometa: file is DRM-protected or encrypted")
	// ErrTagTooLarge reports serialized metadata that exceeds a format limit.
	ErrTagTooLarge = errors.New("audiometa: metadata exceeds format size limit")
	// ErrConflictingEdits reports a typed field and its raw key edited to different values.
	ErrConflictingEdits = errors.New("audiometa: conflicting tag edits")
	// ErrInvalidArtwork reports artwork rejected by AddArtwork or ReplaceArtwork.
	ErrInvalidArtwork = errors.New("audiometa: invalid artwork")
	// ErrConcurrentModification reports that the file changed on disk during Write.
	ErrConcurrentModification = errors.New("audiometa: file changed during write")
	// ErrVerifyFailed reports that a written file failed read-back verification.
	ErrVerifyFailed = errors.New("audiometa: written file failed verification")
)
```

**Verify**: `mise exec -- go build ./internal/types/` → exit 0.

### Step 2: The `Field` enum and field accessors

Create `internal/types/fields.go` with `type Field uint8` and the 35 constants below, in this
order, starting at `iota + 1` so the zero value is invalid. Add a private `fieldCount` sentinel
after the last constant.

`FieldTitle, FieldSubtitle, FieldArtist, FieldArtists, FieldAlbum, FieldAlbumArtist, FieldGenres,
FieldComposers, FieldPerformers, FieldYear, FieldDate, FieldOriginalDate, FieldTrackNumber,
FieldTrackTotal, FieldDiscNumber, FieldDiscTotal, FieldComment, FieldDescription, FieldLyrics,
FieldGrouping, FieldNarrator, FieldSeries, FieldSeriesPart, FieldPublisher, FieldISBN, FieldASIN,
FieldLanguage, FieldCopyright, FieldLabel, FieldCatalogNumber, FieldBarcode, FieldISRC,
FieldMusicBrainzTrackID, FieldMusicBrainzAlbumID, FieldMusicBrainzArtistID`

Then add:
- `func (f Field) String() string`. Return the Go field name (`"Title"`, `"MusicBrainzTrackID"`).
  Write it by hand with a `switch`; do not add a `go:generate` line, because CI checks generated
  code freshness.
- `func fieldValues(t *Tags, f Field) []string`. String fields return `nil` when empty, else
  `[]string{v}`. Slice fields return `slices.Clone`. Int fields return `nil` when 0, else
  `[]string{strconv.Itoa(n)}`.
- `func setFieldValues(t *Tags, f Field, v []string)`. This is the inverse. A string field gets
  `v[0]` or `""`. A slice field gets `slices.Clone(v)`, or nil when `len(v)==0`. An int field gets
  `strconv.Atoi(v[0])`, and 0 on error or when `v` is empty.
- `func AllFields() iter.Seq[Field]`, which yields `FieldTitle … FieldMusicBrainzArtistID` in order.

`gocyclo` will flag one giant switch, so implement `fieldValues`/`setFieldValues` with a
pointer-returning helper per kind: `func stringField(t *Tags, f Field) *string`,
`func sliceField(t *Tags, f Field) *[]string`, `func intField(t *Tags, f Field) *int`. Each
returns nil when `f` is not of that kind. A `switch` with 20+ cases has cyclomatic complexity
above 15, so add `//nolint:gocyclo // flat field dispatch` with that reason, which is the repo's
existing convention (see `internal/types/tags.go:186`).

Create `internal/types/fields_test.go`:
- `TestFieldsCoverEveryTagsField`: use `reflect.TypeFor[Tags]()`. Count the exported fields
  (`NumField` minus the unexported ones) and assert the count equals `int(fieldCount) - 1`
  (that is 35). Also assert that `Field(i).String()`, for `i` in `1..35`, is the name of an
  exported Tags field. This guards future `Tags` additions.
- `TestFieldValuesRoundTrip`: for every field, call `setFieldValues(t, f, sample)` then
  `fieldValues(t, f)` and compare. Use samples `["x"]` for string fields, `["a","b"]` for slice
  fields and `["7"]` for int fields.

**Verify**: `mise exec -- go test ./internal/types/ -run 'Fields|FieldValues' -v` → PASS.

### Step 3: Per-format field bindings

Create `internal/types/fieldmap.go`:

```go
// FieldBinding lists the raw keys a format's parser reads into one typed field,
// highest read precedence first.
type FieldBinding struct {
	Field Field
	Keys  []string
}

// FieldBindings returns the bindings for format (a fresh copy; callers may not
// mutate shared state). Formats without bindings return nil.
func FieldBindings(format Format) []FieldBinding
```

**Re-verify every row against the live parsers before copying it.** The no-inference pass (see
the drift check) may have removed keys, and a binding must exist only where the parser assigns
that key's value **directly** to that field. Drop any key the live parser no longer maps. Never
add a key that maps one field from another. If you are unsure whether a key is a direct mapping,
STOP and ask.

Store the tables below as unexported package vars. `FormatFLAC`, `FormatOgg` and `FormatOpus`
share the Vorbis table, and `FormatM4A` and `FormatM4B` share the M4A table. Copy the keys
**exactly**, including the `\xA9` byte for `©`. These tables mirror what the parsers read today.
The cited lines are where to cross-check each one.

**MP3** (`internal/mp3/id3v2.go:484-527` text frames, `:548-573` TXXX descriptions):

| Field | Keys (precedence order) |
|---|---|
| Title | `TIT2` |
| Subtitle | `TIT3` |
| Artist | `TPE1` |
| Artists | `TPE1` |
| Album | `TALB` |
| AlbumArtist | `TPE2` |
| Genres | `TCON` |
| Composers | `TCOM` |
| Year | `TDRC`, `TYER` |
| TrackNumber | `TRCK` |
| TrackTotal | `TRCK` |
| DiscNumber | `TPOS` |
| DiscTotal | `TPOS` |
| Grouping | `GRP1`, `TIT1`, `GROUPING` |
| Narrator | `NARRATOR` |
| Series | `SERIES`, `MVNM`, `MOVEMENT NAME`, `MOVEMENT` |
| SeriesPart | `SERIES PART`, `SERIESPART`, `SERIES-PART`, `SERIES POSITION`, `MVIN`, `MOVEMENT NUMBER`, `MOVEMENT INDEX` |
| Publisher | `PUBLISHER` |
| ISBN | `ISBN` |
| ASIN | `ASIN`, `AUDIBLE_ASIN` |
| Language | `LANGUAGE`, `LANG` |
| Description | `DESCRIPTION` |

(MP3 `Comment` comes from COMM, which is not in `raw`, so it has no binding. Plan 039 writes it
as a typed-only field.)

**Vorbis** (`internal/vorbis/comments.go:42-128`):

| Field | Keys |
|---|---|
| Title | `TITLE` |
| Subtitle | `SUBTITLE` |
| Artist, Artists | `ARTIST` (two rows) |
| Album | `ALBUM` |
| AlbumArtist | `ALBUMARTIST` |
| Date | `DATE` |
| Year | `DATE` |
| OriginalDate | `ORIGINALDATE` |
| TrackNumber | `TRACKNUMBER` |
| TrackTotal | `TRACKTOTAL`, `TOTALTRACKS` |
| DiscNumber | `DISCNUMBER` |
| DiscTotal | `DISCTOTAL`, `TOTALDISCS` |
| Genres | `GENRE` |
| Composers | `COMPOSER` |
| Performers | `PERFORMER` |
| Comment | `COMMENT` |
| Lyrics | `LYRICS` |
| Narrator | `NARRATOR` |
| Publisher | `PUBLISHER` |
| Series | `SERIES`, `MVNM` |
| SeriesPart | `SERIESPART`, `SERIES-PART`, `SERIES_PART`, `SERIES PART`, `MVIN` |
| Grouping | `GROUPING` |
| ISBN | `ISBN` |
| ASIN | `ASIN`, `AUDIBLE_ASIN` |
| Language | `LANGUAGE`, `LANG` |
| Description | `DESCRIPTION` |
| MusicBrainzTrackID | `MUSICBRAINZ_TRACKID` |
| MusicBrainzAlbumID | `MUSICBRAINZ_ALBUMID` |
| MusicBrainzArtistID | `MUSICBRAINZ_ARTISTID` |
| ISRC | `ISRC` |
| Barcode | `BARCODE` |
| CatalogNumber | `CATALOGNUMBER` |
| Label | `LABEL` |
| Copyright | `COPYRIGHT` |

**M4A/M4B** (`internal/m4a/metadata.go:447-503`, `internal/m4a/audiobook.go:145-178`, and
`resolveSeriesPart` at `:192-205`):

| Field | Keys |
|---|---|
| Title | `"\xA9nam"` |
| Artist, Artists | `"\xA9ART"` (two rows) |
| Album | `"\xA9alb"` |
| Comment | `"\xA9cmt"` |
| Grouping | `"\xA9grp"` |
| Lyrics | `"\xA9lyr"` |
| AlbumArtist | `"aART"` |
| Copyright | `"cprt"` |
| Description | `"ldes"`, `"desc"`, `"\xA9des"`, `"DESCRIPTION"` |
| Genres | `"\xA9gen"` |
| Composers | `"\xA9wrt"` |
| Date | `"\xA9day"` |
| Year | `"\xA9day"` |
| Subtitle | `"SUBTITLE"` |
| Narrator | `"NARRATOR"` |
| Series | `"SERIES"`, `"\xA9mvn"`, `"MVNM"`, `"MOVEMENT NAME"`, `"MOVEMENT"` |
| SeriesPart | `"\xA9mvi"`, `"SERIES PART"`, `"SERIES POSITION"`, `"PART"`, `"VOLUME"`, `"MVIN"`, `"MOVEMENT NUMBER"`, `"MOVEMENT INDEX"` |
| Publisher | `"PUBLISHER"` |
| ISBN | `"ISBN"` |
| ASIN | `"ASIN"`, `"AUDIBLE_ASIN"` |
| Language | `"LANGUAGE"`, `"LANG"` |

(Track and disc come from binary `trkn`/`disk` atoms, which are not in `raw`, so they have no
binding. Plan 041 writes them as typed-only.)

**Decoding a field from raw values.** Add `func decodeField(t *Tags, f Field, key string,
values []string)`, which sets field `f` from the values of `key`:
- Year: the leading 4 characters parsed with `strconv.Atoi`, and kept only if > 0.
- TrackNumber/DiscNumber: when `key` is `TRCK` or `TPOS`, the part before `/`; otherwise the
  whole first value. Parse with `strconv.Atoi(strings.TrimSpace(..))`, and use 0 on error.
- TrackTotal/DiscTotal: when `key` is `TRCK`/`TPOS`, the part after `/` (0 if absent);
  otherwise the first value.
- Everything else goes through `setFieldValues` (string = first value, slice = all values).

Create `internal/types/fieldmap_test.go`:
- `TestFieldBindings_KeysAreNonEmpty`: every binding of every format has at least one key, and no
  key is empty.
- `TestFieldBindings_ReturnsCopy`: mutating the returned slice does not affect a second call.

**Verify**: `mise exec -- go test ./internal/types/ -run FieldBindings -v` → PASS.

### Step 4: Bind `Tags` to a format and sync `Set`

In `internal/types/tags.go`:
1. Add the field `format Format // bound by BindFormat; zero = unbound (Set touches raw only)`
   directly under `raw`.
2. Add a package function:
   ```go
   // BindFormat records the format whose parser produced t, enabling Set to keep
   // typed fields in sync with raw keys. Called by the root package after parsing.
   func BindFormat(t *Tags, format Format) { t.format = format }
   ```
3. Change `Set`. After the existing raw update, call `t.syncKey(key)`. `syncKey` is an
   unexported method: return immediately when `t.format == FormatUnknown`. Otherwise, for every
   binding `b` in the format's table where `slices.Contains(b.Keys, key)`, find the first `k` in
   `b.Keys` with `len(t.raw[k]) > 0`. If one is found, call `decodeField(t, b.Field, k, t.raw[k])`;
   if none, `setFieldValues(t, b.Field, nil)`.
3a. Add the exported method `Delete` (maintainer decision: applications must be able to delete
   any tag, including keys that map to no typed field, e.g. Vorbis `SHOW`, a TXXX description,
   an iTunes `----` freeform item, M4A `gnre`):
   ```go
   // Delete removes the raw tags with the given format-native keys (see Get).
   // Keys that are not present are ignored.
   //
   // On Tags returned by Open, Delete also updates the typed field each key
   // represents: the field is re-derived from any remaining key for that field,
   // or cleared if none remains. When the File is written, deleted keys are
   // removed from the file; no other tag is changed.
   func (t *Tags) Delete(keys ...string) {
   	for _, key := range keys {
   		if _, ok := t.raw[key]; !ok {
   			continue
   		}
   		delete(t.raw, key)
   		t.syncKey(key)
   	}
   }
   ```
   `Set(key)` with no values keeps working as before (it is equivalent to `Delete(key)`); the
   doc comment of `Set` should point to `Delete` as the clearer spelling.
   Tests (in `internal/types/tags_write_test.go`, or wherever Step 4's tests live):
   - `TestDelete_RemovesUnmappedKey`: bound Vorbis Tags with `SHOW=Foo` and `TITLE=T`;
     `Delete("SHOW")` → `Get("SHOW")` empty, `Title == "T"`, and every typed field unchanged.
   - `TestDelete_ZeroesTypedField`: MP3 `TIT2` → `Delete("TIT2")` → `Title == ""`.
   - `TestDelete_MultipleAndMissingKeys`: `Delete("A","missing","B")` removes A and B; no panic
     on a nil `raw` map (zero `Tags{}`).
   - `TestDelete_ReDerivesFromRemainingKey`: Vorbis `SERIES=A`, `MVNM=B` → `Delete("SERIES")` →
     `Series == "B"`.
   The Step 5 diff already turns a removed raw key into `KeyEdit{Values: nil}`. Add a Step 13
   harness case per writable format: open a corpus file, `Delete` one typed key and one key with
   no typed field (inject one into a temp copy first if the corpus file has none), write, re-open
   → both keys absent, every other tag unchanged, audio digest unchanged.
4. In `Clone`, copy `format`. Leave `Equal` and `Merge` unchanged. (`Equal` must keep ignoring
   `format`, and `Merge` keeps the receiver's.)
5. Replace the `Set` doc comment with:
   ```go
   // Set sets a tag value in the raw tag map. Keys are format-native (see Get).
   //
   // If values is empty, the tag is removed. Multiple values can be provided for
   // multi-value tags.
   //
   // On Tags returned by Open, Set also updates the typed field that key
   // represents, so Set("TIT2", "x") on an MP3 makes Title "x". It never changes
   // any other field: Set("TCOM", ...) changes Composers, not Narrator. On a zero
   // or caller-built Tags, Set changes only the raw map.
   // Changes are in memory; use File.WriteTo or Write to persist them.
   ```

Add to `internal/types/tags_test.go`:
- `TestSet_UnboundTouchesRawOnly`: a zero `Tags`; `Set("TITLE","x")` leaves `Title == ""`. This
  is the existing behavior.
- `TestSet_SyncsTypedField`: a table over `(format, key, values, field, want)`. It needs at least
  one row per format family:
  - MP3 `TIT2` → `Title`;
  - MP3 `TRCK` `"3/12"` → `TrackNumber` 3 and `TrackTotal` 12;
  - MP3 `TDRC` `"2021-05-03"` → `Year` 2021;
  - Vorbis `ARTIST` `["A","B"]` → `Artists` `[A B]` and `Artist` `A`;
  - Vorbis `DATE` → `Date` and `Year`;
  - M4A `"\xA9nam"` → `Title`;
  - M4A `NARRATOR` → `Narrator`.

  Each row builds `t := &Tags{}`, calls `BindFormat(t, format)`, then `t.Set(...)`, and asserts
  the field.
- `TestSet_DeleteZeroesField` (MP3 `TIT2` set then deleted → `Title == ""`).
- `TestSet_LowerPrecedenceKeyDoesNotOverride`: Vorbis, `Set("SERIES","A")` then `Set("MVNM","B")`
  → `Series == "A"`. Then `Delete("SERIES")` → `Series == "B"`.
- `TestClone_PreservesBinding`: a cloned bound Tags still syncs on `Set`.
- `TestSet_NoCrossFieldSync` (**no-inference rule**). For each format, take a bound Tags with
  every typed field set to a sentinel value. Then:
  - `Set` the composer key (`TCOM` / `COMPOSER` / `"\xA9wrt"`) → only `Composers` changes, and
    `Narrator` is unchanged;
  - `Set` the grouping key (`GRP1` / `GROUPING` / `"\xA9grp"`) to `"Saga #5"` → only `Grouping`
    changes, and `Series` and `SeriesPart` are unchanged;
  - `Set` the title key to `"Book 3"` → only `Title` changes, and `SeriesPart` is unchanged.

  Assert by comparing `fieldValues` for every field before and after the call.
- `TestFieldBindings_NoCrossFieldKeys`: no composer key is bound to `FieldNarrator`, and no
  grouping key is bound to `FieldSeries` or `FieldSeriesPart`.

**Verify**: `mise exec -- go test ./internal/types/ -v 2>&1 | grep -c '^--- FAIL'` → `0`.
Then `mise exec -- go test ./... ` → all `ok` (the CLI tests at `cmd/audiometa/cli_test.go:183`
use unbound Tags and must still pass).

### Step 5: The edit model and `Diff`

Create `internal/types/edits.go`:

```go
// KeyEdit replaces every value of one format-native raw key. Values == nil deletes the key.
type KeyEdit struct {
	Key    string
	Values []string
}

// FieldEdit sets one typed field that no KeyEdit already covers. Values uses the
// fieldValues encoding (nil = clear).
type FieldEdit struct {
	Field  Field
	Values []string
}

// ArtworkOpKind is the kind of an ArtworkOp.
type ArtworkOpKind uint8

const (
	ArtworkOpAdd       ArtworkOpKind = iota + 1 // append Art
	ArtworkOpRemove                             // remove every image whose Type == Type
	ArtworkOpRemoveAll                          // remove every image
)

// ArtworkOp is one artwork edit. Ops apply in order to the file's picture list.
type ArtworkOp struct {
	Kind ArtworkOpKind
	Type ArtworkType // ArtworkOpRemove
	Art  Artwork     // ArtworkOpAdd
}

// EditSet is everything a format writer needs. Orig and Final are read-only context
// (Orig: freshly re-parsed source; Final: the caller's current Tags).
type EditSet struct {
	Keys    []KeyEdit   // sorted by Key
	Fields  []FieldEdit // sorted by Field
	Artwork []ArtworkOp // in call order
	Orig    *Tags
	Final   *Tags
}

// Empty reports whether the set changes nothing.
func (e *EditSet) Empty() bool { return len(e.Keys) == 0 && len(e.Fields) == 0 && len(e.Artwork) == 0 }

// Diff computes key and field edits that turn orig into cur for format, applying the
// rules in plan 038 D2. It returns an error wrapping ErrConflictingEdits on a conflict.
func Diff(orig, cur *Tags, format Format) ([]KeyEdit, []FieldEdit, error)
```

`Diff` algorithm. Split it into helpers so each stays under `gocyclo` 15:
1. `diffKeys`: take the union of the keys of `orig.raw` and `cur.raw`, sorted. For each key where
   `!slices.Equal(orig.raw[k], cur.raw[k])`, emit `KeyEdit{k, slices.Clone(cur.raw[k])}` (nil if
   absent or empty).
2. `derived := orig.Clone(); BindFormat(derived, format)`. For each key edit, set
   `derived.raw[k]` (or delete it) and call `derived.syncKey(k)`.
3. For each `f` in `AllFields()`, let `c, d, o := fieldValues(cur,f), fieldValues(derived,f),
   fieldValues(orig,f)`:
   - `slices.Equal(c, d)` → skip;
   - `!slices.Equal(d, o)` → return `fmt.Errorf("%w: %s and its raw key were both edited",
     ErrConflictingEdits, f)`;
   - otherwise append `FieldEdit{f, c}`.
4. The cross-field checks of D2.3 (Artist/Artists, Year/Date), each returning
   `ErrConflictingEdits` wrapped with the two field names.

Create `internal/types/edits_test.go` with table tests. Build `orig` by `BindFormat` plus `Set`
calls, `cur := orig.Clone()`, then mutate `cur`:
- no change → no edits;
- `cur.Set("TIT2","x")` (MP3) → one KeyEdit, zero FieldEdits;
- `cur.Title = "x"` (MP3) → zero KeyEdits, one FieldEdit `{FieldTitle, ["x"]}`;
- `cur.Set("TIT2","x"); cur.Title = "y"` → `errors.Is(err, ErrConflictingEdits)`;
- `cur.Set("TIT2","x"); cur.Title = "x"` → one KeyEdit, zero FieldEdits;
- delete a key → `KeyEdit{Values: nil}`;
- `cur.Artist = "A"; cur.Artists = []string{"B"}` → conflict;
- `cur.Year = 1999; cur.Date = "2001-01-01"` (Vorbis) → conflict;
- KeyEdits come back sorted by key, regardless of map order (run 20 times).
- `cur.Narrator = "N"` (MP3, where a `TCOM` exists) → exactly one FieldEdit `{FieldNarrator}`
  and no edit that touches `Composers` (no-inference rule).

**Verify**: `mise exec -- go test ./internal/types/ -run Diff -v` → PASS.

### Step 6: Writer registry (internal only)

Create `internal/registry/writer.go`:

```go
// TagWriter is implemented by format packages that can serialize edited metadata.
// Implementations must be deterministic: the same src and edits produce the same bytes.
type TagWriter interface {
	// WriteFile streams a complete new file to w: src (size bytes) with edits applied.
	// It never modifies src. It returns bytes written. Errors wrap the types.Err* sentinels
	// where one applies; structural problems return *types.CorruptedFileError.
	WriteFile(ctx context.Context, w io.Writer, src io.ReaderAt, size int64, edits *types.EditSet) (int64, error)

	// AudioDigest returns the SHA-256 of src's audio payload: the bytes no metadata
	// write may change (MP3: after the ID3v2 tag, before any ID3v1 trailer; FLAC: after
	// the last metadata block; M4A: the concatenated payloads of every mdat atom).
	AudioDigest(ctx context.Context, src io.ReaderAt, size int64) ([]byte, error)
}

// InPlaceWriter is optionally implemented by TagWriters that can apply edits by
// overwriting one region without changing the file size.
type InPlaceWriter interface {
	// InPlacePatch returns the bytes to write at off. ok is false when the edits do
	// not fit (the caller then falls back to WriteFile); that is not an error.
	InPlacePatch(ctx context.Context, src io.ReaderAt, size int64, edits *types.EditSet) (off int64, patch []byte, ok bool, err error)
}

// RegisterWriter registers w for format. Called from format packages' init().
func RegisterWriter(format types.Format, w TagWriter)

// GetWriter returns the writer for format, or nil.
func GetWriter(format types.Format) TagWriter
```

Use a second map guarded by the existing `mu`. Create `internal/registry/writer_test.go` with
`TestRegisterWriter_GetWriter`: register a stub for `types.FormatWAV`, assert `GetWriter` returns
it, and that `GetWriter(types.FormatAIFF)` is nil. Clean up with `t.Cleanup`, deleting the WAV
entry through an unexported test helper in the same package.

**Verify**: `mise exec -- go test ./internal/registry/ -v` → PASS.

### Step 7: `internal/writeutil`

Create `internal/writeutil/writeutil.go` (package doc: "Package writeutil holds helpers shared by
format writers."):
- `func CopyRange(ctx context.Context, w io.Writer, src io.ReaderAt, off, n int64) (int64,
  error)`: copy `n` bytes from `src` at `off` in 1 MiB chunks, checking `ctx.Err()` before every
  chunk. A short source returns `io.ErrUnexpectedEOF`. Use one reusable buffer.
- `func DigestRange(ctx context.Context, src io.ReaderAt, off, n int64) ([]byte, error)`: SHA-256
  of that range, by `CopyRange` into `sha256.New()`.
- `type CountingWriter struct{ W io.Writer; N int64 }` with `Write` that adds the written
  bytes to `N`.

Tests (`writeutil_test.go`):
- `CopyRange` of a 3 MiB buffer at offsets 0 and 1 copies exactly;
- a canceled context returns `context.Canceled` and writes nothing;
- `n` beyond the source → `io.ErrUnexpectedEOF`;
- `DigestRange` equals `sha256.Sum256` of the slice.

**Verify**: `mise exec -- go test ./internal/writeutil/ -v` → PASS.

### Step 8: `internal/atomicfile` (Unix-first, documented Windows notes)

Create `internal/atomicfile/atomicfile.go`:

```go
// Package atomicfile replaces a file's contents crash-safely: write a temp file in
// the same directory, fsync it, verify it, rename it over the original, fsync the
// directory. The original is never opened for writing.
//
// Windows notes: os.Rename maps to MoveFileEx(MOVEFILE_REPLACE_EXISTING), which fails
// with a sharing violation while another process holds the target open without
// FILE_SHARE_DELETE (and Go's os.Open does not request it); callers must close their
// own handles before Replace renames. Directory fsync is not available and is skipped.
// Only the read-only attribute maps from Unix mode bits; ACLs are not copied.
package atomicfile

// Options tunes Replace.
type Options struct {
	PreserveModTime bool                   // set the new file's mtime to the original's
	Verify          func(*os.File) error   // after Sync, before rename; may ReadAt the temp file
	BeforeRename    func() error           // last chance to abort (concurrent-modification check, closing handles)
}

// Replace atomically replaces the regular file at path (symlinks are resolved and
// the target is replaced; the link is kept) with the bytes produced by write.
// On any error the temp file is removed and the original is untouched.
func Replace(path string, write func(io.Writer) error, opts Options) error
```

Implementation order:
1. `target, err := filepath.EvalSymlinks(path)`, then `st, err := os.Stat(target)`. If it is not
   a regular file, return an error.
2. `tmp, err := os.CreateTemp(filepath.Dir(target), "."+filepath.Base(target)+".audiometa-*.tmp")`.
   Defer a cleanup that, unless `committed`, calls `tmp.Close()` and `os.Remove(tmp.Name())`.
3. `tmp.Chmod(st.Mode().Perm())`, then `copyOwner(tmp, st)`. That is a best-effort `Fchown`
   from `st.Sys().(*syscall.Stat_t)` in `atomicfile_unix.go` (`//go:build unix`), with errors
   ignored, and a no-op in `atomicfile_other.go` (`//go:build !unix`).
4. Wrap `tmp` in `bufio.NewWriterSize(tmp, 1<<20)`, call `write(bw)`, then `bw.Flush()` and
   `tmp.Sync()`.
5. If `opts.Verify != nil`, call `opts.Verify(tmp)`.
6. If `opts.PreserveModTime`, call `os.Chtimes(tmp.Name(), time.Time{}, st.ModTime())`. (A zero
   atime leaves it unchanged; this has been documented behavior since Go 1.22.)
7. `tmp.Close()`. Then, if `opts.BeforeRename != nil`, call it.
8. `os.Rename(tmp.Name(), target)`, set `committed = true`, then `syncDir(filepath.Dir(target))`.
   On Unix that is open, `Sync`, close, and a failure is returned. In `atomicfile_other.go` it is
   a no-op.

Tests (`atomicfile_test.go`, all in `t.TempDir()`):
- the happy path replaces the content and keeps mode `0o640`;
- a write error, a Verify error and a BeforeRename error each leave the original byte-identical
  and leave no `.audiometa-*.tmp` file in the dir (`filepath.Glob`);
- a symlink `link -> real`: `Replace("link")` updates `real` and `link` is still a symlink
  (`os.Lstat`);
- `PreserveModTime` keeps the original mtime (set it first with `os.Chtimes` to a fixed
  2001-02-03 time);
- a directory path → error;
- a non-existent path → error.

**Verify**: `mise exec -- go test -race ./internal/atomicfile/ -v` → PASS;
`GOOS=windows GOARCH=amd64 mise exec -- go vet ./internal/atomicfile/` → exit 0.

### Step 9: Bind the format at open, and add pending-edit state to `File`

In `file.go`:
- In `openReader`, after `file.Size = size`, add `types.BindFormat(&file.Tags, format)`.
- Add these fields to `File`, under `artworkLoaded`:
  ```go
  	editMu     sync.Mutex        // guards artworkOps
  	artworkOps []types.ArtworkOp // pending artwork edits, applied by WriteTo
  ```

**Verify**: `mise exec -- go test ./... ` → all `ok`, including
`TestBenchFixturesParse` (bench goldens unchanged: `format` is unexported and not serialized).
If a golden differs, STOP.

### Step 10: Root errors and options

`write_errors.go` (root): a `var (...)` block re-exporting the seven sentinels (D1), each with a
doc comment. Example:
`// ErrUnsupportedWrite is returned (wrapped) by WriteTo and Write when the file's format has no
writer, or an edit cannot be represented in the format. Test with errors.Is.`

`write_options.go`:

```go
// WriteOption configures Write and WriteContext.
type WriteOption func(*writeOptions)

type writeOptions struct {
	inPlace         bool
	preserveModTime bool
}

// WithInPlace lets Write overwrite the metadata region of the original file when
// the edited metadata fits in the existing space (for example ID3v2 or FLAC padding).
// It is a hint: when the format or the edits cannot be applied in place, Write falls
// back to the default atomic rewrite. In-place writes are not crash-atomic: a crash
// mid-write can leave a damaged tag (the audio is never touched). Write keeps a copy
// of the overwritten bytes and restores them if verification fails.
func WithInPlace() WriteOption

// WithPreserveModTime keeps the file's modification time. By default the written
// file has a new modification time, which is what library managers use to detect changes.
func WithPreserveModTime() WriteOption
```

**Verify**: `mise exec -- go build ./...` → exit 0.

### Step 11: Artwork edit methods

Create `artwork_edit.go`:
- `AddArtwork(a Artwork) error` validates and then appends `ArtworkOp{Kind: ArtworkOpAdd, Art:
  cloned a}` under `editMu`. `Data` must be cloned, because the caller may reuse its buffer.
  Validation returns `fmt.Errorf("%w: <reason>", ErrInvalidArtwork)` when:
  - `len(a.Data) == 0`;
  - `a.Type` is outside `ArtworkOther..ArtworkPublisherLogotype`;
  - `a.Description` is not valid UTF-8;
  - `a.MIMEType` is empty and `imagemeta.MIMEType(a.Data)` returns `""`.

  When `MIMEType` is empty and sniffing succeeds, store the sniffed type. When
  `Width`/`Height` are 0, fill them from `imagemeta.Dimensions`.
- `ReplaceArtwork(a Artwork) error`: validate first, then append `Remove(a.Type)` and `Add(a)`.
  Nothing is appended when validation fails.
- `RemoveArtwork(t ArtworkType)` and `RemoveAllArtwork()` append their ops.
- `func (f *File) pendingArtwork() []types.ArtworkOp` (unexported) returns a copy under `editMu`.

Godoc for all four must state:
- edits are pending until `WriteTo`/`Write`;
- `ExtractArtwork` keeps reporting the artwork stored in the file;
- some formats restrict picture types (M4A stores no type), and violations surface as
  `ErrUnsupportedWrite` from `WriteTo`.

`imagemeta` is `github.com/simonhull/audiometa/internal/imagemeta` (landed by plan 016). If it
does not exist, STOP.

**Verify**: `mise exec -- go build ./...` → exit 0.

### Step 12: `WriteTo`, `WriteToContext`, `Write`, `WriteContext`

Create `write.go`. Shapes:

```go
// lookupWriter is swapped by tests.
var lookupWriter = registry.GetWriter

// WriteTo writes a complete copy of the file with all pending edits applied to w:
// changes made to f.Tags (through Set or by assigning typed fields) and artwork
// edits (AddArtwork, ReplaceArtwork, RemoveArtwork, RemoveAllArtwork). The original
// file is never modified and f is unchanged; calling WriteTo twice writes identical bytes.
// It implements io.WriterTo.
//
// Unknown frames and atoms, chapters and audio data are preserved. Edits that the
// format cannot represent, and any change to f.Chapters, return an error wrapping
// ErrUnsupportedWrite; edits to f.Audio are ignored. A typed field and its raw key
// edited to different values return ErrConflictingEdits. DRM-protected files return
// ErrDRMProtected. With no pending edits the output is byte-identical to the input.
func (f *File) WriteTo(w io.Writer) (int64, error) { return f.WriteToContext(context.Background(), w) }

// WriteToContext is the cancellable form of WriteTo.
func (f *File) WriteToContext(ctx context.Context, w io.Writer) (int64, error)
```

`WriteToContext` steps:
1. `ctx.Err()`.
2. `tw := lookupWriter(f.Format)`. If it is nil, return
   `0, fmt.Errorf("%w: %s", ErrUnsupportedWrite, f.Format)`.
3. `edits, err := f.buildEdits(ctx)` (below).
4. If `edits.Empty()`, return `writeutil.CopyRange(ctx, w, f.reader, 0, f.Size)`.
5. Otherwise `writeSafely(ctx, tw, w, f.reader, f.Size, edits)`. That is a `recover()`
   wrapper modeled on `extractSafely` (`file.go:190-197`): a panic becomes `panicError(f.Path,
   rec)`.

`buildEdits(ctx)`:
- `orig, err := parseSafely(ctx, f.parser, f.reader, f.Size, f.Path)`, then
  `types.BindFormat(&orig.Tags, f.Format)`;
- check chapters: `slices.EqualFunc(f.Chapters, orig.Chapters, chapterEqual)` must hold, where
  `chapterEqual` compares Index, Title, StartTime, EndTime and Source;
- `keys, fields, err := types.Diff(&orig.Tags, &f.Tags, f.Format)`;
- return `&types.EditSet{Keys: keys, Fields: fields, Artwork: f.pendingArtwork(), Orig: &orig.Tags,
  Final: &f.Tags}`.

```go
// Write opens the file at path, calls edit to change its tags and artwork, and
// replaces the file with the edited version. With no pending edits after edit
// returns, the file is not touched.
//
// The replacement is atomic: the new contents are written to a temporary file in
// the same directory, synced, verified by reading them back (the audio data must be
// bit-identical and chapters unchanged), and renamed over the original; the
// directory is then synced. Symlinks are followed and their target replaced. If the
// file changes on disk while Write runs, it returns ErrConcurrentModification and
// leaves it alone. Concurrent Writes to the same file are not coordinated.
//
// The File passed to edit is only valid during the call.
func Write(path string, edit func(f *File) error, opts ...WriteOption) error {
	return WriteContext(context.Background(), path, edit, opts...)
}

// WriteContext is the cancellable form of Write.
func WriteContext(ctx context.Context, path string, edit func(f *File) error, opts ...WriteOption) error
```

`WriteContext` steps:
1. Apply the options. `target, err := filepath.EvalSymlinks(path)`. `st0, err := os.Stat(target)`.
2. `f, err := OpenContext(ctx, target)`, with `defer f.Close()`. (Close may run twice; ignore the
   second error.)
3. Look up the writer; if none, return `ErrUnsupportedWrite` (D6). Then `edit(f)`; if it returns
   an error, return that error unchanged.
4. `edits, err := f.buildEdits(ctx)`. If `edits.Empty()`, return nil.
5. `srcDigest, err := tw.AudioDigest(ctx, f.reader, f.Size)`.
6. `unchanged := func() error`: `os.Stat(target)`; if size or `ModTime` differs from `st0`,
   return `ErrConcurrentModification`.
7. **In-place branch** (`opts.inPlace`, and `tw` implements `registry.InPlaceWriter`, and
   `InPlacePatch` returns `ok`):
   - `unchanged()`;
   - `os.OpenFile(target, os.O_RDWR, 0)`;
   - read `backup` of `len(patch)` bytes at `off`;
   - `WriteAt(patch, off)`, then `Sync`;
   - `verifyFile(ctx, rwFile, st0.Size(), target, f.Format, tw, srcDigest, orig chapters)`;
   - on a verify error: `WriteAt(backup, off)`, `Sync`, and return the error wrapping
     `ErrVerifyFailed`;
   - `PreserveModTime` → `os.Chtimes`;
   - return nil.
8. **Atomic branch**: call `atomicfile.Replace(target, write, atomicfile.Options{...})` with:
   - `write = func(w io.Writer) error { _, err := writeSafely(ctx, tw, w, f.reader, f.Size,
     edits); return err }`;
   - `Verify = func(tmp *os.File) error { st, _ := tmp.Stat(); return verifyFile(ctx, tmp,
     st.Size(), target, ...) }`;
   - `BeforeRename = func() error { _ = f.Close(); return unchanged() }`;
   - `PreserveModTime: opts.preserveModTime`.

`verifyFile(ctx, r io.ReaderAt, size int64, labelPath string, ...)`:
- `openReader(ctx, r, size, labelPath)` (D5: the original path is the label);
- compare its chapters with the source's;
- compute `tw.AudioDigest(ctx, r, size)`, compare with `bytes.Equal`;
- on any mismatch return `fmt.Errorf("%w: <what>", ErrVerifyFailed)`.

Keep each function under gocyclo 15 by extracting `writeInPlace` and `writeAtomic`.

**Verify**: `mise exec -- go build ./... && mise exec -- go vet ./...` → exit 0.

### Step 13: Tests, the round-trip harness skeleton, and examples

**`write_internal_test.go`** (`package audiometa`). It swaps `lookupWriter` for a fake
(`t.Cleanup` restores it; do not use `t.Parallel()` in this file). The fake `TagWriter` records
the `EditSet` it got, writes `"FAKE"` and the source bytes, and returns a fixed `AudioDigest`
(sha256 of the source). Open `testdata/sample.flac` with `Open`. Cases:
1. Real registry, no writer for FLAC yet → `errors.Is(err, ErrUnsupportedWrite)` (D6), for
   `WriteTo` and for `Write` on a temp copy.
2. Fake writer, no edits → the output equals the source bytes (D7) and the fake was not called.
3. `f.Tags.Title = "New"` → the fake got `Fields == [{FieldTitle, ["New"]}]` and no `Keys`.
4. `f.Tags.Set("TITLE","New")` → one KeyEdit, zero FieldEdits.
5. `f.Tags.Set("TITLE","A"); f.Tags.Title = "B"` → `ErrConflictingEdits`.
6. Change `f.Chapters` (append one) → `ErrUnsupportedWrite`.
7. `AddArtwork` with empty data → `ErrInvalidArtwork`. `AddArtwork` with a valid 1×1 PNG and
   `MIMEType: ""` → the fake's `Artwork[0].Art.MIMEType == "image/png"`. `ReplaceArtwork` →
   two ops `Remove, Add`.
8. `Write` with the fake on a temp copy (copy `testdata/sample.flac` into `t.TempDir()`):
   - the edit callback sets Title → the file now starts with `"FAKE"` (the fake output), and
     `TempDir` has no `.audiometa-*.tmp` left;
   - an edit callback returning an error → that error is returned and the file is
     byte-identical;
   - no edits → the file's mtime is unchanged;
   - a fake whose `AudioDigest` returns different bytes for the temp file →
     `errors.Is(err, ErrVerifyFailed)` and the original is byte-identical;
   - concurrent modification: a fake `WriteFile` that, while running, appends one byte to the
     original path → `ErrConcurrentModification` and the original is unchanged apart from that
     byte.
9. A fake writer that panics → the error is a `*CorruptedFileError` (`errors.As`).

**`write_harness_test.go`** (`package audiometa_test`). These are independent audio-payload
oracles. Do **not** call internal packages; they must stay independent of the writers they check.
- `mp3AudioDigest(t, data []byte) [32]byte`: start at 0. If `data[:3]=="ID3"`, start =
  `10 + synchsafe(data[6:10])`, plus 10 more if `data[3]==4 && data[5]&0x10 != 0`. End =
  `len(data)`, minus 128 if the last 128 bytes start with `"TAG"`. Return the sha256 of that range.
- `flacAudioDigest(t, data) [32]byte`: walk the metadata block headers from offset 4 until the
  is-last bit, and hash the rest.
- `m4aAudioDigest(t, data) [32]byte`: walk top-level atoms. Handle `size==1` (64-bit size at
  +8, header 16) and `size==0` (to EOF). Feed every `mdat` payload into one `sha256.New()` in
  file order.
- `m4aTopLevelTypes(t, data) []string`: the top-level atom types in order, excluding `moov`,
  `free` and `skip`. Writers must preserve this list.
- `audioDigest(t, format, data)` dispatches on the format.

Self-tests:
- `TestAudioDigest_MP3IgnoresTag`: digest(sample.mp3) equals the digest of
  `id3Header(v4, body of 20 zero bytes) + sample.mp3[tagEnd:]`.
- `TestAudioDigest_FLACIgnoresMetadata`: digest(sample.flac) equals the digest of `"fLaC" +` its
  STREAMINFO block re-flagged as last `+` the audio.
- `TestAudioDigest_M4AIgnoresMoov`: flip one byte of the `©nam` value inside `moov` (find
  `"Test Title"` with `bytes.Index` and change `T`→`X`) → same digest.

**`write_roundtrip_test.go`** (`package audiometa_test`): the corpus round-trip harness that plans
039–042 fill in.

```go
type roundTripCase struct {
	path     string
	writable bool // flipped to true by the plan that adds the format's writer
}

var roundTripCorpus = []roundTripCase{
	{"testdata/sample.mp3", false},
	{"testdata/sample_mono_vbr.mp3", false},
	{"testdata/sample_mpeg2.mp3", false},
	{"testdata/sample.flac", false},
	{"testdata/sample_upper.flac", false},
	{"testdata/sample.m4a", false},
	{"testdata/sample.m4b", false},
	{"testdata/sample.ogg", false}, // deferred format: stays false
	{"testdata/sample.opus", false}, // deferred format: stays false
}

// tagEdit is one format-agnostic edit; skip lists formats where it must return ErrUnsupportedWrite.
type tagEdit struct {
	name        string
	apply       func(f *audiometa.File) error
	check       func(t *testing.T, f *audiometa.File) // asserts on the re-opened file
	unsupported []audiometa.Format
}
```

`TestWriteRoundTrip_Corpus`, for each case:
- When `!writable`: assert that `WriteTo(io.Discard)` with no edits returns `ErrUnsupportedWrite`,
  and `continue`.
- When `writable`:
  1. A no-edit `WriteTo` is byte-identical to the source.
  2. For each `tagEdit` in `roundTripEdits`: open the file, `apply`, then `WriteTo` a buffer.
     If the format is in `unsupported`, assert `ErrUnsupportedWrite` and move on. Otherwise write
     the buffer to `filepath.Join(t.TempDir(), filepath.Base(path))` (same basename) and
     re-open it. Then:
     - `check(t, reopened)`;
     - the audio digest is equal;
     - the chapters are equal;
     - every raw key not touched by the edit is unchanged;
     - every typed field except the edited ones is unchanged, with **no exclusions**. This is
       the no-inference check: an edit of one field never changes another;
     - for M4A, `m4aTopLevelTypes` is equal;
     - a second `WriteTo` of the same edited `File` is byte-identical to the first.
  3. The same for `roundTripArtworkEdits`.

Seed `roundTripEdits`:
- set Title to `"Nouveau titre ✓ 日本語"`;
- clear Album;
- set Artists to `["A","B"]` and Artist to `"A"`;
- set Comment to a 4096-character string;
- set Year to 1999. Its check is that `Year == 1999`.

Seed `roundTripArtworkEdits`:
- `ReplaceArtwork` with a front-cover 1×1 PNG (build it with `image/png` from the standard
  library in a helper);
- `RemoveAllArtwork`;
- `AddArtwork` with a back-cover PNG. Its `unsupported` is `{FormatM4A, FormatM4B}`.

Plans 039–041 flip `writable` and add format-specific edits. With everything `false`, the
harness only asserts D6, which is the intended no-op table.

**`example_write_test.go`** (`package audiometa_test`):
- `ExampleFile_WriteTo` opens `testdata/sample.ogg`, sets Title, and prints
  `errors.Is(err, audiometa.ErrUnsupportedWrite)`. `// Output: true`. It runs now, and
  documents the unsupported path.
- `ExampleWrite` has **no** `// Output:` line yet, so it is compile-only. Plan 039 makes it
  runnable on MP3. Body: `err := audiometa.Write("book.mp3", func(f *audiometa.File) error {
  f.Tags.Title = "New Title"; return f.ReplaceArtwork(cover) })`.
- `ExampleFile_ReplaceArtwork`, compile-only.

**Verify**:
- `mise exec -- go test -race ./... ` → all `ok`;
- `mise exec -- go test -run 'Example' -v . ` → `ExampleFile_WriteTo` PASS;
- `mise exec -- golangci-lint run ./...` → `0 issues.`;
- `gofmt -l .` → no output;
- `GOOS=windows GOARCH=amd64 mise exec -- go build ./...` → exit 0.

### Step 14: API-freeze review gate (STOP and report)

Do **not** continue past this step on your own.

1. Produce the exported surface of every new symbol:
   ```sh
   for s in Write WriteContext WriteOption WithInPlace WithPreserveModTime \
            File.WriteTo File.WriteToContext File.AddArtwork File.ReplaceArtwork \
            File.RemoveArtwork File.RemoveAllArtwork ErrUnsupportedWrite Tags.Set; do
     echo "=== $s"; mise exec -- go doc -all . "$s"; done > /tmp/038-api.txt
   ```
   (`go doc . ErrUnsupportedWrite` prints the whole `var` block.)
2. Prove that no existing exported API broke. Use `apidiff` against the planned-at commit:
   ```sh
   rm -rf /tmp/am-base && mkdir /tmp/am-base && git archive 0d60738 | tar -x -C /tmp/am-base
   (cd /tmp/am-base && go run golang.org/x/exp/cmd/apidiff@latest -m -w /tmp/base.api github.com/simonhull/audiometa)
   go run golang.org/x/exp/cmd/apidiff@latest -m -w /tmp/head.api github.com/simonhull/audiometa
   go run golang.org/x/exp/cmd/apidiff@latest -m -incompatible /tmp/base.api /tmp/head.api
   ```
   **Expected**: the last command prints no incompatible changes. If `apidiff` cannot be
   fetched, say so in the report and include
   `git diff 0d60738 -- '*.go' ':!*_test.go' ':!internal/**'` instead.
3. Set the 038 row in `plans/README.md` to `IN PROGRESS (awaiting API sign-off)`.
4. Report to the maintainer. Include:
   - `/tmp/038-api.txt`;
   - the apidiff output;
   - the decisions D1–D7 as implemented, with one line each;
   - every place you deviated from this plan;
   - this question: **"Approve this exported surface as frozen? Plan 039 must not start until
     you answer yes."**

**Verify**: the report was sent; `grep -n '| 038 ' plans/README.md` shows `awaiting API sign-off`.

## Test plan

- New test files:
  - `internal/types/fields_test.go`, `fieldmap_test.go`, `edits_test.go`;
  - additions to `internal/types/tags_test.go`;
  - `internal/registry/writer_test.go`;
  - `internal/writeutil/writeutil_test.go`;
  - `internal/atomicfile/atomicfile_test.go`;
  - `write_internal_test.go`, `write_harness_test.go`, `write_roundtrip_test.go`,
    `example_write_test.go`.
- Patterns:
  - `internal/types/tags_test.go` (table style over `Tags`);
  - `file_boundary_test.go` (root-package tests that use `openReader` and panic recovery);
  - `audiometa_corpus_test.go` (corpus loops with `t.Run(path)`).
- Verification: `mise exec -- go test -race ./...`. All pass. The new tests run, and
  `TestWriteRoundTrip_Corpus` reports 9 subtests, each asserting `ErrUnsupportedWrite`.

## Done criteria

All must hold (the maintainer, not you, ticks the last one):

- [ ] `mise exec -- go build ./...`, `go vet ./...` exit 0; cross-builds for windows/amd64 and darwin/arm64 exit 0
- [ ] `mise exec -- go test -race ./...` all `ok`; `TestBenchFixturesParse` unchanged
- [ ] `mise exec -- golangci-lint run ./...` → `0 issues.`; `gofmt -l .` empty
- [ ] `mise exec -- go doc . File.WriteTo` prints `func (f *File) WriteTo(w io.Writer) (int64, error)`
- [ ] `grep -rn 'type TagWriter\|type InPlaceWriter' --include=*.go . | grep -v internal/registry` → no matches (writer interfaces are not exported publicly)
- [ ] `grep -rln 'RegisterWriter(' internal/ | grep -v registry` → no matches (no format writer yet)
- [ ] apidiff reports no incompatible changes against `0d60738`
- [ ] `TestSet_NoCrossFieldSync` and `TestFieldBindings_NoCrossFieldKeys` pass; `grep -n 'Narrator\|Series' write_roundtrip_test.go` shows no exclusion of those fields from the "unchanged" check (no-inference rule)
- [ ] `git status` shows only in-scope files
- [ ] Maintainer signed off on the API (row moved to DONE by the maintainer)

## STOP conditions

- Any `0d60738` excerpt above has drifted, beyond the changes from plan 016 and the idioms pass
  that are listed in the drift check.
- `internal/imagemeta` does not exist, which means plan 016 has not landed.
- `TestBenchFixturesParse` or any pre-existing test fails after Step 4 or Step 9. The Set-sync
  change must not alter parse results. Report the failing test.
- apidiff reports an incompatible change.
- Implementing D2 seems to require an exported method on `types.Tags` (which would become
  public API). Report it instead of adding one.
- You want to add any exported symbol not in D1. Ask instead.
- **Step 14 always stops.**

## Maintenance notes

- **The frozen surface is D1.** The additive candidates the maintainer may want later are
  `WithBackup()`, `Format.Writable()`, chapter editing (`File.SetChapters`), a progress callback
  and a CLI `write` subcommand. All can be added without breaking the surface.
- **Field bindings mirror the parsers.** If a parser starts reading a new key (for example
  `TPUB` for Publisher in MP3, which it does not read today), update `internal/types/fieldmap.go`
  in the same change. Otherwise Set-sync and write mapping drift from read behavior.
  `TestFieldsCoverEveryTagsField` fails when a typed field is added to `Tags` without a `Field`
  constant.
- **No cross-field inference** (maintainer rule). Neither readers nor writers derive one field
  from another, so the harness compares **every** non-edited typed field. If a future change
  reintroduces a fallback (for example Narrator from Composer), `TestSet_NoCrossFieldSync` and
  the harness will fail. That failure is intended.
- `Write` closes the source `File` before renaming (required on Windows; harmless elsewhere).
  If `File.Close` ever gains side effects, revisit `BeforeRename`.
- `verifyFile` reads all audio twice, once for the source digest and once for the temp file. For
  multi-GB M4B files this is the dominant cost of `Write`. An optimization (hash while copying)
  belongs in the writers, not here. Measure first.
- Reviewer focus:
  - `Diff`'s conflict rule (Step 5);
  - the `Set` precedence recompute (Step 4);
  - every error path in `atomicfile.Replace` leaves the original untouched and removes the temp
    file.
