// program_test.go — unit tests for the Bubbletea dispatcher and
// Program wrapper. We test the dispatcher directly (same-package
// access) and the idempotency of Program.Done().
//
// We do NOT exercise tea.NewProgram's Run() here — that touches the
// real terminal and would hang in `go test`. The Run-loop contract
// is covered indirectly: the integration is a small wrapper, and the
// dispatcher (which is the only state machine) is fully exercised.
package tui

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/LCUstinian/FG-QiMen/internal/types"
)

// TestDispatcherStatsMsg — a statsMsg updates the model's counters
// and elapsed fields, and returns a nil cmd (no follow-up work).
//
// TestDispatcherStatsMsg — statsMsg 更新 model 的 counters 和 elapsed
// 字段，返回 nil cmd（无后续工作）。
func TestDispatcherStatsMsg(t *testing.T) {
	m := NewModel(nil)
	d := dispatcher{inner: &m}
	view := types.CountersView{Alive: 7, Ports: 12, Results: 3, Creds: 1, Errors: 0}
	newM, cmd := d.Update(statsMsg{view: view, elapsed: "5s"})
	if cmd != nil {
		t.Errorf("statsMsg returned non-nil cmd: %v", cmd)
	}
	dd := newM.(dispatcher)
	if dd.inner.counters != view {
		t.Errorf("counters = %+v, want %+v", dd.inner.counters, view)
	}
	if dd.inner.elapsed != "5s" {
		t.Errorf("elapsed = %q, want %q", dd.inner.elapsed, "5s")
	}
}

// TestDispatcherEventMsg — eventMsg flows into the v0.7.0 event
// ring buffer. The contract is "never grow past eventCap": we send
// 5×eventCap events and assert the ring holds exactly eventCap
// entries with the newest sequence preserved (wrap keeps
// chronological order, newest last).
//
// Note: the dispatcher's eventMsg case mutates d.inner directly
// through the shared *Model pointer, so the model can be inspected
// after the loop without threading return values.
//
// TestDispatcherEventMsg — eventMsg 流入 v0.7.0 事件 ring buffer。
// 契约是"永远不超过 eventCap"：发 5×eventCap 条事件，断言 ring
// 恰好容纳 eventCap 条，且最新序列保留（回绕保持时间顺序，最新
// 在末尾）。
//
// 注意：dispatcher 的 eventMsg 分支通过共享的 *Model 指针直接改
// d.inner，所以循环结束后可以直接检查 model，无需串联返回值。
func TestDispatcherEventMsg(t *testing.T) {
	mm := NewModel(nil)
	m := tea.Model(dispatcher{inner: &mm})
	for i := 0; i < eventCap*5; i++ {
		ev := eventMsg{
			when: "12:00:00", tag: "scan",
			host: fmt.Sprintf("10.0.0.%d", i), port: 22, svc: "ssh",
			text: "OpenSSH 9.0",
		}
		m, _ = m.Update(ev)
	}
	got := mm.eventsOrdered()
	if len(got) != eventCap {
		t.Fatalf("len(eventsOrdered) = %d, want %d (ring cap)", len(got), eventCap)
	}
	// After 5×eventCap pushes the ring wrapped 4 times; the last
	// entry must be the newest host (10.0.0.<eventCap*5-1>).
	// 发 5×eventCap 条后 ring 回绕 4 次；最后一条必须是最新的
	// host（10.0.0.<eventCap*5-1>）。
	wantHost := fmt.Sprintf("10.0.0.%d", eventCap*5-1)
	if last := got[len(got)-1]; last.Host != wantHost {
		t.Errorf("last event host = %q, want %q", last.Host, wantHost)
	}
	// scan tags map to the "hit" kind. / scan 标签映射为 "hit" kind。
	if last := got[len(got)-1]; last.Kind != "hit" {
		t.Errorf("last event kind = %q, want %q", last.Kind, "hit")
	}
}

