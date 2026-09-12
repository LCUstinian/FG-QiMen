# TUI v2 — Spec B (Panel Layout) + Spec C (Visual Polish) — Design Spec

**Date:** 2026-09-12
**Author:** Claude (brainstorming session with LCUstinian)
**Target release:** v0.7.0 (post-v0.6.0)
**Branch:** `main`
**Scope:** Comprehensive TUI overhaul addressing 4 layout pain points + tasteful visual polish. Builds on Spec A (info density, v0.5.1/0.5.2) which shipped 5 features.

---

## 1. Background

Spec A (shipped 2026-09-10) gave the TUI the 5 information-density features: stage indicator, real-time rate, ETA, top-plugins bar chart, error-category breakdown. Operators using FG-QiMen in production have surfaced 4 remaining pain points with the current layout:

1. **Narrow-window collapse loses information.** When terminal width is `<100`, the two-column `STAGE | TOP PLUGINS` layout collapses to a single column and the `TOP PLUGINS` panel becomes invisible — operators on SSH sessions or split-tmux panes lose the plugin chart.
2. **No real-time event stream.** The TUI shows aggregate counters (alive 18/24, ports 142/8000, hits/s) but operators can't see *which* host:port just hit or failed. To debug a slow plugin, they have to read the post-scan log.
3. **Rate/EPS scattered across rows.** The `rate 142 pps · 28 hits/s` text in the header shows the *current* value but no trend. Operators can't tell if a scan is speeding up or slowing down.
4. **ERRORS panel overflows and truncates.** The bottom errors row is fixed at 1 line; high-concurrency scans push timeout/refused/dns/reset counts beyond the visible space and lose the *host:port* granularity.

Spec C (visual polish — colors, animations) was explicitly deferred from Spec A. After brainstorming, the user chose to ship Spec B + Spec C together in a single cycle (the two are naturally intertwined: progress bars need a `colorAccent`, severity colors need symbols to disambiguate).

The project philosophy ("pure scanner", "more features ≠ better", "iterate version numbers slowly") drives every design decision: no theme system, no animations beyond a 2-frame flash, no tabbed views. Tasteful, not flashy.

---

## 2. Goals

### Spec B — Layout (4 features)

1. **3-breakpoint responsive layout** (narrow `<80`, medium `80–119`, wide `≥120`). All 6 regions visible at every breakpoint; only the horizontal placement and density changes.
2. **LIVE EVENTS panel** — ring buffer of last 20 events (host:port + service + status symbol + timestamp), scrollable tail, full-width below header.
3. **Rate sparkline** — 60-sample ring buffer of hits/sec rendered as Unicode block-element sparkline (`▁▂▃▄▅▆▇█`) in the header.
4. **Collapsible ERRORS panel** — 1 row collapsed (default), 4 rows expanded (press `e`); `E` to clear.

### Spec C — Visual polish (4 features)

5. **Single dark theme + severity colors** — GitHub Dark palette; red/yellow/green/gray semantics for critical/info/success/metadata.
6. **Progress bars** — `▓/░` bars replace plain counters for `alive` and `ports` (the two with denominators).
7. **Status symbols + light animations** — `✓/✗/⚠/●/○/!!` symbols + Bubbletea spinner during scanning + 200ms flash on hit.
8. **Layout breakpoints visual polish** — `NormalBorder` everywhere, `ThickBorder` only in wide (two-column) mode; subtle divider color `#30363d`.

---

## 3. Non-goals

- ❌ Theme system / theme switching (one dark theme only)
- ❌ Modal screens / overlay layers (the design stays single-glance)
- ❌ Tabbed views (Overview | Events | Plugins | Errors)
- ❌ Mouse support (keyboard-only)
- ❌ Configurable breakpoints (fixed at 80 / 120)
- ❌ Layout DSL / YAML config (code-only — explicit Go functions)
- ❌ Italic typography (terminal support is unreliable)
- ❌ Smooth animations / gradients / theme-switching transitions
- ❌ Persisting TUI preferences between sessions (`--no-tui` already exists)
- ❌ Network bandwidth / throughput display (not in scope; future spec)
- ❌ Worker-utilization display (no instrumentation; future spec)

