# Plan 010: Fix the batch example's error handling

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving on. If a
> STOP condition occurs, stop and report — do not improvise. When done, update
> the status row for this plan in `plans/README.md`.
>
> **Drift check (run first)**: `git diff --stat aca9750..HEAD -- examples/batch/main.go file.go`
> On a mismatch with the excerpts below, STOP.

## Status

- **Priority**: P2
- **Effort**: S
- **Risk**: LOW (example code only)
- **Depends on**: none
- **Category**: docs (example teaches an anti-pattern)
- **Planned at**: commit `aca9750`, 2026-10-30 (refreshed from 7b22897, 2026-07-01)

## Why this matters

`OpenMany`'s documented contract (`file.go:252-296`) is *partial success*: one bad file does not
abort the others; the returned error is the `errors.Join` of individual failures; `results[i]` is
nil for failed files. The canonical concurrency example defeats exactly that design: it
`log.Fatalf`s on any non-nil error — discarding every successfully parsed file because one failed —
and its cleanup loop calls `f.Close()` without a nil check, which would panic on any failed slot.
Users copy examples verbatim; this one crashes their batch scan on the first corrupt file.

## Current state

`examples/batch/main.go:44-55` (verified):

```go
// Open all files concurrently
files, err := audiometa.OpenMany(ctx, paths...)
if err != nil {
	log.Fatalf("Failed to process files: %v", err)   // <-- aborts despite partial success
}

// Ensure all files are closed when done
defer func() {
	for _, f := range files {
		f.Close()                                     // <-- nil-panics on failed slots
	}
}()
```

The correct pattern is documented in `OpenMany`'s own godoc example (`file.go:263-287`):

```go
files, err := audiometa.OpenMany(ctx, paths...)
if err != nil {
	// One or more files failed; successful files are still returned.
	log.Printf("partial failure: %v", err)
}
defer func() {
	for _, f := range files {
		if f != nil {
			f.Close()
		}
	}
}()
for i, f := range files {
	if f == nil {
		continue // failed; see joined error
	}
	...
}
```

Also check the results loop later in the example (`examples/batch/main.go:~51-97`) — it iterates
`files` and dereferences fields; every dereference needs the `if f == nil { continue }` guard.

The example's header comment (`examples/batch/main.go:5`) claims "Handle errors gracefully" —
after this fix it will be true.

## Commands you will need

| Purpose | Command | Expected |
|---|---|---|
| Build | `go build ./examples/batch/` | exit 0 |
| Vet | `go vet ./examples/...` | exit 0 |
| Run (smoke) | `go run ./examples/batch testdata/sample.mp3 /nonexistent.mp3 testdata/sample.flac` | prints results for the 2 valid files, logs the failure, exits 0 |
| Lint | `mise exec -- golangci-lint run ./...` | exit 0, prints `0 issues.` |

(The run command assumes plan 001's `testdata/` exists; if not, use any two audio files and one
bad path. Check the example's expected CLI arguments first — read its `main()` for how it takes paths.)

> **Lint note**: run lint via `mise exec -- golangci-lint run ./...` so the mise-pinned
> golangci-lint v2.15.0 is used. A stale global golangci-lint (e.g. 2.11.4) on PATH reports
> false "export data version" typecheck errors under Go 1.27.

## Scope

**In scope**:
- `examples/batch/main.go`

**Out of scope**:
- `file.go` / `OpenMany` itself — its behavior and godoc are correct; do not change them.
- Other examples (`basic`, `artwork`, `chapters`) — out of scope unless the drift check reveals
  the same `Fatalf`-on-partial pattern; if it does, note it in the report, don't fix here.

## Git workflow

- Branch: `advisor/010-batch-example-fix`
- Commit style: gitmoji single-line, no body, no attribution (e.g. `📝 Fix batch example partial-failure handling`).
- Do NOT push or open a PR unless instructed.

## Steps

### Step 1: Align the example with the OpenMany godoc pattern

Replace the `log.Fatalf` with a non-fatal `log.Printf("partial failure: %v", err)`, add the
`if f != nil` guard in the deferred close loop, and add `if f == nil { continue }` at the top of
every loop over `files`. Keep the example's output formatting otherwise unchanged.

**Verify**: `go build ./examples/batch/` → exit 0; `go vet ./examples/...` → exit 0.

### Step 2: Smoke-run with a deliberate failure

Run the example with two valid files and one nonexistent path (see command above). Expected:
the two valid files' metadata prints, the failure is logged non-fatally, exit code 0.

**Verify**: the run command → output as described.

## Test plan

Examples have no test files in this repo (excluded in `.golangci.yml:119`); the smoke run is the test.

## Done criteria

- [ ] `grep -n "log.Fatalf" examples/batch/main.go` → no match on the OpenMany error path
- [ ] Every `range files` loop guards `f == nil`
- [ ] Smoke run with a bad path prints valid results and exits 0
- [ ] `go build ./...`, `mise exec -- golangci-lint run ./...` exit 0
- [ ] `plans/README.md` status row for 010 updated

## STOP conditions

- `OpenMany`'s semantics changed since planning (drift check on `file.go`) — STOP and re-read its
  godoc; the example must match whatever the current contract is.

## Maintenance notes

- If `OpenMany` ever gains option support or changes its error contract, this example must be
  updated in the same PR — reviewer should treat example drift as a build breakage.
