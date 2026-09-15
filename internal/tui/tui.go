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
// Layout model (spec §5, TUI v3 lattice):
//   - pickBreakpoint maps width to narrow (<80) / medium (80–119) /
//     wide (≥120); regionsV2 computes per-region row budgets with the
//     EVENTS ≥3 floor and the documented contraction order.
//   - View() composes a single-layer shared-border lattice frame
//     (frame.go) sized to exactly m.height rows; height reconciliation
//     below stays as the last-resort guard.
//
// 布局模型（spec §5，TUI v3 lattice）：pickBreakpoint 把宽度映射为
// narrow（<80）/ medium（80–119）/ wide（≥120）；regionsV2 计算各区
// 域行预算（EVENTS 保底 3、约定收缩序）。View() 用 frame.go 组合单
// 层共享边框 lattice 帧，恰好 m.height 行；高度对账保留为兜底守卫。
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

// LingerBudget is the wall-clock window the TUI needs to run its
// full doneMsg → linger → self-quit chain (lingerTicks × spinnerTick
// ≈ 1.5s, plus margin). External quitters — notably the cmd-layer
// cleanup watcher — MUST NOT force-quit inside this window, or the
// DONE chip and final summary get cut off (the race observed in the
// /24 live smoke test). Force-quit is only legitimate after this
// deadline, as a backstop for paths where Done() never fired.
//
// LingerBudget 是 TUI 跑完 doneMsg → linger → 自退全链路所需的墙
// 钟窗口（lingerTicks × spinnerTick ≈ 1.5s，加余量）。外部退出者
// ——特别是 cmd 层的 cleanup watcher——不得在该窗口内强制退出，
// 否则 DONE 芯片与最终摘要会被截断（/24 实机冒烟测试观察到的竞
// 态）。只有超过该期限后才允许强制退出，作为 Done() 从未触发的
// 路径的兜底。
const LingerBudget = 3 * time.Second

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
		// Column hysteresis step (spec §5.4): the tick is the
		// frame beat, so the grow-immediate / shrink-after-20
		// law lives here — all mutations stay in this goroutine.
		// / 列宽滞回步进（spec §5.4）：tick 就是帧拍，增长立即/
		// 收缩 20 帧的律法落在这里——所有突变都留在本 goroutine。
		m.hostW, m.hostStreak = stepHostWidth(m.curHostW(), m.hostNeed(), m.hostStreak)
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
			// Pause: freeze the *viewport* (spec §4.1 paused 语义升
			// 级) — snapshot the whole frame so nothing re-renders,
			// while the hub keeps collecting. The event frontier
			// (last ID + ingested counter) is captured for the exact
			// hidden-count on resume.
			// / 暂停：冻结*视口*（spec §4.1 paused 语义升级）——拍下
			// 整帧不再重渲染，hub 继续收集。事件前沿（最后 ID +
			// ingested 计数）在此捕获，供恢复时算精确隐藏数。
			if m.uiMode == modeRun {
				m.frozenView = m.View()
				m.pauseAnchorID = m.nextEventID
				m.pauseIngest0 = m.ingested
				m.uiMode = modePaused
			}
		case "r":
			// Resume: unfreeze and mark the hidden gap. N = ingested
			// delta over the pause (snapshot-exact, not an estimate);
			// the separator renders right after the pre-pause anchor
			// and self-cleans when that event leaves the ring.
			// / 恢复：解冻并标记隐藏缺口。N = 暂停期间 ingested 差值
			// （快照精确值非估计）；分隔行渲染在暂停前锚事件之后，该
			// 事件离开 ring 时自清理。
			if m.uiMode == modePaused {
				m.uiMode = modeRun
				m.frozenView = ""
				if n := m.ingested - m.pauseIngest0; n > 0 {
					m.gapAfterID = m.pauseAnchorID
					m.gapCount = n
				}
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
		case "up", "k":
			m.scrollUp()
		case "down", "j":
			m.scrollDown()
		case "pgup":
			m.scrollPage(true, m.eventsPage())
		case "pgdown":
			m.scrollPage(false, m.eventsPage())
		case "g":
			m.scrollTop()
		case "G", "f":
			m.follow()
		case "enter":
			m.toggleExpand()
		case "esc":
			// Esc outside help returns to follow (spec §4.2); help
			// mode's esc is consumed by the overlay branch above.
			// / help 之外的 Esc 回到 follow（spec §4.2）；help 态的
			// esc 已被上面的浮层分支消费。
			m.follow()
		}
	}
	return m, cmd
}

