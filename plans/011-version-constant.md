# Plan 011: Fix the stale Version constant

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving on. If a
> STOP condition occurs, stop and report — do not improvise. When done, update
> the status row for this plan in `plans/README.md`.
>
> **Drift check (run first)**: `git diff --stat aca9750..HEAD -- version.go`
> On a mismatch with the excerpt below, STOP.

## Status

- **Priority**: P2
- **Effort**: S
- **Risk**: LOW
- **Depends on**: none
- **Category**: dx
- **Planned at**: commit `aca9750`, 2026-10-30 (refreshed from 7b22897, 2026-07-01)

## Why this matters

`version.go:6` declares `const Version = "0.1.0"`, but the module's released tags run
`v0.2.0` through `v0.10.0` (verified via `git tag -l`; latest is `v0.10.0`). Any consumer
calling `audiometa.GetVersion()` or `GetVersionInfo()` is told "0.1.0" — nine minor releases
behind — which misleads bug reports and version gating. The constant is hand-maintained with
no automation keeping it honest, so it will drift again unless the mechanism changes. Go embeds
the real module version in build info on tagged installs; deriving the version from
`runtime/debug.ReadBuildInfo()` removes the manual constant entirely.

## Current state

`version.go` (verified, full relevant excerpt):

```go
// Version is the semantic version of the audiometa library.
const Version = "0.1.0"

// GetVersion returns the current version string.
func GetVersion() string {
	return Version
}
...
func GetVersionInfo() VersionInfo {
	goVer := goVersion
	if goVer == "unknown" {
		goVer = runtime.Version()
	}
	return VersionInfo{
		Version:   Version,
		GitCommit: gitCommit,
		BuildTime: buildTime,
		GoVersion: goVer,
	}
}

// Variables populated at build time via -ldflags.
var (
	gitCommit = "unknown"
	buildTime = "unknown"
	goVersion = "unknown"
)
```

Notes:
- `Version` is an exported **const**; converting it to a `var` or a function-derived value is
  technically an API change but the module is pre-1.0 and nothing in-repo references `Version`
  except `GetVersion`/`GetVersionInfo` (verify: `grep -rn "audiometa.Version\b" .`).
- `debug.ReadBuildInfo()` returns `Main.Version` = the module version (e.g. `v0.10.0`) when the
  package is consumed as a dependency at a tagged version; it returns `(devel)` or a pseudo-version
  when built from a working tree. That is *more* honest than a hardcoded string.

## Commands you will need

| Purpose | Command | Expected |
|---|---|---|
| Build | `go build ./...` | exit 0 |
| Tests | `go test ./...` | all pass |
| Latest tag | `git describe --tags --abbrev=0` | `v0.10.0` (or newer) |
| Lint | `mise exec -- golangci-lint run ./...` | exit 0, prints `0 issues.` |

> **Lint note**: run lint via `mise exec -- golangci-lint run ./...` so the mise-pinned
> golangci-lint v2.15.0 is used. A stale global golangci-lint (e.g. 2.11.4) on PATH reports
> false "export data version" typecheck errors under Go 1.27.

## Scope

**In scope**:
- `version.go`
- `version_test.go` (create)

**Out of scope**:
- Release automation / tag-push workflows — plan 018 territory (and even there only a note).
- README references to versioning (none exist today).

## Git workflow

- Branch: `advisor/011-version-constant`
- Commit style: gitmoji single-line, no body, no attribution (e.g. `🐛 Derive version from build info`).
- Do NOT push or open a PR unless instructed.

## Steps

### Step 1: Derive the version from build info, keep ldflags as override

Replace the hardcoded constant with a function that prefers (in order): the ldflags-injected
value, the module build info, then a fallback:

```go
// version is overridable at build time via
//   -ldflags "-X github.com/simonhull/audiometa.version=v1.2.3"
// When unset, the module version from build info is used (tagged installs
// report their real tag; working-tree builds report "(devel)").
var version = ""

// GetVersion returns the library's version string.
func GetVersion() string {
	if version != "" {
		return version
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" {
		return bi.Main.Version
	}
	return "unknown"
}
```

Update `GetVersionInfo()` to use `GetVersion()` for its `Version` field. Delete the exported
`const Version` (pre-1.0; nothing external can be verified to use it — note the removal in the
report as a breaking change for the changelog). Import `runtime/debug`.

Keep `gitCommit`/`buildTime` ldflags vars as-is (they're accurate: "unknown" unless injected).
Optional improvement while here: `debug.ReadBuildInfo()` also exposes VCS settings
(`vcs.revision`, `vcs.time`) — prefer those over "unknown" when ldflags are absent.

**Verify**: `go build ./...` → exit 0.

### Step 2: Test it

Create `version_test.go`:
- `GetVersion()` returns a non-empty string.
- With the package under test (not a tagged dependency), the value will be `(devel)` or `unknown`
  — assert only non-empty and that it does **not** equal the old lie `"0.1.0"`.
- `GetVersionInfo().Version == GetVersion()`.

**Verify**: `go test -run Version ./...` → PASS; `go test ./...` → all pass.

## Test plan

- `version_test.go` as above. Pattern: plain stdlib asserts like the rest of the repo.

## Done criteria

- [ ] `grep -n '"0.1.0"' version.go` → no matches
- [ ] `go build ./...`, `go test ./...`, `mise exec -- golangci-lint run ./...` exit 0
- [ ] `GetVersionInfo().Version` sourced from `GetVersion()`
- [ ] Report notes the `const Version` removal as an API change
- [ ] `plans/README.md` status row for 011 updated

## STOP conditions

- Something in-repo (or a doc promise) requires `Version` to remain a const — STOP and report;
  the fallback is to keep the const but add a CI check comparing it to `git describe` (plan 018
  could host that check).

## Maintenance notes

- After this lands, tagging a release requires **no** version-file edit — that's the point.
- If a `cmd/audiometa` CLI ships (plan 021), have it print `GetVersion()`; installs via
  `go install ...@vX.Y.Z` will then report correctly for free.
