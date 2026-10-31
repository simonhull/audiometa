# Plan 033: Make local lint match CI and tighten the lint config

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving on. If a
> STOP condition occurs, stop and report — do not improvise. When done, update
> the status row for this plan in `plans/README.md`.
>
> **Drift check (run first)**: `git diff --stat aca9750..HEAD -- .golangci.yml mise.toml internal/mp3/technical.go internal/ogg/container.go internal/ogg/opus.go internal/flac/cuesheet.go internal/vorbis/chapters.go examples/artwork/main.go audiometa_test.go format_test.go internal/flac/parser_test.go internal/ogg/parser_test.go`
> If any changed, compare against the excerpts below; on a mismatch, STOP. If plan 018 has
> landed, `.golangci.yml` will have a `formatters:` block and `mise.toml` extra tool pins —
> that is expected; keep them.

## Status

- **Priority**: P3
- **Effort**: S
- **Risk**: LOW (lint config, one-line local tasks, mechanical `go fix` rewrites, 5 annotated
  integer conversions)
- **Depends on**: none (coordinates with plan 018 — see Scope)
- **Category**: dx
- **Planned at**: commit `aca9750`, 2026-10-30

## Why this matters

This library parses untrusted binary input, yet the single most relevant gosec check — G115,
integer-overflow on conversion — is switched off repo-wide, so new unchecked `uint64 → int64`
style conversions merge silently. The lint config also carries dead entries (a G304 exclusion
already covered by `gosec.excludes`, exclusions for linters that aren't enabled, a path regex
that can never match) that make it harder to read and trust. And there is no one-command way to
run "what CI runs" locally: on this machine a stale global golangci-lint 2.11.4 sits first on
PATH and reports false typecheck errors, while CI and `mise.toml` use 2.15.0. This plan
re-enables G115 for everything except the M4A package (whose 51 hits get their own follow-up),
removes the dead config, applies the 11 pending `go fix` modernizations and turns on the
`modernize` linter so they stay applied, and adds `mise run` tasks that mirror CI.

## Current state

**Tool pins** — `mise.toml` (whole file):

```toml
[tools]
go = "1.27.3"
golangci-lint = "2.15.0"
```

No `[tasks]`. `mise tasks ls` → empty. `which -a golangci-lint` on this machine lists
`~/.local/share/mise/installs/golangci-lint/2.11.4/...` **before** the mise shim, so a bare
`golangci-lint` runs 2.11.4. CI (`.github/workflows/ci.yml`) runs `go test -v -race
-coverprofile=coverage.txt -covermode=atomic ./...`, golangci-lint-action `version: v2.15.0`,
`go build -v ./...` and `go build -race -v ./...`.

**`.golangci.yml`** (verified line numbers):

- `:72-77` gosec settings:
  ```yaml
      gosec:
        excludes:
          - G104   # Duplicate of errcheck
          - G304   # File path from variable - this library opens user-provided paths by design
          - G307   # Deferred file close - false positives on best-effort cleanup
          - G115   # Integer overflow conversion
  ```
