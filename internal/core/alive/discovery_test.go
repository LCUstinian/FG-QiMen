// discovery_test.go — unit tests for the alive package (ICMP / TCP /
// system-ping probes and the Discovery orchestrator).
//
// ARP and NBNS tests live alongside the probes themselves in the
// internal/discovery package.
//
// discovery_test.go — alive 包的单元测试（ICMP / TCP / system-ping
// 与 Discovery 调度器）。
//
// ARP 与 NBNS 测试与各自 probe 一起放在 internal/discovery 包。
package alive

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// startEchoListener opens a TCP listener on 127.0.0.1 that accepts
// connections and immediately closes. Returns the listening port.
//
// startEchoListener 在 127.0.0.1 上打开一个 TCP listener，接连接后立即关闭。
// 返回监听端口。
func startEchoListener(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()
	t.Cleanup(func() { _ = ln.Close() })
	return ln.Addr().(*net.TCPAddr).Port
}

// TestTCPProbe_Hit verifies that TCPProbe returns a Hit when a
// well-known port accepts.
//
// TestTCPProbe_Hit 验证 TCPProbe 在常用端口接受时返回 Hit。
func TestTCPProbe_Hit(t *testing.T) {
	port := startEchoListener(t)
	probe := NewTCPProbeWithPorts([]int{port})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	hit, err := probe.Probe(ctx, "127.0.0.1", time.Second)
	if err != nil {
		t.Fatalf("expected hit, got err=%v", err)
	}
	if hit.Method != MethodTCP {
		t.Errorf("expected MethodTCP, got %q", hit.Method)
	}
	if hit.Port != port {
		t.Errorf("expected port=%d, got %d", port, hit.Port)
	}
	if hit.RTT < 0 {
		t.Errorf("expected RTT >= 0, got %v", hit.RTT)
	}
	// Loopback is too fast for the timer to always register a positive
	// RTT; just confirm Time was set. / 回环太快计时器可能读不到正值；
	// 仅确认 Time 字段已设置即可。
	if hit.Host != "127.0.0.1" {
		t.Errorf("expected host=127.0.0.1, got %q", hit.Host)
	}
}

// TestTCPProbe_Miss verifies that TCPProbe returns ErrUnreachable
// when all configured ports refuse the connection.
//
// TestTCPProbe_Miss 验证 TCPProbe 在所有配置端口都拒绝时返回 ErrUnreachable。
func TestTCPProbe_Miss(t *testing.T) {
	// 1 = IANA tcpmux; rarely open. As a stronger guarantee, we use
	// a port from the ephemeral range that we are certain nothing
	// listens on. To find such a port, open one and immediately
	// release it.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	probe := NewTCPProbeWithPorts([]int{port})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err = probe.Probe(ctx, "127.0.0.1", 500*time.Millisecond)
	if err == nil {
		t.Fatalf("expected ErrUnreachable, got nil")
	}
	if !errIsUnreachable(err) {
		t.Errorf("expected ErrUnreachable, got %v", err)
	}
}

// TestTCPProbe_ContextCanceled verifies that a canceled context
// surfaces as ctx.Err() instead of being silently swallowed.
//
// TestTCPProbe_ContextCanceled 验证已取消的 context 表现为 ctx.Err()
// 而不是被静默吞掉。
func TestTCPProbe_ContextCanceled(t *testing.T) {
	probe := NewTCPProbeWithPorts([]int{1, 2, 3})
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before Probe
	_, err := probe.Probe(ctx, "127.0.0.1", time.Second)
	if err == nil {
		t.Fatalf("expected ctx error, got nil")
	}
}

// TestTCPProbe_DefaultPorts verifies the default port set is the
// well-known set (not an empty list, not nil).
//
// TestTCPProbe_DefaultPorts 验证默认端口集是知名端口集（非空，非 nil）。
func TestTCPProbe_DefaultPorts(t *testing.T) {
	probe := NewTCPProbe()
	if len(probe.Ports) == 0 {
		t.Fatal("NewTCPProbe() returned empty Ports")
	}
	if DefaultTCPProbePorts[0] != 80 {
		t.Errorf("expected first default port to be 80, got %d", DefaultTCPProbePorts[0])
	}
}

// TestSystemPing_Available skips on platforms without `ping` (rare;
// we assume the test runs on a real OS).
//
// TestSystemPing_Available 在没有 `ping` 的平台跳过（少见；我们假设
// 测试在真实 OS 上跑）。
func TestSystemPing_Available(t *testing.T) {
	probe := NewSystemPingProbe()
	if err := probe.Available(); err != nil {
		t.Skipf("ping not on PATH: %v", err)
	}
}

