# Plan 016: Consolidate duplicated per-format tag/series/chapter logic

> **Executor instructions**: Follow this plan step by step. Run every
> verification command and confirm the expected result before moving on. If a
> STOP condition occurs, stop and report — do not improvise. When done, update
> the status row for this plan in `plans/README.md`.
>
> **Drift check (run first)**: `git diff --stat 7b22897..HEAD -- internal/`
> This plan touches many files; before starting, re-locate each excerpt below and
> confirm it still matches. Plans 002/003/005/006/007 may have landed in these
> files — that is expected drift; the duplicated *blocks* this plan removes should
> still be recognizable. If a block is already consolidated, skip that step.

## Status

- **Priority**: P3
- **Effort**: L
- **Risk**: MED (behavior-preserving refactor across all four parsers; wide test surface)
- **Depends on**: 001 (characterization tests), 005 (raw-tag parity — do NOT consolidate before it)
- **Category**: tech-debt
- **Planned at**: commit `7b22897`, 2026-07-01

## Why this matters

The same audiobook logic is re-implemented per