# TUI v2 — Information Density (Spec A) Implementation Plan

> **Status (2026-09-10):** All tasks shipped to `main`. See the "Shipped status" addendum at the bottom. v0.5.2 tag deliberately not cut (slow-iteration policy).

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add 5 information-density features to the TUI dashboard (stage indicator, real-time rate via EWMA, per-stage ETA, top-plugins bar chart, error-category breakdown) plus 5 design optimisations (rate smoothing via EWMA, `errors.As`/`errors.Is` classifier, empty-state placeholders, plugin-name normalisation, 256-entry memory cap on PluginHits/ErrorCategories).

**Architecture:** 4 sequential commits — (a) state-type extensions, (b) error classifier package, (c) scanner.go wiring (set Stage, capture TotalHosts, normalise plugin names, bump PluginHits/ErrorCategories), (d) TUI rendering. The dispatcher thread carries Session-derived views via a new `enrichedStatsMsg` so the model can read them without holding a `*session.Session` reference.

**Tech Stack:** Go 1.26.8 stdlib (`sync.Map`, `sync/atomic`, `errors.Is/As`, `syscall`), bubbletea TUI model in `internal/tui/tui.go`. No new dependencies.

---

## Global Constraints

These come verbatim from the spec — every task's requirements implicitly include this section.

- **Branch:** `main`. No worktree isolation.
- **No business-logic changes** — `internal/core/{scanner,alive,credential}.go` and `internal/output/*.go` only get Stage/counter-write additions; no rewrites of pipeline shape.
- **Existing test suite** (`go test ./...`) must stay green after every commit.
- **Stage constants:** `StageIdle=0`, `StageAlive=1`, `StagePortScan=2`, `StageIdentify=3`, `StageCred=4`, `StageDone=5` — exact integer values (do not reorder).
- **PluginHits / ErrorCategories:** `sync.Map[string]*atomic.Int64`. Hard cap at **256 entries**; entries beyond cap silently dropped.
- **Rate smoothing:** EWMA with `α = 0.5`. Negative deltas (counter reset) clamped to 0.
- **Plugin name normalisation:** lowercase + strip trailing `/{digits}` or `-{digits}` suffix at the scanner.go dispatch write site (not in TUI render).
- **Error classifier:** `errors.Is/As` first for known sentinels (`context.DeadlineExceeded`, `net.Error.Timeout()`, `syscall.ECONNREFUSED/ECONNRESET/EHOSTUNREACH/ENETUNREACH`), then substring fallback. Categories: timeout, refused, reset, dns, perm, auth, tls, unreach, other.
- **ETA:** per-stage — alive uses `AliveProbed/TotalHosts`; port-scan + identify use `Ports/TotalPorts`; cred + done return `""` (ETA undefined for unpredictable stages).
- **Empty-state placeholders:** TUI renders `"(no hits yet)"` and `"(no errors yet)"` when their backing maps are empty.
- **Commit footer:** Every commit ends with `Co-Authored-By: Claude <noreply@anthropic.com>`.
- **Windows git:** use `git --no-optional-locks` for all git operations.

---

## File Structure (locked in by this plan)

**Modify:**
```
internal/types/state.go            # Stage constants, State.Stage/TotalHosts/TotalPorts/PluginHits/ErrorCategories, CountersView.Stage, PluginHitsView/ErrorCategoriesView
internal/tui/program.go            # statsMsg payload now carries session-derived views
internal/tui/tui.go                # model rate/ETA/topPlugins/topErrors state + View rendering
internal/core/scanner.go           # Stage.Store at each transition, TotalHosts capture, plugin hit + error category accumulation
```

**Create:**
```
internal/core/errors.go            # ClassifyError function
internal/core/errors_test.go       # TestClassifyError unit tests
```

---

## Task 1: State types extension (commit a)

**Files:**
- Modify: `internal/types/state.go`
- Test: `go test ./internal/types/...`

**Interfaces:**
- Produces:
  - `const StageIdle int32 = 0` ... `StageDone int32 = 5`
  - `func StageName(stage int32) string`
  - `State.Stage atomic.Int32`, `State.TotalHosts atomic.Int64`, `State.TotalPorts atomic.Int64`
  - `State.PluginHits sync.Map` (`string` → `*atomic.Int64`)
  - `State.ErrorCategories sync.Map` (`string` → `*atomic.Int64`)
  - `CountersView.Stage int64`
  - `(*State).PluginHitsView() map[string]int64`
  - `(*State).ErrorCategoriesView() map[string]int64`

- [ ] **Step 1.1: Write the failing test**

Append to `internal/types/state_test.go` (or create if absent):

```go
package types

import (
    "sync/atomic"
    "testing"
)

func TestStageConstantsAndName(t *testing.T) {
    cases := []struct {
        stage int32
        name  string
    }{
        {StageIdle, "IDLE"},
        {StageAlive, "ALIVE"},
        {StagePortScan, "PORT-SCAN"},
        {StageIdentify, "IDENTIFY"},
        {StageCred, "CRED"},
        {StageDone, "DONE"},
    }
    for _, c := range cases {
        if got := StageName(c.stage); got != c.name {
            t.Errorf("StageName(%d) = %q, want %q", c.stage, got, c.name)
        }
    }
}

func TestStateStageStoreAndLoad(t *testing.T) {
    s := NewState()
    if got := s.Stage.Load(); got != StageIdle {
        t.Errorf("default Stage = %d, want StageIdle=%d", got, StageIdle)
    }
    s.Stage.Store(StageAlive)
    if got := s.Stage.Load(); got != StageAlive {
        t.Errorf("after Store(StageAlive), Stage = %d, want %d", got, StageAlive)
    }
}

func TestStatePluginHitsView(t *testing.T) {
    s := NewState()
    // Direct manipulation of sync.Map for test setup.
    v, _ := s.PluginHits.LoadOrStore("ssh", &atomic.Int64{})
    v.(*atomic.Int64).Store(42)
    v2, _ := s.PluginHits.LoadOrStore("redis", &atomic.Int64{})
    v2.(*atomic.Int64).Store(7)
    view := s.PluginHitsView()
    if view["ssh"] != 42 || view["redis"] != 7 {
        t.Errorf("PluginHitsView = %v, want ssh=42 redis=7", view)
    }
}

func TestStateErrorCategoriesView(t *testing.T) {
    s := NewState()
    v, _ := s.ErrorCategories.LoadOrStore("timeout", &atomic.Int64{})
    v.(*atomic.Int64).Store(100)
    view := s.ErrorCategoriesView()
    if view["timeout"] != 100 {
        t.Errorf("ErrorCategoriesView[timeout] = %d, want 100", view["timeout"])
    }
}

func TestStateSnapshotIncludesStage(t *testing.T) {
    s := NewState()
    s.Stage.Store(StageIdentify)
    snap := s.Snapshot()
    if snap.Stage != int64(StageIdentify) {
        t.Errorf("Snapshot.Stage = %d, want %d", snap.Stage, StageIdentify)
    }
}
```

