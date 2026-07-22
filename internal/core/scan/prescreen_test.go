// prescreen_test.go — unit tests for the network-segment prescreener.
//
// Coverage focus is the cancellation contract (Task 6): when the
// caller's context is cancelled mid-probe, probSegments must return
// promptly without waiting for the full per-gateway probe timeout,
// without blocking on a full semaphore, and without dispatching new
// tasks for already-iterated segment/gateway pairs. The previous
// implementation's `break` exited only the inner select, so the for
// loop kept firing new goroutines until the task slice was exhausted
// (each blocked on a never-drained semaphore slot once ctx fired),
// and `wg.Wait()` waited for the in-flight probes' full timeouts.
//
// prescreen_test.go — 网段预筛的单测。覆盖重点是取消契约（Task 6）：
// 当调用方 context 在探测中途被取消时，probSegments 必须立即返回，不
// 等待完整 per-gateway 探测超时，不在已满 semaphore 上阻塞，也不为
// 已迭代的 segment/gateway 对派发新 task。旧实现的 `break` 只跳出内层
// select，外层 for 继续派发新 goroutine 直到 task slice 耗尽（ctx 触发
// 后都阻塞在永不消费的 semaphore slot 上），`wg.Wait()` 还等所有在飞
// 探测走完完整超时。
package scan

