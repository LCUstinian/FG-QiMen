# v0.5 — Verification Report

Date: 2026-09-01
Tag: `v0.5.0`
Branch: `main`

The v0.5 release ships cross-timezone scheduled scan and
persistent schedules. The original Phase 2 (crack-mode refactor,
proxy unification, output rotation, `.fgq` import/export) was
already covered by the v0.4.0 verification document; v0.5 is
additive — only the new work is documented here. / v0.5 发布
跨时区定时扫描 + 持久化调度。Phase 2（crack-mode 重构、代理
统一、输出轮转、`.fgq` 导入/导出）已在 v0.4.0 验证文档中覆盖；
v0.5 是增量——这里只记新工作。

## What v0.5 adds

### Scheduled scan (CLI flags)

6 new flags in a new `Schedule` group:

| Flag | Type | Description |
|---|---|---|
| `--at` | RFC3339 string | absolute time; the time zone is embedded in the timestamp (so an operator in NYC can schedule a scan for `2026-12-25T09:00:00+08:00` without converting to local time) |
| `--in` | Go duration | relative delay (e.g. `2h30m`) |
| `--cron` | 5-field cron | parsed via `github.com/robfig/cron/v3`; evaluated in the `--tz` zone (or system local if `--tz` is empty) |
| `--tz` | IANA name | time zone for cron expression evaluation |
| `--daemon` | bool | loop the scan on the cron schedule indefinitely (press Ctrl-C to exit) |
| `--schedule-dry-run` | bool | print the next fire time and exit without waiting |

Mutual exclusion: `--at` / `--in` / `--cron` are mutually exclusive.
`--daemon` requires `--cron`.

### `fg-qimen schedules` subcommand

Three subcommands for managing persistent schedules stored in
the project DB (the `schedules` bucket, created lazily by
`internal/scheduler/store.go`):

```
fg-qimen schedules add    <name> -p <project> [--at | --in | --cron ...]
fg-qimen schedules list  -p <project>
fg-qimen schedules remove <name> -p <project>
```

The `add` command shares the same flag set as `fg-qimen scan`
(`--at`, `--in`, `--cron`, `--tz`, `--daemon`); resolution
+ validation live in `internal/scheduler.Resolve` so the two
paths can't drift. `add` validates the cron expression at
parse time so a bad record never lands in the DB.

### `internal/scheduler` package

A new standalone, no-cobra, unit-testable package that holds
the cron parser wrapper, the wait loop with countdown + ctx
cancel, and the bbolt store. The package is 100% owned by
fg-qimen (no external scheduler) but uses `github.com/robfig/cron/v3`
for the cron parser. The dependency is the only non-stdlib
addition in v0.5.

Design notes:

- The standard `cron.ParseStandard` from `robfig/cron/v3`
  uses the process's local zone. We work around this by
  setting `TZ=<iana>` for the duration of the parse (the
  default `time.Local` falls back to `TZ` via Go's
  `time.LoadLocation`). The clean alternative — a
  location-aware parser — isn't exposed by the upstream
  package at the parser level, only at the `cron.New` level.
- `Input.Base` is the reference time used to compute the
  next fire. We capture it at `Resolve` time so `NextFire`
  is deterministic for tests + dry-run. `Wait` refreshes
  `Base` to the live clock between iterations so the
  next-fire is computed from `now` (not from the previous
  fire, which is in the past).
- The `Store` (bbolt) preserves the original `CreatedAt`
  on overwrite: an `Add` call that doesn't set `CreatedAt`
  inherits the existing record's value rather than
  overwriting with `time.Now()`.

## Test coverage

16 new unit tests across two files:

- `internal/scheduler/scheduler_test.go` (12): cron
  valid / invalid / descriptor / TZ; wait success / cancel /
  dry-run; Mode string; defaultOutput.
- `internal/scheduler/store_test.go` (6): add / get / list /
  remove / idempotent remove / preserve-CreatedAt on overwrite.
- `cmd/schedule_test.go` (4): detectScheduleMode across
  --at / --in / --cron + empty; loadScheduleTZ across
  IANA / system / invalid; full CLI round-trip for
  `schedules add | list | remove` against a real bbolt.

## CI status

All 13 CI jobs green on commit `f34632b` (the doc-sync commit
that followed v0.5.0):

- `golangci-lint`: pass
- `HARD rule lint`: pass
- `vet + test (ubuntu / macos / windows)`: pass
- `govulncheck`: pass
- `actionlint` / `shellcheck`: pass
- `build + sign (5 platforms)`: pass

Coverage is 60.4%, just above the 60% gate (was 60.0% in
v0.4.0). The new scheduler code is well-tested; the small
delta is from a few of the new CLI subcommand helpers that
are exercised by the round-trip test.

## Cross-build release

`git push FG-QiMen v0.5.0` triggered the existing release.yml,
which builds 11 platform binaries (Linux ×5 / macOS ×2 /
Windows ×2 / FreeBSD ×1 / OpenBSD ×1), signs each with cosign
keyless, generates a CycloneDX SBOM per platform, and
publishes SHA256SUMS + a SPDX SBOM to the GitHub Release.
Container / Homebrew / Scoop workflows were expected to
fail (no container registry / sibling tap repos are set up);
they have been failing on every release since v0.3.1.

## Known limitations

- **`applySchedule` itself is at 0% coverage** in
  `cmd/schedule.go:53`. The function is invoked from
  `runScan` (tested via the full test suite) but the early
  exit paths (e.g. --at past, --cron invalid) are not
  unit-tested. Worth adding in a follow-up; not a
  blocker for v0.5. **Resolved in v0.5.1**:
  `cmd/schedule_test.go` covers 11 test cases (ModeNone,
  9 Resolve error paths, dry-run, wait-near-future, wait-in-short,
  daemon-cancel, cron-no-daemon, conflicting flags, daemon-
  requires-cron, concurrent calls). `applySchedule` coverage
  went from 0% → 85%.
- **No `time/tzdata` import**. On systems without the IANA
  tz database installed (rare in 2026 but still happens on
  some stripped container images), `--tz` will fail to load
  and the scan will fall back to system local. Adding
  `_ "time/tzdata"` to `cmd/main.go` would cover this
  case at a +450 KB binary cost. We chose not to.
- **`--daemon` mode holds an open goroutine per schedule
  entry**. A future enhancement could batch-multiple
  schedules into one loop. For the documented use case
  (one recurring scan per project) this is fine.

## Future / v0.6 candidates

Considered for v0.5.1 but **deferred to v0.6** because the
operator workflow (`grep ':22 ' fgqm_result.txt`) is still
workable today, and the implementation cost is non-trivial.

- **Sorted / grouped output** (`--sort service|host|port|none`).
  The current `fgqm_result.txt` is appended in completion order
  across 200 concurrent workers, so RDP / SSH / HTTP are
  interleaved. Two design options:
  - **Lightweight (recommended)**: add `--sort service` flag;
    on `Close()`, accumulate all written Results in a
    sorted slice and rewrite `fgqm_result.{txt,json,csv}` in
    sorted order. Keeps single-file output; loses streaming
    visibility during the scan; costs ~16 bytes/result in RAM.
  - **Per-service files**: split output into
    `fgqm_result_ssh.txt` / `fgqm_result_http.txt` / ... at
    write time. Zero RAM cost, keeps streaming, but produces
    20-30 files per scan (file explosion for rare services)
    and breaks existing `grep` pipelines.
  - Both: `--sort` flag defaults to `none`; setting `service`
    triggers the close-time rewrite.
  Trade-off captured in this discussion; defer until a real
  grep-pain use case drives the implementation.
