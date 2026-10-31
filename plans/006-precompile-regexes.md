# Plan 006: Precompile series-parsing regexes once

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving on. If a
> STOP condition occurs, stop and report — do not improvise. When done, update
> the status row for this plan in `plans/README.md`.
>
> **Drift check (run first)**: `git diff --stat aca9750..HEAD -- internal/parsing/series.go`
> If it changed, compare against the excerpts below before proceeding; on a mismatch, STOP.

## Status

- **Priority**: P2
- **Effort**: S
- **Risk**: LOW (pure hoist of constant regexes to package level; no behavior change)
- **Depends on**: none
- **Category**: performance
- **Planned at**: commit `aca9750`, 2026-10-30 (refreshed from 7b22897, 2026-07-01)

## Why this matters

`internal/parsing/series.go` compiles its regular expressions **inside the functions**, on every
call. `ExtractSeriesPartFromText` compiles 9 patterns per call (`series.go:33-35`), and
`ParseGrouping` compiles 4 per call (`series.go:95-101`). These run in the audiobook series-fallback
path of every format parser (`internal/m4a/audiobook.go:48,184-194`, `internal/flac/parser.go:151,160`,
`internal/ogg/parser.go:168,177`, `internal/mp3/id3v2.go:65,76`). For an M4B with a series but no
explicit part, `resolveSeriesPart` can call `ExtractSeriesPartFromText` three times plus `ParseGrouping`
once — ~27+4 regex compilations on a single `Open()`. Regex compilation is among the most expensive
operations in the whole parse and is 100% avoidable: the patterns are compile-time constants. Hoisting
them to package-level `var`s compiles each once at init.

## Current state

`internal/parsing/series.go:20-42` (verified):

```go
func ExtractSeriesPartFromText(text string) string {
	...
	patterns := []string{
		`(?i)book\s+(\d+(?:\.\d+)?)`,
		... // 9 patterns total
		`^(\d+(?:\.\d+)?)$`,
	}
	for _, pattern := range patterns {
		re := regexp.MustCompile(pattern)                 // <-- compiled every call
		if matches := re.FindStringSubmatch(text); len(matches) > 1 {
			return normalizeSeriesPart(matches[1])
		}
	}
	return ""
}
```

`internal/parsing/series.go:87-102` (verified) — `ParseGrouping` builds a `[]struct{re *regexp.Regexp; ...}`
with 4 `regexp.MustCompile` calls **inside** the function body each call.

There is a third function `ExtractSeriesPartFromPath` — check whether it also compiles inline (grep
`regexp.MustCompile` in the file) and hoist any it has too.

## Commands you will need

| Purpose | Command | Expected |
|---|---|---|
| Build | `go build ./...` | exit 0 |
| Tests | `go test ./internal/parsing/ ./...` | all pass |
| Bench | `go test -bench . -benchmem -run '^$' ./internal/parsing/` | records ns/op (add a bench if none) |
| Lint | `mise exec -- golangci-lint run ./...` | exit 0, prints `0 issues.` |

> **Lint note**: run lint via `mise exec -- golangci-lint run ./...` so the mise-pinned
> golangci-lint v2.15.0 is used. A stale global golangci-lint (e.g. 2.11.4) on PATH reports
> false "export data version" typecheck errors under Go 1.27.

## Scope

**In scope**:
- `internal/parsing/series.go`
- `internal/parsing/series_test.go` (existing tests must pass unchanged; optionally add a benchmark)

**Out of scope**:
- The callers (m4a/flac/ogg/mp3) — they call the same functions; no change needed there.
- Regex *patterns* themselves — do not alter matching behavior.

## Git workflow

- Branch: `advisor/006-precompile-regexes`
- Commit style: gitmoji single-line, no body, no attribution (e.g. `⚡ Precompile series-parsing regexes`).
- Do NOT push or open a PR unless instructed.

## Steps

### Step 1: Hoist the patterns to package-level compiled vars

Move each `regexp.MustCompile` to a package-level `var`, compiled once. For the slice in
`ExtractSeriesPartFromText`:

```go
var seriesPartPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)book\s+(\d+(?:\.\d+)?)`),
	// ... all 9, in the SAME order as today
	regexp.MustCompile(`^(\d+(?:\.\d+)?)$`),
}

func ExtractSeriesPartFromText(text string) string {
	...
	for _, re := range seriesPartPatterns {
		if matches := re.FindStringSubmatch(text); len(matches) > 1 {
			return normalizeSeriesPart(matches[1])
		}
	}
	return ""
}
```

For `ParseGrouping`, hoist the `[]struct{re *regexp.Regexp; seriesGroup, partGroup int}` to a
package-level var with the same entries and order.

**Order matters** — the comment in `ParseGrouping` says "more specific patterns first." Preserve the
exact order so matching precedence is unchanged.

**Verify**: `go build ./...` → exit 0; `go test ./internal/parsing/` → all pass (behavior identical).

### Step 2: Confirm no inline MustCompile remains

**Verify**: `grep -n "regexp.MustCompile" internal/parsing/series.go` shows all matches now at package
scope (top-level `var` declarations), none inside a function body. `grep -n "MustCompile" ` should not
appear indented inside a `func`.

### Step 3 (optional): Add a benchmark to lock the win

Add `BenchmarkExtractSeriesPartFromText` and `BenchmarkParseGrouping` in `series_test.go` over a few
representative strings. Record before/after in your report (compilation cost is now amortized to zero).

**Verify**: `go test -bench . -benchmem -run '^$' ./internal/parsing/` → runs.

## Test plan

- No new correctness tests required — existing `series_test.go` proves behavior is unchanged.
- Optional benchmarks as above.
- Verification: `go test ./...` all pass.

## Done criteria

- [ ] `go build ./...`, `go vet ./...`, `mise exec -- golangci-lint run ./...` all exit 0
- [ ] `go test ./...` exits 0; `internal/parsing/series_test.go` passes unchanged
- [ ] `grep -n "regexp.MustCompile" internal/parsing/series.go` — all occurrences at package/var scope, none inside a function
- [ ] `plans/README.md` status row for 006 updated

## STOP conditions

- Reordering during the hoist changes a `series_test.go` expectation — STOP; the order must be preserved exactly.
- A pattern uses a value only available at runtime (it should not — all are literals) — STOP and report.

## Maintenance notes

- New series/grouping patterns should be added to the package-level vars, keeping the specific-first order.
- Reviewer should diff the pattern strings byte-for-byte against the originals to confirm no accidental edits.