import (
	"context"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// slowProbe simulates a probe that respects ctx cancellation but
// otherwise sleeps close to its declared timeout. Used to drive the
// test into the "many probes in flight, ctx fires" state.
//
// slowProbe 模拟一个尊重 ctx 取消但否则会睡满声明超时的 probe。用于
// 把测试驱动到"多个探测在飞、ctx 触发"状态。
type slowProbe struct {
	// inFlight counts how many Probe calls are currently executing
	// (entered Probe but not yet returned). / inFlight 计数当前正在
	// 执行（已进入 Probe 但尚未返回）的调用数。
	inFlight atomic.Int64
	// total counts every Probe call that started (including ones
	// that returned early due to ctx). / total 计数每个启动过的
	// Probe 调用（包括因 ctx 而提前返回的）。
	total atomic.Int64
	// probeDelay is the wall-clock sleep before returning when ctx
	// is not cancelled. / probeDelay 是 ctx 未取消时的 wall-clock
	// 睡眠时间。
	probeDelay time.Duration
}

func (p *slowProbe) Name() string     { return "slow" }
func (p *slowProbe) Method() Method   { return MethodTCPConnect }
func (p *slowProbe) Available() error { return nil }

func (p *slowProbe) Probe(ctx context.Context, _ string, _ int, _ time.Duration) (Result, error) {
	p.total.Add(1)
	p.inFlight.Add(1)
	defer p.inFlight.Add(-1)
	select {
	case <-ctx.Done():
		return Result{}, ctx.Err()
	case <-time.After(p.probeDelay):
		return Result{State: StateClosed, Method: MethodTCPConnect}, nil
	}
}

// makeSegments returns a /24 segment map with the given number of
// segments. Each segment has a single host so probSegments creates
// 2*segmentCount gateway tasks.
//
// makeSegments 返回具有指定段数的 /24 segment map。每段一个 host，
// 让 probSegments 创建 2*segmentCount 个网关 task。
func makeSegments(n int) map[string][]string {
	seg := make(map[string][]string, n)
	for i := 0; i < n; i++ {
		// Use 10.<i>.0.0/24 to avoid colliding with realistic RFC1918
		// ranges used elsewhere in the test suite.
		// 用 10.<i>.0.0/24 避开测试套件中其他测试用的 RFC1918 段。
		netStr := "10." + itoa3(i) + ".0"
		seg[netStr] = []string{netStr + ".5"}
	}
	return seg
}

func itoa3(i int) string {
	// Zero-pad to three digits so lexicographic order matches
	// numeric order (matters for human readability, not for the
	// algorithm). / 零填充到三位，让字典序与数字序一致（人读友好，
	// 算法不依赖）。
	if i < 10 {
		return "00" + string(rune('0'+i))
	}
	if i < 100 {
		return "0" + string(rune('0'+i/10)) + string(rune('0'+i%10))
	}
	return string(rune('0'+i/100)) + string(rune('0'+(i/10)%10)) + string(rune('0'+i%10))
}

// TestPrescreenCancellationExitsPromptly — Task 6 (first-batch fixes).
//
// Setup: 32 segments → 64 gateway tasks, Concurrency=4 so the
// semaphore is small enough to fill quickly. slowProbe sleeps 5s
// when ctx is not cancelled, so without the fix each call would
// block for ~5s.
//
// Action: cancel ctx AFTER the first ~3 probes have entered Probe
// (verified via inFlight.Load()), then assert probSegments returns
// well under the 5s probe delay. Without the fix this test fails:
// the for loop keeps trying to acquire the semaphore and blocks
// until ctx-driven Probe calls finally free up slots, by which time
// the 5s delay has already elapsed.
func TestPrescreenCancellationExitsPromptly(t *testing.T) {
	const (
		segCount      = 32
		concurrency   = 4
		probeDelay    = 5 * time.Second
		wantReturnMax = 1 * time.Second // generous upper bound
	)

	probe := &slowProbe{probeDelay: probeDelay}
	pres := NewPrescreener(PrescreenOptions{
		Enabled:     true,
		Threshold:   1, // disable the host-count gate; we only care about the loop
		ProbePorts:  []int{22},
		Timeout:     probeDelay,
		Concurrency: concurrency,
	}, probe)

	ctx, cancel := context.WithCancel(context.Background())

	// Fire probSegments in a goroutine; cancel after at least one
	// probe is in flight, then wait for the call to return.
	// 在子 goroutine 跑 probSegments；至少一个探测在飞后取消，然后等
	// 调用返回。
	done := make(chan map[string]bool, 1)
	go func() {
		done <- pres.probSegments(ctx, makeSegments(segCount))
	}()

	// Wait until at least one probe is in flight, then cancel. Use a
	// short poll budget so a deadlock in probSegments fails the test
	// rather than hanging forever.
	deadline := time.Now().Add(2 * time.Second)
	for probe.inFlight.Load() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("no probe entered Probe within 2s; scheduler broken?")
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()

	select {
	case <-done:
		// Returned in time. Sanity-check that the test was actually
		// exercising the slow path (≥1 probe must have entered).
		// / 及时返回。健全性检查：测试确实走的是慢路径（至少 1 个
		// probe 必须已进入）。
		if probe.total.Load() < 1 {
			t.Errorf("probe.total = %d, want ≥1 (slow path never entered)",
				probe.total.Load())
		}
	case <-time.After(wantReturnMax):
		t.Fatalf("probSegments did not return within %v of ctx cancel (inFlight=%d, total=%d)",
			wantReturnMax, probe.inFlight.Load(), probe.total.Load())
	}
}

// TestPrescreenCancellationDoesNotBlockOnFullSemaphore — focuses on
// the specific bug the audit flagged: when ctx fires while the
// semaphore is already full, the next `sem <- struct{}{}` must not
// block. We force the semaphore full by making Concurrency=1 and
// ensuring one slow probe is in flight, then cancel and assert no
// goroutine is blocked on sem.
//
// TestPrescreenCancellationDoesNotBlockOnFullSemaphore — 聚焦审计
// 标记的具体 bug：ctx 在 semaphore 已满时触发，下一次 `sem <- struct{}{}`
// 不能阻塞。通过 Concurrency=1 并确保一个慢探测在飞来强制 semaphore
// 满，然后取消并断言没有 goroutine 阻塞在 sem 上。
func TestPrescreenCancellationDoesNotBlockOnFullSemaphore(t *testing.T) {
	const segCount = 4 // 8 gateway tasks; Concurrency=1 keeps the queue full
	probe := &slowProbe{probeDelay: 5 * time.Second}
	pres := NewPrescreener(PrescreenOptions{
		Enabled:     true,
		Threshold:   1,
		ProbePorts:  []int{22},
		Timeout:     5 * time.Second,
		Concurrency: 1,
	}, probe)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan map[string]bool, 1)
	go func() {
		done <- pres.probSegments(ctx, makeSegments(segCount))
	}()

	// Wait for the single probe slot to fill, then cancel.
	// / 等单个探测 slot 填满，然后取消。
	deadline := time.Now().Add(2 * time.Second)
	for probe.inFlight.Load() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("no probe entered within 2s")
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()

	select {
	case <-done:
		// Good — returned even though 7 more tasks were waiting on
		// the full 1-slot semaphore. / 好——尽管还有 7 个 task 等在
		// 满的 1-slot semaphore 上，仍能返回。
	case <-time.After(1 * time.Second):
		t.Fatalf("probSegments blocked on full semaphore after ctx cancel (inFlight=%d)",
			probe.inFlight.Load())
	}
}