// View renders the dashboard as a lattice frame (spec §5.1/§5.2):
// one-layer shared-border grid with the title embedded in the top
// border. Region budgets come from regionsV2; every cell is padded to
// its budget so the frame has exactly one row per terminal line (the
// height reconciliation below stays as the last-resort guard).
//
// Wide (≥120) puts PROGRESS + TOP PLUGINS in the left column and LIVE
// EVENTS in the right column, joined by a shared border with ┬/┴
// junctions. Medium/narrow stack full-width cells; the footer sits
// outside the frame. PAUSED renders as an extra chip in the title
// border (zero budget impact).
//
// / View 把 dashboard 渲染为 lattice 帧（spec §5.1/§5.2）：单层共享
// 边框网格，标题嵌在顶边框里。区域预算来自 regionsV2；每个格都补
// 齐到预算，帧的行数恰好等于终端行数（下面的高度对账保留为兜底守
// 卫）。
//
// 宽屏（≥120）左列为 PROGRESS + TOP PLUGINS，右列为 LIVE EVENTS，
// 中缝共享边框用 ┬/┴ 三通衔接。medium/narrow 全宽堆叠，footer 在框
// 外。PAUSED 作为附加芯片渲染在标题边框里（零预算影响）。
func (m Model) View() string {
	if m.quitting {
		return m.finalSummary + "\n"
	}
	if m.uiMode == modeHelp {
		return m.renderHelp()
	}
	// Paused freezes the whole frame (spec §4.1): the snapshot taken
	// at pause entry renders unchanged no matter what data lands.
	// Empty fallback renders live — golden/test models are built
	// directly without a pause transition.
	// / Paused 冻结整帧（spec §4.1）：暂停入口拍的快照原样渲染，无
	// 视 arriving 数据。为空时回退实时渲染——golden/测试 model 是直
	// 接构造的，没有暂停转换。
	if m.uiMode == modePaused && m.frozenView != "" {
		return m.frozenView
	}

	w := m.width
	if w <= 0 {
		w = 80
	}
	// Height fallback for the 0-size start-up race: without it
	// regionsV2 contracts to the tiny-terminal floor and drops whole
	// cells (TOP PLUGINS, LIVE EVENTS) on the first frames.
	// / 高度为 0 的启动竞态回退：否则 regionsV2 收缩到极小终端下限，
	// 首帧就丢掉整格（TOP PLUGINS、LIVE EVENTS）。
	h := m.height
	if h <= 0 {
		h = 24
	}
	bp := pickBreakpoint(w)
	b := regionsV2(bp, h, m.errorsExpanded)

	// Narrow 'L' overlay: 5 event rows, paid for by PROGRESS (min 2
	// so the cell keeps its title + one stat row).
	// / narrow 的 'L' overlay：5 行事件，由 PROGRESS 支付（保底 2，
	// 让格保留标题 + 一行统计）。
	if bp == BreakNarrow && m.showLiveOverlay {
		b.events = 5
		b.progress -= 6
		if b.progress < 2 {
			b.progress = 2
		}
	}

	// Title border: prefix + status chip (+ PAUSED chip).
	// / 标题边框：前缀 + 状态芯片（+ PAUSED 芯片）。
	chip := m.runStateChip()
	if m.uiMode == modePaused {
		chip += " " + stWarn.Render("[PAUSED]")
	}
	title := fmt.Sprintf("FG-QIMEN %s ─ project: %s ─ mode: %s",
		version.Value, m.project, m.mode)

	cellW := w - 2
	lines := []string{titleRow(w, title, chip)}

	// Header cell. / header 格。
	lines = append(lines, borderedRows(cellRows(m.viewHeader(b.header, cellW), b.header, cellW))...)
	lines = append(lines, stFrame.Render(hBorder(w, boxLS, boxRS, "", 0)))

	switch bp {
	case BreakWide:
		leftW, rightW := wideSplit(w)
		lines = append(lines, stFrame.Render(hBorder(w, boxLS, boxRS, boxDn, leftW+1)))

		// Left column: PROGRESS + blank separator + TOP PLUGINS; the
		// compose loop pads (or caps) to the body height. Rows past the
		// left content pad as blank so the shared border has no gaps.
		// / 左列：PROGRESS + 空行分隔 + TOP PLUGINS；组合循环补齐
		// （或裁掉）到 body 高度。左列内容耗尽的行补空白，共享边框无
		// 断口。
		left := m.viewStage(b.progress, leftW)
		if b.plugins > 0 {
			left += "\n\n" + m.viewTopPlugins(b.plugins, leftW)
		}
		leftRows := strings.Split(left, "\n")
		rightRows := cellRows(m.viewLiveEvents(b.events, rightW), b.events, rightW)
		blank := strings.Repeat(" ", leftW)
		for i := 0; i < b.events; i++ {
			l := blank
			if i < len(leftRows) {
				l = padTo(leftRows[i], leftW)
			}
			lines = append(lines, rowTwo(l, rightRows[i]))
		}
		lines = append(lines, stFrame.Render(hBorder(w, boxLS, boxRS, boxUp, leftW+1)))
		lines = append(lines, borderedRows(cellRows(m.viewErrors(b.errors, cellW), b.errors, cellW))...)
		lines = append(lines, stFrame.Render(hBorder(w, boxLS, boxRS, "", 0)))
		lines = append(lines, rowCell(padTo(m.viewFooter(b.footer, cellW), cellW)))
		lines = append(lines, stFrame.Render(hBorder(w, boxBL, boxBR, "", 0)))

	default: // medium / narrow: stacked full-width cells, footer outside
		if b.progress > 0 {
			lines = append(lines, borderedRows(cellRows(m.viewStage(b.progress, cellW), b.progress, cellW))...)
			lines = append(lines, stFrame.Render(hBorder(w, boxLS, boxRS, "", 0)))
		}
		if b.events > 0 {
			lines = append(lines, borderedRows(cellRows(m.viewLiveEvents(b.events, cellW), b.events, cellW))...)
			lines = append(lines, stFrame.Render(hBorder(w, boxLS, boxRS, "", 0)))
		}
		if b.plugins > 0 {
			lines = append(lines, borderedRows(cellRows(m.viewTopPlugins(b.plugins, cellW), b.plugins, cellW))...)
			lines = append(lines, stFrame.Render(hBorder(w, boxLS, boxRS, "", 0)))
		}
		lines = append(lines, borderedRows(cellRows(m.viewErrors(b.errors, cellW), b.errors, cellW))...)
		lines = append(lines, stFrame.Render(hBorder(w, boxBL, boxBR, "", 0)))
		// Footer rides outside the frame on medium/narrow.
		// / medium/narrow 上 footer 在框外。
		lines = append(lines, m.viewFooter(b.footer, cellW))
	}

	// Height reconciliation (unchanged contract, last-resort guard):
	// bubbletea's standard renderer counts frame lines as
	// strings.Split(view, "\n") and drops the TOP lines when the
	// count exceeds the terminal height. regionsV2 + cellRows make
	// the composed frame exactly m.height rows; the truncation below
	// only fires on arithmetic drift (and beats scrolling a frame).
	// 高度对账（契约不变，兜底守卫）：bubbletea 标准 renderer 用
	// strings.Split(view, "\n") 数帧行数，超终端高时丢**顶部**行。
	// regionsV2 + cellRows 已让帧恰好 m.height 行；下面的截断只在
	// 计算漂移时触发（丢行好过整帧滚动）。
	if m.height > 0 {
		for len(lines) > m.height {
			lines = lines[:len(lines)-1]
		}
		for len(lines) < m.height {
			lines = append(lines, "")
		}
	}
	return strings.Join(lines, "\n")
}

