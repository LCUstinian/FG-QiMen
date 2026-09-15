// hub.go — Telemetry Hub (spec §3.2): the single ingestion point
// between the scan pipeline and the Bubbletea event loop.
//
// Pipeline goroutines call Enqueue (non-blocking, cap 256; overflow is
// counted as dropped, never blocks the scan). One hub goroutine drains
// the queue on the 100ms render beat, cleans every field (ANSI strip,
// control bytes, text capped at 160 runes — 写时清洗), runs the storm
// detector, and delivers ONE eventsBatchMsg per beat — the dirty-flag
// coalescing that keeps the frame rate ≤10fps regardless of the event
// rate. Concurrency contract: the hub goroutine only cleans and
// delivers; it never touches Model state. All Model mutations stay in
// the Bubbletea Update goroutine.
//
// hub.go — Telemetry Hub（spec §3.2）：扫描管线与 Bubbletea 事件循环
// 之间的唯一摄入点。
//
// 管线 goroutine 调 Enqueue（非阻塞，cap 256；满了计入丢弃，绝不阻
// 塞扫描）。唯一的 hub goroutine 在 100ms 渲染拍上排空队列，清洗每
// 个字段（剥 ANSI、控制字节、text 截 160 rune——写时清洗），跑风暴
// 判定，每拍只投递一条 eventsBatchMsg——这正是 dirty-flag 合帧，让
// 帧率与事件率解耦、恒 ≤10fps。并发契约：hub goroutine 只做清洗与
// 投递，绝不碰 Model 状态；所有 Model 突变都留在 Bubbletea Update
// goroutine 内。
package tui

import (
	"context"
	"regexp"
	"sync/atomic"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
)

// Hub constants (spec §10 defaults). / Hub 常量（spec §10 默认值）。
const (
	// hubQueueCap bounds the ingest queue. Overflow drops (counted),
	// it never blocks the pipeline. / hubQueueCap 限制摄入队列。溢出
	// 丢弃（计数），绝不阻塞管线。
	hubQueueCap = 256
	// hubBeat is the delivery cadence: one batch per beat caps the
	// render rate at 10fps. / hubBeat 是投递节拍：每拍一条 batch，
	// 渲染率封顶 10fps。
	hubBeat = 100 * time.Millisecond
	// hubTextMax caps cleaned evidence text (runes). / hubTextMax 限
	// 制清洗后证据文本的长度（rune）。
	hubTextMax = 160
	// stormEnterRate / stormEnterHold / stormExitHold are the storm
	// detector's spec §5.4 thresholds: enter when the 2s-average rate
	// exceeds 500 ev/s, exit when the 5s-average falls back to ≤500.
	// / stormEnterRate / stormEnterHold / stormExitHold 是风暴判定器
	// 的 spec §5.4 阈值：2s 均值超 500 ev/s 进入，5s 均值回落 ≤500
	// 退出。
	stormEnterRate = 500
	stormEnterHold = 2 * time.Second
	stormExitHold  = 5 * time.Second
)

// rawEvent is one pipeline event waiting in the hub queue. Fields are
// raw (uncleaned) — cleaning happens on the hub goroutine at flush.
// / rawEvent 是 hub 队列里等待的一条管线事件。字段是原始（未清洗）
// 的——清洗在 hub goroutine 的 flush 时进行。
type rawEvent struct {
	kind string // "hit" | "cred_success"（source 处判定）
	host string
	port int
	svc  string
	text string
	at   time.Time
}

// eventsBatchMsg is the hub's per-beat delivery: one message carries
// everything ingested since the last beat plus the drop delta and the
// storm snapshot. One batch = at most one extra render per beat, so
// the frame rate decouples from the event rate (spec §3.1).
// / eventsBatchMsg 是 hub 每拍的投递：一条消息携带自上一拍以来摄入
// 的全部事件、丢弃增量与风暴快照。一条 batch = 每拍至多一次额外渲
// 染，帧率因此与事件率解耦（spec §3.1）。
type eventsBatchMsg struct {
	entries   []eventEntry
	ingested  int  // events delivered in this batch / 本批投递的事件数
	dropped   int  // drop delta since the last batch / 距上批的丢弃增量
	storm     bool // storm detector state / 风暴判定器状态
	stormRate int  // current ev/s (1s window) / 当前 ev/s（1s 窗口）
}

