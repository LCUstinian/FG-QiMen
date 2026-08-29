# PR body: fix/fuzz-range-bounds → main

**URL to create PR:** https://github.com/LCUstinian/FG-QiMen/pull/new/fix/fuzz-range-bounds

---

## Problem

The daily `fuzz` workflow hung on certain malformed range specs that slipped past `parseRange`. Two distinct classes:

  1. **end < start** — e.g. `"0.0.0.1-0"` resolves to start=`0.0.0.1`, end=`0.0.0.0` via the bare-octet fallback. The expansion loop runs `incIP` ~2³² times before wrapping.
  2. **range size > MaxTargets** — e.g. `"::-8::"` is a syntactically valid IPv6 range (`::` → `8::`, 2¹²³ addresses), and `"0.0.0.0-255.255.255.255"` ≈ 2³². The pre-count in `initOne` and the emit loop in `emitOne` iterate an astronomical number of times before the streaming cap in `tryEmit` ever fires.

Both inputs caused `FuzzExpandTargets` to be killed with exit 1, breaking the daily fuzz workflow run.

## Fix

`parseRange` now rejects both classes up-front with normal parse errors. The `rangeSize` helper uses `big.Int` so IPv6 ranges don't overflow. All three call sites (`expandOne`, `initOne`, `emitOne`) already propagated `parseRange`'s error, so the fix is local.

## Regression coverage

- `TestExpandTargetsRangeEndBeforeStart` (3 sub-cases for bare-octet + full-form `end < start`)
- `TestExpandTargetsRangeTooLarge` (2 sub-cases for IPv6 mega-range + IPv4 full-space)
- Two `FuzzExpandTargets` corpus entries (`f968cad1e38925ec`, `a6a96672ca21d165`) re-run on every `go test` as regression tests

Both new tests have a 2-second hang-guard so a regression fails fast rather than hanging the test runner.

## Verification

- 90-second fuzz sweep: 34.7 M executions, 236 interesting inputs, **no new hangs**.
- Full unit test suite across all packages: passes.
- Both failing fuzz corpus entries complete in 12 ms.

## Scope

This PR is intentionally minimal — only `internal/types/network.go`, `internal/types/types_test.go`, and the two fuzz corpus entries. The other `main` CI failures (Dependabot bumps, lint errors, the broken actionlint docker:// syntax, the missing `go mod tidy` fix) are tracked in a separate PR (`ci/release-ecosystem-fixes`) so this bug fix can review independently.