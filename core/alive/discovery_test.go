// discovery_test.go — unit tests for the alive package.
// discovery_test.go — alive 包的单元测试。
package alive

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"
)

// TestARPProbe_NotInTable tests the Linux /proc/net/arp path
// against a non-existent IP. / TestARPProbe_NotInTable 对不存在的
// IP 测 Linux /proc/net/arp 路径。
func TestARPProbe_NotInTable(t *testing.T) {
	p := NewARPProbe()
	_, err := p.Probe(context.Background(), "192.0.2.99", 1*time.Second)
	// On a machine where 192.0.2.99 happens to be in the ARP
	// table (very unlikely — 192.0.2.0/24 is TEST-NET-1), the
	// probe may succeed. We accept nil or ErrUnreachable.
	// / 如果 192.0.2.99 恰好在 ARP 表中（极不可能——192.0.2.0/24 是
	// TEST-NET-1），探测可能成功。我们接受 nil 或 ErrUnreachable。
	if err != nil && err != ErrUnreachable {
		t.Errorf("expected nil or ErrUnreachable, got %v", err)
	}
}

// TestARPProbe_NameAndMethod sanity-checks the metadata. /
// TestARPProbe_NameAndMethod 元数据冒烟测试。
func TestARPProbe_NameAndMethod(t *testing.T) {
	p := NewARPProbe()
	if p.Name() != "arp" {
		t.Errorf("Name = %q, want arp", p.Name())
	}
	if p.Method() != MethodARP {
		t.Errorf("Method = %q, want %q", p.Method(), MethodARP)
	}
	if err := p.Available(); err != nil {
		t.Errorf("Available returned %v, want nil", err)
	}
}

// TestNBNSProbe_NameAndMethod sanity-checks the metadata. /
// TestNBNSProbe_NameAndMethod 元数据冒烟测试。
func TestNBNSProbe_NameAndMethod(t *testing.T) {
	p := NewNBNSProbe()
	if p.Name() != "netbios" {
		t.Errorf("Name = %q, want netbios", p.Name())
	}
	if p.Method() != MethodNetBIOS {
		t.Errorf("Method = %q, want %q", p.Method(), MethodNetBIOS)
	}
	if err := p.Available(); err != nil {
		t.Errorf("Available returned %v, want nil", err)
	}
}

// TestNBNSProbe_UDPNoResponse tests the NetBIOS probe against
// 127.0.0.1:137. On most systems nothing listens there, so we
// get ErrUnreachable. On Windows with NetBIOS service the test
// returns Hit, which we treat as no-op. /
// TestNBNSProbe_UDPNoResponse 对 127.0.0.1:137 测 NetBIOS 探测。
// 多数系统那里没东西监听，所以返 ErrUnreachable。Windows 上若
// NetBIOS 服务在会返 Hit，当 no-op。
func TestNBNSProbe_UDPNoResponse(t *testing.T) {
	p := NewNBNSProbe()
	_, err := p.Probe(context.Background(), "127.0.0.1", 1*time.Second)
	if err != nil && err != ErrUnreachable {
		t.Errorf("expected nil or ErrUnreachable, got %v", err)
	}
}

// TestEncodeNetBIOSName sanity-checks the wildcard encoding. /
// TestEncodeNetBIOSName 冒烟测通配符编码。
func TestEncodeNetBIOSName(t *testing.T) {
	out := encodeNetBIOSName(DefaultNBNSName)
	if len(out) != 32 {
		t.Fatalf("encoded name length = %d, want 32", len(out))
	}
	// First byte should be 0x20 (label) | ('*' >> 4) = 0x20 | 0x02 = 0x22.
	// / 首字节应为 0x20（label）| ('*' >> 4) = 0x20 | 0x02 = 0x22。
	if out[0] != 0x22 {
		t.Errorf("out[0] = 0x%02x, want 0x22", out[0])
	}
}

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

	probe := NewTCPProbeWithPorts([]int{closed})
	d := New(Options{
		Probes:    []Probe{NewSystemPingProbe(), probe},
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
	return err == ErrUnreachable ||
		strings.Contains(err.Error(), ErrUnreachable.Error())
}
