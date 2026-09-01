# Changelog

All notable changes to FG-QiMen are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

_No pending changes._

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