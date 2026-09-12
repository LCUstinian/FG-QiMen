// model.go — Model state extracted from tui.go for the v0.7.0 TUI
// overhaul. State-only (no Update, no View); render layer reads from
// these fields. / model.go — 从 tui.go 提取的 Model state 供 v0.7.0
// TUI 大改用。仅 state（无 Update、无 View）；渲染层读这些字段。
package tui

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/LCUstinian/FG-QiMen/internal/types"
)

// eventEntry is one row in the LIVE EVENTS panel. / eventEntry
// 是 LIVE EVENTS 面板的一行。
type eventEntry struct {
	Host    string
	Port    int
	Service string
	Kind    string // "hit" | "miss" | "cred_success" | "warn" | "critical_hit"
	At      time.Time
}

// Model is the Bubbletea model for the dashboard.
// Model 是 Bubbletea dashboard 的 model.
//
// v0.7.0: struct definition relocated here from tui.go so the new
// ring-buffer / flash / spinner fields sit next to the state they
// describe. Methods (Init / Update / View / constructors) remain in
// tui.go since they reference the type by name within the same
// package.
// v0.7.0：结构体定义从 tui.go 搬到这里，让新增的 ring buffer /
// flash / spinner 字段与它们所描述的状态相邻。方法（Init /
// Update / View / 构造函数）留在 tui.go，因为同包内按类型名引用。
type Model struct {
	// Width / height are the terminal size; bubbletea auto-updates them
	// via WindowSizeMsg.
	// Width / height 是终端尺寸；bubbletea 通过 WindowSizeMsg 自动更新。
	width  int
	height int

	// Stats snapshot / 统计快照
	counters types.CountersView
	elapsed  string
	mode     string
	project  string

	// pending events appended by the dispatcher; flushed into
	// events on the next tick. Keeps the dispatcher contract
	// (append-only) intact while letting the model amortise the
	// cost of re-sorting/trimming to one operation per render.
	// pending 事件由 dispatcher 追加；在下一次 tick 时刷入 events。
	// 保留 dispatcher 的追加契约，同时让模型在每次渲染时把排序
	// /修剪的开销摊销成一次。
	pending []liveEvent

	// Live events (newest last) / 实时事件（最新在末尾）
	events []liveEvent

	// uiMode is the dashboard interaction state. / uiMode 是
	// dashboard 的交互状态。
	uiMode mode

	// runState mirrors the pipeline lifecycle (idle/scanning/done)
	// for the status bar chip + title bar. Independent of uiMode.
	// runState 镜像 pipeline 生命周期（空闲/扫描/完成），供状态
	// 条芯片 + 标题栏使用。独立于 uiMode。
	runState runState

	// frameIdx is the current frame in the spinner rotation. We
	// keep it on the model (not in styles) so the rotation is
	// driven by tickMsg, not a global counter.
	// frameIdx 是 spinner 旋转的当前帧。放在 model 上（而非
	// styles）让旋转由 tickMsg 驱动，而非全局计数器。
	frameIdx int

	// lingerLeft counts down the linger frames after runDone; the
	// dashboard exits the bubbletea loop when it hits zero (unless
	// the user pressed 'q', which exits immediately).
	// lingerLeft 在 runDone 后递减；归零时 dashboard 退出
	// bubbletea 循环（除非用户按了 'q'，那条路径立即退出）。
	lingerLeft int

	// Quit flag / 退出标志
	quitting bool

	// Final summary printed after bubbletea exits / bubbletea 退出
	// 后打印的最终摘要。
	finalSummary string

	// ── v0.5.2: TUI v2 Spec A info-density panels ──
	// v0.5.2：TUI v2 Spec A 信息密度面板

	// state is the shared pipeline state (PluginHits /
	// ErrorCategories views). Optional; nil-safe — when nil, the
	// topPlugins / topErrors panels render placeholders.
	// state 是共享的 pipeline 状态（PluginHits / ErrorCategories
	// 视图）。可选；nil 安全——为 nil 时 topPlugins / topErrors 面
	// 板渲染占位符。
	state *types.State

	// start is the wall-clock time the model began tracking ETA.
	// Populated by the first statsMsg — that way computeETA has
	// a stable "t0" even when the model is constructed before the
	// pipeline starts.
	// start 是 model 开始追踪 ETA 的墙钟时间。由第一条 statsMsg
	// 填充——这样 computeETA 拥有稳定的 "t0"，即使 model 在
	// pipeline 启动前就已构造。
	start time.Time

	// rate is the EWMA state for hits/s and ports/s; embedded so
	// the helpers in render.go operate on the parent's fields.
	// rate 是 hits/s 与 ports/s 的 EWMA 状态；内嵌让 render.go
	// 的辅助函数能直接操作父结构体字段。
	rate rateTracker

	// rateHits / ratePorts are the latest smoothed rates cached
	// from rate.update(); the View renders them directly without
	// re-computing.
	// rateHits / ratePorts 是 rate.update() 缓存的最新平滑速率；
	// View 直接渲染，不再重算。
	rateHits  float64
	ratePorts float64

	// eta is the per-stage ETA string (or "" when unavailable).
	// eta 是按阶段的 ETA 字符串（不可用时为 ""）。
	eta string

	// topPlugins / topErrors are the top-5 (name, countString)
	// tuples rendered in the right-hand panels. Recomputed on
	// every statsMsg from the State views.
	// topPlugins / topErrors 是右面板渲染的 top-5 (name,
	// countString) 元组。每次 statsMsg 从 State 视图重算。
	topPlugins [][2]string
	topErrors  [][2]string

	// flashUntil maps "host:port" → expiry wall-clock for the
	// "recent critical event" highlight. Looked up by
	// severityColor in styles.go so freshly-flashed rows paint
	// red regardless of their underlying kind. / flashUntil 把
	// "host:port" 映射到"最近关键事件"高亮的到期墙钟时间。
	// styles.go 的 severityColor 会查它，让刚 flash 的行不管
	// 底层 kind 是什么都画红。
	flashUntil map[string]time.Time

	// ── v0.7.0 additions: ring buffers, errors toggle, spinner ──
	// v0.7.0 新增：ring buffer、错误切换、spinner。

	// events is a fixed-size ring buffer (cap 20) of eventEntry
	// rows. Cap is fixed at construction so pushEvent doesn't
	// allocate per call. The buffer is chronological when
	// eventsFull is true; eventsHead indexes the next write slot.
	// events 是固定大小（cap 20）的 eventEntry 行环形缓冲。构造
	// 时固定 cap，pushEvent 不再每次分配。eventsFull 为 true 时
	// 缓冲按时间顺序；eventsHead 指向下一个写入槽位。
	eventsBuf []eventEntry

	// eventsCap is cap(eventsBuf). Stored explicitly (instead of
	// recomputed) so pushEvent's hot path doesn't read len() every
	// call. / eventsCap 是 cap(eventsBuf)。显式存（非重算）以
	// 避免 pushEvent 热路径每次读 len()。
	eventsCap int

	// eventsHead is the next write index into eventsBuf. Wraps via
	// modulo eventsCap. / eventsHead 是 eventsBuf 下一个写入索引，
	// 按 eventsCap 取模回绕。
	eventsHead int

	// eventsFull is true once the buffer has wrapped at least once;
	// eventsOrdered uses it to choose the iteration start (head vs 0).
	// eventsFull 在缓冲至少回绕过一次后为 true；eventsOrdered 据此
	// 选择迭代起点（head 或 0）。
	eventsFull bool

	// rateSamples is a fixed-size ring buffer (cap 60) of hits/sec
	// samples. 60 samples = 60s of history at 1Hz, plenty for the
	// sparkline width without unbounded growth. / rateSamples 是
	// 固定大小（cap 60）的 hits/sec 样本环形缓冲。60 样本 = 1Hz
	// 下 60s 历史，足够 sparkline 宽度又不无限增长。
	rateSamples []float64

	// rateCap is cap(rateSamples). / rateCap 是 cap(rateSamples)。
	rateCap int

	// rateHead is the next write index into rateSamples.
	// / rateHead 是 rateSamples 下一个写入索引。
	rateHead int

	// rateFull is true once rateSamples has wrapped at least once.
	// / rateFull 在 rateSamples 至少回绕过一次后为 true。
	rateFull bool

	// errorsExpanded is the collapse/expand state for the error
	// categories row. Toggled by an 'e' key (handlers wired in a
	// later task). Defaults to false (collapsed). / errorsExpanded
	// 是错误分类行的折叠/展开态。由 'e' 按键切换（接线由后续任务
	// 完成）。默认 false（折叠）。
	errorsExpanded bool

	// NOTE: a Bubbletea spinner.Model field was planned for the
	// stage badge glyph per the v0.7.0 brief, but bubbles/spinner
	// is not currently a dependency ("no new dependencies" plan
	// constraint). The existing frameIdx + spinnerFrames string
	// rotation handles the glyph without it; if a later task
	// pulls bubbles in, add `spinner spinner.Model` here.
	// 注：v0.7.0 brief 原本计划在阶段徽章字形处加 Bubbletea
	// spinner.Model 字段，但 bubbles/spinner 当前不是依赖
	// （"无新增依赖"计划约束）。现有 frameIdx + spinnerFrames
	// 字符串轮转已能处理；若后续任务引入 bubbles，再加 `spinner
	// spinner.Model`。
}