// TestDispatcherDoneMsg — doneMsg sets the final summary, flips
// runState to runDone, primes the linger countdown, and returns
// nil cmd. The actual quit is fired by the model from its
// tickMsg handler after `lingerTicks` frames (so the dashboard
// can show the final summary inside the TUI frame long enough
// to read). We verify the contract is "ready to linger" rather
// than "already quit".
//
// TestDispatcherDoneMsg — doneMsg 设置最终摘要、把 runState 翻
// 为 runDone、启动 linger 倒计时、返回 nil cmd。真正退出由 model
// 在 `lingerTicks` 帧后从自己的 tickMsg 处理器触发（让 dashboard
// 在 TUI 框内显示最终摘要够久可读）。我们验证契约是"准备 linger"
// 而非"已退出"。
func TestDispatcherDoneMsg(t *testing.T) {
	m := NewModel(nil)
	d := dispatcher{inner: &m}
	newM, cmd := d.Update(doneMsg{summary: "scan complete: 1 cred"})
	if cmd != nil {
		t.Errorf("doneMsg returned non-nil cmd: %v (want nil; quit comes from tickMsg)", cmd)
	}
	dd := newM.(dispatcher)
	if dd.inner.finalSummary != "scan complete: 1 cred" {
		t.Errorf("finalSummary = %q", dd.inner.finalSummary)
	}
	if dd.inner.runState != runDone {
		t.Errorf("runState = %d, want runDone (%d)", dd.inner.runState, runDone)
	}
	if dd.inner.lingerLeft != lingerTicks {
		t.Errorf("lingerLeft = %d, want %d", dd.inner.lingerLeft, lingerTicks)
	}
	if dd.inner.quitting {
		t.Error("quitting should be false right after doneMsg; quit fires from tickMsg")
	}
}

