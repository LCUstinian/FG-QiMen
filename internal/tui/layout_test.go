// layout_test.go — additional layout / state-transition tests for
// the dashboard. P5.4 (audit roadmap).
package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/LCUstinian/FG-QiMen/internal/types"
)

// TestTwoColumn_NarrowWidthCollapses verifies the layout collapses to
// a single column when the terminal is narrower than minWidth. /
// 验证终端宽度低于 minWidth 时布局塌缩为单列。
func TestTwoColumn_NarrowWidthCollapses(t *testing.T) {
	m := newTestModel()
	m.width = 79 // one below minWidth=80
	if m.twoColumn() {
		t.Errorf("twoColumn() = true at width 79, want false (below minWidth=80)")
	}
	m.width = 80
	if !m.twoColumn() {
		t.Errorf("twoColumn() = false at width 80, want true (at minWidth=80)")
	}
	m.width = 200
	if !m.twoColumn() {
		t.Errorf("twoColumn() = false at width 200, want true")
	}
}

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
// the model is constructed sets the new width (so twoColumn() reflects
// it). P6.4 (audit roadmap). / 验证构造后 WindowSizeMsg 设置新宽度
// （让 twoColumn() 反映）。
func TestWindowSizeMsg_ReValidatesLayout(t *testing.T) {
	m := newTestModel()
	if m.width != 0 {
		t.Fatalf("initial width = %d, want 0", m.width)
	}
	dispatcher{&m}.Update(tea.WindowSizeMsg{Width: 60, Height: 24})
	if m.width != 60 {
		t.Errorf("width = %d after WindowSizeMsg, want 60", m.width)
	}
	if m.twoColumn() {
		t.Errorf("twoColumn() = true at width 60, want false")
	}
	dispatcher{&m}.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	if !m.twoColumn() {
		t.Errorf("twoColumn() = false at width 120, want true")
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
