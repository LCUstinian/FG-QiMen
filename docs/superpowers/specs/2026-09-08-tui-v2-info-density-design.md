# TUI v2 — Information Density (Spec A of 3) — Design Spec

**Date:** 2026-09-08
**Author:** Claude (brainstorming session with LCUstinian)
**Target release:** v0.5.2 (deferred — see "Shipped" note below)
**Branch:** `main`
**Scope:** Add 5 information-density features to the TUI dashboard: stage indicator, real-time rate, ETA, top-plugins bar chart, error-category breakdown.

---

## Shipped status (2026-09-10)

**All 5 spec features + 5 design optimisations are implemented and on
`main`** (commits `9825a3e`, `c4f8651`, `bcc0abe`, `51e57e9`, `060e2fa`,
`f096096`, `2a909c9`). The v0.5.2 tag is **deliberately not cut** —
per user direction to iterate slowly on version numbers, the
in-source `version.Value` default was bumped to `0.5.1-dev` instead,
and the changelog entries live under `[Unreleased]`. The next release
tag will roll these up.

**5 spec features:** ✅ stage indicator, ✅ real-time rate (EWMA), ✅
per-stage ETA, ✅ top-plugins bar chart, ✅ error-category breakdown.
**5 design optimisations:** ✅ EWMA rate smoothing, ✅
`errors.Is`/`errors.As`-first classifier with substring fallback,
✅ empty-state placeholders, ✅ plugin-name normalisation at scanner
write site, ✅ 256-entry memory cap on `PluginHits` / `ErrorCategories`.

Drift from spec: none material. The `256-entry` cap is enforced via
`sync.Map` write side; mid-run reordering of plugin names is done at
the scanner dispatch layer (per spec) and not in the TUI render layer.
The mid-alive-sweep counter stuck-at-zero bug (`060e2fa`) is fixed via
`alive.Progress()` as the public API surface, matching the spec's
"external callers use the public API" intent.

---

## 1. Background

After v0.5.1 shipped with the "TUI counter frozen during alive sweep" fix (commits `f096096` and `060e2fa`), the operator's at-a-glance visibility is better but still thin: only the final counters (alive / ports / results / creds / errors) plus a per-second ticker for elapsed. Operators have no way to know:

- Which scan **stage** is currently running (alive / port-scan / identify / cred-spray / done).
- The **rate** of work — am I making progress or stuck?
- **ETA** — how long until this scan finishes?
- Which **plugins** are most active — useful for tuning targets / wordlists.
- **What kinds of errors** are dominating — connection refused vs timeout vs DNS fail tells very different stories.

This spec (Spec A of three, the user chose "complete Spec A — 5 features") adds those five features. Spec B (panel layout rework) and Spec C (visual polish — colors, animations) are deferred to subsequent spec cycles.

---

## 2. Goals

1. **Stage indicator** — a single-glance badge showing the current scan stage with a spinner.
2. **Real-time rate** — hits/sec, ports/sec computed from snapshot deltas over the 1Hz stats tick.
3. **ETA** — estimated time remaining, computed from elapsed time and probed/total ratio.
4. **Top-plugins bar chart** — top 5 plugin names by hit count, rendered as horizontal bars.
5. **Error-category breakdown** — top 5 error categories with counts, surface so operators can spot dominant failure modes (timeouts vs DNS vs auth-fail).

## 3. Non-goals

- ❌ Interactive filtering / search / drill-down (the user explicitly excluded).
- ❌ Per-plugin credential-success rate (Spec B+ material).
- ❌ Network bandwidth / throughput display.
- ❌ Worker-utilization display (no instrumentation today; would require goroutine metrics).
- ❌ Color/animation polish (Spec C).

---

## 4. Architecture

### 4.1 Data model changes (`internal/types/state.go`)

Add to `State`:

```go
// Stage constants — atomic.Int32 in State tracks which scan phase is
// currently running. / Stage 常量——State 用 atomic.Int32 追踪当
// 前运行的扫描阶段。
const (
    StageIdle     int32 = 0
    StageAlive    int32 = 1
    StagePortScan int32 = 2
    StageIdentify int32 = 3
    StageCred     int32 = 4
    StageDone     int32 = 5
)

// StageName returns a short human label for a stage value.
// / StageName 返回阶段值的短人类标签。
func StageName(stage int32) string {
    switch stage {
    case StageAlive: return "ALIVE"
    case StagePortScan: return "PORT-SCAN"
    case StageIdentify: return "IDENTIFY"
    case StageCred: return "CRED"
    case StageDone: return "DONE"
    default: return "IDLE"
    }
}

type State struct {
    seen     sync.Map
    Counters Counters
    StartTime time.Time
    
    // v0.5.2 additions for TUI information density.
    Stage           atomic.Int32   // current scan stage
    TotalHosts      atomic.Int64   // target count captured at scan start (for ETA)
    TotalPorts      atomic.Int64   // total port-scan items (for ETA)
    PluginHits      sync.Map       // plugin-name string → *atomic.Int64 hit count
    ErrorCategories sync.Map       // category string → *atomic.Int64 error count
}

// Snapshot returns the plain-int64 view. Existing fields plus
// Stage int64 for the dashboard.
// / Snapshot 返回纯 int64 视图。现有字段加上 Stage int64。
func (s *State) Snapshot() CountersView {
    return CountersView{
        // ... existing fields ...
        Stage: int64(s.Stage.Load()),
    }
}

// PluginHitsView returns a snapshot of plugin-name → hit count as a
// plain map (atomic load per entry). Read-only; safe to call from
// TUI's stats handler. / PluginHitsView 返回 plugin-name → hit count
// 的快照 map（每项 atomic load）。只读；可从 TUI 的 stats handler 安全调用。
func (s *State) PluginHitsView() map[string]int64 {
    out := make(map[string]int64)
    s.PluginHits.Range(func(k, v any) bool {
        if c, ok := v.(*atomic.Int64); ok {
            out[k.(string)] = c.Load()
        }
        return true
    })
    return out
}

// ErrorCategoriesView returns a snapshot of category → count as a
// plain map (atomic load per entry). / ErrorCategoriesView 返回
// category → count 快照 map（每项 atomic load）。
func (s *State) ErrorCategoriesView() map[string]int64 {
    out := make(map[string]int64)
    s.ErrorCategories.Range(func(k, v any) bool {
        if c, ok := v.(*atomic.Int64); ok {
            out[k.(string)] = c.Load()
        }
        return true
    })
    return out
}
```

`CountersView` gets a `Stage int64` field. Existing `Snapshot()` callers are unaffected (default zero is fine).

### 4.2 Error classifier (`internal/core/errors.go`, new file)

```go
package core

import (
    "context"
    "errors"
    "net"
    "strings"
    "syscall"
)

// ClassifyError maps an error from plugin execution to a stable
// category label suitable for grouping in the TUI. Categories are
// intentionally coarse — finer sub-classes can be added later
// without changing the aggregation shape.
// / ClassifyError 把插件执行错误映射到 TUI 分组用的稳定类别标签。
// 类别故意粗——后续可加更细的子类而不改聚合 shape。
func ClassifyError(err error) string {
    if err == nil {
        return ""
    }
    // Specific sentinel checks first / 先检查特定 sentinel。
    if errors.Is(err, context.DeadlineExceeded) {
        return "timeout"
    }
    var netErr net.Error
    if errors.As(err, &netErr) && netErr.Timeout() {
        return "timeout"
    }
    // OS-level errno sentinel unwrap (Linux/Darwin/Windows).
    // / 系统级 errno sentinel 解包（Linux/Darwin/Windows）。
    if errors.Is(err, syscall.ECONNREFUSED) {
        return "refused"
    }
    if errors.Is(err, syscall.ECONNRESET) {
        return "reset"
    }
    if errors.Is(err, syscall.EHOSTUNREACH) || errors.Is(err, syscall.ENETUNREACH) {
        return "unreach"
    }
    // Fallback: substring match on lowercased error text.
    // / 回退：在小写错误文本上做子串匹配。
    s := strings.ToLower(err.Error())
    switch {
    case strings.Contains(s, "timeout"), strings.Contains(s, "i/o timeout"):
        return "timeout"
    case strings.Contains(s, "refused"):
        return "refused"
    case strings.Contains(s, "reset"):
        return "reset"
    case strings.Contains(s, "no such host"),
        strings.Contains(s, "dns"):
        return "dns"
    case strings.Contains(s, "permission"),
        strings.Contains(s, "denied"):
        return "perm"
    case strings.Contains(s, "auth"):
        return "auth"
    case strings.Contains(s, "tls"),
        strings.Contains(s, "ssl"),
        strings.Contains(s, "handshake"):
        return "tls"
    case strings.Contains(s, "unreachable"), strings.Contains(s, "no route"):
        return "unreach"
    }
    return "other"
}
```

