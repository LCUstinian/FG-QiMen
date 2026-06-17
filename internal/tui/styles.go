// tui/styles.go — Sliver C2 inspired terminal palette.
//
// Dark navy background, cyan accents, minimal borders.
// Professional operator terminal aesthetic.
//
// Palette honours the NO_COLOR env var (https://no-color.org/).
package tui

import (
	"os"

	"github.com/charmbracelet/lipgloss"
)

// Color palette — Sliver C2 inspired
const (
	colBg      = "#0a0e14" // deep navy (Sliver dark)
	colPanel   = "#0d1117" // subtle panel fill (GitHub dark-ish)
	colAccent  = "#00d4ff" // electric cyan (Sliver primary)
	colAmber   = "#ffb347" // warm amber (creds)
	colRed     = "#ff4757" // coral red (errors)
	colCyan    = "#00d4ff" // cyan (scanning/live)
	colYellow  = "#ffd700" // gold (transitional)
	colViolet  = "#a78bfa" // soft violet (idle)
	colDim     = "#4a5568" // slate gray (secondary)
	colMuted   = "#718096" // light slate (tertiary)
	colBright  = "#e2e8f0" // off-white (primary text)
	colBorder  = "#1a202c" // dark border (subtle)
)

// Symbols
const (
	spinnerFrames = "◐◓◑◒"
	symSpinner    = "◐"
	symSuccess    = "▸"
	symError      = "✗"
	symDone       = "✓"
	symWarn       = "⚠"
	symActive     = "▶"
	symDot        = "·"
)

// Box drawing
const (
	boxH  = "─"
	boxV  = "│"
	boxTL = ""
	boxTR = "┐"
	boxBL = "└"
	boxBR = ""
)

// Layout
const (
	minWidth      = 80
	statsColWidth = 28
	eventsColMin  = 48
	chromeLines   = 6
)

// Styles
var (
	stTitle       lipgloss.Style
	stDim         lipgloss.Style
	stMuted       lipgloss.Style
	stSuccess     lipgloss.Style
	stWarn        lipgloss.Style
	stError       lipgloss.Style
	stBox         lipgloss.Style
	stPanelHeader lipgloss.Style
	stKeyHint     lipgloss.Style
	stCounter     lipgloss.Style
	stHelp        lipgloss.Style
	stRunning     lipgloss.Style
	stIdle        lipgloss.Style
	stFinished    lipgloss.Style
	stStatNum     lipgloss.Style
)

func init() {
	accent := colAccent
	dimFg := colDim
	mutedFg := colMuted
	if isNoColor() {
		accent = colDim
		dimFg = colDim
		mutedFg = colDim
	}

	// Title: cyan bold, no background
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

	// Panel box: subtle dark border
	stBox = lipgloss.NewStyle().
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color(colBorder)).
		Padding(0, 1)

	// Panel header: cyan bold
	stPanelHeader = lipgloss.NewStyle().
		Foreground(lipgloss.Color(accent)).
		Bold(true).
		MarginBottom(1)

	// Key hint: cyan bg, dark text
	stKeyHint = lipgloss.NewStyle().
		Foreground(lipgloss.Color(colBg)).
		Background(lipgloss.Color(accent)).
		Bold(true).
		Padding(0, 1)

	stCounter = lipgloss.NewStyle().
		Foreground(lipgloss.Color(colBright)).
		Bold(true)

	// Help overlay: dark panel, cyan border
	stHelp = lipgloss.NewStyle().
		Foreground(lipgloss.Color(colBright)).
		Background(lipgloss.Color(colPanel)).
		Border(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color(accent)).
		Padding(1, 2)

	// Status chips
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

	// Counter numbers: cyan
	stStatNum = lipgloss.NewStyle().
		Foreground(lipgloss.Color(colCyan)).
		Bold(true)
}

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
