// layout.go — pure breakpoint + region placement helpers for the
// v0.7.0 TUI v2 Spec B layout. No state, no I/O; trivial to test.
//
// layout.go — v0.7.0 TUI v2 Spec B 布局的纯 breakpoint + 区域放置
// 辅助函数。无 state、无 I/O；易于测试。
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

// regions returns the height in lines for each of 6 regions given
// breakpoint and terminal dimensions. Pure function.
//
// Chrome rows reserved up-front: title bar 2 (title + separator),
// header 2 (stage badge line + rate line; the rate line may be
// empty in warm-up but reserving it keeps the accounting stable),
// errors 1 (collapsed), footer 1. Body (events + leftCol + rightCol)
// gets whatever's left, with a minimum of 4 to keep STAGE + TOP
// PLUGINS usable. The events budget is capped to the body so tiny
// terminals never allocate an events panel that can't fit.
//
// / regions 给定 breakpoint 和终端尺寸，返回 6 个区域的行高。纯
// 函数。chrome 行预先保留：标题栏 2（title + 分隔线）、header 2
// （stage badge 行 + rate 行；rate 行热身时可能为空，但保留它让
// 记账稳定）、errors 1（折叠）、footer 1。body（events + 左列 +
// 右列）拿剩下的，保底 4 行让 STAGE + TOP PLUGINS 可用。events
// 预算被钳到 body 内，极小终端不会分出装不下的 events 面板。
//
// Returned order: header, events, leftCol, rightCol, errors, footer.
//
//nolint:gocritic // 6-tuple is part of the public contract (Spec B Task 1)
func regions(bp Breakpoint, width, totalHeight int) (int, int, int, int, int, int) {
	const chrome = 6 // title 2 + header 2 + errors 1 + footer 1
	header := 2
	errors := 1
	footer := 1

	body := totalHeight - chrome
	if body < 4 {
		body = 4 // minimum body for stage + top-plugins
	}

	var events, leftCol, rightCol int
	switch bp {
	case BreakNarrow:
		events = 0 // hidden; 'L' overlay shows last 5
		leftCol = body / 2
		rightCol = body - leftCol
	case BreakMedium:
		events = 8
		leftCol = body / 2
		rightCol = body - leftCol
	case BreakWide:
		events = 12
		leftCol = body
		rightCol = body
	}
	if events > body {
		events = body
	}
	return header, events, leftCol, rightCol, errors, footer
}