---

## 4. Architecture

### 4.1 File layout

```
internal/tui/
├── model.go        (state — ~400 lines, was intermixed in tui.go)
│   • Model struct (Bubbletea)
│   • Event ring buffer (last 20)
│   • Rate sample ring buffer (last 60)
│   • Flash state map (host:port → expiry)
│   • Breakpoint selection
│   • Region orchestration
├── view.go         (rendering — ~500 lines, was 1038)
│   • viewHeader(), viewLiveEvents(), viewStage(),
│     viewTopPlugins(), viewErrors(), viewFooter()
│   • View() composes regions
├── layout.go       (NEW — ~100 lines)
│   • Region placement math (3 breakpoints)
│   • Pure functions, easy to unit test
├── styles.go       (was 171 lines, +80 lines)
│   • Severity color helpers
│   • Status symbols
│   • Progress bar helper
│   • Sparkline helper
├── keymap.go       (NEW — ~30 lines)
│   • 'e' toggle errors, 'E' clear errors, 'L' live overlay (narrow), '?' help
└── layout_test.go  (extend existing 4 tests)
    • New tests per breakpoint
    • New tests for region placement
    • Golden-file tests for view output
```

### 4.2 Data model additions (`internal/tui/model.go`)

```go
package tui

import (
    "time"

    "github.com/charmbracelet/bubbles/spinner"
    tea "github.com/charmbracelet/bubbletea"
)

// Breakpoint is a width category. Pick via pickBreakpoint(width).
// / Breakpoint 是宽度分类。通过 pickBreakpoint(width) 选取。
type Breakpoint int

const (
    BreakNarrow Breakpoint = iota // <80 cols
    BreakMedium                   // 80–119 cols
    BreakWide                     // >=120 cols
)

// eventEntry is one row in the LIVE EVENTS panel.
// / eventEntry 是 LIVE EVENTS 面板的一行。
type eventEntry struct {
    Host    string
    Port    int
    Service string
    Kind    string // "hit" | "miss" | "error" | "warn"
    At      time.Time
}

// Model holds all TUI state. Pure data; rendering reads from it.
// / Model 持有所有 TUI state。纯数据；渲染从中读取。
type Model struct {
    // ... existing fields (counters, viewport, etc.) ...

    // v0.7.0 additions
    events      []eventEntry            // ring buffer, cap 20
    eventsCap   int                     // = cap(events)
    eventsHead  int                     // next write index
    eventsFull  bool                    // wrapped at least once

    rateSamples []float64               // hits/sec; cap 60
    rateCap     int                     // = cap(rateSamples)
    rateHead    int
    rateFull    bool

    flashUntil  map[string]time.Time    // "host:port" -> flash expiry (200ms)
    errorsExpanded bool                 // 'e' toggled errors panel

    spinner     spinner.Model            // Bubbletea spinner
    width       int                     // last seen terminal width (set by WindowSizeMsg)
    height      int                     // last seen terminal height (set by WindowSizeMsg)
    // Note: Breakpoint is computed on demand by pickBreakpoint(width)
    // in View(); no cached field needed — width changes already
    // trigger re-render via the Bubbletea WindowSizeMsg path.
    // / 注：Breakpoint 由 View() 中按需通过 pickBreakpoint(width) 算；
    // 不需要缓存字段——width 变化已通过 Bubbletea WindowSizeMsg 触发重渲染。
}

// pushEvent appends to the event ring buffer. If Kind=="hit", sets
// flash expiry for 200ms. / pushEvent 追加到事件 ring buffer。
// Kind=="hit" 时设 200ms flash 过期。
func (m *Model) pushEvent(e eventEntry) {
    if m.events == nil {
        m.events = make([]eventEntry, 20)
        m.eventsCap = 20
    }
    m.events[m.eventsHead] = e
    m.eventsHead = (m.eventsHead + 1) % m.eventsCap
    if m.eventsHead == 0 {
        m.eventsFull = true
    }
    if e.Kind == "hit" {
        key := fmt.Sprintf("%s:%d", e.Host, e.Port)
        if m.flashUntil == nil {
            m.flashUntil = make(map[string]time.Time)
        }
        m.flashUntil[key] = time.Now().Add(200 * time.Millisecond)
    }
}

// eventsOrdered iterates the ring buffer in chronological order
// (oldest -> newest), handling wrap. / eventsOrdered 按时间顺序
// （旧->新）迭代 ring buffer，处理回绕。
func (m *Model) eventsOrdered() []eventEntry {
    if !m.eventsFull {
        return m.events[:m.eventsHead]
    }
    out := make([]eventEntry, 0, m.eventsCap)
    out = append(out, m.events[m.eventsHead:]...)
    out = append(out, m.events[:m.eventsHead]...)
    return out
}

// recordRate pushes a hits/sec sample into the rate ring buffer.
// / recordRate 把 hits/sec 样本推入 rate ring buffer。
func (m *Model) recordRate(hitsPerSec float64) {
    if m.rateSamples == nil {
        m.rateSamples = make([]float64, 60)
        m.rateCap = 60
    }
    m.rateSamples[m.rateHead] = hitsPerSec
    m.rateHead = (m.rateHead + 1) % m.rateCap
    if m.rateHead == 0 {
        m.rateFull = true
    }
}

// rateOrdered iterates the rate buffer chronologically.
// / rateOrdered 按时间顺序迭代 rate buffer。
func (m *Model) rateOrdered() []float64 {
    if !m.rateFull {
        return m.rateSamples[:m.rateHead]
    }
    out := make([]float64, 0, m.rateCap)
    out = append(out, m.rateSamples[m.rateHead:]...)
    out = append(out, m.rateSamples[:m.rateHead]...)
    return out
}

// pruneExpiredFlashes removes flash entries whose expiry is in the
// past. Called from Update() on flashTickMsg to bound map growth.
// / pruneExpiredFlashes 删除过期的 flash 条目。由 flashTickMsg 在
// Update() 中调用以限制 map 增长。
func (m *Model) pruneExpiredFlashes(now time.Time) {
    if m.flashUntil == nil {
        return
    }
    for k, until := range m.flashUntil {
        if !now.Before(until) {
            delete(m.flashUntil, k)
        }
    }
}

// clearErrors zeroes per-category error counts and the top-errors
// rendering cache. Called from Update() on 'E' keypress.
// / clearErrors 清零每个类别的错误计数和 top-errors 渲染缓存。
// 由 'E' 按键在 Update() 中调用。
func (m *Model) clearErrors() {
    // Implementation detail: clear the snapshot used by viewErrors().
    // Per-category counts live in State.ErrorCategories (read-only
    // here); we only clear the rendered cache + close any expanded panel.
    m.errorsExpanded = false
}

// toEntry converts a runner.Event to a local eventEntry. Defines the
// wire format between runner and TUI. / toEntry 把 runner.Event 转换
// 为本地 eventEntry。定义 runner 与 TUI 之间的线格式。
func toEntry(e runner.Event) eventEntry {
    return eventEntry{
        Host:    e.Host,
        Port:    e.Port,
        Service: e.Service,
        Kind:    e.Kind, // runner emits "hit" | "miss" | "cred_success" | "warn" | "critical_hit"
        At:      e.At,
    }
}
```

