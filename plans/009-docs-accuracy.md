# Plan 009: Fix inaccurate README and package godoc

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving on. If a
> STOP condition occurs, stop and report — do not improvise. When done, update
> the status row for this plan in `plans/README.md`.
>
> **Drift check (run first)**: `git diff --stat aca9750..HEAD -- README.md doc.go file.go`
> If any changed, re-verify each claim below against the live code; on a mismatch
> with the excerpts, STOP.

## Status

- **Priority**: P2
- **Effort**: S
- **Risk**: LOW (docs only; no code changes)
- **Depends on**: none
- **Category**: docs
- **Planned at**: commit `aca9750`, 2026-10-30 (refreshed from 7b22897, 2026-07-01)

## Why this matters

This is a published package (pkg.go.dev badge, README install instructions), and several of its
most visible docs are **actively wrong**: the headline CLI install command fails outright, the
pkg.go.dev landing text omits two fully supported formats and advertises features that don't
exist, the README's support matrix denies a shipped capability (Ogg artwork), and the benchmark
block cites benchmarks that were never written. Stale-and-wrong docs are worse than missing docs:
they break first-run onboarding and undermine trust in everything else. Every fix below is a
verified doc-vs-code mismatch with the ground truth cited.

## Current state — the verified mismatches to fix

Each item: **where the doc lies → what the code truth is → the edit**.

1. **Nonexistent CLI** — `README.md:192-201` documents
   `go install github.com/simonhull/audiometa/cmd/audiometa@latest` and `audiometa song.flac`;
   `README.md:297` says `go install ./cmd/audiometa`. Truth: `git ls-files cmd/` contains only
   `cmd/atom-dump-tool/atom-dump.go`. **Edit**: remove the "### audiometa" CLI section and the
   `./cmd/audiometa` install line (keep the `atom-dump-tool` section). Add a one-line note that a
   CLI is planned (plan 021 builds it — if 021 already landed, point at it instead of removing).

2. **Ogg/Opus missing from godoc** — `doc.go:4` says "supports multiple formats (FLAC, MP3,
   M4A/M4B)"; `doc.go:20-24` lists only those; `file.go:38` (Open godoc) says "Supported formats:
   FLAC, MP3, M4A, M4B". Truth: `internal/ogg/parser.go:256-260` registers `FormatOgg` and
   `FormatOpus`; README already lists them. **Edit**: add "Ogg Vorbis" and "Opus" to all three lists.

3. **README denies shipped Ogg artwork** — `README.md:143-144` marks Artwork "-" for Ogg Vorbis
   and Opus. Truth: `internal/ogg/artwork.go:15-55` implements `ExtractArtwork` (METADATA_BLOCK_PICTURE)
   for the parser registered for both formats, reachable via the public API (`file.go:208`).
   **Edit**: change both Artwork cells from `-` to `✓`.

4. **Fabricated benchmark names** — `README.md:185-190` shows `BenchmarkOpen/FLAC-16`,
   `BenchmarkOpen/MP3-16`, `BenchmarkOpen/M4A-16` with numbers. Truth: `file_bench_test.go:53`
   `BenchmarkOpen` has no sub-benchmarks and opens only a synthetic empty M4B. **Edit**: replace the
   block with real output from `go test -bench 'BenchmarkOpen$' -benchmem -run '^$' .` (run it, paste
   actual output, note the machine), or delete the block. Do not keep invented numbers.

5. **Godoc "Go 1.25 Features" + nonexistent features** — `doc.go:108` heading says "Go 1.25"
   (go.mod says 1.26); `doc.go:112` says iterators use `iter.Seq` (truth: `iter.Seq2`,
   `internal/types/tags.go:69`); `doc.go:116` advertises "slog: Structured logging support"
   (truth: no `log/slog` import anywhere — grep matches only this comment line). **Edit**: retitle
   to Go 1.26, fix `iter.Seq` → `iter.Seq2`, delete the slog bullet.

6. **Dead doc links** — `doc.go:127` references `MODERN_GO_PATTERNS.md`, `doc.go:131` references
   `CONTRIBUTING.md`; neither file exists. **Edit**: remove both references (or the whole
   "Reference Implementation"/"Contributing" pointer lines).

7. **Phantom docs/ directory** — `README.md:240` lists `docs/` in the Project Structure tree; it
   doesn't exist. **Edit**: remove the line.

