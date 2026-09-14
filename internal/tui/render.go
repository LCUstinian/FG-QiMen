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

// viewHeader renders the top status cell. The format adapts per
// breakpoint (derived from the model width): narrow is compact (stage
// + counters only); medium adds the right-edge ETA/elapsed; wide adds
// uptime. The sparkline is appended at the right edge (after
// ETA/uptime) when there is at least 8 columns of remaining width —
// the same 8-col threshold the spec uses for other minimum-sized UI
// affordances.
//
// height is the cell's row budget (1 drops the rate line); width is
// the cell's column budget — both lines are truncated to it. The
// composed cell is capped to height from the top, so line 1 (badge)
// always survives.
//
// / viewHeader 渲染顶部状态格。格式按 breakpoint（由 model 宽度推
// 导）适配：narrow 紧凑（只 stage + counters）；medium 加右端
// ETA/elapsed；wide 加 uptime。sparkline 在右侧剩余宽度 >=8 列时附
// 加（在 ETA/uptime 之后）。height 是格的行预算（1 = 丢 rate 行）；
// width 是列预算——两行都裁到它。格从顶部按 height 截断，第 1 行
// （badge）永远存活。
func (m Model) viewHeader(height, width int) string {
	if height <= 0 {
		return ""
	}
	if width <= 0 {
		width = 80
	}
	bp := pickBreakpoint(m.width)

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

	// Line 2: rate row (counters) — only when at least one rate is
	// positive and the cell budget has room for a second row.
	// Preserves the existing tui.go View() behavior.
	// 第 2 行：rate 行（counters）——仅在至少一个速率 > 0 且格预算
	// 装得下第二行时渲染，保持 tui.go View() 既有行为。
	if counters := m.countersLine(); counters != "" && height >= 2 {
		sb.WriteString("\n")
		sb.WriteString(truncate(counters, width))
	}

	// No trailing newline: region renderers compose via
	// lipgloss.JoinVertical, which adds the separators itself.
	// / 不带结尾换行：区域渲染器经 lipgloss.JoinVertical 组合，
	// 换行由它自己加。
	return sb.String()
}

// Event row column widths (spec §5.1 column law). Fixed-width columns
// keep every row a table: the symbol is a 3-column bracket token,
// host:port sits at 21 columns (padded with spaces when short), and
// the service name is padded to 12. Shorter values never shift the
// columns after them.
// / 事件行列宽（spec §5.1 列律）。定宽列让每行都是一张表：符号是 3 列
// 方括号令牌，host:port 恒为 21 列（不足右侧空格补位），协议名补位到
// 12。短值不推动后面的列。
const (
	evSymW  = 3
	evHostW = 21
	evSvcW  = 12
)

// formatHostPort renders "host:port" in exactly maxW columns. IPv6
// hosts get RFC3986 brackets so the port stays unambiguous; truncation
// eats the host side only and never the port; short values are padded
// with spaces on the right.
// / formatHostPort 渲染恰好 maxW 列的 "host:port"。IPv6 主机加 RFC3986
// 方括号保证端口无歧义；截断只吃主机侧、绝不动端口；不足右侧空格
// 补位。
func formatHostPort(host string, port, maxW int) string {
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	portStr := fmt.Sprintf("%d", port)
	budget := maxW - len(portStr) - 1 // ':'
	if budget < 1 {
		budget = 1
	}
	return padTo(truncate(host, budget)+":"+portStr, maxW)
}

// viewLiveEvents renders the LIVE EVENTS cell: a panel title row plus
// the last N events with severity colors and status symbols. Fixed
// columns per the spec §5.1 law: 2-space pad, timestamp (8), 2-space
// gap, symbol (3), 2-space gap, host:port (21), 2-space gap, service
// (12). Rows are truncated to width BEFORE styling so the cell can
// never overflow. height=0 hides the cell (narrow mode) unless the
// 'L' overlay is on, in which case the last 5 rows show anyway.
// / viewLiveEvents 渲染 LIVE EVENTS 格：面板标题行 + 最近 N 个事件
// （带 severity 颜色与状态符号）。按 spec §5.1 固定列律：2 空格缩
// 进、时间戳（8）、2 空格间隔、符号（3）、2 空格间隔、host:port
// （21）、2 空格间隔、协议名（12）。行在上色**前**裁到 width，格永
// 不溢出。height=0 隐藏（narrow 模式），除非 'L' overlay 开启——此
// 时强制显示最近 5 条。
func (m Model) viewLiveEvents(height, width int) string {
	if height <= 0 {
		return ""
	}
	rows := []string{stPanelHeader.Render("  LIVE EVENTS")}
	events := m.eventsOrdered()
	if len(events) == 0 {
		rows = append(rows, lipgloss.NewStyle().
			Foreground(cDim).
			Render("  (no events yet)"))
		return strings.Join(rows, "\n")
	}
	// Take last height-1 events (the title row occupies one slot),
	// newest at bottom. / 取最后 height-1 条（标题行占一格），最新在底。
	n := len(events)
	start := 0
	if n > height-1 {
		start = n - (height - 1)
	}
	for _, e := range events[start:] {
		c := m.severityColor(e)
		sym := padTo(symFor(e.Kind), evSymW)
		ts := e.At.Format("15:04:05")
		hostPort := formatHostPort(e.Host, e.Port, evHostW)
		svc := padTo(truncate(e.Service, evSvcW), evSvcW)
		rows = append(rows, lipgloss.NewStyle().Foreground(c).Render(
			truncate(fmt.Sprintf("  %s  %s  %s  %s", ts, sym, hostPort, svc), width)))
	}
	return strings.Join(rows, "\n")
}