### 4.3 Layout math (`internal/tui/layout.go`, NEW)

```go
package tui

// pickBreakpoint maps terminal width to a breakpoint.
// / pickBreakpoint 把终端宽度映射到 breakpoint。
func pickBreakpoint(width int) Breakpoint {
    switch {
    case width < 80:
        return BreakNarrow
    case width < 120:
        return BreakMedium
    default:
        return BreakWide
    }
}

// regions returns the height in lines for each of the 6 regions
// given terminal width and total height. Pure function; easy to test.
// Returned values are: header, events, stage, topPlugins, errors, footer.
// / regions 给定终端宽度和总高度，返回 6 个区域各占的行数。纯函数；
// 易于测试。返回值为：header, events, stage, topPlugins, errors, footer。
func regions(bp Breakpoint, width, totalHeight int) (header, events, leftCol, rightCol, errors, footer int) {
    footer = 1
    errors = 1 // collapsed by default; viewErrors() may grow to 4
    header = 1

    // Body height = total - header - errors - footer.
    body := totalHeight - header - errors - footer
    if body < 4 {
        body = 4 // minimum to keep STAGE + TOP PLUGINS usable
    }

    switch bp {
    case BreakNarrow:
        events = 0       // hidden; 'L' overlay shows last 5
        leftCol = body / 2
        rightCol = body - leftCol
    case BreakMedium:
        events = 8
        leftCol = body / 2
        rightCol = body - leftCol
    case BreakWide:
        events = 12
        // Two-column STAGE | TOP PLUGINS side by side
        leftCol = body
        rightCol = body
    }
    return
}
```