### 4.3 scanner.go wiring (`internal/core/scanner.go`)

Set `Stage` and totals at each transition; increment `PluginHits` and `ErrorCategories` in the worker dispatch loop:

```go
func RunScan(ctx context.Context, sess *session.Session) (int, error) {
    // ...existing pre-pipeline setup...

    sess.State.TotalHosts.Store(int64(len(targets)))
    // Estimate total ports: average of all plugin port lists. Cheap
    // approximation; exact value isn't critical for ETA.
    sess.State.TotalPorts.Store(int64(len(targets)) * 100) // rough

    sess.State.Stage.Store(types.StageAlive)
    aliveRes, _ := aliveDiscovery.Run(ctx, targetAddrs(targets))
    sess.State.Stage.Store(types.StagePortScan)
    // ...port scan loop...
    sess.State.Stage.Store(types.StageIdentify)

    for w := 0; w < len(workers); w++ {
        go func() {
            for item := range workCh {
                sess.State.AliveProbed.Store(...)
                hit, err := plugin.Identify(...)
                if err != nil {
                    cat := ClassifyError(err)
                    bumpSyncMap(&sess.State.ErrorCategories, cat)
                    continue
                }
                if hit != nil {
                    bumpSyncMap(&sess.State.PluginHits, hit.Service)
                }
            }
        }()
    }
    sess.State.Stage.Store(types.StageCred)
    // ...cred spray...
    sess.State.Stage.Store(types.StageDone)
}

// bumpSyncMap increments a *atomic.Int64 stored under key in a sync.Map.
// Creates the entry if absent. / bumpSyncMap 递增 sync.Map 中 key 下
// 存储的 *atomic.Int64。如果不存在则创建。
func bumpSyncMap(m *sync.Map, key string) {
    v, _ := m.LoadOrStore(key, &atomic.Int64{})
    if c, ok := v.(*atomic.Int64); ok {
        c.Add(1)
    }
}
```

### 4.4 TUI rendering (`internal/tui/tui.go`)

Add three new render sections + one new background ticker state:

```go
type model struct {
    // ...existing fields...
    
    // v0.5.2 additions
    rateHits    float64       // hits/sec from previous snapshot delta
    ratePorts   float64       // ports/sec from previous snapshot delta
    lastSnapAt  time.Time     // for rate delta
    lastSnap    CountersView  // previous snapshot for delta computation
    eta         string        // formatted ETA or "" if unknown
    topPlugins  [][2]string   // top 5 plugin name → count, sorted desc
    topErrors   [][2]string   // top 5 category → count, sorted desc
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    switch msg := msg.(type) {
    case statsMsg:
        now := time.Now()
        if !m.lastSnapAt.IsZero() {
            dt := now.Sub(m.lastSnapAt).Seconds()
            if dt > 0 {
                dHits := msg.view.Creds - m.lastSnap.Creds
                dPorts := msg.view.Ports - m.lastSnap.Ports
                if dHits < 0 { dHits = 0 }
                if dPorts < 0 { dPorts = 0 }
                m.rateHits = float64(dHits) / dt
                m.ratePorts = float64(dPorts) / dt
            }
        }
        // ETA: only meaningful during alive + port-scan phases.
        switch msg.view.Stage {
        case int64(types.StageAlive), int64(types.StagePortScan):
            m.eta = formatETA(m.lastSnapAt, now,
                msg.view.AliveProbed,
                m.lastSnap.TotalHosts, // would need to add
                msg.view.TotalHosts)
        default:
            m.eta = ""
        }
        // Top plugins / errors from State.
        sess := m.session() // access session via accessor
        m.topPlugins = topN(sess.PluginHitsView(), 5)
        m.topErrors = topN(sess.ErrorCategoriesView(), 5)
        m.lastSnap = msg.view
        m.lastSnapAt = now
        // ...existing model counters update...
    }
}
```

