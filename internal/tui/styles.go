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
	"github.com/muesli/termenv"
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

// ── Symbols (spec §6.3, single table) ──
// ── 符号表（spec §6.3，唯一一表）──
//
// The table has TWO renders: Unicode (default) and pure-ASCII
// (§6.2 ladder level 4, TERM=dumb / --tui-ascii). Every ASCII
// replacement is exactly one display column so the lattice width law
// survives degradation. Glyphs are vars (not consts) for exactly this
// reason — SetASCIIFallback swaps the table before the first frame.
//
// 符号表有两套渲染：Unicode（默认）与纯 ASCII（§6.2 阶梯第 4 级，
// TERM=dumb / --tui-ascii）。每个 ASCII 替换恰好占 1 显示列，宽度律
// 在降级后依然成立。字形是 var 而非 const 正是为此——SetASCIIFallback
// 在首帧前换表。
var (
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

	// Pipeline state symbols + spinner. / 管线状态符号 + spinner。
	spinnerFrames = "◐◓◑◒"
	symActive     = "▶"
	symDone       = "●"
	symIdle       = "○"

	// straggler Unicode glyphs outside the three families above.
	// / 上述三族之外的零散 Unicode 字形。
	symCheck     = "✓" // stage badge, StageDone
	glWarn       = "▲" // stall warn mark (spec: ⚠ retired for width)
	glChipFollow = "▼" // follow-mode chip
	glChipBrowse = "▲" // browse-mode chip
	glLag        = "↓" // browse "↓N new" lag counter
	glUp         = "↑" // help overlay key hints
	glDown       = "↓"
	glFold       = "×" // ×N merge suffix
	glMid        = "·" // in-row separator
	glBarEmpty   = "░" // progress bar empty cell
	glBarFull    = "█" // progress bar full cell
)

// ASCII render of the symbol table (§6.2 level 4). One column per
// glyph, all bytes < 0x80. Box junctions (┌├┬…) all collapse to '+'
// per the spec mapping (─→-, │→|).
// / 符号表的 ASCII 渲染（§6.2 第 4 级）。每字形一列，字节全 < 0x80。
// 框交接字符（┌├┬…）按 spec 映射全部折叠为 '+'（─→-，│→|）。
const (
	aSpinnerFrames = "****"
	aSymActive     = ">"
	aSymDone       = "*"
	aSymIdle       = "o"
	aSymCheck      = "+"
	aGlWarn        = "!"
	aGlChipFollow  = "v"
	aGlChipBrowse  = "^"
	aGlLag         = "v"
	aGlUp          = "^"
	aGlDown        = "v"
	aGlFold        = "x"
	aGlMid         = "."
	aGlBarEmpty    = "-"
	aGlBarFull     = "#"

	aBoxH  = "-"
	aBoxV  = "|"
	aBoxTL = "+"
	aBoxTR = "+"
	aBoxBL = "+"
	aBoxBR = "+"
	aBoxLS = "+"
	aBoxRS = "+"
	aBoxDn = "+"
	aBoxUp = "+"
)