### 4.4 Subscribe pattern for events

```go
// model.go — Update() handles eventMsg from runner.EventStream.
// / model.go — Update() 处理来自 runner.EventStream 的 eventMsg。

type eventMsg struct {
    E runner.Event
}

func waitForEvent(sub <-chan runner.Event) tea.Cmd {
    return func() tea.Msg {
        return eventMsg{<-sub}
    }
}

// In Update():
//   case eventMsg:
//       m.pushEvent(toEntry(msg.E))
//       return m, waitForEvent(m.sub)  // re-subscribe
```

### 4.5 Rendering (`internal/tui/view.go`)

View() composes 6 region functions:

```go
func (m Model) View() string {
    bp := pickBreakpoint(m.width)
    h, ev, l, r, e, f := regions(bp, m.width, m.height)

    header := m.viewHeader(h, bp)
    events := m.viewLiveEvents(ev, bp)
    stage := m.viewStage(l, bp)
    topPlugins := m.viewTopPlugins(r, bp)
    errorsPanel := m.viewErrors(e)
    footer := m.viewFooter(f)

    if bp == BreakWide {
        // Two-column stage + top-plugins.
        body := lipgloss.JoinHorizontal(lipgloss.Top, stage, topPlugins)
        return lipgloss.JoinVertical(lipgloss.Left,
            header, events, body, errorsPanel, footer)
    }
    return lipgloss.JoinVertical(lipgloss.Left,
        header, events, stage, topPlugins, errorsPanel, footer)
}
```

### 4.6 Visual style (`internal/tui/styles.go`, +80 lines)

