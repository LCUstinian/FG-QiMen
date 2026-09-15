// model_test.go — tests for ring buffers + flash lifecycle.
// / model_test.go — ring buffer + flash 生命周期测试。
package tui

import (
	"fmt"
	"testing"
	"time"
)

// TestEventRingBuffer_PushAndOrder verifies the ring buffer wraps
// and returns chronological order. Cap is eventCap (64, spec §10);
// pushing cap+5 must evict the 5 oldest.
// / 验证 ring buffer 回绕并返回时间顺序。cap 为 eventCap（64，
// spec §10）；推 cap+5 条必须淘汰最老 5 条。
func TestEventRingBuffer_PushAndOrder(t *testing.T) {
	m := Model{}
	const pushed = eventCap + 5
	for i := 0; i < pushed; i++ {
		m.pushEvent(eventEntry{
			Host: fmt.Sprintf("10.0.0.%d", i),
			Port: 80,
			Kind: "hit",
			At:   time.Unix(int64(i), 0),
		})
	}
	// Cap 64, pushed 69 → should hold events 5..68.
	// / cap 64，推 69 条 → 应保留事件 5..68。
	got := m.eventsOrdered()
	if len(got) != eventCap {
		t.Fatalf("len(eventsOrdered) = %d, want %d", len(got), eventCap)
	}
	// First should be 10.0.0.5, last should be 10.0.0.68.
	// / 首条应为 10.0.0.5，末条应为 10.0.0.68。
	if got[0].Host != "10.0.0.5" {
		t.Errorf("got[0].Host = %q, want 10.0.0.5", got[0].Host)
	}
	if last := got[eventCap-1]; last.Host != fmt.Sprintf("10.0.0.%d", pushed-1) {
		t.Errorf("got[%d].Host = %q, want 10.0.0.%d", eventCap-1, last.Host, pushed-1)
	}
	if !m.eventsFull {
		t.Error("eventsFull = false after pushing past cap, want true")
	}
}

// TestEventRingBuffer_LessThanCap verifies no wrap when fewer
// events than cap. / 验证事件数少于 cap 时不回绕。
func TestEventRingBuffer_LessThanCap(t *testing.T) {
	m := Model{}
	for i := 0; i < 5; i++ {
		m.pushEvent(eventEntry{Host: fmt.Sprintf("h%d", i), Kind: "hit"})
	}
	got := m.eventsOrdered()
	if len(got) != 5 {
		t.Errorf("len = %d, want 5", len(got))
	}
	if m.eventsFull {
		t.Error("eventsFull = true after only 5 pushes, want false")
	}
}

// TestRateRingBuffer verifies rate sample ring buffer wrap.
// / 验证 rate sample ring buffer 回绕。
func TestRateRingBuffer(t *testing.T) {
	m := Model{}
	for i := 0; i < 70; i++ {
		m.recordRate(float64(i))
	}
	got := m.rateOrdered()
	if len(got) != 60 {
		t.Fatalf("len(rateOrdered) = %d, want 60", len(got))
	}
	// Last 60 of 0..69 → 10..69.
	if got[0] != 10 {
		t.Errorf("got[0] = %f, want 10", got[0])
	}
	if got[59] != 69 {
		t.Errorf("got[59] = %f, want 69", got[59])
	}
}

// TestPushEvent_SetsFlash verifies hits set a flash expiry in the map.
// / 验证 hits 在 map 里设置 flash 过期。
func TestPushEvent_SetsFlash(t *testing.T) {
	m := Model{}
	before := time.Now()
	m.pushEvent(eventEntry{Host: "1.2.3.4", Port: 80, Kind: "hit"})
	after := time.Now()

	key := "1.2.3.4:80"
	until, ok := m.flashUntil[key]
	if !ok {
		t.Fatalf("flashUntil[%q] not set after hit push", key)
	}
	// Expiry must be ~200ms from now.
	want := 200 * time.Millisecond
	delta := until.Sub(before)
	if delta < want-50*time.Millisecond || delta > want+50*time.Millisecond {
		t.Errorf("flash expiry delta = %v, want ~%v", delta, want)
	}
	_ = after
}

