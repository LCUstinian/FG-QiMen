package scan

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// newAIMDTestPool builds a Pool for deterministic controller tests:
// no probe traffic, adjust() driven by hand-fed metrics.
// / newAIMDTestPool 构造用于确定性控制器测试的 Pool：无探测流量，
// adjust() 由手工投喂的度量驱动。
func newAIMDTestPool(min, max, initial int, env Env) *Pool {
	return NewPool(PoolOptions{
		Probe:          NewTCPConnectProbe(),
		Timeout:        time.Second,
		MinThreads:     min,
		MaxThreads:     max,
		InitialThreads: initial,
		AdjustInterval: 50 * time.Millisecond,
		Env:            env,
	})
}

// feedOpens feeds healthy open traffic (constant RTT ⇒ neutral ratio,
// zero exhaustion ⇒ exhaustRate 0) sized to clear minHealthSamples.
// / feedOpens 投喂健康 open 流量（恒定 RTT ⇒ 中性比值，零耗尽 ⇒
// exhaustRate 0），样本量足以越过 minHealthSamples。
func feedOpens(p *Pool, rtt time.Duration) {
	for i := 0; i < minHealthSamples; i++ {
		p.metrics.RecordOpen(rtt)
	}
}

// completeSlowStart drives the controller through its doubling phase
// (target/4 → target) so tests can assert steady-state AIMD behavior.
// / completeSlowStart 驱动控制器走完翻倍阶段（target/4 → target），
// 让测试得以断言稳态 AIMD 行为。
func completeSlowStart(p *Pool) {
	for p.inSlowStart {
		feedOpens(p, time.Millisecond)
		p.adjust()
	}
}

func TestNewPool_SlowStartBirth(t *testing.T) {
	p := newAIMDTestPool(50, 500, 200, EnvWAN)
	if got := p.currentThreads.Load(); got != 50 {
		t.Fatalf("birth concurrency = %d, want 50 (target/4 floored by MinThreads)", got)
	}
	if !p.inSlowStart {
		t.Fatal("want inSlowStart=true at birth (50 < target 200)")
	}
	if got := p.target; got != 200 {
		t.Fatalf("target = %d, want 200", got)
	}
}

func TestNewPool_InitialEqualsTargetSkipsSlowStart(t *testing.T) {
	// target 60 with floor max(50, 25)=50: birth = max(50, 15) = 50…
	// still below target. Use target == floor to exercise the skip.
	// / target 60 时下限 max(50,25)=50：出生 = max(50,15) = 50…仍低
	// 于 target。用 target == 下限来覆盖跳过路径。
	p := newAIMDTestPool(100, 500, 100, EnvWAN)
	if got := p.currentThreads.Load(); got != 100 {
		t.Fatalf("birth concurrency = %d, want 100 (target == floor)", got)
	}
	if p.inSlowStart {
		t.Fatal("want inSlowStart=false when birth == target")
	}
}

func TestAdjust_SlowStartDoublesToTarget(t *testing.T) {
	p := newAIMDTestPool(50, 500, 200, EnvWAN)
	steps := []int32{100, 200}
	for i, want := range steps {
		feedOpens(p, time.Millisecond)
		p.adjust()
		if got := p.currentThreads.Load(); got != want {
			t.Fatalf("step %d: concurrency = %d, want %d", i, got, want)
		}
	}
	if p.inSlowStart {
		t.Fatal("slow start must exit once target is reached")
	}
}

func TestAdjust_SlowStartExitsOnCongestion(t *testing.T) {
	p := newAIMDTestPool(50, 500, 200, EnvLAN)
	for i := 0; i < minHealthSamples; i++ {
		if i < 3 {
			p.metrics.RecordExhausted()
			continue
		}
		p.metrics.RecordOpen(time.Millisecond)
	}
	p.adjust()
	if p.inSlowStart {
		t.Fatal("congestion must exit slow start")
	}
	if got := p.currentThreads.Load(); got != 50 {
		t.Fatalf("concurrency after halving = %d, want 50 (100/2, floored)", got)
	}
}

func TestAdjust_AIMDGoodGrowsAdditively(t *testing.T) {
	p := newAIMDTestPool(50, 500, 200, EnvWAN)
	// Complete slow start. / 完成慢启动。
	feedOpens(p, time.Millisecond)
	p.adjust()
	feedOpens(p, time.Millisecond)
	p.adjust()
	before := p.currentThreads.Load()
	feedOpens(p, time.Millisecond)
	p.adjust()
	after := p.currentThreads.Load()
	want := before + p.target/20
	if after != want {
		t.Fatalf("AIMD good: %d → %d, want +%d (target/20)", before, after, p.target/20)
	}
}