// ── Lattice box drawing (spec §5.1, shared borders) ──
// ── Lattice 框字符（spec §5.1，共享边框）──
//
// Vars, not consts — the ASCII fallback (§6.2 level 4) swaps them for
// -|+ one-column equivalents before the first frame.
// 用 var 而非 const——ASCII 回退（§6.2 第 4 级）在首帧前把它们换成
// 单列的 -|+ 等价物。
var (
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
// and █ (full), index 0 = 1/8 filled. Var — ASCII fallback maps every
// fraction to '#'. / barFracs 是 ░（空）与 █（满）之间的八分块字族，
// 索引 0 = 1/8 填充。是 var——ASCII 回退把每个八分块映射为 '#'。
var barFracs = "▏▎▍▌▋▊▉"

// glSpark is the sparkline glyph ramp (index 0 = silent, 7 = peak),
// paired with sparkline()'s P95 scale (§6.4). Var — ASCII fallback
// gives a coarse two-level ramp: low half '.', high half '#'.
// / glSpark 是 sparkline 字形坡（索引 0 = 无速率，7 = 峰值），配合
// sparkline() 的 P95 尺度（§6.4）。是 var——ASCII 回退给粗两级坡：
// 低半 '.'，高半 '#'。
var glSpark = "▁▂▃▄▅▆▇█"

// Styles. / 样式。
var (
	stTitle       lipgloss.Style
	stMuted       lipgloss.Style
	stWarn        lipgloss.Style
	stErr         lipgloss.Style
	stFrame       lipgloss.Style
	stFrameZone   lipgloss.Style
	stPanelHeader lipgloss.Style
	stKeyHint     lipgloss.Style
	stHelp        lipgloss.Style
	stRunning     lipgloss.Style
	stIdleChip    lipgloss.Style
	stFinished    lipgloss.Style
)

func init() {
	// Color half of the §6.2 ladder (NO_COLOR kill switch + grayscale
	// for 16-color ANSI profiles); the truecolor→256 step below it is
	// lipgloss's own.
	// / §6.2 阶梯的颜色半边（NO_COLOR 总闸 + 16 色 ANSI profile 的灰
	// 度映射）；其下的 truecolor→256 一步由 lipgloss 自己完成。
	applyColorLadder(lipgloss.DefaultRenderer().ColorProfile())

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

	// stErr: error-signal text (stall !!, hard failures). err red,
	// bold to match stWarn's weight so the two alarm levels read as a
	// pair. / stErr：错误信号文本（stall !!、硬失败）。err 红加粗，
	// 与 stWarn 同字重，两级告警成对可读。
	stErr = lipgloss.NewStyle().
		Foreground(cErr).
		Bold(true)

	// Lattice frame glyphs (borders, separators, corners).
	// lattice 框字形（边框、分隔线、角）。
	stFrame = lipgloss.NewStyle().
		Foreground(cBorder)

	// stFrameZone: zone-accent variant of the frame glyphs — the only
	// sanctioned border recolor (spec §5.1). The active stage's region
	// borders render in cZone; everything else stays stFrame.
	// / stFrameZone：框字形的 zone accent 变体——唯一被认可的边框
	// 换色（spec §5.1）。活跃阶段的区域边框用 cZone，其余保持 stFrame。
	stFrameZone = lipgloss.NewStyle().
		Foreground(cZone)

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

// applyColorLadder is the color half of the §6.2 degradation ladder:
// NO_COLOR dims every signal/focus token (explicit kill switch); a
// 16-color ANSI profile remaps the signal domain to grayscale so the
// semantic hues never quantize to unpredictable palette entries.
// Tests may call it with a forced profile; styles must be rebuilt
// (buildStyles) afterwards to pick up the new tokens.
// / applyColorLadder 是 §6.2 降级阶梯的颜色半边：NO_COLOR 把一切信
// 号/焦点令牌降为 dim（显式总闸）；16 色 ANSI profile 把信号域映射
// 为灰度，语义色不会被量化到不可预测的调色板条目。测试可强制传入
// profile 调用；之后需重建样式（buildStyles）才能用到新令牌。
func applyColorLadder(profile termenv.Profile) {
	if isNoColor() {
		// Signal + focus roles degrade to dim; neutrals stay.
		// / 信号与焦点角色降为 dim；中性不变。
		cAccent = cDim
		cOk = cDim
		cErr = cDim
		cWarn = cDim
		cZone = cDim
		cIdle = cDim
		return
	}
	if profile == termenv.ANSI {
		// Grayscale: err keeps its bold weight via stErr/stWarn, warn
		// drops to the light-gray token, everything else goes plain
		// white. cIdle keeps its token — the IDLE chip stays
		// distinguishable on a monochrome scan surface.
		// / 灰度：err 靠 stErr/stWarn 保留加粗字重，warn 降到浅灰
		// 令牌，其余全部转纯白。cIdle 保留——IDLE 芯片在单色扫描面
		// 上仍可分辨。
		cAccent = cText
		cOk = cText
		cErr = cText
		cWarn = cDim
		cZone = cText
	}
}

// SetASCIIFallback swaps the symbol table to its pure-ASCII render
// (§6.2 ladder level 4). Idempotent; must run before the first frame —
// the production entry point is cmd/scan.go, which honours --tui-ascii
// (TERM=dumb routes to TextUI upstream and never reaches the TUI).
// Also rebuilds the help-overlay border, whose glyphs come from
// lipgloss's Border struct rather than the symbol table. Tests call it
// directly and restore the glyph table via glyphSnapshot.
// / SetASCIIFallback 把符号表换成纯 ASCII 渲染（§6.2 阶梯第 4 级）。
// 幂等；必须在首帧前调用——生产入口在 cmd/scan.go，尊重 --tui-ascii
// （TERM=dumb 在上游就路由到 TextUI，不会进 TUI）。帮助浮层边框的字
// 形来自 lipgloss 的 Border 结构而非符号表，因此一并重建。测试直接
// 调用并用 glyphSnapshot 恢复。
func SetASCIIFallback(on bool) {
	if !on {
		return
	}
	spinnerFrames = aSpinnerFrames
	symActive, symDone, symIdle = aSymActive, aSymDone, aSymIdle
	symCheck = aSymCheck
	glWarn = aGlWarn
	glChipFollow, glChipBrowse = aGlChipFollow, aGlChipBrowse
	glUp, glDown, glLag = aGlUp, aGlDown, aGlLag
	glFold = aGlFold
	glMid = aGlMid
	glBarEmpty, glBarFull = aGlBarEmpty, aGlBarFull
	boxH, boxV = aBoxH, aBoxV
	boxTL, boxTR, boxBL, boxBR = aBoxTL, aBoxTR, aBoxBL, aBoxBR
	boxLS, boxRS, boxDn, boxUp = aBoxLS, aBoxRS, aBoxDn, aBoxUp
	barFracs = strings.Repeat(aGlBarFull, barFracsUnicode)
	glSpark = strings.Repeat(aGlBarEmpty, sparkLow) +
		strings.Repeat(aGlBarFull, glSparkUnicode-sparkLow)
	// Help overlay border glyphs live in lipgloss's Border struct, not
	// in the symbol table — rebuild the style with the ASCII border.
	// / 帮助浮层边框字形在 lipgloss 的 Border 结构里而非符号表——用
	// ASCII 边框重建该样式。
	stHelp = stHelp.Border(lipgloss.Border{
		Top: aBoxH, Bottom: aBoxH, Left: aBoxV, Right: aBoxV,
		TopLeft: aBoxTL, TopRight: aBoxTR,
		BottomLeft: aBoxBL, BottomRight: aBoxBR,
	})
}

// glyphSnapshot / restoreGlyphTable bracket glyph-table mutations in
// tests so package-global swaps never leak between test cases.
// / glyphSnapshot / restoreGlyphTable 在测试中夹住符号表突变，包级
// 全局换表绝不泄漏到别的用例。
type glyphSnapshot struct {
	vals []string
}

// glyphNames lists every glyph table var in one place — a new glyph
// var must be added here AND to SetASCIIFallback.
// / glyphNames 把所有字形表 var 集中在一处——新增字形 var 必须同时
// 加进这里和 SetASCIIFallback。
var glyphNames = []string{
	"spinnerFrames", "symActive", "symDone", "symIdle", "symCheck",
	"glWarn", "glChipFollow", "glChipBrowse", "glUp", "glDown", "glLag",
	"glFold", "glMid", "glBarEmpty", "glBarFull", "barFracs", "glSpark",
	"boxH", "boxV", "boxTL", "boxTR", "boxBL", "boxBR", "boxLS", "boxRS",
	"boxDn", "boxUp",
}

// glyphPointers maps names → var addresses for snapshot/restore.
// / glyphPointers 把名字映射到 var 地址，供快照/恢复。
var glyphPointers = map[string]*string{
	"spinnerFrames": &spinnerFrames, "symActive": &symActive,
	"symDone": &symDone, "symIdle": &symIdle, "symCheck": &symCheck,
	"glWarn": &glWarn, "glChipFollow": &glChipFollow,
	"glChipBrowse": &glChipBrowse, "glUp": &glUp, "glDown": &glDown,
	"glLag": &glLag, "glFold": &glFold, "glMid": &glMid,
	"glBarEmpty": &glBarEmpty, "glBarFull": &glBarFull,
	"barFracs": &barFracs, "glSpark": &glSpark,
	"boxH": &boxH, "boxV": &boxV, "boxTL": &boxTL, "boxTR": &boxTR,
	"boxBL": &boxBL, "boxBR": &boxBR, "boxLS": &boxLS, "boxRS": &boxRS,
	"boxDn": &boxDn, "boxUp": &boxUp,
}

func glyphSnapshotFn() glyphSnapshot {
	s := glyphSnapshot{vals: make([]string, 0, len(glyphNames))}
	for _, n := range glyphNames {
		s.vals = append(s.vals, *glyphPointers[n])
	}
	return s
}

func (s glyphSnapshot) restore() {
	for i, n := range glyphNames {
		*glyphPointers[n] = s.vals[i]
	}
	// stHelp is a style, not a glyph var — restore its Unicode border
	// explicitly (SetASCIIFallback replaces it with the ASCII border).
	// / stHelp 是样式而非字形 var——显式恢复其 Unicode 边框
	// （SetASCIIFallback 会把它换成 ASCII 边框）。
	stHelp = stHelp.Border(lipgloss.NormalBorder())
}

// Lengths of the Unicode originals, captured at init so the ASCII
// ramp strings keep the same RUNE count (index compatibility for
// barFracs fractions and sparkline levels).
// / Unicode 原串的长度，init 时捕获，让 ASCII 坡串保持相同 RUNE 数
// （barFracs 八分块与 sparkline 层级的索引兼容）。
var (
	barFracsUnicode = len([]rune(barFracs))
	glSparkUnicode  = len([]rune(glSpark))
	sparkLow        = 4 // glSpark indexes 0-3 → '.', 4-7 → '#' / 低半 '.' 高半 '#'
)

// renderBar renders a width-w progress bar with sub-character
// precision: full cells are glBarFull, a fractional cell takes the
// nearest eighth glyph (barFracs), the rest are glBarEmpty. ASCII
// fallback renders #-ramps (§6.2 level 4). filled/total=0 (unknown
// denominator) renders w empty cells; over-fill clamps to 100%.
//
// renderBar 以亚字符精度渲染宽度 w 的进度条：整格 glBarFull，小数格
// 取最接近的八分块字形（barFracs），其余 glBarEmpty。ASCII 回退渲染
// #-坡（§6.2 第 4 级）。filled/total=0（未知分母）渲染 w 个空格；
// 超填钳到 100%。
func renderBar(filled, total, w int) string {
	if w <= 0 {
		return ""
	}
	if total <= 0 {
		return strings.Repeat(glBarEmpty, w)
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
	sb.WriteString(strings.Repeat(glBarFull, full))
	if full < w {
		used := full
		if eighths := int(frac * 8); eighths > 0 {
			// barFracs is a string, so slicing is byte-based; go through
			// []rune to grab one whole glyph.
			// / barFracs 是 string，切片按字节；经 []rune 取完整字形。
			sb.WriteRune([]rune(barFracs)[eighths-1])
			used++
		}
		sb.WriteString(strings.Repeat(glBarEmpty, w-used))
	}
	return sb.String()
}

// sparkline returns a string of `width` ramp glyphs (glSpark) repres-
// enting the samples normalized to [0, 1]. Empty samples or width<=0
// → empty string. Samples shorter than width are padded with the
// lowest glyph. (Fixed-scale P95 normalization lands in T3 — spec
// §6.4; the windowed max keeps T0 visually unchanged.)
//
// sparkline 返回 `width` 个坡形字形（glSpark）的字符串，表示归一化
// 到 [0, 1] 的样本。（P95 固定尺度归一化 T3 落地——spec §6.4；
// T0 沿用窗口 max，视觉不变。）
func sparkline(samples []float64, width int) string {
	if len(samples) == 0 || width <= 0 {
		return ""
	}
	glyphs := []rune(glSpark)

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
