# Plan 042: Gate write support in CI (corpus round-trip, write fuzzing, ffprobe oracle) and document it

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report. Do not improvise. When done, update the status row for this plan
> in `plans/README.md`, unless a reviewer dispatched you and told you they
> maintain the index.
>
> **This plan has two parts.** **Part A** (Steps 1–5: the oracle test, the fuzz target and the CI
> job) may start as soon as plan 039 is DONE. **Part B** (Steps 6–9: README, godoc and
> CHANGELOG) flips a format to "supported" only after that format's writer plan is DONE **and**
> the Part A gate is green for it. The plan is DONE only when 041 is DONE and every step has
> passed. If you are dispatched before 041 is DONE, do Part A plus the Part B rows for the
> formats that are ready. Then set the row to `IN PROGRESS (Part B waiting on 040/041)` and stop.
>
> **Drift check (run first)**: `git diff --stat 0d60738..HEAD -- .github/workflows/ci.yml README.md doc.go CHANGELOG.md write_roundtrip_test.go write_harness_test.go`
> Expected drift:
> - plans 038–041 created and edited `write_roundtrip_test.go` and `write_harness_test.go`;
> - plan **009** (README and godoc accuracy, already DONE) edited `README.md` and `doc.go`;
> - plan 016 and the **Go 1.27 idioms pass** (`binary.Read[T](sr, …)` → `sr.Read[T](…)`,
>   benchmarks → `b.Loop`) do not touch these files.
>
> If `README.md`'s "Supported Formats" table no longer has a `Write` column, or `ci.yml` no longer
> has the `test`/`lint`/`build` jobs quoted below, STOP.

## Status

- **Priority**: P2
- **Effort**: M
- **Risk**: LOW (tests, CI and docs only; no library code)
- **Depends on**: plans/038-write-api-foundations.md; Part A needs plans/039-mp3-id3v2-writer.md; Part B needs 040 and 041 for their formats
- **Category**: tests
- **Planned at**: commit `0d60738` (branch `fix/improve-plans`), 2026-10-31

## Why this matters

The go decision is conditional. The memo says "no write code merges until the round-trip harness
in the design doc exists and runs in CI" (`docs/design/020-write-support-decision.md`,
"Recommendation"). Plans 038–041 build the harness, but nothing runs it under an external
decoder or fuzzes the writers yet. Nothing tells users either.

This plan does three things:
- it makes CI fail on any regression in write fidelity;
- it adds a fuzz target that exercises random edits on real files;
- it publishes the support matrix, the guarantees and the limits. It does that only for formats
  that pass, because a "✓" in the README is a promise to users with real audiobooks.

## Current state

Excerpts are from `0d60738`.

**CI** (`.github/workflows/ci.yml`) has jobs `test` (`go test -v -race -coverprofile=coverage.txt
-covermode=atomic ./...`, a coverage summary, and govulncheck), `lint` (golangci-lint-action
`v2.15.0`) and `build` (`go build`, a race build, and a stringer freshness check). Settings at the
top:

```yaml
permissions:
  contents: read

env:
  GO_VERSION: '1.27'
  STRINGER_VERSION: v0.52.0
  GOVULNCHECK_VERSION: v1.9.0
```

Actions are pinned by SHA, for example
`actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1` and
`actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e # v7.0.0`. Reuse exactly these pins.
No job installs ffmpeg, and no job runs fuzzing.

**README** (`README.md:135-146`):

```markdown
| Format      | Read | Write | Artwork | Chapters | Technical Info |
|-------------|------|-------|---------|----------|----------------|
| FLAC        | ✓    | 🚧    | ✓       | ✓        | ✓              |
| MP3         | ✓    | 🚧    | ✓       | ✓        | ✓              |
| M4A         | ✓    | 🚧    | ✓       | ✓        | ✓              |
| M4B         | ✓    | 🚧    | ✓       | ✓        | ✓              |
| Ogg Vorbis  | ✓    | 🚧    | -       | ✓        | ✓              |
| Opus        | ✓    | 🚧    | -       | ✓        | ✓              |

🚧 = Under design (see docs/design/020-write-support-design.md); not available yet.
```

