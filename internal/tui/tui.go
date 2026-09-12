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
//
// v0.7.0: Model struct definition moved to model.go (alongside the
// v0.7.0 ring-buffer / flash / spinner fields it owns). Methods
// below reference the type by name within the same package.
// v0.7.0：Model 结构体定义搬到了 model.go（与它拥有的 v0.7.0
// ring buffer / flash / spinner 字段同处）。下面方法按类型名在
// 同包内引用。

// NewModel constructs a fresh dashboard model.
// NewModel 构造一个新的 dashboard model。
//
// v0.5.2: NewModel takes an optional state for the info-density
// panels (top plugins / error categories). Existing callers that
// pass nil keep working — the panels then render "(no hits yet)"
// / "(no errors yet)" placeholders. The signature change is
// backward-compatible at call sites that pass nil; non-nil is the
// production path wired from runScan.
//
// v0.5.2：NewModel 接收可选的 state 给信息密度面板（top 插件 /
// 错误分类）。传 nil 的现有调用方仍能工作——面板渲染"(no hits
// yet)" / "(no errors yet)"占位符。签名变更在传 nil 的调用点向后
// 兼容；非 nil 是从 runScan 接线的生产路径。
func NewModel(cfg *types.Config) Model {
	return newModelWithState(cfg, nil)
}

// newModelWithState is the full constructor used by NewModel and
// by tests that need a State wired in. Returns a value (not a
// pointer) for parity with the bubbletea Model contract; the
// runtime mutates fields via pointer receivers in Update /
// pushEvent.
//
// newModelWithState 是 NewModel 和需要接入 State 的测试共用的完
// 整构造函数。返回值（而非指针）以匹配 bubbletea Model 契约；
// runtime 在 Update / pushEvent 中通过指针接收者变更字段。
func newModelWithState(cfg *types.Config, st *types.State) Model {
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
		state:   st,
	}
}

// Init kicks off the spinner tick plus the v0.7.0 flash-decay and
// rate-sample ticks, batched. Each tick self-perpetuates via the
// cmd its Update case returns, so all three keep running until the
// program quits. bubbletea handles the timing — the model doesn't
// poll.
//
// Init 启动 spinner tick 外加 v0.7.0 的 flash 衰减与速率采样
// tick（批量）。每个 tick 由其 Update 分支返回的 cmd 自我延续，
// 三者一直跑到 program 退出。bubbletea 处理时序——model 不轮询。
func (m Model) Init() tea.Cmd {
	return tea.Batch(tickCmd(), flashTick(), rateTick())
}

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

// flashTickMsg drives flash-expiry pruning every 100ms — the same
// cadence as the spinner, so a 200ms flash paints red for exactly
// two render frames. / flashTickMsg 每 100ms 驱动一次 flash 过期
// 剪枝——与 spinner 同节拍，200ms 的 flash 恰好画红两帧。
type flashTickMsg time.Time

// rateTickMsg samples the hits/sec ring buffer every 1s, feeding
// the header sparkline. / rateTickMsg 每 1s 采样一次 hits/sec ring
// buffer，供 header sparkline 用。
type rateTickMsg time.Time

// flashTick re-arms the flash-decay tick. / flashTick 重新武装
// flash 衰减 tick。
func flashTick() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {
		return flashTickMsg(t)
	})
}

// rateTick re-arms the rate-sampling tick. / rateTick 重新武装
// 速率采样 tick。
func rateTick() tea.Cmd {
	return tea.Tick(1*time.Second, func(t time.Time) tea.Msg {
		return rateTickMsg(t)
	})
}

