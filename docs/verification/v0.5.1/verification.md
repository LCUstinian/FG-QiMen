# v0.5.1 — Verification Report

Date: 2026-09-04
Tag: `v0.5.1`
Branch: `main`

The v0.5.1 release bundles incremental hardening that landed on
`main` since v0.5.0: hard-exit data-loss fix, daily-bucketed
result directories, HH-MM-SS filename stamp, `fgqm_` prefix on
all result files, short-flag overhaul, `time/tzdata` embed, the
`applySchedule` test push, and the default `fgqm_alive.txt`
alive-list sink. This report documents only the v0.5.1-specific
work; cross-timezone scheduled scan was already verified in the
v0.5 report at
[`docs/verification/v0.5/verification.md`](../v0.5/verification.md).

/ v0.5.1 把 v0.5.0 之后陆续落到 main 的硬化项打包发布：硬退出丢
结果修复、按日分桶、HH-MM-SS 文件名时间戳、全部结果文件
`fgqm_` 前缀、短标志重构、`time/tzdata` 嵌入、`applySchedule`
测试补齐、默认 `fgqm_alive.txt` 存活列表 sink。本报告只记
v0.5.1 工作；跨时区调度扫描的验证见 v0.5 报告。

## What v0.5.1 adds

### Output / file / UX hardening

- **Hard-exit data loss on result sinks** (`cmd/scan.go`,
  `cmd/cmd_test.go`). On the hard-exit path (second SIGINT or
  drain timeout), `os.Exit(1)` previously skipped the deferred
  `sess.Out.Close()` in `runScan`, leaving up to 4 KB per sink
  plus the entire SARIF in-memory document unflushed.
  `preHardExit` now invokes `closeOutputForHardExit(sessOut)`
  before quitting the TUI. 3 regression tests cover the nil case,
  the streaming case (txt), and the SARIF single-doc case.

- **Daily-bucketed result directories** (`cmd/scan.go`,
  `cmd/cmd_test.go`, `cmd/flags.go`). Default result paths now
  include a local-date `YYYY-MM-DD` segment:
  `runs/default/<YYYY-MM-DD>/fgqm_result_<HH-MM-SS>.txt` (and
  the same shape under `runs/projects/<name>/`). Two same-day
  runs no longer clobber each other.

- **Per-run HH-MM-SS stamp on filenames** (`cmd/scan.go`,
  `cmd/cmd_test.go`). Files are now named
  `fgqm_result_<HH-MM-SS>.<ext>`; the stamp is captured once at
  scan start so all sinks in a single run share the same suffix.
  Format is dash-separated for Windows-filename compatibility.

- **`fgqm_` prefix on all result files** (`cmd/scan.go`,
  `cmd/projects.go`, `cmd/flags.go`,
  `internal/output/*_test.go`, `README*`,
  `docs/ARCHITECTURE.md`, `docs/SECURITY.md`). The seven default
  result filenames now carry `fgqm_` so they're identifiable in
  mixed directories. `targets.txt` stays unprefixed (operators
  edit it by hand).

- **Short-flag overhaul** (`cmd/flags.go`, `cmd/multishort.go`,
  `cmd/multishort_test.go`, `cmd/{root,resume,scan,schedules}.go`,
  `internal/core/credential/pool.go`, `README*`). Single-letter
  shorts are now lowercase mnemonic; 2-letter shorts are used for
  namespaced / paired flags. Awkward uppercase shorts (`-M`, `-X`,
  `-U`, `-W`, `-P`) are removed. See CHANGELOG for the full
  migration table.

### Default `fgqm_alive.txt` alive-list sink (NEW in v0.5.1)

- **One-IP-per-line dedup'd host list** (`cmd/scan.go`,
  `internal/output/output.go`, `internal/output/output_test.go`,
  `README.md`). Always-on by default — operators pipe it
  directly into `nmap -iL` / `masscan --targets` / `curl` loops,
  and there's no harm in writing it (an empty scan produces an
  empty file).
- **Same daily bucket + HH-MM-SS stamp** as the other
  timestamped sinks: `runs/<default|projects/<name>>/<YYYY-MM-DD>/
  fgqm_alive_<HH-MM-SS>.txt`.
- **Empty-`Host` no-op** — defensive against stray blank lines
  that would break `nmap -iL` in some versions.
- **`aliveSeen` dedup map under `aliveMu`** — concurrent writes
  from 200 workers produce exactly one line per host.
- **Nil-when-disabled safety** — `ResultAlivePath == ""` leaves
  `out.alive` nil; `WriteResult` safely no-ops the alive branch.
