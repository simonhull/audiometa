# Plan 002: Cap allocations sized from untrusted length/count fields

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving to the
> next step. If anything in the "STOP conditions" section occurs, stop and
> report — do not improvise. When done, update the status row for this plan
> in `plans/README.md`.
>
> **Drift check (run first)**: `git diff --stat 7b22897..HEAD -- internal/binary/ internal/mp3/ internal/m4a/ internal/flac/`
> If any in-scope file changed since this plan was written, compare the
> "Current state" excerpts against the live code before proceeding; on a
> mismatch, treat it as a STOP condition.

## Status

- **Priority**: P1
- **Effort**: M
- **Risk**: LOW (caps only reject pathological inputs; well-formed files are unaffected)
- **Depends on**: plans/001-verification-baseline.md (fuzz targets ver