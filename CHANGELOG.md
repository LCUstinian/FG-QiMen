# Changelog

> [中文版本](CHANGELOG.zh-CN.md)

All notable changes to FG-QiMen are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- **`applySchedule` unit tests** (`cmd/schedule_test.go`,
  12 cases including daemon-loops). The function was at 0%
  coverage in v0.5; now at **100%**. Covers ModeNone early-
  return, all 9 Resolve error paths (--at malformed / past,
  --in malformed / zero / negative, --cron invalid,
  --at+--in mutex, --daemon without --cron, invalid --tz),
  dry-run for all 3 modes,
  wait-for-future-time, wait-for-in, daemon ctx-cancel,
  cron-without-daemon, and concurrent-call sanity. Pinned via
  `errors.Is(..., scheduler.ErrInvalidCombination)` for
  the mutex cases.

- **More cmd/ tests** (`cmd/cmd_test.go`,
  `cmd/schedule_test.go`). Added tests for `applyTransport`
  (nil + flag propagation + empty-KnownHosts protection),
  `applyHTTPForm` (empty + populated), `detectScheduleMode`
  (all 4 mode + precedence), and `loadScheduleTZ` (empty /
  UTC / invalid-IANA no-panic). cmd/ coverage 59.6% → 64.4%
  on unit-testable code. The total coverage stays around 60.5%
  because of 30+ adapted plugins (jenkins/ssh/ftp/kafka/mqtt
  etc.) at 0% — those need fake-server infrastructure
  (tracked as v0.6 goal).

- **Default `fgqm_alive.txt` alive-host list sink**
  (`cmd/scan.go`, `internal/output/output.go`,
  `internal/output/output_test.go`, `README.md`,
  `docs/verification/v0.5.1/verification.md`). One IP per line,
  in-memory dedup'd under `aliveMu` so concurrent workers can't
  double-write. Path is `runs/<default|projects/<name>>/<YYYY-MM-DD>/
  fgqm_alive_<HH-MM-SS>.txt` — same daily-bucket + stamp scheme as
  the other timestamped sinks. The empty-`Host` case is a no-op
  (avoids stray blank lines that would break `nmap -iL`). The
  README output-files list previously described the bare name
  `fgqm_alive.txt`; this entry is the corresponding changelog
  reference for that doc correction. 4 unit tests in
  `internal/output/output_test.go` + 2 end-to-end path tests in
  `cmd/cmd_test.go` pin the contract.

### Changed

- **Coverage floor kept at 60%** (`scripts/ci-coverage-check.py`).
  The original A2 goal was 65%, but the 30+ adapted plugins
  at 0% coverage drag the total to 60.5% — 65% is
  unreachable without investing in plugin fake-server
  fixtures (v0.6 work). The floor stays at 60% with
  extensive docstring explaining the 65% deferral. cmd/
  unit-testable code is already at 64.4%.

- **6-field cron expressions** (`internal/scheduler/cron.go`).
  Changed parser from `cron.ParseStandard` (5-field) to
  `cron.NewParser(SecondOptional | ...)` (5 or 6 fields). The
  documented 5-field form (`0 9 * * *` etc.) still works; 6
  fields (`* * * * * *` = every second) are now valid for
  fast tests and short-interval daemon jobs.

- **Embed `time/tzdata` in binary** (`main.go`). Adds ~400 KB
  (compressed) to the binary so `--tz` works on stripped
  container images that don't ship `/usr/share/zoneinfo`.
  Without this, a missing system tz DB silently falls back
  to `time.Local` (UTC offset 0 in many minimal containers),
  producing wrong cron fire times. v0.5.1 makes this default-
  on to eliminate the "works on my machine, breaks in CI"
  surprise.

- **Short-flag overhaul** (`cmd/flags.go`, `cmd/multishort.go`,
  `cmd/multishort_test.go`, `cmd/{root,resume,scan,schedules}.go`,
  `internal/core/credential/pool.go`, `README*`). Single-letter
  shorts are now all lowercase and mnemonic; 2-letter shorts

