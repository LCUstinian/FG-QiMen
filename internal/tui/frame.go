// frame.go — lattice frame geometry (spec §5.1/§5.2): pure helpers
// that turn region content into the single-layer shared-border grid.
// No model state, no clock — the golden matrix pins these outputs
// byte-for-byte (spec §11.1).
//
// Width law: a single-cell row is "│" + cell(w-2) + "│"; a wide
// two-column row is "│" + left(leftW) + "│" + right(rightW) + "│" with
// leftW + rightW == w-3 (three border columns total).
//
// frame.go — lattice 帧几何（spec §5.1/§5.2）：把区域内容变成单层共
// 享边框网格的纯辅助函数。无 model 状态、无时钟——golden 矩阵逐字
// 节钉住这些输出（spec §11.1）。
//
// 宽度律：单格行 = "│" + cell(w-2) + "│"；宽屏双列行 = "│" +
// left(leftW) + "│" + right(rightW) + "│"，leftW + rightW == w-3
// （共三列边框字符）。
package tui

import (
	"strings"

	"github.com/LCUstinian/FG-QiMen/internal/types"

	"github.com/charmbracelet/lipgloss"
)

// wideSplit returns the wide-mode inner cell widths with
// leftW + rightW == w-3. The left column takes ~40%, clamped to
// [24, 48] so both cells stay readable; the right column keeps ≥ 12.
// / wideSplit 返回宽屏双列的格内宽度，leftW + rightW == w-3。左列约
// 40%，钳到 [24, 48] 保证两格可读；右列保底 12。
func wideSplit(w int) (leftW, rightW int) {
	leftW = w * 2 / 5
	if leftW < 24 {
		leftW = 24
	}
	if leftW > 48 {
		leftW = 48
	}
	if maxLeft := w - 3 - 12; leftW > maxLeft {
		leftW = maxLeft
	}
	rightW = w - 3 - leftW
	return leftW, rightW
}

// hBorder renders one horizontal border line of total width w with
// the given end junctions; junc (┬/┴) replaces the fill at 0-based
// offset jx for the wide body separators (0 = none).
// / hBorder 渲染总宽 w 的水平边框行，端点用给定三通字符；junc
// （┬/┴）替换 0 基偏移 jx 处的填充字符，供宽屏主体分隔线用
// （0 = 无三通）。
func hBorder(w int, left, right, junc string, jx int) string {
	var sb strings.Builder
	sb.WriteString(left)
	for i := 1; i < w-1; i++ {
		if jx > 0 && i == jx {
			sb.WriteString(junc)
		} else {
			sb.WriteString(boxH)
		}
	}
	sb.WriteString(right)
	return sb.String()
}

// zoneRegion names the lattice cells that the zone accent (§5.1) can
// light up. The stage→region mapping: ALIVE/PORT-SCAN → PROGRESS,
// IDENTIFY → TOP PLUGINS, CRED → LIVE EVENTS, DONE/IDLE → none.
// / zoneRegion 枚举 zone accent（§5.1）可点亮的格子。阶段→区域映射：
// ALIVE/PORT-SCAN → PROGRESS，IDENTIFY → TOP PLUGINS，CRED →
// LIVE EVENTS，DONE/IDLE → 无。
type zoneRegion uint8

const (
	zoneNone zoneRegion = iota
	zoneProgress
	zonePlugins
	zoneEvents
)

// stageZone maps a State stage value to its accented region (§5.1).
// / stageZone 把 State 阶段值映射到其 accent 区域（§5.1）。
func stageZone(stage int64) zoneRegion {
	switch {
	case stage == int64(types.StageAlive) || stage == int64(types.StagePortScan):
		return zoneProgress
	case stage == int64(types.StageIdentify):
		return zonePlugins
	case stage == int64(types.StageCred):
		return zoneEvents
	default:
		return zoneNone
	}
}

// frameStyle picks the border style for a cell: zone accent when the
// cell is the active stage's region, plain border otherwise. The
// returned style is applied per border glyph so golden text (ANSI-
// free) is byte-identical either way.
// / frameStyle 为格子选边框样式：该格是活跃阶段区域时用 zone
// accent，否则用普通 border。返回的样式逐字形施加，golden 文本（无
// ANSI）两种情况下逐字节一致。
func frameStyle(cell, active zoneRegion) lipgloss.Style {
	if cell != zoneNone && cell == active {
		return stFrameZone
	}
	return stFrame
}

