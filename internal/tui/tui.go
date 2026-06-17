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
	"time"

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

// runState is the pipeline lifecycle reflected on the dashboard.
// Distinct from mode: mode is the *user*'s interaction state
// (paused / running), runState is the *pipeline*'s state
// (idle → scanning → done). They compose: a paused + scanning
// dashboard shows the SCANNING chip alongside the PAUSED chip.
// runState 是 dashboard 上反映的 pipeline 生命周期。区别于
// mode：mode 是*用户*的交互态（暂停/运行），runState 是*pipeline*
// 的状态（空闲 → 扫描 → 完成）。两者组合：暂停+扫描的 dashboard
// 同时显示 SCANNING 芯片和 PAUSED 芯片。
type runState int

const (
	// runIdle: no stats push has arrived yet. The operator just
	// launched the scan; we want a clear "waiting" signal that
	// is distinguishable from "scan is running but quiet" (the
	// previous behaviour showed a static spinner with no other
	// indicator, which read as "is it even doing anything?").
	// runIdle：还没有 stats 推送到达。操作员刚启动扫描；我们
	// 想给个清晰的"等待"信号，与"在跑但安静"区分开（之前只
	// 有一个静态 spinner，缺少其他指示，读起来像"它在动吗？"）。
	runIdle runState = iota
	// runScanning: at least one statsMsg has been received; the
	// pipeline is actively progressing.
	// runScanning：收到过至少一条 statsMsg；pipeline 在活跃推进。
	runScanning
	// runDone: a doneMsg has been received. We keep the dashboard
	// mounted for a brief "linger" period (lingerTicks) so the
	// operator can read the final summary inside the TUI frame
	// before bubbletea exits to the terminal. The linger is what
	// fixes the "I can't tell if it actually finished" problem.
	// runDone：收到 doneMsg。我们让 dashboard 短暂"停留"
	// （lingerTicks）几帧，让操作员在 bubbletea 退出终端前能在
	// TUI 框内读到最终摘要。linger 修复了"我分不清它到底完没完
	// 成了"的问题。
	runDone
)

// spinnerTick is the cadence at which the dashboard re-renders
// just to advance the spinner glyph. Independent of statsMsg
// cadence (1Hz) — at 1Hz the spinner would look static between
// stats updates. 100ms gives a smooth 10fps rotation without
// burning CPU on idle terminals.
//
// spinnerTick 是 dashboard 仅为推进 spinner 字形而重渲染的节
// 拍。独立于 statsMsg 节拍（1Hz）——1Hz 下 spinner 在两次 stats
// 之间会显得静止。100ms 给出平滑的 10fps 旋转，又不会让空闲终
// 端烧 CPU。
const spinnerTick = 100 * time.Millisecond

// lingerTicks is how many additional spinner frames the dashboard
// stays mounted after runDone before bubbletea actually quits.
// At 100ms/tick this is ~1.5s — long enough for the operator
// to read the summary line, short enough that an impatient user
// pressing 'q' still exits immediately (the quit path bypasses
// the linger).
//
// lingerTicks 是 runDone 之后 dashboard 在 bubbletea 真正退出前
// 保持挂载的额外 spinner 帧数。100ms/tick 下约 1.5s——够操作员读
// 完摘要行，又不耽误不耐烦的用户按 'q' 立即退出（quit 路径绕
// 过 linger）。
const lingerTicks = 15

// tickMsg advances the spinner frame. Bubbletea uses this pattern
// (Update returning a tea.Cmd that sends itself) instead of timers
// the model has to poll, so the model only re-renders when
// something actually changed.
//
// tickMsg 推进 spinner 帧。bubbletea 用这个模式（Update 返回一个
// 给自己发消息的 tea.Cmd）代替让 model 轮询的 timer，所以 model
// 只在确实有变化时才重渲染。
type tickMsg time.Time

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

	// runState mirrors the pipeline lifecycle (idle/scanning/done)
	// for the status bar chip + title bar. Independent of uiMode.
	// runState 镜像 pipeline 生命周期（空闲/扫描/完成），供状态
	// 条芯片 + 标题栏使用。独立于 uiMode。
	runState runState

	// frameIdx is the current frame in the spinner rotation. We
	// keep it on the model (not in styles) so the rotation is
	// driven by tickMsg, not a global counter.
	// frameIdx 是 spinner 旋转的当前帧。放在 model 上（而非
	// styles）让旋转由 tickMsg 驱动，而非全局计数器。
	frameIdx int

	// lingerLeft counts down the linger frames after runDone; the
	// dashboard exits the bubbletea loop when it hits zero (unless
	// the user pressed 'q', which exits immediately).
	// lingerLeft 在 runDone 后递减；归零时 dashboard 退出
	// bubbletea 循环（除非用户按了 'q'，那条路径立即退出）。
	lingerLeft int

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

