# Plan 015: Stop tripping strict mode on informational Opus warnings

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving on. If a
> STOP condition occurs, stop and report — do not improvise. When done, update
> the status row for this plan in `plans/README.md`.
>
> **Drift check (run first)**: `git diff --stat aca9750..HEAD -- internal/ogg/opus.go file.go internal/types/errors.go`
> On a mismatch with the excerpts below, STOP.

## Status

- **Priority**: P2
- **Effort**: S
- **Risk**: LOW (removes two informational warnings; strict-mode behavior becomes correct)
- **Depends on**: none
- **Category**: bug / tech-debt
- **Planned at**: commit `aca9750`, 2026-10-30 (refreshed from 7b22897, 2026-07-01)

## Why this matters

`WithStrictParsing` treats **any** warning as fatal (`file.go:101-104`:
`if options.strictParsing && len(file.Warnings) > 0 { return nil, ... }`). But the Opus parser
emits warnings for perfectly normal conditions: an original input sample rate other than 48 kHz,
and a non-zero output gain. Nearly every Opus file transcoded from CD/44.1 kHz sources carries
`inputSampleRate = 44100` — so `Open(path, WithStrictParsing())` rejects most real-world Opus
files. The `Warning` type is documented as "non-fatal **issues** ... corrupted or unusual data"
(`internal/types/errors.go:52-60`); "this file was encoded from 44.1 kHz" is neither. No other
parser emits informational warnings — this is an Opus-only inconsistency that breaks a public
option.

## Current state

`internal/ogg/opus.go:56-70` (verified):

```go
// Add informational warnings for non-default values
if inputSampleRate != 48000 && inputSampleRate > 0 {
	file.Warnings = append(file.Warnings, types.Warning{
		Stage:   types.StageTechnical,
		Message: fmt.Sprintf("original sample rate was %d Hz (Opus outputs at 48 kHz)", inputSampleRate),
	})
}

if outputGain != 0 {
	gainDB := float64(outputGain) / 256.0
	file.Warnings = append(file.Warnings, types.Warning{
		Stage:   types.StageTechnical,
		Message: fmt.Sprintf("output gain: %.2f dB", gainDB),
	})
}
```

The strict gate — `file.go:100-104`:

```go
// Check strict parsing mode
if options.strictParsing && len(file.Warnings) > 0 {
	_ = f.Close()
	return nil, fmt.Errorf("strict parsing failed: %s", file.Warnings[0].Message)
}
```

Design decision made by this plan: **remove the two informational warnings** rather than invent a
severity taxonomy. Rationale: the data they carry is either recoverable from the raw tags/headers
or not actionable; a severity field on `Warning` is a public API change better designed in plan 022
(the `Validate()` spike), which explicitly owns the "what counts as a defect" taxonomy. Keep this
plan minimal and reversible.

If the operator wants to preserve the information: the original input sample rate is a natural
candidate for a raw tag entry (e.g. via `Tags` raw map once plan 005 lands) — optional Step 3.

## Commands you will need

| Purpose | Command | Expected |
|---|---|---|
| Build | `go build ./...` | exit 0 |
| Tests | `go test ./internal/ogg/ ./...` | all pass |
| Opus fixture (44.1k source) | plan 001's `testdata/sample.opus` (ffmpeg anullsrc r=44100) | exists |
| Lint | `mise exec -- golangci-lint run ./...` | exit 0, prints `0 issues.` |

> **Lint note**: run lint via `mise exec -- golangci-lint run ./...` so the mise-pinned
> golangci-lint v2.15.0 is used. A stale global golangci-lint (e.g. 2.11.4) on PATH reports
> false "export data version" typecheck errors under Go 1.27.

## Scope

**In scope**:
- `internal/ogg/opus.go` (the two warning blocks)
- `internal/ogg/parser_test.go` or `internal/ogg/*_test.go` for the strict-mode regression test
- Root-level test file for the public strict-mode assertion (may live in `options_test.go` from plan 008)