- `:78-82` govet `enable-all: true` with `disable: [shadow, fieldalignment]`.
- `:114-119`:
  ```yaml
    exclusions:
      generated: lax
      paths:
        - third_party$
        - builtin$
        - examples$
  ```
  `paths` regexes are matched against file paths (e.g. `examples/basic/main.go`), so
  `examples$` never matches a file; and the config separately has examples-specific rules
  (`:171-182`), showing examples are meant to be linted. Dead entry. (`third_party$`/`builtin$`
  are golangci's stock template defaults — leave them.)
- `:121-137` `_test\.go` exclusion rule lists `unparam` (line 137) — `unparam` is not in
  `linters.enable` (`:17-54`). Dead entry.
- `:142-143`:
  ```yaml
      - path: _test\.go
        text: 'shadow: declaration of "err"'
  ```
  `shadow` is disabled (`:81`). Dead entry.
- `:156-158`:
  ```yaml
      # Library opens user-provided file paths by design
      - linters: [gosec]
        text: "G304"
  ```
  Redundant with `G304` in `gosec.excludes` (`:75`). Dead entry.
- gosec is already excluded for `_test\.go` (`:122-129`) and `cmd/` (`:146-152`).
- `modernize` is not enabled.

**G115 hits if re-enabled** (measured with golangci-lint 2.15.0, gosec G115 only): 89 total;
excluding `_test.go` (already exempt from gosec) and `cmd/` (already exempt): **56**, of which
**51 are in `internal/m4a/`** (chapters.go 20, artwork.go 8, audiobook.go 7, technical.go 7,
metadata.go 5, parser.go 3, atoms.go 1) and **5** elsewhere:

```
internal/mp3/technical.go:95:24:  if bitrateIdx < uint32(len(bitrateTable)) {          int -> uint32
internal/mp3/technical.go:101:27: if sampleRateIdx < uint32(len(sampleRateTable)) {    int -> uint32
internal/ogg/container.go:92:25:  GranulePosition: int64(granule),                     uint64 -> int64
internal/ogg/container.go:178:14: return int64(granule), nil                           uint64 -> int64
internal/ogg/opus.go:45:21:       outputGain := int16(binary.LittleEndian.Uint16(data[16:18]))  uint16 -> int16
```

The M4A hits overlap the arithmetic that plans 002 (allocation caps) and 014 (mvhd overflow)
rewrite, so they are deliberately left for a follow-up after those land.

**`go fix -diff ./...`** (Go 1.27.3, read-only) reports 11 hunks in 8 files — exactly the set
the `modernize` linter reports:
- `internal/flac/cuesheet.go:87,158` — `for i := byte(0); i < trackCount; i++` → `for i := range trackCount` (rangeint)
- `internal/ogg/container.go:149` — `if searchStart < 0 { searchStart = 0 }` → `max(fileSize-65536, 0)` (minmax)
- `internal/vorbis/chapters.go:42` — `strings.IndexByte` + slicing → `strings.Cut` (stringscut)
- `examples/artwork/main.go:134` — `HasPrefix`+`TrimPrefix` → `strings.CutPrefix` (stringscutprefix)
- tests: `audiometa_test.go:89`, `format_test.go:89,103,118`, `internal/flac/parser_test.go:233`,
  `internal/ogg/parser_test.go:365` — `errors.As(err, &v)` → `errors.AsType[T](err)` (errorsastype;
  `errors.AsType` is Go 1.26, within the `go 1.26.0` floor)

**unparam** (if it were enabled) reports 6 hits, 4 in production code
(`internal/m4a/audiobook.go:12,67`, `internal/m4a/codecs.go:52`, `internal/m4a/technical.go:12`
— error results that are always nil). Not enabled by this plan (signature changes in files other
plans are rewriting) — recorded as follow-up.

Conventions: `nolintlint` is enabled; every `//nolint` must name the linter and carry a reason,
e.g. existing `//nolint:gocyclo // Block processing requires multiple conditional branches`
(`internal/flac/parser.go:34`).

## Commands you will need

| Purpose | Command | Expected on success |
|---|---|---|
| Install pinned tools | `mise install` | exit 0 |
| Lint | `mise exec -- golangci-lint run ./...` | `0 issues.` |
| Lint config check | `mise exec -- golangci-lint config verify` | exit 0 |
| Modernize preview | `go fix -diff ./...` | lists hunks (empty after Step 3) |
| Build | `go build ./...` | exit 0 |
| Vet | `go vet ./...` | exit 0 |
| Tests | `go test -race ./...` | all pass |
| Format | `gofmt -l .` | no output |
| Tasks | `mise tasks ls` | lists the tasks added in Step 4 |

> Always run golangci-lint via `mise exec --` (or a `mise run` task). A bare `golangci-lint`
> may resolve to a stale 2.11.4 and report false typecheck errors.

## Scope

**In scope**:
- `.golangci.yml` — gosec/G115 scoping, dead-entry removal, enable `modernize`
- `mise.toml` — add `[tasks]` only (leave `[tools]` as is)
- `internal/mp3/technical.go`, `internal/ogg/container.go`, `internal/ogg/opus.go` — the 5 G115
  sites only
- Files rewritten by `go fix ./...`: `internal/flac/cuesheet.go`, `internal/ogg/container.go`,
  `internal/vorbis/chapters.go`, `examples/artwork/main.go`, `audiometa_test.go`,
  `format_test.go`, `internal/flac/parser_test.go`, `internal/ogg/parser_test.go`
- `AGENTS.md` (create — short toolchain notes; no existing AGENTS.md/CLAUDE.md in the repo, and
  plans 009/018 do not create one)

**Out of scope** (owned elsewhere — do not touch even though they look related):
- `scripts/lint.sh`, the README "Development" section, pinning `stringer`/`goimports` in
  `mise.toml`, a `formatters:` block in `.golangci.yml`, `.editorconfig`, and every CI workflow
  change — **plan 018** (Steps 1, 2, 6) owns all of these. Note for 018's executor (put it in
  your report): 018's text still says golangci-lint 2.11.4; the pin is now 2.15.0.
