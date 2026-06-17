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
const (
	colBg     = "#0B0F12" // deep slate (was #000000 — pure black crushed dim text on some terminals)
	colPanel  = "#11181D" // subtle panel fill
	colAccent = "#00E676" // matrix green (geek terminal signature) — slightly cooler than #00FF41 for readability
	colAmber  = "#FFB300" // amber (creds / focus) — bumped from #FFB000 for AA contrast on dark bg
	colRed    = "#FF4D5E" // coral red (errors) — softer than #FF3344 but still clearly an error
	colDim    = "#5A6470" // slate gray (secondary info) — brighter than #3A3A3A so it survives on dark bg
	colMuted  = "#8A95A1" // light slate (tertiary)
	colBright = "#FFFFFF" // white (rare, emphasis only)
)

// Symbols / 符号
//
// Kept compact and ASCII-leaning where possible so the dashboard
// is readable on terminals that don't ship the heavier Unicode
// glyphs (Windows console hosts, some SSH gateways). Only the
// spinner/warn glyphs use non-ASCII, mirroring common terminal
// tool conventions (lazygit / k9s / btop).
const (
	symSpinner = "◐" // half-filled circle (geek aesthetic, not a spinner per se)
	symSuccess = "▸" // play / hit
	symError   = "✗"
	symDone    = "✓"
	symWarn    = "⚠"
	symActive  = "▶"
	symDot     = "·"
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
	// stats bar, keymap and surrounding newlines. View() uses this
	// to size the events list to fit the terminal without
	// scrolling.
	// chromeLines 是标题栏、状态条、按键提示和换行占用的行数。
	// View() 据此把事件列表裁剪到不超出终端高度。
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

	stTitle = lipgloss.NewStyle().
		Foreground(lipgloss.Color(colBright)).
		Background(lipgloss.Color(colAccent)).
		Bold(true).
		Padding(0, 1)

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

	stBox = lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color(accent)).
		Padding(0, 1)

	stPanelHeader = lipgloss.NewStyle().
		Foreground(lipgloss.Color(accent)).
		Bold(true).
		MarginBottom(0)

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
