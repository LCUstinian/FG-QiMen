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
// visual regression. The spinner glyph is symActive while scanning
// and symCheck once StageDone (both degrade via the symbol table).
// / stageBadge 返回旧的 "[ ▶ STAGE ]" 前缀。格式与 tui.go View()
// 第 451 行保持一致，避免视觉回归。扫描中用 symActive，StageDone
// 后用 symCheck（两者都随符号表降级）。
func (m Model) stageBadge() string {
	stage := types.StageName(int32(m.counters.Stage))
	spinner := symActive
	if int64(m.counters.Stage) == int64(types.StageDone) {
		spinner = symCheck
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

// hostPortNeed returns the rune width needed to render host:port
// untruncated — the input to the column-width hysteresis.
// / hostPortNeed 返回不截断渲染 host:port 所需的 rune 宽度——列宽
// 滞回的输入。
func hostPortNeed(host string, port int) int {
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return len(host) + 1 + len(fmt.Sprintf("%d", port)) // ':' / '：' 分隔
}

// Column hysteresis bounds (spec §5.4): the column grows for IPv6 up
// to the longest [addr]:port and shrinks back only after hostWHold
// consecutive narrow frames (2s at the 100ms beat).
// / 列宽滞回边界（spec §5.4）：IPv6 使列长最高增长到最长
// [addr]:port，收缩需连续 hostWHold 帧窄值（100ms 节拍下 2s）。
const (
	hostWMax  = 45
	hostWHold = 20
)

// ── ×N render-layer merge (spec §5.4: storage never folds) ──
// ── ×N 渲染层合并（spec §5.4：存储层不折叠）──

// mergeWindow is the max gap between adjacent same-source events for
// ×N merging (spec §5.4: 间隔 ≤1s).
// / mergeWindow 是相邻同源事件合并的最大间隔（spec §5.4：≤1s）。
const mergeWindow = time.Second

// mergeable reports whether two chronologically adjacent events merge
// into one ×N row: same source (host, port, kind), ≤1s apart, same
// local display date (a date separator breaks the run).
// / mergeable 判定两条时间相邻事件是否合并为一行 ×N：同源
// （host, port, kind）、间隔 ≤1s、显示日期相同（日期分隔行打断 run）。
func mergeable(a, b eventEntry) bool {
	return a.Host == b.Host && a.Port == b.Port && a.Kind == b.Kind &&
		b.At.Sub(a.At) >= 0 && b.At.Sub(a.At) <= mergeWindow &&
		a.At.Format("2006-01-02") == b.At.Format("2006-01-02")
}

// runBounds expands idx out to the full merge run containing it.
// / runBounds 把 idx 扩展到包含它的完整合并 run。
func runBounds(events []eventEntry, idx int) (first, last, size int) {
	first = idx
	for first > 0 && mergeable(events[first-1], events[first]) {
		first--
	}
	last = idx
	for last < len(events)-1 && mergeable(events[last], events[last+1]) {
		last++
	}
	return first, last, last - first + 1
}

// replayMax caps an expanded run's replay (spec §5.4: ≤10 条原文).
// / replayMax 限制展开 run 的重放条数（spec §5.4：≤10 条原文）。
const replayMax = 10

// evRow is one decorated display row: an event (possibly a merged ×N
// run, possibly expanded into its originals) or a separator.
// / evRow 是一行装饰后的显示行：事件（可能是合并的 ×N run，也可能
// 已展开为原文）或分隔行。
type evRow struct {
	typ     int // rowEvent | rowDateSep | rowGapSep
	entry   eventEntry
	run     int // ≥1; >1 renders the ×N suffix / >1 渲染 ×N 后缀
	firstID uint64
	lastID  uint64
	replay  []eventEntry // non-nil when the run is Enter-expanded / run 被 Enter 展开时非 nil
	label   string       // separator text / 分隔行文本
}

const (
	rowEvent = iota
	rowDateSep
	rowGapSep
)

// decorateEvents builds the display rows from the ring chronology:
// adjacent same-source events merge to ×N, day boundaries insert date
// separators, and the paused-gap separator lands right after its
// anchor event (self-cleaning: gone once the anchor leaves the ring).
// Returns the rows plus the count of entries currently folded away
// (the title's `· N merged`).
// / decorateEvents 从 ring 时序构建显示行：相邻同源合并为 ×N，跨日
// 插入日期分隔行，暂停缺口分隔行落在锚事件之后（自清理：锚事件离
// 开 ring 即消失）。返回行 + 当前被折叠省略的条数（标题的
// `· N merged`）。
func decorateEvents(events []eventEntry, expandedRun, gapAfterID uint64, gapCount int) ([]evRow, int) {
	var rows []evRow
	merged := 0
	i := 0
	for i < len(events) {
		e := events[i]
		if len(rows) > 0 {
			if prev := rows[len(rows)-1]; prev.typ == rowEvent &&
				prev.entry.At.Format("2006-01-02") != e.At.Format("2006-01-02") {
				// Local day boundary (events carry local time per the
				// pipeline contract). / 跨日边界（按管线契约事件携带本地
				// 时间）。
				rows = append(rows, evRow{typ: rowDateSep,
					label: e.At.Format("2006-01-02")})
			}
		}
		first, last, size := runBounds(events, i)
		rep := events[first]
		row := evRow{typ: rowEvent, entry: rep, run: size,
			firstID: rep.ID, lastID: events[last].ID}
		if size > 1 && expandedRun == rep.ID {
			// Replay the newest ≤replayMax originals, chronologically.
			// / 按时间序重放最新的 ≤replayMax 条原文。
			n := size
			if n > replayMax {
				n = replayMax
			}
			row.replay = events[last+1-n : last+1]
		}
		merged += size - 1
		if row.replay != nil {
			merged -= len(row.replay) - 1
		}
		rows = append(rows, row)
		if gapCount > 0 && rep.ID <= gapAfterID && gapAfterID <= events[last].ID {
			rows = append(rows, evRow{typ: rowGapSep,
				label: fmt.Sprintf("%s %d hidden while paused %s",
					strings.Repeat(glMid, 3), gapCount, strings.Repeat(glMid, 3))})
		}
		i = last + 1
	}
	return rows, merged
}

// eventLine formats one event row: fixed columns per spec §5.1 plus
// the evidence text; the host column uses the hysteresis width.
// / eventLine 格式化一行事件：spec §5.1 定宽列 + 证据文本；host 列
// 使用滞回宽度。
func (m Model) eventLine(e eventEntry) string {
	sym := padTo(symFor(e.Kind), evSymW)
	ts := e.At.Format("15:04:05")
	hostPort := formatHostPort(e.Host, e.Port, m.curHostW())
	svc := padTo(truncate(e.Service, evSvcW), evSvcW)
	line := fmt.Sprintf("  %s  %s  %s  %s", ts, sym, hostPort, svc)
	if e.Text != "" {
		// Evidence text trails the fixed columns; the outer
		// truncate caps it to the cell width. / 证据文本缀在定宽
		// 列之后；外层 truncate 负责裁到格宽。
		line += "  " + e.Text
	}
	return line
}

// eventsTitle builds the LIVE EVENTS title row: hub counters on the
// left (spec §7.2 — drop>0 must surface), the scroll-state chip on
// the right (follow / browse + lag). Separator and chip glyphs come
// from the symbol table so ASCII fallback holds here too.
// / eventsTitle 构建 LIVE EVENTS 标题行：左侧 hub 计数（spec §7.2
// ——drop>0 必须上屏），右侧滚动状态芯片（follow / browse + 滞后计
// 数）。分隔与芯片字形走符号表，ASCII 回退同样成立。
func (m Model) eventsTitle(width, merged int) string {
	left := "  LIVE EVENTS"
	if m.ingested > 0 {
		left += fmt.Sprintf(" %s %d in", glMid, m.ingested)
	}
	if m.dropped > 0 {
		left += fmt.Sprintf(" %s %d dropped", glMid, m.dropped)
	}
	if merged > 0 {
		left += fmt.Sprintf(" %s %d merged", glMid, merged)
	}
	chip := "follow " + glChipFollow
	if m.browse {
		chip = "browse " + glChipBrowse
		if m.browseLag > 0 {
			n := fmt.Sprintf("%d", m.browseLag)
			if m.browseLag > 999 {
				n = "999+"
			}
			chip += " " + glLag + n + " new"
		}
	}
	leftS := stPanelHeader.Render(left)
	chipS := stMuted.Render(chip)
	gap := width - lipgloss.Width(leftS) - lipgloss.Width(chipS)
	if gap < 1 {
		return leftS
	}
	return leftS + strings.Repeat(" ", gap) + chipS
}

// viewLiveEvents renders the LIVE EVENTS cell (spec §5.4): title row
// with hub counters + scroll chip, then the decorated event rows —
// follow glues to the tail, browse shows a viewport anchored at
// browseID with the newest rows hidden behind the ↓N counter. Storm
// mode swaps the content for the critical-only summary view. Rows are
// truncated to width BEFORE styling so the cell can never overflow;
// height=0 hides the cell unless the 'L' overlay is on.
// / viewLiveEvents 渲染 LIVE EVENTS 格（spec §5.4）：标题行含 hub 计
// 数 + 滚动芯片，下面是装饰后的事件行——follow 吸底，browse 显示以
// browseID 锚定的视口、最新行藏在 ↓N 计数之后。风暴态把内容换成
// critical-only 摘要视图。行在上色**前**裁到 width，格永不溢出；
// height=0 隐藏，除非 'L' overlay 开启。
func (m Model) viewLiveEvents(height, width int) string {
	if height <= 0 {
		return ""
	}
	if m.storm {
		return m.viewStormEvents(height, width)
	}
	events := m.eventsOrdered()
	rows, merged := decorateEvents(events, m.expandedRun, m.gapAfterID, m.gapCount)
	title := m.eventsTitle(width, merged)
	var window []evRow
	switch {
	case len(rows) == 0:
		return title + "\n" + lipgloss.NewStyle().Foreground(cDim).Render("  (no events yet)")
	case m.browse:
		// Viewport bottom = the row containing the anchor event;
		// evicted anchors clamp to the oldest row.
		// / 视口底 = 锚点事件所在行；被淘汰的锚点钳到最老行。
		bottom := len(rows) - 1
		for i, r := range rows {
			if r.typ == rowEvent && r.lastID >= m.browseID {
				bottom = i
				break
			}
		}
		start := bottom - (height - 2)
		if start < 0 {
			start = 0
		}
		window = rows[start : bottom+1]
	default: // follow: glue to the tail / follow：吸底
		start := len(rows) - (height - 1)
		if start < 0 {
			start = 0
		}
		window = rows[start:]
	}
	out := make([]string, 0, len(window)+1)
	out = append(out, title)
	for _, r := range window {
		switch r.typ {
		case rowDateSep:
			out = append(out, lipgloss.NewStyle().Foreground(cDim).
				Render(truncate("  "+strings.Repeat(boxH, 2)+" "+r.label+" "+
					strings.Repeat(boxH, 2), width)))
		case rowGapSep:
			out = append(out, lipgloss.NewStyle().Foreground(cDim).
				Render(truncate("  "+r.label, width)))
		default:
			if r.replay != nil {
				for _, e := range r.replay {
					out = append(out, lipgloss.NewStyle().
						Foreground(m.severityColor(e)).
						Render(truncate(m.eventLine(e), width)))
				}
				continue
			}
			line := m.eventLine(r.entry)
			if r.run > 1 {
				line += fmt.Sprintf(" %s%d", glFold, r.run)
			}
			out = append(out, lipgloss.NewStyle().
				Foreground(m.severityColor(r.entry)).
				Render(truncate(line, width)))
		}
	}
	return strings.Join(out, "\n")
}

// viewStormEvents renders the storm-mode EVENTS cell (spec §5.4 风暴
// 显示): one summary line (rate · hits% · critical only) at 2Hz-ish
// freshness, then the critical sidecar rows individually (≤8) and a
// count for sidecar-evicted criticals.
// / viewStormEvents 渲染风暴态的 EVENTS 格（spec §5.4 风暴显示）：
// 顶部一行摘要（速率 · 命中率 · critical only），下面 critical 侧车
// 行逐条上屏（≤8），被侧车淘汰的 critical 以计数呈现。
func (m Model) viewStormEvents(height, width int) string {
	title := m.eventsTitle(width, 0)
	// hits% derived from the ring window (≤64 samples) — cumulative
	// counters would smear the storm window with pre-storm history.
	// / hits% 从 ring 窗口导出（≤64 样本）——累计计数会把风暴前历
	// 史糊进窗口。
	total := len(m.eventsOrdered())
	hits := 0
	for _, e := range m.eventsOrdered() {
		if isHitKind(e.Kind) {
			hits++
		}
	}
	pct := 0
	if total > 0 {
		pct = hits * 100 / total
	}
	summary := lipgloss.NewStyle().Foreground(cWarn).Render(truncate(
		fmt.Sprintf("  %s %d ev/s %s %d%% hits %s critical only",
			glWarn, m.stormRate, glMid, pct, glMid), width))
	rows := []string{title, summary}
	crit := m.critOrdered()
	if m.critTotal > len(crit) {
		rows = append(rows, lipgloss.NewStyle().Foreground(cDim).Render(truncate(
			fmt.Sprintf("  + %d more criticals", m.critTotal-len(crit)), width)))
	}
	// Fill the remaining viewport with the newest sidecar rows.
	// / 剩余视口填最新的侧车行。
	n := height - len(rows)
	if n > 0 {
		if n > len(crit) {
			n = len(crit)
		}
		for _, e := range crit[len(crit)-n:] {
			rows = append(rows, lipgloss.NewStyle().Foreground(m.severityColor(e)).
				Render(truncate(m.eventLine(e), width)))
		}
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
// State-cached totals, and the done/inflight/deferred ledger rows
// (§5.5, wide/medium only) with the stall detector. The legacy plain
// results/creds/errors counters are retired — the spec §5.2 frames
// pin PROGRESS to bars + ledger (creds surface as [*] events, errors
// have their own ERRORS cell). The cell is capped to height from the
// top so the title row survives; rows are truncated to width.
// / viewStage 渲染 PROGRESS 格：面板标题行 + alive/ports 依据 State
// 缓存总数画 renderBar 亚字符进度条 + done/inflight/deferred 账本行
// （§5.5，仅 wide/medium）+ 停滞检测。旧版 results/creds/errors 纯
// 计数行退役——spec §5.2 框图钉死 PROGRESS = 条 + 账本（creds 以
// [*] 事件呈现，errors 有专属 ERRORS 格）。格从顶部按 height 截断，
// 标题行存活；行裁到 width。
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
	}
	// Progress ledger (spec §5.5): the ports row splits into
	// done / inflight / deferred (done=Ports, inflight=pool mirror,
	// deferred=total−done−inflight clamped ≥0) plus the stall
	// detector. Wide renders two ledger rows (§5.2 frame), medium
	// one, narrow none. The stall cell is pre-styled and must never
	// pass through truncate (rune-counting cuts ANSI escapes), so
	// each ledger row's PLAIN prefix is truncated to leave room and
	// the styled cell is appended afterwards; the plain-row pass
	// below skips them.
	// / 进度账本（spec §5.5）：ports 行分解为 done / inflight /
	// deferred（done=Ports，inflight=池镜像，deferred=total−done−
	// inflight 钳 ≥0）+ 停滞检测。宽屏两行账本（§5.2 框图）、medium
	// 一行、narrow 无。stall 单元预上色且绝不能过 truncate（按 rune
	// 数切会断 ANSI 转义），因此每行账本的纯文本前缀先截出空间，再
	// 追加上色单元；下方纯文本行截断循环跳过账本行。
	ledgerStart, ledgerEnd := 0, 0
	if bp != BreakNarrow {
		done := m.counters.Ports
		def := m.totalPorts() - done - m.counters.Inflight
		if def < 0 {
			def = 0
		}
		stall := m.stallCell()
		stallW := lipgloss.Width(stall)
		ledgerStart = len(rows)
		if bp == BreakWide {
			rows = append(rows,
				fmt.Sprintf("  done %d   inflight %d", done, m.counters.Inflight),
				truncate(fmt.Sprintf("  deferred %d   ", def), max(0, width-stallW))+stall,
			)
		} else {
			rows = append(rows,
				truncate(fmt.Sprintf("  done %d  inflight %d  deferred %d   ",
					done, m.counters.Inflight, def), max(0, width-stallW))+stall,
			)
		}
		ledgerEnd = len(rows)
	}
	if len(rows) > height {
		rows = rows[:height]
	}
	// Truncate plain rows only — the styled title row must never pass
	// through truncate (rune-counting would cut ANSI escapes), and the
	// ledger rows carry the pre-styled stall cell (already
	// width-safe). / 只截纯文本行——上色的标题行绝不能过 truncate
	// （按 rune 数切会断 ANSI 转义序列），账本行携带预上色的 stall
	// 单元（已保证宽度安全）。
	for i := 1; i < len(rows); i++ {
		if i >= ledgerStart && i < ledgerEnd {
			continue
		}
		rows[i] = truncate(rows[i], width)
	}
	return strings.Join(rows, "\n")
}

// stallCell renders the stall read-out for the progress ledger
// (spec §5.5): plain `stall 0s` while quiet, `stall Ns ▲` (warn) from
// 15s of no done-count movement, `stall Ns !!` (err) from 60s. Glyphs
// come from the symbol table (glWarn → '!' under ASCII fallback) and
// follow the char charter (⚠ was retired as scarce-width).
// / stallCell 渲染进度账本的停滞读数（spec §5.5）：安静时纯文本
// `stall 0s`，done 计数 15s 无变动起 `stall Ns ▲`（warn），60s 起
// `stall Ns !!`（err）。字形走符号表（ASCII 回退下 glWarn → '!'），
// 遵循字符宪章（⚠ 因宽度稀缺已退役）。
func (m Model) stallCell() string {
	s := fmt.Sprintf("stall %ds", m.stallSec)
	switch {
	case m.stallSec >= 60:
		return stErr.Render(s + " !!")
	case m.stallSec >= 15:
		return stWarn.Render(s + " " + glWarn)
	default:
		return s
	}
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