// ── v0.7.0 helpers ──

// pushEvent appends to the event ring buffer. If Kind is a hit-family
// kind ("hit" / "critical_hit" / "cred_success"), sets a 200ms flash
// expiry keyed by host:port so the row paints red for one render cycle
// regardless of its underlying severity. / pushEvent 追加到事件 ring
// buffer。Kind 是 hit 类（"hit" / "critical_hit" / "cred_success"）
// 时设 200ms flash 过期，以 host:port 为键让该行在一帧内画红，不
// 管底层 severity。
func (m *Model) pushEvent(e eventEntry) {
	if m.eventsBuf == nil {
		// First push: lazy-init the ring buffer. Cap 20 = ~20s of
		// history at 1Hz, which fits a typical 24-row terminal with
		// room to spare for older context. / 首次 push：懒初始化 ring
		// buffer。Cap 20 = 1Hz 下约 20s 历史，能塞进典型 24 行终端并
		// 留出给旧上下文的余地。
		const eventCap = 20
		m.eventsBuf = make([]eventEntry, eventCap)
		m.eventsCap = eventCap
	}
	m.eventsBuf[m.eventsHead] = e
	m.eventsHead = (m.eventsHead + 1) % m.eventsCap
	if m.eventsHead == 0 {
		// Wrapped exactly once: subsequent pushes overwrite the
		// oldest entry. / 恰好回绕一次：之后的 push 会覆盖最老条目。
		m.eventsFull = true
	}
	if isHitKind(e.Kind) {
		key := fmt.Sprintf("%s:%d", e.Host, e.Port)
		if m.flashUntil == nil {
			m.flashUntil = make(map[string]time.Time)
		}
		m.flashUntil[key] = time.Now().Add(200 * time.Millisecond)
	}
}

