// tui/styles.go — cyberpunk color palette and Lipgloss styles.
// tui/styles.go — 赛博朋克配色和 Lipgloss 样式。
package tui

import "github.com/charmbracelet/lipgloss"

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
var (
	// stTitle is the title bar at the top. / stTitle 顶部标题栏。
	stTitle = lipgloss.NewStyle().
		Foreground(lipgloss.Color(colGreen)).
		Background(lipgloss.Color(colBg)).
		Bold(true).
		Padding(0, 1)

	// stDim is for secondary info (counts, dim text). / stDim 次要信息。
	stDim = lipgloss.NewStyle().
		Foreground(lipgloss.Color(colDim))

	// stSuccess is for positive results (HTTP 200, banner, etc.).
	// stSuccess 成功结果。
	stSuccess = lipgloss.NewStyle().
			Foreground(lipgloss.Color(colGreen))

	// stWarn is for warnings / cred hits. / stWarn 警告/凭据命中。
	stWarn = lipgloss.NewStyle().
		Foreground(lipgloss.Color(colAmber)).
		Bold(true)

	// stError is for errors. / stError 错误。
	stError = lipgloss.NewStyle().
		Foreground(lipgloss.Color(colRed))

	// stBox wraps a panel with a single-line border. / stBox 面板边框。
	stBox = lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color(colGreen)).
		Padding(0, 1)
)
