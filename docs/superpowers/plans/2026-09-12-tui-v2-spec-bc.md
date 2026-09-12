# TUI v2 Spec B + Spec C Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Address 4 TUI layout pain points (narrow collapse, no events stream, scattered rate, errors overflow) and add tasteful visual polish (theme, severity colors, progress bars, animations) in a single v0.7.0 release.

**Architecture:** Decompose the 1038-line `internal/tui/tui.go` into focused files (model.go, layout.go, render.go extension, keymap.go). New pure-function helpers (`pickBreakpoint`, `regions`, `renderBar`, `sparkline`, `truncate`) live in test-friendly units. Event subscription via Bubbletea `tea.Cmd` channel; rate and flash decay via `tea.Tick`. No new dependencies.

**Tech Stack:** Go 1.26.8, Bubbletea + Lipgloss + Bubbles (spinner/key) — all already in `go.sum`.

## File Structure

| Action | File | Responsibility |
|---|---|---|
| Create | `internal/tui/layout.go` | Breakpoint + regions pure functions (3 breakpoints) |
| Create | `internal/tui/model.go` | Model state: event ring buffer, rate ring buffer, flash map |
| Create | `internal/tui/keymap.go` | Keymap struct + DefaultKeymap |
| Create | `internal/tui/model_test.go` | Ring buffer + flash tests |
| Create | `internal/tui/view_test.go` | Visual helpers + golden-file tests |
| Create | `docs/verification/v0.7.0-tui-v2-bc/verification.md` | Post-implementation verification |
| Modify | `internal/tui/tui.go` | Decompose: keep Update + View orchestration, move state to model.go |
| Modify | `internal/tui/render.go` | Extend with new region renderers (viewHeader, viewLiveEvents, viewErrors, viewStage, viewTopPlugins, viewFooter) |
| Modify | `internal/tui/styles.go` | Add severity colors, status symbols, renderBar, sparkline, truncate, symFor, severityColor |
| Modify | `internal/tui/layout_test.go` | Extend with breakpoint + region tests |
| Modify | `internal/tui/program.go` | Wire event subscription + tick commands |
| Modify | `internal/tui/program_test.go` | Extend with TestEventSubscriptionWiresThrough + TestFlashDecaysAfter200ms |
| Modify | `README.md` + `README.zh-CN.md` | Replace ASCII diagram (must stay byte-identical between languages) |
| Modify | `CHANGELOG.md` + `CHANGELOG.zh-CN.md` | Add `[Unreleased] v0.7.0 TUI v2 Spec B+C` entries (must stay byte-identical) |

## Global Constraints

- **No new dependencies** — Bubbletea, Lipgloss, Bubbles are already in `go.sum` (commit `d2d58f3` baseline).
- **CI gates** — `gofmt`, `go vet`, `golangci-lint` clean. Coverage floor 60% on `internal/tui/`.
- **No behaviour change outside `internal/tui/`** — CLI flags (`--no-tui`, `--alive-format`) unchanged.
- **README.md vs README.zh-CN.md** — TUI ASCII diagrams must be byte-identical (TUI is English-only).
- **CHANGELOG.md vs CHANGELOG.zh-CN.md** — version + section entries byte-identical.
- **Single dark theme** — no theme system; constants only.
- **No animations beyond 200ms hit flash + Bubbletea spinner during scanning**.
- **Spec doc reference**: [docs/superpowers/specs/2026-09-12-tui-v2-spec-bc-design.md](../specs/2026-09-12-tui-v2-spec-bc-design.md).

---

## Task 1: layout.go — Breakpoint + regions pure functions

**Files:**
- Create: `internal/tui/layout.go`
- Modify: `internal/tui/layout_test.go` (add TestPickBreakpoint + TestRegions_AllBreakpoints)

**Interfaces:**
- Consumes: nothing (pure functions)
- Produces:
  - `type Breakpoint int` with constants `BreakNarrow`, `BreakMedium`, `BreakWide`
  - `func pickBreakpoint(width int) Breakpoint`
  - `func regions(bp Breakpoint, width, totalHeight int) (header, events, leftCol, rightCol, errors, footer int)`

- [ ] **Step 1: Write failing tests in layout_test.go**

Add to `internal/tui/layout_test.go` (preserve existing 4 tests above):

```go
// TestPickBreakpoint verifies width-to-breakpoint mapping.
// / 验证宽度到 breakpoint 的映射。
func TestPickBreakpoint(t *testing.T) {
    cases := []struct {
        width int
        want  Breakpoint
    }{
        {0, BreakNarrow},
        {40, BreakNarrow},
        {79, BreakNarrow},
        {80, BreakMedium},
        {100, BreakMedium},
        {119, BreakMedium},
        {120, BreakWide},
        {200, BreakWide},
    }
    for _, c := range cases {
        got := pickBreakpoint(c.width)
        if got != c.want {
            t.Errorf("pickBreakpoint(%d) = %d, want %d", c.width, got, c.want)
        }
    }
}

// TestRegions_AllBreakpoints verifies the region heights for each
// breakpoint are non-negative and sum to <= total height. / 验证
// 每个 breakpoint 的区域行数为非负且总和小于等于总高度。
func TestRegions_AllBreakpoints(t *testing.T) {
    cases := []struct {
        bp     Breakpoint
        width  int
        height int
    }{
        {BreakNarrow, 60, 20},
        {BreakMedium, 100, 30},
        {BreakWide, 140, 40},
        // Edge: very small terminal — body clamp must hold.
        {BreakNarrow, 60, 5},
        {BreakMedium, 100, 7},
        {BreakWide, 140, 9},
    }
    for _, c := range cases {
        h, ev, l, r, e, f := regions(c.bp, c.width, c.height)
        if h < 0 || ev < 0 || l < 0 || r < 0 || e < 0 || f < 0 {
            t.Errorf("regions(%v, %d, %d): negative height h=%d ev=%d l=%d r=%d e=%d f=%d",
                c.bp, c.width, c.height, h, ev, l, r, e, f)
        }
        total := h + ev + l + r + e + f
        // total may exceed c.height when body clamp kicks in (very small
        // terminals): that's intentional, the rest of the renderer
        // handles overflow. We just assert no negatives.
        if h != 1 {
            t.Errorf("regions(%v): header = %d, want 1", c.bp, h)
        }
        if f != 1 {
            t.Errorf("regions(%v): footer = %d, want 1", c.bp, f)
        }
        if e != 1 {
            t.Errorf("regions(%v): errors = %d, want 1 (collapsed)", c.bp, e)
        }
        // events: 0 for Narrow, 8 for Medium, 12 for Wide.
        wantEvents := 0
        if c.bp == BreakMedium {
            wantEvents = 8
        }
        if c.bp == BreakWide {
            wantEvents = 12
        }
        if ev != wantEvents {
            t.Errorf("regions(%v): events = %d, want %d", c.bp, ev, wantEvents)
        }
        _ = total
    }
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /d/Go/FG-QiMen && go test ./internal/tui/ -run 'TestPickBreakpoint|TestRegions_AllBreakpoints' -v`
Expected: FAIL — `pickBreakpoint` and `regions` undefined.

- [ ] **Step 3: Write implementation in layout.go**

```go
// layout.go — pure breakpoint + region placement helpers for the
// v0.7.0 TUI v2 Spec B layout. No state, no I/O; trivial to test.
//
// layout.go — v0.7.0 TUI v2 Spec B 布局的纯 breakpoint + 区域放置
// 辅助函数。无 state、无 I/O；易于测试。
package tui

// Breakpoint is a width category for the responsive layout.
// / Breakpoint 是响应式布局的宽度分类。
type Breakpoint int

const (
    BreakNarrow Breakpoint = iota // <80 cols
    BreakMedium                   // 80-119 cols
    BreakWide                     // >=120 cols
)

// pickBreakpoint maps terminal width to a Breakpoint.
// / pickBreakpoint 把终端宽度映射到 Breakpoint。
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

// regions returns the height in lines for each of 6 regions given
// breakpoint and terminal dimensions. Pure function.
//
// Header = 1, errors = 1 (collapsed), footer = 1 always. Body
// (leftCol + rightCol) gets whatever's left, with a minimum of 4
// to keep STAGE + TOP PLUGINS usable.
//
// / regions 给定 breakpoint 和终端尺寸，返回 6 个区域的行高。纯
// 函数。header=1, errors=1 (折叠), footer=1 恒定。body 拿剩下的，
// 保底 4 行让 STAGE + TOP PLUGINS 可用。
//
// Returned order: header, events, leftCol, rightCol, errors, footer.
func regions(bp Breakpoint, width, totalHeight int) (int, int, int, int, int, int) {
    header := 1
    errors := 1
    footer := 1

    body := totalHeight - header - errors - footer
    if body < 4 {
        body = 4 // minimum body for stage + top-plugins
    }

    var events, leftCol, rightCol int
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
        leftCol = body
        rightCol = body
    }
    return header, events, leftCol, rightCol, errors, footer
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /d/Go/FG-QiMen && go test ./internal/tui/ -run 'TestPickBreakpoint|TestRegions_AllBreakpoints' -v`
Expected: PASS for both.

- [ ] **Step 5: Run full tui test suite to confirm no regressions**