func TestAdjust_AIMDCongestedHalvesAndFloor(t *testing.T) {
	p := newAIMDTestPool(50, 500, 400, EnvWAN)
	completeSlowStart(p) // 100 → 200 → 400
	// Feed 16% exhaustion on WAN (> 15% = congested).
	// / WAN 上喂 16% 耗尽（> 15% 判据 → Congested）。
	for i := 0; i < minHealthSamples; i++ {
		if i < 5 {
			p.metrics.RecordExhausted()
			continue
		}
		p.metrics.RecordOpen(time.Millisecond)
	}
	p.adjust()
	if got := p.currentThreads.Load(); got != 200 {
		t.Fatalf("congested: concurrency = %d, want 200 (400×0.5)", got)
	}
	// Drive to the floor: sustained congestion must never undercut
	// max(MinThreads, MaxThreads/20). / 持续拥塞必须止步于
	// max(MinThreads, MaxThreads/20)。
	for i := 0; i < 20; i++ {
		for j := 0; j < minHealthSamples; j++ {
			p.metrics.RecordExhausted()
		}
		p.adjust()
	}
	if got := p.currentThreads.Load(); got != 50 {
		t.Fatalf("sustained congestion: concurrency = %d, want floor 50", got)
	}
}

func TestAdjust_AIMDStressedShrinks85(t *testing.T) {
	p := newAIMDTestPool(50, 500, 400, EnvWAN)
	completeSlowStart(p) // 100 → 200 → 400
	// 8% exhaustion is above the WAN stress cut-off (5%), below
	// congestion (15%). / 8% 耗尽高于 WAN 压力判据（5%），低于拥塞
	// （15%）。
	for i := 0; i < minHealthSamples; i++ {
		if i < 3 {
			p.metrics.RecordExhausted()
			continue
		}
		p.metrics.RecordOpen(time.Millisecond)
	}
	p.adjust()
	want := int32(float64(400) * 0.85)
	if got := p.currentThreads.Load(); got != want {
		t.Fatalf("stressed: concurrency = %d, want %d (×0.85)", got, want)
	}
}

func TestAdjust_OKHolds(t *testing.T) {
	p := newAIMDTestPool(50, 500, 200, EnvWAN)
	feedOpens(p, time.Millisecond)
	p.adjust()
	feedOpens(p, time.Millisecond)
	p.adjust() // slow start completes at target 200
	// Pure-timeout windows read OK (silence guard) and must hold.
	// / 纯 timeout 窗口读作 OK（静默守则），必须维持。
	for i := 0; i < minHealthSamples; i++ {
		p.metrics.RecordTimeout()
	}
	p.adjust()
	if got := p.currentThreads.Load(); got != 200 {
		t.Fatalf("OK window: concurrency = %d, want hold 200", got)
	}
}

func TestAdjust_ClampsAtMaxThreads(t *testing.T) {
	p := newAIMDTestPool(50, 300, 300, EnvWAN)
	for i := 0; i < 10; i++ {
		feedOpens(p, time.Millisecond)
		p.adjust()
	}
	if got := p.currentThreads.Load(); got != 300 {
		t.Fatalf("sustained Good: concurrency = %d, want ceiling 300", got)
	}
}

func TestAdjust_TooFewSamplesHolds(t *testing.T) {
	p := newAIMDTestPool(50, 500, 400, EnvWAN)
	completeSlowStart(p) // hold baseline at 400
	p.metrics.RecordOpen(time.Millisecond)
	p.metrics.RecordExhausted() // would be "congested" if trusted
	p.adjust()
	if got := p.currentThreads.Load(); got != 400 {
		t.Fatalf("thin-sample window: concurrency = %d, want hold 400", got)
	}
}

