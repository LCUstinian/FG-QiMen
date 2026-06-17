// Package tui provides the Bubbletea-based terminal UI.
//
// Package tui 提供基于 Bubbletea 的终端 UI。
//
// The Model has three responsibilities:
//  1. Render the dashboard (title bar, stats, live events, keymap)
//  2. Receive events from the core pipeline (via the Events() channel)
//  3. Forward Stats() pushes to the dashboard counters
//
// Bubbletea handles all event-loop concerns; we disable its default
// SIGINT handler (tea.WithoutSignalHandler) and let the main goroutine
// own the shutdown flow (see cmd/root.go).
//
// Bubbletea 处理所有事件循环；我们禁用其默认 SIGINT handler
// （tea.WithoutSignalHandler），由 main goroutine 负责关闭流程
// （见 cmd/root.go）。
//
// Layout model:
//   - width >= minWidth  → two columns side-by-side
//   - width <  minWidth  → single column stack (stats above events)
//   - height is used to clamp the events list so the dashboard never
//     overflows the terminal. chromeLines (in styles.go) accounts
//     for the title bar, stats bar, keymap and blank lines.
//
// 布局模型：
//   - width >= minWidth  → 两栏并排
//   - width <  minWidth  → 单列堆叠（统计在上，事件在下）
//   - height 用于裁剪事件列表，避免 dashboard 溢出终端。chromeLines
//     （见 styles.go）覆盖标题栏、状态条、按键提示和空行。
package tui

import (
	"fmt"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/LCUstinian/FG-QiMen/internal/types"
	"github.com/LCUstinian/FG-QiMen/internal/version"
)

// maxLiveEvents caps how many events the TUI remembers for display.
// 200 keeps the ring buffer small enough that a slow consumer
// can't grow it unbounded (10s of creds/sec × 200 = ~20s of
// history; more than that is wasted on a 24-row terminal).
//
// maxLiveEvents 限制 TUI 保留的最近事件数。200 让环形缓冲足够小，
// 慢消费者不会无限增长（10s 条凭据/秒 × 200 = 约 20s 历史；再久
// 在 24 行的终端上也是浪费）。
const maxLiveEvents = 200

// liveEvent is a single result entry rendered in the right column.
// liveEvent 是右栏显示的单条结果。
type liveEvent struct {
	when string
	tag  string // "scan" / "cred" / "err"
	host string
	port int
	svc  string
	text string
}

// Mode is the dashboard's interactive state.
// Mode 是 dashboard 的交互状态。
type mode int

const (
	// modeRun is the normal scanning state. / modeRun 普通扫描态。
	modeRun mode = iota
	// modePaused freezes event ingestion: new events queue in the
	// dispatcher's buffer but the dashboard stops re-rendering on
	// them. The pipeline itself is *not* paused by the TUI — that
	// would race with the scan goroutine's shutdown contract. The
	// TUI just stops *displaying* new events so the operator can
	// read the screen.
	// modePaused 冻结事件摄入：新事件在 dispatcher 缓冲里排队，
	// 但 dashboard 停止对它们重渲染。pipeline 本身不被 TUI 暂停
	// —— 那会与 scan goroutine 的关闭契约赛跑。TUI 只是停止*显示*
	// 新事件，让操作员能看清屏幕。
	modePaused
	// modeHelp shows the help overlay. / modeHelp 显示帮助浮层。
	modeHelp
)

// Model is the Bubbletea model for the dashboard.
// Model 是 Bubbletea dashboard 的 model。
type Model struct {
	// Width / height are the terminal size; bubbletea auto-updates them
	// via WindowSizeMsg.
	// Width / height 是终端尺寸；bubbletea 通过 WindowSizeMsg 自动更新。
	width  int
	height int

	// Stats snapshot / 统计快照
	counters types.CountersView
	elapsed  string
	mode     string
	project  string

	// pending events appended by the dispatcher; flushed into
	// events on the next tick. Keeps the dispatcher contract
	// (append-only) intact while letting the model amortise the
	// cost of re-sorting/trimming to one operation per render.
	// pending 事件由 dispatcher 追加；在下一次 tick 时刷入 events。
	// 保留 dispatcher 的追加契约，同时让模型在每次渲染时把排序
	// /修剪的开销摊销成一次。
	pending []liveEvent

	// Live events (newest last) / 实时事件（最新在末尾）
	events []liveEvent

	// uiMode is the dashboard interaction state. / uiMode 是
	// dashboard 的交互状态。
	uiMode mode

	// Quit flag / 退出标志
	quitting bool

	// Final summary printed after bubbletea exits / bubbletea 退出
	// 后打印的最终摘要。
	finalSummary string
}