// TestPruneExpiredFlashes verifies expired entries are removed.
// / 验证过期条目被删除。
func TestPruneExpiredFlashes(t *testing.T) {
	m := Model{}
	m.flashUntil = map[string]time.Time{
		"a:1": time.Now().Add(1 * time.Second),  // live
		"b:2": time.Now().Add(-1 * time.Second), // expired
	}
	m.pruneExpiredFlashes(time.Now())
	if _, ok := m.flashUntil["a:1"]; !ok {
		t.Error("live entry a:1 was pruned, want kept")
	}
	if _, ok := m.flashUntil["b:2"]; ok {
		t.Error("expired entry b:2 still present, want pruned")
	}
}

// TestClearErrors verifies clearErrors resets state.
// / 验证 clearErrors 重置 state。
func TestClearErrors(t *testing.T) {
	m := Model{}
	m.errorsExpanded = true
	m.clearErrors()
	if m.errorsExpanded {
		t.Error("errorsExpanded = true after clearErrors(), want false")
	}
}

// TestNoteStatsBeat drives the §5.5 stall detector state machine: a
// done-count change re-baselines, a hold accumulates stallSec, and
// runIdle / runDone / paused suppress it (reset + baseline refresh, so
// a resume never alarms on pre-pause silence).
// / TestNoteStatsBeat 驱动 §5.5 停滞检测器状态机：done 计数变化即重
// 置基线，保持不变则累计 stallSec；runIdle / runDone / paused 抑制
// （清零 + 刷新基线，恢复后绝不因暂停前的静默告警）。
func TestNoteStatsBeat(t *testing.T) {
	base := time.Date(2026, 9, 15, 12, 0, 0, 0, time.Local)

	t.Run("hold accumulates and change rebaselines", func(t *testing.T) {
		m := newTestModel()
		m.runState = runScanning
		m.noteStatsBeat(base, 100)
		m.noteStatsBeat(base.Add(time.Second), 100)
		if m.stallSec != 1 {
			t.Fatalf("hold: stallSec = %d, want 1", m.stallSec)
		}
		m.noteStatsBeat(base.Add(10*time.Second), 142) // ports moved
		if m.stallSec != 0 || m.lastPorts != 142 {
			t.Fatalf("change: stallSec = %d lastPorts = %d, want 0 / 142", m.stallSec, m.lastPorts)
		}
	})

	t.Run("suppressed while idle/done/paused", func(t *testing.T) {
		for _, setup := range []func(*Model){
			func(m *Model) { m.runState = runIdle },
			func(m *Model) { m.runState = runDone },
			func(m *Model) { m.runState = runScanning; m.uiMode = modePaused },
		} {
			m := newTestModel()
			setup(&m)
			m.noteStatsBeat(base, 100)
			m.noteStatsBeat(base.Add(90*time.Second), 100) // long silence
			if m.stallSec != 0 {
				t.Fatalf("suppressed state: stallSec = %d, want 0", m.stallSec)
			}
		}
	})

	t.Run("resume after pause starts from fresh baseline", func(t *testing.T) {
		m := newTestModel()
		m.runState = runScanning
		m.noteStatsBeat(base, 100)
		m.noteStatsBeat(base.Add(5*time.Second), 100) // 5s pre-pause stall
		m.uiMode = modePaused
		m.noteStatsBeat(base.Add(8*time.Second), 100) // paused beat resets
		m.uiMode = modeRun
		m.noteStatsBeat(base.Add(9*time.Second), 100) // resumed, still quiet
		if m.stallSec != 1 {
			t.Fatalf("post-resume stallSec = %d, want 1 (only post-resume silence counts)", m.stallSec)
		}
	})
}