8. **"Zero-copy" claim** — `doc.go:100-101` and `README.md:16` claim zero-copy/minimal allocation.
   Truth: every scalar read allocates (`internal/binary/reader.go:78`), and until plans 002/007 land,
   artwork bytes are read eagerly on MP3. **Edit**: soften to accurate wording (e.g. "lazy artwork
   loading; metadata-only reads") — do not claim zero-copy.

9. **Go-version feature attribution** — `README.md:218-224` says Go 1.26+ is required *for* features
   that shipped in 1.21–1.23 (slices/maps/cmp, min/max/clear, range-int, iterators). **Edit**: keep
   the 1.26 requirement statement (go.mod still says `go 1.26.0`; CI and `mise.toml` build with
   Go 1.27, which does not change the minimum) but reword so the feature list isn't presented as
   the reason (e.g. "Requires Go 1.26+ (see go.mod). Uses modern Go features including:").

10. **Open() options example** — while editing `file.go` godoc for item 2, verify the `Open` doc
    comment's option example (`file.go:46-51`) matches real option names (it does today:
    `WithStrictParsing`, `WithArtworkPreload`) — leave as-is unless drifted.

## Commands you will need

| Purpose | Command | Expected |
|---|---|---|
| Build (docs can't break it, but sanity) | `go build ./...` | exit 0 |
| Godoc render check | `go doc github.com/simonhull/audiometa` | shows corrected format list |
| Real benchmark output (for item 4) | `go test -bench 'BenchmarkOpen$' -benchmem -run '^$' .` | prints real numbers |
| Lint (godot checks comment periods) | `mise exec -- golangci-lint run ./...` | exit 0, prints `0 issues.` |

> **Lint note**: run lint via `mise exec -- golangci-lint run ./...` so the mise-pinned
> golangci-lint v2.15.0 is used. A stale global golangci-lint (e.g. 2.11.4) on PATH reports
> false "export data version" typecheck errors under Go 1.27.

## Scope

**In scope**:
- `README.md`
- `doc.go`
- `file.go` (godoc comment text only — no code)

**Out of scope**:
- `options.go` godoc — plan 008 owns the `WithMaxArtworkSize` wording; don't double-edit.
- `examples/` — plan 010 owns the batch example.
- Building the CLI — plan 021.
- Any behavioral code change whatsoever.

## Git workflow

- Branch: `advisor/009-docs-accuracy`
- Commit style: gitmoji single-line, no body, no attribution (e.g. `📝 Fix stale README and godoc claims`).
- Do NOT push or open a PR unless instructed.

## Steps

### Step 1: Fix `doc.go` (items 2, 5, 6, 8)

Apply the edits listed above. Keep the package comment's overall structure and voice; you are
correcting facts, not rewriting style.

**Verify**: `go doc github.com/simonhull/audiometa | head -30` mentions FLAC, MP3, M4A/M4B,
Ogg Vorbis, Opus; `grep -n "slog\|1.25\|MODERN_GO_PATTERNS\|CONTRIBUTING" doc.go` → no matches.

### Step 2: Fix `file.go` godoc (items 2, 10)

**Verify**: `grep -n "Supported formats" file.go` shows the full five-format list.

### Step 3: Fix `README.md` (items 1, 3, 4, 7, 8, 9)

For item 4, run the benchmark command and paste genuine output (label the hardware briefly), or
remove the block entirely — either is acceptable; invented numbers are not.

**Verify**:
- `grep -n "cmd/audiometa" README.md` → no matches (unless plan 021 landed and you're pointing at the real tool)
- `grep -n "docs/" README.md` → no Project Structure match
- `grep -n "BenchmarkOpen/" README.md` → no matches
- The format table row for Ogg Vorbis and Opus shows `✓` in the Artwork column

### Step 4: Full gates

**Verify**: `go build ./...`, `go test ./...`, `mise exec -- golangci-lint run ./...` → all exit 0 (comment
edits can trip `godot`'s period rule — fix any flagged comment).

## Test plan

No new tests — this plan is documentation truth-telling. The greps in each step are the checks.

## Done criteria

- [ ] All greps in Steps 1–3 return the expected (non-)matches
- [ ] `go build ./...`, `go test ./...`, `mise exec -- golangci-lint run ./...` exit 0
- [ ] No `.go` file has non-comment changes (`git diff` shows only comment/README hunks)
- [ ] Benchmark block contains only real, reproduced output (or is removed)
- [ ] `plans/README.md` status row for 009 updated

## STOP conditions

- A claim marked "truth" above no longer matches the code (e.g. someone built `cmd/audiometa`,
  or Ogg artwork was removed) — STOP and re-verify that item before editing; fix docs to match
  the *current* truth, and note the discrepancy in your report.
- You find yourself wanting to change code to make a doc claim true — STOP; that's a different
  plan (021 for the CLI, 002/007 for performance claims).

## Maintenance notes

- Plan 021 (CLI) reinstates a Command-Line Tools section when the tool actually exists.
- Plan 018 adds CI checks; consider a future doc-link checker if drift recurs.
- Reviewer: spot-check each edited claim against the code one more time at review; docs drift
  is exactly the class of bug that survives inattentive review.