**Drift already known:** plan 009 landed after `0d60738`. On the live branch every `Write` cell
reads `planned`, and the footnote has been replaced by a paragraph that starts "audiometa is
read-only today. Write support is planned and designed …". Step 6 edits whichever form is live:
replace the `planned`/`🚧` cells and that paragraph or footnote.

**CHANGELOG** (`CHANGELOG.md`) has an `## Unreleased` section with the subsections
`### Breaking-ish`, `### Added`, `### Fixed`, `### Changed`, `### Security`, `### Deprecated`.

**`doc.go`** is the package godoc. It describes reading (`:9` "Reading metadata from an audio
file:") and has no write section.

**Fuzz convention**: each format package has `FuzzParse<Format>` (for example
`internal/mp3/fuzz_test.go:11-28`). It seeds from `../../testdata/<file>`, adds tiny synthetic
inputs, and asserts only "no panic, no hang". The root package has no fuzz target.

**Harness from plans 038–041** (read the live files):
- `write_roundtrip_test.go`: `roundTripCorpus`, `roundTripEdits`, `roundTripArtworkEdits`,
  `TestWriteRoundTrip_Corpus`;
- `write_harness_test.go`: `audioDigest`, `m4aTopLevelTypes`;
- `write_internal_test.go` (package `audiometa`): has access to `openReader` (`file.go:207`).

**Tools available locally**: `ffmpeg`, `ffprobe`, `flac`, `metaflac`, `mid3v2`. On GitHub's
`ubuntu-latest`, install `ffmpeg` and `flac` with apt.

## Commands you will need

| Purpose | Command | Expected |
|---|---|---|
| Oracle test | `AUDIOMETA_REQUIRE_ORACLES=1 mise exec -- go test -race -run 'WriteOracle' -v .` | all PASS (none SKIP) |
| Fuzz (short) | `mise exec -- go test -run '^$' -fuzz '^FuzzWriteRoundTrip$' -fuzztime 60s .` | `PASS`, no new crashers |
| Harness | `mise exec -- go test -race -run 'WriteRoundTrip\|AudioDigest\|Example' -v .` | all PASS |
| All | `mise exec -- go test -race ./...` | all `ok` |
| Workflow lint | `mise exec -- go run github.com/rhysd/actionlint/cmd/actionlint@latest .github/workflows/ci.yml` | no output |
| Lint / format | `mise exec -- golangci-lint run ./...` / `gofmt -l .` | `0 issues.` / empty |

## Scope

**In scope**:
- `write_oracle_test.go` (create, `package audiometa_test`)
- `write_fuzz_test.go` (create, `package audiometa`, so it can use `openReader`)
- `write_roundtrip_test.go` (add the bench-fixture rows)
- `.github/workflows/ci.yml` (add the `write-gate` job)
- `README.md` (the Write column, the footnote and a new "Writing metadata" section)
- `doc.go` (a write paragraph)
- `CHANGELOG.md` (Unreleased entries)

**Out of scope**:
- Any non-test `.go` file. If a gate exposes a writer bug, STOP and report it against the plan
  (039/040/041) that owns that writer. Do not fix it here.
- `testdata/` (no new fixtures; the bench fixtures already exist), and `generate*.sh`.
- Ogg/Opus write support. They stay "not supported" in every document.
- `RELEASING.md` and `.goreleaser.yaml`. Cutting a release is the maintainer's call.

## Git workflow

- Branch: `advisor/042-write-release-gate`
- Commits: gitmoji, single line, no body, no attribution. Suggested:
  - `✅ Add ffprobe oracle and write fuzz target`
  - `👷 Add write-gate CI job`
  - `📝 Document metadata write support`
- Do NOT push or open a PR. If the maintainer pushes, the new CI job must be green before Part B
  merges.

## Steps

### Step 1: Add the bench fixtures to the round-trip corpus

In `write_roundtrip_test.go`, append these rows:
- `{"testdata/bench/book.mp3", true}`, `{"testdata/bench/book.flac", true}` and
  `{"testdata/bench/book.m4b", true}`. These have 50 chapters each and rich tags, generated by
  `internal/testcorpus/generate_bench.sh`.
- `{"testdata/bench/book.opus", false}`.

Set a row to `true` only when its format's writer plan is DONE. Leave it `false` otherwise, and
note that in your report.

**Verify**: `mise exec -- go test -race -run 'WriteRoundTrip_Corpus' -v .` → PASS, with the
bench subtests running the edit loop.

### Step 2: The external oracle test

Create `write_oracle_test.go` (`package audiometa_test`):

```go
// oracleTool returns the path of name, or skips (fails when AUDIOMETA_REQUIRE_ORACLES=1).
func oracleTool(t *testing.T, name string) string
```

`TestWriteOracle`. For every `writable` row of `roundTripCorpus`, and for each of three edits
(Title set; the 4 KiB Comment; `ReplaceArtwork` with the 1×1 PNG, skipped for formats where it
is unsupported):
1. Open the file, apply the edit, `WriteTo` a buffer, and write the buffer to
   `t.TempDir()/<basename>`.
2. Run `ffprobe -v error -show_format -show_streams -show_chapters -of json <out>`. It must exit
   0 with empty stderr. Decode the JSON and assert:
   - `len(streams)` and `len(chapters)` equal the same command's results on the **source**;
   - for the Title edit, `format.tags` has a key equal to `title` under `strings.EqualFold`, whose
     value is the edited title.
3. Run `ffmpeg -v error -i <out> -f null -`. It must exit 0 with empty stderr. This means the
   whole stream decodes.
4. For FLAC outputs, also run `flac -t -s <out>`, which must exit 0 (the STREAMINFO MD5 still
   matches the decoded audio).

Use `exec.CommandContext` with a 60 s timeout per command. Capture stderr into a buffer and
include it in failure messages.

`oracleTool` uses `exec.LookPath(name)`. On failure it calls `t.Fatalf` when
`os.Getenv("AUDIOMETA_REQUIRE_ORACLES") == "1"`, and `t.Skipf` otherwise.

**Verify**:
- `AUDIOMETA_REQUIRE_ORACLES=1 mise exec -- go test -race -run WriteOracle -v .` → all PASS,
  and `grep -c SKIP` of the output is `0`;
- `grep -n 'LookPath' write_oracle_test.go` → 1 match.

### Step 3: `FuzzWriteRoundTrip`

Create `write_fuzz_test.go` (`package audiometa`). Signature: `f.Fuzz(func(t *testing.T, file
[]byte, ops []byte) { ... })`.

- **Seeds**: for each writable corpus path (read with `os.ReadFile`, skipping missing files), add
  `(fileBytes, []byte{})` and `(fileBytes, []byte{0, 1, 'x', 2, 'y'})`. Also add
  `([]byte("ID3\x04\x00\x00\x00\x00\x00\x00"), []byte{0})` and `([]byte("fLaC"), []byte{0})`.
- **Body**:
  1. `p, err := openReader(ctx, bytes.NewReader(file), int64(len(file)), "fuzz")`. If there is an
     error, return.
  2. Build `f := &File{File: *p.file, reader: bytes.NewReader(file), parser: p.parser}`.
  3. If `registry.GetWriter(f.Format) == nil`, return.
  4. Decode `ops` as a little interpreter, consuming bytes until they run out. The op byte is
     `b % 6`:
     - **0** sets a raw key: the key index is the next byte, modulo the format's
       `types.FieldBindings` keys plus `"ZZTEST"`; the value is the next `n` bytes, where `n` is
       the next byte modulo 32;
     - **1** sets `Title` to the next `n` bytes;
     - **2** sets `Artists` to up to 3 short values;
     - **3** sets `Year` from the next byte plus 1900;
     - **4** calls `AddArtwork` with a fixed 1×1 PNG and type `next % 21`;
     - **5** calls `RemoveArtwork(ArtworkType(next % 21))`.

     Ignore errors from `AddArtwork`.
  5. `var buf bytes.Buffer; _, err = f.WriteToContext(ctx, &buf)`. **Invariants**:
     - it never panics (panics surface as `*CorruptedFileError` through `writeSafely`; fail when
       `errors.As` finds one whose `Reason` starts with `"parser panic"`);
     - if `err == nil`:
       - `openReader` on `buf` succeeds;
       - `registry.GetWriter(f.Format).AudioDigest` of the input equals that of `buf`;
       - a second `WriteToContext` yields identical bytes.
- **Bounds**: return early if `len(file) > 1<<20`. Use a `context.WithTimeout` of 10 s per
  input.

**Verify**: `mise exec -- go test -run '^$' -fuzz '^FuzzWriteRoundTrip$' -fuzztime 60s .` → ends
with `PASS` and no `testdata/fuzz/FuzzWriteRoundTrip/` crasher files. If a crasher appears,
STOP and report the input path, plus which writer it hit.

### Step 4: The `write-gate` CI job

Add this job to `.github/workflows/ci.yml`, using the same pinned actions as the `test` job:

```yaml
  write-gate:
    name: Write gate
    runs-on: ubuntu-latest

    steps:
    - name: Checkout code
      uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1

    - name: Set up Go
      uses: actions/setup-go@b7ad1dad31e06c5925ef5d2fc7ad053ef454303e # v7.0.0
      with:
        go-version: ${{ env.GO_VERSION }}
        cache: true

    - name: Install oracles
      run: sudo apt-get update && sudo apt-get install -y --no-install-recommends ffmpeg flac

    - name: Corpus round-trip
      run: go test -race -count=1 -run 'WriteRoundTrip|AudioDigest|Example' -v .

    - name: External oracle
      env:
        AUDIOMETA_REQUIRE_ORACLES: '1'
      run: go test -race -count=1 -run 'WriteOracle' -v .

    - name: Write fuzzing
      run: go test -run '^$' -fuzz '^FuzzWriteRoundTrip$' -fuzztime 120s .

    - name: Upload fuzz crashers
      if: failure()
      uses: actions/upload-artifact@cf430e030ddbb5b0abf93d22962f4752f3646cd9 # v7.0.2
      with:
        name: fuzz-crashers
        path: testdata/fuzz
```

**Verify**: the actionlint command → no output. Then run `mise exec -- go test -race -count=1
-run 'WriteRoundTrip|AudioDigest|Example' -v .` and the oracle/fuzz commands locally. All pass.

### Step 5: Part A checkpoint

**Verify**:
- `mise exec -- go test -race ./...` → all `ok`;
- `mise exec -- golangci-lint run ./...` → `0 issues.`;
- `gofmt -l .` → empty.

Commit Part A. If 040 or 041 is not DONE yet, do Step 6 only for the ready formats, then follow
the instruction at the top of this plan and stop.

### Step 6: README support matrix and write section

1. In the "Supported Formats" table, set the `Write` cell:
   - to `✓` for each format whose writer plan is DONE **and** whose rows pass Steps 2–4: MP3
     (039), FLAC (040), M4A and M4B (041);
   - to `-` for Ogg Vorbis and Opus;
   - to `🚧` for any approved format still pending.

   Replace the footnote with: `Ogg Vorbis and Opus writing is not supported yet (see
   docs/design/020-write-support-design.md).` Keep a `🚧` footnote line only while a format is
   still pending.
2. Add a `## Writing metadata` section after the format tables. It needs a code example using
   `audiometa.Write(path, func(f *audiometa.File) error { f.Tags.Title = "…"; f.Tags.Narrator =
   "…"; return f.ReplaceArtwork(cover) })` and a second example using `f.WriteTo(w)`. Then add
   these bullets, each one sentence:
   - **Safety**: the file is replaced atomically (temp file in the same directory, fsync,
     verify, rename); the audio data is never modified and is verified bit-for-bit; on error the
     original is untouched.
   - **Preservation**: unknown frames and atoms, chapters, and untouched tags are kept
     byte-for-byte.
   - **ID3**: v2.3 stays v2.3, v2.4 stays v2.4, and v2.2 or untagged files get v2.4. An existing
     ID3v1 trailer is updated, never added or removed.
   - **Editing**: change `f.Tags` fields directly or with `Tags.Set` using format-native keys.
     Editing both to different values is an `ErrConflictingEdits` error.
   - **Artwork**: `AddArtwork`, `ReplaceArtwork`, `RemoveArtwork` and `RemoveAllArtwork`. M4A
     cover art has no picture type (only front cover/other).
   - **Not supported** (returns `ErrUnsupportedWrite`):
     - Ogg Vorbis/Opus;
     - chapter editing;
     - fields a format does not store. Add a compact table: MP3 cannot write `Date`,
       `OriginalDate`, `Lyrics`, `Performers`, `Copyright`, `Label`, `CatalogNumber`, `Barcode`,
       `ISRC`, `MusicBrainz*`; M4A cannot write `OriginalDate`, `Performers`, `Label`,
       `CatalogNumber`, `Barcode`, `ISRC`, `MusicBrainz*`;
     - fragmented MP4;
     - faststart M4A files whose chunk offsets would pass 4 GiB.
   - **DRM**: encrypted or DRM files (FairPlay, CENC, AAX, ID3 encrypted frames) return
     `ErrDRMProtected`.
   - **No inference**: audiometa never derives one field from another, on read or on write.
     Writing `Narrator` leaves the composer alone, setting `Series` does not rewrite the
     grouping, and an existing ID3v1 trailer is preserved as-is. Only the tags you change are
     written.
   - **Behavior note**: `Tags.Set` on a file from `Open` also updates the matching typed field.

**Verify**:
- `grep -n '🚧' README.md` → no matches once 041 is DONE (earlier, only pending formats);
- `grep -c 'ErrUnsupportedWrite' README.md` → at least 1;
- every code block in the new section compiles. Copy it into a scratch `_test.go` example, run
  `mise exec -- go vet`, then delete the scratch file.

### Step 7: Package godoc

In `doc.go`, after the reading example, add a short "Writing metadata" paragraph with a
`Write` example of 4–6 lines and one sentence each on atomic replacement and on
`ErrUnsupportedWrite` for Ogg/Opus. Keep godoc formatting (tab-indented code).

**Verify**: `mise exec -- go doc . | head -60` shows the new paragraph; `mise exec --
golangci-lint run ./...` → `0 issues.` (`godot` requires sentences to end with periods).

### Step 8: CHANGELOG

Under `## Unreleased`:
- `### Added`:
  - "Metadata write support for MP3 (ID3v2.3/2.4), FLAC and M4A/M4B: `File.WriteTo`,
    `File.WriteToContext`, `Write`, `WriteContext`, `WithInPlace`, `WithPreserveModTime`.";
  - "Artwork editing: `File.AddArtwork`, `ReplaceArtwork`, `RemoveArtwork`,
    `RemoveAllArtwork`.";
  - "Sentinel errors `ErrUnsupportedWrite`, `ErrDRMProtected`, `ErrTagTooLarge`,
    `ErrConflictingEdits`, `ErrInvalidArtwork`, `ErrConcurrentModification`,
    `ErrVerifyFailed`.";
  - "The write API is stable; it follows the same compatibility promise as the read API.".
- `### Changed`:
  - "`Tags.Set` on tags returned by `Open` now also updates the corresponding typed field (for
    example `Set("TIT2", …)` updates `Title`). Tags built by the caller are unaffected.";
  - "M4A: multi-value ilst items (several `data` atoms) are now all read; `Artists`, `Genres`
    and `Composers` contain every value.".