// viewErrors renders the bottom errors panel. Collapsed (default)
// shows a single summary line; expanded shows up to 4 rows of top
// error categories by count. / viewErrors 渲染底部 errors 面板。
// 折叠态（默认）显示单行汇总；展开态显示最多 4 行 top 错误类别。
func (m Model) viewErrors(height, width int) string {
	if m.errorsExpanded && height >= errExpandedRows {
		return m.viewErrorsExpanded(errExpandedRows, width)
	}
	return m.viewErrorsCollapsed(width)
}

// viewErrorsCollapsed renders a single summary line: "ERRORS: timeout 42 refused 15 dns 7 reset 3".
// Indented 2 spaces + dim like every other region, and truncated to
// the cell width so a wide category list can't wrap the frame.
// / viewErrorsCollapsed 渲染单行汇总。与其他区域一致缩进 2 空格 +
// dim 色，并按格宽裁剪，防止类别过多撑折画面。
func (m Model) viewErrorsCollapsed(width int) string {
	if width <= 0 {
		width = 80
	}
	// Extract top 4 categories from State.ErrorCategories via m.state.
	// / 从 m.state 的 State.ErrorCategories 提取 top 4 类别。
	cats := m.topErrorCategories(4)
	parts := []string{"ERRORS:"}
	for _, c := range cats {
		parts = append(parts, fmt.Sprintf("%s %d", c.name, c.count))
	}
	if len(parts) == 1 {
		parts = append(parts, "(none)")
	}
	return lipgloss.NewStyle().Foreground(cDim).
		Render(truncate("  "+strings.Join(parts, " "), width))
}

// viewErrorsExpanded renders up to 4 rows of top error categories
// with severity-colored bars. / viewErrorsExpanded 渲染最多 4 行
// top 错误类别，带 severity 颜色 bar。
func (m Model) viewErrorsExpanded(maxRows, width int) string {
	cats := m.topErrorCategories(maxRows)
	if len(cats) == 0 {
		return lipgloss.NewStyle().Foreground(cDim).Render("  (no errors)")
	}
	rows := make([]string, 0, len(cats))
	for _, c := range cats {
		// Each row: "  timeout ████████████████████ 42"
		barW := 20
		if width-14 < barW {
			barW = width - 14
		}
		if barW < 1 {
			barW = 1
		}
		bar := renderBar(int(c.count), int(c.maxCount), barW)
		// Truncate BEFORE styling — truncate counts runes and would
		// cut ANSI escapes on a styled string.
		// / 先截断再上色——truncate 按 rune 数计，对已上色字符串会
		// 切断 ANSI 转义序列。
		plain := truncate(fmt.Sprintf("  %-8s %s %d", c.name, bar, c.count), width)
		rows = append(rows, lipgloss.NewStyle().Foreground(cDim).Render(plain))
	}
	return strings.Join(rows, "\n")
}

// topErrorCategories returns up to n top categories sorted desc by count.
// Reads from State.ErrorCategories via the m.state field (nil-safe).
// / topErrorCategories 返回按 count 降序的前 n 个类别。
// 经 m.state 字段读 State.ErrorCategories（nil 安全）。
type errorCategory struct {
	name     string
	count    int64
	maxCount int64
}