// TestModelLingerExits — drives the model through a full linger
// cycle via tickMsg and asserts it quits when the countdown
// reaches zero. The model is value-typed, so we thread the
// returned model through the loop (same pattern as
// TestDispatcherEventMsg).
//
// TestModelLingerExits — 通过 tickMsg 驱动 model 走完一个完整
// linger 周期，断言倒计时归零时 model 退出。Model 是值类型，所
// 以循环里把返回的 model 串联下去（同 TestDispatcherEventMsg）。
func TestModelLingerExits(t *testing.T) {
	m := NewModel(nil)
	m.runState = runDone
	m.lingerLeft = 3
	// First two ticks: lingerLeft decrements, no quit.
	// 前两 tick：lingerLeft 递减，不退出。
	for i := 0; i < 2; i++ {
		newM, _ := m.Update(tickMsg(time.Time{}))
		m = *newM.(*Model)
		if m.quitting {
			t.Fatalf("tick %d: quitting = true, want false (lingerLeft=%d)", i, m.lingerLeft)
		}
	}
	// Third tick: lingerLeft hits 0, model quits and returns
	// tea.Quit. We invoke the cmd and type-assert the result to
	// QuitMsg (functions aren't comparable in Go, only to nil).
	// 第三 tick：lingerLeft 归零，model 退出并返回 tea.Quit。
	// 调用 cmd 并对结果做 QuitMsg 类型断言（Go 里 func 不能互比，
	// 只能与 nil 比）。
	_, cmd := m.Update(tickMsg(time.Time{}))
	if cmd == nil {
		t.Fatal("third tick returned nil cmd, want tea.Quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Errorf("third tick cmd() did not return tea.QuitMsg; got %T", cmd())
	}
}

// TestModelTickAdvancesSpinner — the tickMsg handler advances
// frameIdx modulo len(spinnerFrames); after N ticks the frame
// should have advanced by N.
//
// TestModelTickAdvancesSpinner — tickMsg 处理器把 frameIdx 按
// len(spinnerFrames) 取模推进；N 次 tick 后 frame 应推进 N。
func TestModelTickAdvancesSpinner(t *testing.T) {
	m := NewModel(nil)
	// Pre-seed runState = runScanning so the linger path doesn't
	// fire (lingerLeft is 0 in runScanning, which is the no-op
	// branch). The spinner still advances either way.
	// 预置 runState = runScanning 避免走 linger 路径（runScanning
	// 下 lingerLeft 为 0 走 no-op 分支）。spinner 无论如何都推
	// 进。
	m.runState = runScanning
	before := m.frameIdx
	// Thread the returned model through: Model.Update is a pointer
	// receiver (v0.5.2) — direct mutation, but we still rebind so
	// the assertion reads from the post-Update copy.
	// 串联返回的 model：Model.Update 是指针接收者（v0.5.2）——
	// 直接变更，但仍重绑让断言读到 Update 后的副本。
	newM, _ := m.Update(tickMsg(time.Time{}))
	m = *newM.(*Model)
	if m.frameIdx != (before+1)%len(spinnerFrames) {
		t.Errorf("frameIdx = %d, want %d", m.frameIdx, (before+1)%len(spinnerFrames))
	}
}

// TestDispatcherFallthrough — non-custom messages (WindowSizeMsg,
// KeyMsg) are routed to the inner Model. We verify by sending a
// WindowSizeMsg and checking that width/height get set.
//
// TestDispatcherFallthrough — 非自定义消息（WindowSizeMsg、KeyMsg）
// 透传到底层 Model。通过发 WindowSizeMsg 验证 width/height 被设置。
func TestDispatcherFallthrough(t *testing.T) {
	m := NewModel(nil)
	d := dispatcher{inner: &m}
	newM, _ := d.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	dd := newM.(dispatcher)
	if dd.inner.width != 120 || dd.inner.height != 40 {
		t.Errorf("width/height = %d/%d, want 120/40", dd.inner.width, dd.inner.height)
	}
}

// TestDispatcherViewDelegates — View() returns the inner model's
// rendering; we don't assert the exact string (lipgloss styling
// is environment-dependent) but the dispatcher's View must be
// non-empty and not panic on a fresh model.
//
// TestDispatcherViewDelegates — View() 返回内部 model 的渲染；不
// 断言确切字符串（lipgloss 样式随环境变），但 dispatcher.View 必须
// 在新 model 上非空且不 panic。
func TestDispatcherViewDelegates(t *testing.T) {
	m := NewModel(nil)
	d := dispatcher{inner: &m}
	v := d.View()
	if v == "" {
		t.Error("View() returned empty string on fresh model")
	}
}

// TestNewProgramDoesNotStartRun — NewProgram must construct without
// touching the terminal. We can't actually call Quit or Run here
// (bubble tea's Send blocks until a consumer reads from the
// program's message channel, and there's no consumer until Run()
// is invoked), so the assertion is just that the constructor
// returns a non-nil Program without panicking.
//
// TestNewProgramDoesNotStartRun — NewProgram 必须能在不碰终端的
// 情况下构造。这里不能真去调 Quit 或 Run（bubbletea 的 Send 会阻
// 塞直到 program 消息通道有消费者，而 Run() 没启动就没有消费者），
// 所以只断言构造函数返回非 nil 且不 panic。
func TestNewProgramDoesNotStartRun(t *testing.T) {
	p := NewProgram(nil)
	if p == nil {
		t.Fatal("NewProgram(nil) returned nil")
	}
}

// TestNewProgramPopulatesRan — NewProgram records start time; we
// verify the field is non-zero and recent.
//
// TestNewProgramPopulatesRan — NewProgram 记录启动时间；验证字段非
// 零且是最近的时间。
func TestNewProgramPopulatesRan(t *testing.T) {
	before := time.Now()
	p := NewProgram(nil)
	after := time.Now()
	if p.ran.Before(before) || p.ran.After(after) {
		t.Errorf("ran = %v, want in [%v, %v]", p.ran, before, after)
	}
}

// TestProgramDoneIdempotent — scanner.go calls Done() from both the
// success and the early-error paths (scanner.go:69, :163). A second
// Done() that arrives while bubbletea is exiting must not panic on
// the closed send channel. We simulate this by calling Done twice
// in rapid succession; both must complete without panic.
//
// TestProgramDoneIdempotent — scanner.go 在成功和早错路径都会调
// Done()（scanner.go:69、:163）。第二次 Done() 到达时 bubbletea 正
// 在退出，不能因为 send 通道已关闭而 panic。模拟：连发两次 Done()，
// 都得正常返回。
func TestProgramDoneIdempotent(t *testing.T) {
	p := NewProgram(nil)
	// Note: we don't run the bubbletea program — Done() buffers into
	// the program's send channel. Without a Run() the channel is
	// unread but unbounded up to its buffer, so the first send
	// succeeds. The second send either buffers or hits a closed
	// channel. We don't care which — the once-guard should make the
	// function return without sending in the second case.
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Done() panicked on second call: %v", r)
		}
	}()
	p.Done("summary 1")
	p.Done("summary 2 — must be no-op")
	// doneOnce is private; we verify the contract by relying on
	// the absence of panic. A direct field check would be nicer
	// but the guard is the only state we expose.
}

