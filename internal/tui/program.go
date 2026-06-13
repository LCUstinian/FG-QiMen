// tui/program.go — Bubbletea program wrapper implementing ui.UI.
// tui/program.go — Bubbletea program 包装，实现 ui.UI。
package tui

import (
	"fmt"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/LCUstinian/FG-QiMen/internal/types"
)

// ─────────────────────────────────────────────────────────────────────
// Custom message types
// 自定义消息类型
// ─────────────────────────────────────────────────────────────────────

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

type dispatcher struct {
	inner Model
}

func (d dispatcher) Init() tea.Cmd { return d.inner.Init() }

func (d dispatcher) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case statsMsg:
		d.inner.counters = m.view
		d.inner.elapsed = m.elapsed
		return d, nil
	case eventMsg:
		ev := liveEvent{when: m.when, tag: m.tag, host: m.host, port: m.port, svc: m.svc, text: m.text}
		d.inner.events = append(d.inner.events, ev)
		if len(d.inner.events) > maxLiveEvents*2 {
			d.inner.events = d.inner.events[len(d.inner.events)-maxLiveEvents:]
		}
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
		d.inner = mm
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
// Program 是 *tea.Program 的薄包装，实现 ui.UI 接口。所有 UI 方法
// 都并发安全；它们向 bubbletea 事件循环发送消息。
type Program struct {
	p   *tea.Program
	d   *dispatcher
	mu  sync.Mutex
	ran time.Time
}

// NewProgram constructs a Program and starts the bubbletea event loop.
// NewProgram 构造一个 Program 并启动 bubbletea 事件循环。
//
// We disable bubbletea's default signal handler because the parent
// (cmd/root.go) already owns the SIGINT-driven shutdown. The program
// can still be told to quit by calling Done() or Quit().
//
// 我们禁用 bubbletea 的默认 signal handler，因为父级（cmd/root.go）已经
// 拥有 SIGINT 驱动的关闭逻辑。仍然可以通过调用 Done() 或 Quit() 让
// program 退出。
func NewProgram(cfg *types.Config) *Program {
	d := &dispatcher{inner: NewModel(cfg)}
	p := tea.NewProgram(*d, tea.WithoutSignalHandler(), tea.WithAltScreen())
	return &Program{
		p:   p,
		d:   d,
		ran: time.Now(),
	}
}

// Run blocks until the bubbletea program exits. Returns the final
// program state or any error.
// Run 阻塞到 bubbletea program 退出。返回最终 program 状态或错误。
func (p *Program) Run() (tea.Model, error) {
	return p.p.Run()
}

// Quit sends a quit message to bubbletea, then blocks until it exits.
// Quit 向 bubbletea 发送 quit 消息，然后阻塞到退出。
func (p *Program) Quit() { p.p.Quit() }

// Banner implements ui.UI by sending a refresh message that
// triggers a redraw.
// Banner 实现 ui.UI——发送 refresh 触发重绘。
func (p *Program) Banner(cfg *types.Config) {
	p.p.Send(refreshMsg{})
}

// Stats implements ui.UI by pushing a fresh counters snapshot.
// Stats 实现 ui.UI——推送最新计数器快照。
func (p *Program) Stats(s *types.State) {
	if s == nil {
		return
	}
	p.p.Send(statsMsg{view: s.Snapshot(), elapsed: time.Since(p.ran).Round(time.Second).String()})
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
// CredFound 实现 ui.UI——推送高优先级凭据事件。
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
		text: fmt.Sprintf("%s / %s", r.Cred.User, r.Cred.Pass),
	})
}

// Done implements ui.UI by setting the final summary and quitting.
// Done 实现 ui.UI——设置最终摘要并退出。
func (p *Program) Done(summary string) {
	p.mu.Lock()
	// (we keep the summary in dispatcher state via doneMsg)
	p.mu.Unlock()
	p.p.Send(doneMsg{summary: summary})
}

type refreshMsg struct{}