// Hub owns the ingest queue and the storm detector. Enqueue is safe
// for concurrent use (pipeline goroutines); Run must be called exactly
// once, on its own goroutine.
// / Hub 拥有摄入队列与风暴判定器。Enqueue 并发安全（管线
// goroutine 调用）；Run 恰好调用一次，跑在独立 goroutine 上。
type Hub struct {
	queue chan rawEvent
	// dropped counts enqueue-side overflows. Atomic: incremented from
	// pipeline goroutines, read on the hub beat. / dropped 记入队侧
	// 溢出。原子量：管线 goroutine 递增，hub 拍读取。
	dropped atomic.Int64
	// droppedSent mirrors how many drops have already been delivered
	// in a batch, so each beat sends only the delta. / droppedSent
	// 记录已随 batch 投递过的丢弃数，每拍只发增量。
	droppedSent int
	// send injects the batch into the Bubbletea loop. / send 把
	// batch 注入 Bubbletea 循环。
	send func(tea.Msg)
	// storm is the sliding-window detector (hub-local state, not
	// Model state). / storm 是滑动窗判定器（hub 本地状态，非 Model
	// 状态）。
	storm stormDetector
}

// NewHub constructs a Hub that delivers batches through send.
// / NewHub 构造一个经 send 投递 batch 的 Hub。
func NewHub(send func(tea.Msg)) *Hub {
	return &Hub{
		queue: make(chan rawEvent, hubQueueCap),
		send:  send,
	}
}

// Enqueue hands a raw event to the hub. Non-blocking: a full queue
// counts as a drop (spec §3.2 — silent loss is forbidden, counted
// loss is fine). / Enqueue 把原始事件交给 hub。非阻塞：队列满计为
// 丢弃（spec §3.2——禁止静默丢失，计数丢失可以）。
func (h *Hub) Enqueue(ev rawEvent) {
	select {
	case h.queue <- ev:
	default:
		h.dropped.Add(1)
	}
}

// Run drives the hub until ctx is cancelled. Call once on its own
// goroutine. / Run 驱动 hub 直到 ctx 取消。独立 goroutine 上调用一
// 次。
func (h *Hub) Run(ctx context.Context) {
	ticker := time.NewTicker(hubBeat)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			h.flush()
		}
	}
}

// flush drains the queue, cleans each event, updates the storm
// detector, and delivers one batch. With an empty queue and no new
// drops it delivers nothing — the dirty-flag that keeps idle scans
// from re-rendering. / flush 排空队列、清洗每个事件、更新风暴判定
// 器并投递一条 batch。队列空且无新丢弃时什么都不投——这就是让空
// 闲扫描不重渲染的 dirty-flag。
func (h *Hub) flush() {
	n := len(h.queue)
	droppedTotal := int(h.dropped.Load())
	droppedDelta := droppedTotal - h.droppedSent
	if n == 0 && droppedDelta == 0 {
		return // dirty-flag clear / dirty-flag 未置位
	}
	entries := make([]eventEntry, 0, n)
	for i := 0; i < n; i++ {
		ev := <-h.queue
		entries = append(entries, eventEntry{
			Host:    cleanText(ev.host),
			Port:    ev.port,
			Service: cleanText(ev.svc),
			Kind:    ev.kind,
			Text:    cleanText(ev.text),
			At:      ev.at,
		})
	}
	h.storm.add(n)
	h.droppedSent = droppedTotal
	h.send(eventsBatchMsg{
		entries:   entries,
		ingested:  n,
		dropped:   droppedDelta,
		storm:     h.storm.active,
		stormRate: h.storm.rate(),
	})
}

// ── 写时清洗 (spec §5.4 / §7.1) ──

// csiRe matches ANSI CSI sequences (cursor moves, SGR colors, etc.).
// / csiRe 匹配 ANSI CSI 序列（光标移动、SGR 颜色等）。
// [\x40-\x7e] is the ECMA-48 CSI final-byte range (0x40–0x7E), not a
// typo — hence the nolint. / [\x40-\x7e] 是 ECMA-48 CSI 终止字节范围
// （0x40–0x7E），非笔误——故加 nolint。
var csiRe = regexp.MustCompile("\x1b\\[[0-9;:?<=>!$\"'#&()*+,\\-./]*[\x40-\x7e]") //nolint:gocritic // ECMA-48 final-byte range / ECMA-48 终止字节范围

// oscRe matches ANSI OSC sequences (title setting, hyperlinks) in
// both BEL- and ST-terminated forms. / oscRe 匹配 ANSI OSC 序列（设
// 置标题、超链接），兼容 BEL 与 ST 两种终止形式。
var oscRe = regexp.MustCompile("\x1b\\][^\x07\x1b]*(?:\x07|\x1b\\\\?)")