// TestProgramDoneConcurrentSafe — Done() must be safe to call from
// multiple goroutines (e.g. if the cred scheduler and the pipeline
// race to fire Done during a hard exit). This is a smoke test; a
// real race detector run (go test -race) gives a stronger guarantee.
//
// TestProgramDoneConcurrentSafe — Done() 必须可并发调用（例如凭据
// 调度器和 pipeline 在硬退出时抢着发 Done）。这是烟雾测试；用
// `go test -race` 可获得更强保证。
func TestProgramDoneConcurrentSafe(t *testing.T) {
	p := NewProgram(nil)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("concurrent Done() panicked: %v", r)
				}
			}()
			p.Done("summary")
		}(i)
	}
	wg.Wait()
}

// TestProgramBannerNoOp — Banner is a no-op for the TUI; it must
// not panic on a nil cfg (the dispatcher renders its own banner).
//
// TestProgramBannerNoOp — Banner 对 TUI 是空操作；nil cfg 也不能
// panic（dispatcher 自行渲染 banner）。
func TestProgramBannerNoOp(t *testing.T) {
	p := NewProgram(nil)
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Banner(nil) panicked: %v", r)
		}
	}()
	p.Banner(nil)
}

// TestProgramStatsNilSafe — Stats on a nil state must be a silent
// no-op, not a panic. The pipeline tick fires on a 1-second cadence
// and could plausibly race with shutdown.
//
// TestProgramStatsNilSafe — Stats 收到 nil state 必须是静默空操作，
// 不能 panic。pipeline 1秒滴答可能在 shutdown 时和它赛跑。
func TestProgramStatsNilSafe(t *testing.T) {
	p := NewProgram(nil)
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Stats(nil) panicked: %v", r)
		}
	}()
	p.Stats(nil)
}

// TestProgramEventNilSafe — same defensive contract for Event.
//
// TestProgramEventNilSafe — Event 同等防御契约。
func TestProgramEventNilSafe(t *testing.T) {
	p := NewProgram(nil)
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Event(nil) panicked: %v", r)
		}
	}()
	p.Event(nil)
}

// TestProgramCredFoundNilSafe — CredFound with nil result or nil
// cred must be a no-op (the real path checks both).
//
// TestProgramCredFoundNilSafe — CredFound 对 nil result 或 nil cred
// 必须是空操作（真实路径会检查两者）。
func TestProgramCredFoundNilSafe(t *testing.T) {
	p := NewProgram(nil)
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("CredFound(nil) panicked: %v", r)
		}
	}()
	p.CredFound(nil)
	p.CredFound(&types.Result{Host: "h", Cred: nil})
}

// ─────────────────────────────────────────────────────────────────────
// v0.5.2 TUI info-density panels (Task 4 of TUI v2 Spec A)
// v0.5.2 TUI 信息密度面板（TUI v2 Spec A Task 4）
// ─────────────────────────────────────────────────────────────────────

// newTestState returns a fresh *types.State for tests. We use a
// direct State (not a *session.Session) because the session package
// imports internal/ui, which in turn imports internal/tui — adding
// session back here would form an import cycle. The Model only needs
// the State for PluginHitsView / ErrorCategoriesView, so a direct
// *types.State is the minimum-surface wiring.
// newTestState 为测试返回一个新的 *types.State。我们直接用 State
// （而非 *session.Session），因为 session 包导入了 internal/ui，而
// ui 又导 internal/tui —— 把 session 引回来会形成 import 环。Model
// 只需要 State 的 PluginHitsView / ErrorCategoriesView，所以直接用
// *types.State 是最小面积的接线。
func newTestState(t *testing.T) *types.State {
	t.Helper()
	return types.NewState()
}

