// tui/styles.go — modernised terminal palette and Lipgloss styles.
//
// tui/styles.go — 现代化终端配色与 Lipgloss 样式。
//
// Palette honours the NO_COLOR env var (https://no-color.org/).
// When the operator sets NO_COLOR=1 (or any non-empty non-"0" /
// non-"false" value), the foreground accents collapse to the dim
// gray and the cyberpunk accents go away — the dashboard still
// renders correctly, just monochrome. This is an in-process
// override; we don't reload on env changes mid-scan.
//
// The palette keeps the "geek terminal" identity (matrix green +
// amber cred hits + red errors) but lifts the contrast and tone
// so it reads cleanly on both dark and light backgrounds: greens
// were pushed toward #00E676 (more readable than the original
// neon #00FF41), the title bar gained a subtle gradient feel via
// bold + background, and every panel now has a dedicated header
// style so the right column isn't the only one with a heading.
//
// 调色板尊重 NO_COLOR 环境变量（https://no-color.org/）。当操作员
// 设 NO_COLOR=1（或任何非空、非"0"、非"false"值）时，所有前景色
// 塌缩到 dim gray，赛博朋克口音消失——dashboard 仍正常渲染，只是
// 单色。进程内覆盖；扫描中环境变化不重载。
//
// 调色板保留"极客终端"身份（matrix 绿 + 琥珀凭据命中 + 红色错误），
// 但提升对比度与色温，使明/暗背景下都可读：绿色推向 #00E676（比
// 原霓虹 #00FF41 更易读），标题栏加粗加深色背景，每块面板有专用
// 头部样式，右栏不再是唯一有标题的面板。
package tui

import (
	"os"

	"github.com/charmbracelet/lipgloss"
)

// Color palette / 配色方案
// (kept as plain strings so we can swap them at test time if needed)
// （用纯字符串保存，方便测试时替换）
//
// The palette is intentionally broader than the strict cyberpunk
// "green / amber / red" triad: a small set of accent colors lets
// us give each dashboard region its own visual identity (target
// counters, live events, paused state, status banner) without
// losing the geek-terminal feel. Cyan = live / scanning, magenta
// = informational, yellow = transitional state, violet = idle /
// stopped. All five accents are tuned for AA contrast on the
// slate background.
// 调色板有意比严格的赛博朋克"绿/琥珀/红"三元更宽：一小组口
// 音色让每个 dashboard 区域有独立视觉身份（目标计数、实时事件、
// 暂停、状态条），仍保留极客终端感。cyan = 实时/扫描，magenta
// = 信息，yellow = 过渡态，violet = 空闲/停止。5 个口音都对 slate
// 背景做了 AA 对比度调校。
const (
	colBg     = "#0B0F12" // deep slate (was #000000 — pure black crushed dim text on some terminals)
	colPanel  = "#11181D" // subtle panel fill
	colAccent = "#00E676" // matrix green (geek terminal signature) — slightly cooler than #00FF41 for readability
	colAmber  = "#FFB300" // amber (creds / focus) — bumped from #FFB000 for AA contrast on dark bg
	colRed    = "#FF4D5E" // coral red (errors) — softer than #FF3344 but still clearly an error
	colCyan   = "#22D3EE" // electric cyan (scanning / live state)
	colYellow = "#F4D03F" // warm yellow (transitional / in-progress)
	colViolet = "#A78BFA" // soft violet (idle / stopped / done)
	colDim    = "#5A6470" // slate gray (secondary info) — brighter than #3A3A3A so it survives on dark bg
	colMuted  = "#8A95A1" // light slate (tertiary)
	colBright = "#FFFFFF" // white (rare, emphasis only)
)