```go
package tui

import "github.com/charmbracelet/lipgloss"

// GitHub Dark palette — well-tested for legibility, contrast >=4.5:1.
// / GitHub Dark 调色板——可读性经过验证，对比度 >=4.5:1。
var (
    colorBg       = lipgloss.Color("#0e1116") // almost-black
    colorFg       = lipgloss.Color("#e6edf3") // off-white
    colorFgDim    = lipgloss.Color("#8b949e") // dimmed gray
    colorAccent   = lipgloss.Color("#58a6ff") // cyan-blue (brand)
    colorOk       = lipgloss.Color("#3fb950") // green (credential success)
    colorWarn     = lipgloss.Color("#d29922") // yellow (info hit / partial)
    colorErr      = lipgloss.Color("#f85149") // red (critical hit / default-creds)
    colorBorder   = lipgloss.Color("#30363d") // subtle dividers
)

// Symbol table — no emoji; terminal fallback safe.
// / 符号表——无 emoji；terminal 回退安全。
const (
    symCriticalHit  = "!!" // default-creds accepted (red flag)
    symInfoHit      = "✓"  // service identified (yellow)
    symCredSuccess  = "✓✓" // credential success (green)
    symMiss         = "✗"  // refused/timeout/dns (gray)
    symWarn         = "⚠"  // partial / TLS handshake fail
    symScanning     = "..." // spinner uses Bubbletea spinner.Model
    symDone         = "●"
    symIdle         = "○"
)

// severityColor returns the right color for an event kind. Honors
// flash expiry: if host:port is currently flashing, return colorErr.
// / severityColor 返回事件类型对应的颜色。遵循 flash 过期：如果
// host:port 当前在 flash，返回 colorErr。
func (m Model) severityColor(e eventEntry) lipgloss.Color {
    key := fmt.Sprintf("%s:%d", e.Host, e.Port)
    if until, ok := m.flashUntil[key]; ok && time.Now().Before(until) {
        return colorErr
    }
    switch e.Kind {
    case "cred_success":
        return colorOk
    case "hit":
        return colorWarn
    case "warn":
        return colorWarn
    case "miss":
        return colorFgDim
    default:
        return colorFg
    }
}

// symFor returns the status symbol for an event kind.
// / symFor 返回事件类型对应的状态符号。
func symFor(kind string) string {
    switch kind {
    case "critical_hit":
        return symCriticalHit
    case "hit":
        return symInfoHit
    case "cred_success":
        return symCredSuccess
    case "warn":
        return symWarn
    case "miss":
        return symMiss
    default:
        return symIdle
    }
}

// truncate limits s to n runes, appending "..." if truncated. Stays
// safe on multi-byte UTF-8 boundaries (cuts at rune boundary, not byte).
// / truncate 限制 s 到 n 个 rune，超出加 "..."。UTF-8 安全（在
// rune 边界切，不是字节）。
func truncate(s string, n int) string {
    if n <= 0 {
        return ""
    }
    runes := []rune(s)
    if len(runes) <= n {
        return s
    }
    if n <= 3 {
        return string(runes[:n])
    }
    return string(runes[:n-3]) + "..."
}

// renderBar renders a progress bar of width w filled to ratio.
// / renderBar 渲染宽度 w、按 ratio 填充的进度条。
func renderBar(filled, total, w int) string {
    if w <= 0 || total <= 0 {
        return strings.Repeat("░", w)
    }
    ratio := float64(filled) / float64(total)
    if ratio > 1 {
        ratio = 1
    }
    if ratio < 0 {
        ratio = 0
    }
    f := int(float64(w) * ratio)
    return strings.Repeat("▓", f) + strings.Repeat("░", w-f)
}

// sparkline returns a string of `width` Unicode block-element glyphs
// representing the samples normalized to [0, 1]. Pure function.
// / sparkline 返回 `width` 个 Unicode 块元素字符的字符串，表示归一
// 化到 [0, 1] 的样本。纯函数。
func sparkline(samples []float64, width int) string {
    glyphs := []rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}
    if len(samples) == 0 || width <= 0 {
        return ""
    }
    // Take last `width` samples.
    n := len(samples)
    if n > width {
        samples = samples[n-width:]
        n = width
    }
    // Find max for normalization.
    max := 0.0
    for _, s := range samples {
        if s > max {
            max = s
        }
    }
    if max == 0 {
        return strings.Repeat(string(glyphs[0]), width)
    }
    // Pad with lowest glyph if we have fewer samples than width.
    out := make([]rune, width)
    pad := width - n
    for i := 0; i < pad; i++ {
        out[i] = glyphs[0]
    }
    for i, s := range samples {
        idx := int(float64(len(glyphs)-1) * s / max)
        if idx >= len(glyphs) {
            idx = len(glyphs) - 1
        }
        out[pad+i] = glyphs[idx]
    }
    return string(out)
}
```

### 4.7 Keymap (`internal/tui/keymap.go`, NEW)

