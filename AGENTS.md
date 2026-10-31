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

## Doc comment conventions
- Full sentences, starting with the identifier's name. Every package has a `// Package x ...` comment
  (one file only); the root overview lives in `doc.go`.
- Say which errors are returned and when (typed errors, sentinels, `ctx.Err()`), and state
  concurrency safety where it matters.
- Use doc links: `[File]`, `[OpenContext]`, `[errors.As]`, `[context.Context]`, `[*os.PathError]`.
  Methods of root type aliases (`Tags`, `AudioInfo`) do not resolve as links; write them as plain
  `Tags.Get` or link the internal type (`[types.Tags]`).
- Use `# Heading`, `  - ` lists and tab-indented code blocks; a `Deprecated:` note is its own paragraph.
- Prefer a runnable `Example*` in `example_test.go` (package `audiometa_test`, testdata corpus,
  verified `// Output:`) over a code block. Check rendering with `go doc -all .`; `go vet` checks
  Example names.
- README claims (formats, options, benchmark numbers) must match the code; paste real benchmark output only.

## Commits
Gitmoji prefix, single line, no body.