**Out of scope**:
- The strict gate in `file.go` (correct once warnings mean what they're documented to mean).
- A `Warning.Severity` field — plan 022's design space.
- Other parsers' warnings (none are informational — verified for FLAC/M4A/MP3).

## Git workflow

- Branch: `advisor/015-opus-warning-severity`
- Commit style: gitmoji single-line, no body, no attribution (e.g. `🐛 Drop informational Opus warnings`).
- Do NOT push or open a PR unless instructed.

## Steps

### Step 1: Remove the two informational warning blocks

Delete the `inputSampleRate != 48000` and `outputGain != 0` warning appends in
`internal/ogg/opus.go:56-70`. Keep the parsing of those fields (they're read at :43-45); replace
the warning blocks with a short comment:

```go
// inputSampleRate and outputGain are informational (normal encoder metadata,
// not defects) — do not emit warnings for them; warnings are reserved for
// parse degradation and would trip WithStrictParsing.
```

If an existing test asserts these warnings exist, update it to assert they do NOT (that's the
behavior change we want).

**Verify**: `go build ./...` → exit 0; `go test ./internal/ogg/` → pass (fix any test that
asserted the old warnings, per above).

### Step 2: Strict-mode regression test

Add a public-API test: `Open("testdata/sample.opus", WithStrictParsing())` succeeds (no error).
Plan 001's Opus fixture is generated from a 44.1 kHz source, so before this fix it fails strict
mode — making it the perfect regression. If plan 008's `options_test.go` exists, put it there.

```go
func TestStrictParsing_ValidOpus(t *testing.T) {
	f, err := audiometa.Open("testdata/sample.opus", audiometa.WithStrictParsing())
	if err != nil {
		t.Fatalf("strict mode rejected a valid Opus file: %v", err)
	}
	defer f.Close()
	if len(f.Warnings) != 0 {
		t.Errorf("unexpected warnings on valid Opus: %v", f.Warnings)
	}
}
```

**Verify**: `go test -race ./...` → all pass.

### Step 3 (optional): Preserve the info as raw tags

If plan 005 has landed (raw-tag parity), record `inputSampleRate`/`outputGain` into the raw map
under stable keys (e.g. `OPUS_INPUT_SAMPLE_RATE`, `OPUS_OUTPUT_GAIN`) so the data remains
accessible without abusing warnings. Skip this step if 005 hasn't landed; note the skip.

## Test plan

- `TestStrictParsing_ValidOpus` (public API) + updated internal Opus tests.
- Pattern: plan 008's options tests / existing ogg parser tests.

## Done criteria

- [ ] `go build ./...`, `go vet ./...`, `mise exec -- golangci-lint run ./...` exit 0
- [ ] `go test -race ./...` exits 0 including `TestStrictParsing_ValidOpus`
- [ ] `grep -n "original sample rate\|output gain" internal/ogg/opus.go` → only the explanatory comment, no Warning appends
- [ ] A genuinely corrupt file still produces warnings/errors (existing corrupt-input tests unchanged)
- [ ] `plans/README.md` status row for 015 updated

## STOP conditions

- A consumer-facing test or example depends on these two warnings' presence — STOP and report
  (the operator may prefer plan 022's severity approach instead of removal).
- The Opus fixture passes strict mode even *before* the change — then ffmpeg wrote 48000 as the
  input rate; craft an OpusHead with `inputSampleRate=44100` in the synthetic builder
  (`internal/ogg/parser_test.go` has Opus header builders) to get a failing-before test.

## Maintenance notes

- Plan 022 (Validate spike) is the right home for a real severity taxonomy; if it lands, it may
  reintroduce these data points as `Info`-level findings — by design, not via `Warnings`.
- Rule for future parser code (worth a comment in `types.Warning`'s godoc if drift recurs):
  a Warning must describe a *degradation of parsing*, not a property of the file.