// Symbols / 符号
//
// Kept compact and ASCII-leaning where possible so the dashboard
// is readable on terminals that don't ship the heavier Unicode
// glyphs (Windows console hosts, some SSH gateways). The spinner
// cycles through four frames at ~100ms (see tui.go tickMsg) for
// a proper "is it actually doing something?" indicator — a static
// glyph (the old ◐) didn't read as a spinner on a 1Hz update.
// 尽量紧凑并偏 ASCII，让 dashboard 在不带宽 Unicode 字形的终端
// （Windows console、SSH 跳板）也可读。spinner 通过 4 帧约 100ms
// 循环（见 tui.go tickMsg）形成真正"在动吗？"的视觉信号——静态
// ◐ 在 1Hz 更新下读不出 spinner 感。
const (
	spinnerFrames = "◐◓◑◒" // four-frame half-filled circle rotation
	symSpinner    = "◐"    // initial frame; View() picks by frameIdx
	symSuccess    = "▸"    // play / hit
	symError      = "✗"
	symDone       = "✓"
	symWarn       = "⚠"
	symActive     = "▶"
	symDot        = "·"
)

// Box drawing / 边框
const (
	boxH  = "─"
	boxV  = "│"
	boxTL = "┌"
	boxTR = "┐"
	boxBL = "└"
	boxBR = "┘"
)

// Layout / 布局
const (
	// minWidth is the column width below which we collapse to a
	// single-column stack. Most "old laptop" terminals are 80×24;
	// the two-column layout we ship today is 28 (left) + ~50
	// (right with padding) ≈ 80, so 80 is the safe floor.
	// minWidth 是折叠为单列堆叠的列宽阈值。多数老笔记本终端是
	// 80×24；当前两栏布局为 28 + ~50 ≈ 80，故 80 是安全底线。
	minWidth = 80
	// statsColWidth is the fixed width of the left "Targets" panel.
	// Picked so the longest label ("results") + the widest counter
	// (5 digits) fit with one space of padding on each side.
	// statsColWidth 是左侧 "Targets" 面板的固定宽度。取值使最长的
	// 标签（"results"）+ 最宽的计数器（5 位）能在一格 padding 下
	// 容纳。
	statsColWidth = 28
	// eventsColMin is the floor for the right column when the
	// terminal is wider than minWidth. Below this the right column
	// wraps aggressively; above it we let the layout breathe.
	// eventsColMin 是终端宽于 minWidth 时右栏的最小宽度。低于此值
	// 时右栏会激进换行；高于此值则自由伸展。
	eventsColMin = 48
	// chromeLines is the number of rows consumed by the title bar,
	// title separator, stats bar, keymap and surrounding newlines.
	// View() uses this to size the events list to fit the terminal
	// without scrolling. Title (1) + separator (1) + stats (1) +
	// blank (1) + blank after panels (1) + keymap (1) = 6.
	// chromeLines 是标题栏、标题分割线、状态条、按键提示和换行
	// 占用的行数。View() 据此把事件列表裁剪到不超出终端高度。
	// 标题(1) + 分割线(1) + 状态(1) + 空行(1) + 面板后空行(1) +
	// 按键(1) = 6。
	chromeLines = 6
)