// TestPrescreenDoesNotDispatchAfterCancel — verifies the fix's
// "only dispatch on successful acquire" property: when ctx fires
// the for loop must stop dispatching new goroutines. Without the
// fix the loop kept firing goroutines that immediately hit the
// full semaphore and blocked, inflating total way past the live
// task count.
//
// TestPrescreenDoesNotDispatchAfterCancel — 验证修法的"成功获取才派
// 发"属性：ctx 触发后 for 循环必须停止派发新 goroutine。旧实现循环
// 继续派发 goroutine，立即撞满的 semaphore 并阻塞，让 total 远超
// 真实 task 数。
func TestPrescreenDoesNotDispatchAfterCancel(t *testing.T) {
	const segCount = 64 // 128 tasks, but Concurrency=2 caps in-flight at 2
	probe := &slowProbe{probeDelay: 5 * time.Second}
	pres := NewPrescreener(PrescreenOptions{
		Enabled:     true,
		Threshold:   1,
		ProbePorts:  []int{22},
		Timeout:     5 * time.Second,
		Concurrency: 2,
	}, probe)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan map[string]bool, 1)
	go func() {
		done <- pres.probSegments(ctx, makeSegments(segCount))
	}()

	// Wait for the semaphore to fill (inFlight == Concurrency), then
	// cancel. After cancel the for loop must stop dispatching new
	// goroutines — so total must stay close to inFlight.
	deadline := time.Now().Add(2 * time.Second)
	for probe.inFlight.Load() < 2 {
		if time.Now().After(deadline) {
			t.Fatal("semaphore did not fill within 2s")
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	<-done

	total := probe.total.Load()
	inFlight := probe.inFlight.Load()
	// Allow a tiny slack for goroutines that entered Probe just
	// before cancel landed. The bug causes total >> inFlight (every
	// remaining task would have dispatched a goroutine that entered
	// Probe and immediately returned due to ctx). The fix keeps
	// total close to inFlight.
	// / 允许少量松量（刚好在 cancel 落地前进入 Probe 的 goroutine）。
	// 旧 bug 让 total 远大于 inFlight（每个剩余 task 都会派发一个进
	// 入 Probe 并因 ctx 立即返回的 goroutine）。修法让 total 接近
	// inFlight。
	if total > inFlight+4 {
		t.Errorf("dispatched %d probes after cancel (inFlight=%d); loop did not stop",
			total, inFlight)
	}
}

// TestPrescreenFilterHostsPassThroughSmall — sanity guard for the
// public FilterHosts API: under the host-count threshold, FilterHosts
// must return the input untouched and never call Probe.
//
// / TestPrescreenFilterHostsPassThroughSmall — 公开 FilterHosts API
// 的健全性保护：在 host-count 阈值之下，FilterHosts 必须原样返回输入
// 且不调 Probe。
func TestPrescreenFilterHostsPassThroughSmall(t *testing.T) {
	probe := &slowProbe{probeDelay: 5 * time.Second}
	pres := NewPrescreener(PrescreenOptions{
		Enabled:     true,
		Threshold:   PrescreenThreshold, // 256
		ProbePorts:  []int{22},
		Timeout:     5 * time.Second,
		Concurrency: 4,
	}, probe)

	// 10 hosts, well under the threshold. / 10 个 host，远低于阈值。
	hosts := []string{"10.0.0.1", "10.0.0.2", "10.0.0.3", "10.0.0.4", "10.0.0.5"}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	got := pres.FilterHosts(ctx, hosts)
	if !equalStrings(got, hosts) {
		t.Errorf("FilterHosts below threshold = %v, want %v", got, hosts)
	}
	if probe.total.Load() != 0 {
		t.Errorf("Probe called %d times below threshold; want 0", probe.total.Load())
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// silence unused-import warnings on platforms / build configs that
// prune these. / 在裁掉这些 import 的平台 / 构建配置下抑制未用导入
// 警告。
var (
	_ = net.IPv4len
	_ = strings.Contains
	_ = sync.Mutex{}
)