# Changelog

All notable changes to FG-QiMen are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added (v0.3.1 — audit roadmap)
- **Performance — bbolt batched writes**: new `store.BatchWriter` + `PutMany`
  amortise one fsync per result into one per batch of 32 ops or 200ms.
  Wired through the pipeline via `sess.BatchWriter`. New `--no-batch`
  flag falls back to per-write semantics.
- **Performance — Output per-sink mutex split**: `Output` now uses 6
  per-sink mutexes (txt / json / creds / rdp.json / rdp.txt / csv) so a
  slow sink can't head-of-line block the others. `csv.Writer` is
  hoisted to a field to avoid per-row allocation.
- **Performance — `RawTCPIdentify` helper**: shared TCP-dial
  boilerplate in `internal/plugins/rawtcp.go`. 5 plugins (redis,
  memcached, postgresql, mongodb, socks5) refactored to use it.
- **Performance — HTTP Transport reuse**: elasticsearch and web
  plugins now use process-level `http.Client` instead of allocating
  a fresh `http.Transport` per Identify call.
- **Security — magic-byte AAD binding**: `store/crypto.go` binds the
  magic byte (0x02) to the ciphertext via GCM AAD. Bit-flips on the
  magic byte are detected as `ErrDecryptFailed` rather than silently
  converting an encrypted row to "plaintext".
- **Security — host flag injection guard**: alive `cmd` probe rejects
  host values starting with `-` (would be parsed as a `ping` flag).
- **Security — graceful resume degradation**: corrupt bbolt on
  `--resume` logs a warning and continues with an empty seen-set
  rather than aborting the scan.
- **Security — input validation**: `--ports 99999` / `--ports abc`
  now propagates the parse error to the user instead of silently
  running 0 ports.
- **CI — govulncheck + coverage**: new `govulncheck` job and coverage
  upload (Linux only) in `.github/workflows/ci.yml`. `go mod tidy`
  enforced as a CI gate.
- **CI — release workflow**: new `.github/workflows/release.yml`
  tag-triggered build with `cosign` keyless signing and CycloneDX
  SBOM generation.
- **Tests — FTP authenticator**: 4 new tests (NoCreds / Hit / MissAll /
  NotFTP) using an in-process fake FTP server.
- **Tests — store**: 5 new tests for `BatchWriter` (count-threshold,
  time-ticker, Stop-blocks-until-flush, PutOpSeen timestamp format,
  encrypted PutMany roundtrip) and a new test for magic-byte AAD
  tamper detection.
- **Tests — alive**: new test for the host flag-injection guard.
- **Tests — output**: new concurrent-writes test pinning the per-sink
  mutex contract.
- **Tests — plugins**: 4 new tests for the `RawTCPIdentify` helper.
- **TUI — NO_COLOR honoured**: text banner now respects `NO_COLOR=1`
  (previously only the in-TUI rendering respected it).
- **TUI — dead code removed**: unused `boxTL` / `boxBR` constants
  deleted.
- **Docs**: new `docs/SECURITY.md`, `docs/PLUGIN_GUIDE.md`,
  `docs/ARCHITECTURE.md`, `docs/CONFIGURATION.md`.

