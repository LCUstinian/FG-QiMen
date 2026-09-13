package scan

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestAdaptiveTimeout_WarmupReturnsBase(t *testing.T) {
	at := NewAdaptiveTimeout(3 * time.Second)
	if got := at.Timeout(); got != 3*time.Second {
		t.Fatalf("no samples: Timeout() = %v, want base 3s", got)
	}
	for i := 0; i < adaptiveWarmup-1; i++ {
		at.Record(5 * time.Millisecond)
	}
	if got := at.Timeout(); got != 3*time.Second {
		t.Fatalf("%d samples: Timeout() = %v, want base 3s", adaptiveWarmup-1, got)
	}
}

func TestAdaptiveTimeout_ClampFloorAndCeiling(t *testing.T) {
	at := NewAdaptiveTimeout(3 * time.Second)
	// Tight fast LAN: mean+4σ ≈ 50ms → floored at max(500ms, 3s/5)=600ms.
	// / 紧凑快速局域网：mean+4σ ≈ 50ms → 下限 max(500ms, 3s/5)=600ms。
	for i := 0; i < adaptiveWarmup; i++ {
		at.Record(50 * time.Millisecond)
	}
	if got := at.Timeout(); got != 600*time.Millisecond {
		t.Fatalf("fast LAN: Timeout() = %v, want floor 600ms", got)
	}

	// Over-ceiling path: mean+4σ > base → clamped to base. Samples
	// sit ABOVE the 2s base (lossy slow path), so the σ margin can't
	// rescue them and the ceiling clamps.
	// / 超上限路径：mean+4σ > base → clamp 到 base。样本高于 2s base
	// （高丢包慢路径），σ 余量救不回来，由上限 clamp。
	at2 := NewAdaptiveTimeout(2 * time.Second)
	for i := 0; i < adaptiveWarmup; i++ {
		at2.Record(2100 * time.Millisecond)
	}
	if got := at2.Timeout(); got != 2*time.Second {
		t.Fatalf("slow path: Timeout() = %v, want ceiling 2s", got)
	}
}

func TestAdaptiveTimeout_RingKeepsRecentSamples(t *testing.T) {
	at := NewAdaptiveTimeout(5 * time.Second)
	// 90 fast samples then 10 slow ones: the ring must reflect the
	// recent regime, not the historical one.
	// / 90 个快样本再 10 个慢样本：环形缓冲必须反映近期状态而非历史。
	for i := 0; i < 90; i++ {
		at.Record(10 * time.Millisecond)
	}
	for i := 0; i < 10; i++ {
		at.Record(500 * time.Millisecond)
	}
	got := at.Timeout()
	// Ring keeps the last 64 samples: 54×10ms + 10×500ms →
	// mean=86.6ms, σ≈177.9ms → mean+4σ ≈ 798ms; below base 5s but
	// under the 1s floor (floor = max(500ms, 5s/5)). Expect 1s.
	// / 环形缓冲保留最后 64 个样本：54×10ms + 10×500ms → mean=86.6ms，
	// σ≈177.9ms → mean+4σ ≈ 798ms；低于 base 5s 但低于下限 1s
	// （floor = max(500ms, 5s/5)）。期望 1s。
	if got != 1*time.Second {
		t.Fatalf("ring: Timeout() = %v, want floor 1s (recent regime dominates)", got)
	}
}

func TestAdaptiveTimeout_IgnoresNonPositiveRTT(t *testing.T) {
	at := NewAdaptiveTimeout(3 * time.Second)
	for i := 0; i < adaptiveWarmup; i++ {
		at.Record(0)
		at.Record(-time.Second)
	}
	if got := at.Timeout(); got != 3*time.Second {
		t.Fatalf("Timeout() = %v, want base 3s (zero/negative RTTs ignored)", got)
	}
}

func TestAdaptiveTimeout_Concurrent(t *testing.T) {
	at := NewAdaptiveTimeout(3 * time.Second)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				at.Record(time.Duration(j+1) * time.Millisecond)
				_ = at.Timeout()
			}
		}()
	}
	wg.Wait()
}