### Changed

- **Short-flag overhaul** (`cmd/flags.go`, `cmd/multishort.go`,
  `cmd/multishort_test.go`, `cmd/{root,resume,scan,schedules}.go`,
  `internal/core/credential/pool.go`, `README*`). Single-letter
  shorts are now all lowercase and mnemonic; 2-letter shorts
  are used for namespaced / paired flags (output-* and
  user/pass-file, nmap `-oN/-oX/-oG/-oA` precedent); awkward
  uppercase shorts with no mnemonic (`-M`, `-X`, `-U`, `-W`,
  `-P`) are removed. **Migration table** (v0.5.0 → v0.5.1):

  | 旧 | 新 |
  |---|---|
  | `-p corp` | `--project corp` |
  | `-M scan` | `--mode scan` |
  | `-X http://proxy:8080` | `--proxy http://proxy:8080` |
  | `-U users.txt` | `-uf users.txt` |
  | `-W pass.txt` | `-pf pass.txt` |
  | `-P admin,root` | `-p admin,root` |
  | `-o result.txt` | `-ot result.txt` |
  | `-j result.json` | `-oj result.json` |
  | (无) | `-oc result.csv` (新增) |
  | (无) | `-r` (`--resume` 短参新增) |

  No deprecated aliases kept — clean break. Underlying impl
  notes: pflag v1.0.9 panics on multi-char shorthands at
  registration, so `-ot` / `-oj` / `-oc` / `-uf` / `-pf` go
  through a 50-line pre-parse hook in `cmd/multishort.go`
  that rewrites them to `--output-txt` etc. before cobra
  sees the args. A flag-value heuristic (skip rewrite when
  the previous arg is flag-shaped) ensures literal passwords
  like `-p "-ot"` round-trip correctly via the long form.

- **`fgqm_` prefix on all result files** (`cmd/scan.go`,
  `cmd/projects.go`, `cmd/flags.go`, `internal/output/*_test.go`,
  `README*`, `docs/ARCHITECTURE.md`, `docs/SECURITY.md`).
  All seven default result filenames now carry the `fgqm_`
  prefix so they're identifiable as fg-qimen's in mixed
  directories (`fgqm_result.txt`, `fgqm_result.json`,
  `fgqm_result.csv`, `fgqm_result.sarif`, `fgqm_creds.txt`,
  `fgqm_rdp.json`, `fgqm_rdp.txt`). `targets.txt` stays
  unprefixed because operators edit it by hand. The `-o` /
  `-j` / `--output-csv` / `--output-sarif` flags still let
  callers override the filename (and the path), so existing
  scripted pipelines that pass an explicit filename continue
  to work.

- **Per-run HH-MM-SS stamp on filenames** (`cmd/scan.go`,
  `cmd/cmd_test.go`). Two runs on the same day now produce
  distinct filenames instead of overwriting each other. The
  directory is still bucketed by `YYYY-MM-DD`; the timestamp
  goes on the file (`fgqm_result_14-30-22.txt` rather than
  `fgqm_result.txt`). Format is `HH-MM-SS` local-time
  (dash-separated for Windows-filename compatibility and to
  match the `YYYY-MM-DD` style). `-o` / `-j` /
  `--output-csv` / `--output-sarif` still bypass the stamp —
  operators who pass explicit paths want their exact path,
  not an auto-decorated one. The stamp is captured once at
  scan start, so all sinks in a single run share the same
  suffix.

- **Daily-bucketed result directories** (`cmd/scan.go`,
  `cmd/cmd_test.go`, `cmd/flags.go`). Default result-file paths
  now include a local-date `YYYY-MM-DD` segment so multi-day
  scans against the same project don't clobber each other.
  New layout (ephemeral mode shown, project mode is the same
  shape under `runs/projects/<name>/`):
  ```
  runs/default/2026-09-02/result.txt
  runs/default/2026-09-02/result.json
  runs/default/2026-09-02/creds.txt
  runs/default/2026-09-02/rdp.json
  runs/default/2026-09-02/rdp.txt
  ```
  `fg.db` (the persistent state / dedup DB) stays at the
  project root and is shared across days — only result
  artifacts are bucketed. The `-o` / `-j` / `--output-csv` /
  `--output-sarif` flags still take an explicit path and
  bypass the bucketing (operators who pass these want their
  exact path, not an auto-bucketed one). The bucket name is
  captured once at scan start, so a run that crosses midnight
  lands in a single folder rather than splitting its results.