// Update handles bubbletea messages (keypresses, window resize, etc.).
// Update 处理 bubbletea 消息（按键、窗口大小变化等）。
//
// v0.5.2: changed to a pointer receiver so tests can read mutated
// fields directly (rate / topN / ETA) without the dispatcher
// return-value plumbing. The bubbletea runtime passes a pointer
// here regardless of the receiver kind (it does so internally via
// type-assertion), so this is a no-op for production callers.
//
// v0.5.2：改成指针接收者，让测试能直接读到变更后的字段（rate /
// topN / ETA），免去 dispatcher 返回值接线的麻烦。bubbletea 运行
// 时无论接收者类型都传指针（内部通过类型断言），所以对生产调用方
// 是 no-op。
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
	case flashTickMsg:
		// Prune expired flashes so the map doesn't grow unbounded
		// on scans hitting many distinct hosts, then re-arm.
		// 剪掉过期 flash，避免命中大量不同 host 的扫描让 map 无限
		// 增长，然后重新武装。
		m.pruneExpiredFlashes(time.Time(msg))
		cmd = flashTick()
	case rateTickMsg:
		// Sample the current smoothed hits/sec into the sparkline
		// ring, then re-arm. / 把当前平滑 hits/sec 采进 sparkline
		// ring，然后重新武装。
		m.recordRate(m.rateHits)
		cmd = rateTick()
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
		case "e":
			// Toggle the errors panel collapse/expand. / 切换错误
			// 面板折叠/展开。
			m.errorsExpanded = !m.errorsExpanded
		case "E":
			// Collapse the errors panel and keep it collapsed.
			// / 折叠错误面板并保持折叠。
			m.clearErrors()
		case "L":
			// Toggle the narrow-mode live-events overlay. / 切换
			// narrow 模式的实时事件 overlay。
			m.showLiveOverlay = !m.showLiveOverlay
		}
	}
	return m, cmd
}

