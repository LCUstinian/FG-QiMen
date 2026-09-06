# v0.6.0 fake-server + 80% 覆盖率 — Design Spec

**Date:** 2026-09-06
**Author:** Claude (brainstorming session with LCUstinian)
**Target release:** v0.6.0
**Branch:** `main`
**Scope:** In-process Go fake servers + Identify/Credential happy-path tests for 30+ adapted plugins, plus shared `internal/fakeserver/` helper package, plus coverage floor 60% → 80% + per-plugin floor 70%.
**Out of scope:** Windows race-detector workaround (Spec #2), CI workflow audit + fuzz cron (Spec #3), real-target E2E harness (Spec #4), audit pass (Spec #5), error/edge/timeout paths in plugins.

---

## 1. Background

After v0.5.1 shipped, the user pivoted from a feature-additive roadmap
(v0.6 UDP / v0.7 fake-server / v0.8 fingerprint / v0.9 distributed) to
a quality-focused push: "supporting features are enough; pursue 丝滑
no bugs." Concretely, three acceptance criteria were agreed:

1. **Coverage** climbs to **≥ 80% total** with a **per-plugin floor
   of ≥ 70%** in `internal/plugins/adapted/*`.
2. **E2E zero-crash** on real targets (handled by Spec #4).
3. **CI** stays fully green with no latent failures (handled by
   Spec #3).

This spec covers the first item — fake-server infrastructure plus
the 30+ plugin coverage push. The other two items have their own
specs and timeline slots in the 3-month quality program.

The existing codebase already has the right pattern seeds: the
`plugintest.Smoke(t, New())` helper in
[internal/plugins/plugintest/plugintest.go](internal/plugins/plugintest/plugintest.go)
gives every plugin a non-zero coverage baseline via a 10-line
`smoke_test.go`, and `ntp_test.go` / `dns_test.go` / `rdp_test.go`
demonstrate the per-plugin in-process fake-server pattern. The job
is to **scale that pattern to all 43 adapted plugins** with a
shared helper package so the per-plugin work stays mechanical
(~30-60 LOC per plugin) rather than reinventing listener plumbing
each time.

---

## 2. Goals

1. **Total project coverage ≥ 80%** — measured by `go test -cover`
   against the current 60.5% baseline; target gain ~20 points.
2. **Per-plugin coverage ≥ 70%** — enforced per package by an
   updated `scripts/ci-coverage-check.py` that knows how to walk
   `internal/plugins/adapted/*/`.
3. **Identify + Credential happy-path coverage** for every adapted
   plugin. The fake server is a *minimal* responder (banner /
   probe / auth success) sufficient for the plugin's
   `Identify(ctx, host, port)` and `Credential(ctx, host, port,
   user, pass)` to complete without error.
4. **No production-code changes** — fake servers live in test
   files (`*_test.go`) only; production plugin code is read but
   not modified.

## 3. Non-goals (explicit out-of-scope)

- ❌ **Error / edge / timeout paths** in plugin Identify / Credential
  (wrong cred, partial handshake, mid-stream disconnect). Deferred
  to a later polish pass after happy-path coverage is achieved.
- ❌ **Windows race-detector workaround** — Spec #2.
- ❌ **CI workflow audit + fuzz cron** — Spec #3.
- ❌ **Real-target E2E harness** — Spec #4. Will depend on this
  spec's `internal/fakeserver/` package, however.
- ❌ **Audit pass (govulncheck/gosec/latent bug sweep)** — Spec #5.
- ❌ **New CI jobs** — fake servers run inside the existing
  `ci.yml` test job's `go test ./...` invocation.
- ❌ **Plugin code refactors** — we add tests, we don't rewrite
  plugins to be more testable.

---

## 4. Architecture

### 4.1 Reuse the existing pattern

The reference pattern from
[internal/plugins/adapted/network/ntp/ntp_test.go:12-43](internal/plugins/adapted/network/ntp/ntp_test.go#L12)
already shows the right shape:

```go
func startFakeNTP(t *testing.T, stratum byte) (string, int) {
    t.Helper()
    addr, _ := net.ResolveUDPAddr("udp", "127.0.0.1:0")
    conn, _ := net.ListenUDP("udp", addr)
    t.Cleanup(func() { _ = conn.Close() })
    go func() { /* protocol handler loop */ }()
    return "127.0.0.1", conn.LocalAddr().(*net.UDPAddr).Port
}

func TestNTP_Hit(t *testing.T) {
    host, port := startFakeNTP(t, 2)
    auth := New()
    // … call auth.Identify(ctx, host, port) and assert a hit
}
```

We repeat this pattern for every plugin, but extract the listener /
cleanup boilerplate into a shared package so each plugin only writes
its protocol handler.

### 4.2 New package: `internal/fakeserver/`

```
internal/fakeserver/
  doc.go              // package overview, YAGNI rules
  tcp.go              // ListenLoop helper (net.Listen + goroutine + t.Cleanup)
  udp.go              // ListenUDPLoop same for UDP
  http.go             // StartHTTP wrapping httptest.NewServer
  bin.go              // Tiny binary protocol helpers (write magic + length-prefixed reply)
  fakeserver_test.go  // Self-tests of the helpers themselves
```

**Public surface** (sketch):

```go
// TCP
func ListenLoop(t *testing.T, handler func(net.Conn)) (host string, port int)
func ListenTLS(t *testing.T, handler func(net.Conn)) (host string, port int)  // optional, only if a plugin needs it

// UDP
func ListenUDPLoop(t *testing.T, handler func([]byte, *net.UDPAddr) []byte) (host string, port int)

// HTTP
func StartHTTP(t *testing.T, h http.Handler) (url string)
```

Each helper:
- Uses port `:0` so the OS picks a free port → no conflicts
- `t.Cleanup` closes the listener / server so the test doesn't leak
- Returns the bound address so the plugin test can target it

### 4.3 Per-plugin file shape

Every plugin ends up with one additional file (or replaces an existing
`smoke_test.go` if happy-path coverage makes the smoke baseline
redundant):

```go
// internal/plugins/adapted/database/redis/redis_test.go
package redis

import (
    "context"
    "testing"
    "time"

    "github.com/LCUstinian/FG-QiMen/internal/fakeserver"
    "github.com/LCUstinian/FG-QiMen/internal/types"
)

// startFakeRedis is the minimum protocol handler that makes the
// plugin's Identify + Credential return a hit. / 启动一个 in-process
// Redis 假服务器，仅响应足以让插件 Identify + Credential 返回命中的
// 最小字节。
func startFakeRedis(t *testing.T) (host string, port int) {
    t.Helper()
    return fakeserver.ListenLoop(t, func(c net.Conn) {
        // RESP: PING -> +PONG\r\n, AUTH <user> <pass> -> +OK\r\n
        // ...
    })
}

func TestRedis_IdentifyHit(t *testing.T) {
    host, port := startFakeRedis(t)
    p := New()
    ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
    defer cancel()
    r := p.Identify(ctx, host, port)
    if r == nil { t.Fatal("expected hit, got nil") }
    // assert r.Service == "redis" (or whatever Identify populates)
}

func TestRedis_CredentialHit(t *testing.T) {
    host, port := startFakeRedis(t)
    p := New()
    ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
    defer cancel()
    r := p.Credential(ctx, host, port, "admin", "admin")
    if r == nil { t.Fatal("expected credential hit, got nil") }
}
```

`Smoke(t, New())` from `plugintest` stays as the baseline for plugins
where the fake-server version isn't written yet; once a fake-server
test exists for a plugin, the smoke file can be removed (since the
fake-server test exercises more).

---

## 5. Tiered execution (43 plugins)

The 43 adapted plugins split into 4 tiers by fake-server complexity.
Sequencing Tier 1 → Tier 4 is the recommended path because each
tier is independently shippable: if we run out of time at any tier,
the higher tiers have already raised coverage.

| Tier | Plugins | # | Difficulty | Week |
|---|---|---|---|---|
| **1 — HTTP** | jenkins, kibana, weblogic, webtitle, elasticsearch, aws, azure, activemq, rabbitmq, rocketmq | 10 | low (`httptest`) | W1-2 |
| **2 — Text-line TCP** | redis, memcached, smtp, pop3, imap, mysql, postgresql, mongodb, ftp, nfs | 10 | medium (`bufio.Scanner`) | W3 |
| **3 — Stateful TCP** | ssh, smb, rdp, rdpnla, winrm, telnet, vnc, ipmi, kafka, mqtt, mssql, oracle, rsync | 13 | high (state machine) | W4-5 |
| **4 — UDP / simple** | dns (already has test), ntp (already has test), snmp, snmpv3, tftp, bacnet, modbus, ldap, socks5, docker | 10 | medium (`ListenUDP`) | W6 |

dns and ntp already have fake-server tests from prior work — they're
included in the table for tier-completeness but need no new work,
just verification that they meet the per-plugin ≥ 70% bar.

---

## 6. CI integration

- **Existing `ci.yml` test job** picks up the new tests automatically
  (`go test ./...` already runs them). Estimated added wall-clock:
  +30-60s (well within the 240s timeout on the existing job).
- **`scripts/ci-coverage-check.py`**:
  - Floor `60%` → `80%` (single global change)
  - New mode: walk `internal/plugins/adapted/*/` directories; for each
    plugin package, run `go test -cover`; fail the job if any
    package is below 70%.
  - Print a per-plugin table to make regressions obvious.
- **No new CI job added.** Everything runs in the existing `test`
  matrix on Linux/macOS/Windows. Windows coverage is best-effort
  because of the `STATUS_ENTRYPOINT_NOT_FOUND` race-detector issue
  (out of scope; Spec #2).

---

## 7. Acceptance criteria

- `go test -race -cover ./...` reports **total coverage ≥ 80%**.
- `scripts/ci-coverage-check.py` (updated) reports all 43 plugin
  packages at **≥ 70%**.
- Every plugin in `internal/plugins/adapted/*/` has a
  `Test{Name}_IdentifyHit` and `Test{Name}_CredentialHit` passing.
- No production-code (`*.go` outside `_test.go`) is modified.
- `gofmt -l .` is empty; `go vet ./...` clean.
- The `internal/fakeserver/` package has its own self-tests.
- CI's existing `test` + `coverage` jobs green on the wrap-up
  commit.

---

## 8. Risks & mitigations

| Risk | Likelihood | Mitigation |
|---|---|---|
| Tier 3 / Tier 4 take longer than 2 weeks combined | high | Define a "Tier 3 ≥ 80% complete + Tier 4 ≥ 50% complete" minimum bar; defer incomplete plugins to v0.6.1 polish |
| Fake-server protocol handler written wrong → false-positive green | medium | Each `fakeserver` helper gets a self-test that pokes the listener and asserts a basic byte response; if the helper lies, the self-test fails |
| Coverage floor jump from 60% → 80% trips CI on plugins that aren't ready | high | Roll out in two waves: first set floor to 70% (matching v0.5.1 intent), then bump to 80% once Tier 1-3 complete |
| A plugin's `Identify` is invasive (mutates shared state, blocks on global lock) → fake server can't actually exercise it | low | Reuse `plugintest.Smoke` for such plugins and exclude them from the per-plugin floor; document the exclusion |
| Plugin uses CGo / external binary | very low | mssql/oracle/kafka use go-mssqldb etc., pure Go; no CGo in `adapted/` |
| Existing CI job's 240s timeout exceeded | low | Each fake takes <100ms; 43 plugins × ~3 tests each × 100ms ≈ 13s added wall-clock |

---

## 9. Out-of-band but related

This spec only covers item (1) of the three quality-program goals.
The companion specs (Spec #2: Windows race, Spec #3: CI hardening,
Spec #4: E2E harness, Spec #5: audit pass) cover the rest. They
each get their own spec → plan → SDD cycle, sequenced to fit the
3-month budget.

The `internal/fakeserver/` package built here is the foundation
that Spec #4 (E2E harness) will reuse for unit-style assertions
about end-to-end scan behavior against fake servers.

---

## 10. Open questions for the user

None — all scope decisions resolved in the brainstorming session:
approach A (in-process Go fake), coverage granularity
"总 ≥ 80% + 每插件 ≥ 70%", depth "Identify + Credential happy
path", tier ordering "Tier 1 HTTP first". Ready for
`writing-plans`.
