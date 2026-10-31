# Plan 029: Map missing M4A ilst fields, decode integer atoms, and skip covr during Open

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving on. If a
> STOP condition occurs, stop and report — do not improvise. When done, update
> the status row for this plan in `plans/README.md` (add a row for 029 if none
> exists) — unless a reviewer dispatched you and told you they maintain the index.
>
> **Drift check (run first)**: `git diff --stat aca9750..HEAD -- internal/m4a/metadata.go internal/m4a/metadata_test.go internal/m4a/audiobook.go internal/m4a/audiobook_test.go`
> If any of these changed, compare the "Current state" excerpts against the live code. If the
> only drift comes from plan 002 (`CheckLen` guards) or plan 005 (raw-tag recording in
> `mapTagToField`) and `plans/README.md` marks that plan DONE, keep their additions and apply
> this plan around them. Any other mismatch is a STOP condition.

## Status

- **Priority**: P2
- **Effort**: M
- **Risk**: LOW (additive mappings; the only behavior changes are for values that were previously garbage or empty)
- **Depends on**: none
- **Category**: bug
- **Planned at**: commit `aca9750`, 2026-10-30

## Why this matters

The M4A/M4B ilst parser treats every tag atom's payload as text, ignoring the `data` atom's
type indicator. Four concrete consequences for an audiobook server: (1) `©mvi` (series index) is
written by iTunes/mutagen as a big-endian integer (type 21, e.g. bytes `00 05`), so
`Tags.SeriesPart` becomes binary garbage — and because it is non-empty it also suppresses the
fallback series-part resolution; (2) `©day` holding an ISO date (`2015-03-01T07:00:00Z`, the
form Audible/iTunes write) fails `strconv.Atoi`, so `Year` stays 0 and `Date` is never set;
(3) common atoms `aART` (album artist), `disk` (disc n/m), `desc`/`ldes` (description),
`cprt` (copyright) and `©lyr` (lyrics) are never mapped although `types.Tags` has fields for
all of them; (4) the `covr` atom (cover art, typically 0.1–2 MB) is fully read into memory and
converted to a string on every `Open`, then discarded. Separately, custom `----` series-part
tags are looked up with exact-case keys, so `SERIES PART` or `series part` is ignored. After
this plan, those fields are populated correctly and `Open` no longer touches cover bytes.

## Current state

Files:
- `internal/m4a/metadata.go` — ilst text-tag parsing (`parseMetadataTag`, `extractIlstMetadata`,
  `mapTagToField`, `parseTrackNumber`).
- `internal/m4a/audiobook.go` — second ilst walk for freeform `----` atoms and series-part
  fallback resolution.
- `internal/types/tags.go:17-54` — `Tags` struct. Fields this plan uses all exist:
  `AlbumArtist`, `Copyright`, `Date`, `Description`, `Lyrics`, `SeriesPart`, `DiscNumber`,
  `DiscTotal`, `Year`. **Do not add fields.**
- `internal/m4a/parser.go:110` calls `extractIlstMetadata`; `parser.go:141` calls
  `parseAudiobookTags` (after technical info and chapters).

`internal/m4a/metadata.go:12-48` — the type indicator (bytes 1–3 of the data atom's
version/flags word) is skipped unread; the whole payload is read and stringified:

```go
// parseMetadataTag extracts the string value from an iTunes metadata tag atom.
func parseMetadataTag(sr *binary.SafeReader, tagAtom *Atom) (string, error) {
	...
	dataAtom, err := findAtom(sr, tagAtom.DataOffset(), tagAtom.DataOffset()+int64(tagAtom.DataSize()), "data")
	if err != nil {
		// No data atom found - return empty
		return "", nil
	}

	// Skip version (1 byte) + flags (3 bytes) + reserved (4 bytes) = 8 bytes
	valueOffset := dataAtom.DataOffset() + 8
	valueSize := int64(dataAtom.DataSize()) - 8
	...
	buf := make([]byte, valueSize)
	if err := sr.ReadAt(buf, valueOffset, "metadata value"); err != nil {
		return "", err
	}
	value := string(buf)
	value = strings.TrimRight(value, "\x00")
	value = strings.TrimSpace(value)
	return value, nil
}
```