// View renders the dashboard. Returns a single string that lipgloss
// will then lay out.
// View 渲染 dashboard。返回 lipgloss 将布局的单个字符串。
//
// v0.7.0 (Spec B): the body is a 6-region composition — header,
// live events, stage, top plugins, errors, footer — placed by
// regions() per breakpoint. Wide terminals put STAGE + TOP PLUGINS
// side-by-side; medium/narrow stack them. The title bar, help
// overlay, pause chip, quit/summary paths and the height fill are
// chrome kept verbatim from v0.5.2.
//
// v0.7.0（Spec B）：主体是 6 区域组合——header、实时事件、stage、
// top plugins、errors、footer——由 regions() 按断点放置。宽终端
// STAGE + TOP PLUGINS 并排；medium/narrow 堆叠。标题栏、帮助浮层、
// 暂停芯片、退出/摘要路径和高度填充是 v0.5.2 原样保留的 chrome。
func (m Model) View() string {
	if m.quitting {
		return m.finalSummary + "\n"
	}
	if m.uiMode == modeHelp {
		return m.renderHelp()
	}
	var sb strings.Builder

	// Title bar — plain text + thin dim separator, kept verbatim
	// from v0.5.2 (see the git history for the three-box rationale).
	// 标题栏——纯文本 + 细 dim 分割线，v0.5.2 原样保留（三层框的
	// 取舍见 git 历史）。
	titleChip := m.runStateChip()
	title := fmt.Sprintf(
		" FG-QIMEN %s  project: %s   mode: %s   %s",
		version.Value, m.project, m.mode, titleChip,
	)
	sb.WriteString(stTitle.Render(title))
	sb.WriteString("\n")
	sb.WriteString(stDim.Render(m.titleSeparator()))
	sb.WriteString("\n")

	// ── v0.7.0 six-region body (Spec B) ──
	// v0.7.0 六区域主体（Spec B）
	bp := pickBreakpoint(m.width)
	h, ev, l, r, e, f := regions(bp, m.width, m.height)
	// Expanded errors need 4 rows (1 header-equivalent + up to 4 bars);
	// regions() doesn't know the toggle state, so widen the budget here.
	// The measured fixed-accounting below picks up the real height and
	// re-clamps events, so a 24-row terminal just shows fewer events.
	// / 展开态 errors 需要 4 行；regions() 不知道开关状态，在这里
	// 加宽预算。下面的实测 fixed 记账会取真实高度并重新钳 events，
	// 24 行终端只是少显示几条事件。
	if m.errorsExpanded {
		e = 4
	}

	// Render the fixed regions first and MEASURE them, then clamp the
	// events budget to whatever height is actually left. regions() is
	// a static guess; this is the ground truth, so the composed frame
	// never exceeds the terminal (the probe showed the old order
	// overflowing 80×24 by 3 rows once events filled).
	// 先渲染固定区域并"实测"高度，再把 events 预算钳到实际剩余。
	// regions() 是静态预估；这里才是真实值，保证整帧不超终端
	// （探针显示旧顺序在 events 填满时 80×24 会溢出 3 行）。
	header := m.viewHeader(h, bp)
	stage := m.viewStage(l, bp)
	topPlugins := m.viewTopPlugins(r, bp)
	errorsPanel := m.viewErrors(e)
	footer := m.viewFooter(f)

	fixed := 2 + lipgloss.Height(header) + lipgloss.Height(errorsPanel) +
		lipgloss.Height(footer)
	if m.uiMode == modePaused {
		fixed++ // [PAUSED] chip / 暂停芯片
	}
	if bp == BreakWide {
		fixed += max(lipgloss.Height(stage), lipgloss.Height(topPlugins))
	} else {
		fixed += lipgloss.Height(stage) + lipgloss.Height(topPlugins)
	}
	// Narrow 'L' overlay default (5 rows) is applied inside
	// viewLiveEvents when ev==0; pre-request it here so the clamp
	// below can shave it. / narrow 的 'L' overlay 默认 5 行由
	// viewLiveEvents 在 ev==0 时套用；这里先预申请，让下面的钳制
	// 能削它。
	if bp == BreakNarrow && m.showLiveOverlay {
		ev = 5
	}
	if rem := m.height - fixed; rem < ev {
		ev = rem
	}
	if ev < 0 {
		ev = 0
	}
	events := m.viewLiveEvents(ev, bp)

	// Pause chip rides directly under the header so the operator
	// can tell at a glance the dashboard is frozen (the pipeline
	// keeps running). / 暂停芯片紧贴 header 下方，操作员一眼看出
	// dashboard 已冻结（pipeline 仍在跑）。
	parts := []string{header}
	if m.uiMode == modePaused {
		parts = append(parts, "  "+stWarn.Render("[PAUSED]"))
	}
	if bp == BreakWide {
		// Side-by-side: STAGE | TOP PLUGINS. / 并排：STAGE | TOP PLUGINS。
		body := lipgloss.JoinHorizontal(lipgloss.Top, stage, topPlugins)
		parts = append(parts, events, body, errorsPanel, footer)
	} else {
		// Stacked; empty regions (e.g. narrow hides events) are
		// skipped so no stray blank lines appear. / 堆叠；空区域
		// （如 narrow 隐藏 events）跳过，避免多余空行。
		for _, region := range []string{events, stage, topPlugins, errorsPanel, footer} {
			if region != "" {
				parts = append(parts, region)
			}
		}
	}
	sb.WriteString(lipgloss.JoinVertical(lipgloss.Left, parts...))
	sb.WriteString("\n")

	// Height reconciliation: pad short frames (ghost-content guard)
	// and hard-truncate overframes. The measured events clamp keeps
	// overframes impossible above ~16 rows; the truncate is the
	// last-resort for absurdly small terminals (where losing the
	// footer beats scrolling the frame).
	//
	// CRITICAL: bubbletea's standard renderer counts frame lines as
	// strings.Split(view, "\n") and drops the TOP lines when the
	// count exceeds the terminal height (standard_renderer.go:186).
	// A trailing "\n" therefore costs one real top row per frame —
	// the live smoke probe caught the title bar (and the runState
	// chip on it) vanishing from every frame. The reconciled frame
	// must have EXACTLY m.height split elements: join content
	// without a trailing newline, then pad with bare "\n"s whose
	// split artifacts are the pad rows.
	//
	// 高度对账：短帧补行（防残影），超帧硬裁。实测 events 钳制使
	// ~16 行以上的终端不可能超帧；裁剪是极小终端的兜底（那种情况
	// 下丢 footer 好过整帧滚动）。
	//
	// 关键：bubbletea 标准 renderer 用 strings.Split(view, "\n") 数
	// 帧行数，超出终端高度时丢弃**顶部**行（standard_renderer.go:186）。
	// 尾随 "\n" 因此每帧吃掉一行真实顶行——实机冒烟探针抓到标题栏
	// （连同其上的 runState 芯片）每帧消失。对账后的帧必须恰好有
	// m.height 个 split 元素：内容 Join 不带尾随换行，再用裸 "\n"
	// 补行——其 split 产物就是补的空行。
	if m.height > 0 {
		frame := strings.TrimRight(sb.String(), "\n")
		lines := strings.Split(frame, "\n")
		if len(lines) > m.height {
			lines = lines[:m.height]
		}
		sb.Reset()
		sb.WriteString(strings.Join(lines, "\n"))
		for i := len(lines); i < m.height; i++ {
			sb.WriteString("\n")
		}
	}

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
		{"e", "toggle the errors panel (collapsed summary / expanded bars)"},
		{"E", "collapse the errors panel"},
		{"L", "toggle the live-events overlay (narrow mode)"},
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

// twoColumn reports whether the current width supports the
// two-column layout. minWidth is the floor; below it we stack
// to avoid horizontal overflow on 80×24 terminals.
//
// twoColumn 报告当前宽度是否支持两栏布局。minWidth 是下限；低于
// 此值时堆叠以避免 80×24 终端横向溢出。
func (m Model) twoColumn() bool { return m.width >= minWidth }

// totalHosts returns the State-cached total host count, or 0 when
// the State is nil. Used by the rate row's "probed N / M" display.
// totalHosts 返回 State 缓存的总主机数；State 为 nil 时返回 0。
// 给速率行 "probed N / M" 显示用。
func (m Model) totalHosts() int64 {
	if m.state == nil {
		return 0
	}
	return m.state.TotalHosts.Load()
}

// renderTopPluginsPanel builds the right "TOP PLUGINS" panel —
// the top-5 hit-count bars. Renders "(no hits yet)" placeholder
// when m.topPlugins is empty. Rows are joined without trailing
// newlines and the header uses the flush style: a margin or a
// trailing "\n" would inject stray blank lines into the
// JoinVertical composition (visible as ragged gaps in the probe).
//
// renderTopPluginsPanel 构建右侧 "TOP PLUGINS" 面板——top-5
// 命中柱状图。m.topPlugins 为空时渲染 "(no hits yet)" 占位符。
// 行拼接不带结尾换行、标题用 flush 样式：边距或结尾 "\n" 会往
// JoinVertical 组合里注入多余空行（探针里表现为参差空隙）。
func (m Model) renderTopPluginsPanel(width int) string {
	var body strings.Builder
	body.WriteString("  ")
	body.WriteString(stPanelHeaderFlush.Render("TOP PLUGINS"))
	if len(m.topPlugins) == 0 {
		body.WriteString("\n  (no hits yet)")
	} else {
		for _, p := range m.topPlugins {
			// Count → 12-char bar → name. The bar length is fixed
			// at 12 chars so the panel reads as a column even when
			// counts span 1 → 9999. We use a static bar (0.5 fill)
			// rather than a count-proportional one because the
			// proportional version makes a hit count of 1 look
			// indistinguishable from a glitch.
			// 计数 → 12 字符 bar → 名称。bar 长度固定 12 字符，
			// 让面板读作一列，即使计数跨 1 → 9999。用静态 bar
			// （0.5 填充）而非按计数比例，因为按比例的话 1 命中
			// 看起来跟"故障"没区别。
			fmt.Fprintf(&body, "\n  %-10s %s  %s",
				p[1], bar(0.5, 12), p[0])
		}
	}
	if width >= 100 {
		return stBox.Width(statsColWidth).Render(body.String())
	}
	return body.String()
}