Render additions (in the View function):

```
[ ALIVE  ▶  ]                                          elapsed 18s   ETA ~22s
─────────────────────────────────────────────────────────────────────────
rate: 12.3 hits/s    ports: 84.1/s    probed 142 / 256
─────────────────────────────────────────────────────────────────────────
TOP PLUGINS                TOP ERRORS
ssh    ███████████  47     timeout  ██████  42
redis  ████         19     refused  ██      15
mysql  ██           12     reset    █        8
                              dns     ░        2
                              other   ░        3
─────────────────────────────────────────────────────────────────────────
spinner alive=42 probed=142 ports=84 results=23 creds=2 errors=3
...existing live events list...
```

Concrete format strings are detailed in Section 5 of the implementation plan.

### 4.5 Helper functions

```go
// rateTracker keeps a 5-tick sliding window of snapshot counters
// and computes smoothed per-second rates. EWMA with α=0.5 gives
// faster decay than pure mean, so a brief burst still shows up
// quickly. / rateTracker 保留 5 tick 的滑动窗口并计算平滑的每秒
// 速率。α=0.5 EWMA 衰减比纯均值快，瞬时突增仍能迅速体现。
type rateTracker struct {
    lastHits  int64
    lastPorts int64
    lastAt    time.Time
    emaHits   float64
    emaPorts  float64
}

func (r *rateTracker) update(now time.Time, hits, ports int64) (rateHits, ratePorts float64) {
    if !r.lastAt.IsZero() {
        dt := now.Sub(r.lastAt).Seconds()
        if dt > 0 {
            dH := float64(hits - r.lastHits)
            dP := float64(ports - r.lastPorts)
            if dH < 0 { dH = 0 } // counter reset or wrap — treat as 0
            if dP < 0 { dP = 0 }
            instHits := dH / dt
            instPorts := dP / dt
            const alpha = 0.5
            r.emaHits = alpha*instHits + (1-alpha)*r.emaHits
            r.emaPorts = alpha*instPorts + (1-alpha)*r.emaPorts
        }
    }
    r.lastHits = hits
    r.lastPorts = ports
    r.lastAt = now
    return r.emaHits, r.emaPorts
}

// stageETASpec tells the ETA calculator which (done, total) pair to
// use based on the current stage — different stages have different
// denominators. / stageETASpec 告诉 ETA 计算器基于当前阶段用哪对
// (done, total) 计数。不同阶段的分母不同。
type stageETASpec struct {
    useAliveRatio bool   // alive/port-scan phases
    usePortRatio  bool   // port-scan / identify phases
    useHitRate    bool   // identify phase (estimated via hits/sec)
}

func computeETA(start, now time.Time, stage int32, view CountersView) string {
    elapsed := now.Sub(start).Seconds()
    if elapsed <= 0 {
        return ""
    }
    switch int(stage) {
    case int(types.StageAlive):
        if view.AliveProbed > 0 && view.TotalHosts > 0 && view.AliveProbed < view.TotalHosts {
            rem := elapsed * float64(view.TotalHosts-view.AliveProbed) / float64(view.AliveProbed)
            return fmt.Sprintf("~%ds", int(rem))
        }
    case int(types.StagePortScan), int(types.StageIdentify):
        if view.Ports > 0 && view.TotalPorts > 0 && view.Ports < view.TotalPorts {
            rem := elapsed * float64(view.TotalPorts-view.Ports) / float64(view.Ports)
            return fmt.Sprintf("~%ds", int(rem))
        }
    }
    return "" // cred stage + done stage: ETA undefined
}

// topN returns the top n (key, count) pairs from m sorted by count desc.
// Returns slice of [name, countString] tuples for TUI rendering.
// / topN 从 m 取计数前 n 名，返回排序好的 [name, countString] 对。
func topN(m map[string]int64, n int) [][2]string {
    type kv struct{ k string; v int64 }
    var all []kv
    for k, v := range m {
        if v <= 0 { continue } // skip empty buckets
        all = append(all, kv{k, v})
    }
    sort.Slice(all, func(i, j int) bool { return all[i].v > all[j].v })
    if len(all) == 0 {
        return nil
    }
    out := make([][][2]string, 0, n)
    for i := 0; i < len(all) && i < n; i++ {
        out = append(out, [2]string{all[i].k, fmt.Sprintf("%d", all[i].v)})
    }
    return out
}

// normalisePluginName lowercases and strips version-like suffixes
// so the same plugin doesn't show up as 3 rows ("ssh", "SSH",
// "ssh/2.0"). Called at the scanner.go dispatch write site, not
// in TUI render.
// / normalisePluginName 小写化并去掉版本后缀，让同一个 plugin
// 不以 3 行显示。在 scanner.go dispatch 写入点调用，TUI 渲染不调。
func normalisePluginName(name string) string {
    n := strings.ToLower(strings.TrimSpace(name))
    // Strip trailing "/x.y" or "-x.y" suffixes (e.g. "ssh/2.0" -> "ssh").
    // / 去掉尾部 "/x.y" 或 "-x.y" 后缀。
    for _, sep := range []string{"/", "-"} {
        if i := strings.Index(n, sep); i >= 0 {
            tail := n[i+1:]
            // Only strip if tail looks like a version: starts with digit.
            // / 只在 tail 像版本号（以数字开头）时去掉。
            if len(tail) > 0 && tail[0] >= '0' && tail[0] <= '9' {
                n = n[:i]
            }
        }
    }
    return n
}

// bar renders a fixed-width horizontal bar of width w filled to ratio.
// Uses `█` for filled, `░` for empty. / bar 渲染固定宽度的横向 bar，
// 按 ratio 填充。█ 实心，░ 空心。
func bar(ratio float64, w int) string {
    if ratio < 0 { ratio = 0 }
    if ratio > 1 { ratio = 1 }
    filled := int(float64(w) * ratio)
    return strings.Repeat("█", filled) + strings.Repeat("░", w-filled)
}
```

