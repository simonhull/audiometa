# Plan 020: Design metadata write support (spike — no file-mutation code on main)

> **Executor instructions**: This is a **design spike**, not a feature build. Its outputs are
> (1) a go/no-go decision memo, (2) if "go", a design document, and optionally (3) a throwaway
> prototype on a side branch that is **never merged**. Follow the steps in order. Run every
> verification command. If a STOP condition occurs, stop and report — do not improvise.
> When done, update the status row for this plan in `plans/README.md`.
>
> **Drift check (run first)**: `git diff --stat aca9750..HEAD -- README.md internal/types/tags.go file.go internal/m4a/ internal/mp3/`
> If `README.md`'s "Supported Formats" table or `Tags.Set`'s godoc changed, re-read them before
> Step 1; if any write/serialize code has appeared (see the grep in "Current state"), STOP.

## Status

- **Priority**: direction
- **Effort**: L (spike: memo S, design doc M, prototype M–L)
- **Risk**: HIGH (the eventual feature can destroy a user's only copy of a file; the spike itself is LOW because nothing lands on main except docs)
- **Depends on**: plans/001-verification-baseline.md (its `testdata/` corpus is the round-trip harness input)
- **Category**: direction
- **Planned at**: commit `aca9750`, 2026-10-30

## Why this matters

The README advertises write support as "planned" for every format, and `Tags.Set` documents
itself as "for future write support", but there is no serialization code anywhere. That is a
promise users can plan around and the library cannot keep. Write support is also the single
riskiest feature an audio-metadata library can ship: a bug corrupts the user's only copy of an
audiobook. The direction audit recommended **deferring** it — Audiobookshelf shells out to
`tone`/`ffmpeg` for writes, and an audiobook server can keep user edits in its own database and
never touch the media. So this plan's first job is to force an explicit go/no-go decision and make
the README honest either way; only on "go" does it design the feature, and it stops before any
file-mutating code reaches `main`.

## Current state

- `README.md:135-146` (verified) — every format row has `🚧` in the Write column:

  ```markdown
  | Format      | Read | Write | Artwork | Chapters | Technical Info |
  |-------------|------|-------|---------|----------|----------------|
  | FLAC        | ✓    | 🚧    | ✓       | ✓        | ✓              |
  | MP3         | ✓    | 🚧    | ✓       | ✓        | ✓              |
  | M4A         | ✓    | 🚧    | ✓       | ✓        | ✓              |
  | M4B         | ✓    | 🚧    | ✓       | ✓        | ✓              |
  | Ogg Vorbis  | ✓    | 🚧    | -       | ✓        | ✓              |
  | Opus        | ✓    | 🚧    | -       | ✓        | ✓              |

  🚧 = Planned for future release
  ```

  (The Ogg/Opus Artwork "-" is wrong too, but that cell belongs to plan 009 — leave it.)
- `internal/types/tags.go:153-174` — `Tags.Set`:

  ```go
  // Set sets a tag value (for future write support).
  //
  // If values is empty, the tag is removed.
  // Multiple values can be provided for multi-value tags.
  //
  // Note: This only modifies the in-memory representation.
  // Write support is planned for a future release.
  ```

  `Tags.Merge` (:175), `Tags.Clone` (:297) and `Tags.Equal` (:363) exist — the in-memory editing
  model a write path would need is already there. `Set` writes only to the private `raw` map
  (`tags.go:18`), not to the typed fields (`Title`, `Narrator`, …) — so even the in-memory model
  has two sources of truth that a writer would have to reconcile.
- Zero serialization paths: `grep -rn --include='*.go' -E 'io\.Writer|os\.Create|os\.Rename|WriteAt|OpenFile' . | grep -v _test.go | grep -v '^./plans/' | grep -v '^./tmp/'`
  → no output at `aca9750`. The only `os.WriteFile` uses are artwork examples (`doc.go:57`,
  `file.go:191` godoc, `examples/artwork/main.go:95`).
- The library opens files read-only: `os.Open` in `file.go:73`; parsers take `io.ReaderAt`
  (`internal/registry/registry.go:18`).
- Relevant parsers a writer would mirror: M4A atom walking `internal/m4a/atoms.go`
  (`readAtomHeader` :58-), `ilst` metadata `internal/m4a/metadata.go`, chapter `chpl` and text
  track `internal/m4a/chapters.go`; ID3v2 frame walking `internal/mp3/id3v2.go`
  (`parseID3v2Frames` :144, `readSingleFrame` :166, synchsafe `decodeSynchsafe` :226).
- Consumer context the memo must weigh: the maintainer's audiobook server **ListenUp** now
  has its own Kotlin port of the metadata reader (`~/Code/listenup/ListenUp/server/src/commonMain/kotlin/com/calypsan/listenup/server/embeddedmeta/`;
  its `format/mp4/IlstReader.kt` header says it ports `internal/m4a/metadata.go`) and records
  chapter provenance and user edits in its own data model (`ChapterSource.USER` in
  `contract/.../api/sync/BookSyncPayload.kt:266-270`). Verify this before relying on it — it
  suggests ListenUp may no longer import this Go module at all, which weakens the "primary
  consumer needs writes" argument.

## Commands you will need

| Purpose | Command | Expected on success |
|---|---|---|
| Build | `go build ./...` | exit 0 |
| Tests | `go test -race ./...` | all `ok` |
| Lint | `mise exec -- golangci-lint run ./...` | `0 issues.` (must use `mise exec --`) |
| Format | `gofmt -l .` | no output |
| Main untouched | `git diff --stat main -- '*.go'` (on your docs branch) | no output |

## Scope

**In scope (main / docs branch `advisor/020-write-support-spike`)**:
- `plans/design/020-write-support-decision.md` (create) — the go/no-go memo
- `plans/design/020-write-support-design.md` (create, only on "go")
- `README.md` — **only** the Write column / its legend line (`README.md:137-146`)
- `internal/types/tags.go` — **only** the `Tags.Set` godoc sentence about write support (comment-only)

**In scope (throwaway branch `spike/020-write-prototype`, never merged)**:
- anything under `internal/write/` or `cmd/spike-write/` you create for the prototype

**Out of scope**:
- Any non-comment `.go` change on the docs branch.
- README edits outside the Write column — plan 009 owns the rest of the README (including the
  Ogg/Opus Artwork cells). If plan 009 is IN PROGRESS, hand your README change to it instead.
- Writing any format other than the one chosen in Step 3.

## Git workflow

- Docs branch: `advisor/020-write-support-spike`. Prototype branch: `spike/020-write-prototype`
  (branched from the docs branch; delete it locally after reporting unless the maintainer asks to keep it).
- gitmoji single-line commits, no body, no attribution, e.g. `📝 Add write-support go/no-go memo`,
  `📝 Mark write support as not planned in README`, `🧪 Prototype M4B ilst rewrite (spike)`.
- Do NOT push or open a PR.

## Steps

### Step 1: Write the go/no-go decision memo

Create `plans/design/020-write-support-decision.md` with exactly these sections:

1. `## Question` — "Should audiometa add metadata write support before 1.0?"
2. `## Who would use it` — list concrete consumers. Check whether ListenUp imports this module:
   `grep -rl 'simonhull/audiometa' ~/Code/listenup --include=go.mod` (empty output = it does not)
   and record the result verbatim.
3. `## Arguments for` — at least 3 (e.g. pure-Go no-cgo tagging; fixing bad tags at ingest; parity with TagLib/Mutagen).
4. `## Arguments against` — at least 4, including: data-loss blast radius on single-copy audiobooks;
   ABS delegates writes to `tone`/`ffmpeg`; a server can store edits in its DB; the read side still
   has open correctness plans (002, 004, 012, 013, 019) that should land first; the
   `Tags.raw` vs typed-field dual source of truth.
5. `## Alternatives` — at least: (a) defer + README honest; (b) "export only" — serialize tags to
   JSON/sidecar (`metadata.json`/OPF) without touching media; (c) document `tone`/`ffmpeg` recipes.
6. `## Recommendation` — one of `GO`, `NO-GO (defer)`, `NO-GO (never)`, with a one-paragraph rationale.
   The advisor's prior recommendation is **NO-GO (defer)**; you may disagree, but say why.
7. `## Decision` — leave a line `Maintainer decision: ____` for the maintainer to fill.

**Verify**: `grep -c '^## ' plans/design/020-write-support-decision.md` → `7`;
`grep -n 'Maintainer decision:' plans/design/020-write-support-decision.md` → one match.

### Step 2: Make the README honest (both outcomes)

Edit only `README.md:137-146`:
- If the memo recommends NO-GO: drop the Write column from the table and replace the legend line
  `🚧 = Planned for future release` with `audiometa is read-only; it never modifies your files.`
- If GO: keep the column but change the legend to `🚧 = Under design (see plans/design/020-write-support-design.md); not available yet.`

Edit the `Tags.Set` godoc (`internal/types/tags.go:153-159`): replace `(for future write support)`
with `(in-memory only)` and delete the line `// Write support is planned for a future release.`
(keep `// Note: This only modifies the in-memory representation.`). Comment-only change.

**Verify**: `grep -n '🚧' README.md` → matches only if GO; `go build ./... && gofmt -l .` →
exit 0 / no output; `git diff --stat -- '*.go'` lists only `internal/types/tags.go` with a
comment-only diff (`git diff internal/types/tags.go | grep '^[+-][^+-]' | grep -v '^\s*[+-]\s*//'` → no output).

**Commit** steps 1–2, then **STOP and report** with the memo path. Continue to Step 3 only
after the maintainer fills `Maintainer decision: GO`. If NO-GO, the plan is DONE here — mark
the README row DONE with "NO-GO, README made honest".

### Step 3 (GO only): Choose the first format

In `plans/design/020-write-support-design.md`, section `## First format`, compare **M4B (ilst)**
vs **MP3 (ID3v2)** on: share of the audiobook corpus; structural risk; existing read-side
helpers. Facts to include:
- M4B: tags live in `moov/udta/meta/ilst`. Growing `ilst` grows `moov`; if `moov` precedes
  `mdat` ("faststart"), every `stco`/`co64` chunk offset in every `trak` must be shifted by the
  delta, or the audio becomes unplayable. A `free` atom adjacent to `ilst` can absorb growth
  without moving `mdat`. Chapter tracks (`chapters.go`) also store chunk offsets.
- MP3: ID3v2 sits at file start with optional padding; a tag that fits in existing padding can
  be rewritten in place; otherwise the whole file must be rewritten. v2.3 vs v2.4 size encoding
  differs (plain vs synchsafe — `decodeFrameSize`, `internal/mp3/id3v2.go:204`).
Pick one and justify.

### Step 4 (GO only): API shape options

Section `## API`. Evaluate at least these three, with a code sketch of each and a pick:
- `func (f *File) Save() error` — mutates the opened path (needs the file reopened read-write; today `File` holds a read-only `*os.File`).
- `func (f *File) WriteTo(w io.Writer) (int64, error)` — produce a complete new file stream; caller decides where it goes (safest; composes with temp-file+rename).
- `func Write(path string, tags *Tags, opts ...WriteOption) error` — stateless.
State which `Tags` fields are writable in v1, and how `Tags.Set` raw keys vs typed fields are reconciled (one source of truth must win — say which).

### Step 5 (GO only): Rewrite strategy and safety

Sections `## In-place vs full rewrite`, `## Atomicity`, `## Failure modes`:
- In-place only when the new tag fits in existing padding/`free`; otherwise full rewrite.
- Always write to a temp file in the **same directory**, `fsync`, then `os.Rename` over the
  original; preserve mode bits; never truncate the original before the rename.
- Enumerate at least 6 failure modes (disk full mid-write, crash before rename, cross-device
  temp, concurrent reader, unknown atoms/frames that must be preserved byte-for-byte,
  DRM/encrypted atoms) and the mitigation for each.

### Step 6 (GO only): Round-trip harness design

Section `## Verification`. Design (not implement on main) a harness over plan 001's `testdata/`
corpus: for each file → read tags → write unchanged → read again → `Tags.Equal` true; audio
payload bytes (M4B `mdat` / MP3 frames after the tag) byte-identical (hash before/after);
duration and chapters unchanged; and an external oracle check (`ffprobe -v error -show_format`
exit 0 on the output). Include a fuzz idea: random tag edits must never change the audio hash.

### Step 7 (GO only, optional): Throwaway prototype

On `spike/020-write-prototype` only, prototype the chosen format's simplest case (e.g. M4B: replace
`©nam` value when the new `ilst` fits in an adjacent `free` atom) plus the round-trip check on one
corpus file. Record findings in the design doc's `## Prototype findings` section **on the docs branch**.