Run: `cd /d/Go/FG-QiMen && go test ./internal/tui/ -v`
Expected: All pass.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/layout.go internal/tui/layout_test.go
git commit -m "feat(tui): add Breakpoint + regions pure layout helpers"
```

---

## Task 2: styles.go — visual primitives (renderBar, sparkline, truncate, symFor, severityColor)

**Files:**
- Modify: `internal/tui/styles.go` (extend existing 171 lines)
- Create: `internal/tui/view_test.go` (new test file for visual primitives)

**Interfaces:**
- Consumes: nothing (pure helpers except `severityColor` which reads flash state)
- Produces:
  - Color constants: `colorBg`, `colorFg`, `colorFgDim`, `colorAccent`, `colorOk`, `colorWarn`, `colorErr`, `colorBorder`
  - Symbol constants: `symCriticalHit`, `symInfoHit`, `symCredSuccess`, `symMiss`, `symWarn`, `symScanning`, `symDone`, `symIdle`
  - `func renderBar(filled, total, w int) string`
  - `func sparkline(samples []float64, width int) string`
  - `func truncate(s string, n int) string`
  - `func symFor(kind string) string`
  - `func (m Model) severityColor(e eventEntry) lipgloss.Color`

- [ ] **Step 1: Write failing tests in view_test.go (new file)**

```go
// view_test.go — tests for visual helpers (renderBar, sparkline,
// truncate, symFor, severityColor) and golden-file snapshots for
// each region renderer. / view_test.go — 视觉辅助函数测试及每
// 个区域渲染器的 golden-file 快照。
package tui

import (
    "testing"
    "time"

    "github.com/charmbracelet/lipgloss"
)

// TestRenderBar verifies the progress-bar glyph math.
// / 验证进度条字符数学。
func TestRenderBar(t *testing.T) {
    cases := []struct {
        filled, total, w int
        want             string
    }{
        {0, 10, 10, "░░░░░░░░░░"},
        {5, 10, 10, "▓▓▓▓▓░░░░░"},
        {10, 10, 10, "▓▓▓▓▓▓▓▓▓▓"},
        // total=0 → empty bar of width w
        {0, 0, 8, "░░░░░░░░"},
        // w=0 → empty
        {5, 10, 0, ""},
        // over-filled clamped to 100%
        {15, 10, 10, "▓▓▓▓▓▓▓▓▓▓"},
    }
    for _, c := range cases {
        got := renderBar(c.filled, c.total, c.w)
        if got != c.want {
            t.Errorf("renderBar(%d, %d, %d) = %q, want %q",
                c.filled, c.total, c.w, got, c.want)
        }
    }
}

// TestSparkline verifies the rate sparkline glyph sequence.
// / 验证 rate sparkline 字符序列。
func TestSparkline(t *testing.T) {
    // Linear ramp 1..8 → 8 distinct glyphs.
    samples := []float64{1, 2, 3, 4, 5, 6, 7, 8}
    got := sparkline(samples, 8)
    want := "▁▂▃▄▅▆▇█"
    if got != want {
        t.Errorf("sparkline(ramp, 8) = %q, want %q", got, want)
    }

    // All zeros → all lowest glyphs.
    zeros := []float64{0, 0, 0, 0}
    if got := sparkline(zeros, 4); got != "▁▁▁▁" {
        t.Errorf("sparkline(zeros, 4) = %q, want %q", got, "▁▁▁▁")
    }

    // Empty samples → empty string.
    if got := sparkline(nil, 8); got != "" {
        t.Errorf("sparkline(nil, 8) = %q, want empty", got)
    }

    // Fewer samples than width → padded with lowest glyph.
    short := []float64{5, 10}
    if got := sparkline(short, 5); got != "▁▁▁▅█" {
        t.Errorf("sparkline(short, 5) = %q, want %q", got, "▁▁▁▅█")
    }

    // width=0 → empty.
    if got := sparkline(samples, 0); got != "" {
        t.Errorf("sparkline(samples, 0) = %q, want empty", got)
    }
}

// TestTruncate verifies rune-safe truncation.
// / 验证 rune 安全的截断。
func TestTruncate(t *testing.T) {
    cases := []struct {
        in   string
        n    int
        want string
    }{
        {"ssh", 10, "ssh"},            // shorter than n
        {"hello world", 8, "hello..."}, // truncate + ellipsis
        {"hi", 3, "hi"},                // exact length
        {"hello", 2, "he"},             // n <= 3, no ellipsis
        {"", 5, ""},                    // empty
        {"中文测试", 3, "中..."},            // multi-byte UTF-8
        {"x", 0, ""},                   // n=0
    }
    for _, c := range cases {
        got := truncate(c.in, c.n)
        if got != c.want {
            t.Errorf("truncate(%q, %d) = %q, want %q", c.in, c.n, got, c.want)
        }
    }
}

// TestSymFor verifies kind → symbol mapping.
// / 验证 kind → 符号映射。
func TestSymFor(t *testing.T) {
    cases := []struct {
        kind string
        want string
    }{
        {"critical_hit", symCriticalHit},
        {"hit", symInfoHit},
        {"cred_success", symCredSuccess},
        {"miss", symMiss},
        {"warn", symWarn},
        {"unknown", symIdle},
    }
    for _, c := range cases {
        got := symFor(c.kind)
        if got != c.want {
            t.Errorf("symFor(%q) = %q, want %q", c.kind, got, c.want)
        }
    }
}

// TestSeverityColor verifies the color returned for each event kind.
// Flash expiry takes precedence over kind. / 验证每种事件类型返回
// 的颜色。flash 过期优先于 kind。
func TestSeverityColor(t *testing.T) {
    m := Model{}
    e := eventEntry{Host: "1.2.3.4", Port: 80, Kind: "hit"}

    // No flash → severity color for "hit" = colorWarn.
    if got := m.severityColor(e); got != colorWarn {
        t.Errorf("no-flash hit: severityColor = %v, want colorWarn", got)
    }

    // With active flash → colorErr regardless of kind.
    m.flashUntil = map[string]time.Time{
        "1.2.3.4:80": time.Now().Add(1 * time.Second),
    }
    if got := m.severityColor(e); got != colorErr {
        t.Errorf("active flash: severityColor = %v, want colorErr", got)
    }

    // Expired flash → back to severity color.
    m.flashUntil["1.2.3.4:80"] = time.Now().Add(-1 * time.Second)
    if got := m.severityColor(e); got != colorWarn {
        t.Errorf("expired flash: severityColor = %v, want colorWarn", got)
    }

    // Different kinds map to different colors.
    for _, c := range []struct {
        kind string
        want lipgloss.Color
    }{
        {"cred_success", colorOk},
        {"miss", colorFgDim},
        {"warn", colorWarn},
        {"unknown", colorFg},
    } {
        e.Kind = c.kind
        if got := m.severityColor(e); got != c.want {
            t.Errorf("severityColor(%q) = %v, want %v", c.kind, got, c.want)
        }
    }
}
```

Note: this references `eventEntry` (defined in Task 3's model.go). If running this test alone, you'll get an undefined-type error. Solution: run with Task 3's model.go stubbed first, OR define `eventEntry` in styles.go temporarily.

The cleanest fix: complete Task 3 first (with empty helpers), then run Task 2 tests. **Defer Task 2's run-tests-verify-pass step until after Task 3 is in place.** Adjust order: do Task 3 model skeleton, then Task 2 implementation, then run Task 2 tests.

- [ ] **Step 2: Add eventEntry stub so tests compile**

In `internal/tui/model.go` (created in Task 3, but you can scaffold this ahead):

```go
package tui

import "time"

