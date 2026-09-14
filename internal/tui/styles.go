// tui/styles.go — TUI v3 role-token visual system (spec §6).
//
// tui/styles.go — TUI v3 角色令牌视觉系统（spec §6）。
//
// ONE palette, expressed as role tokens in four domains (spec §6.1):
// background (cBg/cPanel), neutral (cBorder/cText/cDim/cMuted), signal
// (cAccent/cOk/cErr/cWarn), focus (cZone/cIdle). The v0.7.0 era mixed
// a Sliver-C2 palette and a GitHub-Dark palette in the same frame —
// both are retired; every color reference in the package must go
// through these tokens.
//
// 一套调色板，以四大域的角色令牌表达（spec §6.1）：背景（cBg/cPanel）、
// 中性（cBorder/cText/cDim/cMuted）、信号（cAccent/cOk/cErr/cWarn）、
// 焦点（cZone/cIdle）。v0.7.0 时代同一帧里混用 Sliver C2 与 GitHub
// Dark 两套色——全部废弃；包内一切颜色引用必须走这些令牌。
//
// ONE symbol table (spec §6.3): spinner + event symbols + state
// symbols. The legacy symSuccess/symError/symCredHit/symWarnTag aliases
// are deleted.
//
// 一套符号表（spec §6.3）：spinner + 事件符号 + 状态符号。legacy 的
// symSuccess/symError/symCredHit/symWarnTag 别名已删除。
//
// ONE progress-bar implementation (spec §5.5): renderBar with
// sub-character precision on the single glyph family
// {░▏▎▍▌▋▊▉█}. The dual-glyph ▓/░ bars are retired — one family,
// one baseline, no misalignment.
//
// 一套进度条实现（spec §5.5）：renderBar 以单字族 {░▏▎▍▌▋▊▉█} 做
// 亚字符精度。双字形 ▓/░ 条已废弃——一字族、一基线、无错位。
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

// ── Role tokens (spec §6.1) ──
// ── 角色令牌（spec §6.1）──
var (
	// background domain / 背景域
	cBg    = lipgloss.Color("#0b0e14")
	cPanel = lipgloss.Color("#0d1117")

	// neutral domain / 中性域
	cBorder = lipgloss.Color("#1c2333")
	cText   = lipgloss.Color("#e6edf3")
	cDim    = lipgloss.Color("#8b949e")
	cMuted  = lipgloss.Color("#6e7681")

	// signal domain / 信号域
	cAccent = lipgloss.Color("#00e5ff")
	cOk     = lipgloss.Color("#2bd576")
	cErr    = lipgloss.Color("#ff5c57")
	cWarn   = lipgloss.Color("#ffb347")

	// focus domain / 焦点域
	cZone = lipgloss.Color("#58a6ff") // T3 wires zone accent / T3 接线 zone accent
	cIdle = lipgloss.Color("#a78bfa")
)

// Compile-time references to palette tokens not yet wired to a renderer
// (T3 zone accent). Keeps the tokens in the single palette without
// tripping the unused linter. / 尚未接线到渲染器的调色板令牌的编译期
// 引用（T3 zone accent）。让令牌留在唯一调色板里又不触发 unused。
var _ = cZone

// ── Symbols (spec §6.3, single table) ──
// ── 符号表（spec §6.3，唯一一表）──
const (
	spinnerFrames = "◐◓◑◒"

	// Event severity symbols. Every token is a fixed 3-column bracket
	// tag: pure ASCII, unambiguous width in every monospace font (the
	// old ✓/✗ glyphs are ambiguous-width in some fonts and sized
	// unevenly), and all differentiation rides on the signal color.
	// / 事件严重度符号。每个令牌都是固定 3 列的方括号标记：纯 ASCII，
	// 在任何等宽字体下列宽无歧义（旧 ✓/✗ 字形在部分字体里是 ambiguous
	// width 且大小不齐），语义差异完全由信号色承担。
	symCriticalHit = "[!]" // default-creds accepted (red flag)
	symInfoHit     = "[+]" // service identified (yellow)
	symCredSuccess = "[*]" // credential success (green)
	symMiss        = "[-]" // refused/timeout/dns (gray)
	symWarn        = "[~]" // partial / TLS handshake fail

	// Pipeline state symbols. / 管线状态符号。
	symActive = "▶"
	symDone   = "●"
	symIdle   = "○"
)

// ── Lattice box drawing (spec §5.1, shared borders) ──
// ── Lattice 框字符（spec §5.1，共享边框）──
const (
	boxH  = "─"
	boxV  = "│"
	boxTL = "┌"
	boxTR = "┐"
	boxBL = "└"
	boxBR = "┘"
	boxLS = "├"
	boxRS = "┤"
	boxDn = "┬" // body-top junction (wide two-column) / 主体上分隔的三通（宽屏双列）
	boxUp = "┴" // body-bottom junction / 主体下分隔的三通
)

// barFracs is the eighth-fraction glyph family used between ░ (empty)
// and █ (full), index 0 = 1/8 filled. / barFracs 是 ░（空）与 █（满）
// 之间的八分块字族，索引 0 = 1/8 填充。
const barFracs = "▏▎▍▌▋▊▉"

// Styles. / 样式。
var (
	stTitle       lipgloss.Style
	stMuted       lipgloss.Style
	stWarn        lipgloss.Style
	stFrame       lipgloss.Style
	stPanelHeader lipgloss.Style
	stKeyHint     lipgloss.Style
	stHelp        lipgloss.Style
	stRunning     lipgloss.Style
	stIdleChip    lipgloss.Style
	stFinished    lipgloss.Style
)