// cleanText strips ANSI escapes, replaces control bytes with '·',
// and hard-caps the result at hubTextMax runes. This is the 写时清洗
// gate: the render layer never sees a dirty byte, so display stays
// O(1) and the frame purity law holds by construction.
// / cleanText 剥 ANSI 转义、控制字节替换为 '·'，并把结果硬截到
// hubTextMax 个 rune。这就是写时清洗门：渲染层永远见不到脏字节，
// 显示保持 O(1)，帧纯度律由构造保证。
func cleanText(s string) string {
	if s == "" {
		return ""
	}
	s = oscRe.ReplaceAllString(s, "")
	s = csiRe.ReplaceAllString(s, "")
	runes := make([]rune, 0, len(s))
	for _, r := range s {
		switch {
		case r == 0x1b:
			continue // lone ESC leftover / 孤立的 ESC 残留
		case r < 0x20 || r == 0x7f || unicode.IsControl(r):
			r = '·' // control byte → middle dot / 控制字节 → 间隔号
		}
		runes = append(runes, r)
	}
	if len(runes) > hubTextMax {
		runes = runes[:hubTextMax]
	}
	return string(runes)
}

// ── 风暴判定器 (spec §5.4) ──

// stormSamples is the sliding-window length: 60 × 100ms = 6s of
// history, enough for both the 2s enter window and the 5s exit
// window. / stormSamples 是滑动窗长度：60 × 100ms = 6s 历史，同时
// 覆盖 2s 进入窗与 5s 退出窗。
const stormSamples = 60

// stormDetector converts per-beat ingest counts into a debounced
// storm state: enter when the 2s-average rate exceeds stormEnterRate,
// exit when the 5s-average falls back to ≤ it. Debouncing via two
// different windows prevents enter/exit flicker (spec §5.4).
// / stormDetector 把每拍摄入计数转成防抖的风暴状态：2s 均值超
// stormEnterRate 进入，5s 均值回落 ≤ 该值退出。双窗口防抖避免
// enter/exit 闪烁（spec §5.4）。
type stormDetector struct {
	samples [stormSamples]int64 // per-beat ingest counts / 每拍摄入计数
	head    int
	full    bool
	active  bool
}

// add records one beat's ingest count. / add 记录一拍的摄入计数。
func (s *stormDetector) add(n int) {
	s.samples[s.head] = int64(n)
	s.head = (s.head + 1) % stormSamples
	if s.head == 0 {
		s.full = true
	}
	s.update()
}

// windowAvg returns the ev/s average over the last d of history.
// / windowAvg 返回最近 d 历史内的 ev/s 均值。
func (s *stormDetector) windowAvg(d time.Duration) int {
	available := stormSamples
	if !s.full {
		available = s.head
	}
	n := int(d / hubBeat)
	if n > available {
		n = available
	}
	if n <= 0 {
		return 0
	}
	var sum int64
	for i := 0; i < n; i++ {
		idx := (s.head - 1 - i + stormSamples) % stormSamples
		sum += s.samples[idx]
	}
	// Each sample covers hubBeat; convert the sum to ev/s by scaling
	// one beat up to a second. / 每个样本覆盖一个 hubBeat；把和按一
	// 拍换算到一秒。
	const beatsPerSec = int(time.Second / hubBeat) // 10
	return int(sum * int64(beatsPerSec) / int64(n))
}

// rate is the current ev/s over the last 1s. / rate 是最近 1s 的当
// 前 ev/s。
func (s *stormDetector) rate() int { return s.windowAvg(time.Second) }

// covered reports whether at least d of history has accumulated.
// Without this gate windowAvg clamps to the available prefix, so a
// single 100ms beat over the rate would trip the "2s sustained" enter
// rule (spec §5.4: 持续 2s). Both transitions require their full
// window to exist before the average is consulted.
// / covered 报告历史是否已积累满 d。没有这道门 windowAvg 会钳到
// available 前缀，单个 100ms 拍超速就会误触"持续 2s"的进入规则
// （spec §5.4）。两个转换都必须等完整窗口存在后才查均值。
func (s *stormDetector) covered(d time.Duration) bool {
	available := stormSamples
	if !s.full {
		available = s.head
	}
	return available >= int(d/hubBeat)
}

// update recomputes the debounced storm state after each sample.
// / update 在每个样本后重算防抖风暴状态。
func (s *stormDetector) update() {
	if !s.active {
		if s.covered(stormEnterHold) && s.windowAvg(stormEnterHold) > stormEnterRate {
			s.active = true
		}
		return
	}
	if s.covered(stormExitHold) && s.windowAvg(stormExitHold) <= stormEnterRate {
		s.active = false
	}
}