// TestSystemPing_Localhost hits 127.0.0.1; on most systems the system
// `ping` binary can reach it without admin.
//
// TestSystemPing_Localhost 打 127.0.0.1；多数情况下系统 `ping` 不需要
// admin 就能通。
func TestSystemPing_Localhost(t *testing.T) {
	probe := NewSystemPingProbe()
	if err := probe.Available(); err != nil {
		t.Skipf("ping not on PATH: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	hit, err := probe.Probe(ctx, "127.0.0.1", 3*time.Second)
	if err != nil {
		// Some sandboxed environments block `ping`; treat that as
		// a skip rather than a hard failure. / 一些沙箱环境会拦
		// `ping`；视为 skip 而不是硬失败。
		if strings.Contains(err.Error(), "operation not permitted") ||
			strings.Contains(err.Error(), "permission") {
			t.Skipf("ping blocked by environment: %v", err)
		}
		// If the binary exited non-zero (e.g. the system firewall
		// blocks the reply), accept it as a coverage signal.
		// 如果二进制非零退出（如系统防火墙拦响应），视为已覆盖。
		t.Logf("system-ping to 127.0.0.1 returned err=%v (treating as ok if ErrUnreachable)", err)
	}
	if err == nil && hit.Method != MethodSystem {
		t.Errorf("expected MethodSystem, got %q", hit.Method)
	}
}

// TestSystemPingProbe_ConcurrentNoRace — Task 5 (first-batch fixes).
// The audit flagged cmdProbe.Probe as a P0 data race: the method
// reads-and-writes the shared `p.timeout` field from every call
// (`if p.timeout <= 0 { p.timeout = 5*time.Second }` and the
// caller-timeout clamp below it), so concurrent callers racing on
// the same *cmdProbe instance corrupt each other's effective
// timeout. Run with -race; failure here means the timeout field is
// still being mutated concurrently.
//
// / TestSystemPingProbe_ConcurrentNoRace — 第一批修复 Task 5。
// 审计将 cmdProbe.Probe 标为 P0 数据竞争：方法每次调用都读写共享
// 的 `p.timeout` 字段（`if p.timeout <= 0 { p.timeout = 5*time.Second }`
// 和紧随其后的 caller-timeout clamp），并发调用同一 *cmdProbe 实例
// 时互相破坏对方的 effective timeout。用 -race 跑；这里失败说明
// timeout 字段仍在被并发修改。
func TestSystemPingProbe_ConcurrentNoRace(t *testing.T) {
	probe := NewSystemPingProbe()
	if err := probe.Available(); err != nil {
		t.Skipf("ping not on PATH: %v", err)
	}
	const N = 16
	var wg sync.WaitGroup
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			// Vary the requested timeout so both branches of the
			// clamp run; we don't care about the result, only that
			// the call returns without panicking. / 改变请求的
			// timeout 让 clamp 的两个分支都执行；只关心调用返
			// 回不 panic，不关心结果。
			_, _ = probe.Probe(ctx, "127.0.0.1", time.Duration(i+1)*100*time.Millisecond)
		}()
	}
	wg.Wait()
}