// newTestModelWithState returns a Model wired with the given State,
// suitable for tests that need to inspect the rate / topN / ETA
// fields after pushing statsMsg deltas. Mirrors the production
// constructor's session plumbing — see NewProgram in program.go for
// the wiring site.
//
// Renamed from newTestModel to avoid a clash with layout_test.go's
// no-arg newTestModel (the layout helper returns a fresh Model with
// safe defaults; this one wires a State for the v0.5.2 panels).
//
// newTestModelWithState 返回一个接上 State 的 Model，给需要查看
// statsMsg delta 后 rate / topN / ETA 字段的测试用。镜像生产构造
// 函数的 session 接线——接线点见 program.go 的 NewProgram。
//
// 改名为 newTestModelWithState 是为了不和 layout_test.go 的无参
// newTestModel 冲突（layout 助手返回带安全默认值的空 Model；这个
// 给 v0.5.2 面板接上 State）。
func newTestModelWithState(s *types.State) *Model {
	m := newModelWithState(nil, s)
	return &m
}

// TestDispatcher_RendersRateAndPlugins verifies that after multiple
// statsMsg deltas, the model's rate fields stabilise (EWMA) and
// topPlugins / topErrors reflect the State view methods.
// TestDispatcher_RendersRateAndPlugins 验证多次 statsMsg delta 后，
// model 的 rate 字段稳定（EWMA），topPlugins/topErrors 反映 State
// 的 view methods。
func TestDispatcher_RendersRateAndPlugins(t *testing.T) {
	st := newTestState(t)
	// Pretend 5 plugins hit with these counts.
	type kv struct {
		name string
		n    int64
	}
	seed := []kv{{"ssh", 47}, {"redis", 21}, {"mysql", 12}, {"http", 9}, {"postgres", 5}}
	for _, k := range seed {
		v, _ := st.PluginHits.LoadOrStore(k.name, &atomic.Int64{})
		v.(*atomic.Int64).Store(k.n)
	}

	// Send 6 statsMsg deltas with monotonically increasing
	// Creds and Ports (so rate is non-zero).
	m := newTestModelWithState(st)
	base := time.Now()
	for i := 0; i < 6; i++ {
		msg := statsMsg{
			view: types.CountersView{
				Alive: 12, AliveProbed: 256, Ports: int64(80 * (i + 1)),
				Results: int64(5 * (i + 1)), Creds: int64(2 * (i + 1)),
				Errors: 3, Stage: int64(types.StageIdentify),
			},
			elapsed: (time.Duration(i+1) * time.Second).String(),
			when:    base.Add(time.Duration(i) * time.Second),
		}
		// Thread the returned dispatcher so the mutation applies
		// (Model.Update is a value receiver — see tui.go).
		// 串联返回的 dispatcher 让变更生效（Model.Update 是值接
		// 收者——见 tui.go）。
		newM, _ := dispatcher{inner: m}.Update(msg)
		*m = *newM.(dispatcher).inner
	}

	// After 6 ticks, rate fields should be non-zero (since
	// Creds and Ports each incremented by 12 / 80 * tick).
	if got := m.rateHits; got <= 0 {
		t.Errorf("rateHits = %v, want > 0 after 6 deltas", got)
	}
	if got := m.ratePorts; got <= 0 {
		t.Errorf("ratePorts = %v, want > 0 after 6 deltas", got)
	}
	// topPlugins: top 5 by count = full seed list (sorted desc).
	if len(m.topPlugins) != 5 {
		t.Errorf("topPlugins len = %d, want 5", len(m.topPlugins))
	}
	if m.topPlugins[0][0] != "ssh" {
		t.Errorf("topPlugins[0] = %q, want ssh", m.topPlugins[0][0])
	}
	// topErrors: empty (no errors seeded).
	if len(m.topErrors) != 0 {
		t.Errorf("topErrors len = %d, want 0", len(m.topErrors))
	}
}

