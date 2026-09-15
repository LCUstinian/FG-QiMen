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

// eventEntry is one row in the LIVE EVENTS panel (spec §7.1).
// / eventEntry 是 LIVE EVENTS 面板的一行（spec §7.1）。
type eventEntry struct {
	Host    string
	Port    int
	Service string
	Kind    string // "hit" | "miss" | "cred_success" | "warn" | "critical_hit"
	At      time.Time
	// Text is the evidence snippet (credential pair, banner summary).
	// Cleaned + redacted at the hub (写时清洗) — the render layer
	// never mutates it. / Text 是证据片段（凭据对、banner 摘要）。
	// 在 hub 完成清洗 + redact（写时清洗）——渲染层从不改它。
	Text string
	// ID is a monotonic sequence assigned at push time; browse-mode
	// anchoring (T2) uses it. ×N same-source merging is a render-layer
	// view, never stored. / ID 是 push 时赋予的单调序号；browse 模式
	// 锚定（T2）用。×N 同源合并是渲染层派生视图，不入结构体。
	ID uint64
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

	// ── v0.10.0 T1: Telemetry Hub contracts (spec §3.2/§7.2) ──
	// v0.10.0 T1：Telemetry Hub 契约（spec §3.2/§7.2）。

	// critBuf is the critical sidecar ring (cap critSidecarCap):
	// cred_success / critical_hit / warn rows survive main-ring
	// flushes during event storms. Same ring discipline as eventsBuf.
	// / critBuf 是 critical 侧车环（cap critSidecarCap）：事件风暴
	// 冲刷主环时，cred_success / critical_hit / warn 行仍可在侧车
	// 中追溯。与 eventsBuf 同一套环形纪律。
	critBuf []eventEntry

	// critCap is cap(critBuf). / critCap 是 cap(critBuf)。
	critCap int

	// critHead is the next write index into critBuf. / critHead 是
	// critBuf 下一个写入索引。
	critHead int

	// critFull is true once critBuf has wrapped. / critFull 在
	// critBuf 回绕后为 true。
	critFull bool

	// ingested / dropped are the cumulative hub counters shown in the
	// EVENTS title row (dropped>0 must surface — silent loss is
	// forbidden). / ingested / dropped 是 EVENTS 标题行显示的 hub
	// 累计计数（dropped>0 必须上屏——禁止静默丢失）。
	ingested int
	dropped  int

	// storm / stormRate mirror the hub's storm detector: active flag
	// + current ev/s. Display (storm summary row) lands in T2.
	// / storm / stormRate 镜像 hub 的风暴判定器：激活标志 + 当前
	// ev/s。显示（风暴汇总行）在 T2 落地。
	storm     bool
	stormRate int

	// nextEventID is the monotonic ID source for pushEvent. Kept on
	// the model so all mutations stay in the Update goroutine.
	// / nextEventID 是 pushEvent 的单调 ID 源。放在 model 上让所有
	// 突变都留在 Update goroutine 内。
	nextEventID uint64

	// showLiveOverlay is the narrow-mode 'L' toggle: when the
	// events region is hidden (height=0), the overlay reveals the
	// last 5 events anyway. / showLiveOverlay 是 narrow 模式的 'L'
	// 开关：events 区域被隐藏（height=0）时，overlay 强制显示
	// 最近 5 条。
	showLiveOverlay bool

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

// eventCap is the fixed size of the main event ring buffer (spec §10:
// 64). Package-level so tests can assert the cap contract.
// / eventCap 是主事件 ring buffer 的固定大小（spec §10：64）。放在
// 包级让测试能断言 cap 契约。
const eventCap = 64

// critSidecarCap is the fixed size of the critical sidecar ring
// (spec §3.2: 8). During storms the main ring flushes, but credential
// rows stay recoverable here. / critSidecarCap 是 critical 侧车环的
// 固定大小（spec §3.2：8）。风暴冲刷主环时，凭据行仍可在此追溯。
const critSidecarCap = 8

// ── v0.7.0 helpers ──

// pushEvent appends to the main event ring buffer, assigning the
// monotonic ID. If Kind is a hit-family kind ("hit" / "critical_hit" /
// "cred_success"), sets a 200ms flash expiry keyed by host:port so the
// row paints red for one render cycle regardless of its underlying
// severity. / pushEvent 追加到主事件 ring buffer 并赋单调 ID。Kind
// 是 hit 类（"hit" / "critical_hit" / "cred_success"）时设 200ms
// flash 过期，以 host:port 为键让该行在一帧内画红，不管底层
// severity。
func (m *Model) pushEvent(e eventEntry) {
	if m.eventsBuf == nil {
		// First push: lazy-init the ring buffer. / 首次 push：懒初始化
		// ring buffer。
		m.eventsBuf = make([]eventEntry, eventCap)
		m.eventsCap = eventCap
	}
	m.nextEventID++
	e.ID = m.nextEventID
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

// isCriticalKind returns true for the severity-faithful kinds that
// must also land in the critical sidecar (spec §3.2).
// / isCriticalKind 对必须同时进入 critical 侧车的严重度保真 kind
// 返回 true（spec §3.2）。
func isCriticalKind(kind string) bool {
	switch kind {
	case "cred_success", "critical_hit", "warn":
		return true
	}
	return false
}

// pushCritical appends to the critical sidecar ring. / pushCritical
// 追加到 critical 侧车环。
func (m *Model) pushCritical(e eventEntry) {
	if m.critBuf == nil {
		m.critBuf = make([]eventEntry, critSidecarCap)
		m.critCap = critSidecarCap
	}
	m.critBuf[m.critHead] = e
	m.critHead = (m.critHead + 1) % m.critCap
	if m.critHead == 0 {
		m.critFull = true
	}
}

// critOrdered iterates the critical sidecar chronologically (oldest →
// newest). / critOrdered 按时间顺序迭代 critical 侧车。
func (m *Model) critOrdered() []eventEntry {
	if m.critBuf == nil {
		return nil
	}
	if !m.critFull {
		return m.critBuf[:m.critHead]
	}
	out := make([]eventEntry, 0, m.critCap)
	out = append(out, m.critBuf[m.critHead:]...)
	out = append(out, m.critBuf[:m.critHead]...)
	return out
}

// appendBatch is the single ingestion path from the Telemetry Hub
// (spec §3.2): counters accumulate, entries land in the main ring,
// and severity-faithful kinds land in the critical sidecar. The
// dispatcher is the only production caller; all mutations stay in the
// Bubbletea Update goroutine (single-writer contract).
// / appendBatch 是 Telemetry Hub 的唯一摄入路径（spec §3.2）：计数
// 累加，条目进主环，严重度保真 kind 进 critical 侧车。dispatcher
// 是唯一生产调用方；所有突变都留在 Bubbletea Update goroutine 内
// （单写者契约）。
func (m *Model) appendBatch(entries []eventEntry, ingested, dropped int) {
	m.ingested += ingested
	m.dropped += dropped
	for _, e := range entries {
		m.pushEvent(e)
		if isCriticalKind(e.Kind) {
			m.pushCritical(e)
		}
	}
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
// Wired by the dispatcher's eventMsg case (the production runner→TUI
// path) and available to tests constructing entries from raw fields.
// / toEntry 把 runner.Event 形态的调用转成 eventEntry。由
// dispatcher 的 eventMsg case（生产 runner→TUI 路径）接线，测试也
// 可用它从原始字段构造条目。
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