func TestAdaptiveTimeout_NonPositiveBaseFallsBack(t *testing.T) {
	at := NewAdaptiveTimeout(0)
	if got := at.Timeout(); got != 3*time.Second {
		t.Fatalf("Timeout() = %v, want 3s default base", got)
	}
}

// fakeTimeoutProbe records the timeout each Probe call received and
// reports an open result with a fixed RTT.
// / fakeTimeoutProbe 记录每次 Probe 收到的超时，并返回带固定 RTT 的
// open 结果。
type fakeTimeoutProbe struct {
	mu  sync.Mutex
	got []time.Duration
	rtt time.Duration
}

func (f *fakeTimeoutProbe) Name() string     { return "fake-timeout" }
func (f *fakeTimeoutProbe) Method() Method   { return MethodTCPConnect }
func (f *fakeTimeoutProbe) Available() error { return nil }

func (f *fakeTimeoutProbe) Probe(_ context.Context, host string, port int, timeout time.Duration) (Result, error) {
	f.mu.Lock()
	f.got = append(f.got, timeout)
	f.mu.Unlock()
	return Result{Host: host, Port: port, State: StateOpen, RTT: f.rtt, Time: time.Now()}, nil
}

// runFake runs a pool over n synthetic items and waits for completion.
// / runFake 用 n 个合成 item 跑一个池并等待完成。
func runFake(t *testing.T, probe Probe, at *AdaptiveTimeout, n int) {
	t.Helper()
	p := NewPool(PoolOptions{
		Probe:          probe,
		Timeout:        3 * time.Second,
		Adaptive:       at,
		MinThreads:     1,
		MaxThreads:     2,
		InitialThreads: 1,
	})
	ch := make(chan Item, n)
	for i := 0; i < n; i++ {
		ch <- Item{Host: "127.0.0.1", Port: i + 1}
	}
	close(ch)
	out := make(chan Result, 64)
	if err := p.Run(context.Background(), NewChanIterator(ch), out); err != nil {
		t.Fatalf("Run: %v", err)
	}
	close(out)
}

func TestPool_AdaptiveTimeoutShrinks(t *testing.T) {
	probe := &fakeTimeoutProbe{rtt: 5 * time.Millisecond}
	at := NewAdaptiveTimeout(3 * time.Second)
	// 20 probes: 10 warmup at base, then the computed timeout takes
	// over (floored at 600ms for a 5ms-RTT regime).
	// / 20 次探测：前 10 次冷启动按 base，之后计算超时接管（5ms RTT
	// 状态下被 clamp 到 600ms 下限）。
	runFake(t, probe, at, 20)

	if len(probe.got) != 20 {
		t.Fatalf("probe calls = %d, want 20", len(probe.got))
	}
	for i, got := range probe.got[:adaptiveWarmup] {
		if got != 3*time.Second {
			t.Fatalf("warmup probe %d timeout = %v, want base 3s", i, got)
		}
	}
	for i, got := range probe.got[adaptiveWarmup:] {
		if got != 600*time.Millisecond {
			t.Fatalf("post-warmup probe %d timeout = %v, want 600ms floor", i, got)
		}
	}
}

func TestPool_AdaptiveSkipsSilentRTT(t *testing.T) {
	// A silent UDP probe reports open|filtered with RTT=0 (no
	// response — no RTT). The pool must not feed it into the ring:
	// with zero valid samples the timeout stays at the base for the
	// WHOLE run instead of drifting toward the degenerate values.
	// / 静默 UDP 探测以 RTT=0 报 open|filtered（无响应——无 RTT）。
	// 池不得把它喂进采样环：零有效样本时超时全程保持在 base，而不
	// 向退化值漂移。
	probe := &fakeTimeoutProbe{rtt: 0}
	at := NewAdaptiveTimeout(3 * time.Second)
	runFake(t, probe, at, 20)

	for i, got := range probe.got {
		if got != 3*time.Second {
			t.Fatalf("probe %d timeout = %v, want base 3s (silent open results must never feed the sampler)", i, got)
		}
	}
}
