// tui/program.go — Bubbletea program wrapper implementing ui.UI.
// tui/program.go — Bubbletea 包装，实现 ui.UI。
package tui

import (
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/LCUstinian/FG-QiMen/internal/types"
)

// ─────────────────────────────────────────────────────────────────────
// Custom message types
// 自定义消息类型
// ─────────────────────────────────────────────────────────────────────

// statsThrottle is the minimum gap between statsMsg dispatches.
// The pipeline ticks at 1Hz (see pipeline.go) but a slow consumer
// could still smear the screen; we coalesce faster pushes by
// keeping a pending snapshot and letting the runtime flush it on
// the next tick. 250ms = 4Hz ceiling, which is plenty for a
// dashboard a human is reading.
//
// statsThrottle 是 statsMsg 派发之间的最小间隔。pipeline 1Hz 滴答
// （见 pipeline.go），但慢消费者仍可能让屏幕花；我们合并更快的推
// 送，保留一个待定快照让 runtime 在下一 tick 刷出。250ms = 4Hz
// 上限，对人眼读的 dashboard 绰绰有余。
const statsThrottle = 250 * time.Millisecond

type statsMsg struct {
	view    types.CountersView
	elapsed string
}

type eventMsg struct {
	when, tag, host, svc, text string
	port                       int
}

type doneMsg struct {
	summary string
}

// ─────────────────────────────────────────────────────────────────────
// dispatcher wraps Model and understands our custom messages.
// dispatcher 包装 Model 并能处理自定义消息。
// ─────────────────────────────────────────────────────────────────────

// dispatcher is the bubbletea model façade. The inner Model is
// mutated through pointer-receiver methods (appendEvent) for
// streaming events, and through value-returning Update for
// bubbletea messages. We do NOT swap the inner model in place
// (as we used to) — that pattern was redundant once the model
// grew the pending→events drain in Update().
//
// dispatcher 是 bubbletea model 的外观。内部 Model 通过指针接收者
// 方法（appendEvent）变更以处理流式事件，通过值返回的 Update 处理
// bubbletea 消息。我们不再像以前那样原地替换 inner model —— 一旦
// model 在 Update() 里加上了 pending→events drain，那个模式就多
// 余了。
type dispatcher struct {
	inner *Model
}

func (d dispatcher) Init() tea.Cmd { return d.inner.Init() }

func (d dispatcher) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case statsMsg:
		d.inner.counters = m.view
		d.inner.elapsed = m.elapsed
		return d, nil
	case eventMsg:
		// Stream straight into the model's pending buffer. The
		// model drains pending → events on the next Update tick
		// (see Model.Update in tui.go), so a burst of 100 events
		// results in a single re-render, not 100.
		// 直接流入 model 的 pending 缓冲。model 在下一次 Update
		// tick 把 pending → events（见 tui.go 的 Model.Update），
		// 100 条事件爆发只触发 1 次重渲染，不是 100 次。
		d.inner.appendEvent(liveEvent{
			when: m.when, tag: m.tag, host: m.host, port: m.port, svc: m.svc, text: m.text,
		})
		return d, nil
	case doneMsg:
		d.inner.finalSummary = m.summary
		d.inner.quitting = true
		return d, tea.Quit
	}
	// Fall through to the wrapped model for key/window messages.
	// 透传按键/窗口消息到底层 model。
	newInner, cmd := d.inner.Update(msg)
	if mm, ok := newInner.(Model); ok {
		*d.inner = mm
	}
	return d, cmd
}

func (d dispatcher) View() string { return d.inner.View() }

// ─────────────────────────────────────────────────────────────────────
// Program wraps tea.Program and implements ui.UI.
// Program 包装 tea.Program 并实现 ui.UI。
// ─────────────────────────────────────────────────────────────────────