func TestMaybeReduceTarget_RatchetDownToFloor(t *testing.T) {
	p := newAIMDTestPool(50, 500, 200, EnvWAN)
	// White-box: force a sustained ratio > 3.0. / 白盒：强制持续比值
	// > 3.0。
	p.metrics.rttSamples.Store(100)
	p.metrics.rttSlowNs.Store(int64(time.Millisecond))
	p.metrics.rttFastNs.Store(int64(5 * time.Millisecond))
	p.maybeReduceTarget()
	want := int32(float64(200) * 0.9)
	if got := p.target; got != want {
		t.Fatalf("target after one reduction = %d, want %d", got, want)
	}
	// Ratchet never re-raises while ratio stays high; floor is
	// max(MinThreads=50, MaxThreads/5=100) = 100.
	// / 比值持续高时棘轮不再回升；下限 max(50, 100) = 100。
	for i := 0; i < 30; i++ {
		p.maybeReduceTarget()
	}
	if got := p.target; got != 100 {
		t.Fatalf("target after sustained degradation = %d, want floor 100", got)
	}
	// Ratio back to normal must NOT re-raise the ratchet.
	// / 比值恢复正常也绝不回升棘轮。
	p.metrics.rttFastNs.Store(int64(time.Millisecond))
	p.maybeReduceTarget()
	if got := p.target; got != 100 {
		t.Fatalf("target after recovery = %d, want unchanged 100 (one-way ratchet)", got)
	}
}

func TestAdjust_HealthRTTTrendShrinksOnWAN(t *testing.T) {
	// Black-box path: real RecordOpen traffic with a 50× RTT jump
	// must push the WAN pool down via the stressed/congested RTT
	// cut-offs (ratio > 1.8). / 黑盒路径：带 50 倍 RTT 跳变的真实
	// RecordOpen 流量必须通过 WAN 的 RTT 判据（比值 > 1.8）把池压
	// 下去。
	p := newAIMDTestPool(50, 500, 400, EnvWAN)
	seedStableRTT(&p.metrics, 20, time.Millisecond)
	seedStableRTT(&p.metrics, minHealthSamples, 50*time.Millisecond)
	p.adjust()
	if got := p.currentThreads.Load(); got >= 400 {
		t.Fatalf("RTT-trend stress: concurrency = %d, want < 400", got)
	}
}

// gateProbe parks every probe on a channel so the test can pin the
// in-flight count while workers are blocked inside Probe.
// / gateProbe 把每个 probe 停泊在 channel 上，让测试能在 worker 阻
// 塞在 Probe 内时钉住在飞计数。
type gateProbe struct{ release chan struct{} }

func (g *gateProbe) Name() string     { return "gate" }
func (g *gateProbe) Method() Method   { return MethodTCPConnect }
func (g *gateProbe) Available() error { return nil }

func (g *gateProbe) Probe(_ context.Context, _ string, _ int, _ time.Duration) (Result, error) {
	<-g.release
	return Result{State: StateOpen, Method: MethodTCPConnect}, nil
}

// TestPool_InflightSinkBalance pins the §7.3 ledger wiring: the sink
// tracks workers parked inside Probe and returns to exactly zero once
// Run drains — the TUI PROGRESS ledger's inflight column is only
// credible if the mirror is leak-free on both sides.
// / TestPool_InflightSinkBalance 钉住 §7.3 账本接线：sink 跟踪停泊
// 在 Probe 内的 worker，Run 排空后必须精确归零——只有镜像两侧都无
// 泄漏，TUI PROGRESS 账本的 inflight 列才可信。
func TestPool_InflightSinkBalance(t *testing.T) {
	var sink atomic.Int64
	g := &gateProbe{release: make(chan struct{})}
	p := NewPool(PoolOptions{
		Probe:          g,
		Timeout:        time.Second,
		MinThreads:     2,
		MaxThreads:     2,
		InitialThreads: 2,
		AdjustInterval: time.Hour, // keep the controller out of the way
		InflightSink:   &sink,
	})

	hosts := make([]string, 6)
	for i := range hosts {
		hosts[i] = "10.0.0.1"
	}
	iter := NewCrossIterator(hosts, []int{80})
	out := make(chan Result, 6)

	errCh := make(chan error, 1)
	go func() { errCh <- p.Run(context.Background(), iter, out) }()

	// Wait until both workers are parked in Probe and mirrored.
	// / 等两个 worker 都停泊进 Probe 并完成镜像。
	waitForInflight(t, &sink, 2)

	close(g.release)
	if err := <-errCh; err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := sink.Load(); got != 0 {
		t.Fatalf("sink after Run = %d, want 0 (mirror leak)", got)
	}
	if got := p.Inflight(); got != 0 {
		t.Fatalf("Pool.Inflight after Run = %d, want 0 (pool counter leak)", got)
	}
}

// waitForInflight polls until the sink reaches want or times out.
// / waitForInflight 轮询直到 sink 达到 want 或超时。
func waitForInflight(t *testing.T, sink *atomic.Int64, want int64) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if got := sink.Load(); got == want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("sink never reached %d (now %d)", want, sink.Load())
}
