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
	"strconv"
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

// routingProbe returns Open only for host:port pairs listed in open
// (key "host|port"), Closed otherwise. Records every probe by category
// (gateway = host ends in .1/.254) so tests can assert on phase-1 vs
// phase-2 traffic separately.
//
// routingProbe 仅对 open 列表（键 "host|port"）中的 host:port 返回
// Open，其余 Closed。按类别记录每次探测（网关 = host 以 .1/.254 结
// 尾），供测试分别断言阶段 1 / 阶段 2 流量。
type routingProbe struct {
	mu     sync.Mutex
	open   map[string]bool
	probed []string
}

func newRoutingProbe(open map[string]bool) *routingProbe {
	return &routingProbe{open: open}
}

func (p *routingProbe) Name() string     { return "routing" }
func (p *routingProbe) Method() Method   { return MethodTCPConnect }
func (p *routingProbe) Available() error { return nil }

func (p *routingProbe) Probe(_ context.Context, host string, port int, _ time.Duration) (Result, error) {
	p.mu.Lock()
	p.probed = append(p.probed, host+"|"+strconv.Itoa(port))
	p.mu.Unlock()
	if p.open[host+"|"+strconv.Itoa(port)] {
		return Result{State: StateOpen, Method: MethodTCPConnect}, nil
	}
	return Result{State: StateClosed, Method: MethodTCPConnect}, nil
}

// countHostProbes counts probes to non-gateway hosts (.1/.254 excluded).
// countHostProbes 统计对非网关主机（排除 .1/.254）的探测次数。
func (p *routingProbe) countHostProbes() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := 0
	for _, rec := range p.probed {
		host := rec[:strings.IndexByte(rec, '|')]
		if !strings.HasSuffix(host, ".1") && !strings.HasSuffix(host, ".254") {
			n++
		}
	}
	return n
}

func (p *routingProbe) totalProbes() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.probed)
}

// segHosts builds "10.<b>.0.i" hosts for i in [1, n].
// segHosts 构建 "10.<b>.0.i"（i ∈ [1, n]）主机列表。
func segHosts(b, n int) []string {
	hosts := make([]string, 0, n)
	for i := 1; i <= n; i++ {
		// Plain itoa for octets: net.ParseIP rejects leading zeros, so
		// zero-padded hosts would fall into the pass-through bucket.
		// / 八位组用普通 itoa：net.ParseIP 拒绝前导零，零填充主机会被
		// 归入直通分组。
		hosts = append(hosts, "10."+itoa3(b)+".0."+strconv.Itoa(i))
	}
	return hosts
}

// TestPrescreenPhase2RescuesGatewaySilentSegment — the core reason the
// phase-2 fallback exists: two segments, all gateways firewalled, but
// one host in segment B answers on its rotating fallback port. Without
// phase 2 the gateway heuristic would drop BOTH segments; with it,
// segment B is rescued and segment A (fully silent) is still dropped.
//
// / TestPrescreenPhase2RescuesGatewaySilentSegment — 阶段 2 兜底存在
// 的核心理由：两个网段、全部网关被防火墙挡住，但 B 网段有一台主机在
// 轮换兜底端口上应答。没有阶段 2 时网关启发式会把两个网段都丢掉；
// 有阶段 2 时 B 被救回，完全静默的 A 仍被丢弃。
func TestPrescreenPhase2RescuesGatewaySilentSegment(t *testing.T) {
	// 10.200.0.x and 10.201.0.x, 130 hosts each → 260 ≥ threshold 256.
	// / 10.200.0.x 和 10.201.0.x 各 130 台 → 260 ≥ 阈值 256。
	a := segHosts(200, 130)
	b := segHosts(201, 130)
	hosts := append(append([]string{}, a...), b...)

	probe := newRoutingProbe(map[string]bool{
		// Octet 41 → 0-based index 40 → 40%8=0 → phase2ProbePorts[0]=80.
		// / 八位组 41 → 0-based 下标 40 → 40%8=0 → phase2ProbePorts[0]=80。
		"10.201.0.41|80": true,
	})
	pres := NewPrescreener(PrescreenOptions{
		Enabled:     true,
		Threshold:   PrescreenThreshold,
		ProbePorts:  []int{22, 80, 443, 3389},
		Timeout:     50 * time.Millisecond,
		Concurrency: 32,
		Phase2:      true,
	}, probe)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	got, summary := pres.FilterHostsSummary(ctx, hosts)

	for _, h := range a {
		if containsStr(got, h) {
			t.Errorf("dead segment A host %s survived prescreen", h)
		}
	}
	if !containsStr(got, "10.201.0.41") {
		t.Errorf("rescued segment B is missing host 10.201.0.41; got %d hosts", len(got))
	}
	if len(got) != len(b) {
		t.Errorf("got %d hosts, want %d (all of segment B)", len(got), len(b))
	}
	if summary.DeadSegments != 1 || summary.LiveSegments != 1 {
		t.Errorf("summary dead=%d live=%d, want 1/1", summary.DeadSegments, summary.LiveSegments)
	}
	if summary.SkippedHosts != len(a) || summary.FilteredHosts != len(b) {
		t.Errorf("summary skipped=%d filtered=%d, want %d/%d",
			summary.SkippedHosts, summary.FilteredHosts, len(a), len(b))
	}
	if !summary.WasPrescreened {
		t.Error("summary.WasPrescreened = false, want true")
	}
}