```go
package tui

import "github.com/charmbracelet/bubbles/key"

// Keymap is the binding table for v0.7.0 additions. Existing
// bindings (q, p, arrows) are preserved in the main Model.Update.
// / Keymap 是 v0.7.0 新增的绑定表。已有绑定（q、p、箭头）在主
// Model.Update 中保留。
type Keymap struct {
    ToggleErrors key.Binding
    ClearErrors  key.Binding
    LiveOverlay  key.Binding // narrow mode only
    Help         key.Binding
}

func DefaultKeymap() Keymap {
    return Keymap{
        ToggleErrors: key.NewBinding(key.WithKeys("e"),
            key.WithHelp("e", "toggle errors panel")),
        ClearErrors: key.NewBinding(key.WithKeys("E"),
            key.WithHelp("E", "clear errors")),
        LiveOverlay: key.NewBinding(key.WithKeys("L"),
            key.WithHelp("L", "live events overlay (narrow)")),
        Help: key.NewBinding(key.WithKeys("?"),
            key.WithHelp("?", "toggle help")),
    }
}
```

### 4.8 Header layout per breakpoint

**Narrow (<80):**
```
[ ▶ IDENTIFY ] 18/24 142/8000 28h/s ▁▂▃▅▇▅▃▂▁
```

**Medium (80–119):**
```
[ ▶ IDENTIFY ]  ETA ~12s  alive 18/24  ports 142/8000  rate 142 pps · 28 hits/s  ▁▂▃▅▇▅▃▂▁
```

**Wide (≥120):**
```
[ ▶ IDENTIFY ]  ETA ~12s  alive 18/24  ports 142/8000  rate 142 pps · 28 hits/s  uptime 18s  ▁▂▃▅▇▅▃▂▁
```

Sparkline is **always last** in header; width-trimmed; hidden if header would drop below 40 chars.

### 4.9 Live events rendering

```go
func (m Model) viewLiveEvents(height int, bp Breakpoint) string {
    if height == 0 {
        return "" // narrow mode: hidden
    }
    events := m.eventsOrdered()
    if len(events) == 0 {
        return lipgloss.NewStyle().
            Foreground(colorFgDim).
            Render("  (no events yet)")
    }
    // Take last `height` events, newest at bottom.
    n := len(events)
    start := 0
    if n > height {
        start = n - height
    }
    var rows []string
    for _, e := range events[start:] {
        c := m.severityColor(e)
        sym := symFor(e.Kind)
        ts := e.At.Format("15:04:05")
        hostPort := truncate(fmt.Sprintf("%s:%d", e.Host, e.Port), 21)
        svc := truncate(e.Service, 12)
        rows = append(rows, lipgloss.NewStyle().Foreground(c).Render(
            fmt.Sprintf("  [%s] %s %s %s", ts, sym, hostPort, svc)))
    }
    return strings.Join(rows, "\n")
}
```

### 4.10 Bubbletea tick management

```go
// In Model.Init():
func (m Model) Init() tea.Cmd {
    return tea.Batch(
        flashTick(),
        rateTick(),
        waitForEvent(m.sub),
    )
}

type flashTickMsg time.Time
type rateTickMsg time.Time

// flashTick fires every 100ms to prune expired flash entries and
// trigger re-render. / flashTick 每 100ms 触发一次，清除过期
// flash 条目并触发重渲染。
func flashTick() tea.Cmd {
    return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {
        return flashTickMsg{t}
    })
}

// rateTick fires every 1s to sample current rate into the sparkline
// ring buffer. / rateTick 每 1s 触发一次，把当前速率采样到 sparkline
// ring buffer。
func rateTick() tea.Cmd {
    return tea.Tick(1*time.Second, func(t time.Time) tea.Msg {
        return rateTickMsg{t}
    })
}

// In Update():
//   case flashTickMsg:
//       m.pruneExpiredFlashes(time.Time(msg))
//       return m, flashTick() // re-arm
//   case rateTickMsg:
//       m.recordRate(m.rateHits) // from existing rateTracker
//       return m, rateTick()
```