- **6 new tests** pinning the contract:
  - `cmd/cmd_test.go`:
    - `TestResolveOutputPath_AliveProjectMode`
    - `TestResolveOutputPath_AliveEphemeralMode`
  - `internal/output/output_test.go`:
    - `TestWriteResult_AliveSinkDedup`
    - `TestWriteResult_AliveSinkEmptyHost`
    - `TestWriteResult_AliveSinkConcurrent`
    - `TestWriteResult_AliveSinkNilWhenDisabled`
- **README doc correction**: the output-files list previously
  described the bare name `fgqm_alive.txt`; this release
  documents it as `fgqm_alive_HH-MM-SS.txt` to match the actual
  resolved path. CHANGELOG has the corresponding Added entry.

### Quality triad (A1 / A2 / A3 from the v0.5.1 roadmap)

- **A1: `applySchedule` test push** (`cmd/schedule_test.go`).
  The function was at 0% coverage in v0.5; now at 100%. Covers
  ModeNone early-return, all 9 Resolve error paths (`--at`
  malformed / past, `--in` malformed / zero / negative, `--cron`
  invalid, `--at+--in` mutex, `--daemon` without `--cron`,
  invalid `--tz`), dry-run for all 3 modes, wait-for-future-time,
  wait-for-in, daemon ctx-cancel, cron-without-daemon, and
  concurrent-call sanity.

- **A2: Coverage floor stays at 60%**
  (`scripts/ci-coverage-check.py`). The original A2 goal was
  65%, but 30+ adapted plugins (jenkins / ssh / ftp / kafka /
  mqtt etc.) sit at 0% coverage, dragging the total to 60.5%.
  65% is unreachable without investing in plugin fake-server
  fixtures (v0.6 / v0.7 work). The floor stays at 60% with an
  extensive docstring explaining the deferral. cmd/-unit-testable
  code is already at 64.4%.

- **A3: Embed `time/tzdata` in binary** (`main.go`). Adds ~400
  KB (compressed) so `--tz` works on stripped container images
  that don't ship `/usr/share/zoneinfo`. Default-on to eliminate
  the "works on my machine, breaks in CI" surprise.

- **6-field cron expressions** (`internal/scheduler/cron.go`).
  Changed parser from `cron.ParseStandard` (5-field) to
  `cron.NewParser(SecondOptional | ...)` (5 or 6 fields). The
  documented 5-field form (`0 9 * * *` etc.) still works; 6
  fields (`* * * * * *` = every second) are valid for fast tests
  and short-interval daemon jobs.

## Test coverage

- `cmd/` (unit-testable subset): **64.4%**
- Total (including the 30+ 0%-coverage adapted plugins): **~60.5%**
- `applySchedule`: **100%**
- New v0.5.1 alive-sink tests: **6** (2 path + 4 behavior)

## CI status

All 13 CI jobs green on the v0.5.1 tag commit:

- `golangci-lint`: pass
- `test` (Linux + macOS + Windows): pass
- `coverage` (60% floor enforced): pass
- `build` (Linux + macOS + Windows): pass
- `fuzz`: pass
- `container`: pass
- `nightly`: pass
- `workflow-lint`: pass
- `release` (dry-run): pass
- `homebrew-tap` (dry-run): pass
- `scoop-bucket` (dry-run): pass

## Compatibility / migration

- **Short-flag migration** (v0.5.0 → v0.5.1): see CHANGELOG
  `[Unreleased]` → "Short-flag overhaul" for the full table. No
  deprecated aliases kept — clean break.
- **Daily bucket + HH-MM-SS stamp on default result paths**:
  scripts that pass `-o` / `-j` / `--output-csv` / `--output-sarif`
  with an explicit path are unaffected (those flags bypass both
  the bucket and the stamp).
- **No breaking changes** to the `internal/output.OutputConfig`
  struct, `OpenOutput` signature, or `WriteResult` / `WriteCred`
  / `WriteRDP` method signatures.

## Known gaps (carried to v0.6+)

- A2 coverage floor at 65% — deferred until plugin fake-server
  fixtures exist (v0.6 / v0.7 work).
- 30+ adapted plugins at 0% coverage — same root cause.
- No alive-file format toggle (`--alive-format {txt,json,csv}`)
  — YAGNI for v0.5.1; revisit if operators request it.
- No comment header on the alive file (`# fg-qimen alive list`)
  — would be backwards-incompatible for `nmap -iL` users.

## See also

- `CHANGELOG.md` → `[Unreleased]` for the per-commit details.
- `docs/verification/v0.5/verification.md` for v0.5 (scheduled
  scan) verification.
- `docs/verification/v0.4/verification.md` for v0.4 (crack mode,
  proxy unification, output rotation, .fgm export/import, MQTT
  plugin) verification.

