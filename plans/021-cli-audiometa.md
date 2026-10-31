# Plan 021: Build the cmd/audiometa CLI the README advertises

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving on. If a
> STOP condition occurs, stop and report — do not improvise. When done, update
> the status row for this plan in `plans/README.md`.
>
> **Drift check (run first)**: `git diff --stat aca9750..HEAD -- cmd/ README.md internal/types/chapter.go internal/types/tags.go internal/types/audio.go internal/types/errors.go internal/types/format.go version.go`
> If `cmd/audiometa/` already exists, STOP. If `internal/types/chapter.go` gained a `Source`
> field (plan 036), include it in the DTO (Step 2 says how). Other drift: re-check the excerpts.

## Status

- **Priority**: direction
- **Effort**: M
- **Risk**: LOW (new `package main`; no library behavior change)
- **Depends on**: none (benefits from plan 001's `testdata/` corpus and plan 036's `Chapter.Source` if they landed)
- **Category**: direction
- **Planned at**: commit `aca9750`, 2026-10-30

## Why this matters

The README tells users to `go install github.com/simonhull/audiometa/cmd/audiometa@latest` and
run `audiometa song.flac` — that package does not exist, so the very first command a CLI user
tries fails. A small, stable-JSON CLI is also the cheapest way to dogfood the public API, to
triage "wrong chapters/tags" reports from users ("send me `audiometa inspect --json`"), and to
give scripts (and plan 022's validator) a machine-readable surface. The existing
`cmd/atom-dump-tool` is M4A-only, has an infinite-loop bug on hostile input, and duplicates
atom-walking logic; folding it in as `audiometa atoms` removes a second, buggy binary.

## Current state

- `README.md:192-210` (verified):

  ````markdown
  ## Command-Line Tools

  ### audiometa

  Display metadata for audio files:

  ```bash
  go install github.com/simonhull/audiometa/cmd/audiometa@latest
  audiometa song.flac
  ```

  ### atom-dump-tool

  Debug M4A/M4B atom structure:
  ...
  go install github.com/simonhull/audiometa/cmd/atom-dump-tool@latest
  ````

  and `README.md:296-301`: `go install ./cmd/audiometa`, `go install ./cmd/atom-dump-tool`,
  `go build -v ./cmd/...`. `.gitignore` already ignores a root `/audiometa` binary.
- `git ls-files cmd` → only `cmd/atom-dump-tool/atom-dump.go`. `cmd/chapdbg/` exists on disk as
  an **empty, untracked** directory (an abandoned chapter-debug tool; git does not track empty
  dirs).
- `cmd/atom-dump-tool/atom-dump.go` (95 lines) uses `encoding/binary` directly, not the library.
  Bugs (verified):

  ```go
  // atom-dump.go:46-57
  size := binary.BigEndian.Uint32(header[0:4])
  ...
  atomSize := uint64(size)
  headerSize := int64(8)
  if size == 1 {
  	extSize := make([]byte, 8)
  	r.ReadAt(extSize, offset+8)            // error ignored
  	atomSize = binary.BigEndian.Uint64(extSize)
  	headerSize = 16
  }
  ...
  // atom-dump.go:70-78
  dataEnd := offset + int64(atomSize)
  dumpAtoms(r, dataOffset, dataEnd, depth+1)   // recursion depth unbounded
  offset += int64(atomSize)                    // atomSize > MaxInt64 → negative → offset goes backwards → infinite loop
  if atomSize == 0 { break }                   // size 0 means "extends to EOF" in ISO-BMFF; treated as stop
  ```

  Also: `stat, _ := f.Stat()` ignores errors (:32); a size in 2..7 (smaller than the header) is
  not rejected; child `dataEnd` is not clamped to the parent's end. (Plan 023 tracks the same
  negative-size cycle class in the library's own atom walker.)
- JSON-readiness of library types (why we need DTOs):
  - Only `internal/types/chapter.go:24-27` has json tags (`title`, `index`, `start_time`,
    `end_time`; the times are `time.Duration` → JSON integers in **nanoseconds**).
  - `Tags.raw` (`internal/types/tags.go:18`) is unexported, so `json.Marshal(file.Tags)` silently
    drops all raw tags; the public way to read them is `Tags.All()` (`tags.go:69`, `iter.Seq2[string, []string]`).
  - `Format` is an `int` enum (`internal/types/format.go:12`) with a stringer `String()`
    (`format_string.go`, `-linecomment`: e.g. `FormatOgg.String() == "Ogg Vorbis"`) but no
    `MarshalText` — `json.Marshal` emits a bare number.
  - `Warning.Err` is an `error` (`internal/types/errors.go:72`) → marshals as `{}`.
  - `AudioInfo.Duration` is `time.Duration` (ns).
- Public API the CLI should use (root package `audiometa`): `Open(path, opts...)`,
  `(*File).Close`, `(*File).ExtractArtwork()`, fields `Path, Format, Size, Tags, Audio, Chapters,
  Warnings` (promoted from embedded `types.File`, `internal/types/file.go:16-24`),
  `Version` / `GetVersionInfo()` (`version.go:6`, `:35`), `Artwork{MIMEType, Description, Data, Type, Width, Height}`.
- The artwork example derives file extensions with a fallback `"." + strings.TrimPrefix(mimeType, "image/")`
  (`examples/artwork/main.go:133-136`) — a crafted MIME like `image/../../x` would become a path
  component. The CLI must not copy that fallback.
- Lint config `.golangci.yml` enables `gosec`, `errcheck`, `revive`, `godot`, `perfsprint`,
  `gocyclo`, `nestif`, among others — new code must pass them.

**Decision taken in this plan — DTOs, not json tags on library types.** Rationale: (1) the CLI's
JSON is a *contract* for scripts and must not change when an internal field is renamed or added;
(2) library types have shapes that serialize badly (`Format` int, `Duration` ns, `error` field,
unexported raw map) and fixing that with `MarshalJSON` on public types is a public-API change
better made deliberately later; (3) DTOs keep the change inside `cmd/` (zero library risk).
Revisit if a second consumer needs the same JSON.

## Commands you will need

| Purpose | Command | Expected on success |
|---|---|---|
| Build | `go build ./...` | exit 0 |
| Vet | `go vet ./...` | exit 0 |
| CLI tests | `go test -race ./cmd/audiometa/` | `ok` |
| All tests | `go test -race ./...` | all `ok` |
| Lint | `mise exec -- golangci-lint run ./...` | `0 issues.` (must use `mise exec --`) |
| Format | `gofmt -l .` | no output |
| Smoke | `go run ./cmd/audiometa version` | prints a version line, exit 0 |

## Scope

**In scope**:
- `cmd/audiometa/` (create): `main.go`, `run.go`, `inspect.go`, `chapters.go`, `artwork.go`,
  `atoms.go`, `dto.go`, `*_test.go`
- `cmd/atom-dump-tool/` — delete (Step 6) once `audiometa atoms` replaces it
- `cmd/chapdbg/` — remove the empty directory (`rmdir cmd/chapdbg`; untracked, no commit effect)
- `README.md` — only the "Command-Line Tools" section (:192-210) and the two `go install ./cmd/...` lines (:296-298)

**Out of scope**:
- Any change to library packages (root `*.go`, `internal/**`). If the CLI needs something the
  public API lacks, note it in your report — do not add it.
- `json` tags or `MarshalJSON` on library types.
- Third-party CLI frameworks (cobra, urfave) — stdlib `flag` only; `go.mod` must not change.
- Validation/QA output (plan 022 designs `Validate()`; the CLI can grow a `validate` subcommand after).
- The rest of `README.md` (plan 009 owns it).

## Git workflow

- Branch: `advisor/021-cli-audiometa`
- gitmoji single-line commits, no body, no attribution, e.g. `✨ Add audiometa CLI with inspect and version`,
  `✨ Add chapters and artwork subcommands`, `✨ Add atoms subcommand with bounded walker`,
  `🔥 Remove atom-dump-tool superseded by audiometa atoms`, `📝 Document audiometa CLI`.
- Do NOT push or open a PR.

## Steps

### Step 1: Skeleton with testable entry point and `version`

`cmd/audiometa/main.go`:

```go
// Command audiometa prints audio file metadata.
package main

import "os"

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }
```

`cmd/audiometa/run.go`: `func run(args []string, stdout, stderr io.Writer) int` dispatches on
`args[0]`: `inspect`, `chapters`, `artwork`, `atoms`, `version`, `help`/`-h`/`--help`. **If
`args[0]` is not a known subcommand and does not start with `-`, treat the whole arg list as
`inspect`** (so the README's `audiometa song.flac` works). Each subcommand builds its own
`flag.NewFlagSet(name, flag.ContinueOnError)` with `SetOutput(stderr)`.

Exit codes (document them in the usage text): `0` success; `1` at least one file failed to
open/parse or a write failed; `2` usage error (unknown flag, no files).

`version` prints `audiometa <Version> (commit <GitCommit>, built <BuildTime>, <GoVersion>)` from
`audiometa.GetVersionInfo()`; with `--json` prints `{"version":…,"git_commit":…,"build_time":…,"go_version":…}`.

**Verify**: `go run ./cmd/audiometa version` → one line starting `audiometa `; `go run ./cmd/audiometa`
→ usage on stderr, exit code 2 (`echo $?`).

### Step 2: Define the JSON contract (`dto.go`)

Create explicit DTO structs with snake_case json tags. Times are **integer milliseconds**
(`int64`, `d.Milliseconds()`), named `*_ms`. Top-level object carries `"schema_version": 1`.

```go
type fileDTO struct {
	SchemaVersion int          `json:"schema_version"`
	Path          string       `json:"path"`
	Error         string       `json:"error,omitempty"`  // set when Open failed; other fields zero
	Format        string       `json:"format,omitempty"` // Format.String(), e.g. "M4B", "Ogg Vorbis"
	Size          int64        `json:"size,omitempty"`
	Tags          *tagsDTO     `json:"tags,omitempty"`
	Audio         *audioDTO    `json:"audio,omitempty"`
	Chapters      []chapterDTO `json:"chapters"`         // [] not null when none
	Warnings      []warningDTO `json:"warnings"`         // [] not null when none
}
type chapterDTO struct {
	Index   int    `json:"index"`
	Title   string `json:"title"`
	StartMs int64  `json:"start_ms"`
	EndMs   int64  `json:"end_ms"`
	// Source string `json:"source,omitempty"` — add ONLY if internal/types/chapter.go has a Source field (plan 036)
}
type warningDTO struct {
	Stage   string `json:"stage"`
	Message string `json:"message"`
	Offset  int64  `json:"offset,omitempty"`
	Error   string `json:"error,omitempty"` // Warning.Err.Error() when non-nil
}
```

`tagsDTO`: one field per exported `Tags` field (read the struct at `internal/types/tags.go:17-53`;
string fields `omitempty`, slices `omitempty`, ints `omitempty`), plus
`Raw map[string][]string \`json:"raw,omitempty"\`` filled from `Tags.All()`.
`audioDTO`: `codec, codec_description, codec_profile, container, duration_ms, sample_rate,
bit_depth, channels, bitrate, lossless, vbr` and `replay_gain` (pointer, omitted when nil) with
`track_gain_db, track_peak, album_gain_db, album_peak`.

Write `func toFileDTO(f *audiometa.File) fileDTO` and a unit test that marshals a hand-built
`audiometa.File` and compares against a golden JSON string (sorted map keys are guaranteed by
`encoding/json`).

**Verify**: `go test ./cmd/audiometa/ -run DTO` → ok; the golden test asserts `"chapters":[]` for a file with no chapters.

### Step 3: `inspect [--json] FILE...`

For each path: `audiometa.Open(path)`; on error, record `fileDTO{Path, Error: err.Error()}` and
continue (exit code 1 at the end). Always `Close()` opened files.
- `--json`: emit **one JSON object per line (NDJSON)** per file, in argument order. Rationale:
  streams over thousands of files with constant memory, and `jq` consumes it directly; document it.
- Human output (default): a stable, aligned block per file — `Path`, `Format`, `Audio` (use
  `file.Audio.String()`), `Duration`, non-empty typed tags as `Key: value`, chapter count, then
  warnings as `warning: <Warning.String()>`. Errors go to **stderr** as `<path>: <err>`.

**Verify**: `go test -race ./cmd/audiometa/ -run Inspect` → ok (see Test plan for cases).

### Step 4: `chapters [--json] FILE...`

Human: `[index] HH:MM:SS.mmm → HH:MM:SS.mmm  Title` per chapter. JSON: NDJSON of
`{"schema_version":1,"path":…,"chapters":[…]}` (reuse `chapterDTO`).

### Step 5: `artwork --out DIR FILE...`

`--out` is required (exit 2 without it); create DIR with `os.MkdirAll(dir, 0o750)`. For each file,
`ExtractArtwork()`; write each image to `DIR/<basename-without-ext>[_<n>].<ext>` with
`os.WriteFile(…, 0o600)`. Extension comes from an **allowlist only**: `image/jpeg`,`image/jpg`→`.jpg`,
`image/png`→`.png`, `image/gif`→`.gif`, `image/bmp`→`.bmp`, `image/webp`→`.webp`, `image/tiff`→`.tiff`,
anything else → `.bin`. Never derive a path component from file contents. Refuse to overwrite an
existing file unless `--force`. Print one line per written file (`wrote <path> (<Artwork.String()>)`).

**Verify**: test with a MIME of `image/../../evil` → file written as `….bin` inside DIR; nothing outside DIR.

### Step 6: `atoms FILE...` (absorb and fix atom-dump-tool)

Implement in `cmd/audiometa/atoms.go` a bounded walker using `internal/binary.SafeReader`
(importable: `cmd/audiometa` is inside the module) — **not** raw `encoding/binary` + unchecked
`ReadAt`. Rules:
- Read header via `binary.Read[uint32]` (size) + 4-byte type; `size == 1` → 64-bit extended size
  via `binary.Read[uint64]`, header 16; `size == 0` → atom extends to the parent's end.
- Reject (print `<indent>! invalid atom size N at offset O` and stop walking **this level**) when
  `size < headerSize`, or `size > uint64(parentEnd - offset)` (this also rules out the
  >MaxInt64 negative-offset loop), or the header read fails.
- Clamp children to the parent range; cap depth at 32 (print `! max depth` and stop descending).
- Containers: the same set as the old tool (`moov trak mdia minf stbl udta meta ilst edts`); `meta`
  skips 4 version/flag bytes.
- Output format identical to the old tool: `<2-space indent per depth><type> (size: N, offset: O)`.
- Non-MP4 input: check the format with `audiometa.DetectFormat`; if not M4A/M4B print
  `<path>: not an MP4 container` to stderr, exit 1.

Then delete `cmd/atom-dump-tool/` (`git rm -r cmd/atom-dump-tool`) and `rmdir cmd/chapdbg`.

**Verify**: tests for (a) a valid nested `moov/udta/meta/ilst` tree prints the expected lines;
(b) an atom with extended size `0xFFFFFFFFFFFFFFFF` terminates with an `invalid atom size` line;
(c) 40 nested `moov` containers stop at depth 32; (d) size 4 (< header) is rejected.
`go test -timeout 30s -run Atoms ./cmd/audiometa/` → ok (a hang = the loop bug is back).

### Step 7: README

Rewrite only `README.md:192-210` to document `audiometa` subcommands (one example each, NDJSON
note, exit codes) and remove the `atom-dump-tool` section; change `README.md:297-298` to the single
line `go install ./cmd/audiometa`. If plan 009 is DONE and removed the `### audiometa` section,
re-add it in this form. If plan 009 is IN PROGRESS, hand this text to it instead of editing.

**Verify**: `grep -n 'atom-dump-tool' README.md` → no output; `grep -n 'cmd/audiometa' README.md` → ≥ 2 matches.

### Step 8: Full gate

**Verify**: `go build ./... && go vet ./... && go test -race -timeout 120s ./... && mise exec -- golangci-lint run ./... && gofmt -l .`
→ all clean; `git diff --stat aca9750 -- go.mod go.sum` → no output.

## Test plan

All in `cmd/audiometa/*_test.go`, calling `run(args, &stdout, &stderr)` and asserting exit code +
output (no subprocesses). Fixtures: if plan 001's `testdata/` exists, use its files (one per
format); otherwise write a minimal MP3 to `t.TempDir()` using the byte layout of
`createMinimalMP3WithID3` in `internal/mp3/parser_test.go:99-135` (ID3v2.3 header + `TIT2 "Test Title"` + one MPEG frame header) — copy the bytes, do not import test code.
Cases:
- `version` and `version --json` (valid JSON, has `version` key).
- bare `audiometa <file>` behaves as `inspect`.
- `inspect --json` on one good + one missing file → 2 NDJSON lines, second has `"error"`, exit 1.
- `inspect --json` output for the MP3 fixture has `"format":"MP3"`, `"tags":{"title":"Test Title"…}`, `"chapters":[]`, `"warnings":[…]` (array, maybe empty).
- `chapters` on a file without chapters → no lines / `"chapters":[]`.
- `artwork` without `--out` → exit 2; MIME allowlist test; no-overwrite without `--force`.
- `atoms` cases (a)–(d) from Step 6, plus non-MP4 input → exit 1.
- Unknown flag → exit 2, usage on stderr.

## Done criteria

- [ ] `go install ./cmd/audiometa` succeeds; `audiometa version` exits 0
- [ ] `go test -race -timeout 120s ./...` exit 0, including ≥ 12 new tests in `cmd/audiometa`
- [ ] `mise exec -- golangci-lint run ./...` → `0 issues.`; `gofmt -l .` empty; `go vet ./...` exit 0
- [ ] `git ls-files cmd` → only files under `cmd/audiometa/`; `test -d cmd/chapdbg` → false
- [ ] No changes outside `cmd/` and `README.md` (`git diff --stat aca9750 -- . ':!cmd' ':!README.md' ':!plans'` → empty)
- [ ] `go.mod`/`go.sum` unchanged
- [ ] `plans/README.md` row for 021 updated

## STOP conditions

- `cmd/audiometa/` already exists at start.
- The CLI needs a library change (e.g. a field the public API doesn't expose) — report it; don't add it.
- `internal/binary` cannot be imported from `cmd/audiometa` (it should — same module).
- golangci-lint flags something that can only be fixed with a `//nolint` you can't justify in one line.
- Plan 009 is IN PROGRESS on the same README lines.

## Maintenance notes

- The JSON contract is versioned by `schema_version`. Adding fields is compatible; renaming or
  changing units requires bumping it. Reviewer: check every time field is `_ms` integer milliseconds.
- When plan 036 lands, add `source` to `chapterDTO` (omitempty) — compatible change, no bump.
- When plan 022's `Validate()` lands, add `audiometa validate [--json]`, reusing its Issue type's JSON shape.
- `atoms` deliberately reimplements a small walker; if `internal/m4a` ever exports a safe walker
  (plan 023 hardens the library's), switch to it.
- Removing `cmd/atom-dump-tool` breaks `go install …/cmd/atom-dump-tool@latest` for anyone using
  it — mention it in release notes.
