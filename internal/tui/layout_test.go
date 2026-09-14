// layout_test.go — layout / state-transition tests for the TUI v3
// lattice dashboard. P5.4 (audit roadmap) + spec §5.3 budget contract.
package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/LCUstinian/FG-QiMen/internal/types"
)

// TestUpdate_PromotesRunStateIdleToScanning verifies the state
// machine promotes runIdle → runScanning on the first statsMsg. /
// 验证状态机在首个 statsMsg 把 runIdle 提升为 runScanning。
func TestUpdate_PromotesRunStateIdleToScanning(t *testing.T) {
	m := newTestModel()
	if m.runState != runIdle {
		t.Fatalf("initial runState = %d, want runIdle (%d)", m.runState, runIdle)
	}
	// Dispatch a statsMsg through Update. The dispatcher promotes
	// runIdle → runScanning. / 通过 Update 派发 statsMsg。dispatcher
	// 提升 runIdle → runScanning。
	dispatcher{&m}.Update(statsMsg{view: types.CountersView{}})
	if m.runState != runScanning {
		t.Errorf("after statsMsg: runState = %d, want runScanning (%d)", m.runState, runScanning)
	}
}

// TestUpdate_PromotesScanningToDone verifies doneMsg transitions
// runScanning → runDone and primes the linger countdown. / 验证
// doneMsg 把 runScanning 转 runDone 并启动 linger 倒计时。
func TestUpdate_PromotesScanningToDone(t *testing.T) {
	m := newTestModel()
	m.runState = runScanning
	dispatcher{&m}.Update(doneMsg{})
	if m.runState != runDone {
		t.Errorf("after doneMsg: runState = %d, want runDone (%d)", m.runState, runDone)
	}
	if m.lingerLeft <= 0 {
		t.Errorf("lingerLeft = %d, want > 0 (primed by doneMsg)", m.lingerLeft)
	}
}

// TestWindowSizeMsg_ReValidatesLayout verifies a WindowSizeMsg after
// the model is constructed sets the new width (so pickBreakpoint
// reflects it). P6.4 (audit roadmap). / 验证构造后 WindowSizeMsg 设
// 置新宽度（让 pickBreakpoint 反映）。
func TestWindowSizeMsg_ReValidatesLayout(t *testing.T) {
	m := newTestModel()
	if m.width != 0 {
		t.Fatalf("initial width = %d, want 0", m.width)
	}
	dispatcher{&m}.Update(tea.WindowSizeMsg{Width: 60, Height: 24})
	if m.width != 60 {
		t.Errorf("width = %d after WindowSizeMsg, want 60", m.width)
	}
	if got := pickBreakpoint(m.width); got != BreakNarrow {
		t.Errorf("pickBreakpoint(60) = %d, want BreakNarrow", got)
	}
	dispatcher{&m}.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	if got := pickBreakpoint(m.width); got != BreakWide {
		t.Errorf("pickBreakpoint(120) = %d, want BreakWide", got)
	}
}

// newTestModel builds a Model with safe defaults for tests. /
// newTestModel 用安全默认值构造一个 Model 供测试用。
func newTestModel() Model {
	return Model{
		uiMode:   modeRun,
		runState: runIdle,
	}
}

// TestPickBreakpoint verifies width-to-breakpoint mapping.
// / 验证宽度到 breakpoint 的映射。
func TestPickBreakpoint(t *testing.T) {
	cases := []struct {
		width int
		want  Breakpoint
	}{
		{0, BreakNarrow},
		{40, BreakNarrow},
		{79, BreakNarrow},
		{80, BreakMedium},
		{100, BreakMedium},
		{119, BreakMedium},
		{120, BreakWide},
		{200, BreakWide},
	}
	for _, c := range cases {
		got := pickBreakpoint(c.width)
		if got != c.want {
			t.Errorf("pickBreakpoint(%d) = %d, want %d", c.width, got, c.want)
		}
	}
}

// TestRegionsV2_ExactFill verifies the spec §5.3 budget contract on
// comfortable terminals: budgets are non-negative, footer is always
// 1 row, errors defaults to 1 collapsed row, and the composed frame
// is exactly totalHeight rows (the elastic region absorbs leftovers).
// / 验证舒适终端上 spec §5.3 预算契约：预算非负、footer 恒 1 行、
// errors 默认折叠 1 行，且组合帧恰好 totalHeight 行（弹性区吸收余
// 量）。
func TestRegionsV2_ExactFill(t *testing.T) {
	for _, bp := range []Breakpoint{BreakNarrow, BreakMedium, BreakWide} {
		for _, h := range []int{16, 20, 24, 30, 40, 50} {
			b := regionsV2(bp, h, false)
			if b.progress < 0 || b.events < 0 || b.plugins < 0 || b.header < 1 {
				t.Errorf("regionsV2(%v, %d): bad budget %+v", bp, h, b)
			}
			if b.footer != 1 {
				t.Errorf("regionsV2(%v, %d): footer = %d, want 1 (never cut)", bp, h, b.footer)
			}
			if b.errors != 1 {
				t.Errorf("regionsV2(%v, %d): errors = %d, want 1 (collapsed)", bp, h, b.errors)
			}
			if got := b.frameHeight(bp); got != h {
				t.Errorf("regionsV2(%v, %d): frameHeight = %d, want %d (budget %+v)",
					bp, h, got, h, b)
			}
		}
	}
}