// TestPrescreenPhase2DisabledDropsSilentSegment — with Phase2 off, the
// gateway-silent-but-live segment is dropped and no per-host probes run.
//
// / TestPrescreenPhase2DisabledDropsSilentSegment — 关掉 Phase2 时，
// 网关静默但存活的网段被丢弃且不产生逐主机探测。
func TestPrescreenPhase2DisabledDropsSilentSegment(t *testing.T) {
	a := segHosts(200, 130)
	b := segHosts(201, 130)
	hosts := append(append([]string{}, a...), b...)

	probe := newRoutingProbe(map[string]bool{"10.201.0.41|80": true})
	pres := NewPrescreener(PrescreenOptions{
		Enabled:     true,
		Threshold:   PrescreenThreshold,
		ProbePorts:  []int{22, 80, 443, 3389},
		Timeout:     50 * time.Millisecond,
		Concurrency: 32,
		Phase2:      false,
	}, probe)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	got, _ := pres.FilterHostsSummary(ctx, hosts)

	if containsStr(got, "10.201.0.41") {
		t.Error("phase2 disabled but gateway-silent segment B survived")
	}
	if n := probe.countHostProbes(); n != 0 {
		t.Errorf("phase2 disabled but %d per-host probes ran", n)
	}
}

// TestPrescreenPhase2SampleCap — the fallback must not probe more than
// Phase2SampleCap hosts per missed segment, even when the segment has
// more hosts and no host ever answers (worst-case cost bound).
//
// / TestPrescreenPhase2SampleCap — 兜底对每个未命中网段的探测不得超
// 过 Phase2SampleCap 台主机，即使网段更大且无主机应答（最坏成本上界）。
func TestPrescreenPhase2SampleCap(t *testing.T) {
	a := segHosts(200, 130)
	b := segHosts(201, 130)
	hosts := append(append([]string{}, a...), b...)

	probe := newRoutingProbe(nil) // nothing open / 全部静默
	pres := NewPrescreener(PrescreenOptions{
		Enabled:         true,
		Threshold:       PrescreenThreshold,
		ProbePorts:      []int{22, 80, 443, 3389},
		Timeout:         50 * time.Millisecond,
		Concurrency:     200,
		Phase2:          true,
		Phase2SampleCap: 16,
	}, probe)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	got, _ := pres.FilterHostsSummary(ctx, hosts)

	if len(got) != 0 {
		t.Errorf("fully silent input should filter to 0 hosts, got %d", len(got))
	}
	// Both segments are gateway-silent → each contributes ≤16 host
	// probes (2 × 16). / 两个网段都网关静默，各贡献 ≤16 台（2 × 16）。
	if n := probe.countHostProbes(); n > 32 {
		t.Errorf("phase-2 probed %d hosts, want ≤ 32 (2 segments × cap 16)", n)
	}
}

// TestPrescreenSingleSegmentGuard — a single-/24 input at threshold
// must pass through untouched (mirrors fscan's len(subnets)<=1 guard):
// with nothing to compare against, a gateway-silent /24 would otherwise
// be wiped out wholesale. Uses duplicated hosts to exceed the threshold
// within one segment (unique IPs cap at 254 per /24).
//
// / TestPrescreenSingleSegmentGuard — 达到阈值的单 /24 输入必须原样
// 通过（对齐 fscan 的 len(subnets)<=1 守卫）：没有对照对象时，网关
// 静默的 /24 否则会被一刀切掉。用重复主机使单网段超过阈值（唯一 IP
// 在 /24 内上限 254）。
func TestPrescreenSingleSegmentGuard(t *testing.T) {
	base := segHosts(200, 130)
	hosts := append(append([]string{}, base...), base...) // 260 hosts, 1 segment

	probe := newRoutingProbe(nil)
	pres := NewPrescreener(PrescreenOptions{
		Enabled:     true,
		Threshold:   PrescreenThreshold,
		ProbePorts:  []int{22, 80, 443, 3389},
		Timeout:     50 * time.Millisecond,
		Concurrency: 32,
		Phase2:      true,
	}, probe)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	got, summary := pres.FilterHostsSummary(ctx, hosts)

	if len(got) != len(hosts) {
		t.Errorf("single-segment input filtered: got %d hosts, want %d", len(got), len(hosts))
	}
	if n := probe.totalProbes(); n != 0 {
		t.Errorf("single-segment guard must skip all probes, got %d", n)
	}
	if summary.WasPrescreened {
		t.Error("single-segment guard should not mark summary as prescreened")
	}
}

func containsStr(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// silence unused-import warnings on platforms / build configs that
// prune these. / 在裁掉这些 import 的平台 / 构建配置下抑制未用导入
// 警告。
var (
	_ = net.IPv4len
	_ = strings.Contains
	_ = sync.Mutex{}
)
