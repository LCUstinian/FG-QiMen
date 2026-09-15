// hub_test.go — tests for the Telemetry Hub (spec §3.2): write-time
// cleaning, drop counting, per-beat coalescing, storm debounce, and
// the model-side ingestion contract (ring-64 guard + critical sidecar
// fidelity). The hub goroutine loop itself is only smoke-tested via
// ctx cancel — the deterministic behavior lives in flush(), which Run
// calls on every beat.
// / hub_test.go — Telemetry Hub 测试（spec §3.2）：写时清洗、丢弃计
// 数、每拍合帧、风暴防抖，以及 model 侧摄入契约（ring-64 守卫 +
// critical 侧车保真）。hub goroutine 循环本身只做 ctx 取消冒烟——
// 确定性行为都在 flush() 里，Run 每拍调它。
package tui

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// ── 写时清洗 cleanText ──

// TestCleanText — the 写时清洗 gate: ANSI CSI/OSC sequences stripped,
// control bytes replaced with '·', lone ESC removed, hard cap at
// hubTextMax runes (rune-safe for CJK), empty string short-circuits.
// / cleanText 写时清洗门：剥 ANSI CSI/OSC 序列、控制字节换 '·'、去
// 孤立 ESC、硬截 hubTextMax rune（对 CJK 按 rune 安全截断）、空串
// 短路。
func TestCleanText(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"plain", "OpenSSH 9.0", "OpenSSH 9.0"},
		{"csi color", "\x1b[1;32mOK\x1b[0m", "OK"},
		{"csi erase", "\x1b[2Jhello", "hello"},
		{"osc bel", "\x1b]8;;http://x\x07link\x1b]8;;\x07", "link"},
		{"osc st", "\x1b]0;title\x1b\\tail", "tail"},
		{"lone esc", "a\x1bb", "ab"},
		{"control bytes", "a\x01b\x7fc", "a·b·c"},
		{"tab is control", "a\tb", "a·b"},
		{"cjk preserved", "中文服务", "中文服务"},
	}
	for _, c := range cases {
		if got := cleanText(c.in); got != c.want {
			t.Errorf("%s: cleanText(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
	// Truncation is rune-safe: 200 CJK runes must come back as exactly
	// hubTextMax runes (not bytes). / 截断按 rune：200 个中文字符回来
	// 必须恰好 hubTextMax 个 rune（不是字节）。
	long := strings.Repeat("中", 200)
	got := cleanText(long)
	if got := []rune(got); len(got) != hubTextMax {
		t.Errorf("cleanText(200 cjk) rune count = %d, want %d", len(got), hubTextMax)
	}
	// ASCII over-cap too. / ASCII 超限同理。
	if got := cleanText(strings.Repeat("x", 300)); len(got) != hubTextMax {
		t.Errorf("cleanText(300 ascii) len = %d, want %d", len(got), hubTextMax)
	}
}

// ── 入队 / 丢弃计数 ──

// collectHub returns a hub whose batches land in the returned slice
// (appended by the send closure, all under the test goroutine).
// / collectHub 返回一个 hub，batch 落进返回的切片（send 闭包追加，
// 全在测试 goroutine 上）。
func collectHub() (*Hub, *[]eventsBatchMsg) {
	var batches []eventsBatchMsg
	h := NewHub(func(m tea.Msg) {
		batches = append(batches, m.(eventsBatchMsg))
	})
	return h, &batches
}

// TestHub_DropCounting — overflowing the bounded queue must count as
// drops, never block: fill the queue to hubQueueCap, push 10 more
// (dropped), then one flush delivers exactly hubQueueCap entries with
// the drop delta, and a second flush is a clean no-op (dirty-flag
// cleared — no idle re-render).
// / TestHub_DropCounting — 有界队列溢出必须计为丢弃、绝不阻塞：灌
// 满队列到 hubQueueCap，再推 10 条（丢弃），一次 flush 恰好投递
// hubQueueCap 条 + 丢弃增量；第二次 flush 干净空操作（dirty-flag
// 清除——空闲不重渲染）。
func TestHub_DropCounting(t *testing.T) {
	h, batches := collectHub()
	for i := 0; i < hubQueueCap+10; i++ {
		h.Enqueue(rawEvent{kind: "hit", host: "10.0.0.1", port: 22, svc: "ssh"})
	}
	if got := h.dropped.Load(); got != 10 {
		t.Fatalf("dropped after overflow = %d, want 10", got)
	}
	h.flush()
	if len(*batches) != 1 {
		t.Fatalf("flush delivered %d batches, want 1", len(*batches))
	}
	b := (*batches)[0]
	if b.ingested != hubQueueCap {
		t.Errorf("batch ingested = %d, want %d (queue cap)", b.ingested, hubQueueCap)
	}
	if b.dropped != 10 {
		t.Errorf("batch dropped delta = %d, want 10", b.dropped)
	}
	if len(b.entries) != hubQueueCap {
		t.Errorf("batch entries = %d, want %d", len(b.entries), hubQueueCap)
	}
	// Second flush: queue empty, drop delta 0 → nothing delivered.
	// / 第二次 flush：队列空、丢弃增量为 0 → 不投递。
	h.flush()
	if len(*batches) != 1 {
		t.Errorf("idle flush delivered %d batches, want 1 (no re-render)", len(*batches))
	}
	// New events only → no phantom drop delta. / 只有新事件 → 无幻
	// 影丢弃增量。
	h.Enqueue(rawEvent{kind: "hit", host: "10.0.0.2", port: 80, svc: "http"})
	h.flush()
	if len(*batches) != 2 {
		t.Fatalf("second flush delivered %d batches, want 2", len(*batches))
	}
	if b := (*batches)[1]; b.dropped != 0 || b.ingested != 1 {
		t.Errorf("second batch dropped=%d ingested=%d, want 0/1", b.dropped, b.ingested)
	}
}

// TestHub_Flush_CleansFields — the hub, not the caller, cleans: dirty
// text in the queue comes out sanitized in the batch entries (写时清
// 洗 happens at flush).
// / TestHub_Flush_CleansFields — 清洗在 hub 侧：队列里的脏文本出队
// 时已净化（写时清洗发生在 flush）。
func TestHub_Flush_CleansFields(t *testing.T) {
	h, batches := collectHub()
	h.Enqueue(rawEvent{
		kind: "hit", host: "\x1b[31m10.0.0.1", port: 22,
		svc: "ssh", text: "banner\x01with\x1b[0mctrl",
	})
	h.flush()
	if len(*batches) != 1 {
		t.Fatalf("flush delivered %d batches, want 1", len(*batches))
	}
	e := (*batches)[0].entries[0]
	if e.Host != "10.0.0.1" {
		t.Errorf("Host = %q, want %q (ANSI stripped)", e.Host, "10.0.0.1")
	}
	if e.Text != "banner·withctrl" {
		t.Errorf("Text = %q, want %q", e.Text, "banner·withctrl")
	}
}

// TestHub_Run_ExitsOnCtxCancel — smoke test for the goroutine loop:
// Run returns promptly once ctx is cancelled.
// / TestHub_Run_ExitsOnCtxCancel — goroutine 循环冒烟：ctx 取消后
// Run 及时返回。
func TestHub_Run_ExitsOnCtxCancel(t *testing.T) {
	h, _ := collectHub()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		h.Run(ctx)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return within 2s of ctx cancel")
	}
}

// ── 风暴判定器 ──

// TestStormDetector_Debounce — spec §5.4 hysteresis: enter only after
// the full 2s window averages >500 ev/s (a single hot beat must NOT
// trip it), exit only after the full 5s window falls back to ≤500,
// and exactly 500 ev/s never enters (strict >).
// / TestStormDetector_Debounce — spec §5.4 防抖：完整 2s 窗口均值
// >500 才进入（单个热拍不得误触），完整 5s 窗口回落 ≤500 才退出，
// 恰好 500 ev/s 永不进入（严格大于）。
func TestStormDetector_Debounce(t *testing.T) {
	enterBeats := int(stormEnterHold / hubBeat) // 20
	exitBeats := int(stormExitHold / hubBeat)   // 50

	t.Run("not before 2s of history", func(t *testing.T) {
		var s stormDetector
		for i := 0; i < enterBeats-1; i++ {
			s.add(60) // 600 ev/s — over rate, but window not covered
		}
		if s.active {
			t.Fatal("storm entered before 2s of history")
		}
	})

	t.Run("enters at 2s sustained", func(t *testing.T) {
		var s stormDetector
		for i := 0; i < enterBeats; i++ {
			s.add(60)
		}
		if !s.active {
			t.Fatal("storm did not enter after 2s sustained at 600 ev/s")
		}
	})

	t.Run("boundary 500 never enters", func(t *testing.T) {
		var s stormDetector
		for i := 0; i < enterBeats*2; i++ {
			s.add(50) // exactly 500 ev/s — enter requires strictly >500
		}
		if s.active {
			t.Fatal("storm entered at exactly 500 ev/s (want strict >)")
		}
	})

	t.Run("exits only after 5s window", func(t *testing.T) {
		var s stormDetector
		for i := 0; i < enterBeats; i++ {
			s.add(60) // enter
		}
		if !s.active {
			t.Fatal("precondition: storm should be active")
		}
		// Rate drops to zero. Storm must persist until the 5s window
		// is fully covered, then release on the beat that completes
		// coverage (20 hot + 30 zero = 50 samples → avg 240 ≤ 500).
		// / 速率归零。风暴必须坚持到 5s 窗口覆盖满，在补满覆盖的
		// 那一拍退出（20 热 + 30 零 = 50 样本 → 均值 240 ≤ 500）。
		for i := 0; i < exitBeats-enterBeats-1; i++ {
			s.add(0)
			if !s.active {
				t.Fatalf("storm exited before 5s window covered (beat %d)", i)
			}
		}
		s.add(0)
		if s.active {
			t.Fatal("storm did not exit after 5s window averaged ≤500")
		}
	})
}

// TestStormDetector_Rate — the 1s rate readout math: two beats of 60
// and 30 events average (60+30)*10/2 = 450 ev/s over the available
// prefix.
// / TestStormDetector_Rate — 1s 速率读数数学：60 + 30 两拍在已有前
// 缀上均值 (60+30)*10/2 = 450 ev/s。
func TestStormDetector_Rate(t *testing.T) {
	var s stormDetector
	s.add(60)
	s.add(30)
	if got := s.rate(); got != 450 {
		t.Errorf("rate = %d, want 450", got)
	}
}

// ── model 侧摄入契约 ──

// TestAppendBatch_Ring64Guard — the main ring never grows past
// eventCap (64): 5×cap pushes leave exactly 64 entries, chronological
// with the newest last, and IDs stay monotonic across wrap.
// / TestAppendBatch_Ring64Guard — 主环永不超 eventCap（64）：推
// 5×cap 条后恰剩 64 条，按时间序最新在末尾，ID 跨回绕保持单调。
func TestAppendBatch_Ring64Guard(t *testing.T) {
	m := NewModel(nil)
	const total = eventCap * 5
	for i := 0; i < total; i++ {
		m.appendBatch([]eventEntry{{
			Host: fmt.Sprintf("10.0.0.%d", i), Port: 22, Service: "ssh",
			Kind: "hit", At: time.Now(),
		}}, 1, 0)
	}
	got := m.eventsOrdered()
	if len(got) != eventCap {
		t.Fatalf("len(eventsOrdered) = %d, want %d (ring cap)", len(got), eventCap)
	}
	wantHost := fmt.Sprintf("10.0.0.%d", total-1)
	if last := got[len(got)-1]; last.Host != wantHost {
		t.Errorf("newest host = %q, want %q", last.Host, wantHost)
	}
	if got[m.eventsCap-1].ID-got[0].ID != uint64(eventCap-1) {
		t.Errorf("IDs not contiguous across wrap: first=%d last=%d",
			got[0].ID, got[m.eventsCap-1].ID)
	}
	if m.ingested != total {
		t.Errorf("ingested = %d, want %d", m.ingested, total)
	}
}

// TestAppendBatch_SidecarFidelity — severity-faithful kinds
// (cred_success / critical_hit / warn) land in the critical sidecar
// while plain hits do not; and when a storm flushes the main ring,
// the sidecar still holds the credential rows (spec §3.2 保真).
// / TestAppendBatch_SidecarFidelity — 严重度保真 kind（cred_success
// / critical_hit / warn）进 critical 侧车，普通 hit 不进；风暴冲刷
// 主环后侧车仍持有凭据行（spec §3.2 保真）。
func TestAppendBatch_SidecarFidelity(t *testing.T) {
	m := NewModel(nil)
	cred := eventEntry{Host: "10.0.0.5", Port: 22, Service: "ssh",
		Kind: "cred_success", At: time.Now()}
	hit := eventEntry{Host: "10.0.0.6", Port: 80, Service: "http",
		Kind: "hit", At: time.Now()}
	warn := eventEntry{Host: "10.0.0.7", Port: 445, Service: "smb",
		Kind: "warn", At: time.Now()}
	m.appendBatch([]eventEntry{cred, hit, warn}, 3, 0)

	crit := m.critOrdered()
	if len(crit) != 2 {
		t.Fatalf("sidecar len = %d, want 2 (cred + warn, hit excluded)", len(crit))
	}
	if crit[0].Kind != "cred_success" || crit[1].Kind != "warn" {
		t.Errorf("sidecar kinds = [%s %s], want [cred_success warn]",
			crit[0].Kind, crit[1].Kind)
	}
	// Storm flush: 200 plain hits overwrite the main ring many times
	// over; the cred row must survive in the sidecar.
	// / 风暴冲刷：200 条普通 hit 反复覆写主环；凭据行必须在侧车存
	// 活。
	storm := make([]eventEntry, 200)
	for i := range storm {
		storm[i] = eventEntry{Host: "10.0.0.9", Port: 1, Service: "svc",
			Kind: "hit", At: time.Now()}
	}
	m.appendBatch(storm, 200, 0)
	crit = m.critOrdered()
	found := false
	for _, e := range crit {
		if e.Kind == "cred_success" && e.Host == "10.0.0.5" {
			found = true
		}
	}
	if !found {
		t.Error("cred row lost from sidecar after storm flush")
	}
	if m.ingested != 203 || m.dropped != 0 {
		t.Errorf("counters = (%d in, %d dropped), want (203, 0)",
			m.ingested, m.dropped)
	}
}

// ── dispatcher 接线 ──

// TestDispatcher_EventsBatchMsg — the dispatcher's hub branch: batch
// lands via appendBatch, storm snapshot mirrors onto the model, and
// the first batch promotes runState from idle to scanning.
// / TestDispatcher_EventsBatchMsg — dispatcher 的 hub 分支：batch 经
// appendBatch 落地，风暴快照镜像到 model，首条 batch 把 runState 从
// idle 提升为 scanning。
func TestDispatcher_EventsBatchMsg(t *testing.T) {
	m := NewModel(nil)
	d := dispatcher{inner: &m}
	batch := eventsBatchMsg{
		entries: []eventEntry{
			{Host: "10.0.0.1", Port: 22, Service: "ssh", Kind: "hit", At: time.Now()},
			{Host: "10.0.0.2", Port: 3389, Service: "rdp", Kind: "cred_success", At: time.Now()},
		},
		ingested:  2,
		dropped:   1,
		storm:     true,
		stormRate: 620,
	}
	newM, cmd := d.Update(batch)
	if cmd != nil {
		t.Errorf("eventsBatchMsg returned non-nil cmd: %v", cmd)
	}
	inner := newM.(dispatcher).inner
	if inner.ingested != 2 || inner.dropped != 1 {
		t.Errorf("counters = (%d in, %d dropped), want (2, 1)",
			inner.ingested, inner.dropped)
	}
	if !inner.storm || inner.stormRate != 620 {
		t.Errorf("storm mirror = (%v, %d), want (true, 620)", inner.storm, inner.stormRate)
	}
	if inner.runState != runScanning {
		t.Errorf("runState = %v, want runScanning", inner.runState)
	}
	if got := len(inner.eventsOrdered()); got != 2 {
		t.Errorf("ring len = %d, want 2", got)
	}
	if got := len(inner.critOrdered()); got != 1 {
		t.Errorf("sidecar len = %d, want 1", got)
	}
}

// TestDispatcher_EventsBatchMsg_PausedDrops — paused mode drops the
// whole batch on the floor (freeze display): no ring growth, no
// counter change, no storm mirror.
// / TestDispatcher_EventsBatchMsg_PausedDrops — 暂停态整批丢弃（冻
// 结显示）：ring 不增长、计数不变、风暴不镜像。
func TestDispatcher_EventsBatchMsg_PausedDrops(t *testing.T) {
	m := NewModel(nil)
	m.uiMode = modePaused
	d := dispatcher{inner: &m}
	batch := eventsBatchMsg{
		entries: []eventEntry{
			{Host: "10.0.0.1", Port: 22, Service: "ssh", Kind: "hit", At: time.Now()},
		},
		ingested:  1,
		dropped:   2,
		storm:     true,
		stormRate: 999,
	}
	d.Update(batch)
	if m.ingested != 0 || m.dropped != 0 {
		t.Errorf("paused batch mutated counters: (%d, %d), want (0, 0)",
			m.ingested, m.dropped)
	}
	if m.storm || m.stormRate != 0 {
		t.Errorf("paused batch mirrored storm: (%v, %d)", m.storm, m.stormRate)
	}
	if got := len(m.eventsOrdered()); got != 0 {
		t.Errorf("paused batch grew ring: %d entries", got)
	}
}