// eventsPage is the PgUp/PgDn page size in events: the EVENTS region
// budget minus the title row, clamped ≥1.
// / eventsPage 是 PgUp/PgDn 的页大小（事件数）：EVENTS 区域预算减标
// 题行，钳到 ≥1。
func (m Model) eventsPage() int {
	w := m.width
	if w <= 0 {
		w = 80
	}
	h := m.height
	if h <= 0 {
		h = 24
	}
	b := regionsV2(pickBreakpoint(w), h, m.errorsExpanded)
	p := b.events - 1
	if p < 1 {
		p = 1
	}
	return p
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
		return stIdleChip.Render(" IDLE ")
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
		{"↑/k · ↓/j", "scroll the events panel (enters browse)"},
		{"PgUp / PgDn", "page the events panel"},
		{"g / G", "events panel top / bottom (G = follow)"},
		{"f / Esc", "events panel back to follow"},
		{"Enter", "expand a ×N folded event row (browse)"},
		{"e", "toggle the errors panel (collapsed summary / expanded bars)"},
		{"E", "collapse the errors panel"},
		{"L", "toggle the live-events overlay (narrow mode)"},
		{"?", "toggle this help overlay"},
	}
	// Stable order so the overlay reads the same across renders.
	// 稳定排序，浮层每次读起来一致。
	sort.Slice(rows, func(i, j int) bool { return rows[i].key < rows[j].key })

	// Width law: the overlay must never exceed the terminal. Budget:
	// box 6 (border 2 + h-padding 4) + leading indent 2 + hint (key+2)
	// + gap 2. Truncate the longest key column first, then descs.
	// / 宽度律：浮层绝不超终端。预算：框 6（边框 2 + 横向 padding 4）
	// + 行首缩进 2 + 键位提示（key+2）+ 间隔 2。先按最长键位列算，再
	// 截描述。
	maxKey := 0
	for _, r := range rows {
		if n := lipgloss.Width(r.key); n > maxKey {
			maxKey = n
		}
	}
	descBudget := m.width - (6 + 2 + maxKey + 2) - 2 // hint padding
	if descBudget < 8 {
		descBudget = 8 // keep something readable on absurd terminals
	}

	var sb strings.Builder
	sb.WriteString(stPanelHeader.Render("KEYMAP"))
	sb.WriteString("\n\n")
	for _, r := range rows {
		kb := stKeyHint.Render(" " + r.key + " ")
		sb.WriteString(fmt.Sprintf("  %s  %s\n", kb, truncate(r.desc, descBudget)))
	}
	sb.WriteString("\n")
	sb.WriteString(stMuted.Render("press ? or esc to close"))
	body := stHelp.Render(sb.String())
	if w := m.width; w > 0 {
		return lipgloss.Place(w, m.height, lipgloss.Center, lipgloss.Center, body)
	}
	return body
}

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
