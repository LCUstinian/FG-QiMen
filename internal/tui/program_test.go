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
	"sync"
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

// TestDispatcherEventMsg — a single eventMsg appends one entry;
// the ring buffer caps the slice at maxLiveEvents*2 (trimming
// back to maxLiveEvents when it exceeds that). The contract is
// "never grow past the trim threshold", so we send 5×maxLiveEvents
// and assert the cap holds and the most-recent event is preserved.
//
// Note: dispatcher.Update is a value-receiver method; each call
// returns a NEW dispatcher with the mutation applied. We must
// thread the returned dispatcher through the loop, otherwise the
// original d.inner.events is never updated.
//
// TestDispatcherEventMsg — 单条 eventMsg 追加一条记录；环形缓冲上限
// 为 maxLiveEvents*2（超过后修剪回 maxLiveEvents）。契约是"永远不超
// 过修剪阈值"，所以发 5×maxLiveEvents 条并断言上限保持 + 最新事件
// 被保留。
//
// 注意：dispatcher.Update 是值接收者；每次调用返回一个新的已应用
// 更新的 dispatcher。循环中必须串联返回的 dispatcher，否则原始
// d.inner.events 永远不会被更新。
func TestDispatcherEventMsg(t *testing.T) {
	mm := NewModel(nil)
	m := tea.Model(dispatcher{inner: &mm})
	ev := eventMsg{when: "12:00:00", tag: "scan", host: "1.1.1.1", port: 22, svc: "ssh", text: "OpenSSH 9.0"}
	for i := 0; i < maxLiveEvents*5; i++ {
		newM, _ := m.Update(ev)
		m = newM
	}
	// Drain pending → events by sending a benign WindowSizeMsg
	// through the dispatcher's fallthrough path. Without this
	// the model would just keep growing its pending buffer (the
	// dispatcher appends to pending, but pending is only flushed
	// on a non-eventMsg Update).
	// 通过 dispatcher 的 fallthrough 路径发一条 WindowSizeMsg 把
	// pending → events。否则 model 只会无限增长 pending 缓冲
	// （dispatcher 往 pending 里加，但 pending 只在非 eventMsg 的
	// Update 时刷入）。
	wm := tea.Model(dispatcher{inner: &mm})
	wm, _ = wm.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	got := wm.(dispatcher).inner.events
	if len(got) > maxLiveEvents*2 {
		t.Errorf("events exceeded cap: len=%d, want <= %d", len(got), maxLiveEvents*2)
	}
	// After a trim cycle the most-recent event is still at the tail
	// (the trim keeps the last maxLiveEvents entries).
	if len(got) == 0 {
		t.Fatal("events is empty after 5*maxLiveEvents appends")
	}
	if got[len(got)-1].host != "1.1.1.1" {
		t.Errorf("last event host = %q, want 1.1.1.1", got[len(got)-1].host)
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
		m = newM.(Model)
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
	// Thread the returned model through: Model.Update is a value
	// receiver, so the mutation is on a copy unless we re-bind.
	// 串联返回的 model：Model.Update 是值接收者，不重绑等于在副
	// 本上改。
	newM, _ := m.Update(tickMsg(time.Time{}))
	m = newM.(Model)
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
