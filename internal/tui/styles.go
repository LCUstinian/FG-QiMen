// tui/styles.go — Sliver C2 inspired terminal palette.
//
// Dark navy background, cyan accents, minimal borders.
// Professional operator terminal aesthetic.
//
// Palette honours the NO_COLOR env var (https://no-color.org/).
package tui

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// Color palette — Sliver C2 inspired
const (
	colBg     = "#0a0e14" // deep navy (Sliver dark)
	colPanel  = "#0d1117" // subtle panel fill (GitHub dark-ish)
	colAccent = "#00d4ff" // electric cyan (Sliver primary)
	colAmber  = "#ffb347" // warm amber (creds)
	colRed    = "#ff4757" // coral red (errors)
	colCyan   = "#00d4ff" // cyan (scanning/live)
	colYellow = "#ffd700" // gold (transitional)
	colViolet = "#a78bfa" // soft violet (idle)
	colDim    = "#4a5568" // slate gray (secondary)
	colMuted  = "#718096" // light slate (tertiary)
	colBright = "#e2e8f0" // off-white (primary text)
	colBorder = "#1a202c" // dark border (subtle)
)

// Symbols
const (
	spinnerFrames = "◐◓◑◒"
	symSpinner    = "◐"
	symSuccess    = "▸"
	symError      = "✗"
	symCredHit    = "✓" // legacy cred-tag glyph (renamed from symDone in v0.7.0)
	symWarnTag    = "⚠" // legacy warn-tag glyph (renamed from symWarn in v0.7.0)
	symActive     = "▶"
	symDot        = "·"
)

// Box drawing
// Box-drawing characters used by the dashboard layout. The
// top-left / bottom-right corners (boxTL / boxBR) were unused since
// v0.2's box-banner removal; deleted in v0.3.1 (P6.2 of the audit
// roadmap). / 仪表板布局用的方框字符。左上 / 右下角（boxTL / boxBR）
// 自 v0.2 移除方框 banner 后就没用；v0.3.1 删除（审计路线图 P6.2）。
const (
	boxH  = "─"
	boxV  = "│"
	boxTR = "┐"
	boxBL = "└"
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

// --- v0.7.0 additions: severity colors, symbols, progress bars, sparkline ---

// GitHub Dark palette — well-tested for legibility, contrast >=4.5:1.
// / GitHub Dark 调色板——可读性经过验证，对比度 >=4.5:1。
//
// renderers in Spec B Tasks 5-9; declared here in Task 2 to lock the
// contract early. Will become used as soon as Task 5's header renderer
// lands.
//
//nolint:unused // colorBg / colorAccent / colorBorder are wired by region
var (
	colorBg     = lipgloss.Color("#0e1116") // almost-black
	colorFg     = lipgloss.Color("#e6edf3") // off-white
	colorFgDim  = lipgloss.Color("#8b949e") // dimmed gray
	colorAccent = lipgloss.Color("#58a6ff") // cyan-blue (brand)
	colorOk     = lipgloss.Color("#3fb950") // green (credential success)
	colorWarn   = lipgloss.Color("#d29922") // yellow (info hit / partial)
	colorErr    = lipgloss.Color("#f85149") // red (critical hit / default-creds)
	colorBorder = lipgloss.Color("#30363d") // subtle dividers
)

// Symbol table — no emoji; terminal fallback safe. / 符号表——
// 无 emoji；terminal 回退安全。
const (
	symCriticalHit = "!!"  // default-creds accepted (red flag)
	symInfoHit     = "✓"   // service identified (yellow)
	symCredSuccess = "✓✓"  // credential success (green)
	symMiss        = "✗"   // refused/timeout/dns (gray)
	symWarn        = "⚠"   // partial / TLS handshake fail
	symScanning    = "..." // spinner uses Bubbletea spinner.Model
	symDone        = "●"
	symIdle        = "○"
)

// renderBar renders a progress bar of width w filled to ratio.
// Width-0 / total-0 returns w empty glyphs. Over-filled clamps to 100%.
// / renderBar 渲染宽度 w、按 ratio 填充的进度条。w=0 或 total=0
// 返回 w 个空字符。超填钳到 100%。
func renderBar(filled, total, w int) string {
	if w <= 0 {
		return ""
	}
	if total <= 0 {
		return strings.Repeat("░", w)
	}
	ratio := float64(filled) / float64(total)
	if ratio > 1 {
		ratio = 1
	}
	if ratio < 0 {
		ratio = 0
	}
	f := int(float64(w) * ratio)
	return strings.Repeat("▓", f) + strings.Repeat("░", w-f)
}

// sparkline returns a string of `width` Unicode block-element glyphs
// representing the samples normalized to [0, 1]. Empty samples or
// width<=0 → empty string. Samples shorter than width are padded
// with the lowest glyph. / sparkline 返回 `width` 个 Unicode 块元
// 素字符的字符串，表示归一化到 [0, 1] 的样本。
func sparkline(samples []float64, width int) string {
	if len(samples) == 0 || width <= 0 {
		return ""
	}
	glyphs := []rune{'▁', '▂', '▃', '▄', '▅', '▆', '▇', '█'}

	// Take last `width` samples.
	n := len(samples)
	if n > width {
		samples = samples[n-width:]
		n = width
	}

	// Find max for normalization.
	maxV := 0.0
	for _, s := range samples {
		if s > maxV {
			maxV = s
		}
	}
	if maxV == 0 {
		return strings.Repeat(string(glyphs[0]), width)
	}

	out := make([]rune, width)
	pad := width - n
	for i := 0; i < pad; i++ {
		out[i] = glyphs[0]
	}
	for i, s := range samples {
		idx := int(float64(len(glyphs)-1) * s / maxV)
		if idx >= len(glyphs) {
			idx = len(glyphs) - 1
		}
		out[pad+i] = glyphs[idx]
	}
	return string(out)
}

// truncate limits s to n runes, appending "..." if truncated. Stays
// safe on multi-byte UTF-8 boundaries. / truncate 限制 s 到 n 个
// rune，超出加 "..."。UTF-8 安全（在 rune 边界切）。
func truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	if n <= 3 {
		return string(runes[:n])
	}
	return string(runes[:n-3]) + "..."
}

// symFor returns the status symbol for an event kind.
// / symFor 返回事件类型对应的状态符号。
func symFor(kind string) string {
	switch kind {
	case "critical_hit":
		return symCriticalHit
	case "hit":
		return symInfoHit
	case "cred_success":
		return symCredSuccess
	case "warn":
		return symWarn
	case "miss":
		return symMiss
	default:
		return symIdle
	}
}

// severityColor returns the right color for an event kind. Honors
// flash expiry: if host:port is currently flashing, return colorErr.
// / severityColor 返回事件类型对应的颜色。遵循 flash 过期：如果
// host:port 当前在 flash，返回 colorErr。
func (m Model) severityColor(e eventEntry) lipgloss.Color {
	key := fmt.Sprintf("%s:%d", e.Host, e.Port)
	if until, ok := m.flashUntil[key]; ok && time.Now().Before(until) {
		return colorErr
	}
	switch e.Kind {
	case "cred_success":
		return colorOk
	case "hit":
		return colorWarn
	case "warn":
		return colorWarn
	case "miss":
		return colorFgDim
	default:
		return colorFg
	}
}