// isHitKind returns true for any hit-family kind. / isHitKind 对
// 任何 hit 类 kind 返回 true。
func isHitKind(kind string) bool {
	switch kind {
	case "hit", "critical_hit", "cred_success":
		return true
	}
	return false
}

// eventsOrdered iterates the ring buffer in chronological order
// (oldest → newest), handling wrap. / eventsOrdered 按时间顺序
// 迭代 ring buffer，处理回绕。
func (m *Model) eventsOrdered() []eventEntry {
	if m.eventsBuf == nil {
		return nil
	}
	if !m.eventsFull {
		// Not wrapped yet: head is the next write slot, so [0:head]
		// is the populated prefix. / 还没回绕：head 是下一个写入槽位，
		// 所以 [0:head] 是已填充的前缀。
		return m.eventsBuf[:m.eventsHead]
	}
	out := make([]eventEntry, 0, m.eventsCap)
	out = append(out, m.eventsBuf[m.eventsHead:]...)
	out = append(out, m.eventsBuf[:m.eventsHead]...)
	return out
}

// recordRate pushes a hits/sec sample into the rate ring buffer.
// Cap 60 = 60s of history at 1Hz, enough for the sparkline at the
// Spec B header. / recordRate 把 hits/sec 样本推入 rate ring
// buffer。Cap 60 = 1Hz 下 60s 历史，足够 Spec B header 的 sparkline。
func (m *Model) recordRate(hitsPerSec float64) {
	if m.rateSamples == nil {
		const rateCap = 60
		m.rateSamples = make([]float64, rateCap)
		m.rateCap = rateCap
	}
	m.rateSamples[m.rateHead] = hitsPerSec
	m.rateHead = (m.rateHead + 1) % m.rateCap
	if m.rateHead == 0 {
		m.rateFull = true
	}
}