### 4.6 Empty-state placeholders

TUI panels must render meaningful placeholders when their backing map is empty, so the dashboard doesn't look broken during early scan / sparse scans:

```go
// In TUI View:
//   if len(m.topPlugins) == 0 { fmt.Fprintln(&b, "  (no hits yet)") }
//   if len(m.topErrors)  == 0 { fmt.Fprintln(&b, "  (no errors yet)") }
```

### 4.7 Memory bounds

PluginHits and ErrorCategories can theoretically grow unbounded if plugins emit a huge variety of service names. Cap each map at 256 entries; oldest evicted by recency (LRU not implemented in v0.5.2 — cap is hard, oldest silently dropped). Realistic max plugin count in FG-QiMen is ~50, so the cap is defensive only.

```go
const maxPluginHitsEntries = 256 // defensive cap; real max is ~50 plugins

// bumpSyncMap with cap: skip increment if cap reached (avoids leaking
// goroutine-local garbage strings).
// / bumpSyncMap 带上限：达上限跳过递增（避免泄漏 goroutine-local
// 垃圾字符串）。
func bumpSyncMap(m *sync.Map, key string, max int) {
    // Fast path: existing entry.
    if v, ok := m.Load(key); ok {
        if c, ok := v.(*atomic.Int64); ok { c.Add(1) }
        return
    }
    // Cap check: count current entries before adding new key.
    // / 上限检查：加新 key 前先数当前条目。
    count := 0
    m.Range(func(_, _ any) bool { count++; return true })
    if count >= max {
        return // cap reached — drop the increment silently
    }
    v, _ := m.LoadOrStore(key, &atomic.Int64{})
    if c, ok := v.(*atomic.Int64); ok { c.Add(1) }
}
```

---

## 5. File impact summary

**Modify:**
- `internal/types/state.go` — add Stage constants + State.Stage/TotalHosts/TotalPorts/PluginHits/ErrorCategories; extend Snapshot; add PluginHitsView/ErrorCategoriesView; extend CountersView with Stage.
- `internal/core/scanner.go` — set Stage at transitions; capture TotalHosts; bump PluginHits on identify hit; bump ErrorCategories on identify error.
- `internal/core/errors.go` — NEW: ClassifyError function.
- `internal/tui/tui.go` — add rate/ETA/plugin/error tracking to model; render new panels in View.
- `internal/tui/program.go` — pass `*session.Session` through to the model so it can call PluginHitsView (or pass views through statsMsg).

