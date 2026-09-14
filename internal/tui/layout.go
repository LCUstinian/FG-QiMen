// layout.go — pure breakpoint + region budget helpers for the TUI v3
// lattice layout (spec §5.3). No state, no I/O; trivial to test.
//
// layout.go — TUI v3 lattice 布局的纯 breakpoint + 区域预算辅助
// （spec §5.3）。无 state、无 I/O；易于测试。
package tui

// Breakpoint is a width category for the responsive layout.
// / Breakpoint 是响应式布局的宽度分类。
type Breakpoint int

const (
	BreakNarrow Breakpoint = iota // <80 cols
	BreakMedium                   // 80-119 cols
	BreakWide                     // >=120 cols
)

// pickBreakpoint maps terminal width to a Breakpoint.
// / pickBreakpoint 把终端宽度映射到 Breakpoint。
func pickBreakpoint(width int) Breakpoint {
	switch {
	case width < 80:
		return BreakNarrow
	case width < 120:
		return BreakMedium
	default:
		return BreakWide
	}
}

// errExpandedRows is the ERRORS budget when expanded: up to 4 top
// category bar rows (the collapsed line doubles as the header, so the
// expanded cell has no separate title row).
// / errExpandedRows 是展开态的 ERRORS 预算：最多 4 行 top 类别 bar
// （折叠行兼任标题，展开格没有独立标题行）。
const errExpandedRows = 4

// regionBudget carries the row budget of each lattice region for one
// frame. Rows INCLUDE each panel's title row.
// / regionBudget 携带一帧中每个 lattice 区域的行预算。行数含各面板
// 的标题行。
type regionBudget struct {
	header   int // header cell: stage badge (+ rate line)
	events   int // LIVE EVENTS cell
	progress int // PROGRESS cell
	plugins  int // TOP PLUGINS cell
	errors   int // ERRORS rows (1 collapsed / 4 expanded)
	footer   int // footer rows (always 1)
}

// sum returns the variable body rows (header + content cells) whose
// total must fit the height left after fixed chrome on medium.
// / sum 返回可变主体行数（header + 内容格），在 medium 上必须塞进
// 固定 chrome 之外的高度。
func (b regionBudget) sum() int {
	return b.header + b.progress + b.events + b.plugins
}

// frameHeight returns the exact number of terminal rows the composed
// frame occupies for these budgets — the number regionsV2's
// contraction loops drive toward totalHeight and the golden guards
// assert on.
// / frameHeight 返回这些预算下组合帧占用的终端行数——regionsV2 的
// 收缩循环把它推向 totalHeight，golden 守卫对它做断言。
func (b regionBudget) frameHeight(bp Breakpoint) int {
	rows := 1 + b.header + 1 // title border + header cell + sep
	switch bp {
	case BreakWide:
		// body-top ┬ sep + body (right column; left pads) + body-bottom
		// ┴ sep + errors + sep + footer (inside) + bottom.
		// / 主体上 ┬ 分隔 + 主体（右列；左列补白）+ 主体下 ┴ 分隔 +
		// errors + 分隔 + footer（框内）+ 底边。
		rows += 1 + b.events + 1 + b.errors + 1 + b.footer + 1
	case BreakMedium:
		if b.progress > 0 {
			rows += b.progress + 1
		}
		if b.events > 0 {
			rows += b.events + 1
		}
		if b.plugins > 0 {
			rows += b.plugins + 1
		}
		rows += b.errors + 1 + b.footer // errors + bottom + footer(outside)
	case BreakNarrow:
		if b.progress > 0 {
			rows += b.progress + 1
		}
		if b.events > 0 {
			rows += b.events + 1
		}
		rows += b.errors + 1 + b.footer
	}
	return rows
}