func (m Model) topErrorCategories(n int) []errorCategory {
	if m.state == nil {
		return nil
	}
	view := m.state.ErrorCategoriesView()
	type kv struct {
		k string
		v int64
	}
	all := make([]kv, 0, len(view))
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

// totalPorts returns the State-cached total port count, or 0 when
// the State is nil. Mirror of tui.go's totalHosts() for the ports
// progress bar. / totalPorts 返回 State 缓存的总端口数；State 为
// nil 时返回 0。totalHosts() 的镜像，供 ports 进度条用。
func (m Model) totalPorts() int64 {
	if m.state == nil {
		return 0
	}
	return m.state.TotalPorts.Load()
}

// viewStage renders the PROGRESS cell: a panel title row, then alive
// and ports with renderBar sub-character progress bars against their
// State-cached totals; results / creds / errors stay as plain
// counters (no denominator). The cell is capped to height from the
// top so the title row survives; rows are truncated to width.
// / viewStage 渲染 PROGRESS 格：面板标题行 + alive/ports 依据 State
// 缓存总数画 renderBar 亚字符进度条；results / creds / errors 保持
// 纯计数（无分母）。格从顶部按 height 截断，标题行存活；行裁到
// width。
func (m Model) viewStage(height, width int) string {
	if height <= 0 {
		return ""
	}
	bp := pickBreakpoint(m.width)
	barW := 20
	if bp == BreakNarrow {
		barW = 10
	}
	rows := []string{
		stPanelHeader.Render("  PROGRESS"),
		fmt.Sprintf("  %-8s %s %d/%d", "alive",
			renderBar(int(m.counters.AliveProbed), int(m.totalHosts()), barW),
			m.counters.AliveProbed, m.totalHosts()),
		fmt.Sprintf("  %-8s %s %d/%d", "ports",
			renderBar(int(m.counters.Ports), int(m.totalPorts()), barW),
			m.counters.Ports, m.totalPorts()),
		fmt.Sprintf("  %-8s %d", "results", m.counters.Results),
		fmt.Sprintf("  %-8s %d", "creds", m.counters.Creds),
		fmt.Sprintf("  %-8s %d", "errors", m.counters.Errors),
	}
	if len(rows) > height {
		rows = rows[:height]
	}
	// Truncate plain rows only — the styled title row must never pass
	// through truncate (rune-counting would cut ANSI escapes).
	// / 只截纯文本行——上色的标题行绝不能过 truncate（按 rune 数切
	// 会切断 ANSI 转义序列）。
	for i := 1; i < len(rows); i++ {
		rows[i] = truncate(rows[i], width)
	}
	return strings.Join(rows, "\n")
}

// viewTopPlugins renders the TOP PLUGINS cell: a panel title row plus
// the top-5 hit-count bars, capped to the cell budget from the top so
// the title survives. Rows are truncated to width.
// / viewTopPlugins 渲染 TOP PLUGINS 格：面板标题行 + top-5 命中柱
// 条，从顶部按格预算截断保证标题存活。行裁到 width。
func (m Model) viewTopPlugins(height, width int) string {
	if height <= 0 {
		return ""
	}
	rows := []string{stPanelHeader.Render("  TOP PLUGINS")}
	if len(m.topPlugins) == 0 {
		rows = append(rows, "  (no hits yet)")
	} else {
		for _, p := range m.topPlugins {
			// Count → 12-char bar → name. The bar length is fixed at
			// 12 chars so the panel reads as a column even when counts
			// span 1 → 9999. Static half-fill (renderBar(6, 12, 12))
			// rather than count-proportional: proportional makes a hit
			// count of 1 look indistinguishable from a glitch.
			// 计数 → 12 字符 bar → 名称。bar 固定 12 字符让面板读作
			// 一列，即使计数跨 1 → 9999。静态半填充
			// （renderBar(6, 12, 12)）而非按计数比例——按比例的话
			// 1 命中看起来跟"故障"没区别。
			rows = append(rows, fmt.Sprintf("  %-10s %s  %s",
				p[1], renderBar(6, 12, 12), p[0]))
		}
	}
	if len(rows) > height {
		rows = rows[:height]
	}
	// Plain rows only (see viewStage) — skip the styled title.
	// / 只截纯文本行（见 viewStage）——跳过上色标题。
	for i := 1; i < len(rows); i++ {
		rows[i] = truncate(rows[i], width)
	}
	return strings.Join(rows, "\n")
}

// viewFooter renders the bottom keymap hint line. Always 1 row.
// Truncated to the cell width: inside the lattice (wide) an
// overflowing footer would break the frame's width law; outside
// (medium/narrow) it would wrap.
// / viewFooter 渲染底部 keymap 提示行。始终 1 行。裁到格宽：在
// lattice 内（wide）溢出的 footer 破坏帧宽度律；框外（medium/
// narrow）则折行。
func (m Model) viewFooter(height, width int) string {
	if height <= 0 {
		return ""
	}
	if width <= 0 {
		width = 80
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
		Foreground(cDim).
		Render(truncate(strings.Join(parts, "  "), width))
}