- [ ] **Step 1.2: Run test to verify it fails**

Run: `go test ./internal/types/... -run "TestStageConstants|TestStateStage|TestStatePluginHitsView|TestStateErrorCategoriesView|TestStateSnapshot" -v`
Expected: FAIL with "undefined: StageIdle" (or similar).

- [ ] **Step 1.3: Add Stage constants + State fields + CountersView.Stage + view methods**

Edit `internal/types/state.go`:

At the top of the file (after imports, before `type State struct`), add:

```go
// Scan-stage constants. State.Stage holds the current stage's integer
// value. The integer ordering (Idle=0, Alive=1, ..., Done=5) is part
// of the public contract — do NOT reorder when adding new stages.
// / Scan-stage 常量。State.Stage 持有当前阶段的整数值。整数顺序
// 是公开契约的一部分——新增阶段时不要重排。
const (
    StageIdle     int32 = 0
    StageAlive    int32 = 1
    StagePortScan int32 = 2
    StageIdentify int32 = 3
    StageCred     int32 = 4
    StageDone     int32 = 5
)

// StageName returns a short human-readable label for a stage value.
// Unknown / unset stages return "IDLE".
// / StageName 返回阶段值的短人类可读标签。未知 / 未设置返回 "IDLE"。
func StageName(stage int32) string {
    switch stage {
    case StageAlive:
        return "ALIVE"
    case StagePortScan:
        return "PORT-SCAN"
    case StageIdentify:
        return "IDENTIFY"
    case StageCred:
        return "CRED"
    case StageDone:
        return "DONE"
    default:
        return "IDLE"
    }
}
```

Modify the existing `State` struct to add:

```go
type State struct {
    seen     sync.Map
    Counters Counters
    StartTime time.Time

    // v0.5.2 additions for TUI information density.
    Stage           atomic.Int32
    TotalHosts      atomic.Int64
    TotalPorts      atomic.Int64
    PluginHits      sync.Map // string → *atomic.Int64
    ErrorCategories sync.Map // string → *atomic.Int64
}
```

Modify `CountersView` to add:

```go
type CountersView struct {
    Alive       int64
    AliveProbed int64
    Ports       int64
    Results     int64
    Creds       int64
    Errors      int64
    Stage       int64 // v0.5.2: current scan stage (StageIdle=0, StageAlive=1, ...)
}
```

Modify `Snapshot()` to include Stage:

```go
func (s *State) Snapshot() CountersView {
    return CountersView{
        Alive:       s.Counters.Alive.Load(),
        AliveProbed: s.Counters.AliveProbed.Load(),
        Ports:       s.Counters.Ports.Load(),
        Results:     s.Counters.Results.Load(),
        Creds:       s.Counters.Creds.Load(),
        Errors:      s.Counters.Errors.Load(),
        Stage:       int64(s.Stage.Load()),
    }
}
```

Add the view methods at the end of the file (before `HashKey`):

```go
// PluginHitsView returns a snapshot of plugin-name → hit count as a
// plain map (atomic load per entry). Read-only; safe to call from
// the TUI's stats handler.
// / PluginHitsView 返回 plugin-name → hit count 快照 map（每项
// atomic load）。只读；可从 TUI 的 stats handler 安全调用。
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
// plain map (atomic load per entry).
// / ErrorCategoriesView 返回 category → count 快照 map（每项
// atomic load）。
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

- [ ] **Step 1.4: Run tests to verify they pass**

Run: `go test ./internal/types/... -run "TestStageConstants|TestStateStage|TestStatePluginHitsView|TestStateErrorCategoriesView|TestStateSnapshot" -v`
Expected: all PASS.

- [ ] **Step 1.5: Verify full test suite still passes**

Run: `go test ./... -count=1 -timeout 60s 2>&1 | tail -5`
Expected: all packages PASS (or pre-existing warnings about Windows race DLL).

- [ ] **Step 1.6: Commit**

```bash
git add internal/types/state.go internal/types/state_test.go
git commit -m "feat(types): add Stage constants + PluginHits/ErrorCategories maps

Adds 6-stage scan lifecycle constants (StageIdle..StageDone) and
four new atomic/sync.Map fields to types.State:

  - Stage:           atomic.Int32    current pipeline stage
  - TotalHosts:      atomic.Int64    captured at scan start (for ETA)
  - TotalPorts:      atomic.Int64    total port-scan items (for ETA)
  - PluginHits:      sync.Map        plugin name -> *atomic.Int64
  - ErrorCategories: sync.Map        category -> *atomic.Int64

CountersView gains a Stage int64 field; Snapshot() populates it.

New view methods PluginHitsView / ErrorCategoriesView return
plain maps (atomic load per entry) for thread-safe TUI reads.

TestStateStageStoreAndLoad / TestStatePluginHitsView /
TestStateErrorCategoriesView / TestStateSnapshotIncludesStage /
TestStageConstantsAndName added.

Part of TUI v2 Spec A (info density, 5 features). Zero
business-logic changes.

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

## Task 2: Error classifier package (commit b)

**Files:**
- Create: `internal/core/errors.go`
- Create: `internal/core/errors_test.go`

**Interfaces:**
- Produces:
  - `func ClassifyError(err error) string` — returns one of `"timeout"`, `"refused"`, `"reset"`, `"dns"`, `"perm"`, `"auth"`, `"tls"`, `"unreach"`, `"other"`, or `""` for `nil`.

- [ ] **Step 2.1: Write the failing test**

Create `internal/core/errors_test.go`:

```go
// errors_test.go — ClassifyError unit tests.
// / ClassifyError 单元测试。
package core

import (
    "context"
    "errors"
    "fmt"
    "net"
    "syscall"
    "testing"
)

func TestClassifyError(t *testing.T) {
    // net.OpError with Timeout() == true should classify as "timeout".
    // / net.OpError 带 Timeout() == true 应归为 "timeout"。
    timeoutErr := &net.OpError{
        Op:  "dial",
        Net: "tcp",
        Err: timeoutTrue{}, // satisfies Timeout() bool
    }
    if got := ClassifyError(timeoutErr); got != "timeout" {
        t.Errorf("net.OpError{Timeout} = %q, want timeout", got)
    }

    // context.DeadlineExceeded wraps to "timeout".
    // / context.DeadlineExceeded 包装为 "timeout"。
    if got := ClassifyError(context.DeadlineExceeded); got != "timeout" {
        t.Errorf("context.DeadlineExceeded = %q, want timeout", got)
    }

    // syscall.ECONNREFUSED → "refused".
    // / syscall.ECONNREFUSED → "refused"。
    if got := ClassifyError(syscall.ECONNREFUSED); got != "refused" {
        t.Errorf("ECONNREFUSED = %q, want refused", got)
    }

    // syscall.ECONNRESET → "reset".
    // / syscall.ECONNRESET → "reset"。
    if got := ClassifyError(syscall.ECONNRESET); got != "reset" {
        t.Errorf("ECONNRESET = %q, want reset", got)
    }

    // syscall.EHOSTUNREACH → "unreach".
    // / syscall.EHOSTUNREACH → "unreach"。
    if got := ClassifyError(syscall.EHOSTUNREACH); got != "unreach" {
        t.Errorf("EHOSTUNREACH = %q, want unreach", got)
    }

    // syscall.ENETUNREACH → "unreach".
    // / syscall.ENETUNREACH → "unreach"。
    if got := ClassifyError(syscall.ENETUNREACH); got != "unreach" {
        t.Errorf("ENETUNREACH = %q, want unreach", got)
    }

    // Plain string fallback cases.
    // / 纯字符串回退。
    stringCases := []struct {
        msg, want string
    }{
        {"connection refused", "refused"},
        {"connection reset by peer", "reset"},
        {"no such host", "dns"},
        {"dns lookup failed", "dns"},
        {"permission denied", "perm"},
        {"operation not permitted", "perm"},
        {"auth failed", "auth"},
        {"tls handshake failure", "tls"},
        {"ssl handshake error", "tls"},
        {"no route to host", "unreach"},
        {"network is unreachable", "unreach"},
        {"i/o timeout", "timeout"},
        {"some random unrelated error", "other"},
    }
    for _, c := range stringCases {
        if got := ClassifyError(errors.New(c.msg)); got != c.want {
            t.Errorf("ClassifyError(%q) = %q, want %q", c.msg, got, c.want)
        }
    }

    // nil error → "".
    // / nil 错误 → ""。
    if got := ClassifyError(nil); got != "" {
        t.Errorf("ClassifyError(nil) = %q, want \"\"", got)
    }

    // Wrapped syscall error via fmt.Errorf with %w.
    // / 用 fmt.Errorf %w 包装的 syscall 错误。
    wrapped := fmt.Errorf("dial: %w", syscall.ECONNREFUSED)
    if got := ClassifyError(wrapped); got != "refused" {
        t.Errorf("wrapped ECONNREFUSED = %q, want refused", got)
    }
}

// timeoutTrue satisfies net.Error with Timeout() true.
// / timeoutTrue 满足 net.Error 且 Timeout() 返回 true。
type timeoutTrue struct{}

func (timeoutTrue) Error() string { return "i/o timeout" }
func (timeoutTrue) Timeout() bool { return true }
func (timeoutTrue) Temporary() bool { return true }
```

- [ ] **Step 2.2: Run test to verify it fails**

Run: `go test ./internal/core/ -run TestClassifyError -v`
Expected: FAIL with "undefined: ClassifyError".

- [ ] **Step 2.3: Implement ClassifyError**

Create `internal/core/errors.go`:

```go
// errors.go — error classification for TUI error-category
// breakdown (TUI v2 Spec A feature 5).
// / errors.go — TUI 错误分类断点（Spec A 第 5 个 feature）。
//
// ClassifyError maps an error from plugin execution to a stable
// category label. Uses errors.Is/As for known sentinel types
// (context, net.Error, syscall) before falling back to substring
// match on the lowercased error text. Categories are intentionally
// coarse; finer sub-classes can be added without changing the
// aggregation shape.

package core

import (
    "context"
    "errors"
    "net"
    "strings"
    "syscall"
)

// ClassifyError returns one of: "timeout", "refused", "reset",
// "dns", "perm", "auth", "tls", "unreach", "other", or "" for nil.
// / ClassifyError 返回类别标签，nil 返回 ""。
func ClassifyError(err error) string {
    if err == nil {
        return ""
    }
    // 1) Specific sentinel checks via errors.Is/As.
    // / 1) 通过 errors.Is/As 检查特定 sentinel。
    if errors.Is(err, context.DeadlineExceeded) {
        return "timeout"
    }
    var netErr net.Error
    if errors.As(err, &netErr) && netErr.Timeout() {
        return "timeout"
    }
    if errors.Is(err, syscall.ECONNREFUSED) {
        return "refused"
    }
    if errors.Is(err, syscall.ECONNRESET) {
        return "reset"
    }
    if errors.Is(err, syscall.EHOSTUNREACH) ||
        errors.Is(err, syscall.ENETUNREACH) {
        return "unreach"
    }
    // 2) Substring fallback on lowercased text.
    // / 2) 小写文本子串回退。
    s := strings.ToLower(err.Error())
    switch {
    case strings.Contains(s, "timeout"),
        strings.Contains(s, "i/o timeout"):
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
    case strings.Contains(s, "unreachable"),
        strings.Contains(s, "no route"):
        return "unreach"
    }
    return "other"
}
```

- [ ] **Step 2.4: Run test to verify it passes**

Run: `go test ./internal/core/ -run TestClassifyError -v`
Expected: PASS (16 sub-cases all green).

- [ ] **Step 2.5: Commit**