// regionsV2 computes region row budgets for a frame on a terminal of
// totalHeight rows. Pure function; implements the spec §5.3 contract:
//
//	保底: EVENTS ≥ 3 rows when height allows; the footer is never
//	      cut; the title border row is never cut.
//	收缩序: TOP PLUGINS rows → PROGRESS rows → header line 2 →
//	        EVENTS down to the floor of 3.
//	兜底: the View-layer height reconciliation hard-truncates
//	      (contract unchanged).
//
// The leftover height on tall terminals is absorbed by the elastic
// region of each breakpoint (medium: EVENTS; wide: it pads the left
// column; narrow: PROGRESS) so the frame fills the terminal exactly.
//
// Deviation from spec §5.3: the width parameter is dropped — budgets
// are height-only; column geometry lives in frame.go (wideSplit).
//
// / regionsV2 计算终端总高 totalHeight 下各区域的行预算。纯函数；
// 实现 spec §5.3 契约：
//
//	保底：EVENTS ≥ 3 行（宽高允许时）；footer 永不裁；标题行 1 永不裁。
//	收缩序：TOP PLUGINS 行数 → PROGRESS 行数 → header 第 2 行 →
//	        EVENTS 触底 3。
//	兜底：View 层高度对账硬截断（契约不变）。
//
// 高终端的剩余高度由各断点的弹性区吸收（medium: EVENTS；wide: 左列
// 补白；narrow: PROGRESS），帧恰好填满终端。
//
// 与 spec §5.3 的偏差：去掉 width 参数——预算只与高度相关；列几何
// 在 frame.go（wideSplit）。
//
//nolint:gocritic // bp-first signature mirrors the spec name
func regionsV2(bp Breakpoint, totalHeight int, errorsExpanded bool) regionBudget {
	b := regionBudget{header: 2, footer: 1}
	if errorsExpanded {
		b.errors = errExpandedRows
	} else {
		b.errors = 1
	}

	switch bp {
	case BreakWide:
		// Fixed chrome: title 1 + sepA 1 + body-top ┬ 1 + body-bottom ┴ 1
		// + sepD 1 + footer(inside) 1 + bottom 1 = 7.
		// / 固定 chrome：标题 1 + sepA 1 + 主体上 ┬ 1 + 主体下 ┴ 1 +
		// sepD 1 + footer（框内）1 + 底边 1 = 7。
		body := totalHeight - 7 - b.header - b.errors
		if body < 4 {
			body = 4
		}
		// Desired left column: PROGRESS 6 + blank 1 + TOP PLUGINS 6.
		// Contract it first (plugins → progress) to fit the body.
		// / 左列期望：PROGRESS 6 + 空行 1 + TOP PLUGINS 6。先按收缩序
		// （plugins → progress）压进 body。
		b.progress, b.plugins = 6, 6
		for b.plugins > 2 && b.progress+1+b.plugins > body {
			b.plugins--
		}
		for b.progress > 4 && b.progress+1+b.plugins > body {
			b.progress--
		}
		b.events = body // right column fills the body height

	case BreakMedium:
		// Fixed chrome: title 1 + 4 seps + bottom 1 + footer(outside)
		// 1 = 7. / 固定 chrome：标题 1 + 分隔 4 + 底边 1 + footer
		// （框外）1 = 7。
		avail := totalHeight - 7 - b.errors
		if avail < 5 {
			avail = 5
		}
		b.progress, b.events, b.plugins = 6, 6, 4
		// Shrink order (spec §5.3): plugins → progress → header line
		// 2 → EVENTS floor 3 → EVENTS to 0 as the last resort.
		// / 收缩序（spec §5.3）：plugins → progress → header 第 2 行
		// → EVENTS 触底 3 → 走投无路再降到 0。
		for b.plugins > 0 && b.sum() > avail {
			b.plugins--
		}
		for b.progress > 4 && b.sum() > avail {
			b.progress--
		}
		if b.sum() > avail {
			b.header = 1 // drop the rate line / 丢掉 rate 行
		}
		for b.events > 3 && b.sum() > avail {
			b.events--
		}
		for b.events > 0 && b.sum() > avail {
			b.events--
		}
		// EVENTS is the elastic region: absorb leftovers so the frame
		// fills the terminal exactly. The leftover is computed against
		// the TRUE chrome — a cell zeroed by contraction drops its own
		// separator, freeing a row. Only absorbs into a live EVENTS
		// cell; resurrecting events==0 would re-add its separator and
		// overflow the frame.
		// / EVENTS 是弹性区：吸收余量让帧恰好填满终端。余量按真实
		// chrome 计算——被收缩清零的格连自己的分隔线一起消失，腾出
		// 一行。只向存活的 EVENTS 格吸收；把 events==0 复活会重新带
		// 入分隔线，导致超帧。
		if b.events > 0 {
			chrome := 4 // title + sepA + bottom + footer(outside)
			if b.progress > 0 {
				chrome++
			}
			if b.events > 0 {
				chrome++
			}
			if b.plugins > 0 {
				chrome++
			}
			if target := totalHeight - chrome - b.errors; b.sum() < target {
				b.events += target - b.sum()
			}
		}

	case BreakNarrow:
		// Narrow hides TOP PLUGINS and LIVE EVENTS (spec §5.2); the
		// 'L' overlay re-allocates in View. PROGRESS absorbs the body.
		// / narrow 隐藏 TOP PLUGINS 与 LIVE EVENTS（spec §5.2）；'L'
		// overlay 在 View 里重新分配。PROGRESS 吸收主体。
		b.header = 1
		b.plugins = 0
		b.events = 0
		// Fixed chrome constants: title 1 + sep 1 + sep 1 + bottom 1 +
		// footer 1 = 5 (the hidden events cell adds no separator).
		// / 固定 chrome 常数：标题 1 + 分隔 2 + 底边 1 + footer 1 = 5
		// （隐藏的 events 格不产生分隔线）。
		avail := totalHeight - 5 - b.errors - b.header
		if avail < 3 {
			avail = 3
		}
		b.progress = avail
	}
	return b
}