**Verify (on the docs branch)**: `git diff --stat main -- '*.go' | grep -v 'internal/types/tags.go'` → no output.

### Step 8 (GO only): Open questions

Section `## Open questions` with at least 8 items (e.g. ID3v2.3 vs 2.4 output; preserving unknown
frames; writing chapters; artwork writes and size caps; Vorbis/FLAC padding block reuse; Opus
header CRCs and Ogg page re-pagination; how `WithStrictParsing` interacts with write preconditions;
version/compat promise).

**Verify**: `grep -c '^## ' plans/design/020-write-support-design.md` → ≥ 8;
`awk '/^## Open questions/{f=1;next}/^## /{f=0}f && /^- /' plans/design/020-write-support-design.md | wc -l` → ≥ 8.

## Test plan

- No production tests on main (no production code changes).
- Docs-branch gates: `go build ./...`, `go test -race ./...`, `mise exec -- golangci-lint run ./...`,
  `gofmt -l .` all clean (they prove the comment-only edit broke nothing).
- Prototype (side branch only): one round-trip test on one corpus file, as Step 7 describes.

## Done criteria

- [ ] `plans/design/020-write-support-decision.md` exists with 7 `##` sections and a `Maintainer decision:` line
- [ ] README Write column and legend are honest (Step 2); `Tags.Set` godoc no longer promises writes
- [ ] On the docs branch, `git diff --stat main -- '*.go'` lists at most `internal/types/tags.go`, comment-only
- [ ] If GO: design doc exists with sections First format, API, In-place vs full rewrite, Atomicity, Failure modes, Verification, Open questions (≥ 8 items)
- [ ] No branch containing file-mutation code has been merged into `main`
- [ ] `plans/README.md` row for 020 updated

## STOP conditions

- After Step 2, always — wait for the maintainer's decision.
- Any step would require a non-comment `.go` change on the docs branch.
- Plan 009 is IN PROGRESS and also editing `README.md:135-146` — hand the change over, don't race it.
- Plan 001's corpus doesn't exist when you reach Step 6/7 — design the harness, skip the prototype, report.
- You find that a write path already exists (the "zero serialization" grep returns hits).

## Maintenance notes

- If NO-GO, re-evaluate only when (a) a real consumer asks and (b) plans 001, 002, 004, 019 are DONE.
- If GO, the implementation plan(s) derived from the design doc must include: temp-file+rename,
  byte-preservation of unknown atoms/frames, and the round-trip harness as a CI gate before any
  release mentions writes.
- Reviewer: the only `.go` diff on main should be a godoc sentence.