// rateOrdered iterates the rate buffer chronologically (oldest →
// newest). / rateOrdered 按时间顺序迭代 rate buffer。
func (m *Model) rateOrdered() []float64 {
	if m.rateSamples == nil {
		return nil
	}
	if !m.rateFull {
		return m.rateSamples[:m.rateHead]
	}
	out := make([]float64, 0, m.rateCap)
	out = append(out, m.rateSamples[m.rateHead:]...)
	out = append(out, m.rateSamples[:m.rateHead]...)
	return out
}

// pruneExpiredFlashes removes flash entries whose expiry is in the
// past. Called from Update on flashTickMsg to bound map growth — a
// long-running scan with many distinct hit hosts would otherwise
// leak entries past their 200ms window. / pruneExpiredFlashes 删除
// 过期的 flash 条目。由 flashTickMsg 在 Update 中调用以限制 map
// 增长——长扫描若命中大量不同 host 会让条目超过 200ms 窗口仍留存。
func (m *Model) pruneExpiredFlashes(now time.Time) {
	if m.flashUntil == nil {
		return
	}
	for k, until := range m.flashUntil {
		if !now.Before(until) {
			delete(m.flashUntil, k)
		}
	}
}

// clearErrors zeroes the errors-expanded flag. Called on 'E' key
// from Update. Per-category error counts live in State.ErrorCategories
// (read-only from the TUI); we don't touch that here — we only
// collapse the panel. / clearErrors 清零 errors-expanded 标记。由
// Update 中 'E' 按键调用。每类错误计数存在于 State.ErrorCategories
// （TUI 只读）；此处不动那个——只折叠面板。
func (m *Model) clearErrors() {
	m.errorsExpanded = false
}

// toEntry converts a runner.Event-shaped call into an eventEntry.
// The 5-argument form is the fallback when runner.Event is unexported;
// if the runner package later exposes a public Event with matching
// fields, replace this with a struct conversion. / toEntry 把
// runner.Event 形态的调用转成 eventEntry。5 参数形式是 runner.Event
// 未导出时的回退；若 runner 包后续导出公开 Event，改成结构体转换。
//
// eventEntry as the wire format (planned Task 4+); not used yet
// because no runner package code calls into the TUI package's
// unexported helpers today.
//
//nolint:unused // wired by the runner→TUI dispatcher once it adopts
func toEntry(host string, port int, service, kind string, at time.Time) eventEntry {
	return eventEntry{Host: host, Port: port, Service: service, Kind: kind, At: at}
}

// Compile-time guard: Model satisfies tea.Model via pointer receiver
// (Update has pointer receiver, so *Model is the tea.Model type).
// / 编译期保险：Model 通过指针接收者满足 tea.Model（Update 是指针
// 接收者，所以 *Model 才是 tea.Model 类型）。
var _ tea.Model = (*Model)(nil)

// Ensure lipgloss import stays used even if helpers move later.
// / 即便后续迁移辅助函数也保留 lipgloss import 使用。
var _ = lipgloss.Color("")