- `internal/m4a/**` G115 hits — follow-up after plans 002/014.
- Enabling `unparam` — follow-up (would change m4a signatures).
- README benchmark/godoc accuracy — plan 009.

## Git workflow

- Branch: `advisor/033-local-tooling-and-lint`
- Commit style: gitmoji single-line, no body, no attribution. Suggested commits:
  `🔧 Drop dead golangci-lint exclusions`, `🎨 Apply go fix modernizations`,
  `🚨 Re-enable gosec G115 outside internal/m4a`, `🔧 Add mise tasks mirroring CI`,
  `📝 Add AGENTS.md toolchain notes`.
- Do NOT push or open a PR unless instructed.

## Steps

### Step 1: Remove dead config entries

In `.golangci.yml`:
1. Delete `- examples$` from `linters.exclusions.paths` (`:119`).
2. Delete `- unparam` from the first `_test\.go` rule (`:137`).
3. Delete the rule at `:142-143` (`path: _test\.go` / `text: 'shadow: declaration of "err"'`).
4. Delete the rule at `:156-158` (comment `# Library opens user-provided file paths by design`
   + `linters: [gosec]` / `text: "G304"`) — `G304` stays in `gosec.excludes`.

Removing `examples$` makes examples linted for real; they were already linted (the regex never
matched), so the issue count must stay 0.

**Verify**: `mise exec -- golangci-lint config verify` → exit 0;
`mise exec -- golangci-lint run ./...` → `0 issues.`

### Step 2: Apply `go fix` modernizations

Run `go fix ./...`. Confirm the diff touches exactly the 8 files listed in Scope
(`git diff --stat`). Review each hunk reads naturally (the `strings.Cut` rewrite in
`internal/vorbis/chapters.go:42` introduces `before, after, ok` — fine).

**Verify**: `go fix -diff ./...` → no output; `gofmt -l .` → no output;
`go build ./...` → exit 0; `go test -race ./...` → all pass.

### Step 3: Enable `modernize` and scope G115

In `.golangci.yml`:
1. Add `- modernize` to `linters.enable` (keep the list alphabetical: between `misspell` and
   `nakedret`).
2. Remove `- G115   # Integer overflow conversion` from `gosec.excludes` (`:77`).
3. Add an exclusion rule (next to the other gosec rules) so the M4A backlog doesn't block:
   ```yaml
      # G115 backlog in the M4A parser (51 hits) is triaged separately, after
      # plans 002/014 rewrite that arithmetic. Do not widen this rule.
      - linters: [gosec]
        path: internal/m4a/
        text: "G115"
   ```
   (`_test.go` and `cmd/` are already exempt from gosec entirely.)
4. Fix the 5 remaining hits:
   - `internal/mp3/technical.go:95` and `:101`: compare as `int` instead of converting the table
     length: `if int(bitrateIdx) < len(bitrateTable) {` and
     `if int(sampleRateIdx) < len(sampleRateTable) {` (the indices are masked to 4 and 2 bits;
     `uint32 → int` is lossless on the 64-bit targets this repo builds for). If gosec still
     flags them, add `//nolint:gosec // G115: index masked to 4 bits` (resp. 2 bits) instead.
   - `internal/ogg/container.go:92` and `:178`: the reinterpretation is intentional — the Ogg
     spec defines granule position as a signed two's-complement value where `-1` means "no
     packet finishes on this page". Annotate each line:
     `//nolint:gosec // G115: Ogg granule position is two's-complement; -1 means no packet ends here`.
   - `internal/ogg/opus.go:45`: Opus output gain is a signed Q7.8 value (RFC 7845 §5.1);
     annotate `//nolint:gosec // G115: OpusHead output gain is signed Q7.8 (RFC 7845)`.

**Verify**: `mise exec -- golangci-lint run ./...` → `0 issues.`;
`grep -n "G115" .golangci.yml` → only the comment + `text: "G115"` in the m4a rule;
`go test -race ./...` → all pass.

### Step 4: Add mise tasks that mirror CI

Append to `mise.toml` (keep `[tools]` unchanged):

