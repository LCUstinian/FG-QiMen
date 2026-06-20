// pipeline_workers_test.go — focused tests for the plugin worker.
//
// pipeline_workers_test.go — plugin worker 的针对性测试。
//
// We don't exercise the full pipeline here (that's scanner_test.go).
// We test the per-worker invariants: panic recovery, item dispatch,
// ctx cancel drain. / 这里不跑完整管线（那是 scanner_test.go）。
// 测试 per-worker 不变量：panic 恢复、item 派发、ctx 取消排空。
package core

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/session"
	"github.com/LCUstinian/FG-QiMen/internal/types"
	"github.com/LCUstinian/FG-QiMen/internal/ui"
)

// TestPipelineWorkers_DispatchesAllItems verifies the worker
// drains the input channel and exits. We don't assert on result
// count (the per-item Identify / banner match may produce 0
// results on a minimal session). The contract is: the worker
// processes every item and exits when the channel closes. /
// 验证 worker 排空输入 channel 并退出。不断言 result 数（最小
// session 下 per-item Identify / banner 匹配可能产 0 结果）。
// 契约是：worker 处理每个 item 并在 channel 关闭时退出。
func TestPipelineWorkers_DispatchesAllItems(t *testing.T) {
	sess, items, results, cancel := newWorkerFixture(t)
	defer cancel()

	const n = 10
	done := make(chan struct{})
	go func() {
		defer close(done)
		runPluginWorker(context.Background(), sess, nil, items, results)
	}()

	// Push n items, then ask the fixture to close the input.
	// Don't close items directly — the fixture owns it.
	// / 推 n 个 item 然后让 fixture 关 input。不要直接关 items——
	// fixture 拥有它。
	go func() {
		for i := 0; i < n; i++ {
			items <- types.ScanItem{Host: "10.0.0.1", Port: 22, Banner: ""}
		}
		cancel()
	}()

	// Drain results opportunistically (don't assert count). The
	// real test is that the worker exits. / 顺带排空 results（不
	// 断言数）。真正测的是 worker 退出。
	go func() {
		for range results {
		}
	}()

	select {
	case <-done:
		// good — worker exited after input closed
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not exit within 2s after input close")
	}
}

// TestPipelineWorkers_ContextCancelStops verifies ctx cancellation
// terminates the worker promptly. / 验证 ctx 取消后 worker 立即终
// 止。
func TestPipelineWorkers_ContextCancelStops(t *testing.T) {
	sess, items, results, cancel := newWorkerFixture(t)
	// Intentionally not defer cancel() here — the test cancels
	// explicitly. / 这里故意不 defer cancel()——测试显式取消。

	ctx, ctxCancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		runPluginWorker(ctx, sess, nil, items, results)
	}()

	// Let the worker block on its select. / 让 worker 阻塞在 select。
	time.Sleep(20 * time.Millisecond)
	ctxCancel()

	select {
	case <-done:
		// good
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not exit within 2s of ctx cancel")
	}
	cancel() // close the item/result channels
}

// TestPipelineWorkers_PanicRecovery verifies a single bad plugin
// doesn't crash the scan. The real audit regression test is at
// session_test.go; this is a small smoke test. / 验证单个坏 plugin
// 不拖垮扫描。真正的审计回归测试在 session_test.go；这是小冒烟。
func TestPipelineWorkers_PanicRecovery(t *testing.T) {
	sess, items, results, cancel := newWorkerFixture(t)
	defer cancel()

	// runPluginWorker has a recover() in defer. We can't easily
	// inject a panicking plugin (the registry is global), so we
	// just verify the worker exits cleanly with empty input. The
	// panic recovery is tested more directly by the M7 audit test
	// suite in session_test.go. / runPluginWorker 的 defer 里有
	// recover()。我们没法轻易注入 panicking plugin（注册表是全
	// 局的），所以只验证空输入下 worker 干净退出。panic 恢复由
	// session_test.go 的 M7 审计测试套件更直接覆盖。
	done := make(chan struct{})
	go func() {
		defer close(done)
		runPluginWorker(context.Background(), sess, nil, items, results)
	}()

	cancel() // close both channels
	select {
	case <-done:
		// good
	case <-time.After(2 * time.Second):
		t.Fatal("worker did not exit after channel close")
	}
}

// newWorkerFixture builds a minimal session and the input/result
// channels for worker tests. The cancel func closes both channels
// to release any blocked goroutines. Safe to call multiple times. /
// newWorkerFixture 为 worker 测试构建最小 session 和输入/结果
// channel。cancel 函数关闭两个 channel 以释放任何阻塞的 goroutine。
// 可安全多次调用。
func newWorkerFixture(t *testing.T) (*session.Session, chan types.ScanItem, chan *types.Result, func()) {
	t.Helper()
	sess := &session.Session{
		Ctx:    context.Background(),
		Config: &types.Config{},
		State:  types.NewState(),
		UI:     ui.NopUI(),
		Log:    types.DiscardLogger{},
	}
	items := make(chan types.ScanItem, 4)
	results := make(chan *types.Result, 4)
	var closeOnce sync.Once
	cancel := func() {
		closeOnce.Do(func() {
			close(items)
			close(results)
		})
	}
	return sess, items, results, cancel
}
