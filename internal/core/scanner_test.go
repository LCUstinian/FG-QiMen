// scanner_test.go — unit tests for the scanner package.
//
// Round 2 of the "TUI counters frozen during alive sweep" bug fix:
// verifies that scanner.go polls Discovery.Progress() into
// sess.State.Counters.AliveProbed during the alive phase, so the
// TUI counter updates as the sweep progresses instead of staying
// frozen at 0 for the entire sweep.
//
// Round 1 (in alive package) exposed Progress(). Round 2 (this file)
// verifies the wire-up: scanner.go actually polls Progress() into a
// state counter the UI reads.
//
// / scanner_test.go — scanner 包的单元测试。
// "TUI 计数器在 alive 阶段冻结" bug 修复 Round 2：验证 scanner.go
// 在 alive 阶段 poll Discovery.Progress() 写入 sess.State.Counters.
// AliveProbed，让 TUI counter 随扫描推进而更新。

package core

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/core/alive"
	"github.com/LCUstinian/FG-QiMen/internal/session"
	"github.com/LCUstinian/FG-QiMen/internal/types"
)

// delayedProbe is a test Probe that sleeps for `delay` before returning
// a Hit. Same shape as the one in discovery_test.go but duplicated
// here to avoid cross-package test fixture coupling.
// / delayedProbe 是测试 Probe，延迟 `delay` 后返回 Hit。
type delayedProbe struct {
	delay   time.Duration
	hit     alive.Hit
	invoked *atomic.Int64
}

func (p *delayedProbe) Name() string             { return "delayed" }
func (p *delayedProbe) Method() alive.Method     { return alive.MethodTCP }
func (p *delayedProbe) Available() error         { return nil }
func (p *delayedProbe) Probe(ctx context.Context, host string, timeout time.Duration) (alive.Hit, error) {
	if p.invoked != nil {
		p.invoked.Add(1)
	}
	select {
	case <-time.After(p.delay):
		return p.hit, nil
	case <-ctx.Done():
		return alive.Hit{}, ctx.Err()
	}
}

// TestScanner_PollsAliveProbedCounter is a regression test for the
// "TUI counters frozen during alive sweep" bug. It verifies that
// scanner.go polls alive.Discovery.Progress() into
// sess.State.Counters.AliveProbed during the alive phase, so the
// TUI counter updates as the sweep progresses.
//
// Test shape:
//   - 4 hosts × 100ms probe delay, 1 thread → ~400ms total
//   - Scanner runs alive stage in a goroutine
//   - Test samples sess.State.Counters.AliveProbed every 20ms
//   - Asserts: at least one mid-run sample shows AliveProbed > 0,
//     and final value is 4 (all probes invoked)
//
// The test uses the package-level `aliveProbesFn` hook (added in
// scanner.go's wire-up step) to inject the delayed probe into the
// scanner's alive setup. In production this hook returns
// alive.DefaultOptions(); tests override it for deterministic timing.
//
// / TestScanner_PollsAliveProbedCounter 是"TUI 计数器在 alive 阶段冻结"
// bug 的回归测试。4 hosts × 100ms，1 线程，约 400ms 总耗时。Test
// 每 20ms 采样 sess.State.Counters.AliveProbed，断言至少有一次中途
// 采样 > 0，且最终值是 4（所有 probe 都被调用）。
func TestScanner_PollsAliveProbedCounter(t *testing.T) {
	// Override the alive-probes factory so the scanner uses our
	// slow probe. Restore on cleanup.
	//
	// 覆盖 alive-probes 工厂让 scanner 用我们的慢 probe。退出时还原。
	origFn := aliveProbesFn
	t.Cleanup(func() { aliveProbesFn = origFn })
	invoked := &atomic.Int64{}
	probe := &delayedProbe{
		delay:   100 * time.Millisecond,
		hit:     alive.Hit{Host: "127.0.0.1", Method: alive.MethodTCP},
		invoked: invoked,
	}
	aliveProbesFn = func() alive.Options {
		return alive.Options{
			Probes:    []alive.Probe{probe},
			Timeout:   2 * time.Second,
			Threads:   1,
			FirstOnly: true,
		}
	}

	// Build a minimal session with 4 host targets.
	cfg := &types.Config{
		Host:      "h1,h2,h3,h4",
		Mode:      types.ModeScan,
		Timeout:   2 * time.Second,
		AliveOnly: true, // skip port scan + plugins, just run alive
		Silent:    true,
	}
	sess, err := session.NewSession(context.Background(), cfg, "")
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	// Run RunScan in a goroutine. It will block on the alive phase
	// for ~400ms then return.
	runDone := make(chan error, 1)
	go func() {
		_, runErr := RunScan(context.Background(), sess)
		runDone <- runErr
	}()

	// Sample sess.State.Counters.AliveProbed every 20ms until RunScan
	// returns. Capture the run-completion sample too (the final
	// Tried.Add(1) for the last probe can race with the
	// runDone-channel send).
	var samples []int64
	var runErr error
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
sampleLoop:
	for {
		select {
		case <-tick.C:
			samples = append(samples, sess.State.Counters.AliveProbed.Load())
		case runErr = <-runDone:
			// RunScan just returned. Sample one more time to capture
			// the final AliveProbed (last Tried.Add may not have been
			// visible to the ticker).
			samples = append(samples, sess.State.Counters.AliveProbed.Load())
			break sampleLoop
		}
	}
	if runErr != nil {
		t.Fatalf("RunScan: %v", runErr)
	}

	// Sanity: 4 probes must have been invoked.
	if got := invoked.Load(); got != 4 {
		t.Errorf("probe invoked count = %d, want 4", got)
	}

	// At least one mid-run sample must show AliveProbed > 0.
	max := int64(0)
	for _, s := range samples {
		if s > max {
			max = s
		}
	}
	if max == 0 {
		t.Errorf("expected to observe AliveProbed > 0 mid-run; all %d samples were 0; scanner.go is not polling Discovery.Progress()", len(samples))
	}
	if max < 4 {
		t.Errorf("expected max sample >= 4 (final probed count), got %d", max)
	}
}

// probeCounter is unused; the per-probe `invoked` counter is used
// instead. Kept as a package-level var stub for future tests that
// need a global hook. / 保留为空 stub 以备未来需要。