// hBorderZone renders hBorder with per-segment styles: glyphs left of
// jx use styleL, glyphs from jx on use styleR (jx ≤ 0 → whole line in
// styleL). The wide separators split into a left and a right half that
// close different cells, so each half takes its cell's border style.
// / hBorderZone 渲染分段样式的 hBorder：jx 左侧字形用 styleL，jx 起
// 用 styleR（jx ≤ 0 → 整行 styleL）。宽屏分隔线左右两半闭合不同的
// 格，各自取所属格的边框样式。
func hBorderZone(w int, left, right, junc string, jx int, styleL, styleR lipgloss.Style) string {
	var sb strings.Builder
	sb.WriteString(styleL.Render(left))
	for i := 1; i < w-1; i++ {
		st := styleL
		if jx > 0 && i >= jx {
			st = styleR
		}
		if jx > 0 && i == jx {
			sb.WriteString(st.Render(junc))
		} else {
			sb.WriteString(st.Render(boxH))
		}
	}
	sb.WriteString(styleR.Render(right))
	return sb.String()
}

// padTo pads s with trailing spaces to exactly w display columns.
// Renderers pre-truncate their content; padding only ever appends.
// / padTo 用尾随空格把 s 补到恰好 w 个显示列。渲染器已预截断内容；
// 补齐只追加。
func padTo(s string, w int) string {
	if d := w - lipgloss.Width(s); d > 0 {
		return s + strings.Repeat(" ", d)
	}
	return s
}

// rowCell wraps one pre-padded content row between the side borders.
// / rowCell 用左右边框字符包住一行已补齐的内容。
func rowCell(padded string) string {
	return stFrame.Render(boxV) + padded + stFrame.Render(boxV)
}

// borderedRows wraps a batch of pre-padded rows with the side borders
// so the lattice's vertical lines run the full frame height.
// / borderedRows 把一批已补齐的行包上左右边框，让 lattice 竖线通高
// 不断。
func borderedRows(rows []string) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = rowCell(r)
	}
	return out
}

// rowTwo wraps two pre-padded cells with the shared middle border.
// / rowTwo 用共享中缝边框包住两个已补齐的格。
func rowTwo(leftPadded, rightPadded string) string {
	return stFrame.Render(boxV) + leftPadded + stFrame.Render(boxV) +
		rightPadded + stFrame.Render(boxV)
}

// cellRows splits a rendered region block into exactly h rows, each
// padded to w display columns; short blocks pad with blank rows so a
// cell always fills its budget (the lattice never reflows).
// / cellRows 把渲染好的区域块切成恰好 h 行，每行补齐到 w 显示列；
// 不足的块用空行补齐，格永远填满预算（lattice 不回流）。
func cellRows(block string, h, w int) []string {
	out := make([]string, 0, h)
	if h <= 0 {
		return out
	}
	rows := []string{}
	if block != "" {
		rows = strings.Split(block, "\n")
	}
	for i := 0; i < h; i++ {
		if i < len(rows) {
			out = append(out, padTo(rows[i], w))
		} else {
			out = append(out, strings.Repeat(" ", w))
		}
	}
	return out
}

// titleRow renders the top lattice border with the title text and
// status chip embedded: ┌ FG-QIMEN … ───── [ CHIP ] ┐. prefix is
// plain text (safely truncatable when the terminal is tiny); chip is
// pre-styled and kept intact at the right end.
// / titleRow 渲染嵌入标题与状态芯片的 lattice 顶边框：┌ FG-QIMEN …
// ───── [ CHIP ] ┐。prefix 是纯文本（极小终端下可安全截断）；chip
// 预先上色，完整保留在右端。
func titleRow(w int, prefix, chip string) string {
	chipW := lipgloss.Width(chip)
	chipExtra := 0
	if chip != "" && w >= chipW+10 {
		chipExtra = 1 + chipW // gap space + chip / 间隔空格 + 芯片
	} else {
		chip = ""
	}
	avail := w - 4 - chipExtra // ┌ + pad + ┐ + pad, minus chip block
	if avail < 0 {
		avail = 0
	}
	prefix = truncate(prefix, avail)
	fill := w - 4 - lipgloss.Width(prefix) - chipExtra
	if fill < 0 {
		fill = 0
	}
	var sb strings.Builder
	sb.WriteString(stFrame.Render(boxTL))
	sb.WriteString(" ")
	sb.WriteString(stTitle.Render(prefix))
	if fill > 0 {
		sb.WriteString(strings.Repeat(boxH, fill))
	}
	if chip != "" {
		sb.WriteString(" ")
		sb.WriteString(chip)
	}
	sb.WriteString(" ")
	sb.WriteString(stFrame.Render(boxTR))
	return sb.String()
}
