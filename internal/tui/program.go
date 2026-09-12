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
	// when is the wall-clock time of this snapshot. Optional; when
	// zero, View / rate-tracker fall back to time.Now(). The
	// dispatcher populates it in v0.5.2+ so the EWMA rate stays
	// stable when the renderer is paused (rate should keep
	// counting even if no Update fires for a few seconds).
	// when 是本快照的墙钟时间。可选；为 0 时 View / rate-tracker
	// 回退到 time.Now()。v0.5.2+ dispatcher 填充它，让 EWMA 速率
	// 在 renderer 暂停时保持稳定（即使几秒无 Update，速率仍累
	// 计）。
	when time.Time
	// state carries the shared pipeline *types.State used to
	// populate the topPlugins / topErrors / TotalHosts / TotalPorts
	// panels. Program.Stats() captures it on the first call and
	// threads it through every subsequent statsMsg; the dispatcher
	// then writes it onto the model so the info-density panels
	// have a live source. Optional — tests that drive statsMsg
	// directly without a state still work (the model's state
	// field stays nil and the panels render placeholders).
	// state 携带共享的 pipeline *types.State，用于填充 topPlugins
	// / topErrors / TotalHosts / TotalPorts 面板。Program.Stats()
	// 在第一次调用时捕获它并串联到后续每条 statsMsg；dispatcher
	// 随后把它写到 model 上，让信息密度面板拿到实时数据源。可选
	// —— 直接派发 statsMsg 不带 state 的测试仍能工作（model 的
	// state 字段保持 nil，面板渲染占位符）。
	state *types.State
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
// mutated through pointer-receiver methods (pushEvent) for
// streaming events, and through value-returning Update for
// bubbletea messages.
//
// dispatcher 是 bubbletea model 的外观。内部 Model 通过指针接收者
// 方法（pushEvent）变更以处理流式事件，通过值返回的 Update 处理
// bubbletea 消息。
type dispatcher struct {
	inner *Model
}

func (d dispatcher) Init() tea.Cmd { return d.inner.Init() }