### Fixed

- **Hard-exit data loss on result sinks** (`cmd/scan.go`,
  `cmd/cmd_test.go`). On the hard-exit path — second SIGINT
  or drain timeout — `os.Exit(1)` skips the deferred
  `sess.Out.Close()` in `runScan`, leaving up to 4 KB per
  sink (default `bufio.Writer`) of buffered writes plus the
  entire SARIF in-memory document unflushed on disk.
  `preHardExit` now invokes `closeOutputForHardExit(sessOut)`
  before quitting the TUI, so the last result row and the
  full SARIF document both land before the process dies.
  Three regression tests cover the nil case, the streaming
  case (txt), and the SARIF single-doc case.

  硬退出丢结果（cmd/scan.go、cmd/cmd_test.go）。硬退出路径
  （第二次 SIGINT 或 drain 超时）上 os.Exit(1) 跳过 runScan
  里 defer 的 sess.Out.Close()，导致每 sink 最多 4 KB（默认
  bufio.Writer）缓冲写入加上整个 SARIF 文档不落盘。preHardExit
  现在在 Quit TUI 前调 closeOutputForHardExit(sessOut)，让最
  后一行结果和完整 SARIF 文档在进程死前都落到磁盘。三个回归
  测试覆盖 nil、流式（txt）、SARIF 单文档场景。

## [0.5.0] - 2026-09-01

Cross-timezone scheduled scan. The user runs scans
regularly from a different time zone than the targets and
needed a way to schedule without host-side cron. v0.5 adds
`--at` / `--in` / `--cron` / `--tz` / `--daemon` for
in-band scheduling, and a `fg-qimen schedules add | list |
remove` subcommand for persistent schedules stored in the
project DB. / 跨时区定时扫描。用户经常从与目标不同时区发起
扫描，需要不依赖宿主机 cron 的调度方式。v0.5 加 `--at` /
`--in` / `--cron` / `--tz` / `--daemon` 做带内调度，加
`fg-qimen schedules add | list | remove` 子命令做存项目 DB
的持久化调度。

Full verification: [`docs/verification/v0.5/verification.md`](docs/verification/v0.5/verification.md)

### Added

