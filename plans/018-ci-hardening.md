# Plan 018: Harden CI — formatting, generate-freshness, coverage, vuln scan, permissions

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving on. If a
> STOP condition occurs, stop and report — do not improvise. When done, update
> the status row for this plan in `plans/README.md`.
>
> **Drift check (run first)**: `git diff --stat aca9750..HEAD -- .github/ .golangci.yml mise.toml scripts/`
> On a mismatch with the excerpts below, STOP.

## Status

- **Priority**: P2
- **Effort**: M (many small, independent config edits)
- **Risk**: LOW (CI/config only; no library code)
- **Depends on**: none
- **Category**: dx / deps
- **Planned at**: commit `aca9750`, 2026-10-30 (refreshed from 7b22897, 2026-07-01)

## Why this matters

CI is green today but has blind spots that let quality rot merge silently:

1. **No formatting enforcement.** `.golangci.yml` (v2 schema) has **no `formatters:` section**,
   so the lint job never checks `gofmt`/`goimports`. The only format gate lives in
   `scripts/lint.sh`, which CI never runs. Unformatted code merges green.
2. **No `go generate` freshness check.** `//go:generate stringer` directives exist at
   `internal/types/format.go:11` and `internal/types/artwork.go:23` with committed outputs
   (`format_string.go`, `artworktype_string.go`). Nothing verifies they're regenerated after an
   enum edit — stale `String()` output ships silently.
3. **Coverage is computed then discarded.** `ci.yml:34` produces `coverage.txt`; no upload,
   no artifact, no threshold. Pure wasted CI work with zero visibility.
4. **No vulnerability scanning.** No `govulncheck` step anywhere; a stdlib or `x/sync` advisory
   affecting the parser would go unnoticed.
5. **No update automation.** No dependabot/renovate config for `gomod` or `github-actions`.
6. **Workflow permissions/pinning.** No `permissions:` block (the `GITHUB_TOKEN` gets the repo
   default scope), and actions ride mutable major tags (`actions/checkout@v7`, `setup-go@v7`,
   `golangci-lint-action@v9`) — standard supply-chain hygiene gaps for a public repo.
7. **Tooling drift paths.** `scripts/lint.sh` installs/uses whatever `golangci-lint` is on PATH
   (unversioned `curl | sh` suggestion), bypassing the `mise.toml` pin of 2.15.0 → local lint can
   disagree with CI. (Concretely: a stale global 2.11.4 on PATH fails under Go 1.27 with false
   "export data version" typecheck errors.)

## Current state

`.github/workflows/ci.yml` (verified, abridged):

```yaml
name: CI
on:
  push: { branches: [ main ] }
  pull_request: { branches: [ main ] }
env:
  GO_VERSION: '1.27'
jobs:
  test:    # checkout@v7, setup-go@v7 (cache: true), go mod download/verify,
           # go test -v -race -coverprofile=coverage.txt -covermode=atomic ./...
  lint:    # checkout@v7, setup-go@v7, golangci/golangci-lint-action@v9 (version v2.15.0)
  build:   # checkout@v7, setup-go@v7, go build -v ./..., go build -race -v ./...
```

