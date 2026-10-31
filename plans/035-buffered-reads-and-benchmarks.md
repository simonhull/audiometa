# Plan 035: Measure, then cut per-file ReadAt calls with a buffered SafeReader

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving on. If a
> STOP condition occurs, stop and report — do not improvise. When done, update
> the status row for this plan in `plans/README.md`.
>
> **This is a measure-first plan.** Step 1 (benchmarks) is valuable on its own and is always
> committed. Steps 2–3 (the optimization) are kept only if the Step 3 gate passes.
>
> **Drift check (run first)**: `git diff --stat aca9750..HEAD -- internal/binary/ file.go file_bench_test.go internal/testcorpus/ testdata/`
> If `internal/binary/reader.go` or `endian.go` changed (plans 002 and 017 also edit them),
> compare against the excerpts below; on a mismatch in `SafeReader.ReadAt`, `Read[T]` or
> `ReadEndian[T]`, STOP.

## Status

- **Priority**: P3
- **Effort**: M
- **Risk**: MED (touches the one primitive every parser reads through; mitigated by the full
  suite + exact-error-message tests + a measurable gate)
- **Depends on**: none (if plan 001 landed, its `testdata/` and `internal/testcorpus/` exist —
  add alongside them, don't modify its files)
- **Category**: perf
- **Planned at**: commit `aca9750`, 2026-10-30

## Why this matters

Every integer the parsers read is a separate `io.ReaderAt.ReadAt` — on an `*os.File` that is
one `pread` syscall — plus a 1–8 byte heap allocation. Measured at `aca9750` with a counting
reader around `openReader` on ffmpeg-generated files with 50 chapters: **M4B 302 ReadAt calls,
MP3 124, Ogg Opus 33, FLAC 24** per open. For an audiobook server scanning tens of thousands of
files, M4B open cost is dominated by syscalls, not parsing. A simulation that put a single
read-through window in front of the reader cut the M4B count from 302 to ≤7 (window 4–16 KiB)
— roughly 40×. But the repo has no benchmark that can show this: the existing ones open an empty
M4B or time `os.Open`+`Stat`, and README cites benchmark names that don't exist. This plan first
builds honest, per-format benchmarks with a `readat/op` metric, then adds a small window cache
inside `SafeReader` and keeps it only if the numbers prove it.

## Current state

**`internal/binary/reader.go:11-56`** — the primitive:

```go
type SafeReader struct {
	r    io.ReaderAt
	path string
	size int64
}
...
func (sr *SafeReader) ReadAt(b []byte, off int64, what string) error {
	// Check bounds
	if off < 0 || off >= sr.size {
		return fmt.Errorf("%s: offset %d out of bounds (file size: %d) while reading %s",
			sr.path, off, sr.size, what)
	}

	if off+int64(len(b)) > sr.size {
		return fmt.Errorf("%s: read of %d bytes at offset %d would exceed file size %d while reading %s",
			sr.path, len(b), off, sr.size, what)
	}

	n, err := sr.r.ReadAt(b, off)
	if err != nil && err != io.EOF {
		return fmt.Errorf("%s: failed to read %s at offset %d: %w", sr.path, what, off, err)
	}

	if n < len(b) {
		return fmt.Errorf("%s: short read for %s at offset %d: got %d bytes, expected %d",
			sr.path, what, off, n, len(b))
	}

	return nil
}
```

**`internal/binary/reader.go:60-97`** `Read[T]` (big-endian) and **`internal/binary/endian.go:50-97`**
`ReadEndian[T]` (used by `ReadLE`/`ReadBE`) both do:

```go
	buf := make([]byte, size)          // reader.go:78 / endian.go:66 — heap alloc (escapes via the io.ReaderAt interface)
	if err := sr.ReadAt(buf, off, what); err != nil {
		return zero, err
	}
```

Existing micro-benchmark confirms it: `BenchmarkRead_Uint32 … 13.92 ns/op 4 B/op 1 allocs/op`.

Read-heavy call sites (why the counts are high):
- `internal/m4a/atoms.go:61,68` — every atom header is 2 reads (size, then type).
- `internal/m4a/chapters.go:353-364,397-398,428-434` — `stts`/`stsz`/`stco` tables read one
  integer per `ReadAt`.
- `internal/ogg/container.go:28-88` `readPage` — 9 reads per page header (magic, version,
  header type, granule, serial, sequence, segment count, segment table, data).

Concurrency: every `binary.NewSafeReader` call creates a reader local to one parse/extract/
detect call; nothing in `internal/` starts goroutines (`grep -rn "go func" internal/` → none).
So making `SafeReader` stateful is safe; it must be documented "not safe for concurrent use".

**Existing benchmarks** (baseline at `aca9750`, AMD 32-thread box):

```
BenchmarkOpen-32                 12124 ns/op   2339 B/op   28 allocs/op   (file_bench_test.go:53 — empty-moov M4B only)
BenchmarkParseFLAC-32            12942 ns/op   1875 B/op   29 allocs/op   (internal/flac — loop includes os.Open+Stat)
BenchmarkParseOgg-32             22932 ns/op   2824 B/op   54 allocs/op   (internal/ogg — loop includes os.Open+Stat)
```

No M4A/MP3 benchmarks exist. README.md:186-190 cites `BenchmarkOpen/FLAC-16`, `/MP3-16`,
`/M4A-16` — these do not exist (plan 009 owns fixing README; do not edit it here).

Test conventions: stdlib `testing` only; root internal tests use `package audiometa`
(`format_test.go`). `openReader(ctx, r io.ReaderAt, size, path, options)` (`file.go:130`) is the
internal entry that takes any `io.ReaderAt` — use it to inject a counting reader. (If plan 032
landed, `openReader` may no longer take `options`; drop that argument.)

Tools on this machine (verified): `ffmpeg` with libopus and aac encoders.

## Commands you will need

| Purpose | Command | Expected on success |
|---|---|---|
| Generate fixtures | `sh internal/testcorpus/generate_bench.sh` | 4 files in `testdata/bench/` |
| New benchmarks | `go test -run '^$' -bench 'BenchmarkParse\|BenchmarkOpenFile' -benchmem -count 6 . \| tee <out>` | prints ns/op, B/op, allocs/op, readat/op per format |
| Binary micro-bench | `go test -run '^$' -bench . -benchmem ./internal/binary/` | `Read_Uint32` shows allocs |
| Compare (optional, needs network) | `go run golang.org/x/perf/cmd/benchstat@latest before.txt after.txt` | table of deltas |
| Build / vet | `go build ./... && go vet ./...` | exit 0 |
| Tests | `go test -race ./...` | all pass |
| Lint | `mise exec -- golangci-lint run ./...` | `0 issues.` |
| Format | `gofmt -l .` | no output |

Write benchmark outputs to the scratch/temp dir (e.g. `"$(go env GOTMPDIR)"` or `/tmp`), never
into the repo.

## Scope

**In scope**:
- `internal/testcorpus/generate_bench.sh` (create)
- `testdata/bench/` (create; 4 generated fixtures, committed)
- `bench_parse_test.go` (create, package `audiometa`) — counting reader + per-format benchmarks
- `internal/binary/reader.go` — `SafeReader` window + scratch buffer, `Read[T]`
- `internal/binary/endian.go` — `ReadEndian[T]` scratch buffer only
- `internal/binary/reader_test.go` — window/error-semantics tests

**Out of scope**:
- `README.md` benchmark block — plan 009 owns it. Report the exact command and your output so
  009 can paste real numbers.
- Restructuring parsers to read whole tables/headers in one call (e.g. read all of `stsz` as one
  slice, or the 27-byte Ogg page header at once) — follow-up; the window makes most of it moot.
- Precompiling regexes — plan 006. Allocation caps — plan 002. Deleting `Reader`/`ChainReader`
  — plan 017.
- `cmd/` tools; any parser logic change.

## Git workflow

- Branch: `advisor/035-buffered-reads-and-benchmarks`
- Commit style: gitmoji single-line, no body, no attribution, e.g.
  `✅ Add per-format parse benchmarks with readat/op`, `⚡️ Buffer small reads in SafeReader`.
- Step 1 is its own commit. Do NOT push or open a PR unless instructed.

## Steps

### Step 1: Fixtures + honest benchmarks (always kept)

1. Create `internal/testcorpus/generate_bench.sh` (`chmod +x`), run from repo root:

   ```sh
   #!/usr/bin/env bash
   # Regenerates testdata/bench/ fixtures for bench_parse_test.go. Requires ffmpeg (aac, libopus).
   set -euo pipefail
   OUT=testdata/bench
   mkdir -p "$OUT"
   META="$(mktemp)"
   trap 'rm -f "$META"' EXIT
   {
     echo ";FFMETADATA1"
     for kv in title=Book artist=Author album=Book album_artist=Author genre=Audiobook \
               composer=Narrator date=2020 comment=Comment description=Description \
               copyright=Copyright publisher=Publisher grouping="Series #1" show=Series \
               episode_id=1 track=1/1 disc=1/1 language=eng; do
       echo "$kv"
     done
     for i in $(seq 0 49); do
       printf '[CHAPTER]\nTIMEBASE=1/1000\nSTART=%d\nEND=%d\ntitle=Chapter %d\n' \
         $((i*200)) $((i*200+200)) "$i"
     done
   } > "$META"
   gen() { out=$1; shift
     ffmpeg -loglevel error -y -f lavfi -t 10 -i anullsrc=r=44100:cl=mono -i "$META" \
       -map 0:a -map_metadata 1 "$@" "$OUT/$out"; }
   gen book.m4b  -c:a aac -b:a 16k -map_chapters 1
   gen book.mp3  -b:a 32k -id3v2_version 3 -map_chapters 1
   gen book.flac
   gen book.opus -c:a libopus -map_chapters 1
   ls -l "$OUT"
   ```

   Note: `-t 10` must come **before** `-i anullsrc` (it is an input option); placed elsewhere,
   `anullsrc` is infinite and ffmpeg never exits.

2. Create `bench_parse_test.go` (package `audiometa`):

   ```go
   // countingReaderAt counts ReadAt calls on the wrapped reader.
   type countingReaderAt struct {
   	r     io.ReaderAt
   	calls int
   }

   func (c *countingReaderAt) ReadAt(p []byte, off int64) (int, error) {
   	c.calls++
   	return c.r.ReadAt(p, off)
   }

   var benchFixtures = []string{"book.m4b", "book.mp3", "book.flac", "book.opus"}

   // BenchmarkParse measures in-memory parsing (no syscalls) and reports how many
   // ReadAt calls one open issues — the proxy for pread syscalls on a real file.
   func BenchmarkParse(b *testing.B) {
   	for _, name := range benchFixtures {
   		b.Run(name, func(b *testing.B) {
   			data, err := os.ReadFile(filepath.Join("testdata", "bench", name))
   			if err != nil {
   				b.Fatal(err)
   			}
   			br := bytes.NewReader(data)
   			opts := defaultOptions()
   			b.ReportAllocs()
   			var calls int
   			for b.Loop() {
   				c := &countingReaderAt{r: br}
   				if _, err := openReader(context.Background(), c, int64(len(data)), name, opts); err != nil {
   					b.Fatal(err)
   				}
   				calls = c.calls
   			}
   			b.ReportMetric(float64(calls), "readat/op")
   		})
   	}
   }

   // BenchmarkOpenFile measures the public Open on a real file, syscalls included.
   func BenchmarkOpenFile(b *testing.B) {
   	for _, name := range benchFixtures {
   		b.Run(name, func(b *testing.B) {
   			path := filepath.Join("testdata", "bench", name)
   			b.ReportAllocs()
   			for b.Loop() {
   				f, err := Open(path)
   				if err != nil {
   					b.Fatal(err)
   				}
   				_ = f.Close()
   			}
   		})
   	}
   }
   ```

   Also add `TestBenchFixturesParse` asserting each fixture opens without error and that
   `book.m4b`, `book.mp3`, `book.opus` each report 50 chapters (verified at `aca9750` with
   equivalent 100 s fixtures: 50/50/50; FLAC 0 — ffmpeg writes no FLAC chapters). This pins
   parse *results* so Step 2 can't silently change them. Additionally snapshot each fixture's
   full parse result for Step 2: in the same test, compare `json.MarshalIndent(pf.file, "", "  ")` against
   a golden file (JSON, not `%+v`: `%+v` prints pointer fields such as `Audio.ReplayGain` as
   addresses, which differ run to run) stored in `testdata/bench/<name>.golden` (write the golden files once, with a
   `-update` flag pattern: `var update = flag.Bool("update", false, "rewrite golden files")`).

3. Record the baseline:
   `go test -run '^$' -bench 'BenchmarkParse|BenchmarkOpenFile' -benchmem -count 6 . | tee /tmp/bench-before.txt`

**Verify**: `ls testdata/bench/` → `book.flac book.m4b book.mp3 book.opus` (+ 4 `.golden`);
`go test -run TestBenchFixturesParse .` → PASS; the benchmark output has a `readat/op` column;
expect roughly M4B ≈300, MP3 ≈120, Opus ≈30, FLAC ≈25 (exact values will differ slightly from the
100 s fixtures measured during planning — record yours). Commit this step.

### Step 2: Window cache + allocation-free integer reads in `SafeReader`

In `internal/binary/reader.go`:

```go
// windowSize is the read-through cache size. Small reads are served from one
// cached window of the file; reads larger than windowSize/4 go straight to the
// underlying reader and leave the window untouched.
const windowSize = 16 << 10

// SafeReader wraps io.ReaderAt with bounds checking and helpful error messages.
//
// A SafeReader caches one window of the file to turn many small reads into one
// underlying ReadAt. It is not safe for concurrent use; create one per parse.
type SafeReader struct {
	r       io.ReaderAt
	path    string
	size    int64
	win     []byte  // allocated on first small read
	winOff  int64   // file offset of win[0]
	winLen  int     // valid bytes in win (0 = empty)
	scratch [8]byte // backing store for Read/ReadEndian, avoids per-call allocation
}
```

`ReadAt` keeps its two bounds checks **byte-for-byte unchanged** (same order, same messages).
After them:

```go
	if len(b) <= windowSize/4 {
		if sr.winLen > 0 && off >= sr.winOff && off+int64(len(b)) <= sr.winOff+int64(sr.winLen) {
			copy(b, sr.win[off-sr.winOff:])
			return nil
		}
		if sr.fill(off) {
			copy(b, sr.win[:len(b)])
			return nil
		}
		// fill failed: fall through to the direct read so errors are reported exactly as before
	}
	...existing direct-read code unchanged...
```

```go
// fill loads the window starting at off. It reports false (and leaves the window
// empty) on any error or short read, so the caller can fall back to a direct read.
func (sr *SafeReader) fill(off int64) bool {
	if sr.win == nil {
		sr.win = make([]byte, windowSize)
	}
	n := int64(windowSize)
	if rest := sr.size - off; rest < n {
		n = rest
	}
	got, err := sr.r.ReadAt(sr.win[:n], off)
	if (err != nil && !errors.Is(err, io.EOF)) || int64(got) < n {
		sr.winLen = 0
		return false
	}
	sr.winOff, sr.winLen = off, got
	return true
}
```

(`off+len(b) <= size` is already guaranteed by the bounds check, so after a successful fill
`len(b) <= n` holds.) Use `errors.Is` — `errorlint` is enabled; the existing `err != io.EOF`
comparison in the direct path may stay as is (it already passes lint).

Then remove the per-call allocation in `Read[T]` (`reader.go:78`) and `ReadEndian[T]`
(`endian.go:66`): replace `buf := make([]byte, size)` with `buf := sr.scratch[:size]`. Decode
from `buf` before any other read on `sr` (it is overwritten by the next call) — the existing
code already decodes immediately. Do not change the `Read`/`ReadEndian` signatures.

Add tests in `internal/binary/reader_test.go` (reuse `mockReader`; add a counting variant):
1. 100 sequential 4-byte reads inside 16 KiB → exactly 1 underlying `ReadAt`.
2. A read past the current window → 1 more underlying call, correct bytes.
3. A read larger than `windowSize/4` → goes direct (+1 call) and a subsequent small read inside
   the old window is still a hit (window not evicted).
4. Error messages unchanged: `off < 0`, `off >= size`, and overrun cases return exactly the old
   strings (assert with `==` on `err.Error()`).
5. Underlying reader that returns `(0, someErr)`: error message is
   `"<path>: failed to read <what> at offset <off>: <someErr>"` and `errors.Is(err, someErr)`.
6. Underlying reader that returns a short read with nil error: the old
   `short read for ... got N bytes, expected M` message.
7. Reads near EOF (window truncated to `size-off`) return correct bytes.
8. `testing.AllocsPerRun(100, func(){ Read[uint32](sr, 0, "x") })` → `0` after the first warm-up
   call (the window is allocated once).

**Verify**: `go test -race ./internal/binary/` → all pass;
`go test -run '^$' -bench 'BenchmarkRead_Uint32' -benchmem ./internal/binary/` → `0 allocs/op`;
`go test -race ./...` → all pass, **including `TestBenchFixturesParse` goldens unchanged**.

### Step 3: Re-measure and apply the gate

`go test -run '^$' -bench 'BenchmarkParse|BenchmarkOpenFile' -benchmem -count 6 . | tee /tmp/bench-after.txt`
(and `benchstat /tmp/bench-before.txt /tmp/bench-after.txt` if network allows).

Gate — ALL must hold to keep Step 2:
- `BenchmarkParse/book.m4b` readat/op drops by **≥5×** vs. baseline.
- `BenchmarkOpenFile/book.m4b` ns/op does not regress (median of 6 runs ≤ baseline median).
- `BenchmarkParse/*` and `BenchmarkOpenFile/*` B/op grow by at most ~3×`windowSize` per open
  (three SafeReaders can exist per open: detect, parse, artwork) — report the actual delta.

Optionally try `windowSize` 4 KiB and 64 KiB and keep the best `BenchmarkOpenFile/book.m4b`
ns/op; report all three.

If the gate fails: revert the Step 2 commit (`git revert`), keep Step 1, and STOP with both
benchmark outputs.

**Verify**: gate numbers written into your report (before/after table per format: ns/op, B/op,
allocs/op, readat/op).

## Test plan

- `TestBenchFixturesParse` + golden snapshots (Step 1) — parse results identical before/after.
- 8 `SafeReader` tests (Step 2) — window hits/misses/bypass, exact error messages, zero allocs.
- Full `go test -race ./...` — every existing parser test reads through `SafeReader`, so the
  whole suite exercises the change.
- Pattern: `internal/binary/reader_test.go:26-61` (`TestSafeReader_ReadAt_*`).

## Done criteria

- [ ] `testdata/bench/` holds 4 fixtures + 4 goldens; `internal/testcorpus/generate_bench.sh` exists
- [ ] `go test -run '^$' -bench 'BenchmarkParse' -benchmem .` prints a `readat/op` metric for all 4 formats
- [ ] Either (gate passed) `BenchmarkRead_Uint32` shows `0 allocs/op` and M4B readat/op dropped ≥5×,
      or (gate failed) Step 2 is reverted and the report says why
- [ ] `go build ./...`, `go vet ./...`, `gofmt -l .` clean; `mise exec -- golangci-lint run ./...` → `0 issues.`
- [ ] `go test -race ./...` exits 0; goldens unchanged
- [ ] `git status` shows only in-scope files
- [ ] `plans/README.md` row for 035 updated; report includes before/after tables and the exact
      benchmark command for plan 009

## STOP conditions

- ffmpeg can't produce one of the fixtures (missing aac/libopus) — STOP, report the encoder.
- A fixture fails to open or a chapter count differs from 50 (M4B/MP3/Opus) at baseline — that
  is a parser finding; STOP and report before optimizing.
- Any golden snapshot changes after Step 2 — STOP (the cache changed parse results); do not
  regenerate goldens to make it pass.
- An existing test asserts an error string that changes after Step 2 — STOP.
- The Step 3 gate fails — revert Step 2, keep Step 1, STOP with numbers.
- `internal/binary/reader.go` was already changed by plan 002 or 017 such that the excerpts
  don't match — STOP and report (rebase the design onto their version only if the
  `SafeReader.ReadAt` body is unchanged).

## Maintenance notes

- `SafeReader` is now stateful and not safe for concurrent use. Any future code that shares one
  across goroutines (e.g. parallel artwork decode) must create its own.
- Byte slices returned by `ReadAt` are always the caller's buffer (copied out of the window);
  never return `sr.win` sub-slices — a later fill would mutate them.
- Known observations from planning, not acted on here (report them; they belong to other plans):
  with ffmpeg-written FLAC/Opus files, `Tags.Title` comes back empty because ffmpeg writes
  lowercase Vorbis keys (`title=`) and `internal/vorbis/comments.go:41` switches on uppercase
  keys only, despite its comment saying keys are case-insensitive; and MP3 (ID3v2.3) `Title`
  comes back as `"Book\x00"` — the trailing NUL terminator is not stripped. These are plans 028
  (Vorbis key case) and 027 (ID3 terminators); if they have landed, the goldens should show real titles.
- Follow-up: read fixed headers in one call (Ogg page header 27 bytes; M4A atom header 8/16
  bytes) and whole `stts`/`stsz`/`stco` tables — fewer `SafeReader` calls and bounds checks,
  independent of the window.
- Reviewer focus: bounds checks precede the window lookup; fill failure falls back to the
  untouched direct path; scratch buffer decoded before any other read.