`internal/m4a/metadata.go:62-83` — every child except `trkn` goes through the text path,
including `covr` and `----`:

```go
		if tagAtom.Type == "trkn" {
			trackData, err := parseTrackNumber(sr, tagAtom)
			if err == nil {
				file.Tags.TrackNumber = trackData.Number
				file.Tags.TrackTotal = trackData.Total
			}
		} else {
			// Parse as text tag
			value, err := parseMetadataTag(sr, tagAtom)
			...
				mapTagToField(tagAtom.Type, value, file)
```

`internal/m4a/metadata.go:93-124` — `mapTagToField` (relevant cases):

```go
	case "\xA9day": // Year (©day)
		if year, err := strconv.Atoi(value); err == nil {
			file.Tags.Year = year
		}
	...
	case "\xA9des": // Description (©des) - long description separate from comment
		file.Tags.Description = value
	...
	case "\xA9mvi": // Movement Index (©mvi) - used for series position
		if file.Tags.SeriesPart == "" {
			file.Tags.SeriesPart = value
		}
	}
```

No cases exist for `aART`, `disk`, `desc`, `ldes`, `cprt`, `\xA9lyr`.

`internal/m4a/metadata.go:132-167` — `parseTrackNumber` reads the `data` payload as
`[2 reserved][2 number][2 total]...`. The `disk` atom has the same first 6 bytes
(`[2 reserved][2 disc][2 total]`, payload length 6 instead of trkn's 8), so
`parseTrackNumber` works unchanged for `disk`.

`internal/m4a/audiobook.go:31-33` stores freeform names with original case:

```go
			if fieldName, value, err := parseCustomAtomWithTags(sr, atom, file); err == nil {
				customTags[fieldName] = value
			}
```

`internal/m4a/audiobook.go:59-61` — the fallback only runs when `SeriesPart` is still empty
(this is why a garbage `©mvi` blocks it):

```go
	if file.Tags.Series != "" && file.Tags.SeriesPart == "" {
		file.Tags.SeriesPart = resolveSeriesPart(sr, file, customTags)
	}
```

`internal/m4a/audiobook.go:168-181` — exact-case lookups, while `mapAudiobookField`
(`audiobook.go:131-133`) lowercases the same names:

```go
func resolveSeriesPart(sr *binary.SafeReader, file *types.File, customTags map[string]string) string {
	// Priority 1: Explicit custom iTunes atoms
	if part := customTags["Series Part"]; part != "" {
		return part
	}
	if part := customTags["Series Position"]; part != "" {
	...
	if part := customTags["Part"]; part != "" {
	...
	if part := customTags["Volume"]; part != "" {
```

Existing tests (`internal/m4a/audiobook_test.go:175-223, 370-404, 405+`) call
`resolveSeriesPart` directly with maps keyed `"Series Part"`, `"Part"`, etc. — they must keep
passing unchanged.

### Data atom type indicators (Apple "well-known types")

The 4 bytes after the `data` atom header are `[1 version][3 type indicator]`, then 4 bytes
locale/reserved, then the payload. Relevant type indicators:

| Value | Meaning |
|---|---|
| 0 | implicit / reserved (used by `trkn`, `disk`; some old writers use it for text) |
| 1 | UTF-8 text |
| 13 | JPEG, 14 PNG, 27 BMP |
| 21 | big-endian signed integer (payload 1, 2, 3, 4 or 8 bytes) |
| 22 | big-endian unsigned integer (payload 1, 2, 3, 4 or 8 bytes) |

### Conventions

- Warnings: `file.Warnings = append(file.Warnings, types.Warning{Stage: types.StageMetadata, Message: ..., Err: err})` — see `metadata.go:74-78`.
- Binary reads: `binary.Read[T](sr, off, "what")` and `sr.ReadAt(buf, off, "what")` on
  `*binary.SafeReader` (`internal/binary/reader.go:33-80`).
- Test fixtures: synthetic bytes. Builders already in the package: `createMockAtom(type, data)`
  (`internal/m4a/atoms_test.go:12`), `createDataAtom(value)` / `createMetadataItem(itemType, value)`
  (`internal/m4a/metadata_test.go:13-54`; note `createDataAtom` writes version/flags
  `uint32(1)` = type 1 UTF-8), `createTrackDataAtom` (`metadata_test.go:165`),
  `createCustomAtom(namespace, name, value)` (`audiobook_test.go:19`). Model new tests on
  `TestExtractIlstMetadata` (`metadata_test.go:99-132`).

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

Lint MUST run via `mise exec --` (a stale global golangci-lint earlier on PATH reports false
typecheck errors). The toolchain is Go 1.27 via mise, but `go.mod` says `go 1.26.0`: use no
standard-library API newer than Go 1.26.

## Scope

**In scope** (the only files you should modify):
- `internal/m4a/metadata.go`
- `internal/m4a/metadata_test.go`
- `internal/m4a/audiobook.go` (`resolveSeriesPart` lookup only)
- `internal/m4a/audiobook_test.go` (new test only)

**Out of scope** (do NOT touch):
- Populating the raw tag map (`Tags.Set`/`All`/`Get`) or `Tags.Artists` for M4A — that is plan
  005 (`plans/005-raw-tag-parity.md`).
- Allocation caps on `make([]byte, valueSize)` — plan 002. Do not add `CheckLen` yourself.
- Merging the two ilst walks (`extractIlstMetadata` and `parseAudiobookTags`) into one — tempting
  but changes ordering semantics (freeform tags currently run after chapters/technical); leave it.
- Type-indicator handling for the freeform `----` `data` child in `parseCustomAtomWithTags`
  (`audiobook.go:105-116`) — freeform values are text in practice.
- `internal/m4a/artwork.go` — artwork extraction is a separate API and already reads `covr`
  correctly.
- `internal/types/tags.go` — no new fields.
- Series-fallback consolidation across formats — plan 016.

## Git workflow

- Branch: `advisor/029-m4a-ilst-field-coverage`
- Commit per step; gitmoji single-line message, no body, no attribution
  (e.g. `🐛 Decode integer-typed M4A ilst atoms`; repo example:
  `♻️ Modernize for Go 1.26, add context plumbing, refresh tooling`).
- Do NOT push or open a PR.

## Steps

### Step 1: Decode the data atom by type indicator in `parseMetadataTag`

In `internal/m4a/metadata.go` `parseMetadataTag`, after locating `dataAtom` and before
allocating the payload buffer:

1. Read the version/flags word: `vf, err := binary.Read[uint32](sr, dataAtom.DataOffset(), "data type indicator")`;
   on error return `"", err`. `typeIndicator := vf & 0x00FFFFFF`.
2. Keep the existing `valueSize <= 0` early return.
3. Switch on `typeIndicator`:
   - `0`, `1` → existing text path, unchanged (read, `TrimRight "\x00"`, `TrimSpace`).
   - `21` (signed) / `22` (unsigned) → if `valueSize` is not one of 1, 2, 3, 4, 8 return `"", nil`.
     Otherwise read the payload (small, ≤ 8 bytes), accumulate big-endian into a `uint64`, and
     return `strconv.FormatUint(u, 10)` for 22. For 21, sign-extend from `valueSize*8` bits
     (`int64(u<<(64-bits)) >> (64-bits)`) and return `strconv.FormatInt(...)`.
   - anything else (13/14/27 images, 2 UTF-16, floats, unknown) → return `"", nil` **without
     reading the payload**.

Put the integer decoding in its own helper, e.g.
`func decodeBEInteger(b []byte, signed bool) string`, so `parseMetadataTag` stays under the
`gocyclo` limit (15, see Step 3). Add named constants for the indicators (e.g. `dataTypeImplicit = 0`, `dataTypeUTF8 = 1`,
`dataTypeBESigned = 21`, `dataTypeBEUnsigned = 22`) near the top of `metadata.go`; keep the
signature `func parseMetadataTag(sr *binary.SafeReader, tagAtom *Atom) (string, error)`
unchanged.

**Verify**: `go build ./... && go test ./internal/m4a/ -run 'TestParseMetadataTag|TestExtractIlstMetadata|TestMapTagToField' -v`
→ all existing tests PASS (their fixtures use type 1).

### Step 2: Skip `covr` and `----` by header size; add `disk`

In `extractIlstMetadata` (`metadata.go:62-83`), replace the `if tagAtom.Type == "trkn" { ... } else { ... }`
with a `switch tagAtom.Type`:

```go
switch tagAtom.Type {
case "trkn":
	// existing trkn block, unchanged
case "disk":
	if d, err := parseTrackNumber(sr, tagAtom); err == nil {
		file.Tags.DiscNumber = d.Number
		file.Tags.DiscTotal = d.Total
	}
case "covr", "----":
	// covr: artwork, extracted lazily by ExtractArtwork; never read during Open.
	// ----: freeform atoms, handled by parseAudiobookTags.
default:
	// existing text-tag block (parseMetadataTag + warning + mapTagToField), unchanged
}
```

The trailing `offset += int64(tagAtom.Size)` already advances past the skipped atom using only
the header — no data read is needed.

**Verify**: `go build ./... && go test ./internal/m4a/` → `ok`.

### Step 3: Map the missing fields and fix `©day` / `©mvi` / descriptions

The repo's lint config sets `gocyclo` `min-complexity: 15` (`.golangci.yml:70-71`), and
`mapTagToField` is already near it, so adding cases to the switch would trip the lint gate.
Restructure `mapTagToField` (`metadata.go:93-124`) as follows:

1. First line: `if value == "" { return }` (no mapping ever wants an empty string; this also
   stops `©gen`/`©wrt` from appending empty entries).
2. A package-level table for plain overwrite fields, consulted first:
   ```go
   // ilstTextFields maps ilst atoms whose value is assigned verbatim to a Tags field.
   var ilstTextFields = map[string]func(*types.Tags) *string{
   	"\xA9nam": func(t *types.Tags) *string { return &t.Title },
   	"\xA9ART": func(t *types.Tags) *string { return &t.Artist },
   	"\xA9alb": func(t *types.Tags) *string { return &t.Album },
   	"\xA9cmt": func(t *types.Tags) *string { return &t.Comment },
   	"\xA9grp": func(t *types.Tags) *string { return &t.Grouping },
   	"\xA9lyr": func(t *types.Tags) *string { return &t.Lyrics },
   	"aART":    func(t *types.Tags) *string { return &t.AlbumArtist },
   	"cprt":    func(t *types.Tags) *string { return &t.Copyright },
   	"ldes":    func(t *types.Tags) *string { return &t.Description }, // long form always wins
   }
   ```
   `if field, ok := ilstTextFields[tag]; ok { *field(&file.Tags) = value; return }`
   Keep the existing per-atom comments (e.g. `// ©nam title`) next to the entries.
3. The remaining switch keeps only the special cases:
   - `"\xA9gen"` → append to `Genres`; `"\xA9wrt"` → append to `Composers` (unchanged).
   - `"\xA9day"` → call a new helper `applyIlstDate(file, value)` that sets
     `file.Tags.Date = value` and, if `len(value) >= 4` and `strconv.Atoi(value[:4])` succeeds
     with a result `> 0`, sets `file.Tags.Year`. (Mirrors `internal/vorbis/comments.go:53-61`.)
     `"2015"` → Year 2015, Date `"2015"`; `"2015-03-01T07:00:00Z"` → Year 2015, Date the full string.
   - `"desc", "\xA9des"` → `if file.Tags.Description == "" { file.Tags.Description = value }`.
     Combined with `ldes` in the table, priority is `ldes` > first of `desc`/`©des`, regardless
     of atom order. (This changes `©des` from unconditional to fill-if-empty — intended.)
   - `"\xA9mvn"` → unchanged (fill `Series` if empty).
   - `"\xA9mvi"` → `if value != "0" && file.Tags.SeriesPart == "" { file.Tags.SeriesPart = value }`
     (an integer index of 0 means "unset" in iTunes).

**Verify**: `go build ./... && go test ./internal/m4a/` → `ok`; and
`mise exec -- golangci-lint run ./internal/m4a/...` → `0 issues.`

### Step 4: Case-insensitive custom series-part lookup

In `internal/m4a/audiobook.go`, add:

```go
// lookupFold returns the first non-empty value whose key equals key case-insensitively.
func lookupFold(tags map[string]string, key string) string {
	for k, v := range tags {
		if v != "" && strings.EqualFold(k, key) {
			return v
		}
	}
	return ""
}
```

and change the four Priority-1 lookups in `resolveSeriesPart` (`audiobook.go:170-181`) to
`lookupFold(customTags, "Series Part")`, `"Series Position"`, `"Part"`, `"Volume"`, keeping
their order. Do not change how `customTags` is populated (existing tests pass maps directly).

**Verify**: `go test ./internal/m4a/ -run 'TestResolveSeriesPart|TestParseAudiobookTags' -v` → all PASS.

### Step 5: Regression tests

Add the tests in "Test plan" below.

**Verify**: `go test ./internal/m4a/ -run 'TestIlst|TestParseMetadataTag_Integer|TestParseAudiobookTags_SeriesPartCaseInsensitive' -v`
→ all new tests PASS; then the full gate list under "Done criteria".

## Test plan

Add to `internal/m4a/metadata_test.go` a builder for typed data atoms, modelled on
`createDataAtom` (`metadata_test.go:13-31`):

```go
// createTypedMetadataItem builds an ilst item whose data atom has the given type indicator.
func createTypedMetadataItem(itemType []byte, typeIndicator uint32, payload []byte) []byte
```

(data atom = size, `"data"`, `uint32(typeIndicator)` (version 0), `uint32(0)` reserved, payload;
wrap with `createAtomWithType(itemType, dataAtom)` from `audiobook_test.go:48`.)

New tests (model on `TestExtractIlstMetadata`, `metadata_test.go:99-132`):

1. `TestParseMetadataTag_Integer` — table: type 21 payload `00 05` → `"5"`; type 21 payload
   `FF FF` → `"-1"`; type 22 payload `FF FF` → `"65535"`; type 22 payload `00 00 00 07` → `"7"`;
   type 21 payload of 5 bytes → `""`; type 13 payload of 100 bytes → `""`.
2. `TestIlstSeriesPartInteger` — ilst with `©mvn` text `"The Expanse"` and `©mvi` type 21
   payload `00 05`; after `extractIlstMetadata`, `Series == "The Expanse"`, `SeriesPart == "5"`.
3. `TestIlstSeriesPartZeroIgnored` — `©mvi` type 21 payload `00 00` → `SeriesPart == ""`.
4. `TestIlstDate` — table: `©day` `"2015-03-01T07:00:00Z"` → Year 2015, Date
   `"2015-03-01T07:00:00Z"`; `"1999"` → Year 1999, Date `"1999"`; `"unknown"` → Year 0, Date
   `"unknown"`.
5. `TestIlstNewFields` — ilst with `aART`, `cprt`, `\xA9lyr` text items and a `disk` item (type 0,
   payload `00 00 00 02 00 03`) → `AlbumArtist`, `Copyright`, `Lyrics` set; `DiscNumber == 2`,
   `DiscTotal == 3`.
6. `TestIlstDescriptionPriority` — order `ldes`, `desc`, `©des` → Description == ldes value;
   order `©des`, `desc` → Description == ©des value; order `desc`, `ldes` → ldes value.
7. `TestIlstSkipsCoverData` — define in the test file:
   ```go
   type maxReadReaderAt struct {
   	r   io.ReaderAt
   	max int
   }
   func (m *maxReadReaderAt) ReadAt(p []byte, off int64) (int, error) {
   	m.max = max(m.max, len(p))
   	return m.r.ReadAt(p, off)
   }
   ```
   Build ilst = `©nam "Title"` + `covr` item (type 13, 65536-byte payload) + `aART "AA"`.
   Wrap with `createMockAtom("ilst", ...)`, read through
   `audiobinary.NewSafeReader(&maxReadReaderAt{r: bytes.NewReader(ilst)}, ...)`, read the ilst
   header, call `extractIlstMetadata`. Assert `Title == "Title"`, `AlbumArtist == "AA"` (proves
   the walk continued past covr), and `rr.max < 1024`. Before this plan `rr.max` would be 65536.

Add to `internal/m4a/audiobook_test.go`:

8. `TestParseAudiobookTags_SeriesPartCaseInsensitive` — ilst with
   `createCustomAtom("com.apple.iTunes", "Series", "Dune")` and
   `createCustomAtom("com.apple.iTunes", "SERIES PART", "3")`; reader from
   `audiobinary.NewSafeReader(bytes.NewReader(ilst), int64(len(ilst)), "")`; after
   `parseAudiobookTags`, `SeriesPart == "3"`. Model on `TestParseAudiobookTags_Series`
   (`audiobook_test.go:76`).

Verification: `go test ./internal/m4a/ -v 2>&1 | grep -c '^--- PASS'` increases by at least 8
(table tests count per top-level test) and `go test ./...` → all `ok`.

## Done criteria

- [ ] `go build ./...` and `go vet ./...` exit 0
- [ ] `go test ./...` and `go test -race ./...` → all `ok`, including the 8 new tests
- [ ] `mise exec -- golangci-lint run ./...` → `0 issues.`
- [ ] `gofmt -l .` → no output
- [ ] `grep -n '"covr"' internal/m4a/metadata.go` → one match (the skip case)
- [ ] `grep -n 'customTags\["' internal/m4a/audiobook.go` → no matches (all lookups go through `lookupFold`)
- [ ] `git status --short` shows only the four in-scope files modified (plus `plans/README.md`)
- [ ] `plans/README.md` status row for 029 updated

## STOP conditions

Stop and report back (do not improvise) if:

- The excerpts above don't match the live code and the drift isn't explained by plan 002/005
  as described in the drift check.
- An existing test fails after Step 1 or Step 3 — specifically, if a test pins the old
  unconditional `©des` overwrite or the old `©day` behavior, report it instead of editing it.
- Implementing any step seems to require adding a field to `types.Tags` or touching
  `internal/m4a/artwork.go` / `parser.go`.
- `golangci-lint` still flags `gocyclo` on `mapTagToField` or `parseMetadataTag` after the
  Step 3 restructure — extract another small helper; do not add a `//nolint`. If that fails
  twice, STOP and report.

## Maintenance notes

- Plan 005 will add raw-tag recording after the switch in `mapTagToField`; whichever plan lands
  second must keep both. With this plan, integer atoms (e.g. `©mvi`, `tmpo`, `stik`) arrive as
  decimal strings, which is what 005's raw map should store.
- Plan 002's `CheckLen` guard for `metadata.go:37` should sit in the text/integer branches,
  just before the `make`.
- The `0`/`1`-only text policy means UTF-16 (type 2) text atoms now yield `""` instead of
  mojibake. If a real file shows up with type 2, add UTF-16BE decoding via `unicode/utf16`.
- Reviewer: check the sign extension for 3-byte type-21 payloads, and that `covr` truly
  incurs no payload read (test 7).
- Deferred: merging the two ilst walks; freeform `----` type decoding.
