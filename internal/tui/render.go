// render.go — view-layer helpers for the v0.5.2 info-density TUI
// (Task 4 of TUI v2 Spec A) and the v0.7.0 Spec B header (Task 5).
// The main Model + View stays in tui.go (which grows ~50 lines) but
// the per-render math — rate EWMA, top-N extraction, ETA projection,
// fixed-width bar chart, header composition — lives here so tui.go
// stays readable.
//
// render.go — v0.5.2 信息密度 TUI（Task 4 of TUI v2 Spec A）和
// v0.7.0 Spec B header（Task 5）的视图层辅助函数。Model + View 主
// 体仍在 tui.go（它增长 ~50 行），但每次渲染的数学——EWMA 速率、
// top-N 抽取、ETA 估算、固定宽度柱图、header 组合——在这里，让
// tui.go 保持可读。
package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/LCUstinian/FG-QiMen/internal/types"
)

// rateTracker keeps an EWMA-smoothed per-second rate of hits and
// ports. EWMA α=0.5 gives faster decay than pure mean so a brief
// burst still shows up quickly.
//
// rateTracker 维持 hits 和 ports 的 EWMA 平滑每秒速率。α=0.5
// 衰减比纯均值快，瞬时突增仍能迅速体现。
type rateTracker struct {
	lastHits  int64
	lastPorts int64
	lastAt    time.Time
	emaHits   float64
	emaPorts  float64
}