Only list formats whose README cell is `✓`.

**Verify**: `grep -n 'WriteTo' CHANGELOG.md` → at least 1 match under `## Unreleased`.

### Step 9: Final gates

**Verify**:
- `mise exec -- go test -race ./...` → all `ok`;
- `AUDIOMETA_REQUIRE_ORACLES=1 mise exec -- go test -run WriteOracle -v .` → PASS with no SKIP;
- `mise exec -- go test -run '^$' -fuzz '^FuzzWriteRoundTrip$' -fuzztime 120s .` → `PASS`;
- `mise exec -- golangci-lint run ./...` → `0 issues.`;
- `gofmt -l .` → empty;
- actionlint → no output.

## Test plan

- New:
  - `write_oracle_test.go` (ffprobe/ffmpeg/flac oracles over every writable corpus row and three
    edits; skip locally when a tool is missing, required in CI);
  - `write_fuzz_test.go` (`FuzzWriteRoundTrip`, with invariants: no panic, re-open succeeds,
    audio digest unchanged, deterministic).
- Extended: `write_roundtrip_test.go` (bench fixtures with 50 chapters each).
- Patterns:
  - `internal/mp3/fuzz_test.go` (fuzz seeding style);
  - `write_roundtrip_test.go` (edit tables);
  - `cmd/audiometa/cli_test.go` (`exec`-style assertions on output).
