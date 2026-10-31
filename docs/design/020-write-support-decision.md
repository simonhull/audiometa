# 020: Write support go/no-go memo

Planned at `aca9750`; re-verified against branch `fix/p2-spikes` (Go 1.27 base, 2026-10-31).

## Question

Should audiometa add metadata write support before 1.0?

## Who would use it

audiometa is a general-purpose public library with external users (maintainer confirmation).
It is not a ListenUp-internal dependency. Concrete consumer classes:

- Library managers and taggers (Go CLIs, TUIs, self-hosted media tools) that today shell out to
  `ffmpeg`, `tone`, `mp4v2`, `id3v2` or `metaflac` to fix tags.
- Ingest pipelines that normalise bad tags (missing narrator, wrong series) on import.
- Audiobook tooling that wants to write chapters and series data back into M4B/MP3.
- Go developers who want a pure-Go (no cgo, no external binary) alternative to TagLib/Mutagen.

ListenUp import check, run verbatim: `grep -rl 'simonhull/audiometa' ~/Code/listenup --include=go.mod`
produced no output (exit 1). ListenUp does not import this module. It has its own Kotlin port of the
reader and keeps user edits in its own data model, so it is not a driver for this feature either way.
(`hasScanWarning` was not found under `~/Code/listenup` with a `*.kt` grep; do not cite it.)

## Arguments for

- Pure-Go, no-cgo tagging is a real gap: Go has no widely trusted write library covering FLAC, MP3, M4A and Ogg together. Read-only libraries get adopted, then users ask for writes.
- Fixing bad tags at ingest (the normal audiobook workflow) requires writes; without it every user shells out to `tone`/`ffmpeg` and re-parses the result.
- Parity with TagLib/Mutagen: a "reader only" label caps adoption for a public tool.
- The in-memory edit model already exists (`Tags.Set`, `Merge`, `Clone`, `Equal`), and the read parsers already walk every container that a writer must walk.
- The corpus (`testdata/`, `audiometa_corpus_test.go`, fuzz targets) now exists, so a round-trip gate can be built before any write code ships.

## Arguments against

- Data-loss blast radius: a bug corrupts a user's only copy of an audiobook. A read bug costs a wrong string; a write bug costs the file.
- Audiobookshelf delegates writes to `tone`/`ffmpeg`; users have a working (if clumsy) path today.
- A server can keep user edits in its database and never touch the media (ListenUp does this).
- The read side still has open correctness plans (002, 004, 012, 013, 019). Several have since landed (012, 013), but a writer inherits every read-side misparse: if the reader mis-decodes a frame and the writer re-emits it, the file is silently changed.
- `Tags` has two sources of truth: `Tags.Set` writes only to the private `raw` map (`internal/types/tags.go`), not to typed fields (`Title`, `Narrator`, ...). A writer must pick one winner or it will write stale data.
- M4B structural risk: growing `ilst` grows `moov`; chunk offsets in `stco`/`co64` (including the chapter text track) must shift, or audio becomes unplayable.
- Ongoing cost: every new tag convention (iTunes freeform atoms, ID3 CHAP/CTOC, Vorbis picture blocks) becomes a write-compat promise.

## Alternatives

- (a) Defer, README honest: drop the Write column, state "read-only". Zero risk, zero capability.
- (b) Export only: serialise `Tags`/chapters to JSON or an OPF/`metadata.json` sidecar without touching media. Covers the "server keeps edits" case, tiny, safe, but is not tagging.
- (c) Document `tone`/`ffmpeg`/`metaflac` recipes and a `cmd/` helper that shells out. Honest and cheap, but not pure-Go.
- (d) Staged GO: ship a narrow, heavily-gated writer for one format, mark it experimental, expand only after the gates hold.

## Recommendation

**GO (staged).** This differs from the advisor's prior NO-GO (defer), and the reason is the confirmed
consumer base: the deferral argument rested on ListenUp not needing writes and ABS shelling out, but
both are statements about two particular servers. For a general-purpose public library, absence of
writes is the first limitation users hit, and a pure-Go option is the differentiator. The risk is real,
so the GO is conditional and staged: (1) no write code merges until the round-trip harness in the design
doc exists and runs in CI; (2) the writer is `WriteTo(io.Writer)`-shaped and atomic by construction, so
the library never truncates an original; (3) v1 covers one format (MP3/ID3v2, the lowest structural
risk), preserves unknown frames byte-for-byte, and is documented experimental; (4) read-side plans
002/004/019 should be DONE first, since a writer re-emits whatever the reader understood. The design is in
`020-write-support-design.md`. If the maintainer prefers the conservative path, choose alternative (a)
or (b); the README change would then be to drop the Write column.

## Decision

Maintainer decision (2026-10-31): **GO.**

- Format order: MP3 (ID3v2), then FLAC, then M4B/M4A. Ogg/Opus is deferred.
- v1 scope: text tags **and artwork**. Chapters are preserved byte-for-byte but are not editable in v1; chapter writing gets a follow-up plan.
- ID3 output version: preserve the input version (v2.3 stays v2.3, v2.4 stays v2.4). An untagged file, or an ID3v2.2 input, gets v2.4.
- API status: **stable from day one**. There is no experimental label, so the public surface must be reviewed and frozen before the first format writer merges.