// NewModel constructs a fresh dashboard model.
// NewModel 构造一个新的 dashboard model。
func NewModel(cfg *types.Config) Model {
	mode := "scan"
	if cfg != nil {
		mode = string(cfg.Mode)
	}
	project := ""
	if cfg != nil {
		project = cfg.Project
	}
	return Model{
		mode:    mode,
		project: project,
	}
}

// Init is a no-op. Events arrive via Event/Stats pushers.
// Init 是空操作。事件通过 Event/Stats 推入。
func (m Model) Init() tea.Cmd { return nil }

// Update handles bubbletea messages (keypresses, window resize, etc.).
// Update 处理 bubbletea 消息（按键、窗口大小变化等）。
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case tea.KeyMsg:
		// Help overlay eats every key except '?' / 'q' / 'esc'.
		// 帮助浮层只放过 '?' / 'q' / 'esc'。
		if m.uiMode == modeHelp {
			switch msg.String() {
			case "q", "ctrl+c", "esc", "?":
				m.uiMode = modeRun
			}
			return m, nil
		}
		switch msg.String() {
		case "q", "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		case "p":
			// Toggle pause / 切换暂停
			if m.uiMode == modeRun {
				m.uiMode = modePaused
			}
		case "r":
			// Resume from pause / 从暂停恢复
			if m.uiMode == modePaused {
				m.uiMode = modeRun
			}
		case "?":
			m.uiMode = modeHelp
		}
	}
	// Drain pending into events on every non-keyboard message too
	// (statsMsg / eventMsg flow through the dispatcher, which calls
	// Model.Update via the fallthrough path with the *dispatcher's*
	// inner model already mutated). Doing the drain here keeps
	// model.Update() self-contained: any caller that bypasses the
	// dispatcher still flushes pending → events.
	//
	// 每次非键盘消息也把 pending 刷入 events（statsMsg / eventMsg 经
	// dispatcher 流入，dispatcher 通过 fallthrough 调 Model.Update
	// 时其 inner model 已经被改过）。在这里 drain 让 model.Update()
	// 自我包含：任何绕过 dispatcher 的调用者也能把 pending → events。
	if len(m.pending) > 0 {
		m.events = append(m.events, m.pending...)
		m.pending = m.pending[:0]
		if len(m.events) > maxLiveEvents*2 {
			m.events = m.events[len(m.events)-maxLiveEvents:]
		}
	}
	return m, nil
}

// appendEvent is the model-side hook used by the dispatcher to
// push a new event. We keep it on the model (not just in the
// dispatcher) so future renderers (tests, headless replay) can
// drive the model without a bubbletea program.
//
// appendEvent 是 dispatcher 用来推送新事件的 model 侧钩子。把它放
// 在 model 上（不只是 dispatcher），让未来的渲染器（测试、无头回
// 放）能在没有 bubbletea program 的情况下驱动 model。
func (m *Model) appendEvent(ev liveEvent) {
	if m.uiMode == modePaused {
		// Drop on the floor: paused mode freezes display. We
		// don't buffer because the pipeline may produce >maxLiveEvents
		// events during a long pause; better to lose history than
		// to OOM the dashboard.
		// 暂停态直接丢弃：暂停冻结显示。我们不缓冲，因为 pipeline
		// 在长暂停期间可能产出 >maxLiveEvents 条事件；丢历史比
		// OOM 掉 dashboard 好。
		return
	}
	m.pending = append(m.pending, ev)
}