No `permissions:` key anywhere in the file. `mise.toml` pins `go = "1.27.3"` and
`golangci-lint = "2.15.0"` (no stringer/goimports). `go.mod` still declares `go 1.26.0`.
`.golangci.yml` starts `version: "2"` with `linters:` but no top-level `formatters:` block.
No `.github/dependabot.yml`. `stringer` is not declared in any tool manifest (it resolves from
the developer's global installs today).

## Commands you will need

| Purpose | Command | Expected |
|---|---|---|
| Lint config validity | `mise exec -- golangci-lint config verify` | exit 0 |
| Lint run | `mise exec -- golangci-lint run ./...` | exit 0, prints `0 issues.` |
| Format check | `mise exec -- golangci-lint fmt --diff` | empty diff (after Step 1) |
| Generate freshness | `go generate ./... && git diff --exit-code` | exit 0 |
| Vuln scan | `go run golang.org/x/vuln/cmd/govulncheck@latest ./...` | "No vulnerabilities found" (or listed findings) |
| Workflow syntax | push to a branch / `gh workflow view` after merge | CI green |

> **Lint note**: run lint via `mise exec -- golangci-lint run ./...` so the mise-pinned
> golangci-lint v2.15.0 is used. A stale global golangci-lint (e.g. 2.11.4) on PATH reports
> false "export data version" typecheck errors under Go 1.27.

## Scope

**In scope**:
- `.github/workflows/ci.yml`
- `.github/dependabot.yml` (create)
- `.golangci.yml` (add `formatters:` only)
- `mise.toml` (add tool pins)
- `scripts/lint.sh` (use pinned tooling)
- `.editorconfig` (create, minimal)
- `README.md` — only the Development section lines that reference unpinned installs

**Out of scope**:
- Any `.go` source change. If `golangci-lint fmt` reformats files, commit that as its own
  mechanical commit (`🎨 gofmt/goimports`), listing files in the report.
- Release automation/goreleaser (note as follow-up only).
- Lowering the `go.mod` Go version floor — that's a compatibility decision for the operator;
  record it as an open question in your report, do not change `go.mod`.

## Git workflow

- Branch: `advisor/018-ci-hardening`
- Commit style: gitmoji single-line, no body, no attribution (e.g. `👷 Enforce formatting and generate-freshness in CI`).
- One commit per numbered concern is ideal (they're independently revertable).
- Do NOT push or open a PR unless instructed.

## Steps

### Step 1: Enable formatters in `.golangci.yml`

Add at top level (v2 schema):

```yaml
formatters:
  enable:
    - gofmt
    - goimports
  settings:
    gofmt:
      simplify: true
```

Run `mise exec -- golangci-lint fmt --diff`; if it reports diffs, apply with `mise exec -- golangci-lint fmt` and commit
separately as noted in Scope.

**Verify**: `mise exec -- golangci-lint config verify` → exit 0; `mise exec -- golangci-lint run ./...` → `0 issues.`;
`mise exec -- golangci-lint fmt --diff` → empty.

### Step 2: Add a generate-freshness CI step

Pin stringer and wire the check. In `mise.toml`:

```toml
[tools]
go = "1.27.3"
golangci-lint = "2.15.0"
"go:golang.org/x/tools/cmd/stringer" = "latest"   # or the current released version
"go:golang.org/x/tools/cmd/goimports" = "latest"
```

(If the mise `go:` backend is unavailable in CI, instead add a plain
`go install golang.org/x/tools/cmd/stringer@latest` line in the CI step below — choose one
mechanism and use it in both docs and CI.)

In `ci.yml`, extend the **build** job (or a new `generate` job):

```yaml
    - name: Verify generated code is fresh
      run: |
        go install golang.org/x/tools/cmd/stringer@latest
        go generate ./...
        git diff --exit-code
```

**Verify**: locally run `go generate ./... && git diff --exit-code` → exit 0.

### Step 3: Surface coverage

Simplest robust option (no external service account needed): upload the profile as an artifact
and print a summary. In the **test** job after the test step:

```yaml
    - name: Coverage summary
      run: go tool cover -func=coverage.txt | tail -5
    - name: Upload coverage artifact
      uses: actions/upload-artifact@<SHA>   # pin per Step 5
      with: { name: coverage, path: coverage.txt }
```

(If the operator prefers Codecov, note it as an alternative in the report; don't add a service
dependency unasked.)

**Verify**: workflow YAML parses (CI run on the branch is the real check).

### Step 4: Add govulncheck and dependabot

New CI step (test job or its own job):

```yaml
    - name: govulncheck
      run: go run golang.org/x/vuln/cmd/govulncheck@latest ./...
```

Create `.github/dependabot.yml`:

```yaml
version: 2
updates:
  - package-ecosystem: gomod
    directory: /
    schedule: { interval: weekly }
  - package-ecosystem: github-actions
    directory: /
    schedule: { interval: weekly }
```

**Verify**: run the govulncheck command locally → completes (report any findings verbatim).

### Step 5: Least-privilege token + SHA-pin actions

At the top of `ci.yml` add:

```yaml
permissions:
  contents: read
```

Pin each action to a full commit SHA with a version comment, e.g.
`uses: actions/checkout@<full-sha> # v7.x.y`. Resolve each SHA with
`gh api repos/<owner>/<repo>/git/ref/tags/<tag>` or from the action's releases page; use the
latest release of the already-used major (checkout v7, setup-go v7, golangci-lint-action v9).
Dependabot (Step 4) keeps them updated.

**Verify**: `grep -c "uses:.*@[0-9a-f]\{40\}" .github/workflows/ci.yml` equals the number of
`uses:` lines.

### Step 6: Fix the local-tooling drift paths

- `scripts/lint.sh`: replace the PATH lookup / unversioned curl-install suggestion with
  `mise exec -- golangci-lint run ./...` (or check `command -v mise` and instruct
  `mise install`), so local lint always matches the CI-pinned 2.15.0.
- `README.md` Development section: point at `mise install` as the tooling setup step; remove the
  unversioned golangci-lint install instruction.
- Create `.editorconfig`:

```ini
root = true
[*]
charset = utf-8
end_of_line = lf
insert_final_newline = true
[*.go]
indent_style = tab
[*.{yml,yaml,md}]
indent_style = space
indent_size = 2
[*.sh]
indent_style = space
indent_size = 4
```

**Verify**: `sh scripts/lint.sh` → runs the pinned version (`golangci-lint version` inside the
script path prints 2.15.0) and exits 0.

## Test plan

No Go tests — the verification commands above and one green CI run on the branch are the test.

## Done criteria

- [ ] `mise exec -- golangci-lint config verify` and `mise exec -- golangci-lint run ./...` exit 0 (`0 issues.`); `mise exec -- golangci-lint fmt --diff` empty
- [ ] `go generate ./... && git diff --exit-code` exits 0 locally and in CI
- [ ] CI uploads/prints coverage; govulncheck step present; dependabot.yml present
- [ ] `permissions: contents: read` at workflow top; every `uses:` is SHA-pinned with a version comment
- [ ] `scripts/lint.sh` uses the mise-pinned linter; README Development section updated; `.editorconfig` present
- [ ] All jobs green on the branch's CI run
- [ ] `plans/README.md` status row for 018 updated

## STOP conditions

- `golangci-lint fmt` wants to rewrite a large number of files (>10) — pause and report the list
  before committing the mechanical change.
- govulncheck reports a finding in a *reachable* call path — report it verbatim; do not attempt
  dependency upgrades in this plan.
- The mise `go:` backend fails in CI and the `go install` fallback also fails (network policy) —
  STOP with the CI logs.

## Maintenance notes

- Dependabot will now open PRs for action SHA bumps — merge cadence is the operator's call.
- Open question recorded for the operator (not acted on): lower the `go.mod` floor from
  `1.26.0` toward the oldest minor the code actually needs (README's feature list is all ≤1.23)
  to widen the consumer base; needs a CI build matrix on the older version before changing.
  Note that since `aca9750` CI builds only with Go 1.27 while `go.mod` declares 1.26.0, so the
  declared floor itself is no longer exercised by CI — the same matrix would cover it.
- Follow-up candidates (not in scope): tag-triggered release workflow; coverage threshold gate
  once plan 001 raises the baseline.