// TestDispatcher_RateEmptyState verifies placeholders.
// TestDispatcher_RateEmptyState 验证空态占位符。
func TestDispatcher_RateEmptyState(t *testing.T) {
	st := newTestState(t)
	m := newTestModelWithState(st)
	msg := statsMsg{
		view: types.CountersView{Stage: int64(types.StageAlive)},
		when: time.Now(),
	}
	newM, _ := dispatcher{inner: m}.Update(msg)
	*m = *newM.(dispatcher).inner
	// topPlugins / topErrors should be nil/empty so View() renders
	// "(no hits yet)" / "(no errors yet)".
	// topPlugins/topErrors 应为空以便 View() 渲染占位符。
	if len(m.topPlugins) != 0 {
		t.Errorf("topPlugins = %v, want empty", m.topPlugins)
	}
	if len(m.topErrors) != 0 {
		t.Errorf("topErrors = %v, want empty", m.topErrors)
	}
	// View() string should contain the placeholders. v0.7.0: the
	// errors row collapsed format is "ERRORS: (none)" (viewErrors).
	// View() 字符串应包含占位符。v0.7.0：errors 行折叠格式为
	// "ERRORS: (none)"（viewErrors）。
	v := m.View()
	if !strings.Contains(v, "(no hits yet)") {
		t.Errorf("View missing '(no hits yet)' placeholder: %q", v)
	}
	if !strings.Contains(v, "ERRORS: (none)") {
		t.Errorf("View missing 'ERRORS: (none)' placeholder: %q", v)
	}
}