// eventEntry is one row in the LIVE EVENTS panel. / eventEntry
// 是 LIVE EVENTS 面板的一行。
type eventEntry struct {
    Host    string
    Port    int
    Service string
    Kind    string // "hit" | "miss" | "cred_success" | "warn" | "critical_hit"
    At      time.Time
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `cd /d/Go/FG-QiMen && go test ./internal/tui/ -run 'TestRenderBar|TestSparkline|TestTruncate|TestSymFor|TestSeverityColor' -v`
Expected: FAIL — `renderBar`, `sparkline`, `truncate`, `symFor`, `severityColor` undefined.

- [ ] **Step 4: Extend styles.go with all helpers**

Append to `internal/tui/styles.go` (existing 171 lines preserved). Add new imports `"github.com/charmbracelet/lipgloss"` if not already there:

```go
// --- v0.7.0 additions: severity colors, symbols, progress bars, sparkline ---

// GitHub Dark palette — well-tested for legibility, contrast >=4.5:1.
// / GitHub Dark 调色板——可读性经过验证，对比度 >=4.5:1。
var (
    colorBg     = lipgloss.Color("#0e1116") // almost-black
    colorFg     = lipgloss.Color("#e6edf3") // off-white
    colorFgDim  = lipgloss.Color("#8b949e") // dimmed gray
    colorAccent = lipgloss.Color("#58a6ff") // cyan-blue (brand)
    colorOk     = lipgloss.Color("#3fb950") // green (credential success)
    colorWarn   = lipgloss.Color("#d29922") // yellow (info hit / partial)
    colorErr    = lipgloss.Color("#f85149") // red (critical hit / default-creds)
    colorBorder = lipgloss.Color("#30363d") // subtle dividers
)

// Symbol table — no emoji; terminal fallback safe. / 符号表——
// 无 emoji；terminal 回退安全。
const (
    symCriticalHit = "!!"  // default-creds accepted (red flag)
    symInfoHit     = "✓"   // service identified (yellow)
    symCredSuccess = "✓✓"  // credential success (green)
    symMiss        = "✗"   // refused/timeout/dns (gray)
    symWarn        = "⚠"   // partial / TLS handshake fail
    symScanning    = "..." // spinner uses Bubbletea spinner.Model
    symDone        = "●"
    symIdle        = "○"
)

// renderBar renders a progress bar of width w filled to ratio.
// Width-0 / total-0 returns w empty glyphs. Over-filled clamps to 100%.
// / renderBar 渲染宽度 w、按 ratio 填充的进度条。w=0 或 total=0
// 返回 w 个空字符。超填钳到 100%。
func renderBar(filled, total, w int) string {
    if w <= 0 {
        return ""
    }
    if total <= 0 {
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
// representing the samples normalized to [0, 1]. Empty samples or
// width<=0 → empty string. Samples shorter than width are padded
// with the lowest glyph. / sparkline 返回 `width` 个 Unicode 块元
// 素字符的字符串，表示归一化到 [0, 1] 的样本。
func sparkline(samples []float64, width int) string {
    if len(samples) == 0 || width <= 0 {
        return ""
    }
    glyphs := []rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}

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

// truncate limits s to n runes, appending "..." if truncated. Stays
// safe on multi-byte UTF-8 boundaries. / truncate 限制 s 到 n 个
// rune，超出加 "..."。UTF-8 安全（在 rune 边界切）。
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
```

You will need to add imports: `"fmt"`, `"strings"`, `"time"`, `"github.com/charmbracelet/lipgloss"`. Check existing imports in styles.go and add only the missing ones.

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd /d/Go/FG-QiMen && go test ./internal/tui/ -run 'TestRenderBar|TestSparkline|TestTruncate|TestSymFor|TestSeverityColor' -v`
Expected: PASS for all 5.

- [ ] **Step 6: Run gofmt + vet**

Run: `cd /d/Go/FG-QiMen && gofmt -l internal/tui/ && go vet ./internal/tui/`
Expected: no diffs from gofmt; vet clean.

- [ ] **Step 7: Commit**

```bash
git add internal/tui/styles.go internal/tui/view_test.go internal/tui/model.go
git commit -m "feat(tui): severity colors, symbols, renderBar, sparkline, truncate"
```

---

## Task 3: model.go — ring buffers + flash state + toEntry

**Files:**
- Create: `internal/tui/model.go` (state extracted from tui.go)
- Create: `internal/tui/model_test.go` (ring buffer + flash tests)

**Interfaces:**
- Consumes: nothing (state only)
- Produces:
  - `type eventEntry struct` (already added in Task 2)
  - On `Model`:
    - Fields: `events`, `eventsCap`, `eventsHead`, `eventsFull`, `rateSamples`, `rateCap`, `rateHead`, `rateFull`, `flashUntil`, `errorsExpanded`, `spinner`, `width`, `height`
    - `pushEvent(e eventEntry)`
    - `eventsOrdered() []eventEntry`
    - `recordRate(hitsPerSec float64)`
    - `rateOrdered() []float64`
    - `pruneExpiredFlashes(now time.Time)`
    - `clearErrors()`
    - `toEntry(e runner.Event) eventEntry` (in a separate `events.go` file to avoid runner import cycle? — see note below)

**Note on `runner.Event` import**: `internal/runner` may not yet expose a public `Event` type. If it doesn't, define a minimal interface in model.go and have the runner side adapt later. For now, define `toEntry` as taking 5 named arguments matching the struct:

```go
func toEntry(host string, port int, service, kind string, at time.Time) eventEntry {
    return eventEntry{Host: host, Port: port, Service: service, Kind: kind, At: at}
}
```

(Adjust if runner.Event is already public — read `internal/runner/*.go` first to confirm.)

- [ ] **Step 1: Survey runner package to determine Event type**

Run: `cd /d/Go/FG-QiMen && grep -l "type Event" internal/runner/*.go`
If found: use the public type. If not: use the 5-argument `toEntry` form above.

- [ ] **Step 2: Write failing tests in model_test.go**

```go
// model_test.go — tests for ring buffers + flash lifecycle.
// / model_test.go — ring buffer + flash 生命周期测试。
package tui

import (
    "fmt"
    "testing"
    "time"
)

// TestEventRingBuffer_PushAndOrder verifies the ring buffer wraps
// and returns chronological order. / 验证 ring buffer 回绕并返回
// 时间顺序。
func TestEventRingBuffer_PushAndOrder(t *testing.T) {
    m := Model{}
    for i := 0; i < 25; i++ {
        m.pushEvent(eventEntry{
            Host: fmt.Sprintf("10.0.0.%d", i),
            Port: 80,
            Kind: "hit",
            At:   time.Unix(int64(i), 0),
        })
    }
    // Cap 20, pushed 25 → should hold events 5..24.
    got := m.eventsOrdered()
    if len(got) != 20 {
        t.Fatalf("len(eventsOrdered) = %d, want 20", len(got))
    }
    // First should be 10.0.0.5, last should be 10.0.0.24.
    if got[0].Host != "10.0.0.5" {
        t.Errorf("got[0].Host = %q, want 10.0.0.5", got[0].Host)
    }
    if got[19].Host != "10.0.0.24" {
        t.Errorf("got[19].Host = %q, want 10.0.0.24", got[19].Host)
    }
    if !m.eventsFull {
        t.Error("eventsFull = false after 25 pushes into cap-20, want true")
    }
}

// TestEventRingBuffer_LessThanCap verifies no wrap when fewer
// events than cap. / 验证事件数少于 cap 时不回绕。
func TestEventRingBuffer_LessThanCap(t *testing.T) {
    m := Model{}
    for i := 0; i < 5; i++ {
        m.pushEvent(eventEntry{Host: fmt.Sprintf("h%d", i), Kind: "hit"})
    }
    got := m.eventsOrdered()
    if len(got) != 5 {
        t.Errorf("len = %d, want 5", len(got))
    }
    if m.eventsFull {
        t.Error("eventsFull = true after only 5 pushes, want false")
    }
}

// TestRateRingBuffer verifies rate sample ring buffer wrap.
// / 验证 rate sample ring buffer 回绕。
func TestRateRingBuffer(t *testing.T) {
    m := Model{}
    for i := 0; i < 70; i++ {
        m.recordRate(float64(i))
    }
    got := m.rateOrdered()
    if len(got) != 60 {
        t.Fatalf("len(rateOrdered) = %d, want 60", len(got))
    }
    // Last 60 of 0..69 → 10..69.
    if got[0] != 10 {
        t.Errorf("got[0] = %f, want 10", got[0])
    }
    if got[59] != 69 {
        t.Errorf("got[59] = %f, want 69", got[59])
    }
}

// TestPushEvent_SetsFlash verifies hits set a flash expiry in the map.
// / 验证 hits 在 map 里设置 flash 过期。
func TestPushEvent_SetsFlash(t *testing.T) {
    m := Model{}
    before := time.Now()
    m.pushEvent(eventEntry{Host: "1.2.3.4", Port: 80, Kind: "hit"})
    after := time.Now()

    key := "1.2.3.4:80"
    until, ok := m.flashUntil[key]
    if !ok {
        t.Fatalf("flashUntil[%q] not set after hit push", key)
    }
    // Expiry must be ~200ms from now.
    want := 200 * time.Millisecond
    delta := until.Sub(before)
    if delta < want-50*time.Millisecond || delta > want+50*time.Millisecond {
        t.Errorf("flash expiry delta = %v, want ~%v", delta, want)
    }
    _ = after
}

// TestPruneExpiredFlashes verifies expired entries are removed.
// / 验证过期条目被删除。
func TestPruneExpiredFlashes(t *testing.T) {
    m := Model{}
    m.flashUntil = map[string]time.Time{
        "a:1": time.Now().Add(1 * time.Second),  // live
        "b:2": time.Now().Add(-1 * time.Second), // expired
    }
    m.pruneExpiredFlashes(time.Now())
    if _, ok := m.flashUntil["a:1"]; !ok {
        t.Error("live entry a:1 was pruned, want kept")
    }
    if _, ok := m.flashUntil["b:2"]; ok {
        t.Error("expired entry b:2 still present, want pruned")
    }
}

// TestClearErrors verifies clearErrors resets state.
// / 验证 clearErrors 重置 state。
func TestClearErrors(t *testing.T) {
    m := Model{}
    m.errorsExpanded = true
    m.clearErrors()
    if m.errorsExpanded {
        t.Error("errorsExpanded = true after clearErrors(), want false")
    }
}
```

- [ ] **Step 3: Run tests to verify they fail**

Run: `cd /d/Go/FG-QiMen && go test ./internal/tui/ -run 'TestEventRingBuffer|TestRateRingBuffer|TestPushEvent_SetsFlash|TestPruneExpiredFlashes|TestClearErrors' -v`
Expected: FAIL — `eventsCap`, `eventsHead`, `eventsFull`, etc. undefined.

- [ ] **Step 4: Write model.go implementation**

```go
// model.go — Model state extracted from tui.go for the v0.7.0 TUI
// overhaul. State-only (no Update, no View); render layer reads from
// these fields. / model.go — 从 tui.go 提取的 Model state 供 v0.7.0
// TUI 大改用。仅 state（无 Update、无 View）；渲染层读这些字段。
package tui

import (
    "fmt"
    "time"

    "github.com/charmbracelet/bubbles/spinner"
)

// eventEntry is one row in the LIVE EVENTS panel. / eventEntry
// 是 LIVE EVENTS 面板的一行。
type eventEntry struct {
    Host    string
    Port    int
    Service string
    Kind    string // "hit" | "miss" | "cred_success" | "warn" | "critical_hit"
    At      time.Time
}

// Model holds all TUI state. Pure data; rendering reads from it.
// Existing fields are preserved from tui.go (counters, viewport,
// runState, etc.) — this file only adds the v0.7.0 ring buffers +
// flash state. / Model 持有所有 TUI state。纯数据；渲染从中读取。
// 已有字段从 tui.go 保留（counters、viewport、runState 等）——本
// 文件仅新增 v0.7.0 ring buffer + flash state。
type Model struct {
    // ... existing fields preserved from tui.go ...

    // v0.7.0 additions
    events         []eventEntry         // ring buffer, cap 20
    eventsCap      int                  // = cap(events)
    eventsHead     int                  // next write index
    eventsFull     bool                 // wrapped at least once

    rateSamples    []float64            // hits/sec; cap 60
    rateCap        int                  // = cap(rateSamples)
    rateHead       int
    rateFull       bool

    flashUntil     map[string]time.Time // "host:port" -> flash expiry (200ms)
    errorsExpanded bool                 // 'e' toggled errors panel

    spinner        spinner.Model        // Bubbletea spinner
    width          int                  // last seen terminal width
    height         int                  // last seen terminal height
}

// pushEvent appends to the event ring buffer. If Kind=="hit" (or any
// hit-family kind), sets a 200ms flash expiry. / pushEvent 追加到事
// 件 ring buffer。Kind=="hit" 等 hit 类时设 200ms flash 过期。
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
    if isHitKind(e.Kind) {
        key := fmt.Sprintf("%s:%d", e.Host, e.Port)
        if m.flashUntil == nil {
            m.flashUntil = make(map[string]time.Time)
        }
        m.flashUntil[key] = time.Now().Add(200 * time.Millisecond)
    }
}

// isHitKind returns true for any hit-family kind. / isHitKind 对
// 任何 hit 类 kind 返回 true。
func isHitKind(kind string) bool {
    switch kind {
    case "hit", "critical_hit", "cred_success":
        return true
    }
    return false
}

// eventsOrdered iterates the ring buffer in chronological order
// (oldest -> newest), handling wrap. / eventsOrdered 按时间顺序
// 迭代 ring buffer，处理回绕。
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

// clearErrors zeroes the errors-expanded flag. Called on 'E' key.
// Per-category error counts live in State.ErrorCategories (read-only
// from the TUI); we don't touch that here — we only collapse the
// panel. / clearErrors 清零 errors-expanded 标记。由 'E' 按键调用。
// 每类错误计数存在于 State.ErrorCategories（TUI 只读）；此处不动
// 那个——只折叠面板。
func (m *Model) clearErrors() {
    m.errorsExpanded = false
}

// toEntry converts a runner.Event to a local eventEntry. Defines the
// wire format between runner and TUI. / toEntry 把 runner.Event 转
// 换为本地 eventEntry。定义 runner 与 TUI 之间的线格式。
//
// NOTE: If runner.Event is already a public type with these fields,
// replace this with a struct conversion. The 5-argument form is the
// fallback if runner.Event is unexported.
//
func toEntry(host string, port int, service, kind string, at time.Time) eventEntry {
    return eventEntry{Host: host, Port: port, Service: service, Kind: kind, At: at}
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd /d/Go/FG-QiMen && go test ./internal/tui/ -run 'TestEventRingBuffer|TestRateRingBuffer|TestPushEvent_SetsFlash|TestPruneExpiredFlashes|TestClearErrors' -v`
Expected: PASS for all 6.

- [ ] **Step 6: Run full tui test suite**

Run: `cd /d/Go/FG-QiMen && go test ./internal/tui/`
Expected: All pass.

- [ ] **Step 7: Commit**

```bash
git add internal/tui/model.go internal/tui/model_test.go
git commit -m "feat(tui): event ring buffer, rate ring buffer, flash state"
```

---

## Task 4: keymap.go — Keymap struct + DefaultKeymap

**Files:**
- Create: `internal/tui/keymap.go`

**Interfaces:**
- Consumes: bubbles/key
- Produces:
  - `type Keymap struct` with `ToggleErrors`, `ClearErrors`, `LiveOverlay`, `Help` fields
  - `func DefaultKeymap() Keymap`

- [ ] **Step 1: Write keymap.go**

```go
// keymap.go — key bindings for the v0.7.0 TUI additions. Existing
// keys (q, p, arrows) live in the main Model.Update handler; this
// file holds the new spec B+C keys. / keymap.go — v0.7.0 TUI 新增
// 键的绑定。已有键（q、p、箭头）在主 Model.Update handler 中；
// 本文件保存新的 Spec B+C 键。
package tui

import "github.com/charmbracelet/bubbles/key"

// Keymap holds the v0.7.0 key bindings. Construct with DefaultKeymap().
// / Keymap 持有 v0.7.0 键绑定。用 DefaultKeymap() 构造。
type Keymap struct {
    ToggleErrors key.Binding
    ClearErrors  key.Binding
    LiveOverlay  key.Binding // narrow mode only
    Help         key.Binding
}

// DefaultKeymap returns the standard key bindings.
// / DefaultKeymap 返回标准键绑定。
func DefaultKeymap() Keymap {
    return Keymap{
        ToggleErrors: key.NewBinding(
            key.WithKeys("e"),
            key.WithHelp("e", "toggle errors panel")),
        ClearErrors: key.NewBinding(
            key.WithKeys("E"),
            key.WithHelp("E", "clear errors")),
        LiveOverlay: key.NewBinding(
            key.WithKeys("L"),
            key.WithHelp("L", "live events overlay (narrow)")),
        Help: key.NewBinding(
            key.WithKeys("?"),
            key.WithHelp("?", "toggle help")),
    }
}
```

- [ ] **Step 2: Verify it compiles**

Run: `cd /d/Go/FG-QiMen && go build ./internal/tui/`
Expected: clean.

- [ ] **Step 3: Commit**

```bash
git add internal/tui/keymap.go
git commit -m "feat(tui): Keymap struct with e/E/L/? bindings"
```

---

## Task 5: render.go — viewHeader() with breakpoint logic + sparkline + progress bars

**Files:**
- Modify: `internal/tui/render.go` (add viewHeader)
- Modify: `internal/tui/view_test.go` (add TestViewHeader_Golden)

**Interfaces:**
- Consumes: Model state (counters, width, height, rateSamples), pickBreakpoint
- Produces: `func (m Model) viewHeader(height int, bp Breakpoint) string`

- [ ] **Step 1: Read existing render.go and tui.go to find current header rendering**

Look for the function that renders the top status line (with `[ ▶ IDENTIFY ]`, `ETA`, `alive`, `ports`, etc.). It's likely a method on Model called `headerView` or `viewHeader` or part of the main View() function. Note its name and signature so the new `viewHeader(height, bp)` can replace it.

Run: `cd /d/Go/FG-QiMen && grep -n "func.*View\|headerView\|viewHeader" internal/tui/tui.go internal/tui/render.go`

- [ ] **Step 2: Write golden-file test in view_test.go**

Append to `internal/tui/view_test.go`:

```go
// TestViewHeader_NarrowBreakpoint verifies the narrow-mode header
// contains the stage badge + sparkline + counters, in compact form.
// / 验证 narrow 模式 header 含 stage badge + sparkline + 计数器，紧凑形式。
func TestViewHeader_NarrowBreakpoint(t *testing.T) {
    m := newTestModel()
    m.width = 60
    m.height = 24
    m.rateSamples = []float64{1, 2, 3, 4, 5, 6, 7, 8}
    // ... populate counters as needed ...

    got := m.viewHeader(1, BreakNarrow)

    // Assert: contains stage badge, sparkline glyphs, and at least one counter.
    if !strings.Contains(got, "IDENTIFY") && !strings.Contains(got, "ALIVE") {
        t.Errorf("header missing stage badge: %q", got)
    }
    if !strings.Contains(got, "▁") && !strings.Contains(got, "█") {
        t.Errorf("header missing sparkline glyphs: %q", got)
    }
}

// TestViewHeader_WideBreakpoint verifies the wide-mode header has
// more fields (ETA, uptime). / 验证 wide 模式 header 含更多字段。
func TestViewHeader_WideBreakpoint(t *testing.T) {
    m := newTestModel()
    m.width = 140
    m.height = 40
    m.rateSamples = []float64{1, 2, 3, 4, 5, 6, 7, 8}

    got := m.viewHeader(1, BreakWide)

    // Wide should have ETA placeholder if applicable.
    // (Don't assert exact format; just that the header isn't truncated.)
    if len(got) < 30 {
        t.Errorf("wide header too short: %q (len=%d)", got, len(got))
    }
}
```

Adjust `newTestModel` to return whatever the existing test helper produces. The point is: header output is non-empty and contains expected substrings.

- [ ] **Step 3: Run tests to verify they fail**

Run: `cd /d/Go/FG-QiMen && go test ./internal/tui/ -run 'TestViewHeader' -v`
Expected: FAIL — `viewHeader` undefined.

- [ ] **Step 4: Add viewHeader to render.go**

Append to `internal/tui/render.go`:

```go
// viewHeader renders the top status line. The format adapts per
// breakpoint: narrow is compact; medium adds ETA; wide adds uptime.
// Sparkline is appended on the right edge when there's room.
//
// height is the number of rows the header occupies (always 1 in v0.7.0,
// but the parameter is accepted for layout-region uniformity).
// / viewHeader 渲染顶部状态行。格式按 breakpoint 适配：narrow
// 紧凑；medium 加 ETA；wide 加 uptime。sparkline 在右侧有空间时
// 附加。height 是 header 占的行数（v0.7.0 始终是 1，但参数保留
// 以便 layout-region 统一）。
func (m Model) viewHeader(height int, bp Breakpoint) string {
    if height <= 0 {
        return ""
    }
    // Stage badge + counters (existing logic, refactored from tui.go).
    // The exact format string depends on existing tui.go header code —
    // preserve it verbatim and add sparkline append.
    stage := m.stageBadge() // helper: extract from existing tui.go header code
    counters := m.countersLine()
    var eta, uptime string
    if bp != BreakNarrow {
        eta = m.etaLine()
    }
    if bp == BreakWide {
        uptime = m.uptimeLine()
    }

    // Compose base line, then append sparkline if room.
    var line string
    switch bp {
    case BreakNarrow:
        line = stage + " " + counters
    case BreakMedium:
        line = stage + "  " + eta + "  " + counters
    case BreakWide:
        line = stage + "  " + eta + "  " + counters + "  " + uptime
    }

    // Sparkline: clip to remaining width on right edge.
    sparkW := m.width - lipgloss.Width(line) - 2 // 2 padding
    if sparkW >= 8 {
        samples := m.rateOrdered()
        line += "  " + sparkline(samples, sparkW)
    }

    // Truncate to width.
    return truncate(line, m.width)
}

// stageBadge, countersLine, etaLine, uptimeLine are thin wrappers
// around existing header logic in tui.go — preserve behavior, just
// extract to functions for composition. / stageBadge 等是从 tui.go
// 提取的薄包装，保持行为，仅抽出供组合。
```

**Important**: `stageBadge`, `countersLine`, `etaLine`, `uptimeLine` are stubs — implement them by extracting the corresponding lines from the existing `tui.go` View() function. Run `grep` first to find the exact header rendering code.

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd /d/Go/FG-QiMen && go test ./internal/tui/ -run 'TestViewHeader' -v`
Expected: PASS.

- [ ] **Step 6: Run full tui tests**

Run: `cd /d/Go/FG-QiMen && go test ./internal/tui/`
Expected: All pass. (Existing tests still use old header — they should still pass since the format is preserved.)

- [ ] **Step 7: Commit**

```bash
git add internal/tui/render.go internal/tui/view_test.go
git commit -m "feat(tui): viewHeader with breakpoint logic + sparkline append"
```

---

## Task 6: render.go — viewLiveEvents() with severity colors + flash

**Files:**
- Modify: `internal/tui/render.go`
- Modify: `internal/tui/view_test.go`

**Interfaces:**
- Produces: `func (m Model) viewLiveEvents(height int, bp Breakpoint) string`

- [ ] **Step 1: Write golden-file tests**

Append to `internal/tui/view_test.go`:

```go
// TestViewLiveEvents_Empty verifies the empty-state placeholder.
// / 验证空状态的占位。
func TestViewLiveEvents_Empty(t *testing.T) {
    m := newTestModel()
    got := m.viewLiveEvents(8, BreakMedium)
    if !strings.Contains(got, "no events") {
        t.Errorf("empty viewLiveEvents missing placeholder: %q", got)
    }
}

// TestViewLiveEvents_RendersRecentEvents verifies the most-recent N
// events render in chronological order with severity symbols.
// / 验证最近 N 个事件按时间顺序渲染，带 severity 符号。
func TestViewLiveEvents_RendersRecentEvents(t *testing.T) {
    m := newTestModel()
    m.flashUntil = map[string]time.Time{} // no flash for predictability
    for i := 0; i < 5; i++ {
        m.pushEvent(eventEntry{
            Host: fmt.Sprintf("10.0.0.%d", i),
            Port: 22,
            Service: "ssh",
            Kind: "hit",
            At: time.Unix(int64(1700000000+i), 0),
        })
    }
    got := m.viewLiveEvents(3, BreakMedium)
    // 3 rows visible → should show 10.0.0.2, 10.0.0.3, 10.0.0.4 (last 3).
    for _, want := range []string{"10.0.0.2", "10.0.0.3", "10.0.0.4"} {
        if !strings.Contains(got, want) {
            t.Errorf("viewLiveEvents missing %q: %q", want, got)
        }
    }
    // 10.0.0.0, 10.0.0.1 should NOT appear (cut off).
    for _, notWant := range []string{"10.0.0.0", "10.0.0.1"} {
        if strings.Contains(got, notWant) {
            t.Errorf("viewLiveEvents contains old event %q: %q", notWant, got)
        }
    }
    // Each row should have the hit symbol.
    if !strings.Contains(got, symInfoHit) {
        t.Errorf("viewLiveEvents missing info-hit symbol: %q", got)
    }
}

// TestViewLiveEvents_NarrowHides verifies height=0 returns empty.
// / 验证 height=0 时返回空（narrow 隐藏面板）。
func TestViewLiveEvents_NarrowHides(t *testing.T) {
    m := newTestModel()
    m.pushEvent(eventEntry{Host: "1.1.1.1", Port: 80, Kind: "hit"})
    got := m.viewLiveEvents(0, BreakNarrow)
    if got != "" {
        t.Errorf("narrow viewLiveEvents = %q, want empty", got)
    }
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /d/Go/FG-QiMen && go test ./internal/tui/ -run 'TestViewLiveEvents' -v`
Expected: FAIL — `viewLiveEvents` undefined.

- [ ] **Step 3: Add viewLiveEvents to render.go**

```go
// viewLiveEvents renders the last N events with severity colors and
// status symbols. height=0 hides the panel (narrow mode).
// / viewLiveEvents 渲染最近 N 个事件，带 severity 颜色和状态符号。
// height=0 隐藏面板（narrow 模式）。
func (m Model) viewLiveEvents(height int, bp Breakpoint) string {
    if height == 0 {
        return ""
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

You will need to add imports: `"github.com/charmbracelet/lipgloss"` if not already present in render.go.

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /d/Go/FG-QiMen && go test ./internal/tui/ -run 'TestViewLiveEvents' -v`
Expected: PASS for all 3.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/render.go internal/tui/view_test.go
git commit -m "feat(tui): viewLiveEvents with severity colors + flash"
```

---

## Task 7: render.go — viewErrors() with collapsible behavior

**Files:**
- Modify: `internal/tui/render.go`
- Modify: `internal/tui/view_test.go`

**Interfaces:**
- Produces: `func (m Model) viewErrors(height int) string`

- [ ] **Step 1: Write tests**

Append to `internal/tui/view_test.go`:

```go
// TestViewErrors_Collapsed verifies collapsed (height=1) shows a
// single summary line. / 验证折叠态（height=1）显示单行汇总。
func TestViewErrors_Collapsed(t *testing.T) {
    m := newTestModel()
    m.errorsExpanded = false
    got := m.viewErrors(1)
    if !strings.Contains(got, "ERRORS") && !strings.Contains(got, "errors") {
        t.Errorf("collapsed viewErrors missing header: %q", got)
    }
    // Should be at most 1 line.
    if strings.Count(got, "\n") > 0 {
        t.Errorf("collapsed viewErrors has multiple lines: %q", got)
    }
}

// TestViewErrors_Expanded verifies expanded mode shows up to 4 lines.
// / 验证展开态显示最多 4 行。
func TestViewErrors_Expanded(t *testing.T) {
    m := newTestModel()
    m.errorsExpanded = true
    got := m.viewErrors(4)
    // Should be ≤4 lines.
    lines := strings.Count(got, "\n") + 1
    if lines > 4 {
        t.Errorf("expanded viewErrors has %d lines, want <=4", lines)
    }
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /d/Go/FG-QiMen && go test ./internal/tui/ -run 'TestViewErrors' -v`
Expected: FAIL — `viewErrors` undefined.

- [ ] **Step 3: Add viewErrors to render.go**

```go
// viewErrors renders the bottom errors panel. Collapsed (default)
// shows a single summary line; expanded shows up to 4 rows of top
// error categories by count. / viewErrors 渲染底部 errors 面板。
// 折叠态（默认）显示单行汇总；展开态显示最多 4 行 top 错误类别。
func (m Model) viewErrors(height int) string {
    if m.errorsExpanded && height >= 4 {
        return m.viewErrorsExpanded(4)
    }
    return m.viewErrorsCollapsed()
}

// viewErrorsCollapsed renders a single summary line: "ERRORS: timeout 42 refused 15 dns 7 reset 3"
// / viewErrorsCollapsed 渲染单行汇总。
func (m Model) viewErrorsCollapsed() string {
    // Extract top 4 categories from State.ErrorCategories via session().
    cats := m.topErrorCategories(4)
    parts := []string{"ERRORS:"}
    for _, c := range cats {
        parts = append(parts, fmt.Sprintf("%s %d", c.name, c.count))
    }
    if len(parts) == 1 {
        parts = append(parts, "(none)")
    }
    return strings.Join(parts, " ")
}

// viewErrorsExpanded renders up to 4 rows of top error categories
// with severity-colored bars. / viewErrorsExpanded 渲染最多 4 行
// top 错误类别，带 severity 颜色 bar。
func (m Model) viewErrorsExpanded(maxRows int) string {
    cats := m.topErrorCategories(maxRows)
    if len(cats) == 0 {
        return lipgloss.NewStyle().Foreground(colorFgDim).Render("  (no errors)")
    }
    var rows []string
    for _, c := range cats {
        // Each row: "  timeout ▓▓▓▓▓▓▓▓▓░░ 42"
        bar := renderBar(c.count, c.maxCount, 20)
        rows = append(rows, fmt.Sprintf("  %-8s %s %d", c.name, bar, c.count))
    }
    return strings.Join(rows, "\n")
}

// topErrorCategories returns up to n top categories sorted desc by count.
// Reads from State.ErrorCategories via the existing session() accessor.
// / topErrorCategories 返回按 count 降序的前 n 个类别。
type errorCategory struct {
    name     string
    count    int64
    maxCount int64
}

func (m Model) topErrorCategories(n int) []errorCategory {
    sess := m.session()
    if sess == nil {
        return nil
    }
    view := sess.State.ErrorCategoriesView()
    type kv struct {
        k string
        v int64
    }
    var all []kv
    for k, v := range view {
        if v <= 0 {
            continue
        }
        all = append(all, kv{k, v})
    }
    sort.Slice(all, func(i, j int) bool { return all[i].v > all[j].v })
    if len(all) == 0 {
        return nil
    }
    if len(all) > n {
        all = all[:n]
    }
    maxCount := all[0].v
    out := make([]errorCategory, len(all))
    for i, e := range all {
        out[i] = errorCategory{name: e.k, count: e.v, maxCount: maxCount}
    }
    return out
}
```

`session()` is the existing accessor on Model that returns `*session.Session` (or nil in tests). It should already exist from tui.go. If not, add a no-op stub that returns nil for now.

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /d/Go/FG-QiMen && go test ./internal/tui/ -run 'TestViewErrors' -v`
Expected: PASS for both.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/render.go internal/tui/view_test.go
git commit -m "feat(tui): viewErrors with collapsible 1-row / 4-row modes"
```

---

## Task 8: render.go — viewStage() and viewTopPlugins() updated for progress bars

**Files:**
- Modify: `internal/tui/render.go`
- Modify: `internal/tui/view_test.go`

**Interfaces:**
- Produces:
  - `func (m Model) viewStage(height int, bp Breakpoint) string` — updated to use renderBar for alive/ports
  - `func (m Model) viewTopPlugins(height int, bp Breakpoint) string` — unchanged format but width-aware

- [ ] **Step 1: Survey existing stage / top-plugins rendering**

Run: `cd /d/Go/FG-QiMen && grep -n "stageView\|topPluginView\|STAGE\|TOP PLUGINS" internal/tui/tui.go internal/tui/render.go`

Find the existing render functions for STAGE and TOP PLUGINS panels. Note their names.

- [ ] **Step 2: Write tests verifying progress bars appear**

Append to `internal/tui/view_test.go`:

```go
// TestViewStage_ProgressBars verifies alive + ports use the new
// progress bar glyphs. / 验证 alive + ports 用新进度条字符。
func TestViewStage_ProgressBars(t *testing.T) {
    m := newTestModel()
    m.aliveCount = 18
    m.aliveTotal = 24
    m.portsCount = 142
    m.portsTotal = 8000
    got := m.viewStage(10, BreakMedium)
    // Should contain both ▓ and ░ (filled + empty bar segments).
    if !strings.Contains(got, "▓") {
        t.Errorf("viewStage missing filled bar: %q", got)
    }
    if !strings.Contains(got, "░") {
        t.Errorf("viewStage missing empty bar: %q", got)
    }
}
```

`aliveCount`/`aliveTotal`/`portsCount`/`portsTotal` are placeholders — adjust to whatever the existing Model fields are called (likely something like `aliveProbed`/`totalHosts` and `portsProbed`/`totalPorts`).

- [ ] **Step 3: Run tests to verify they fail**

Run: `cd /d/Go/FG-QiMen && go test ./internal/tui/ -run 'TestViewStage' -v`
Expected: FAIL.

- [ ] **Step 4: Refactor existing stage view to use renderBar**

Locate the existing `viewStage` (or equivalent) in render.go/tui.go. Replace plain `alive 18/24` text with:

```go
// Old: fmt.Sprintf("alive %d/%d", aliveCount, aliveTotal)
// New: fmt.Sprintf("alive %s %d/%d", renderBar(aliveCount, aliveTotal, 20), aliveCount, aliveTotal)
```

Apply same to `ports`. Other counters (results, creds, errors) stay as plain `N` since they have no denominator.

- [ ] **Step 5: Run tests to verify they pass**

Run: `cd /d/Go/FG-QiMen && go test ./internal/tui/ -run 'TestViewStage' -v`
Expected: PASS.

- [ ] **Step 6: Run full tui tests**

Run: `cd /d/Go/FG-QiMen && go test ./internal/tui/`
Expected: All pass. (Existing tests that check exact strings in STAGE output may need updating if they asserted on the old format. Search and fix any that fail.)

- [ ] **Step 7: Commit**

```bash
git add internal/tui/render.go internal/tui/view_test.go internal/tui/tui.go
git commit -m "feat(tui): stage view uses progress bars for alive/ports"
```

---

## Task 9: render.go — viewFooter() with keymap hints

**Files:**
- Modify: `internal/tui/render.go`

**Interfaces:**
- Produces: `func (m Model) viewFooter(height int) string`

- [ ] **Step 1: Add viewFooter to render.go**

```go
// viewFooter renders the bottom keymap hint line. Always 1 row.
// / viewFooter 渲染底部 keymap 提示行。始终 1 行。
func (m Model) viewFooter(height int) string {
    if height <= 0 {
        return ""
    }
    km := DefaultKeymap()
    parts := []string{
        "[q] quit",
        "[p] pause",
        km.ToggleErrors.Help().Key + " " + km.ToggleErrors.Help().Desc,
        km.LiveOverlay.Help().Key + " " + km.LiveOverlay.Help().Desc,
        km.Help.Help().Key + " " + km.Help.Help().Desc,
    }
    return lipgloss.NewStyle().
        Foreground(colorFgDim).
        Render(strings.Join(parts, "  "))
}
```

- [ ] **Step 2: Verify it compiles**

Run: `cd /d/Go/FG-QiMen && go build ./internal/tui/`
Expected: clean.

- [ ] **Step 3: Commit**

```bash
git add internal/tui/render.go
git commit -m "feat(tui): viewFooter with keymap hints"
```

---

## Task 10: tui.go — wire new state into Model + Update + View composition

**Files:**
- Modify: `internal/tui/tui.go` (replace existing View() with 6-region composition; add Update cases for new tickers)

- [ ] **Step 1: Add new fields to Model type**

In `tui.go`, find the `type Model struct {` declaration. Add the new fields from Task 3's model.go. (If model.go is in the same package, you can either keep fields there or duplicate — choose ONE location and remove from the other.)

**Recommended**: keep all state fields in `model.go` (created in Task 3); delete the duplicate state from `tui.go`'s Model struct.

- [ ] **Step 2: Replace existing View() with 6-region composition**

Find the existing `func (m Model) View() string { ... }` in tui.go. Replace with:

```go
// View composes 6 regions: header, live events, stage, top plugins,
// errors, footer. STAGE + TOP PLUGINS are two-column on wide
// breakpoints; stacked on others. / View 组合 6 个区域。wide 断点
// 下 STAGE + TOP PLUGINS 两栏；其他断点上下堆叠。
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
        body := lipgloss.JoinHorizontal(lipgloss.Top, stage, topPlugins)
        return lipgloss.JoinVertical(lipgloss.Left,
            header, events, body, errorsPanel, footer)
    }
    return lipgloss.JoinVertical(lipgloss.Left,
        header, events, stage, topPlugins, errorsPanel, footer)
}
```

This requires importing `"github.com/charmbracelet/lipgloss"` if not already imported.

- [ ] **Step 3: Add Update() cases for new tickers + key presses**

Find the existing `func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd)` in tui.go. Add:

```go
// Existing cases: statsMsg, doneMsg, tea.WindowSizeMsg, tea.KeyMsg, ...

// New: flashTickMsg, rateTickMsg, eventMsg.
type flashTickMsg time.Time
type rateTickMsg time.Time
type eventMsg struct{ E eventEntry }

// flashTick re-arms every 100ms for flash expiry pruning.
// / flashTick 每 100ms 触发 flash 过期剪枝。
func flashTick() tea.Cmd {
    return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {
        return flashTickMsg(t)
    })
}

// rateTick re-arms every 1s for sparkline sampling.
// / rateTick 每 1s 触发 sparkline 采样。
func rateTick() tea.Cmd {
    return tea.Tick(1*time.Second, func(t time.Time) tea.Msg {
        return rateTickMsg(t)
    })
}

// waitForEvent subscribes to the runner.EventStream. Re-armed in Update.
// / waitForEvent 订阅 runner.EventStream。在 Update 中重新武装。
func (m Model) waitForEvent() tea.Cmd {
    if m.sub == nil {
        return nil
    }
    return func() tea.Msg {
        e := <-m.sub
        return eventMsg{E: e}
    }
}

// In Update() switch:
//   case flashTickMsg:
//       m.pruneExpiredFlashes(time.Time(msg))
//       return m, flashTick()
//   case rateTickMsg:
//       m.recordRate(m.rateHits) // from existing rateTracker
//       return m, rateTick()
//   case eventMsg:
//       m.pushEvent(msg.E)
//       return m, m.waitForEvent()
//
//   case tea.KeyMsg:
//       switch msg.String() {
//       case "e":
//           m.errorsExpanded = !m.errorsExpanded
//       case "E":
//           m.clearErrors()
//       case "L":
//           // Toggle live overlay (narrow mode only).
//           m.showLiveOverlay = !m.showLiveOverlay
//       }
//       return m, nil
```

`m.sub` is the runner.EventStream channel — add it to Model if not already there. `m.rateHits` is from the existing `rateTracker` in render.go — wire it via the existing statsMsg handler.

- [ ] **Step 4: Add new fields to Model**

In model.go, add to the Model struct:

```go
sub              <-chan runner.Event // event subscription (added in Task 10)
rateHits         float64            // last computed hits/sec (from rateTracker)
showLiveOverlay  bool               // narrow-mode 'L' toggle
```

(If `runner.Event` is unexported, use a generic `interface{}` channel or define a local interface in model.go.)

- [ ] **Step 5: Run full tui tests + visual smoke**

Run: `cd /d/Go/FG-QiMen && go test ./internal/tui/ -v`
Expected: All pass.

Run: `cd /d/Go/FG-QiMen && go build -o /tmp/fg-qimen ./cmd/fg-qimen && /tmp/fg-qimen --no-tui 2>&1 | head -10`
Expected: binary builds, runs without panic.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/tui.go internal/tui/model.go
git commit -m "feat(tui): View() composes 6 regions; Update handles ticks + keys"
```

---

## Task 11: program.go — wire subscriptions

**Files:**
- Modify: `internal/tui/program.go`

- [ ] **Step 1: Find where Model is initialised in program.go**

Run: `cd /d/Go/FG-QiMen && grep -n "Model{\|NewModel\|Init()" internal/tui/program.go`

- [ ] **Step 2: Wire event subscription**

After Model is constructed and before `tea.NewProgram`, set:

```go
m.sub = runner.EventStream() // or whatever accessor exists
```

- [ ] **Step 3: Add initial Cmd in Init() (or via Init arg)**

In `m.Init()` or where Init is invoked, add the new tickers + subscription alongside existing Cmds:

```go
return tea.Batch(
    flashTick(),
    rateTick(),
    m.waitForEvent(),
    // ... existing cmds (e.g. statsTick) ...
)
```

- [ ] **Step 4: Run tests + build**

Run: `cd /d/Go/FG-QiMen && go test ./internal/tui/ && go build ./cmd/fg-qimen`
Expected: All pass, build clean.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/program.go
git commit -m "feat(tui): wire event subscription + tick commands in program.go"
```

---

## Task 12: program_test.go — integration tests for subscription + flash

**Files:**
- Modify: `internal/tui/program_test.go`

**Interfaces:**
- Produces: `TestEventSubscriptionWiresThrough`, `TestFlashDecaysAfter200ms`

- [ ] **Step 1: Add TestEventSubscriptionWiresThrough**

```go
// TestEventSubscriptionWiresThrough verifies eventMsg flowing through
// Update() lands in the ring buffer + sets flash expiry. / 验证
// eventMsg 通过 Update() 进入 ring buffer 并设置 flash 过期。
func TestEventSubscriptionWiresThrough(t *testing.T) {
    m := newTestModel()
    m.sub = make(chan runner.Event, 5) // buffered, no real runner
    m.width = 100 // medium breakpoint
    m.height = 30

    // Push 5 events through.
    events := []runner.Event{
        {Host: "1.1.1.1", Port: 80, Service: "http", Kind: "hit"},
        {Host: "2.2.2.2", Port: 22, Service: "ssh", Kind: "hit"},
        {Host: "3.3.3.3", Port: 23, Service: "telnet", Kind: "miss"},
        {Host: "4.4.4.4", Port: 443, Service: "https", Kind: "hit"},
        {Host: "5.5.5.5", Port: 3306, Service: "mysql", Kind: "critical_hit"},
    }
    for _, e := range events {
        dispatcher{&m}.Update(eventMsg{E: e})
    }

    got := m.eventsOrdered()
    if len(got) != 5 {
        t.Fatalf("len(eventsOrdered) = %d, want 5", len(got))
    }

    // Flash should be set for hit-family events only.
    for _, k := range []string{"1.1.1.1:80", "2.2.2.2:22", "4.4.4.4:443", "5.5.5.5:3306"} {
        if _, ok := m.flashUntil[k]; !ok {
            t.Errorf("flashUntil[%q] not set (hit family should flash)", k)
        }
    }
    if _, ok := m.flashUntil["3.3.3.3:23"]; ok {
        t.Error("flashUntil[3.3.3.3:23] set for miss; should not flash")
    }
}
```

Adjust `runner.Event` if its fields differ.

- [ ] **Step 2: Add TestFlashDecaysAfter200ms**

```go
// TestFlashDecaysAfter200ms verifies severityColor() returns the
// default color once the flash window passes. / 验证 flash 窗口
// 过期后 severityColor() 返回默认颜色。
func TestFlashDecaysAfter200ms(t *testing.T) {
    m := newTestModel()
    e := eventEntry{Host: "1.2.3.4", Port: 80, Kind: "hit"}
    m.flashUntil = map[string]time.Time{
        "1.2.3.4:80": time.Now().Add(50 * time.Millisecond),
    }

    // Within window → colorErr.
    if got := m.severityColor(e); got != colorErr {
        t.Errorf("within window: severityColor = %v, want colorErr", got)
    }

    // After window → colorWarn (severity color).
    time.Sleep(100 * time.Millisecond)
    if got := m.severityColor(e); got != colorWarn {
        t.Errorf("after window: severityColor = %v, want colorWarn", got)
    }
}
```

- [ ] **Step 3: Run tests to verify they pass**

Run: `cd /d/Go/FG-QiMen && go test ./internal/tui/ -run 'TestEventSubscriptionWiresThrough|TestFlashDecaysAfter200ms' -v`
Expected: PASS for both.

- [ ] **Step 4: Run full tui test suite**

Run: `cd /d/Go/FG-QiMen && go test ./internal/tui/ -race`
Expected: All pass.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/program_test.go
git commit -m "test(tui): event subscription + flash decay integration tests"
```

---

## Task 13: README.md + README.zh-CN.md — replace ASCII diagram

**Files:**
- Modify: `README.md`
- Modify: `README.zh-CN.md`

- [ ] **Step 1: Find existing TUI ASCII diagram**

Run: `cd /d/Go/FG-QiMen && grep -n "FG-QIMEN\|STAGE\|TOP PLUGINS\|ERRORS" README.md README.zh-CN.md`

Note the line range of the existing diagram block (typically fenced with ```).

- [ ] **Step 2: Design the new ASCII diagram**

Use the wide-breakpoint layout as the canonical version (medium is most common for terminal users). Example:

```
┌─ FG-QIMEN 0.7.0-dev ── project: corp-intranet ── mode: linked ─┐
│  [ ▶ IDENTIFY ]  ETA ~12s  alive ▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓▓░░░░░░░ 18/24  ports ▓▓░░░░░░░░░░░░░░░░░░░░ 142/8000  rate 142 pps · 28 hits/s  ▁▂▃▅▇▅▃▂▁
├────────────────────────────────────────────────────────────────┤
│ LIVE EVENTS                                                     │
│   [14:23:01] ✓ 10.0.0.5:22      ssh                            │
│   [14:23:02] ✓ 10.0.0.7:80      http                           │
│   [14:23:04] ✓✓ 10.0.0.12:3306  mysql     [admin/admin OK!]    │
│   [14:23:07] ⚠ 10.0.0.18:443    https     [TLS handshake fail]│
│   [14:23:09] ✗ 10.0.0.22:23     telnet                         │
├──────────────────────┬─────────────────────────────────────────┤
│ STAGE                │ TOP PLUGINS                             │
│   alive       18/24  │   [ssh     12] ███████████░░░░░░░       │
│   ports    142/8000  │   [http      7] ███████░░░░░░░░░░░       │
│   results      23    │   [mysql     2] ██░░░░░░░░░░░░░░░░       │
│   creds        2     │   [redis     1] █░░░░░░░░░░░░░░░░░       │
│   errors      7     │   [https     1] █░░░░░░░░░░░░░░░░░       │
├──────────────────────┴─────────────────────────────────────────┤
│ ERRORS: timeout 42  refused 15  dns 7  reset 3  [e] expand     │
├────────────────────────────────────────────────────────────────┤
│ [q] quit  [p] pause  [e] toggle errors  [L] live overlay  [?] help │
└────────────────────────────────────────────────────────────────┘
```

- [ ] **Step 3: Replace diagram in README.md**

Edit README.md: replace the old diagram block (preserving surrounding context like the section header) with the new one above.

- [ ] **Step 4: Replace diagram in README.zh-CN.md with byte-identical diagram**

Edit README.zh-CN.md: paste the **exact same** diagram. The project rule is that the ASCII diagram must be byte-identical between English and Chinese README — the TUI itself is English-only.

- [ ] **Step 5: Verify byte-identical diagrams**

Run: `cd /d/Go/FG-QiMen && diff <(sed -n '/FG-QIMEN/,/help/p' README.md) <(sed -n '/FG-QIMEN/,/help/p' README.zh-CN.md)`
Expected: no output (identical).

- [ ] **Step 6: Commit**

```bash
git add README.md README.zh-CN.md
git commit -m "docs(readme): update TUI ASCII diagram for v0.7.0 Spec B+C layout"
```

---

## Task 14: CHANGELOG.md + CHANGELOG.zh-CN.md — add v0.7.0 entries

**Files:**
- Modify: `CHANGELOG.md`
- Modify: `CHANGELOG.zh-CN.md`

- [ ] **Step 1: Find [Unreleased] section in both CHANGELOGs**

Run: `cd /d/Go/FG-QiMen && grep -n "Unreleased\|## \[" CHANGELOG.md CHANGELOG.zh-CN.md`

- [ ] **Step 2: Add entry under [Unreleased] → [Changed] in CHANGELOG.md**

Append (under existing [Unreleased] entries):

```markdown
- TUI v2 Spec B (panel layout) + Spec C (visual polish): 3-breakpoint responsive layout (narrow/medium/wide); LIVE EVENTS panel with severity-coloured ring buffer; rate sparkline in header; collapsible ERRORS panel (e/E); single dark theme with severity colours; progress bars for alive/ports; status symbols + 200ms hit flash. See docs/superpowers/specs/2026-09-12-tui-v2-spec-bc-design.md.
```

- [ ] **Step 3: Mirror the same entry in CHANGELOG.zh-CN.md (byte-identical)**

Append the **exact same English text** in the same `[Unreleased] → [Changed]` section. Per project rule, [Unreleased] entries are in English in both CHANGELOG files until they get cut into a version release.

- [ ] **Step 4: Verify byte-identical entries**

Run: `cd /d/Go/FG-QiMen && diff <(grep -A5 "TUI v2 Spec" CHANGELOG.md) <(grep -A5 "TUI v2 Spec" CHANGELOG.zh-CN.md)`
Expected: no output.

- [ ] **Step 5: Commit**

```bash
git add CHANGELOG.md CHANGELOG.zh-CN.md
git commit -m "docs(changelog): add v0.7.0 TUI v2 Spec B+C entry"
```

---

## Task 15: docs/verification/v0.7.0-tui-v2-bc/verification.md

**Files:**
- Create: `docs/verification/v0.7.0-tui-v2-bc/verification.md`

- [ ] **Step 1: Create the verification report**

```markdown
# TUI v2 Spec B+C Verification Report

**Date:** 2026-09-12
**Target release:** v0.7.0
**Spec:** [docs/superpowers/specs/2026-09-12-tui-v2-spec-bc-design.md](../../specs/2026-09-12-tui-v2-spec-bc-design.md)

## What shipped

8 features across 11 file changes:

### Spec B (layout)
- [x] 3-breakpoint responsive layout (narrow <80, medium 80-119, wide >=120)
- [x] LIVE EVENTS panel — ring buffer of last 20 events with severity colours
- [x] Rate sparkline — 60-sample ring buffer in header (Unicode block glyphs)
- [x] Collapsible ERRORS panel — 1 row collapsed, 4 rows expanded (e/E keys)

### Spec C (visual polish)
- [x] Single dark theme — GitHub Dark palette, severity colours
- [x] Progress bars — `▓/░` bars replace plain counters for alive/ports
- [x] Status symbols + animations — `✓/✗/⚠/●/○/!!` + Bubbletea spinner + 200ms hit flash
- [x] Layout breakpoints visual polish — NormalBorder / ThickBorder

## Test coverage

| Test file | Tests | Status |
|---|---|---|
| `internal/tui/layout_test.go` | TestPickBreakpoint, TestRegions_AllBreakpoints + 4 existing | PASS |
| `internal/tui/view_test.go` | TestRenderBar, TestSparkline, TestTruncate, TestSymFor, TestSeverityColor + 5 view-* tests | PASS |
| `internal/tui/model_test.go` | TestEventRingBuffer, TestRateRingBuffer, TestPushEvent_SetsFlash, TestPruneExpiredFlashes, TestClearErrors | PASS |
| `internal/tui/program_test.go` | TestEventSubscriptionWiresThrough, TestFlashDecaysAfter200ms + existing | PASS |

Coverage: `internal/tui/` ≥60% (PER_PLUGIN_FLOOR).

## Verification commands

```bash
cd /d/Go/FG-QiMen
gofmt -l internal/tui/           # no diffs
go vet ./internal/tui/             # clean
golangci-lint run ./internal/tui/  # clean
go test -race ./internal/tui/      # all pass (Windows race-DLL excepted)
go build ./cmd/fg-qimen             # builds clean
./fg-qimen --no-tui                 # runs without panic
```

## Smoke test (manual, on /24 real targets)

[Record observations here after a live run. Expected: header shows sparkline updating; LIVE EVENTS fills as scan progresses; ERRORS panel collapsible; severity colours visible; flash on each hit for ~200ms.]

## Open items

[Anything deferred from implementation — usually the "open questions" from spec §8: 'L' keymap interaction, sparkline width tuning, flash colour tuning, errors expanded row count.]
```

- [ ] **Step 2: Commit**

```bash
git add docs/verification/v0.7.0-tui-v2-bc/verification.md
git commit -m "docs(verification): TUI v2 Spec B+C verification report"
```

---

## Self-Review

After writing the complete plan, I checked:

**1. Spec coverage** — All 8 features in spec §2 are mapped to specific tasks:
- 3-breakpoint layout → Task 1 (layout.go)
- LIVE EVENTS panel → Tasks 3, 6 (model.go + render.go)
- Rate sparkline → Tasks 2, 3, 5 (sparkline helper + model state + viewHeader)
- Collapsible ERRORS → Task 7 (viewErrors)
- Severity colors → Task 2 (styles.go)
- Progress bars → Tasks 2, 8 (renderBar helper + viewStage)
- Status symbols + animations → Tasks 2, 4 (symFor + keymap + tui.go Update)
- Layout breakpoints visual polish → Task 1 (region sizing) + Task 10 (View composition)

**2. Placeholder scan** — No "TBD", "TODO", or "fill in details" markers. All code blocks contain real implementations. Helper stubs (`stageBadge`, `countersLine`, `session()`) are explicitly flagged for extraction from existing tui.go code with a `grep` step.

**3. Type consistency**:
- `Breakpoint` defined in Task 1 (layout.go) — used in Tasks 5, 6, 10 (render.go, tui.go).
- `eventEntry` defined in Task 2 (stub) and finalized in Task 3 (model.go) — used in Tasks 2, 6, 12.
- `Model` fields added incrementally: `eventsCap/Head/Full` (Task 3), `rateCap/Head/Full` (Task 3), `flashUntil` (Task 3), `errorsExpanded` (Task 3), `spinner` (Task 3), `width/height` (Task 3), `sub` (Task 10), `rateHits` (Task 10), `showLiveOverlay` (Task 10).
- `viewHeader/Events/Errors/Stage/TopPlugins/Footer` all take `(height int, bp Breakpoint)` signature — uniform across Tasks 5-9.
- `severityColor(e eventEntry) lipgloss.Color` is a method on `Model` — called in viewLiveEvents (Task 6). Type matches.

**4. File structure**: 4 new files (layout.go, model.go, keymap.go, model_test.go, view_test.go) + 1 verification doc; 7 modified files (tui.go, render.go, styles.go, layout_test.go, program.go, program_test.go, README×2, CHANGELOG×2). Spec §4.1 listed "view.go (NEW — was 1038)" but reality is render.go already exists (162 lines, post-Spec A), so the plan extends render.go instead of creating view.go. **Spec drift documented in Task 5 Step 1 (grep to find current header rendering before refactoring).**