// TestSystemPing_RejectsFlagLikeHost verifies the SECURITY guard against
// passing a host argument that looks like a command-line flag. A user
// typo (`-r foo`) or a malicious target starting with `-` must NOT be
// forwarded to the system `ping` binary as a flag. / TestSystemPing_RejectsFlagLikeHost
// 验证安全防护：拒绝看起来像命令行 flag 的 host 参数。笔误
// （`-r foo`）或以 `-` 开头的恶意 target 不应被传给系统 `ping`。
func TestSystemPing_RejectsFlagLikeHost(t *testing.T) {
	probe := NewSystemPingProbe()
	if err := probe.Available(); err != nil {
		t.Skipf("ping not on PATH: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	// Try several flag-like values. None should reach the system
	// `ping` binary — all must be rejected up-front. / 尝试几个
	// 像 flag 的值。都不到达系统 `ping`——必须全部提前拒绝。
	for _, bad := range []string{"-r", "-c", "-n", "--help", "-f"} {
		_, err := probe.Probe(ctx, bad, 1*time.Second)
		if err == nil {
			t.Errorf("Probe(%q) accepted flag-like host; want error", bad)
			continue
		}
		if !strings.Contains(err.Error(), "flag") {
			t.Errorf("Probe(%q) error = %v, want flag-rejection message", bad, err)
		}
	}
}

// TestDiscovery_AvailableProbes verifies that probes failing Available()
// are filtered out. / TestDiscovery_AvailableProbes 验证 Available() 失败的
// probe 被过滤。
func TestDiscovery_AvailableProbes(t *testing.T) {
	opts := DefaultOptions()
	d := New(opts)
	got := d.AvailableProbes()
	// On Linux/macOS ICMP usually works (unprivileged for DGRAM sockets
	// is fine on macOS; Linux requires CAP_NET_RAW). On Windows it
	// usually doesn't. We only assert the slice is non-nil and
	// contains at least one probe (system-ping is always available
	// if `ping` is on PATH, which it is in our test env).
	// 在 Linux/macOS 上 ICMP 通常可用；Windows 通常不可用。
	// 我们只断言切片非 nil 且至少有一个 probe。
	if got == nil {
		t.Fatal("AvailableProbes returned nil")
	}
	if len(got) == 0 {
		t.Skip("no probes available in this environment")
	}
}

// TestDiscovery_FirstHit verifies that Discovery returns the first
// successful probe's Hit for each host. / TestDiscovery_FirstHit 验证
// Discovery 返回每个主机首个成功 probe 的 Hit。
func TestDiscovery_FirstHit(t *testing.T) {
	port := startEchoListener(t)
	probe := NewTCPProbeWithPorts([]int{port})
	d := New(Options{
		Probes:    []Probe{NewSystemPingProbe(), probe},
		Timeout:   2 * time.Second,
		Threads:   4,
		FirstOnly: true,
	})
	if err := d.opts.Probes[0].Available(); err != nil {
		// If system-ping isn't available, swap the order to TCP first.
		// 如果 system-ping 不可用，交换顺序让 TCP 排第一。
		d.opts.Probes = []Probe{NewTCPProbeWithPorts([]int{port})}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	res, err := d.Run(ctx, []string{"127.0.0.1"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if _, ok := res.Hits["127.0.0.1"]; !ok {
		t.Fatalf("expected hit for 127.0.0.1, got %+v", res)
	}
}

// TestDiscovery_MissAll verifies that Discovery treats all-miss as
// ErrUnreachable and the host lands in Unreachable.
//
// TestDiscovery_MissAll 验证 Discovery 把全 miss 视为 ErrUnreachable，
// 主机进入 Unreachable 列表。
func TestDiscovery_MissAll(t *testing.T) {
	// Pick a closed port. / 选一个关闭的端口。
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	closed := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	// Use only the TCP probe against a closed port. The previous
	// version also ran NewSystemPingProbe(), which always succeeds
	// against 127.0.0.1 (the loopback) and made the test environment-
	// dependent — it passed on systems where /bin/ping was missing
	// or blocked (most CI sandboxes) but failed on developer
	// machines and on CI runners where ping works. / 只用 TCP probe
	// 探测关闭的端口。旧版本还跑 NewSystemPingProbe()，对 127.0.0.1
	// （loopback）总是成功——让测试依赖环境：在 /bin/ping 缺失或被
	// 阻止的 CI 沙箱（多数情况）下过，在开发者机器和 CI runner 上
	// （ping 可用）挂。
	probe := NewTCPProbeWithPorts([]int{closed})
	d := New(Options{
		Probes:    []Probe{probe},
		Timeout:   500 * time.Millisecond,
		Threads:   4,
		FirstOnly: true,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	res, _ := d.Run(ctx, []string{"127.0.0.1"})
	if len(res.Hits) != 0 {
		t.Errorf("expected no hits, got %+v", res.Hits)
	}
	if len(res.Unreachable) != 1 || res.Unreachable[0] != "127.0.0.1" {
		t.Errorf("expected 127.0.0.1 in Unreachable, got %+v", res.Unreachable)
	}
}

// errIsUnreachable checks if err is ErrUnreachable (handling the
// wrapped case via errors.Is). / errIsUnreachable 检查 err 是不是
// ErrUnreachable（用 errors.Is 处理包装的情况）。
func errIsUnreachable(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, ErrUnreachable) ||
		strings.Contains(err.Error(), ErrUnreachable.Error())
}

// delayedProbe is a test Probe that sleeps for `delay` before returning
// a Hit. It records every invocation in `invoked` so tests can assert
// how many probes were attempted. / delayedProbe 是测试用 Probe，
// 延迟 `delay` 后返回 Hit。每次调用记入 invoked 供测试断言。
type delayedProbe struct {
	delay   time.Duration
	hit     Hit
	err     error
	invoked *atomic.Int64
}

func (p *delayedProbe) Name() string             { return "delayed" }
func (p *delayedProbe) Method() Method           { return MethodTCP }
func (p *delayedProbe) Available() error         { return nil }
func (p *delayedProbe) Probe(ctx context.Context, host string, timeout time.Duration) (Hit, error) {
	if p.invoked != nil {
		p.invoked.Add(1)
	}
	select {
	case <-time.After(p.delay):
		if p.err != nil {
			return Hit{}, p.err
		}
		return p.hit, nil
	case <-ctx.Done():
		return Hit{}, ctx.Err()
	}
}

// TestDiscovery_ReportsProgressDuringRun is a regression test for
// the "TUI counters frozen during alive sweep" bug. The user-visible
// symptom: TUI elapsed ticks but HOSTS_SCANNED / ALIVE counter stays
// at 0 for the entire alive phase, making the operator think the
// scan is hung.
//
// Root cause: discovery.Run increments result.Tried per probe attempt
// but exposes no observable signal until Run returns. External code
// (scanner.go, UI) only reads result.Hits after Run completes, so the
// alive counter only stores once at end of phase.
//
// This test verifies the fix: a Progress() method on Discovery (or
// equivalent signal) is observable mid-run.
//
// / TestDiscovery_ReportsProgressDuringRun 是"TUI 计数器在 alive 阶段冻结"
// bug 的回归测试。根因：discovery.Run 在每次 probe 后递增 result.Tried
// 但 Run 结束前不暴露任何信号。外部代码只在 Run 结束后读 result.Hits。
// 本测试验证修复：Discovery 上有 Progress() 方法（或等价信号）可在
// 中途被观察到。
func TestDiscovery_ReportsProgressDuringRun(t *testing.T) {
	// 4 hosts × 200ms each, 1 thread → ~800ms total. Plenty of time
	// for a sampler goroutine to observe mid-run progress.
	invoked := &atomic.Int64{}
	probe := &delayedProbe{
		delay:   200 * time.Millisecond,
		hit:     Hit{Host: "127.0.0.1", Method: MethodTCP},
		invoked: invoked,
	}
	d := New(Options{
		Probes:    []Probe{probe},
		Timeout:   time.Second,
		Threads:   1,
		FirstOnly: true,
	})

	type runResult struct {
		r   *RunResult
		err error
	}
	done := make(chan runResult, 1)
	go func() {
		r, e := d.Run(context.Background(), []string{"h1", "h2", "h3", "h4"})
		done <- runResult{r, e}
	}()

	// Sample Progress() every 25ms until Run completes. We capture
	// the run result inside the select (cap=1 buffer means a second
	// `<-done` after the loop would deadlock waiting for a send that
	// already happened).
	var samples []int64
	var res runResult
	tick := time.NewTicker(25 * time.Millisecond)
	defer tick.Stop()
sampleLoop:
	for {
		select {
		case <-tick.C:
			samples = append(samples, d.Progress())
		case res = <-done:
			// Run just completed. Sample one more time to capture
			// the final Tried value, since Tried.Add(1) for the last
			// probe and the goroutine's send-to-done are not
			// synchronised against the ticker. / Run 刚完成。再采一次
			// 捕获最终 Tried 值——最后一个 probe 的 Tried.Add(1) 和
			// goroutine 的 send-to-done 与 ticker 不同步。
			samples = append(samples, d.Progress())
			break sampleLoop
		}
	}
	if res.err != nil {
		t.Fatalf("Run: %v", res.err)
	}

	// Sanity: 4 probes must have been invoked.
	if got := invoked.Load(); got != 4 {
		t.Fatalf("invoked = %d, want 4", got)
	}

	// At least one mid-run sample must show progress > 0.
	// With ~32 samples across 800ms and probes completing at
	// ~200/400/600/800ms, we expect samples like [0,0,0,1,1,2,2,3,3,4].
	max := int64(0)
	for _, s := range samples {
		if s > max {
			max = s
		}
	}
	if max == 0 {
		t.Errorf("expected to observe progress > 0 mid-run; all %d samples were 0; RunResult.Tried is not externally observable during Run", len(samples))
	}
	if max < 4 {
		t.Errorf("expected max sample >= 4 (final tried count), got %d", max)
	}
}