```toml
[tasks.build]
description = "Build library and race build (mirrors CI build job)"
run = ["go build ./...", "go build -race ./..."]

[tasks.test]
description = "Race tests with coverage (mirrors CI test job)"
run = "go test -race -coverprofile=coverage.out -covermode=atomic ./..."

[tasks.lint]
description = "golangci-lint at the pinned version (mirrors CI lint job)"
run = "golangci-lint run ./..."

[tasks.fmt-check]
description = "Fail if any file needs gofmt"
run = 'test -z "$(gofmt -l .)" || { gofmt -l .; exit 1; }'

[tasks.generate]
description = "Regenerate stringer output (needs stringer on PATH)"
run = "go generate ./..."

[tasks.check]
description = "Everything CI checks, locally"
depends = ["build", "test", "lint", "fmt-check"]
run = "go vet ./..."
```

Inside `mise run`, the `[tools]` versions are on PATH first, so `lint` uses 2.15.0 regardless
of a stale global binary. The coverage profile goes to `coverage.out` (not CI's `coverage.txt`)
because only `*.out` is gitignored (`.gitignore:12` `*.out`; verified `coverage.txt` is not
ignored); `scripts/test.sh` uses the same name. Do NOT edit `.gitignore`.

**Verify**: `mise tasks ls` → lists build, check, fmt-check, generate, lint, test;
`mise run lint` → `0 issues.`; `mise run check` → exit 0; `git status --short` shows no
coverage file.

### Step 5: Add `AGENTS.md`

Create a short (≤40 lines) `AGENTS.md` at repo root for humans and coding agents:
- Toolchain: `mise install`; Go and golangci-lint versions come from `mise.toml`; `go.mod` floor
  is `go 1.26.0` — no APIs newer than Go 1.26.
- Always lint via `mise run lint` or `mise exec -- golangci-lint run ./...` (stale globals give
  false typecheck errors).
- Full local gate: `mise run check` (mirrors CI). `go generate ./...` requires `stringer`.
- Conventions: warnings via `types.Warning{Stage: types.StageX, ...}` not errors for
  recoverable data problems; `*types.CorruptedFileError` for structural corruption; stdlib
  `testing` only; `//nolint` must name the linter and give a reason.
- Commit style: gitmoji, single line, no body.

**Verify**: `test -f AGENTS.md && wc -l < AGENTS.md` → ≤ 40; `mise run check` → exit 0.

## Test plan

No new Go tests: the changes are config, mechanical rewrites (covered by the existing suite) and
annotations. Verification is `mise run check` (build, race tests, lint, gofmt, vet) all green.

## Done criteria

- [ ] `mise exec -- golangci-lint run ./...` → `0 issues.` with `modernize` enabled and G115
      excluded only for `internal/m4a/`
- [ ] `go fix -diff ./...` → no output
- [ ] `grep -nE "examples\\$|unparam|shadow: declaration" .golangci.yml` → no output
- [ ] `grep -c 'text: "G304"' .golangci.yml` → `0`
- [ ] `mise tasks ls` lists build, test, lint, fmt-check, generate, check; `mise run check` exits 0
- [ ] `go test -race ./...` exits 0; `gofmt -l .` prints nothing
- [ ] `AGENTS.md` exists
- [ ] `git status` shows only in-scope files modified
- [ ] `plans/README.md` status row for 033 updated; report includes the m4a G115 follow-up, the
      unparam follow-up and the "018 says 2.11.4" note

## STOP conditions

- `go fix ./...` touches files other than the 8 listed, or `go fix -diff` shows different
  hunks than "Current state" — STOP and report the file list (toolchain may differ from 1.27.3).
- After Step 3, golangci-lint reports more than the 5 listed G115 hits outside `internal/m4a/`
  — STOP and report the count and locations; do not blanket-`nolint` them.
- Enabling `modernize` reports issues that `go fix` did not fix — STOP and report them.
- `mise` rejects the `[tasks]` syntax (older mise) — STOP with the error and `mise --version`.

## Maintenance notes

- Follow-up plan: triage the 51 `internal/m4a/` G115 hits after plans 002 and 014 land, then
  delete the m4a exclusion rule. Most are `uint64 → int64` offset/size conversions that should
  become checked conversions against `SafeReader` size.
- Follow-up: enable `unparam` once the four always-nil error results in `internal/m4a` are
  simplified.
- `modernize` will now flag new pre-1.2x idioms; `go fix ./...` is the one-command fix.
- Reviewer focus: every new `//nolint:gosec` has a spec-grounded reason; the m4a exclusion is
  path-scoped and text-scoped (G115 only).