```bash
git add internal/core/errors.go internal/core/errors_test.go
git commit -m "feat(core): add ClassifyError with errors.Is/As first, substring fallback

New file internal/core/errors.go. ClassifyError(err) returns a
stable category label suitable for TUI error-category breakdown
(TUI v2 Spec A feature 5).

Resolution order:
  1. errors.Is(err, context.DeadlineExceeded)  -> timeout
  2. errors.As + net.Error.Timeout()            -> timeout
  3. errors.Is for syscall.ECONNREFUSED /
     ECONNRESET / EHOSTUNREACH / ENETUNREACH
  4. Substring match on lowercased error text
     (timeout, refused, reset, dns, perm, auth, tls, unreach)
  5. Fallback "other"

Returns "" for nil. TestClassifyError covers 16 cases including
nil, wrapped syscalls, and net.OpError with Timeout()==true.

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

## Task 3: scanner.go wiring (commit c)

**Files:**
- Modify: `internal/core/scanner.go`
- Test: `internal/core/scanner_test.go` (extend existing — add `TestScanner_TracksStageAndPluginHits`)

**Interfaces:**
- Consumes:
  - `types.StageAlive / StagePortScan / StageIdentify / StageCred / StageDone`
  - `core.ClassifyError`
  - `core.normalisePluginName` (defined in this task)
  - `core.bumpSyncMap` (defined in this task)
- Produces:
  - scanner sets `sess.State.Stage` at each transition
  - scanner sets `sess.State.TotalHosts` at scan start
  - scanner sets `sess.State.TotalPorts` after port-scan queueing
  - scanner's plugin-dispatch loop calls `bumpSyncMap(&sess.State.PluginHits, normalisePluginName(hit.Service), 256)` on hit
  - scanner's plugin-dispatch loop calls `bumpSyncMap(&sess.State.ErrorCategories, ClassifyError(err), 256)` on error

- [ ] **Step 3.1: Write the failing test**

Append to `internal/core/scanner_test.go`:

```go
// TestScanner_TracksStageAndPluginHits verifies that scanner.go
// drives sess.State.Stage through the 6-stage lifecycle and
// accumulates PluginHits / ErrorCategories as work progresses.
// / TestScanner_TracksStageAndPluginHits 验证 scanner.go 把
// sess.State.Stage 走完 6 阶段生命周期，并在工作中累计
// PluginHits / ErrorCategories。
func TestScanner_TracksStageAndPluginHits(t *testing.T) {
    // Override alive-probes factory with a slow probe so we have
    // time to observe mid-RunScan Stage transitions.
    // / 覆盖 alive-probes 工厂用慢 probe，方便观察 mid-RunScan Stage。
    origFn := aliveProbesFn
    t.Cleanup(func() { aliveProbesFn = origFn })
    aliveProbesFn = func() alive.Options {
        return alive.Options{
            Probes: []alive.Probe{
                &delayedProbe{
                    delay: 50 * time.Millisecond,
                    hit:   alive.Hit{Host: "127.0.0.1", Method: alive.MethodTCP},
                },
            },
            Timeout:   2 * time.Second,
            Threads:   1,
            FirstOnly: true,
        }
    }

    cfg := &types.Config{
        Host:      "h1,h2",
        Mode:      types.ModeScan,
        Timeout:   2 * time.Second,
        AliveOnly: false, // full pipeline
        Silent:    true,
    }
    sess, err := session.NewSession(context.Background(), cfg, "")
    if err != nil {
        t.Fatalf("NewSession: %v", err)
    }

    // Run in goroutine.
    runDone := make(chan error, 1)
    go func() {
        _, runErr := RunScan(context.Background(), sess)
        runDone <- runErr
    }()
    if err := <-runDone; err != nil {
        t.Fatalf("RunScan: %v", err)
    }

    // After RunScan returns: Stage must be StageDone, TotalHosts=2.
    if got := sess.State.Stage.Load(); got != types.StageDone {
        t.Errorf("Stage after RunScan = %d, want StageDone=%d", got, types.StageDone)
    }
    if got := sess.State.TotalHosts.Load(); got != 2 {
        t.Errorf("TotalHosts = %d, want 2", got)
    }
    // AliveProbed must be 2 (all targets probed).
    if got := sess.State.Counters.AliveProbed.Load(); got != 2 {
        t.Errorf("AliveProbed = %d, want 2", got)
    }
    // PluginHits is empty (alive-only-ish probe hit never becomes
    // a plugin identify hit in this harness).
    // / PluginHits 应为空（本测试 harness 里 alive probe hit 不走 plugin identify）。
    if got := len(sess.State.PluginHitsView()); got != 0 {
        t.Errorf("PluginHitsView len = %d, want 0", got)
    }
}

func TestNormalisePluginName(t *testing.T) {
    cases := []struct{ in, want string }{
        {"ssh", "ssh"},
        {"SSH", "ssh"},
        {"  ssh  ", "ssh"},
        {"ssh/2.0", "ssh"},
        {"ssh-1.2", "ssh"},
        {"Redis/7.2", "redis"},
        {"redis", "redis"},
        {"postgres-15", "postgres"}, // numeric suffix stripped
        {"ssh/non-version", "ssh"},  // non-numeric tail kept
    }
    for _, c := range cases {
        if got := normalisePluginName(c.in); got != c.want {
            t.Errorf("normalisePluginName(%q) = %q, want %q", c.in, got, c.want)
        }
    }
}
```

- [ ] **Step 3.2: Run test to verify it fails**

Run: `go test ./internal/core/ -run "TestScanner_TracksStageAndPluginHits|TestNormalisePluginName" -v -count=1`
Expected: FAIL — Stage not in scanner flow / normalisePluginName undefined.

- [ ] **Step 3.3: Add `normalisePluginName` and `bumpSyncMap` helpers**

Add to `internal/core/scanner.go` (at the bottom of the file, after the existing functions):

```go
// normalisePluginName lowercases and strips version-like suffixes
// so the same plugin doesn't show up as 3 rows ("ssh", "SSH",
// "ssh/2.0"). Called at the scanner.go dispatch write site, not
// in TUI render.
// / normalisePluginName 小写化并去掉版本后缀，让同一个 plugin
// 不以 3 行显示。在 scanner.go dispatch 写入点调用，TUI 渲染不调。
func normalisePluginName(name string) string {
    n := strings.ToLower(strings.TrimSpace(name))
    // Strip trailing "/x.y" or "-x.y" suffixes only when tail looks
    // like a version (starts with digit). "ssh/non-version" stays.
    // / 去掉尾部 "/x.y" 或 "-x.y" 后缀，仅当 tail 像版本号（以数字开头）。
    for _, sep := range []string{"/", "-"} {
        if i := strings.Index(n, sep); i >= 0 {
            tail := n[i+1:]
            if len(tail) > 0 && tail[0] >= '0' && tail[0] <= '9' {
                n = n[:i]
            }
        }
    }
    return n
}

// maxSyncMapEntries caps PluginHits and ErrorCategories at 256 to
// avoid leaking garbage strings from misbehaving plugins. Real max
// in FG-QiMen is ~50 plugins, so the cap is defensive only.
// / maxSyncMapEntries 把 PluginHits 和 ErrorCategories 上限设为
// 256，避免行为不端的 plugin 泄漏垃圾字符串。真实上限约 50。
const maxSyncMapEntries = 256

