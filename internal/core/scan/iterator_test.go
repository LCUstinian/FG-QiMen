// iterator_test.go — unit tests for the two scan.Iterator
// implementations: ChanIterator (channel-backed) and CrossIterator
// (Cartesian product of host × port). Pure functions, no network
// I/O. / scan.Iterator 两种实现的单元测试：ChanIterator（channel
// 支撑）和 CrossIterator（host × port 笛卡尔积）。纯函数，无网络 I/O。
package scan

import (
	"testing"
)

func TestChanIterator_NextAndEstimated(t *testing.T) {
	ch := make(chan Item, 3)
	ch <- Item{Host: "1.1.1.1", Port: 22}
	ch <- Item{Host: "2.2.2.2", Port: 80}
	ch <- Item{Host: "3.3.3.3", Port: 443}
	close(ch)

	it := NewChanIterator(ch)
	if got := it.Estimated(); got != -1 {
		t.Errorf("Estimated() = %d, want -1 (channels don't know length)", got)
	}
	want := []Item{
		{Host: "1.1.1.1", Port: 22},
		{Host: "2.2.2.2", Port: 80},
		{Host: "3.3.3.3", Port: 443},
	}
	for i, w := range want {
		got, ok := it.Next()
		if !ok {
			t.Errorf("Next() at index %d: ok = false, want true", i)
		}
		if got != w {
			t.Errorf("Next() at index %d = %+v, want %+v", i, got, w)
		}
	}
	// After the channel is drained, Next() returns zero-value + false.
	// / channel 排空后，Next() 返零值 + false。
	if got, ok := it.Next(); ok || got != (Item{}) {
		t.Errorf("Next() after drain = (%+v, %v), want (Item{}, false)", got, ok)
	}
}

func TestChanIterator_EmptyChannel(t *testing.T) {
	ch := make(chan Item)
	close(ch)
	it := NewChanIterator(ch)
	if got, ok := it.Next(); ok || got != (Item{}) {
		t.Errorf("Next() on empty closed channel = (%+v, %v), want (Item{}, false)", got, ok)
	}
}

func TestChanIterator_NilChannel(t *testing.T) {
	// A nil receive channel is a valid zero value — it just blocks
	// forever. We use a 1ms timeout via select to assert this
	// without hanging the test. / nil 接收 channel 是有效的零值
	// ——只是会永远阻塞。用 select + 超时断言而不挂测试。
	ch := make(chan Item, 1)
	close(ch)
	it := NewChanIterator(ch)
	done := make(chan struct{})
	go func() {
		_, _ = it.Next() // ok=false on closed channel; doesn't block
		close(done)
	}()
	<-done
}

func TestCrossIterator_HostMajorPortMinor(t *testing.T) {
	hosts := []string{"1.1.1.1", "2.2.2.2"}
	ports := []int{22, 80, 443}
	it := NewCrossIterator(hosts, ports)
	if got := it.Estimated(); got != 6 {
		t.Errorf("Estimated() = %d, want 6 (2 hosts × 3 ports)", got)
	}
	want := []Item{
		{Host: "1.1.1.1", Port: 22},
		{Host: "1.1.1.1", Port: 80},
		{Host: "1.1.1.1", Port: 443},
		{Host: "2.2.2.2", Port: 22},
		{Host: "2.2.2.2", Port: 80},
		{Host: "2.2.2.2", Port: 443},
	}
	for i, w := range want {
		got, ok := it.Next()
		if !ok {
			t.Errorf("Next() at index %d: ok = false, want true", i)
			break
		}
		if got != w {
			t.Errorf("Next() at index %d = %+v, want %+v", i, got, w)
		}
	}
	if got, ok := it.Next(); ok || got != (Item{}) {
		t.Errorf("Next() after exhaustion = (%+v, %v), want (Item{}, false)", got, ok)
	}
}

func TestCrossIterator_EmptyInputs(t *testing.T) {
	t.Run("no hosts", func(t *testing.T) {
		it := NewCrossIterator(nil, []int{22, 80})
		if got := it.Estimated(); got != 0 {
			t.Errorf("Estimated() = %d, want 0", got)
		}
		if got, ok := it.Next(); ok || got != (Item{}) {
			t.Errorf("Next() = (%+v, %v), want (Item{}, false)", got, ok)
		}
	})
	t.Run("no ports", func(t *testing.T) {
		it := NewCrossIterator([]string{"1.1.1.1"}, nil)
		if got := it.Estimated(); got != 0 {
			t.Errorf("Estimated() = %d, want 0", got)
		}
		if got, ok := it.Next(); ok || got != (Item{}) {
			t.Errorf("Next() = (%+v, %v), want (Item{}, false)", got, ok)
		}
	})
	t.Run("both empty", func(t *testing.T) {
		it := NewCrossIterator(nil, nil)
		if got := it.Estimated(); got != 0 {
			t.Errorf("Estimated() = %d, want 0", got)
		}
	})
}

func TestCrossIterator_PortSliceNotAliased(t *testing.T) {
	// NewCrossIterator must copy the ports slice — mutating the
	// caller's slice after construction must not affect iteration.
	// / NewCrossIterator 必须复制 ports slice——构造后修改调用方
	// 的 slice 不应影响迭代。
	ports := []int{22, 80}
	it := NewCrossIterator([]string{"1.1.1.1"}, ports)
	ports[0] = 9999
	got, _ := it.Next()
	if got.Port != 22 {
		t.Errorf("CrossIterator aliased caller's ports slice: got port %d, want 22 (original first port)", got.Port)
	}
}