### Changed (v0.3.1 — audit roadmap)
- **`--show-creds` documentation clarified**: the flag applies to
  result.txt / result.json / result.csv; creds.txt is always
  cleartext by design (the operator's working file).
- **Pool dedup**: removed unused `Pool.Clear()` method (no
  production callers; Go strings cannot be reliably zeroed).
- **CodedError in `Validate()`**: 5/8 validation sites converted to
  `CodedError` with stable codes (E006 / E005 / E007) for
  grep-friendly log output. Remaining sites kept as-is for the
  next release.
- **SSH auth flow**: `conn.Close()` only on error path; success
  path delegates to `sshConn.Close()`. (No functional change —
  documents the existing intent.)

### Added (v0.3.0-rc1)
- **P0 — At-rest encryption for project DBs**: `internal/store/crypto.go` adds
  value-level AES-256-GCM encryption to `PutResult` / `PutCred`. New
  `--project-key` flag (and `FG_QIMEN_PROJECT_KEY` env var) opt-in. Plaintext
  v0.2.x bbolt files continue to read correctly (forward-compatible magic
  bytes).
- **P0 — Workspace encryption wiring**: `workspace.AsStoreWithKey` plumbs
  the derived 32-byte key into the `Store` constructor; the seen-set bucket
  stays plaintext (it stores only non-secret hashes).
- **P1 — CSV output**: new `--output-csv` flag writes RFC-4180 one-row-per-
  result CSV with a stable column order (`time,host,port,service,plugin,
  state,banner,user,pass`). Same redaction policy as `result.txt` /
  `result.json` — `creds.txt` and the ShowCleartext flag control plaintext
  vs. fingerprint rendering.
- **P1 — Stable error codes**: new `internal/types/errors.go` defines
  `CodedError` with stable 4-character codes (E001..E999). Wired into
  `workspace.ValidateProjectName` and `bolt.Open` failure paths. Codes are
  append-only and will not be renumbered.
- **P1 — Flag grouping**: 28 persistent flags now carry a `group` annotation
  rendered by a custom `usageTemplate` in `cmd/root.go` (Target, Workspace,
  Ports, Network, Concurrency, Credentials, Output, Behavior, Safety).
  Default alphabetical list is preserved below the group reference.
- **P1 — golangci-lint integration**: `.golangci.yml` (modeled on fscan's)
  enables errcheck, govet, staticcheck, revive, gocritic, gosec, errorlint,
  prealloc, misspell. CI workflow adds a `lint` job that runs `only-new-issues`
  so pre-existing warnings don't block the pipeline.
- **P0 — Test coverage**:
  - `internal/network/proxy`: **0% → 86.3%** (in-process fake SOCKS5 + HTTP
    CONNECT servers, 4-stage Validator against full-echo and real-HTTP
    targets, Manager singleton + global lifecycle).
  - `internal/store`: **N/A → 61.3%** (encryption round-trip, wrong-key,
    tampered-ciphertext, plaintext backcompat, persistence across reopen).
  - `internal/output`: **86.8% → 89.5%** (CSV header, row, cleartext gate,
    path-omitted no-op).
  - `internal/types`: **80.2% → 80.6%** + new `errors_test.go` (CodedError
    string, JSON, code-stability tripwire).
  - `cmd`: **47.4% → 50.0%** + new `flags_test.go` (every persistent flag
    has a group annotation; missing annotation fails the test).

### Changed
- `cmd/flags.go` flag count: **23 → 28** (added `project-key`, `output-csv`;
  reorganized existing flags into explicit groups).
- `cmd/root.go` adds `SetUsageTemplate` so `fg-qimen --help` renders the
  grouped reference above the canonical alphabetical list.
- `internal/output/output.go` `Output` struct gains a `csv *flushCloser`
  field and `csvHeaderWritten` bool; `OpenOutput` / `Close` / `Flush` /
  `WriteResult` updated to thread the new sink through.
- `internal/workspace/workspace.go` `AsStore` is a thin wrapper around
  the new `AsStoreWithKey`; behavior is unchanged when no key is provided.

### Tests
- New test files: `internal/store/crypto_test.go`, `internal/store/store_crypto_test.go`,
  `internal/output/csv_test.go`, `internal/types/errors_test.go`,
  `cmd/flags_test.go`, `internal/network/proxy/{manager,socks5,http,validator}_test.go`,
  `internal/network/proxy/testhelpers_test.go`.
- New total tests across these files: **~70 cases** (13 store + 6 output
  + 5 types + 4 flags + 32 proxy).

### Documentation
- `docs/audit-report-vs-fscan.md` — full audit of FG-QiMen vs fscan
  (multi-dimensional: architecture, features, performance, security,
  code quality, testing, documentation, CI, UX, extensibility).
- `docs/optimization-plan-comprehensive.md` — 10-theme optimization plan
  with v0.3 / v0.4 / v0.5 / v1.0 roadmap, ~62 person-day estimate, success
  metrics. Scope explicitly excludes vulnerability-exploitation work.

## [0.2.0] - 2026-06-15

Initial public release (audit fixes from v0.1).
See [docs/RELEASE_NOTES_v0.2.md](docs/RELEASE_NOTES_v0.2.md) for the full list.

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
