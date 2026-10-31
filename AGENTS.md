# Working on audiometa

## Toolchain
- `mise install` installs the pinned Go and golangci-lint (see `mise.toml`). `go.mod` targets Go 1.27.
- Lint through `mise run lint` or `mise exec -- golangci-lint run ./...`. A stale global
  golangci-lint can report false typecheck errors.
- Full local gate: `mise run check` (build, race tests, lint, vet, gofmt; mirrors CI).
- `go generate ./...` (`mise run generate`) needs `stringer`, which mise pins.

## Conventions
- Recoverable data problems become `types.Warning{Stage: types.StageX, ...}`, not errors.
- Structural corruption returns `*types.CorruptedFileError`.
- Binary reads go through `SafeReader` methods: `sr.Read[uint32](off, "what")`, `sr.ReadLE[...]`.
- Standard `testing` package only. Benchmarks use `for b.Loop()`.
- Every `//nolint` names the linter and gives a reason.
- Exported identifiers carry godoc comments that start with the identifier name.

## Commits
Gitmoji prefix, single line, no body.