// TestRegionsV2_TinyTerminal verifies budgets stay non-negative below
// the exact-fill floor; the frame may exceed the terminal there and
// the View-layer reconciliation truncates (the documented 兜底).
// / 验证极小终端（低于精确满帧下限）下预算非负；此时帧可能超终端，
// 由 View 层对账截断（即文档化的兜底）。
func TestRegionsV2_TinyTerminal(t *testing.T) {
	for _, bp := range []Breakpoint{BreakNarrow, BreakMedium, BreakWide} {
		b := regionsV2(bp, 12, false)
		if b.progress < 0 || b.events < 0 || b.plugins < 0 || b.header < 1 || b.errors < 1 || b.footer != 1 {
			t.Errorf("regionsV2(%v, 12): bad budget %+v", bp, b)
		}
	}
}

// TestRegionsV2_ErrorsExpanded verifies the expanded ERRORS cell
// costs errExpandedRows and the frame still fills the terminal
// exactly on a comfortable height. / 验证展开态 ERRORS 占
// errExpandedRows 行，且舒适高度下帧仍恰好填满终端。
func TestRegionsV2_ErrorsExpanded(t *testing.T) {
	for _, bp := range []Breakpoint{BreakNarrow, BreakMedium, BreakWide} {
		b := regionsV2(bp, 30, true)
		if b.errors != errExpandedRows {
			t.Errorf("regionsV2(%v, 30, expanded): errors = %d, want %d",
				bp, b.errors, errExpandedRows)
		}
		if got := b.frameHeight(bp); got != 30 {
			t.Errorf("regionsV2(%v, 30, expanded): frameHeight = %d, want 30 (budget %+v)",
				bp, got, b)
		}
	}
}

// TestRegionsV2_ContractionOrder pins the spec §5.3 shrink order on
// medium (plugins → progress → header line 2 → EVENTS floor 3 → 0)
// with hand-computed budgets. / 用手算预算钉住 medium 的 spec §5.3
// 收缩序（plugins → progress → header 第 2 行 → EVENTS 触底 3 → 0）。
func TestRegionsV2_ContractionOrder(t *testing.T) {
	// avail = h - 7 - errors(1). Initial sum = header(2) + progress(6)
	// + events(6) + plugins(4) = 18. After contraction the elastic
	// absorption tops EVENTS back up against the true chrome (a zeroed
	// cell frees its separator row).
	// / avail = h - 7 - errors(1)。初始 sum = 2+6+6+4 = 18。收缩后弹
	// 性吸收按真实 chrome 把 EVENTS 补回（被清零的格腾出自己的分隔
	// 线行）。
	cases := []struct {
		height                            int
		header, progress, events, plugins int
	}{
		// avail=16: plugins 4→2, everything else intact, no leftover.
		{24, 2, 6, 6, 2},
		// avail=12: plugins→0 frees a separator row; EVENTS absorbs it
		// (6+1=7).
		{20, 2, 4, 7, 0},
		// avail=11: progress→4 forces header 2→1 (drop rate line);
		// EVENTS absorbs the plugins row (6+1=7).
		{19, 1, 4, 7, 0},
		// avail=8: EVENTS stops at the floor of 3, then absorbs the
		// two freed rows (3+1=4).
		{16, 1, 4, 4, 0},
		// avail=5: EVENTS gives up the floor entirely as the last
		// resort; a zeroed EVENTS cell absorbs nothing.
		{13, 1, 4, 0, 0},
	}
	for _, c := range cases {
		b := regionsV2(BreakMedium, c.height, false)
		if b.header != c.header || b.progress != c.progress ||
			b.events != c.events || b.plugins != c.plugins {
			t.Errorf("regionsV2(medium, %d) = %+v, want header=%d progress=%d events=%d plugins=%d",
				c.height, b, c.header, c.progress, c.events, c.plugins)
		}
	}
}

// TestRegionsV2_BreakpointShapes pins the per-breakpoint defaults:
// wide fills the right column from the body budget, narrow hides
// TOP PLUGINS and LIVE EVENTS and hands the body to PROGRESS.
// / 钉住各断点的默认形态：wide 右列吃满 body 预算，narrow 隐藏
// TOP PLUGINS 与 LIVE EVENTS、主体交给 PROGRESS。
func TestRegionsV2_BreakpointShapes(t *testing.T) {
	// Wide h=40: body = 40 - 7 - header(2) - errors(1) = 30 → right
	// column 30 rows; left column keeps PROGRESS 6 + TOP PLUGINS 6.
	b := regionsV2(BreakWide, 40, false)
	if b.events != 30 || b.progress != 6 || b.plugins != 6 || b.header != 2 {
		t.Errorf("regionsV2(wide, 40) = %+v, want events=30 progress=6 plugins=6 header=2", b)
	}

	// Narrow h=24: no plugins, no events, header drops to 1, PROGRESS
	// takes avail = 24 - 5 - errors(1) - header(1) = 17.
	b = regionsV2(BreakNarrow, 24, false)
	if b.plugins != 0 || b.events != 0 || b.header != 1 || b.progress != 17 {
		t.Errorf("regionsV2(narrow, 24) = %+v, want plugins=0 events=0 header=1 progress=17", b)
	}
}