// update samples the next (hits, ports) snapshot, computes the
// instantaneous rate over the gap since the last call, and folds
// it into the running EMA. Negative deltas (counter resets, rare
// race) are clamped to zero so the EWMA never goes negative.
//
// update 采样下一次 (hits, ports) 快照，计算相对上次调用的瞬时速
// 率，并折入运行中的 EMA。负 delta（计数器回滚、罕见的赛跑）被
// 钳为 0，让 EWMA 永不变成负值。
func (r *rateTracker) update(now time.Time, hits, ports int64) (rateHits, ratePorts float64) {
	if !r.lastAt.IsZero() {
		dt := now.Sub(r.lastAt).Seconds()
		if dt > 0 {
			dH := float64(hits - r.lastHits)
			dP := float64(ports - r.lastPorts)
			if dH < 0 {
				dH = 0
			}
			if dP < 0 {
				dP = 0
			}
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
//
// topN 从 m 取计数前 n 名，返回排序好的 [name, countString] 对。
// m 为空时返回 nil 以便 View() 渲染占位符。
func topN(m map[string]int64, n int) [][2]string {
	type kv struct {
		k string
		v int64
	}
	all := make([]kv, 0, len(m))
	for k, v := range m {
		if v <= 0 {
			continue
		}
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
// inputs are insufficient.
//
// computeETA 按阶段返回 ETA 字符串。输入不足时返回 ""。
//
// Per-stage formula:
//   - StageAlive:      probed / totalHosts   (alive sweep progress)
//   - StagePortScan:   ports / totalPorts    (port enumeration)
//   - StageIdentify:   ports / totalPorts    (same envelope; identify
//     also drives port counter)
//   - StageCred / StageDone: empty (no rate projection)
//
// totalHosts / totalPorts are passed separately because
// CountersView doesn't carry them (they live on State directly).
//
// totalHosts / totalPorts 单独传入，因为 CountersView 不携带它们
// （它们直接放在 State 上）。
//
// 按阶段公式：
//   - StageAlive：     probed / totalHosts
//   - StagePortScan：  ports / totalPorts
//   - StageIdentify：  ports / totalPorts
//   - StageCred / StageDone：空（无速率预测）
func computeETA(start, now time.Time, stage int32, view types.CountersView, totalHosts, totalPorts int64) string {
	_ = start // reserved for future total-runtime ETA; per-stage uses elapsed since start
	elapsed := now.Sub(start).Seconds()
	if elapsed <= 0 {
		return ""
	}
	switch int(stage) {
	case int(types.StageAlive):
		if view.AliveProbed > 0 && totalHosts > 0 &&
			view.AliveProbed < totalHosts {
			rem := elapsed * float64(totalHosts-view.AliveProbed) / float64(view.AliveProbed)
			return fmt.Sprintf("~%ds", int(rem))
		}
	case int(types.StagePortScan), int(types.StageIdentify):
		if view.Ports > 0 && totalPorts > 0 &&
			view.Ports < totalPorts {
			rem := elapsed * float64(totalPorts-view.Ports) / float64(view.Ports)
			return fmt.Sprintf("~%ds", int(rem))
		}
	}
	return ""
}

// bar renders a fixed-width horizontal bar made of filled + empty
// Unicode blocks. ratio is clamped to [0, 1] so callers don't have
// to validate. The block characters are intentionally distinct so
// even a 6-char bar reads as a bar (vs. a row of equal-width
// digits).
//
// bar 渲染由填色 + 空心 Unicode block 组成的固定宽度水平条。
// ratio 钳到 [0, 1]，调用方不用校验。block 字符刻意选用有强对比
// 的两种，让 6 字符的 bar 也读作 bar（而非一排等宽数字）。
func bar(ratio float64, w int) string {
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}
	filled := int(float64(w) * ratio)
	return strings.Repeat("█", filled) + strings.Repeat("░", w-filled)
}

// ── v0.7.0 Spec B Task 5: header composition helpers ──
// v0.7.0 Spec B Task 5：header 组合辅助函数
//
// stageBadge / countersLine / etaLine / uptimeLine are thin wrappers
// around the existing header rendering logic in tui.go View(). They
// preserve the existing 2-line header format verbatim — only the
// composition is new (viewHeader) and breakpoint-aware.

// stageBadge returns the existing "[ ▶ STAGE ]" prefix. Format
// kept verbatim from tui.go View() line 451 so callers don't see a
// visual regression. The spinner glyph is ▶ while scanning and ✓
// once StageDone. / stageBadge 返回旧的 "[ ▶ STAGE ]" 前缀。格式
// 与 tui.go View() 第 451 行保持一致，避免视觉回归。
func (m Model) stageBadge() string {
	stage := types.StageName(int32(m.counters.Stage))
	spinner := "▶"
	if int64(m.counters.Stage) == int64(types.StageDone) {
		spinner = "✓"
	}
	return fmt.Sprintf("  [ %s %s ]", spinner, stage)
}

// countersLine returns the "rate: X hits/s    ports: Y/s    probed
// A / B" row from the existing tui.go View() (lines 479-482). The
// row is suppressed when both rates are zero (warming-up state) so
// we never render "rate: 0.0" which reads as "broken".
// / countersLine 返回 tui.go View() 第 479-482 行的 rate 行。两个
// 速率都为 0 时（热身中）抑制，避免渲染"rate: 0.0"被误读为"坏了"。
func (m Model) countersLine() string {
	if m.rateHits <= 0 && m.ratePorts <= 0 {
		return ""
	}
	return fmt.Sprintf("  rate: %.1f hits/s    ports: %.1f/s    probed %d / %d",
		m.rateHits, m.ratePorts, m.counters.AliveProbed, m.totalHosts())
}

// etaLine returns the right-edge ETA / elapsed portion of the stage
// badge line. Returns m.eta when available, else falls back to
// "elapsed <m.elapsed>" so the operator always has a "since when"
// signal — verbatim from tui.go View() lines 453-459.
// / etaLine 返回 stage badge 行右端的 ETA/elapsed 部分。优先返回
// m.eta，否则回退到 "elapsed <m.elapsed>"，让操作员始终有"从何时
// 起"的信号——与 tui.go View() 第 453-459 行一致。
func (m Model) etaLine() string {
	if m.eta != "" {
		return m.eta
	}
	if m.elapsed != "" {
		return "elapsed " + m.elapsed
	}
	return ""
}

// uptimeLine returns the wall-clock time since the model started,
// formatted as "up Xs" (or "up Xm" past 60s). Returns "" when m.start
// is the zero value (model never saw a statsMsg yet) so the wide-mode
// header doesn't render a stale "up 0s" before the pipeline actually
// starts. / uptimeLine 返回 model 启动以来的墙钟时间，格式为
// "up Xs"（超过 60s 后为 "up Xm"）。m.start 为零值时返回 ""（model
// 还没收到第一条 statsMsg），避免宽屏 header 在 pipeline 真正启动
// 前渲染出 "up 0s" 这种失真。
func (m Model) uptimeLine() string {
	if m.start.IsZero() {
		return ""
	}
	d := time.Since(m.start)
	if d <= 0 {
		return ""
	}
	if d < time.Minute {
		return fmt.Sprintf("up %ds", int(d.Seconds()))
	}
	return fmt.Sprintf("up %dm", int(d.Minutes()))
}

// viewHeader renders the top status line. The format adapts per
// breakpoint: narrow is compact (stage + counters only); medium
// adds the right-edge ETA/elapsed; wide adds uptime. The sparkline
// is appended at the right edge (after ETA/uptime) when there is at
// least 8 columns of remaining width — the same 8-col threshold the
// spec uses for other minimum-sized UI affordances.
//
// height is the number of rows the header occupies (always 1 in
// v0.7.0, but the parameter is accepted for layout-region uniformity
// with viewEvents / viewCounters etc.).
//
// / viewHeader 渲染顶部状态行。格式按 breakpoint 适配：narrow 紧
// 凑（只 stage + counters）；medium 加右端 ETA/elapsed；wide 加
// uptime。sparkline 在右侧剩余宽度 >=8 列时附加（在 ETA/uptime 之
// 后）。height 是 header 占的行数（v0.7.0 始终是 1，但参数保留以便
// 与 viewEvents / viewCounters 等区域渲染器统一）。
func (m Model) viewHeader(height int, bp Breakpoint) string {
	if height <= 0 {
		return ""
	}
	width := m.width
	if width <= 0 {
		width = 80
	}

	// Line 1: stage badge (left) + ETA/elapsed (middle-right) +
	// uptime (right of ETA) + sparkline (far right). We compose the
	// line left-to-right, then truncate the whole thing to width so
	// the sparkline naturally clips when room runs out.
	// 第 1 行：stage badge（左）+ ETA/elapsed（中右）+ uptime（ETA 之
	// 右）+ sparkline（最右）。按左到右拼装，整体裁到 width，让
	// sparkline 在空间不足时自然被裁。
	badge := m.stageBadge()
	var line1 strings.Builder
	line1.WriteString(badge)

	// Append ETA (medium/wide only — narrow is compact).
	if bp != BreakNarrow {
		if eta := m.etaLine(); eta != "" {
			line1.WriteString("  ")
			line1.WriteString(eta)
		}
	}

	// Append uptime (wide only). Sits to the right of ETA.
	if bp == BreakWide {
		if up := m.uptimeLine(); up != "" {
			line1.WriteString("    ")
			line1.WriteString(up)
		}
	}

	// Sparkline: appended at the right edge when ≥8 cols remain.
	// sparkline：剩余宽度 ≥8 时附加在右端。
	sparkW := width - lipgloss.Width(line1.String()) - 2
	if sparkW >= 8 {
		samples := m.rateOrdered()
		if s := sparkline(samples, sparkW); s != "" {
			line1.WriteString("  ")
			line1.WriteString(s)
		}
	}

	var sb strings.Builder
	sb.WriteString(truncate(line1.String(), width))
	sb.WriteString("\n")

	// Line 2: rate row (counters) — only when at least one rate is
	// positive. Preserves the existing tui.go View() behavior.
	// 第 2 行：rate 行（counters）——仅在至少一个速率 > 0 时渲染，
	// 保持 tui.go View() 既有行为。
	if counters := m.countersLine(); counters != "" {
		sb.WriteString(truncate(counters, width))
		sb.WriteString("\n")
	}

	return sb.String()
}

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
	rows := make([]string, 0, len(events[start:]))
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
