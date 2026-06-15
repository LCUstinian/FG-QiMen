// tui/styles.go — cyberpunk color palette and Lipgloss styles.
// tui/styles.go — 赛博朋克配色和 Lipgloss 样式。
//
// Palette honors the NO_COLOR env var (https://no-color.org/).
// When the operator sets NO_COLOR=1 (or any non-empty non-"0"/
// non-"false" value), all foreground colors collapse to the dim
// gray and the cyberpunk accent goes away — the dashboard still
// renders correctly, just monochrome. This is an in-process
// override; we don't reload on env changes mid-scan.
//
// Note: the env check lives here in the tui package, not in
// internal/ui, to avoid an import cycle (tui imports ui for
// the factory; ui imports tui for NewProgram). NO_COLOR is
// intrinsically a tui-palette concern, not a ui-selection one,
// so duplicating the 4-line lookup is cleaner than threading
// the value through.
//
// 调色板尊重 NO_COLOR 环境变量（https://no-color.org/）。当操作员
// 设 NO_COLOR=1（或任何非空、非"0"、非"false"值）时，所有前景色塌
// 缩到 dim gray，赛博朋克口音消失——dashboard 仍正常渲染，只是单
// 色。这是进程内覆盖；扫描中环境变化不重载。
//
// 注：env 检查写在这里（tui 包）而非 internal/ui，是为了避免 import
// 循环（tui 为 factory 导 ui；ui 为 NewProgram 导 tui）。NO_COLOR
// 本质上是 tui 调色板问题，不是 ui 选型问题；与其在调用点传值，
// 不如把 4 行查找逻辑各放一份。
package tui

import (
	"os"

	"github.com/charmbracelet/lipgloss"
)

// Color palette / 配色方案
// (kept as plain strings so we can swap them at test time if needed)
// （用纯字符串保存，方便测试时替换）
const (
	colBg     = "#000000" // deep black
	colGreen  = "#00FF41" // cyberpunk green (success / progress)
	colAmber  = "#FFB000" // amber (warning / cred hits / focus)
	colRed    = "#FF3344" // dark red (error)
	colDim    = "#3A3A3A" // dim gray (secondary)
	colBright = "#FFFFFF" // white (rare, emphasis only)
)

// Symbols / 符号
const (
	symSpinner = "⟳"
	symSuccess = "◆"
	symError   = "✗"
	symDone    = "✓"
	symWarn    = "⚠"
	symActive  = "▶"
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
	// stSuccess is for positive results (HTTP 200, banner, etc.).
	// stSuccess 成功结果。
	stSuccess lipgloss.Style
	// stWarn is for warnings / cred hits. / stWarn 警告/凭据命中。
	stWarn lipgloss.Style
	// stError is for errors. / stError 错误。
	stError lipgloss.Style
	// stBox wraps a panel with a single-line border. / stBox 面板边框。
	stBox lipgloss.Style
)

func init() {
	// accent picks the foreground color used for headings, success,
	// and box borders. In NO_COLOR mode it collapses to the dim
	// gray so everything still renders (just monochrome).
	//
	// accent 是标题/成功/边框的前景色。NO_COLOR 模式下塌缩到
	// dim gray，渲染仍正常（只是单色）。
	accent := colGreen
	if isNoColor() {
		accent = colDim
	}

	stTitle = lipgloss.NewStyle().
		Foreground(lipgloss.Color(accent)).
		Background(lipgloss.Color(colBg)).
		Bold(true).
		Padding(0, 1)

	stDim = lipgloss.NewStyle().
		Foreground(lipgloss.Color(colDim))

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