- Command: the Step 9 list.

## Done criteria

- [ ] `.github/workflows/ci.yml` has a `write-gate` job that installs ffmpeg and flac, runs the round-trip, the oracle (with `AUDIOMETA_REQUIRE_ORACLES=1`) and 120 s of `FuzzWriteRoundTrip`; actionlint clean
- [ ] All Step 9 commands pass locally
- [ ] README Write column: MP3, FLAC, M4A, M4B `✓`; Ogg Vorbis, Opus `-`; no `🚧` left (after 041)
- [ ] `doc.go` has a write paragraph; `CHANGELOG.md` Unreleased lists the API and the two behavior changes
- [ ] No non-test `.go` file changed by this plan (`git diff --stat` on `*.go` excluding `_test.go`)
- [ ] `plans/README.md` row for 042 updated

## STOP conditions

- An oracle (ffprobe, ffmpeg or flac) rejects an edited output. That is a writer bug. Report the
  file, the edit and the stderr against plan 039, 040 or 041. Never mark a format `✓` in this
  state.
- Fuzzing finds a crasher.
- A bench fixture (`testdata/bench/book.*`) fails the round-trip on chapters. Those files have
  50 chapters, which would point to a chapter-preservation bug.
- `apt-get install ffmpeg` is not available on the runner image. Report it rather than
  downloading static binaries from an unpinned URL.
- Plan 009's README rewrite no longer has the format table or the `Write` column that Step 6
  edits. If so, ask before restructuring.

## Maintenance notes

- Each new writable format, or each format later added (Ogg/Opus), must:
  - add its corpus rows with `writable: true`;
  - pass `write-gate`;
  - only then flip its README cell.

  The gate is the contract behind every `✓`.
- The oracle compares stream and chapter counts and the title. It deliberately does not compare
  every tag, because ffprobe's tag-name normalization differs per container. Add per-format
  tag checks if a regression slips through.
- Fuzz time in CI is 120 s per run. Raise it, or add a nightly `schedule:` job with a longer
  `-fuzztime`, if the writers change often.
- Future work that will extend this gate: chapter writing, which must also compare chapter
  titles and times through ffprobe; stco → co64 upgrade; Ogg/Opus writers, which must also check
  page CRCs with `ogginfo` or ffmpeg.
- Plan 009 (README accuracy) is DONE. Any later README pass must keep the Write column and the
  "Writing metadata" section in sync with `write-gate`.