// View renders the dashboard. Returns a single string that lipgloss
// will then lay out.
// View 渲染 dashboard。返回 lipgloss 将布局的单个字符串。
func (m Model) View() string {
	if m.quitting {
		return m.finalSummary + "\n"
	}
	if m.uiMode == modeHelp {
		return m.renderHelp()
	}
	var sb strings.Builder

	// Title bar / 标题栏
	// Version sourced from internal/version so a `just build`
	// (which injects via -ldflags) and `go run .` from a clean
	// checkout show the same number; the v0.2 audit (doc-1) flagged
	// the prior hard-coded "v0.1" as user-visible drift.
	//
	// 版本从 internal/version 取，使 `just build`（经 -ldflags 注入）
	// 和干净 checkout 下的 `go run .` 显示一致；v0.2 审计（doc-1）
	// 把硬编码的 "v0.1" 标为用户可见漂移。
	title := fmt.Sprintf(
		"%s FG-QIMEN %s %s project: %s %s mode: %s %s",
		boxTL, version.Value, boxH, m.project, boxH, m.mode, boxTR,
	)
	sb.WriteString(stTitle.Render(title))
	sb.WriteString("\n")

	// Stats bar / 状态条
	stats := fmt.Sprintf(
		"%s alive=%d ports=%d results=%d creds=%d errors=%d  elapsed=%s",
		symSpinner, m.counters.Alive, m.counters.Ports, m.counters.Results,
		m.counters.Creds, m.counters.Errors, m.elapsed,
	)
	// Pause indicator is appended to the stats bar so the operator
	// can tell at a glance that the dashboard is frozen (the
	// pipeline is still running).
	// 暂停指示器加在状态条末尾，让操作员一眼看出 dashboard 已冻结
	// （pipeline 仍在跑）。
	if m.uiMode == modePaused {
		stats = stats + "  " + stWarn.Render("[PAUSED]")
	}
	sb.WriteString(stDim.Render(stats))
	sb.WriteString("\n\n")

	// Two columns on wide terminals, stack on narrow ones.
	// 宽终端两栏，窄终端堆叠。
	left := m.renderStatsCol()
	right := m.renderEventsCol()
	var row string
	if m.twoColumn() {
		row = lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	} else {
		row = left + "\n" + right
	}
	sb.WriteString(row)
	sb.WriteString("\n\n")

	// Keymap / 快捷键
	sb.WriteString(m.renderKeymap())
	sb.WriteString("\n")
	return sb.String()
}

// renderHelp returns the help overlay. Centre-aligned in the
// available width when the terminal is known, otherwise renders
// flush-left so it never breaks on a 0-width start-up race.
//
// renderHelp 返回帮助浮层。终端宽度已知时居中，否则左对齐，避免
// 启动 0 宽竞态时换行。
func (m Model) renderHelp() string {
	type row struct{ key, desc string }
	rows := []row{
		{"q / Ctrl-C", "quit the scan"},
		{"p", "pause the dashboard display (pipeline keeps running)"},
		{"r", "resume the dashboard display"},
		{"?", "toggle this help overlay"},
	}
	// Stable order so the overlay reads the same across renders.
	// 稳定排序，浮层每次读起来一致。
	sort.Slice(rows, func(i, j int) bool { return rows[i].key < rows[j].key })

	var sb strings.Builder
	sb.WriteString(stPanelHeader.Render("KEYMAP"))
	sb.WriteString("\n\n")
	for _, r := range rows {
		kb := stKeyHint.Render(" " + r.key + " ")
		sb.WriteString(fmt.Sprintf("  %s  %s\n", kb, r.desc))
	}
	sb.WriteString("\n")
	sb.WriteString(stMuted.Render("press ? or esc to close"))
	body := stHelp.Render(sb.String())
	if w := m.width; w > 0 {
		return lipgloss.Place(w, m.height, lipgloss.Center, lipgloss.Center, body)
	}
	return body
}

// renderKeymap renders the bottom keymap as a row of pill-shaped
// key chips + dim descriptors. The chips use the accent bg color
// so the eye lands on the keys first, the action second.
//
// renderKeymap 把底部按键提示渲染成一行药丸形按键芯片 + dim 描述。
// 芯片用 accent 背景色，视线先落按键再落动作。
func (m Model) renderKeymap() string {
	type chip struct{ key, desc string }
	parts := []chip{
		{"q", "quit"},
		{"p", "pause"},
		{"r", "resume"},
		{"?", "help"},
	}
	if m.uiMode == modePaused {
		parts[1] = chip{"p", "paused"}
	}
	var sb strings.Builder
	for i, p := range parts {
		if i > 0 {
			sb.WriteString("  ")
		}
		sb.WriteString(stKeyHint.Render(" " + p.key + " "))
		sb.WriteString(" ")
		sb.WriteString(stDim.Render(p.desc))
	}
	return sb.String()
}