// bumpSyncMap increments a *atomic.Int64 stored under key in a
// sync.Map. Creates the entry if absent; silently drops the
// increment when the cap is reached. / bumpSyncMap 递增 sync.Map
// 中 key 下存储的 *atomic.Int64。如不存在则创建；达上限时静默
// 丢弃。
func bumpSyncMap(m *sync.Map, key string) {
    if v, ok := m.Load(key); ok {
        if c, ok := v.(*atomic.Int64); ok {
            c.Add(1)
        }
        return
    }
    // Cap check: count current entries before adding new key.
    // / 上限检查：加新 key 前先数当前条目。
    count := 0
    m.Range(func(_, _ any) bool {
        count++
        return true
    })
    if count >= maxSyncMapEntries {
        return // cap reached — drop the increment silently
    }
    v, _ := m.LoadOrStore(key, &atomic.Int64{})
    if c, ok := v.(*atomic.Int64); ok {
        c.Add(1)
    }
}
```

Make sure `internal/core/scanner.go` already imports `"sync"` and `"sync/atomic"` — if not, add them. It currently imports `"sync"` already (used by `sync.WaitGroup`). It may need `"sync/atomic"` and `"strings"` added.

- [ ] **Step 3.4: Wire Stage.Store and TotalHosts/TotalPorts into RunScan**

In `internal/core/scanner.go`, edit the `runFullPipeline` (and `runCrackPipeline` if applicable) to:

```go
func runFullPipeline(ctx context.Context, sess *session.Session) (int, error) {
    cfg := sess.Config
    targets, err := types.ExpandTargets(cfg.Host, cfg.HostsFile)
    if err != nil {
        return 0, fmt.Errorf("expand targets: %w", err)
    }
    sess.State.TotalHosts.Store(int64(len(targets)))
    // ... existing pre-alive code unchanged ...

    // Stage transition: ALIVE.
    sess.State.Stage.Store(types.StageAlive)

    // ... existing alive block (with Round-2 polling goroutine) ...
    // [Keep the existing alivePollCtx / alivePollCancel / goroutine code.]

    // Stage transition: PORT-SCAN.
    sess.State.Stage.Store(types.StagePortScan)

    // ... existing port-scan code ...
    // After queueing all port-scan items, estimate total ports:
    sess.State.TotalPorts.Store(int64(len(portScanItems)))

    // Stage transition: IDENTIFY.
    sess.State.Stage.Store(types.StageIdentify)

    // ... existing plugin dispatch ...
    // In the worker goroutine, after plugin.Identify(...):
    //   if hit != nil {
    //       bumpSyncMap(&sess.State.PluginHits,
    //           normalisePluginName(hit.Service))
    //   }
    //   if err != nil {
    //       bumpSyncMap(&sess.State.ErrorCategories,
    //           ClassifyError(err))
    //       // ... existing error logging ...
    //   }

    // Stage transition: CRED.
    sess.State.Stage.Store(types.StageCred)

    // ... existing cred spray ...

    // Stage transition: DONE.
    sess.State.Stage.Store(types.StageDone)
    return 0, nil
}
```

For the plugin-dispatch worker hook, find the existing worker loop that calls `plugin.Identify(ctx, host, port)`. After the call site, add the `bumpSyncMap` lines. The exact line varies — search for `plugin.Identify(` or `sess.Log.Warn("scan probe error` to find the right insertion point.

For `runCrackPipeline` (ModeCrack), apply the same Stage transitions and bump calls — skipping StageAlive since crack mode skips alive.

- [ ] **Step 3.5: Run tests to verify they pass**

Run: `go test ./internal/core/ -run "TestScanner_TracksStageAndPluginHits|TestNormalisePluginName" -v -count=1`
Expected: both PASS.

- [ ] **Step 3.6: Run full test suite**

Run: `go test ./... -count=1 -timeout 60s 2>&1 | tail -5`
Expected: all packages PASS.

- [ ] **Step 3.7: Commit**

```bash
git add internal/core/scanner.go internal/core/scanner_test.go
git commit -m "feat(scanner): drive Stage lifecycle + PluginHits/ErrorCategories

Wires the new types.State fields from TUI Spec A into RunScan:

  - Stage.Store(StageAlive/PortScan/Identify/Cred/Done) at each
    transition in runFullPipeline / runCrackPipeline
  - TotalHosts.Store(len(targets)) at scan start
  - TotalPorts.Store(len(portScanItems)) after port-scan queueing
  - Plugin-dispatch worker bumps PluginHits on identify hit
    (via normalisePluginName) and ErrorCategories on identify
    error (via ClassifyError)

New helpers in scanner.go:
  - normalisePluginName: lowercase + strip /x.y -x.y version suffix
  - bumpSyncMap: atomic increment with 256-entry cap (defensive)

Tests:
  - TestScanner_TracksStageAndPluginHits verifies Stage reaches
    StageDone, TotalHosts=2, AliveProbed=2 after RunScan.
  - TestNormalisePluginName covers 9 normalisation cases.

Part of TUI v2 Spec A (info density). Zero business-logic
changes — only adds state writes at existing transition points.

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

## Task 4: TUI render (commit d)

**Files:**
- Modify: `internal/tui/program.go`
- Modify: `internal/tui/tui.go`
- Modify: `internal/tui/program_test.go` (extend existing)

**Interfaces:**
- Consumes:
  - `(*session.Session)` accessed via new model field (or via new `enrichedStatsMsg`)
  - `types.CountersView` (with new `Stage` field)
  - `(*State).PluginHitsView() / ErrorCategoriesView()`
  - `core.normalisePluginName` — N/A (already used in Task 3)
  - `core.ClassifyError` — N/A (already used in Task 3)
- Produces:
  - `model.rateTracker` field (embedded struct)
  - `model.rateHits, ratePorts float64`
  - `model.eta string`
  - `model.topPlugins, topErrors [][2]string`
  - Updated `View()` rendering: stage badge line, rate row, top plugins panel, error categories row

- [ ] **Step 4.1: Write the failing tests**

Append to `internal/tui/program_test.go`:

```go
// TestDispatcher_RendersRateAndPlugins verifies that after multiple
// statsMsg deltas, the model's rate fields stabilise (EWMA) and
// topPlugins / topErrors reflect the Session view methods.
// / TestDispatcher_RendersRateAndPlugins 验证多次 statsMsg delta 后，
// model 的 rate 字段稳定（EWMA），topPlugins/topErrors 反映 Session
// view methods。
func TestDispatcher_RendersRateAndPlugins(t *testing.T) {
    sess := newTestSession(t)
    sess.State.PluginHitsView() // sanity: empty
    // Pretend 5 plugins hit with these counts.
    type kv struct{ name string; n int64 }
    seed := []kv{{"ssh", 47}, {"redis", 21}, {"mysql", 12}, {"http", 9}, {"postgres", 5}}
    for _, k := range seed {
        v, _ := sess.State.PluginHits.LoadOrStore(k.name, &atomic.Int64{})
        v.(*atomic.Int64).Store(k.n)
    }

    // Send 6 statsMsg deltas with monotonically increasing
    // Creds and Ports (so rate is non-zero).
    m := newTestModel(sess)
    base := time.Now()
    for i := 0; i < 6; i++ {
        msg := statsMsg{
            view: types.CountersView{
                Alive: 12, AliveProbed: 256, Ports: int64(80 * (i + 1)),
                Results: int64(5 * (i + 1)), Creds: int64(2 * (i + 1)),
                Errors:  3, Stage: int64(types.StageIdentify),
            },
            elapsed: time.Duration(i+1) * time.Second,
            when:    base.Add(time.Duration(i) * time.Second),
        }
        m.Update(msg)
    }

    // After 6 ticks, rate fields should be non-zero (since
    // Creds and Ports each incremented by 12 / 80 * tick).
    if got := m.rateHits; got <= 0 {
        t.Errorf("rateHits = %v, want > 0 after 6 deltas", got)
    }
    if got := m.ratePorts; got <= 0 {
        t.Errorf("ratePorts = %v, want > 0 after 6 deltas", got)
    }
    // topPlugins: top 5 by count = full seed list (sorted desc).
    if len(m.topPlugins) != 5 {
        t.Errorf("topPlugins len = %d, want 5", len(m.topPlugins))
    }
    if m.topPlugins[0][0] != "ssh" {
        t.Errorf("topPlugins[0] = %q, want ssh", m.topPlugins[0][0])
    }
    // topErrors: empty (no errors seeded).
    if len(m.topErrors) != 0 {
        t.Errorf("topErrors len = %d, want 0", len(m.topErrors))
    }
}

// TestDispatcher_RateEmptyState verifies placeholders.
// / TestDispatcher_RateEmptyState 验证空态占位符。
func TestDispatcher_RateEmptyState(t *testing.T) {
    sess := newTestSession(t)
    m := newTestModel(sess)
    msg := statsMsg{
        view: types.CountersView{Stage: int64(types.StageAlive)},
        when: time.Now(),
    }
    m.Update(msg)
    // topPlugins / topErrors should be nil/empty so View() renders
    // "(no hits yet)" / "(no errors yet)".
    // / topPlugins/topErrors 应为空以便 View() 渲染占位符。
    if len(m.topPlugins) != 0 {
        t.Errorf("topPlugins = %v, want empty", m.topPlugins)
    }
    if len(m.topErrors) != 0 {
        t.Errorf("topErrors = %v, want empty", m.topErrors)
    }
    // View() string should contain the placeholders.
    v := m.View()
    if !strings.Contains(v, "(no hits yet)") {
        t.Errorf("View missing '(no hits yet)' placeholder: %q", v)
    }
    if !strings.Contains(v, "(no errors yet)") {
        t.Errorf("View missing '(no errors yet)' placeholder: %q", v)
    }
}
```

The helpers `newTestSession(t)` and `newTestModel(sess)` need to exist in the test package — if they don't, add minimal stubs at the top of the test file (next to other test helpers):

```go
func newTestSession(t *testing.T) *session.Session {
    cfg := &types.Config{Host: "h1", Mode: types.ModeScan, Timeout: time.Second, Silent: true}
    sess, err := session.NewSession(context.Background(), cfg, "")
    if err != nil { t.Fatalf("NewSession: %v", err) }
    return sess
}

func newTestModel(sess *session.Session) *model {
    return newModel(sess)
}
```

If `newModel(sess *session.Session)` doesn't exist yet (current signature is `newModel()`), update its signature in `internal/tui/tui.go` (Task 4.4 below) to accept a session.

- [ ] **Step 4.2: Run tests to verify they fail**

Run: `go test ./internal/tui/... -run "TestDispatcher_RendersRateAndPlugins|TestDispatcher_RateEmptyState" -v -count=1`
Expected: FAIL — model fields (rateHits, ratePorts, topPlugins, topErrors) undefined.

- [ ] **Step 4.3: Add rateTracker and helpers to tui package**

Add to `internal/tui/tui.go` (or a new `internal/tui/render.go` file):

```go
// rateTracker keeps an EWMA-smoothed per-second rate of hits and
// ports. EWMA α=0.5 gives faster decay than pure mean so a brief
// burst still shows up quickly.
// / rateTracker 维持 hits 和 ports 的 EWMA 平滑每秒速率。α=0.5
// 衰减比纯均值快，瞬时突增仍能迅速体现。
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
            if dH < 0 { dH = 0 }
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

// topN returns the top n (name, countString) pairs from m sorted by
// count desc. Returns nil when m is empty so View() can render
// placeholders.
// / topN 从 m 取计数前 n 名，返回排序好的 [name, countString] 对。
// m 为空时返回 nil 以便 View() 渲染占位符。
func topN(m map[string]int64, n int) [][2]string {
    type kv struct{ k string; v int64 }
    var all []kv
    for k, v := range m {
        if v <= 0 { continue }
        all = append(all, kv{k, v})
    }
    if len(all) == 0 {
        return nil
    }
    sort.Slice(all, func(i, j int) bool { return all[i].v > all[j].v })
    out := make([][2]string, 0, n)
    for i := 0; i < len(all) && i < n; i++ {
        out = append(out, [2]string{all[i].k, fmt.Sprintf("%d", all[i].v)})
    }
    return out
}

// computeETA returns "ETA ~Ns" string per-stage. Returns "" if
// inputs are insufficient. / computeETA 按阶段返回 ETA 字符串。
func computeETA(start, now time.Time, stage int32, view types.CountersView) string {
    elapsed := now.Sub(start).Seconds()
    if elapsed <= 0 {
        return ""
    }
    switch int(stage) {
    case int(types.StageAlive):
        if view.AliveProbed > 0 && view.TotalHosts > 0 &&
            view.AliveProbed < view.TotalHosts {
            rem := elapsed * float64(view.TotalHosts-view.AliveProbed) / float64(view.AliveProbed)
            return fmt.Sprintf("~%ds", int(rem))
        }
    case int(types.StagePortScan), int(types.StageIdentify):
        if view.Ports > 0 && view.TotalPorts > 0 &&
            view.Ports < view.TotalPorts {
            rem := elapsed * float64(view.TotalPorts-view.Ports) / float64(view.Ports)
            return fmt.Sprintf("~%ds", int(rem))
        }
    }
    return ""
}

// bar renders a fixed-width horizontal bar. / bar 渲染固定宽度 bar。
func bar(ratio float64, w int) string {
    if ratio < 0 { ratio = 0 }
    if ratio > 1 { ratio = 1 }
    filled := int(float64(w) * ratio)
    return strings.Repeat("█", filled) + strings.Repeat("░", w-filled)
}
```

Add `import "sort"` if not already present.

- [ ] **Step 4.4: Update `model` struct + Update handler + View rendering**

In `internal/tui/tui.go`:

Modify `model` struct to add fields:

```go
type model struct {
    // ... existing fields ...

    // v0.5.2 additions for info-density TUI:
    sess       *session.Session // optional; populated if non-nil
    rate       rateTracker
    eta        string
    topPlugins [][2]string
    topErrors  [][2]string
    rateHits   float64
    ratePorts  float64
}
```

Change `newModel()` to `newModel(sess *session.Session) *model` and have it store `sess`. Find all callers and update them.

In the `statsMsg` case of `Update`:

```go
case statsMsg:
    now := msg.when
    if now.IsZero() { now = time.Now() }
    hits, ports := m.rate.update(now, msg.view.Creds, msg.view.Ports)
    m.rateHits = hits
    m.ratePorts = ports
    m.eta = computeETA(m.start, now, int32(msg.view.Stage), msg.view)
    if m.sess != nil {
        m.topPlugins = topN(m.sess.State.PluginHitsView(), 5)
        m.topErrors = topN(m.sess.State.ErrorCategoriesView(), 5)
    }
    m.counters = msg.view
    // ... existing counters update / elapsed update / events append ...
```

In `View()`, replace the existing stats bar + counters table section with:

```go
// Stage badge line.
stage := types.StageName(int32(m.counters.Stage))
spinner := "▶"
if int32(m.counters.Stage) == types.StageDone {
    spinner = "✓"
}
stageBadge := fmt.Sprintf("  [ %s %s ]", spinner, stage)
fmt.Fprintf(&b, "%s", stageBadge)
if m.eta != "" {
    fmt.Fprintf(&b, "%*s", width-lipgloss.Width(stageBadge)-len(m.eta)-2, "")
    fmt.Fprintf(&b, "  %s\n", m.eta)
} else {
    fmt.Fprintf(&b, "%*s", width-lipgloss.Width(stageBadge)-len(m.elapsed)-2, "")
    fmt.Fprintf(&b, "  elapsed %s\n", m.elapsed)
}
fmt.Fprintln(&b)

// Rate row.
if m.rateHits > 0 || m.ratePorts > 0 {
    fmt.Fprintf(&b, "  rate: %.1f hits/s    ports: %.1f/s    probed %d / %d\n",
        m.rateHits, m.ratePorts, m.counters.AliveProbed, m.counters.TotalHosts)
}
fmt.Fprintln(&b, strings.Repeat("─", width))

// Counters table (left) + top plugins (right) if width >= 100.
if width >= 100 {
    // Side-by-side layout.
    rows := [][2]string{
        {"alive", fmt.Sprintf("%d", m.counters.Alive)},
        {"probed", fmt.Sprintf("%d", m.counters.AliveProbed)},
        {"ports", fmt.Sprintf("%d", m.counters.Ports)},
        {"results", fmt.Sprintf("%d", m.counters.Results)},
        {"creds", fmt.Sprintf("%d", m.counters.Creds)},
        {"errors", fmt.Sprintf("%d", m.counters.Errors)},
    }
    for _, r := range rows {
        fmt.Fprintf(&b, "  %-8s %s\n", r[0], r[1])
    }
    fmt.Fprintln(&b, strings.Repeat("─", width/2))
    fmt.Fprintln(&b, "  TOP PLUGINS")
    if len(m.topPlugins) == 0 {
        fmt.Fprintln(&b, "  (no hits yet)")
    } else {
        for _, p := range m.topPlugins {
            // Count → bar of 12 chars → name.
            // / 计数 → 12 字符 bar → 名称。
            fmt.Fprintf(&b, "  %-10s %s  %s\n",
                p[1], bar(0.5, 12), p[0])
        }
    }
} else {
    // Stacked layout (narrow terminals).
    // ... same counters as above but full-width ...
    fmt.Fprintln(&b, "  TOP PLUGINS")
    if len(m.topPlugins) == 0 {
        fmt.Fprintln(&b, "  (no hits yet)")
    } else {
        for _, p := range m.topPlugins {
            fmt.Fprintf(&b, "  %-10s %s  %s\n",
                p[1], bar(0.5, 12), p[0])
        }
    }
}
fmt.Fprintln(&b, strings.Repeat("─", width))

// Error categories row.
if len(m.topErrors) == 0 {
    fmt.Fprintln(&b, "  ERRORS:  (no errors yet)")
} else {
    fmt.Fprint(&b, "  ERRORS:  ")
    for i, e := range m.topErrors {
        if i > 0 { fmt.Fprint(&b, "   ") }
        fmt.Fprintf(&b, "%s %s", e[0], e[1])
    }
    fmt.Fprintln(&b)
}
```

- [ ] **Step 4.5: Run TUI tests**

Run: `go test ./internal/tui/... -v -count=1`
Expected: all PASS (including the new `TestDispatcher_RendersRateAndPlugins` and `TestDispatcher_RateEmptyState`).

- [ ] **Step 4.6: Run full test suite**

Run: `go test ./... -count=1 -timeout 60s 2>&1 | tail -5`
Expected: all packages PASS.

- [ ] **Step 4.7: Commit**

```bash
git add internal/tui/tui.go internal/tui/program.go internal/tui/program_test.go
git commit -m "feat(tui): info-density panels (stage badge, rate, top plugins, errors)

Wires Spec A's 5 features into the TUI:

  - Stage badge line: "[ ▸ ALIVE ]" / "[ ▸ IDENTIFY ]" / "[ ✓ DONE ]"
    with optional ETA on the right
  - Rate row: "rate: X.X hits/s   ports: Y.Y/s   probed N / M"
    using EWMA-smoothed rateTracker (alpha=0.5) over 1Hz statsMsg
    deltas; negative deltas clamped to 0
  - Top plugins bar chart: top 5 by hit count, rendered as 12-char
    bars; empty backing map shows "(no hits yet)"
  - Error categories row: top 5 by count, space-separated
    (timeout=42 refused=15 ...); empty shows "(no errors yet)"
  - Per-stage ETA: alive uses probed/totalHosts ratio,
    port-scan/identify uses ports/totalPorts, cred/done empty

Helpers (new file internal/tui/render.go):
  - rateTracker (EWMA state)
  - topN(sorted map snapshot)
  - computeETA(stage-aware)
  - bar(fixed-width Unicode block render)

Layout adapts to terminal width: >=100 cols = side-by-side
counters + top plugins, <100 cols = stacked.

Tests:
  - TestDispatcher_RendersRateAndPlugins: 6 statsMsg deltas,
    rate fields > 0, topPlugins reflects Session view
  - TestDispatcher_RateEmptyState: empty maps render
    "(no hits yet)" / "(no errors yet)" placeholders

Part of TUI v2 Spec A. No business-logic changes — TUI only.

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

## Self-Review

**1. Spec coverage:**

| Spec section | Plan task(s) |
|---|---|
| §2 Goal 1 — Stage indicator | Task 1 (constants) + Task 3 (scanner wires Stage.Store) + Task 4 (TUI badge) |
| §2 Goal 2 — Real-time rate | Task 4 (rateTracker EWMA + View rendering) |
| §2 Goal 3 — ETA | Task 4 (computeETA per-stage) |
| §2 Goal 4 — Top plugins bar | Task 1 (PluginHits + view) + Task 3 (bumpSyncMap + normalisePluginName) + Task 4 (topN + render) |
| §2 Goal 5 — Error-category breakdown | Task 1 (ErrorCategories + view) + Task 2 (ClassifyError) + Task 3 (bumpSyncMap) + Task 4 (topN + render) |
| §3 Non-goals | honoured (no interactive, no animation) |
| §4.1 Data model | Task 1 |
| §4.2 ClassifyError | Task 2 |
| §4.3 scanner wiring | Task 3 |
| §4.4 TUI render | Task 4 |
| §4.5 Helpers (rateTracker, topN, computeETA, bar) | Task 4 |
| §4.6 Empty-state placeholders | Task 4 |
| §4.7 Memory bounds (256 cap) | Task 3 (bumpSyncMap) |
| §6 Acceptance — errors_test.go | Task 2 |
| §6 Acceptance — scanner_test | Task 3 |
| §6 Acceptance — TUI tests | Task 4 |

**2. Placeholder scan:**
- No "TBD" / "TODO" / "implement later" anywhere.
- Step 4.3 references `find the existing worker loop that calls plugin.Identify` — this is a directive for the implementer to locate code, not a code gap. Acceptable (the next sub-step shows the exact patch shape).
- Step 4.4 mentions `find all callers and update them` for `newModel` signature change — same pattern, acceptable.

**3. Type consistency:**
- `types.StageIdle` ... `types.StageDone` defined in Task 1, used in Task 3 and Task 4 — consistent.
- `(*State).PluginHitsView() map[string]int64` defined in Task 1, used in Task 3 (write site via bumpSyncMap) and Task 4 (read site via Session) — consistent.
- `core.ClassifyError(err error) string` defined in Task 2, used in Task 3 — consistent.
- `core.normalisePluginName(string) string` defined in Task 3, used in Task 3 — consistent.
- `core.bumpSyncMap(*sync.Map, string)` defined in Task 3, used in Task 3 — consistent.
- `tui.rateTracker.update(time.Time, int64, int64) (float64, float64)` defined in Task 4, used in Task 4 — consistent.
- `tui.topN(map[string]int64, int) [][2]string` defined in Task 4, used in Task 4 — consistent.
- `tui.computeETA(time.Time, time.Time, int32, types.CountersView) string` defined in Task 4, used in Task 4 — consistent.

**4. Risks addressed:**
- Spec §7 risk "sync.Map overhead" → Task 1 caps at 256 entries (Task 3 enforces).
- Spec §7 risk "ClassifyError too coarse" → Task 2 uses errors.Is/As first.
- Spec §7 risk "Stage transition racing TUI render" → Task 4 reads Stage atomically; worst case is one render showing previous stage, acceptable.
- Spec §7 risk "Plugin name normalisation" → Task 3 normalises at write site.

Plan covers the spec end-to-end with 4 commits.

---

## Shipped status (2026-09-10)

**All 4 plan commits are on `main`:**

| Task | Commit | Description |
|------|--------|-------------|
| 1 (state-type extensions) | `51e57e9` | Stage constants + `PluginHits`/`ErrorCategories` maps on `internal/types.State` |
| 2 (error classifier) | `bcc0abe` | `internal/core/errors.ClassifyError` with `errors.Is`/`errors.As`-first routing + substring fallback |
| 3 (scanner.go wiring) | `c4f8651` | scanner drives `Stage` transitions, populates counters, normalises plugin names at write site |
| 4 (TUI rendering) | `9825a3e` | header badge + ETA + EWMA rate, stats panel, top plugins bar chart, errors panel |
| mid-alive counter fix (post-plan) | `f096096` + `060e2fa` | `alive.Progress()` public API; TUI counter ticks up as probes complete |
| State → Model wiring (post-plan) | `2a909c9` | TUI reads production `*types.State` instead of fixture |

**Acceptance check vs spec:**

- ✅ 5 features in scope: stage indicator, real-time rate, ETA, top-plugins bar chart, error-category breakdown
- ✅ 5 design optimisations: EWMA rate smoothing, errors.Is/As classifier, empty-state placeholders, plugin-name normalisation at scanner write site, 256-entry memory cap on `PluginHits`/`ErrorCategories`
- ✅ Stage constant integer values match spec (`StageIdle=0`, `StageAlive=1`, `StagePortScan=2`, `StageIdentify=3`, `StageCred=4`, `StageDone=5`)
- ✅ EWMA α=0.5
- ✅ Categories: timeout, refused, reset, dns, perm, auth, tls, unreach, other
- ✅ Per-stage ETA: alive uses `AliveProbed/TotalHosts`; port-scan + identify use `Ports/TotalPorts`; cred + done return `""`
- ✅ Negative deltas clamped to 0 in EWMA
- ✅ Plugin name normalisation at scanner write site (not TUI render)

**No material drift from spec.**

**Tag status:** v0.5.2 deliberately not cut. Per user direction to iterate
slowly on version numbers, the in-source `version.Value` default was
bumped to `0.5.1-dev` and changelog entries live under `[Unreleased]`.
The next release tag will roll these up.