// TestDispatcher_E2E_StateWiring exercises the production wiring
// path: a model constructed by NewProgram (no State) receives a
// statsMsg that carries the *types.State captured by Program.Stats
// on its first call. The dispatcher must wire that State onto the
// inner model so the info-density panels (topPlugins, topErrors,
// TotalHosts / TotalPorts) become live. Without this wiring (the
// pre-fix bug) the panels render placeholders forever in
// production because `d.inner.state != nil` is always false.
//
// We simulate the production path by constructing a Model via
// NewModel (no State — same as NewProgram does in cmd/scan.go),
// then dispatching a statsMsg that carries the State. Asserting
// the three production-relevant fields catches regressions.
//
// TestDispatcher_E2E_StateWiring 跑生产接线路径：NewProgram 构造的
// model（无 State）收到第一条 statsMsg，其中携带 Program.Stats 首
// 次调用捕获的 *types.State。dispatcher 必须把这个 State 接到内
// 部 model 上，让信息密度面板（topPlugins、topErrors、TotalHosts
// / TotalPorts）活起来。没有这一步接线（修前的 bug），面板在生
// 产中永远渲染占位符，因为 `d.inner.state != nil` 一直为假。
//
// 我们模拟生产路径：用 NewModel 构造 model（无 State——和
// cmd/scan.go 里 NewProgram 一样），再派发一条带 State 的
// statsMsg。断言 3 个生产相关字段，捕捉回归。
func TestDispatcher_E2E_StateWiring(t *testing.T) {
	// Build a *types.State exactly as the pipeline does. Populate
	// PluginHits / ErrorCategories via LoadOrStore (the same pattern
	// scanner.go uses via bumpSyncMap — see pipeline_workers.go).
	// Stage=Identify puts computeETA on the port-scan branch
	// (Ports > 0 AND TotalPorts > 0 AND Ports < TotalPorts); we
	// seed Ports=80, TotalPorts=100 so the condition holds and ETA
	// renders. TotalHosts=10 makes m.totalHosts() stable.
	//
	// / 像 pipeline 那样构建 *types.State。用 LoadOrStore 填充
	// PluginHits / ErrorCategories（和 scanner.go 经由 bumpSyncMap
	// 用的模式一样——见 pipeline_workers.go）。Stage=Identify 让
	// computeETA 走端口扫描分支（Ports > 0 且 TotalPorts > 0 且
	// Ports < TotalPorts）；埋 Ports=80、TotalPorts=100 让条件成
	// 立，ETA 能渲染。TotalHosts=10 让 m.totalHosts() 稳定。
	st := types.NewState()
	ph, _ := st.PluginHits.LoadOrStore("ssh", &atomic.Int64{})
	ph.(*atomic.Int64).Store(47)
	ph2, _ := st.PluginHits.LoadOrStore("redis", &atomic.Int64{})
	ph2.(*atomic.Int64).Store(21)
	ec, _ := st.ErrorCategories.LoadOrStore("timeout", &atomic.Int64{})
	ec.(*atomic.Int64).Store(8)
	st.Stage.Store(types.StageIdentify)
	st.TotalHosts.Store(10)
	st.TotalPorts.Store(100)

	// Construct a model WITHOUT a state — mirrors what cmd/scan.go
	// gets when it calls NewProgram(cfg). / 构造一个无 state 的
	// model——镜像 cmd/scan.go 调 NewProgram(cfg) 时拿到的。
	m := NewModel(nil)
	if m.state != nil {
		t.Fatalf("precondition: model.state = %v, want nil (matches cmd/scan.go path)", m.state)
	}

	// Send a statsMsg that carries the State — same shape
	// Program.Stats sends after the first-call capture. / 派发一
	// 条携带 State 的 statsMsg——和 Program.Stats 首次捕获后发的
	// 一模一样。
	//
	// Two messages: the first anchors d.inner.start on `now`; the
	// second advances `now` by a second so computeETA sees a
	// positive elapsed. A single call would leave elapsed=0 and
	// computeETA returns "" (the design from render.go:121-126).
	//
	// / 两条消息：第一条把 d.inner.start 锚在 now；第二条把 now 推
	// 后 1 秒，让 computeETA 看到正的 elapsed。单条会让 elapsed=0，
	// computeETA 返回 ""（render.go:121-126 的设计）。
	base := time.Now()
	for i, when := range []time.Time{base, base.Add(time.Second)} {
		msg := statsMsg{
			view: types.CountersView{
				Alive: 5, AliveProbed: 10, Ports: 80, Results: 5,
				Creds: 0, Errors: 3, Stage: int64(types.StageIdentify),
			},
			elapsed: time.Duration(i + 1).String(),
			when:    when,
			state:   st,
		}
		newM, _ := dispatcher{inner: &m}.Update(msg)
		m = *newM.(dispatcher).inner
	}

	// Production assertions: the state must be wired, top plugins
	// and errors must reflect the State views, and ETA must be
	// non-empty (computeETA reads TotalHosts/TotalPorts via
	// d.inner.state). / 生产断言：state 必须接上，top plugins 和
	// errors 必须反映 State 视图，ETA 必须非空（computeETA 通过
	// d.inner.state 读 TotalHosts/TotalPorts）。
	if m.state != st {
		t.Errorf("model.state = %v, want %v (state not wired)", m.state, st)
	}
	if len(m.topPlugins) == 0 {
		t.Errorf("topPlugins is empty, want populated from PluginHitsView")
	}
	if m.topPlugins[0][0] != "ssh" {
		t.Errorf("topPlugins[0] = %q, want ssh (highest hit count)", m.topPlugins[0][0])
	}
	if len(m.topErrors) == 0 {
		t.Errorf("topErrors is empty, want populated from ErrorCategoriesView")
	}
	if m.topErrors[0][0] != "timeout" {
		t.Errorf("topErrors[0] = %q, want timeout", m.topErrors[0][0])
	}
	if m.eta == "" {
		t.Errorf("eta is empty, want non-empty (computeETA needs d.inner.state)")
	}
	// TotalHosts / TotalPorts are read directly via the wired state
	// in the dispatcher's computeETA path. m.totalHosts() is the
	// helper View() uses; assert it returns 10 to prove the wiring
	// is live. / TotalHosts / TotalPorts 由 dispatcher 的 computeETA
	// 路径通过接上的 state 直接读。m.totalHosts() 是 View() 用的
	// 助手函数；断言它返回 10，证明接线生效。
	if got := m.totalHosts(); got != 10 {
		t.Errorf("m.totalHosts() = %d, want 10 (state.TotalHosts)", got)
	}
}
