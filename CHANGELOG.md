# Changelog

All notable changes to FG-QiMen are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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