// Program is a thin wrapper around a *tea.Program that satisfies the
// ui.UI interface. All UI methods are safe for concurrent use; they
// send messages into the bubbletea event loop.
//
// The mu mutex guards a Done() once-guard: scanner.go calls Done()
// from both the success and the early-error paths (see observation
// 1344) so the bubbletea program may receive two doneMsg back-to-back
// if the second call lands before the first has dispatched; the lock
// keeps the second dispatch a clean no-op.
//
// Program 是 *tea.Program 的薄包装，实现 ui.UI 接口。所有 UI 方法
// 都并发安全；它们向 bubbletea 事件循环发送消息。
//
// mu 互斥锁守卫 Done() 的一次性：scanner.go 在成功和早错两条路径都会
// 调 Done()（见 observation 1344），所以 bubbletea program 可能连续收
// 到两个 doneMsg；锁让第二次 dispatch 成为干净的空操作。
type Program struct {
	p  *tea.Program
	mu sync.Mutex
	// doneOnce tracks whether Done() has fired; the bubbletea program
	// is single-use, so a second Done() would send a doneMsg into a
	// dying program. Guards against the success + early-error double
	// call from scanner.go.
	// doneOnce 跟踪 Done() 是否已触发；bubbletea program 是单次使用，
	// 第二次 Done() 会把 doneMsg 投进正在退出的 program。守卫
	// scanner.go 成功 + 早错两次调用。
	doneOnce bool
	// lastStats is the last stats snapshot we successfully
	// dispatched. Stats() compares against it and short-circuits
	// if the counters + elapsed are unchanged AND the last send
	// is still inside the throttle window. This kills the "1Hz
	// tick that re-renders an idle screen" problem.
	// lastStats 是我们成功派发的最后一份 stats 快照。Stats() 与
	// 之对比，若计数器 + elapsed 未变且上次发送仍在节流窗口内
	// 则短路。干掉"1Hz 滴答在空闲屏上空转渲染"的问题。
	lastStats   types.CountersView
	lastWhen    time.Time
	lastElapsed string
	ran         time.Time
	// cfg is the source of truth for ShowCleartext (P0#3 redaction).
	// Held by value; cfg is immutable in practice (cobra flags are
	// populated once at startup). The TUI never mutates it.
	// cfg 是 ShowCleartext（P0#3 redact）的真源。按值持有；cfg 实际
	// 上是不可变的（cobra flags 启动时一次性填充）。TUI 不会修改它。
	cfg *types.Config
}

// NewProgram constructs a Program. The bubbletea event loop does NOT
// start until Run() is called; this keeps the constructor safe to
// invoke from tests and from ui.Select without touching the terminal.
//
// NewProgram 构造一个 Program。bubbletea 事件循环要等 Run() 才启动；
// 这让构造函数可从测试和 ui.Select 安全调用而不触碰终端。
//
// We disable bubbletea's default signal handler because the parent
// (cmd/scan.go) already owns the SIGINT-driven shutdown. The program
// can still be told to quit by calling Done() or Quit().
//
// 我们禁用 bubbletea 的默认 signal handler，因为父级（cmd/scan.go）已
// 经拥有 SIGINT 驱动的关闭逻辑。仍然可以通过调用 Done() 或 Quit() 让
// program 退出。
func NewProgram(cfg *types.Config) *Program {
	m := NewModel(cfg)
	d := &dispatcher{inner: &m}
	p := tea.NewProgram(*d, tea.WithoutSignalHandler(), tea.WithAltScreen())
	return &Program{
		p:   p,
		ran: time.Now(),
		cfg: cfg,
	}
}

// Run blocks until the bubbletea program exits. Returns the final
// program state or any error.
// Run 阻塞到 bubbletea program 退出。返回最终 program 状态或错误。
func (p *Program) Run() (tea.Model, error) {
	return p.p.Run()
}

// Quit sends a quit message to bubbletea. Non-blocking; the program
// exits on its own schedule. Use <-runDone (provided to runScan via
// buildSession's runDone out-param) to wait for the full teardown.
//
// Quit 向 bubbletea 发送 quit 消息。不阻塞；program 按自己的节奏退出。
// 等待完全拆除用 <-runDone（通过 buildSession 的 runDone 出参交给
// runScan）。
func (p *Program) Quit() { p.p.Quit() }

// Banner is a no-op for the TUI — the dashboard renders its own
// title bar with the project / mode strings in NewModel(), so a
// separate Banner call would only blank the screen for one frame.
// Required by the ui.UI interface.
//
// Banner 对 TUI 是空操作——dashboard 在 NewModel() 中已经渲染了带
// project / mode 信息的标题栏，单独的 Banner 调用只会让屏幕闪一帧。
// 仅为满足 ui.UI 接口。
func (p *Program) Banner(*types.Config) {}