### 4.11 Errors panel collapsible

```go
func (m Model) viewErrors(height int) string {
    n := 1 // collapsed default
    if m.errorsExpanded && height >= 4 {
        n = 4
    }
    // Render up to n rows of top error categories by count.
    // If collapsed: single row "ERRORS: timeout 42 refused 15 dns 7 reset 3"
    // If expanded: 4 rows with severity-coloured bars.
    // ...
}

// Update():
//   case tea.KeyMsg:
//       switch msg.String() {
//       case "e":
//           m.errorsExpanded = !m.errorsExpanded
//       case "E":
//           m.clearErrors()
//       }
```

---

## 5. File impact summary

**Modify:**
- `internal/tui/tui.go` — split into `model.go`, `view.go`, `layout.go`, `keymap.go` (no behavior change, just file decomposition).
- `internal/tui/styles.go` — add severity colors, status symbols, `renderBar`, `sparkline`, `severityColor`, `symFor`.
- `internal/tui/layout_test.go` — extend with breakpoint + region tests.
- `internal/tui/program.go` — wire new ticker commands (`flashTick`, `rateTick`); pass runner.EventStream subscription into Model.
- `README.md` + `README.zh-CN.md` — replace ASCII diagram with new layout.
- `CHANGELOG.md` + `.zh-CN.md` — add `[Unreleased] v0.7.0 TUI v2 Spec B+C` entries under `[Changed]`.

**Create:**
- `internal/tui/model.go` — Model struct + ring buffer methods.
- `internal/tui/view.go` — region renderers + View() composition.
- `internal/tui/layout.go` — pickBreakpoint + regions (pure functions).
- `internal/tui/keymap.go` — Keymap struct + DefaultKeymap.
- `internal/tui/model_test.go` — TestEventRingBuffer, TestRateRingBuffer, TestPushEventSetsFlash.
- `internal/tui/layout_test.go` (extend) — TestPickBreakpoint, TestRegions_AllBreakpoints.
- `internal/tui/view_test.go` — TestRenderBar, TestSparkline, TestTruncate, golden-file tests.
- `internal/tui/program_test.go` (extend) — TestEventSubscriptionWiresThrough, TestFlashDecaysAfter200ms.
- `docs/verification/v0.7.0-tui-v2-bc/verification.md` — post-implementation verification report.

**Not modified:**
- `internal/runner/*` — already emits `runner.Event` on a channel; TUI just subscribes.
- `internal/core/*` — no scanning behavior changes.
- `internal/types/*` — no new state; TUI reads from existing `runner.Event` + `State.Snapshot()`.
- Any plugin code.
- CLI flags (`--no-tui`, `--alive-format`, etc.).

---

## 6. Acceptance criteria

1. `gofmt -l internal/tui/` returns no diffs.
2. `go vet ./internal/tui/` clean.
3. `golangci-lint run ./internal/tui/` clean.
4. `go test -race -count=3 ./internal/tui/` green (Windows race-DLL known issue excepted).
5. `go test ./internal/tui/` covers `internal/tui/` to >=60% line coverage (PER_PLUGIN_FLOOR from `ci-coverage-check.py`).
6. **Layout tests:**
   - `TestPickBreakpoint`: widths 0, 79 → BreakNarrow; 80, 119 → BreakMedium; 120, 200 → BreakWide.
   - `TestRegions_AllBreakpoints`: for each (breakpoint, width, height) in [(Narrow, 60, 20), (Medium, 100, 30), (Wide, 140, 40)], `regions()` returns 6 non-negative heights that sum to ≤ total height.
7. **Ring buffer tests:**
   - `TestEventRingBuffer`: push 25 into cap-20 → head=5, full=true, ordered iteration yields the 5 most-recent events.
   - `TestRateRingBuffer`: same for rate samples.
