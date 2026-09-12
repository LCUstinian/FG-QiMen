// model_test.go — tests for ring buffers + flash lifecycle.
// / model_test.go — ring buffer + flash 生命周期测试。
package tui

import (
	"fmt"
	"testing"
	"time"
)

// TestEventRingBuffer_PushAndOrder verifies the ring buffer wraps
// and returns chronological order. / 验证 ring buffer 回绕并返回
// 时间顺序。
func TestEventRingBuffer_PushAndOrder(t *testing.T) {
	m := Model{}
	for i := 0; i < 25; i++ {
		m.pushEvent(eventEntry{
			Host: fmt.Sprintf("10.0.0.%d", i),
			Port: 80,
			Kind: "hit",
			At:   time.Unix(int64(i), 0),
		})
	}
	// Cap 20, pushed 25 → should hold events 5..24.
	got := m.eventsOrdered()
	if len(got) != 20 {
		t.Fatalf("len(eventsOrdered) = %d, want 20", len(got))
	}
	// First should be 10.0.0.5, last should be 10.0.0.24.
	if got[0].Host != "10.0.0.5" {
		t.Errorf("got[0].Host = %q, want 10.0.0.5", got[0].Host)
	}
	if got[19].Host != "10.0.0.24" {
		t.Errorf("got[19].Host = %q, want 10.0.0.24", got[19].Host)
	}
	if !m.eventsFull {
		t.Error("eventsFull = false after 25 pushes into cap-20, want true")
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