// twoColumn reports whether the current width supports the
// two-column layout. minWidth is the floor; below it we stack
// to avoid horizontal overflow on 80×24 terminals.
//
// twoColumn 报告当前宽度是否支持两栏布局。minWidth 是下限；低于
// 此值时堆叠以避免 80×24 终端横向溢出。
func (m Model) twoColumn() bool { return m.width >= minWidth }

// renderStatsCol builds the left "Targets" column.
// renderStatsCol 构建左侧 "Targets" 列。
func (m Model) renderStatsCol() string {
	// Right-pad the labels to 12 chars so the counters line up
	// even when one of them grows from 9 → 10 digits. We use
	// a plain fmt.Sprintf (not lipgloss width) because the
	// numbers are ASCII and we want exact column alignment.
	// 标签右补到 12 字符，让计数器即使从 9 位变 10 位也对齐。用
	// 普通 fmt.Sprintf（不是 lipgloss width）是因为数字是 ASCII
	// 且我们要严格列对齐。
	rows := [][2]string{
		{"alive", fmt.Sprintf("%d", m.counters.Alive)},
		{"ports", fmt.Sprintf("%d", m.counters.Ports)},
		{"results", fmt.Sprintf("%d", m.counters.Results)},
		{"creds", fmt.Sprintf("%d", m.counters.Creds)},
		{"errors", fmt.Sprintf("%d", m.counters.Errors)},
	}
	var body strings.Builder
	body.WriteString(stPanelHeader.Render("TARGETS"))
	body.WriteString("\n")
	for _, r := range rows {
		label := stDim.Render(fmt.Sprintf("  %-12s", r[0]))
		body.WriteString(label)
		body.WriteString(stCounter.Render(r[1]))
		body.WriteString("\n")
	}
	// On stacked layout, the right column will start with its own
	// header so we don't need a panel border around the stats.
	// 堆叠布局下右栏会带自己的标题，所以统计区不需要面板边框。
	if m.twoColumn() {
		return stBox.Width(statsColWidth).Render(body.String())
	}
	return body.String()
}