// Styles / 样式
//
// All styles are constructed in init() rather than package-level
// var initialisers so the NO_COLOR branch is decided once at
// package load (before NewProgram runs) without needing to thread
// cfg through every callsite.
//
// 所有样式都在 init() 里构造（不是包级 var 初始化器），让 NO_COLOR
// 分支在包加载时一次性决定（NewProgram 运行之前），不用给每个调用
// 点传 cfg。
var (
	// stTitle is the title bar at the top. / stTitle 顶部标题栏。
	stTitle lipgloss.Style
	// stDim is for secondary info (counts, dim text). / stDim 次要信息。
	stDim lipgloss.Style
	// stMuted is for tertiary info (timestamps, hints). / stMuted 三级信息。
	stMuted lipgloss.Style
	// stSuccess is for positive results (HTTP 200, banner, etc.).
	// stSuccess 成功结果。
	stSuccess lipgloss.Style
	// stWarn is for warnings / cred hits. / stWarn 警告/凭据命中。
	stWarn lipgloss.Style
	// stError is for errors. / stError 错误。
	stError lipgloss.Style
	// stBox wraps a panel with a single-line border. / stBox 面板边框。
	stBox lipgloss.Style
	// stPanelHeader is the row at the top of a panel (e.g. "TARGETS",
	// "LIVE EVENTS"). Lifted from inside the box to a dedicated
	// style so it can be re-styled independently of the border.
	// stPanelHeader 是面板顶部行（如 "TARGETS"、"LIVE EVENTS"）。从
	// 框内抽出为独立样式，便于独立调整。
	stPanelHeader lipgloss.Style
	// stKeyHint renders a key glyph (e.g. "q", "p") as a small
	// pill, so the bottom keymap reads as [q] quit rather than
	// [q] quit. Uses the accent color to draw the eye.
	// stKeyHint 把按键字形（如 "q"、"p"）渲染成小药丸，让底部的按
	// 键提示读起来是 [q] quit 而不是 [q] quit。用 accent 色吸引视
	// 线。
	stKeyHint lipgloss.Style
	// stCounter is for the right-aligned numeric counters in the
	// stats panel — slightly bolder + brighter than the labels.
	// stCounter 是统计面板里右对齐数字计数器——比标签略粗略亮。
	stCounter lipgloss.Style
	// stHelp is the help overlay body (rendered when '?' is pressed).
	// stHelp 是帮助浮层（按 '?' 时渲染）。
	stHelp lipgloss.Style
	// stRunning styles the "SCANNING" chip in the stats bar when
	// the pipeline is active. Cyan signals "live"; the bold makes
	// it pop above the dim surrounding text.
	// stRunning 是 pipeline 激活时状态条上"SCANNING"芯片的样式。
	// cyan 表达"实时"；加粗让它在周边 dim 文本中跳出。
	stRunning lipgloss.Style
	// stIdle styles the "IDLE" chip before the first stats push
	// arrives. Violet reads as "waiting" without looking broken
	// (red would imply error, amber would imply warning).
	// stIdle 是首条 stats 到达前"IDLE"芯片的样式。violet 读作
	// "等待"，不显得出错（红=错误，琥珀=警告）。
	stIdle lipgloss.Style
	// stFinished styles the "DONE" chip + final summary header.
	// Green signals success; we reuse the accent so the eye reads
	// "scan complete" as a positive close to the run.
	// stFinished 是"DONE"芯片 + 最终摘要头部的样式。绿色表达成
	// 功；复用 accent 让眼睛把"扫描完成"读作运行的正向收尾。
	stFinished lipgloss.Style
	// stStatNum is for the right-aligned numeric counters — now
	// in the cyan accent so the numbers visually anchor the
	// stats panel even when there are no events yet. The label
	// stays dim; only the digits get the accent treatment.
	// stStatNum 是右对齐数字计数器——现用 cyan 口音让数字在无
	// 事件时也视觉锚定统计面板。标签仍 dim，只数字上口音。
	stStatNum lipgloss.Style
)