// Init kicks off the spinner tick. The tick self-perpetuates via
// the returned tickCmd closure: each tickMsg schedules the next
// one, so the spinner keeps rotating until the program quits.
// bubbletea handles the timing — the model doesn't poll.
//
// Init 启动 spinner tick。tick 通过返回的 tickCmd 闭包自我延续：
// 每条 tickMsg 排下一条，spinner 一路转下去直到 program 退出。
// bubbletea 处理时序——model 不轮询。
func (m Model) Init() tea.Cmd { return tickCmd() }

// tickCmd returns a tea.Cmd that sends a tickMsg after one
// spinnerTick interval. The closure captures nothing (the time
// is in the message itself), so multiple ticks don't fight over
// shared state.
//
// tickCmd 返回一条 tea.Cmd，在 spinnerTick 间隔后发 tickMsg。闭
// 包不捕获任何状态（时间在消息里），多 tick 不会争共享状态。
func tickCmd() tea.Cmd {
	return tea.Tick(spinnerTick, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// Update handles bubbletea messages (keypresses, window resize, etc.).
// Update 处理 bubbletea 消息（按键、窗口大小变化等）。
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tickMsg:
		// Advance the spinner frame. This is the only thing the
		// tick drives when there's nothing else to do — when
		// statsMsg / eventMsg arrive they push the runState
		// forward, but the spinner must keep moving regardless
		// of data flow (otherwise an idle scan looks frozen).
		//
		// 推进 spinner 帧。tick 只驱动这一件事——当 statsMsg /
		// eventMsg 到达时它们推 runState 前进，但 spinner 必
		// 须独立于数据流继续转（否则空闲扫描看起来冻住了）。
		m.frameIdx = (m.frameIdx + 1) % len(spinnerFrames)
		// Linger countdown: in runDone we keep ticking the
		// spinner for `lingerLeft` more frames so the operator
		// can read the final summary inside the TUI frame.
		// 当达到 0 时真正退出，bypassing 让 'q' 立即退出。
		if m.runState == runDone {
			m.lingerLeft--
			if m.lingerLeft <= 0 {
				m.quitting = true
				return m, tea.Quit
			}
		}
		// Schedule the next tick. Re-issuing the cmd from Update
		// is the bubbletea-canonical way to self-perpetuate a
		// timer: the runtime runs the cmd, gets the Msg, runs
		// Update, which returns a new cmd, repeat.
		// 排下一条 tick。从 Update 重新发出 cmd 是 bubbletea
		// 自延续 timer 的标准做法：runtime 跑 cmd → 拿到 Msg
		// → 跑 Update → 返回新 cmd → 循环。
		cmd = tickCmd()
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
	return m, cmd
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
	// The title is just a plain line of text — no surrounding
	// box characters, no full-row background. A previous version
	// had `┌─...─┐` brackets AND a Background that filled the
	// whole row, which (combined with the stats bar below and
	// the panel borders further below) stacked three boxes
	// visually: the operator's eye read it as redundant chrome.
	// Foreground-only title + a thin separator below gives the
	// dashboard a clean three-layer stack: title / stats / panels.
	// 标题就是一行文字——无外框字符、无整行背景。之前的版本带
	// `┌─...─┐` 框 + 整行 Background，叠加下方状态条和更下方的
	// 面板边框后视觉上叠了 3 层框：操作员读作冗余 chrome。纯前
	// 景标题 + 下方细分割线让 dashboard 形成清晰的三层堆叠：
	// 标题 / 状态 / 面板。
	titleChip := m.runStateChip()
	title := fmt.Sprintf(
		" FG-QIMEN %s  project: %s   mode: %s   %s",
		version.Value, m.project, m.mode, titleChip,
	)
	sb.WriteString(stTitle.Render(title))
	sb.WriteString("\n")

	// Thin separator under the title: a dim row of horizontal
	// line characters the width of the terminal (or 80 chars on
	// a 0-width start-up). This is the *only* chrome line in the
	// header — replacing what used to be a background-coloured
	// title bar AND a stats-bar padding. The single dim line
	// reads as "this is where the header ends".
	// 标题下方的细分割线：dim 色横线一行，宽与终端同（启动 0 宽
	// 时为 80 字符）。这是 header 区唯一的 chrome 行——替代了
	// 之前用背景色标题栏 + 状态条 padding 凑出来的"两层"。单
	// 一 dim 线读作"header 在此结束"。
	sb.WriteString(stDim.Render(m.titleSeparator()))
	sb.WriteString("\n")

	// Stats bar / 状态条
	// Spinner glyph comes from spinnerFrames[frameIdx], so it
	// actually rotates at 10fps (see tickMsg in Update). The
	// previous static ◐ looked identical between stats updates
	// and read as "is anything happening?".
	// Spinner 字形来自 spinnerFrames[frameIdx]，10fps 真转
	// （见 Update 的 tickMsg）。之前的静态 ◐ 在两次 stats 之
	// 间看着一样，读起来像"有在动吗？"。
	spinner := spinnerFrames[m.frameIdx%len(spinnerFrames):][:1]
	if m.runState == runDone {
		// When done, replace the spinner with a check so the
		// "finished" state is unambiguous.
		// 完成后用对勾替换 spinner，"完成"状态更明确。
		spinner = symDone
	}
	stats := fmt.Sprintf(
		"%s alive=%d  ports=%d  results=%d  creds=%d  errors=%d   elapsed=%s",
		spinner, m.counters.Alive, m.counters.Ports, m.counters.Results,
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
	sb.WriteString(stats)
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

// titleSeparator returns a single dim row of `─` characters
// sized to the current terminal width. Falls back to 80 on
// 0-width (start-up race) so the very first render still has
// a coherent header line.
// titleSeparator 返回一行 dim 色的 `─`，宽与终端同。0 宽时（启
// 动竞态）回退 80，让首帧也有连贯的 header 行。
func (m Model) titleSeparator() string {
	w := m.width
	if w <= 0 {
		w = 80
	}
	return strings.Repeat(boxH, w)
}

// runStateChip returns the right-edge status chip + text for the
// title bar. Mapping:
//
//	runIdle     → " IDLE "     violet  (waiting, no data yet)
//	runScanning → " SCANNING " cyan    (live, pipeline active)
//	runDone     → " DONE "     green   (scan complete, lingering)
//
// runStateChip 返回标题栏右侧状态芯片 + 文本。
//
//	runIdle     → " IDLE "     紫     （等待，暂无数据）
//	runScanning → " SCANNING " 青     （实时，pipeline 活跃）
//	runDone     → " DONE "     绿     （扫描完成，linger 中）
func (m Model) runStateChip() string {
	switch m.runState {
	case runScanning:
		return stRunning.Render(" SCANNING ")
	case runDone:
		return stFinished.Render(" DONE ")
	default:
		return stIdle.Render(" IDLE ")
	}
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
	// The header now carries a progress hint (alive/total) on
	// wide terminals and the trailing count badge; on narrow
	// stacks we drop the hint to save the line. The header is
	// rendered as a single line so the panel's "TARGETS" identity
	// stays stable across row updates.
	// 标题现在带进度提示（alive/总数）和计数尾标；窄终端堆叠
	// 时省掉提示以省行。标题整成一行，让面板"TARGETS"身份在
	// 行更新时保持稳定。
	headerText := "TARGETS"
	if m.twoColumn() {
		// A small secondary counter that gives the panel more
		// "instrument" feel without adding rows. We use the
		// same accent so the eye reads the header + tail as a
		// single unit, not as two competing decorations.
		// 给面板加个二级计数，提升"仪表"感而不加行。用同色
		// accent 让眼睛把头 + 尾读作一个整体，而非两个互相争
		// 抢的装饰。
		tail := stMuted.Render(fmt.Sprintf("· %d metrics", len(rows)))
		headerText = headerText + "  " + tail
	}
	var body strings.Builder
	body.WriteString(stPanelHeader.Render(headerText))
	body.WriteString("\n")
	for _, r := range rows {
		// Layout: "  alive      2"  with the dot separator
		// marking the label→number transition. The dot reads
		// as a soft divider (vs. plain whitespace) and ties
		// the rows together visually — without it the panel
		// reads as 5 disjointed pairs.
		// 布局："  alive      · 2"，点号作为 label→number 的软
		// 分隔。点号比纯空白更有"分隔"感，把 5 行视觉串起来——
		// 没用点号前面板读作 5 个互不相关的对。
		label := stDim.Render(fmt.Sprintf("  %-10s", r[0]))
		body.WriteString(label)
		body.WriteString(stMuted.Render(symDot + " "))
		// Counter coloring: cyan by default (anchors the
		// panel), amber for cred hits (the operator's primary
		// signal), red for non-zero errors. Zero errors stay
		// cyan so a healthy run doesn't shout.
		// 计数器配色：默认 cyan（锚定面板），凭据命中琥珀（操
		// 作员主信号），错误非零时红色。零错误保持 cyan，健康
		// 运行不喊话。
		var numStyle lipgloss.Style = stStatNum
		switch r[0] {
		case "creds":
			if m.counters.Creds > 0 {
				numStyle = stWarn
			}
		case "errors":
			if m.counters.Errors > 0 {
				numStyle = stError
			}
		}
		body.WriteString(numStyle.Render(r[1]))
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