// Stats implements ui.UI by pushing a fresh counters snapshot.
// Implements a short-circuit: identical snapshots inside the
// throttle window are dropped, and the last dispatch is held
// in lastStats so a no-op tick doesn't wake the render loop.
//
// Stats 实现 ui.UI——推送最新计数器快照。带短路：节流窗口内相同
// 快照直接丢弃；最近一次派发记在 lastStats，空 tick 不会叫醒
// 渲染循环。
func (p *Program) Stats(s *types.State) {
	if s == nil {
		return
	}
	view := s.Snapshot()
	elapsed := time.Since(p.ran).Round(time.Second).String()
	// Fast path: identical counters + elapsed within the throttle
	// window is a no-op. This is the common case on an idle scan
	// (no new creds, no new ports, no new errors).
	// 快路径：节流窗口内计数器 + elapsed 都相同则空操作。这是空
	// 闲扫描的常见情况（无新凭据、新端口、新错误）。
	now := time.Now()
	if view == p.lastStats && elapsed == p.lastElapsed && now.Sub(p.lastWhen) < statsThrottle {
		return
	}
	p.lastStats = view
	p.lastElapsed = elapsed
	p.lastWhen = now
	p.p.Send(statsMsg{view: view, elapsed: elapsed})
}

// Event implements ui.UI — push a non-cred live event.
// Event 实现 ui.UI——推送非凭据类的实时事件。
func (p *Program) Event(r *types.Result) {
	if r == nil {
		return
	}
	p.p.Send(eventMsg{
		when: r.Time.Format("15:04:05"),
		tag:  "scan",
		host: r.Host,
		port: r.Port,
		svc:  r.Service,
		text: r.Banner,
	})
}

// CredFound implements ui.UI — push a high-priority cred event.
// Renders via types.ShowUserPassword so cfg.ShowCleartext controls
// whether the cleartext pair or a redacted fingerprint is shown on
// the dashboard. Cleartext on screen is risky in shared-screen /
// screen-recording / bug-report contexts (P0#3); default is redact.
//
// CredFound 实现 ui.UI——推送高优先级凭据事件。
// 走 types.ShowUserPassword 渲染，cfg.ShowCleartext 决定 dashboard
// 上显示明文对还是脱敏指纹。屏幕上的明文在共享屏幕 / 屏幕录制 / bug
// 报告场景有风险（P0#3）；默认 redact。
func (p *Program) CredFound(r *types.Result) {
	if r == nil || r.Cred == nil {
		return
	}
	p.p.Send(eventMsg{
		when: r.Time.Format("15:04:05"),
		tag:  "cred",
		host: r.Host,
		port: r.Port,
		svc:  r.Service,
		text: types.ShowUserPassword(p.cfg, r.Cred.User, r.Cred.Pass),
	})
}

// Done implements ui.UI by setting the final summary and quitting.
// Idempotent: scanner.go calls Done() from both the success and the
// early-error paths (scanner.go:69 and :163), so a second call
// arriving while bubbletea is already exiting would crash on a
// closed send channel. The once-guard makes the second call a
// silent no-op.
//
// The send is dispatched in a fire-and-forget goroutine: bubbletea's
// Send blocks until a consumer reads from the program's message
// channel, so a Done() arriving after the program has exited (or
// before Run() has started — e.g. in tests) would otherwise hang
// the caller. The goroutine pattern + once-guard mean we never
// queue more than one message and we never block on the call site.
//
// Done 实现 ui.UI——设置最终摘要并退出。幂等：scanner.go 在成功和早
// 错两条路径都会调 Done()（scanner.go:69 和 :163），所以第二次调用到
// 达时 bubbletea 正在退出，再发 doneMsg 会因 send 通道已关闭而崩溃。
// once-guard 让第二次调用成为静默空操作。
//
// 发送用 fire-and-forget goroutine 派发：bubbletea 的 Send 会阻塞
// 直到 program 消息通道的消费者读出，所以 program 退出后（或 Run()
// 启动前，例如测试场景）调 Done() 会让调用方挂起。goroutine 模式 +
// once-guard 保证：至多排队一条消息、调用方永不阻塞。
func (p *Program) Done(summary string) {
	p.mu.Lock()
	if p.doneOnce {
		p.mu.Unlock()
		return
	}
	p.doneOnce = true
	p.mu.Unlock()
	go p.p.Send(doneMsg{summary: summary})
}