func (d dispatcher) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case statsMsg:
		d.inner.counters = m.view
		d.inner.elapsed = m.elapsed
		// First statsMsg = first sign of life from the pipeline.
		// We flip runState so the status bar chip changes from
		// IDLE to SCANNING. The transition is one-way until
		// doneMsg; a subsequent paused/resume cycle shouldn't
		// rewind the indicator.
		// 第一条 statsMsg = pipeline 的第一声"有动静"。我们翻
		// 转 runState 让状态条芯片从 IDLE 变 SCANNING。转换单
		// 向（直到 doneMsg）；后续暂停/恢复不应倒转指示。
		if d.inner.runState == runIdle {
			d.inner.runState = runScanning
		}
		// v0.5.2 production wiring: Program.Stats captures the
		// pipeline *types.State on the first call and threads it
		// through every subsequent statsMsg. We write it onto the
		// model here so the info-density panels (topPlugins,
		// topErrors, TotalHosts / TotalPorts) have a live source.
		// Without this, the dispatcher's `if d.inner.state != nil`
		// guards at the bottom of this handler always fail and the
		// panels render placeholders forever in production.
		//
		// v0.5.2 生产接线：Program.Stats 在第一次调用时捕获 pipeline
		// *types.State 并串联到后续每条 statsMsg。这里写到 model
		// 上让信息密度面板（topPlugins、topErrors、TotalHosts /
		// TotalPorts）有实时数据源。没有这一步，handler 底部的
		// `if d.inner.state != nil` 守卫永远失败，面板在生产中
		// 一直渲染占位符。
		if d.inner.state == nil && m.state != nil {
			d.inner.state = m.state
		}
		// v0.5.2: rate / ETA / topN. The dispatcher is the only
		// site that knows "this statsMsg just landed", so we fold
		// the math here. View() reads the cached fields; tests
		// inspect them directly after dispatching the message.
		//
		// v0.5.2：rate / ETA / topN。dispatcher 是唯一知道"这条
		// statsMsg 刚落地"的站点，所以数学折在这里。View() 读
		// 缓存字段；测试派发消息后直接检查。
		now := m.when
		if now.IsZero() {
			now = time.Now()
		}
		if d.inner.start.IsZero() {
			// Anchor ETA on the first statsMsg, not on
			// NewModel() — the model may be constructed seconds
			// before the pipeline actually starts.
			// 把 ETA 锚定在第一条 statsMsg 而非 NewModel()——
			// model 可能在 pipeline 真正启动前几秒就构造了。
			d.inner.start = now
		}
		hits, ports := d.inner.rate.update(now, m.view.Creds, m.view.Ports)
		d.inner.rateHits = hits
		d.inner.ratePorts = ports
		// computeETA needs TotalHosts / TotalPorts which the
		// CountersView doesn't carry (those live on the State
		// directly — see types/state.go). Pass them in alongside
		// the snapshot so we don't have to widen CountersView in
		// this task (the brief restricts changes to internal/tui).
		// computeETA 需要 TotalHosts / TotalPorts，但 CountersView
		// 不携带这两个（它们直接放在 State 上——见
		// types/state.go）。把它们与快照一起传入，避免在本任务
		// 加宽 CountersView（brief 限制只改 internal/tui）。
		var totalHosts, totalPorts int64
		if d.inner.state != nil {
			totalHosts = d.inner.state.TotalHosts.Load()
			totalPorts = d.inner.state.TotalPorts.Load()
		}
		d.inner.eta = computeETA(d.inner.start, now, int32(m.view.Stage), m.view, totalHosts, totalPorts)
		if d.inner.state != nil {
			d.inner.topPlugins = topN(d.inner.state.PluginHitsView(), 5)
			d.inner.topErrors = topN(d.inner.state.ErrorCategoriesView(), 5)
		}
		return d, nil
	case eventMsg:
		// Stream straight into the model's v0.7.0 event ring buffer
		// (pushEvent). Kind mapping: "cred" tags are credential
		// successes; everything else is an info hit. A 200ms flash
		// is set by pushEvent for hit-family kinds.
		// 直接流入 model 的 v0.7.0 事件 ring buffer（pushEvent）。
		// Kind 映射："cred" 标签是凭据成功；其余是信息命中。
		// hit 类 kind 的 200ms flash 由 pushEvent 设置。
		// An event arriving before any statsMsg is also a sign of
		// life (the first cred or open port often lands before
		// the first 1Hz tick). Promote runState here too.
		// 任何 statsMsg 之前到达的事件也算"有动静"（第一个凭据
		// 或开放端口常常先于第一个 1Hz tick 到达）。这里也提升
		// runState。
		if d.inner.runState == runIdle {
			d.inner.runState = runScanning
		}
		if d.inner.uiMode == modePaused {
			// Drop on the floor: paused mode freezes display. We
			// don't buffer because the pipeline may produce a
			// burst of events during a long pause; better to lose
			// history than to OOM the dashboard. Same contract as
			// the pre-v0.7.0 appendEvent path.
			// 暂停态直接丢弃：暂停冻结显示。我们不缓冲，因为
			// pipeline 在长暂停期间可能爆发事件；丢历史比 OOM 掉
			// dashboard 好。与 v0.7.0 前 appendEvent 路径契约一致。
			return d, nil
		}
		kind := "hit"
		if m.tag == "cred" {
			kind = "cred_success"
		}
		at, err := time.Parse("15:04:05", m.when)
		if err != nil {
			at = time.Now()
		}
		d.inner.pushEvent(eventEntry{
			Host:    m.host,
			Port:    m.port,
			Service: m.svc,
			Kind:    kind,
			At:      at,
		})
		return d, nil
	case doneMsg:
		d.inner.finalSummary = m.summary
		// runState = runDone kicks off the linger. We don't set
		// quitting yet — the model will quit from its own
		// tickMsg handler after `lingerTicks` frames. This
		// keeps the final summary visible inside the TUI frame
		// long enough to read, fixing the "I can't tell if it
		// finished" complaint.
		// runState = runDone 启动 linger。我们此时不置 quitting
		// —— model 会在 `lingerTicks` 帧后从自己的 tickMsg 处
		// 理器退出。这让最终摘要在 TUI 框内可见够长时间可读，
		// 修复"我分不清它完没完成"的痛点。
		d.inner.runState = runDone
		d.inner.lingerLeft = lingerTicks
		return d, nil
	}
	// Fall through to the wrapped model for key/window messages.
	// 透传按键/窗口消息到底层 model。
	newInner, cmd := d.inner.Update(msg)
	if mm, ok := newInner.(*Model); ok {
		*d.inner = *mm
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
	// state is the shared pipeline *types.State captured by the
	// first Stats() call. Held under mu; once set it's never
	// changed (pipeline uses a single State for the whole scan).
	// The dispatcher copies the pointer onto its inner model on
	// the first statsMsg so the info-density panels have a live
	// source.
	//
	// state 是首次 Stats() 调用捕获的共享 pipeline *types.State。
	// 在 mu 下持有；设置后不再变（pipeline 单次扫描用同一个 State）。
	// dispatcher 在第一条 statsMsg 把指针拷到内部 model 上，让信
	// 息密度面板有实时数据源。
	state *types.State
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
// v0.5.2 production wiring: the first Stats() call captures the
// *types.State pointer so subsequent statsMsg dispatches can wire
// it onto the inner model (the info-density panels depend on it).
// The capture is a one-shot — a single State is used for the whole
// scan, so re-capturing would be wasted work. Tests that drive
// statsMsg directly without a State still work because state is
// optional on statsMsg.
//
// Stats 实现 ui.UI——推送最新计数器快照。带短路：节流窗口内相同
// 快照直接丢弃；最近一次派发记在 lastStats，空 tick 不会叫醒
// 渲染循环。
//
// v0.5.2 生产接线：首次 Stats() 调用捕获 *types.State 指针，让后续
// statsMsg 派发能把它接到内部 model 上（信息密度面板依赖它）。捕
// 获是一次性的——单次扫描共用一个 State，重捕只会浪费。直接派发
// statsMsg 不带 State 的测试仍能工作，因为 state 在 statsMsg 上是
// 可选的。
func (p *Program) Stats(s *types.State) {
	if s == nil {
		return
	}
	// First-call capture: the pipeline passes the same *State
	// pointer every tick, so we just remember the first one and
	// reuse it forever. Guarded by mu so a concurrent Stats()
	// (rare, but possible across shutdown boundaries) can't race.
	//
	// 首次调用捕获：pipeline 每次 tick 都传同一个 *State 指针，
	// 所以只记第一个，后续复用。mu 守护，避免并发 Stats()（罕见
	// 但 shutdown 边界可能）赛跑。
	p.mu.Lock()
	if p.state == nil {
		p.state = s
	}
	st := p.state
	p.mu.Unlock()
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
	p.p.Send(statsMsg{view: view, elapsed: elapsed, when: now, state: st})
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