8. **Visual tests:**
   - `TestRenderBar`: (0,10,10)→10 `░`; (5,10,10)→5 `▓`+5 `░`; (10,10,10)→10 `▓`.
   - `TestSparkline`: known sample vector `[1,2,3,4,5,6,7,8]` at width 8 → `"▁▂▃▄▅▆▇█"`; max=0 → all `▁`.
   - Golden-file tests in `testdata/view_*.txt` for: (narrow/medium/wide) × (idle/scanning/done) — 9 snapshots total.
9. **Integration test:**
   - `TestEventSubscriptionWiresThrough`: drive Model with mock channel emitting 5 events; assert ring buffer holds all 5; `flashUntil` contains entries with expiry within 200ms±50ms.
   - `TestFlashDecaysAfter200ms`: emit one hit event, advance fake clock 250ms, verify `severityColor()` returns default (not `colorErr`).
10. **Behavior preservation:**
    - Existing keymaps (`q` quit, `p` pause, `↑/↓` nav) continue to work.
    - `--no-tui` flag still gates the entire TUI.
    - Same shutdown semantics (Ctrl-C, q).
    - Same on-disk output files (`--alive-format` unchanged).
11. **Operator smoke test** (manual, runs against `/24` real targets):
    - At 60 cols: LIVE EVENTS panel hidden by default; `L` shows last 5 events overlay.
    - At 100 cols: LIVE EVENTS panel shows last 8 events; STAGE + TOP PLUGINS stacked vertically.
    - At 140 cols: LIVE EVENTS panel shows last 12 events; STAGE | TOP PLUGINS side-by-side.
    - At all widths: pressing `e` toggles errors panel between 1 row and 4 rows; `E` clears errors.
    - Header shows sparkline updating each second over a 60-sample window.
    - Counter bars (`alive`, `ports`) use `▓/░` glyphs.
    - Default-creds-accepted events render in red for 200ms then decay to severity color.

---

## 7. Risks & mitigations

| Risk | Likelihood | Mitigation |
|---|---|---|
| Sparkline width pressure on narrow header | medium | Hide sparkline if header < 40 chars; otherwise clip to `width-1` chars at the right edge. |
| Golden-file tests fragile to Lipgloss version changes | medium | Pin `charmbracelet/lipgloss` version in `go.mod` with a comment explaining the constraint. Add `-update` flag for intentional visual changes. |
| Flash decay not exact at terminal frame rate | low | 200ms expiry + 100ms tick = at most 2 frames of red; acceptable. |
| Event channel blocking the Bubbletea program | low | Subscribe pattern uses non-blocking `tea.Cmd`; if channel is slow, events are batched (lossy OK for visualisation). |
| Lipgloss JoinHorizontal width math off-by-one | medium | `regions()` returns heights only; widths come from `lipgloss` defaults. Add visual snapshot tests. |
| Spec C adds visual surface area for bugs | medium | Golden-file tests + smoke test from §6.11 catch regressions. |
| Bubbletea spinner noise in test output | low | Disable spinner in tests via a flag on Model. |

---

## 8. Open questions / deferred decisions

- **`L` keymap for narrow overlay** — does not conflict with current keys, but reserved for future plugins. No blocker.
- **Sparkline width** — fixed at 20 chars (header right-side pressure allows auto-shrink). Will tune during implementation.
- **Flash color** — red on ALL hits for 200ms, then decay to severity color (per §4.6). Tunable; not a blocker.
- **Errors expanded mode** — 4 rows is provisional; may need more rows for high-error scans. Decision deferred to implementation phase based on observation.

---

## 9. Status

**Drafted:** 2026-09-12 via brainstorming session (user approved all 5 design sections).
**Next:** writing-plans skill to decompose into an implementation plan; ship in v0.7.0 (target Q4 2026, post-v0.6.0).

This spec follows the v0.5.1 wrap-up + Spec A pattern: 4 layout features + 4 visual features in one coherent cycle, with explicit YAGNI list and `pure scanner` philosophy honored throughout. The deferred items in §3 are listed so future specs (D, E, F) can pick them up cleanly without re-litigating scope.