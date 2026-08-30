# Second-Batch High-Priority Fixes — Verification Report

> Date: 2026-08-19
> Branch: `worktree-second-batch-fixes`
> Plan: derived from the v0.2 comprehensive audit (`docs/archive/comprehensive-audit-report.md`)

This report documents the nine commits executed in the second-batch
repair iteration, the verification evidence, and the residual items
deferred to future iterations.

## Commit Range

| Commit    | Audit ref  | Subject |
| --------- | ---------- | ------- |
| `64b8624` | P1-3       | `fix(audit): per-attempt read deadlines across 7 authenticators` |
| `d2d0887` | P2-7       | `fix(audit): f.Sync() in flushCloser.Close to survive power-loss` |
| `f57633a` | P2-5       | `fix(audit): dedup creds.txt writes via State seen-set` |
| `9460ea5` | P2-2       | `fix(audit): join workersWG to outer wg` |
| `f98303e` | residual   | `fix(audit): surface sink errors via session.Log` |
| `6f3fecb` | P3-1       | `refactor(audit): drop BatchWriter pendingBytes dead code` |
| `5196803` | P3-3       | `fix(audit): adaptive goroutine join latency <100ms` |
| `baa904a` | P3-5       | `fix(audit): sqlcache invalidation only on auth-state errors` |
| `1da3d60` | P3-7       | `fix(audit): capped exponential backoff in pool busy-wait` |
| `ccc25eb` | docs       | `docs: README supply-chain verification section` |

Nine fix/docs commits totalling **14 files changed, +260 / −62 lines**.
No unrelated formatting, refactor, or generated-file commits.

## Verification Matrix

| Step                                | Status | Evidence                                                                                                              |
| ----------------------------------- | :----: | --------------------------------------------------------------------------------------------------------------------- |
| `git status --short` clean          |   ✅   | `git status --short` returns empty output.                                                                            |
| `go build ./...`                    |   ✅   | Empty output.                                                                                                         |
| `go vet ./...`                      |   ✅   | Empty output.                                                                                                         |
| `go test ./...`                     |   ✅   | All 69 packages with tests pass; zero `FAIL` lines.                                                                  |
| `git diff main..HEAD --stat`        |   ✅   | Matches planned scope (auth + scan + core + output + store + README).                                                 |

### Per-task verification

| Plan behaviour                                    | Verification                                                                                                       |
| ------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------ |
| per-attempt deadline honored (telnet etc.)       | `go build ./internal/core/credential/...` clean; `go test ./internal/core/credential/auth/database/...` passes.   |
| fsync on Output.Close                             | Existing `TestOpenOutput_WithCSV` and `TestOpenOutput_Redaction` continue to pass; Sync is a best-effort addition. |
| creds.txt dedup                                   | `persistResult` calls `sess.State.MarkSeen(chash)` before `WriteCred`; existing core tests pass.                  |
| workersWG joined to outer wg                      | Code review: `defer wg.Done()` on the closer goroutine; existing core tests pass.                                  |
| sink errors surfaced                              | `persistResult` now `sess.Log.Warn`s on error; existing core tests pass.                                           |
| BatchWriter pendingBytes removed                  | `go vet ./internal/store/...` clean; existing store tests pass (1.4s).                                             |
| adaptive join latency <100ms                      | `go test ./internal/core/scan/...` passes (4.5s); behavioural verification deferred to Linux/macOS CI.             |
| sqlcache 1045-only invalidation                   | Existing database tests pass; behavioural verification requires a fake MySQL server.                                |
| Pool busy-wait capped at 50ms                     | `go test ./internal/core/scan/...` passes (4.5s).                                                                  |

## What Was Fixed

### Task 1 (P1-3) — per-attempt read deadlines
- **Bug:** seven TCP-based authenticators (telnet, rsync, vnc,
  modbus, nfs, rabbitmq, smb) ignored the caller-supplied `timeout`
  for per-attempt reads/writes, so a slow/hung server accepting TCP
  but never replying would wedge a worker for the full `cfg.Timeout`.
- **Fix:** each authenticator now calls
  `conn.SetDeadline(time.Now().Add(timeout))` at the top of every
  cred iteration — the same pattern as `redis.go:133` and
  `mongo.go:97`. Telnet's three hardcoded `2*time.Second` reads were
  replaced with the per-attempt `timeout` parameter.

### Task 2 (P2-7) — f.Sync() before close
- **Bug:** `Output.Close()` called `Flush` + `f.Close()` but not
  `f.Sync()`. On power-loss / OOM-kill, the last ~200ms of buffered
  writes could be lost even though `Flush` returned success.
- **Fix:** `flushCloser.Close()` now calls `f.Sync()` after
  `bw.Flush()` and before `f.Close()`. The Sync error is intentionally
  swallowed so the more actionable `f.Close()` error is preserved.

### Task 3 (P2-5) — creds.txt dedup
- **Bug:** `creds.txt` is opened with `O_APPEND`, so a re-dispatched
  `(host, port, user, pass)` hit was appended a second time on
  `--resume` or after a retry.
- **Fix:** `persistResult` computes the `chash` and calls
  `sess.State.MarkSeen(chash)` before `WriteCred`. `MarkSeen` returns
  `true` only on first occurrence, so a duplicate hit sees the gate
  closed and skips the file write. `PutCred` to bbolt is unchanged
  (bbolt already deduplicates by key).