func init() {
	if isNoColor() {
		// Signal + focus roles degrade to dim; neutrals stay. The
		// finer truecolor→256→grayscale ladder is lipgloss's job
		// (spec §6.2); NO_COLOR is the explicit kill switch.
		// 信号与焦点角色降为 dim；中性不变。truecolor→256→灰度的
		// 细阶梯由 lipgloss 负责（spec §6.2）；NO_COLOR 是显式总闸。
		cAccent = cDim
		cOk = cDim
		cErr = cDim
		cWarn = cDim
		cZone = cDim
		cIdle = cDim
	}

	// Title text inside the lattice top border: accent bold.
	// lattice 顶边框内的标题文本：accent 加粗。
	stTitle = lipgloss.NewStyle().
		Foreground(cAccent).
		Bold(true)

	stMuted = lipgloss.NewStyle().
		Foreground(cMuted)

	stWarn = lipgloss.NewStyle().
		Foreground(cWarn).
		Bold(true)

	// Lattice frame glyphs (borders, separators, corners).
	// lattice 框字形（边框、分隔线、角）。
	stFrame = lipgloss.NewStyle().
		Foreground(cBorder)

	// Panel header: accent bold. Single flush variant — the margin
	// variant is retired; inside the lattice a margin would inject
	// stray blank rows into band composition.
	// 面板标题：accent 加粗。仅保留 flush 变体——带边距变体已废弃；
	// lattice 内边距会往 band 组合里注入多余空行。
	stPanelHeader = lipgloss.NewStyle().
		Foreground(cAccent).
		Bold(true)

	// Key hint: accent bg, dark text. / 键位提示：accent 底、深色字。
	stKeyHint = lipgloss.NewStyle().
		Foreground(cBg).
		Background(cAccent).
		Bold(true).
		Padding(0, 1)

	// Help overlay: panel bg, accent border. / 帮助浮层：panel 底、accent 边框。
	stHelp = lipgloss.NewStyle().
		Foreground(cText).
		Background(cPanel).
		Border(lipgloss.NormalBorder()).
		BorderForeground(cAccent).
		Padding(1, 2)

	// Status chips (inverse-video). / 状态芯片（反白）。
	stRunning = lipgloss.NewStyle().
		Foreground(cBg).
		Background(cAccent).
		Bold(true)

	// stIdleChip (was stIdle): IDLE chip on the idle-role violet.
	// Renamed to avoid confusion with the symIdle constant.
	// stIdleChip（原 stIdle）：IDLE 芯片用 idle 角色紫。改名以避免
	// 与 symIdle 常量混淆。
	stIdleChip = lipgloss.NewStyle().
		Foreground(cText).
		Background(cIdle).
		Bold(true)

	// DONE chip on the ok-role green: success is the semantic.
	// DONE 芯片用 ok 角色绿：语义即"成功"。
	stFinished = lipgloss.NewStyle().
		Foreground(cBg).
		Background(cOk).
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

// renderBar renders a width-w progress bar with sub-character
// precision: full cells are █, a fractional cell takes the nearest
// eighth glyph (▏▎▍▌▋▊▉), the rest are ░. filled/total=0 (unknown
// denominator) renders w empty cells; over-fill clamps to 100%.
//
// renderBar 以亚字符精度渲染宽度 w 的进度条：整格 █，小数格取最
// 接近的八分块字形（▏▎▍▌▋▊▉），其余 ░。filled/total=0（未知分母）
// 渲染 w 个空格；超填钳到 100%。
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
	pos := float64(w) * ratio
	full := int(pos)
	frac := pos - float64(full)

	var sb strings.Builder
	sb.WriteString(strings.Repeat("█", full))
	if full < w {
		used := full
		if eighths := int(frac * 8); eighths > 0 {
			// barFracs is a string, so slicing is byte-based; go through
			// []rune to grab one whole 3-byte glyph.
			// / barFracs 是 string，切片按字节；经 []rune 取完整的
			// 3 字节字形。
			sb.WriteRune([]rune(barFracs)[eighths-1])
			used++
		}
		sb.WriteString(strings.Repeat("░", w-used))
	}
	return sb.String()
}

// sparkline returns a string of `width` Unicode block-element glyphs
// representing the samples normalized to [0, 1]. Empty samples or
// width<=0 → empty string. Samples shorter than width are padded
// with the lowest glyph. (Fixed-scale P95 normalization lands in T3 —
// spec §6.4; the windowed max keeps T0 visually unchanged.)
//
// sparkline 返回 `width` 个 Unicode 块元素字符的字符串，表示归一化
// 到 [0, 1] 的样本。（P95 固定尺度归一化 T3 落地——spec §6.4；
// T0 沿用窗口 max，视觉不变。）
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
// flash expiry: if host:port is currently flashing, return the flash
// color — err red for most kinds, but ok green for cred_success (a
// credential hit is the scan's most valuable finding; flashing it in
// the same red as errors made operators misread wins as failures).
// / severityColor 返回事件类型对应的颜色。遵循 flash 过期：如果
// host:port 当前在 flash，返回 flash 色——多数事件 err 红，但
// cred_success 用 ok 绿（凭据命中价值最高，闪红会被误读为失败）。
func (m Model) severityColor(e eventEntry) lipgloss.Color {
	key := fmt.Sprintf("%s:%d", e.Host, e.Port)
	if until, ok := m.flashUntil[key]; ok && time.Now().Before(until) {
		if e.Kind == "cred_success" {
			return cOk
		}
		return cErr
	}
	switch e.Kind {
	case "cred_success":
		return cOk
	case "hit":
		return cWarn
	case "warn":
		return cWarn
	case "miss":
		return cDim
	default:
		return cText
	}
}