**Create:**
- `internal/core/errors.go` — classifier.
- `internal/core/errors_test.go` — TestClassifyError unit tests.
- `internal/core/scanner_test.go` (extend existing) — TestScanner_TracksStageAndPluginHits (uses aliveProbesFn seam from prior work).
- `internal/tui/program_test.go` (extend existing) — TestDispatcher_RendersRateAndPlugins.

**Not modified:**
- Any non-test file outside internal/types, internal/core, internal/tui.
- Existing plugin code (just emits results/errors as before).

---

## 6. Acceptance criteria

1. `go test -race ./...` green (Windows race-detector DLL known issue excepted).
2. `internal/core/errors_test.go::TestClassifyError` covers each category:
   - `nil`, `context.DeadlineExceeded`, `&net.OpError{Op: "dial", Err: timeout}` → "timeout"
   - `syscall.ECONNREFUSED`, `errors.New("connection refused")` → "refused"
   - `syscall.ECONNRESET`, `errors.New("connection reset by peer")` → "reset"
   - `errors.New("no such host")`, `errors.New("dns lookup failed")` → "dns"
   - `errors.New("permission denied")`, `errors.New("auth failed")` → "perm" / "auth"
   - `errors.New("tls handshake failure")` → "tls"
   - `errors.New("no route to host")`, `syscall.EHOSTUNREACH` → "unreach"
   - Anything else → "other"
3. `internal/core/scanner_test.go::TestScanner_TracksStageAndPluginHits` PASS:
   - Stage.Store transitions verifiable via Snapshot.Stage.
   - PluginHits contains entries normalised to lowercase with version suffix stripped ("SSH/2.0" → "ssh").
   - ErrorCategories contains expected entries on injected errors.
   - PluginHits capped at 256 (defensive); entries beyond cap silently dropped.
4. `internal/tui/program_test.go::TestDispatcher_RendersRateAndPlugins` PASS:
   - After 6 statsMsg deltas, rate fields stabilise (not jumpy single-tick values).
   - topPlugins / topErrors populated from State.PluginHitsView/ErrorCategoriesView.
   - Empty maps render placeholders, not blank panels.
   - Per-stage ETA: alive uses probed/total ratio; port-scan/identify uses ports/totalPorts; cred/done empty.
5. Running the binary against `/24` real targets (operator smoke test):
   - Dashboard shows `[ ALIVE ▶ ]` then `[ PORT-SCAN ▶ ]` then `[ IDENTIFY ▶ ]` then `[ DONE ✓ ]`.
   - Rate row ticks each second with smoothed (not jumpy) values once work is happening.
   - Top plugins bar chart shows >= 1 entry after first plugin identifies a hit.
   - Error categories line shows non-zero counts once connection attempts fail.
   - ETA shows reasonable value during alive and port-scan phases; empty during cred/done.

---

## 7. Risks & mitigations

| Risk | Likelihood | Mitigation |
|---|---|---|
| `sync.Map` of `*atomic.Int64` overhead is high | low | PluginHits has at most ~50 plugins; sync.Map scales fine. Snapshot() reads once per second (TUI 1Hz). |
| `ClassifyError` string match is too coarse | medium | Coarse buckets are fine for v0.5.2 grouping; refinement can land later. |
| Stage transition racing with TUI render | medium | Stage.Load is atomic; worst case is one render showing the previous stage, which is acceptable. |
| `Session` passed through bubbletea message type complicates dispatcher | medium | Alternative: pass plugin/error views through `statsMsg` itself rather than model holding Session. Resolve in plan phase. |
| Plugin name in `Hit.Service` not consistent across plugins | medium | Audit plugin names; normalise to lowercase / strip version suffix in ClassifyError-adjacent helper. |
| 5 features in one ship creates a big diff to review | medium | Split into commits: (a) state types, (b) scanner wiring, (c) error classifier, (d) TUI render. Each commits + passes tests. |

---

## 8. Open questions for the user

None — all scope decisions resolved in the brainstorming session (3 specs, 5 features in Spec A). Ready for `writing-plans`.