// renderEventsCol builds the right "Live Events" column.
// renderEventsCol 构建右侧 "Live Events" 列。
func (m Model) renderEventsCol() string {
	// eventsBudget is the number of event rows we can show
	// without overflowing the terminal. chromeLines accounts for
	// the title bar (1), stats bar (1), blank line (1), blank
	// line after the row (1), and keymap (1). One more for the
	// panel header.
	//
	// eventsBudget 是能显示且不溢出终端的事件行数。chromeLines
	// 覆盖标题栏 (1)、状态条 (1)、空行 (1)、行后空行 (1)、按键
	// 提示 (1)。再 +1 给面板标题。
	eventsBudget := m.height - chromeLines
	if eventsBudget < 1 {
		eventsBudget = 1
	}
	if eventsBudget > maxLiveEvents {
		eventsBudget = maxLiveEvents
	}

	// Build the (newest) tail of the events slice that fits the
	// budget. We don't mutate the source slice — View() is a
	// pure function of Model state.
	// 构造 events 切片的（最新）尾部以适配预算。不修改源切片——
	// View() 是 Model 状态的纯函数。
	start := 0
	if len(m.events) > eventsBudget {
		start = len(m.events) - eventsBudget
	}
	visible := m.events[start:]

	var body strings.Builder
	header := fmt.Sprintf("LIVE EVENTS  %s", stMuted.Render(fmt.Sprintf("(%d)", len(m.events))))
	body.WriteString(stPanelHeader.Render(header))
	body.WriteString("\n")
	for _, ev := range visible {
		var sym string
		var style lipgloss.Style
		switch ev.tag {
		case "cred":
			sym, style = symWarn, stWarn
		case "err":
			sym, style = symError, stError
		default:
			sym, style = symSuccess, stSuccess
		}
		// Trim long banners so a 4KB HTTP response doesn't smear
		// across 20 lines on a 120-col terminal. The right column
		// gets the remaining width after the fixed fields.
		// 裁剪过长 banner，避免 4KB HTTP 响应在 120 列终端上铺
		// 20 行。右列用固定字段之后的剩余宽度。
		text := ev.text
		maxText := m.eventTextWidth()
		if maxText > 0 && lipgloss.Width(text) > maxText {
			text = truncate(text, maxText, "…")
		}
		// Layout: "  ▸ HH:MM:SS  host:port  [svc]  text" — host:port
		// is concatenated inline (not fixed width) so the column
		// doesn't grow tails of whitespace on short hostnames.
		// Stamping the timestamp at the leading edge makes the
		// events column read top-down without re-orienting.
		// 布局："  ▸ HH:MM:SS  host:port  [svc]  text"——host:port
		// 内联拼接（非固定宽），避免短 host 把列尾拉出空白。时间
		// 戳在最前，从上往下读事件列时无需重新定位。
		host := ev.host
		if ev.port > 0 {
			host = fmt.Sprintf("%s:%d", ev.host, ev.port)
		}
		svc := stMuted.Render(fmt.Sprintf("[%s]", ev.svc))
		when := stMuted.Render(ev.when)
		line := fmt.Sprintf("  %s %s  %s  %s  %s", style.Render(sym), when, host, svc, text)
		body.WriteString(line)
		body.WriteString("\n")
	}

	if m.twoColumn() {
		// Compute the right column width from the terminal: total
		// width minus the fixed left panel + a 2-space gap. Floor
		// at eventsColMin so a narrow-but-above-minWidth terminal
		// doesn't crush the right column into 2 chars.
		// 计算右栏宽度：总宽 - 左固定面板 - 2 空格。保底 eventsColMin，
		// 避免稍宽但仍窄的终端把右栏压成 2 字符。
		rightW := m.width - statsColWidth - 2
		if rightW < eventsColMin {
			rightW = eventsColMin
		}
		return stBox.Width(rightW).Render(body.String())
	}
	return body.String()
}

// eventTextWidth returns the per-row budget for the event text
// field, or 0 to disable truncation. The budget is computed from
// the right-column width minus the fixed prefix (timestamp +
// symbol + svc tag + padding). 0 means "no truncation" (e.g. on
// a fresh model with unknown width).
//
// eventTextWidth 返回事件文本字段的每行预算，0 表示不裁剪。预算 =
// 右栏宽度 - 固定前缀（时间戳 + 符号 + svc 标签 + padding）。0
// 表示"不裁剪"（例如刚启动宽度未知的 model）。
func (m Model) eventTextWidth() int {
	if m.width == 0 {
		return 0
	}
	rightW := m.width - statsColWidth - 4 // border + padding slack
	if !m.twoColumn() {
		rightW = m.width - 4
	}
	if rightW < eventsColMin {
		rightW = m.width // best-effort on very narrow terminals
	}
	// Fixed fields: "  ▸ " (4) + "HH:MM:SS" (8) + "  " (2) + "[svc]" (~6) + "  " (2)
	const fixed = 22
	budget := rightW - fixed
	if budget < 8 {
		return 8
	}
	return budget
}

// truncate shortens s to maxW display columns, appending an
// ellipsis if anything was dropped. maxW must be > 0.
//
// truncate 把 s 截短到 maxW 个显示列，如果截掉了就加省略号。
// maxW 必须 > 0。
func truncate(s string, maxW int, ell string) string {
	if maxW <= 0 {
		return s
	}
	if lipgloss.Width(s) <= maxW {
		return s
	}
	// Walk rune-by-rune tracking display width. lipgloss.Width is
	// the canonical measurement; for a hot path (every event row
	// on every render) we use a small inline loop instead of
	// allocating a []rune.
	// 按 rune 遍历并累计显示宽度。lipgloss.Width 是规范的度量方
	// 法；在热路径（每次渲染每行事件）上我们用小内联循环，避免
	// 分配 []rune。
	cur := 0
	cut := len(s)
	for i, r := range s {
		w := lipgloss.Width(string(r))
		if cur+w+lipgloss.Width(ell) > maxW {
			cut = i
			break
		}
		cur += w
	}
	return s[:cut] + ell
}
