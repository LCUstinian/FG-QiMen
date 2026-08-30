# Changelog

All notable changes to FG-QiMen are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

_No pending changes._

## [0.3.1] - 2026-08-19

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