func init() {
	// accent picks the foreground color used for headings, success,
	// and box borders. In NO_COLOR mode it collapses to the dim
	// gray so everything still renders (just monochrome).
	//
	// accent 是标题/成功/边框的前景色。NO_COLOR 模式下塌缩到
	// dim gray，渲染仍正常（只是单色）。
	accent := colAccent
	dimFg := colDim
	mutedFg := colMuted
	if isNoColor() {
		accent = colDim
		dimFg = colDim
		mutedFg = colDim
	}

	// Title bar: no background, just bold + accent foreground. An
	// earlier version painted the full row with the accent bg
	// (lipgloss.Background) which, combined with Padding(0,1) and
	// the stats bar / panel borders below, produced a "box
	// inside a box inside a box" visual stack — operators
	// read it as redundant chrome, not as a header. Foreground-
	// only keeps the bar's identity without claiming a slab of
	// the screen.
	// 标题栏：纯粗体 + accent 前景，零背景。早一版用 accent 背景
	// 涂满整行（lipgloss.Background），叠加 Padding(0,1) 和下面
	// 状态条 / 面板边框后形成"框套框套框"——操作员读作冗余 chrome
	// 而非标题。纯前景保留身份感，又不抢整行。
	stTitle = lipgloss.NewStyle().
		Foreground(lipgloss.Color(accent)).
		Bold(true)

	stDim = lipgloss.NewStyle().
		Foreground(lipgloss.Color(dimFg))

	stMuted = lipgloss.NewStyle().
		Foreground(lipgloss.Color(mutedFg))

	stSuccess = lipgloss.NewStyle().
		Foreground(lipgloss.Color(accent))

	stWarn = lipgloss.NewStyle().
		Foreground(lipgloss.Color(colAmber)).
		Bold(true)

	stError = lipgloss.NewStyle().
		Foreground(lipgloss.Color(colRed))

	// Panel box: dim border so the box reads as "container"
	// without competing with the accent panel header. The
	// previous accent border + accent header piled two accent
	// strokes on top of each other and looked like a
	// double-boxed region.
	// 面板框：dim 边框让它读作"容器"，不与 accent 面板头争抢。
	// 之前的 accent 边框 + accent 头叠了两道 accent 笔画，看
	// 起来像双重框。
	stBox = lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color(dimFg)).
		Padding(0, 1)

	// Panel header: accent + bold + a 1-row gap below. The gap
	// gives the header room to breathe above the content row
	// (the previous MarginBottom(0) butted text against the
	// header and read as crowded on 80×24 terminals).
	// 面板头：accent + 粗体 + 下方 1 行空隙。空隙让标题在内容
	// 行之上有呼吸空间（之前 MarginBottom(0) 把文字紧贴头部，
	// 在 80×24 终端上读起来拥挤）。
	stPanelHeader = lipgloss.NewStyle().
		Foreground(lipgloss.Color(accent)).
		Bold(true).
		MarginBottom(1)

	stKeyHint = lipgloss.NewStyle().
		Foreground(lipgloss.Color(colBg)).
		Background(lipgloss.Color(accent)).
		Bold(true).
		Padding(0, 1)

	stCounter = lipgloss.NewStyle().
		Foreground(lipgloss.Color(colBright)).
		Bold(true)

	stHelp = lipgloss.NewStyle().
		Foreground(lipgloss.Color(colBright)).
		Background(lipgloss.Color(colPanel)).
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color(accent)).
		Padding(1, 2)

	// Status chip: padding(0,0) so the chip hugs the title
	// text without claiming extra space. The bg color is the
	// signal; the chip's own padding was the thing that
	// pushed the title row out to "highlight bar" width.
	// 状态芯片：padding(0,0) 让芯片紧贴标题文字，不占额外宽
	// 度。背景色才是信号；芯片自带 padding 才是把标题行推成
	// "高亮条"宽度的元凶。
	stRunning = lipgloss.NewStyle().
		Foreground(lipgloss.Color(colBg)).
		Background(lipgloss.Color(colCyan)).
		Bold(true)

	stIdle = lipgloss.NewStyle().
		Foreground(lipgloss.Color(colBright)).
		Background(lipgloss.Color(colViolet)).
		Bold(true)

	stFinished = lipgloss.NewStyle().
		Foreground(lipgloss.Color(colBg)).
		Background(lipgloss.Color(accent)).
		Bold(true)

	stStatNum = lipgloss.NewStyle().
		Foreground(lipgloss.Color(colCyan)).
		Bold(true)
}

// isNoColor reports whether NO_COLOR is set per the spec at
// https://no-color.org/. Any non-empty value disables color; "0"
// and "false" (any case) are explicit opt-outs. This is the
// same logic as internal/ui.IsNoColor, duplicated here to avoid
// the tui↔ui import cycle.
//
// isNoColor 按 https://no-color.org/ 规范报告 NO_COLOR 是否被设置。
// 任何非空值禁用颜色；"0" 和 "false"（任意大小写）显式 opt-out。
// 逻辑与 internal/ui.IsNoColor 相同，因为 tui↔ui 的 import 循环
// 在此各放一份。
func isNoColor() bool {
	v, ok := os.LookupEnv("NO_COLOR")
	if !ok {
		return false
	}
	if v == "" || v == "0" || v == "false" || v == "False" || v == "FALSE" {
		return false
	}
	return true
}