- **Scheduled scan flags** (`--at`, `--in`, `--cron`, `--tz`,
  `--daemon`, `--schedule-dry-run`): see [CLI reference](README.md#cli-reference).
  The scan waits for the target time before opening any sockets
  or files, so a misconfigured schedule fails fast.
- **`fg-qimen schedules add | list | remove` subcommand**:
  persistent schedules in the project DB (`schedules` bbolt
  bucket, opened lazily by `internal/scheduler/store.go`).
  `add` validates the cron expression at parse time so a bad
  record never lands in the DB.
- **`internal/scheduler` package**: standalone, no-cobra,
  unit-testable. Holds the cron parser wrapper, wait-loop
  with countdown + ctx cancel, and the bbolt store.

### Changed

- **New external dep: `github.com/robfig/cron/v3`** (~50 KB
  binary). Chosen on explicit user request after the
  classifier blocked the dep on the first pass; the
  alternative was a self-rolled ~150-line cron parser. We
  switched because robfig/cron/v3 handles edge cases (DST
  transitions, seconds field, descriptor syntax like
  `@daily`, `@hourly`) that the self-rolled version would
  inevitably re-discover and re-fix badly.

### Test coverage

16 new unit tests in `internal/scheduler/` and
`cmd/schedule_test.go`:
- cron: valid / invalid / descriptor (`@hourly`, `@daily`,
  `@midnight`) / TZ loading
- wait: success / cancel / dry-run
- bbolt store: add / get / list / remove / idempotent
  remove / preserve-CreatedAt on overwrite
- CLI: detectScheduleMode (3 modes + empty), loadScheduleTZ
  (3 variants), full `schedules add → list → remove` round-trip

Total coverage now 60.4% (was 60.0% in v0.4.0).

### Compatibility

- `--output-rotate-bytes` / `--output-rotate-files` renamed to
  `--rotate-bytes` / `--rotate-files` (renamed in v0.4.1; the
  `output-` prefix was redundant). No other breaking changes.
- The "v0.4 core improvements" content from earlier READMEs
  has moved to this changelog. Per-plugin `(added v0.X)` notes
  have moved to this changelog too. The READMEs now describe
  the current behaviour without per-version changelog.

## [0.4.0] - 2026-08-30

### Added

- **Crack-mode refactor** (`core.RunScan`): `ModeCrack` now
  skips the alive + port-scan stages and feeds a pre-known
  host:port list straight into the plugin worker pool. A
  256-host /24 × 6-port crack now skips ~1536 redundant TCP
  connects vs. the previous mode-conditional path.
- **Proxy unification** (`--proxy` / `--socks5`): all
  auth-tree TCP dial sites route through
  `credential.DialTCP` (or `credential.DialTCPAddr` for
  pre-joined `host:port` strings), so the global proxy
  manager applies uniformly. Telnet, VNC, SSH migrated;
  remaining UDP / custom-protocol plugins to follow.
- **Output rotation** (`--output-rotate-bytes N` +
  `--output-rotate-files M`): size-based rolling-file for
  TXT / NDJSON / CSV / SARIF sinks. Files rotate `<path>` →
  `<path>.1` → `<path>.2` → ... up to M total. Both flags
  must be > 0 to enable rotation; either 0 keeps the
  pre-v0.4 single-file behavior.
- **`.fgq` project import/export** (`projects export <name>
  <out.fgq>` / `projects import <in.fgq> <name>`): portable
  single-file project dump (4-byte magic `FGQ1` + JSON header
  + raw bbolt data). Format is forward-compatible across
  releases.
- **MQTT plugin** (1883 / 8883): Identify plugin for
  MQTT 3.1.1 / 5.0 brokers.
- **CLI ergonomics** (v0.4.1): short aliases added — `-U`
  (`--user-file`), `-W` (`--pass-file`), `-M` (`--mode`),
  `-X` (`--proxy`). The two `--output-rotate-*` flags
  shortened to `--rotate-*`. The audit added 10 missing
  plugins to the README plugin table (snmpv3, rdpnla,
  activemq, kafka, rocketmq, jenkins, kibana, weblogic,
  aws, azure).

### Changed

- **Coverage gate 50% → 60%** (CI fails below 60%).

### CI

- Fixed exit-126 / exit-127 in actionlint + coverage step
  (actionlint now downloads via a real file, not `bash
  <(curl)`; coverage uses a Python script).

## [0.4.0] - 2026-08-30

v0.4 cycle: quality foundation + four core pipeline improvements.
Combines the v0.4.0-rc1 quality pass (CI green, coverage 60%)
with the four Phase 2 features below. / v0.4 周期：质量基础 + 四
项核心管线改进。合并 v0.4.0-rc1 质量阶段（CI 绿、覆盖率 60%）
与下列四项 Phase 2 功能。

Full per-feature verification lives in
[`docs/verification/v0.4/verification.md`](docs/verification/v0.4/verification.md)
and [`docs/verification/v0.4/benchmarks.md`](docs/verification/v0.4/benchmarks.md).

### Added

- **Phase 2.1 — Crack-mode refactor** (`core.RunScan`): RunScan is
  now a thin dispatcher. ModeScan and ModeLinked go through
  `runFullPipeline` (alive → scan → identify → optional credential
  as before). ModeCrack goes through `runCrackPipeline`, which
  skips the alive + port-scan stages entirely and feeds a
  pre-known host:port list straight into the plugin worker pool.
  A 256-host /24 × 6-port crack now skips ~1536 redundant TCP
  connects that the previous mode-conditional code issued. /
  Phase 2.1 crack-mode 重构：RunScan 改为薄派发。ModeScan /
  ModeLinked 走 runFullPipeline（与之前相同 alive → scan →
  identify → 可选 credential）。ModeCrack 走 runCrackPipeline，
  完全跳过 alive + 端口扫描，直接把已知的 host:port 列表喂
  给 plugin worker 池。

- **Phase 2.2 — Proxy unification** (`credential.DialTCPAddr`):
  the new `credential.DialTCPAddr` function takes a pre-joined
  `host:port` string and routes it through the same global proxy
  manager as `credential.DialTCP`, so `--proxy` / `--socks5`
  apply uniformly across the auth tree. Migrated telnet, vnc,
  and ssh from raw `net.Dialer` to the new helper. Other plugins
  (modbus, bacnet, ipmi) keep raw `net.Dialer` because they need
  a transport-specific path (UDP / custom protocol setup) the
  unified TCP dialer doesn't fit. / Phase 2.2 代理统一：新增
  credential.DialTCPAddr 接收预拼的 `host:port` 字符串，走与
  DialTCP 同一全局 proxy manager，让 --proxy / --socks5 在整
  个 auth 树上统一生效。把 telnet、vnc、ssh 从 raw net.Dialer
  迁到新 helper。

- **Phase 2.3 — Output rotation** (`--output-rotate-bytes N`,
  `--output-rotate-files M`): new `rotatingWriter` rolls the
  TXT / NDJSON / CSV / SARIF sinks when they cross the byte cap.
  Files rotate `<path>` → `<path>.1` → `<path>.2` → ... up to
  the configured M total. `flushCloser` refactored to wrap the
  new type. 4 unit tests cover under-cap / at-cap / beyond-cap /
  zero-cap. / Phase 2.3 输出轮转：新增 rotatingWriter，当
  TXT / NDJSON / CSV / SARIF sink 跨过字节阈值时滚动。文件
  滚 `<path>` → `<path>.1` → ... 到 M 个总文件。

- **Phase 2.4 — `.fgq` project import/export** (`projects
  export` / `projects import`): single-file portable project
  dump. Format: 4-byte magic `FGQ1` + 4-byte LE uint32 header
  length + JSON header (version, project, created_at, db_bytes) +
  raw bbolt data, byte-for-byte. CLI: `fg-qimen projects
  export <name> <out.fgq>` / `fg-qimen projects import
  <in.fgq> <name>`. Import refuses to overwrite an existing
  project unless `delete` runs first. 4 unit tests + 2 CLI tests
  in `internal/workspace` + `cmd/projects_test.go`. / Phase 2.4
  .fgq 项目导入/导出：单文件可移植项目转储。格式 4 字节
  magic `FGQ1` + 4 字节 header 长度 + JSON header + 原始 bbolt
  数据。CLI 两条新命令。

### Coverage

- Coverage gate bumped 50% → 60% in this cycle (actual ~60.0%).
- New unit tests this cycle: 9 (5 in `internal/workspace`,
  4 in `internal/output` for rotation, plus 1 in
  `internal/version` for the const→var ldflag regression).

### CI

- CI exit-126 / exit-127 fixes from v0.4.0-rc1 carry forward:
  actionlint downloads via real file (not `bash <(curl)`);
  coverage check uses a Python script (deterministic, no
  SIGPIPE on `tail -1`).

## [0.3.1] - 2026-08-19 (original entry)

Second batch of audit-driven correctness, security, and reliability
fixes. All ten commits are documented in
[`docs/verification/v0.3/second-batch-verification.md`](docs/verification/v0.3/second-batch-verification.md);
this entry lists the user-visible deltas only.

### Security

- **P1-3 — per-attempt read deadlines** across seven TCP-based
  authenticators (telnet, rsync, vnc, modbus, nfs, rabbitmq, smb).
  Previously a slow/hung server that accepted TCP but never replied
  could wedge a worker for the full `cfg.Timeout`. Each authenticator
  now calls `conn.SetDeadline(time.Now().Add(timeout))` at the top of
  every credential iteration — same pattern as `redis.go:133` and
  `mongo.go:97`.
- **P2-7 — `f.Sync()` in `Output.Close()`**: `flushCloser.Close()`
  now fsyncs the underlying file before close, so the last ~200ms of
  buffered writes survive power-loss / OOM-kill instead of being
  silently dropped.
- **P2-5 — `creds.txt` dedup**: a re-dispatched `(host, port,
  user, pass)` hit no longer appends a duplicate line on `--resume`
  or after a retry. Gate is `sess.State.MarkSeen(chash)` at the
  pipeline sink. (`PutCred` to bbolt was already idempotent.)

### Fixed

- **P2-2 — `workersWG` joined to outer `wg`** in `RunScan`. Previously
  a future drift in the producer's `defer close(items)` could leave
  `wg.Wait()` hanging on a still-active worker pool. The closer
  goroutine is now registered with the outer `wg`, so a hung
  producer surfaces as a slow `RunScan` return rather than a
  permanent hang.
- **sink error propagation (residual)**: `persistResult` now checks
  every Output / Store error and surfaces it via `sess.Log.Warn`
  instead of silently dropping it.
- **P3-1 — BatchWriter `pendingBytes` dead code removed**: the
  per-op 64 KiB ceiling always equalled one op, so the byte
  threshold fired on the same op as the count threshold and was
  effectively unreachable.
- **P3-3 — adaptive goroutine join latency**: `Pool.adaptiveLoop`
  now uses a 50ms wake-up ticker (down from `opts.AdjustInterval`'s
  500ms default) so the loop sees `stopAdj` close promptly. `adjust()`
  is still throttled to `opts.AdjustInterval` via a timestamp guard.
- **P3-5 — MySQL `sqlcache` invalidation policy**: cache is now
  invalidated only on MySQL error 1045 (`ER_ACCESS_DENIED`). Network
  errors (refused / timeout / server-gone) leave the cached `*sql.DB`
  in place, preserving the Phase 1.9 warm pool under flaky networks.
- **P3-7 — Pool busy-wait CPU pegging**: producer backoff is now
  capped exponential (1ms → 50ms). CPU usage under saturation drops
  ~50×; responsiveness to a freed slot stays under 50ms.

### Docs

- **README supply-chain verification section**: bilingual
  instructions for verifying SHA256SUMS, cosign keyless signatures,
  CycloneDX SBOMs, source reproducibility, and reporting a
  discrepancy.
- `docs/SECURITY.md` now hosts the full no-exploit Hard Rule
  contract (moved from README); README keeps a 1-paragraph TL;DR.
- `docs/` reorganised: 18 historical progress / audit reports moved
  to `docs/archive/`; the top-level docs tree now holds only current
  guides (ARCHITECTURE / CONFIGURATION / PLUGIN_GUIDE / SECURITY /
  FIRST_BATCH_VERIFICATION / SECOND_BATCH_VERIFICATION /
  RELEASE_NOTES_v0.2).

## [0.3.0] - 2026-07-15

First batch of audit-driven correctness, performance, and security
fixes (7 commits). All seven commits are documented in
[`docs/verification/v0.3/first-batch-verification.md`](docs/verification/v0.3/first-batch-verification.md);
this entry lists the user-visible deltas only.

### Security

- **P0 — at-rest encryption for project DBs**: `internal/store/crypto.go`
  adds value-level AES-256-GCM encryption to `PutResult` / `PutCred`.
  New `--project-key` flag (and `FG_QIMEN_PROJECT_KEY` env var) opt-in.
  Plaintext v0.2.x bbolt files continue to read correctly (forward-
  compatible magic bytes).
- **P0 — workspace encryption wiring**: `workspace.AsStoreWithKey`
  plumbs the derived 32-byte key into the `Store` constructor; the
  seen-set bucket stays plaintext (it stores only non-secret hashes).
- **P1 — magic-byte AAD binding**: `store/crypto.go` binds the
  magic byte to the ciphertext via GCM AAD. Bit-flips on the magic
  byte are detected as `ErrDecryptFailed` rather than silently
  converting an encrypted row to "plaintext".
- **P1 — host flag injection guard**: alive `cmd` probe rejects host
  values starting with `-` (would be parsed as a `ping` flag).
- **P1 — graceful resume degradation**: corrupt bbolt on `--resume`
  logs a warning and continues with an empty seen-set rather than
  aborting.
- **P1 — input validation**: `--ports 99999` / `--ports abc` now
  propagates the parse error instead of silently running 0 ports.

### Performance

- **bbolt batched writes**: new `store.BatchWriter` + `PutMany`
  amortise one fsync per result into one per batch of 32 ops or 200ms.
  New `--no-batch` flag falls back to per-write semantics.
- **Output per-sink mutex split**: `Output` now uses 6 per-sink
  mutexes (txt / json / creds / rdp.json / rdp.txt / csv) so a slow
  sink can't head-of-line block the others. `csv.Writer` is hoisted
  to a field to avoid per-row allocation.
- **`RawTCPIdentify` helper**: shared TCP-dial boilerplate in
  `internal/plugins/rawtcp.go`. 5 plugins (redis, memcached,
  postgresql, mongodb, socks5) refactored to use it.
- **HTTP Transport reuse**: elasticsearch and web plugins now use
  process-level `http.Client` instead of allocating a fresh
  `http.Transport` per Identify call.

### Added

- **CSV output**: new `--output-csv` flag writes RFC-4180 one-row-per-
  result CSV with a stable column order. Same redaction policy as
  `result.txt` / `result.json`.
- **Stable error codes**: `internal/types/errors.go` defines
  `CodedError` with stable 4-character codes (E001..E999). Wired
  into `workspace.ValidateProjectName` and `bolt.Open` failure paths.
- **Flag grouping**: persistent flags now carry a `group` annotation
  rendered by a custom `usageTemplate` in `cmd/root.go` (Target,
  Workspace, Ports, Network, Concurrency, Credentials, Output,
  Behavior, Safety).
- **golangci-lint integration**: `.golangci.yml` enables errcheck,
  govet, staticcheck, revive, gocritic, gosec, errorlint, prealloc,
  misspell. CI workflow adds a `lint` job running `only-new-issues`.
- **CI**: govulncheck job + coverage upload (Linux only); `go mod
  tidy` enforced as a CI gate; release workflow tag-triggered build
  with `cosign` keyless signing and CycloneDX SBOM generation.

### Tests

Test coverage rose materially in this batch. Headline numbers
(see [`docs/verification/v0.3/first-batch-verification.md`](docs/verification/v0.3/first-batch-verification.md) for the full matrix):

- `internal/network/proxy`: **0% → 86.3%**
- `internal/store`: **N/A → 61.3%**
- `internal/output`: **86.8% → 89.5%**
- `internal/types`: **80.2% → 80.6%**
- `cmd`: **47.4% → 50.0%**

Plus ~70 new test cases across FTP authenticator, BatchWriter, magic-
byte AAD tamper detection, alive flag-injection guard, output
concurrent-writes, and the `RawTCPIdentify` helper.

## [0.2.0] - 2026-06-15

Initial public release (audit fixes from v0.1).
See [docs/verification/v0.2/release-notes.md](docs/verification/v0.2/release-notes.md) for the full list.

### Highlights

- 4-stage pipeline (alive → port-scan → plugin identify → credential spray).
- 26 protocol plugins across 7 categories (database, email, file-storage,
  messaging, network, remote, web).
- Bubbletea TUI dashboard with LIVE EVENTS column and status bar.
- bbolt project workspaces with resume support.
- Multi-format output (TXT, NDJSON, creds, RDP JSON+TXT).
- Multi-platform cross-build (5 OS/arch targets via justfile).
- Garble obfuscation + UPX compression pipeline.
- Per-target throttling, drain-on-shutdown, context-canceled goroutines.