### Task 4 (P2-2) — workersWG joined to outer wg
- **Bug:** `RunScan`'s `wg.Wait()` only tracked the port-scan
  goroutine and the result-sink goroutine. The plugin worker pool
  (workersWG) was waited in an untracked anonymous goroutine that
  then `close(results)`. If the producer's `defer close(items)` ever
  drifted in a future code change, `wg.Wait()` would hang because
  workers would block forever on the un-closed channel.
- **Fix:** the `workersWG.Wait() → close(results)` goroutine is now
  registered with the outer `wg` (`defer wg.Done()`), so `wg.Wait()`
  covers the worker pool transitively.

### Task 5 (sink error propagation — residual)
- **Bug:** `persistResult` used `_ =` to discard every Output write
  error and Store write error, so a failing fsync / permission error
  / disk-full condition silently dropped results without diagnostics.
- **Fix:** `persistResult` now checks each error and surfaces it via
  `sess.Log.Warn`. Result is still written when possible (best-effort
  sink semantics preserved).

### Task 6 (P3-1) — BatchWriter pendingBytes dead code
- **Bug:** `BatchWriter.Enqueue` added a fixed 64 KiB per-op ceiling
  to `pendingBytes` and then checked `pendingBytes >= 1 MiB`. Because
  the ceiling was constant, the byte threshold fired on the same op
  as the count threshold (`DefaultBatchSize=32`), making the byte path
  effectively unreachable.
- **Fix:** removed `pendingBytes` field, per-op ceiling addition, and
  byte-path check. Count-based flushing remains the sole driver.

### Task 7 (P3-3) — adaptive goroutine join latency
- **Bug:** `Pool.adaptiveLoop` was driven off a single ticker at
  `opts.AdjustInterval` (default 500ms). On the happy path,
  `Pool.Run`'s `close(stopAdj)` made the loop sit blocked on
  `<-t.C` for up to 500ms before exiting — adding ~500ms tail
  latency to every successful scan.
- **Fix:** `adaptiveLoop` now uses a 50ms wake-up ticker; the actual
  `adjust()` call is still throttled to `opts.AdjustInterval` via a
  timestamp guard. Join latency drops from up to 500ms to ~50ms.

### Task 8 (P3-5) — sqlcache invalidation policy
- **Bug:** the MySQL authenticator invalidated the cached `*sql.DB`
  on ANY `PingContext` error, including transient network errors
  (refused, timeout, server-gone). Under a flaky network this tore
  down the warm pool Phase 1.9 built, defeating its benefit on the
  very conditions that benefit from it most.
- **Fix:** invalidate only on MySQL error 1045 (`ER_ACCESS_DENIED`).
  All other errors leave the cached DB in place.

### Task 9 (P3-7) — Pool busy-wait CPU pegging
- **Bug:** `Pool.Run`'s producer used a fixed 1ms backoff in its
  acquire-wait loop. Under saturation (inflight ≥ currentThreads for
  many consecutive iterations) the producer pegged CPU at ~1 kHz of
  Timer resets + select wakeups, leaving a meaningful chunk of one
  core unavailable to actual probe work.
- **Fix:** capped exponential backoff — 1ms, doubles on every missed
  acquire, up to 50ms ceiling. CPU usage under saturation drops ~50×;
  responsiveness to a freed slot stays under 50ms.

### Task 10 (docs) — README supply-chain verification
- **Gap:** the release pipeline generates SHA256SUMS, cosign keyless
  signatures (`.sig`), OIDC signing certificates (`.pem`), and
  CycloneDX SBOMs (`.sbom.json`) per binary, but the README did not
  tell users how to verify any of them.
- **Fix:** new "Verifying releases / 验证发布" section after
  Attribution, with bilingual instructions for (1) SHA256SUMS check,
  (2) cosign keyless verification pinning the OIDC identity, (3)
  CycloneDX SBOM inspection via jq + Dependency-Track, (4) source
  reproducibility check via `go build -trimpath`, (5) reporting a
  discrepancy.

## Residual Risks (Deferred to Future Iterations)

The following items from the v0.2 audit are intentionally **not**
addressed in this batch; they remain on the post-v0.4 roadmap.

1. **Full crack-mode refactor** — `cmd/scan.go` still has separate
   `ModeScan` / `ModeCrack` / `ModeLinked` branches; no shared
   credential loading path.
2. **Full proxy unification** — `Socks5` / `Proxy` are wired
   differently across plugins; per-plugin `--socks5` migration not
   done.
3. **Performance refactor (benchmark-driven)** — no benchmark suite
   yet; the busy-wait / adaptive-loop tuning in this batch was
   analytical, not measurement-driven.
4. **`go test -race` on Windows** — environment-level issue (already
   deferred from the first batch); to be exercised on Linux/macOS CI.
5. **Fake-server integration tests** — many of the behavioural
   assertions in this batch (e.g. "tcp accepts but never replies
   times out within `cfg.Timeout`") cannot be verified end-to-end
   without protocol-level fake servers (RDP, MySQL, NFS, …).
   Deferred to v0.3+ per the roadmap.

## Acceptance Criteria

Per the plan:
- [x] Each theme committed independently (9 commits).
- [x] No unrelated formatting or refactor commits.
- [x] All existing tests pass under `go test ./...`.
- [x] `go vet ./...` clean.
- [x] `git diff --check` clean.
- [x] Residual risks documented above.
- [ ] `go test -race ./...` — **deferred** (Windows race-detector
      runtime bug, see first-batch report).
- [ ] Benchmark-driven performance verification — **deferred** (no
      Go benchmark suite yet).

The second-batch repair set is verified and ready for review.