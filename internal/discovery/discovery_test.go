// discovery_test.go — unit tests for the LAN-only probes (ARP + NBNS)
// and the encodeNetBIOSName helper.
//
// discovery_test.go — LAN-only probe（ARP + NBNS）和 encodeNetBIOSName
// 帮助函数的单元测试。
package discovery

import (
	"context"
	"testing"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/core/alive"
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
	if err != nil && err != alive.ErrUnreachable {
		t.Errorf("expected nil or alive.ErrUnreachable, got %v", err)
	}
}

// TestARPProbe_NameAndMethod sanity-checks the metadata. /
// TestARPProbe_NameAndMethod 元数据冒烟测试。
func TestARPProbe_NameAndMethod(t *testing.T) {
	p := NewARPProbe()
	if p.Name() != "arp" {
		t.Errorf("Name = %q, want arp", p.Name())
	}
	if p.Method() != alive.MethodARP {
		t.Errorf("Method = %q, want %q", p.Method(), alive.MethodARP)
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
	if p.Method() != alive.MethodNetBIOS {
		t.Errorf("Method = %q, want %q", p.Method(), alive.MethodNetBIOS)
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
	if err != nil && err != alive.ErrUnreachable {
		t.Errorf("expected nil or alive.ErrUnreachable, got %v", err)
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

// TestRegistered verifies init() registered both probes into alive's
// LAN-probe registry. Together with the test's import of this package,
// any package that imports discovery in production gets the same
// registration — the test guards against the init() being removed.
//
// TestRegistered 验证 init() 已把两个 probe 注册进 alive 的 LAN-probe
// 注册表。结合本测试对本包的 import，生产代码中只要 import discovery
// 就同样得到注册——本测试守护 init() 不被误删。
func TestRegistered(t *testing.T) {
	got := alive.RegisteredLANProbes()
	names := make(map[string]bool, len(got))
	for _, p := range got {
		names[p.Name()] = true
	}
	if !names["arp"] {
		t.Errorf("alive.RegisteredLANProbes did not include arp; got %v", probeNames(got))
	}
	if !names["netbios"] {
		t.Errorf("alive.RegisteredLANProbes did not include netbios; got %v", probeNames(got))
	}
}

func probeNames(ps []alive.Probe) []string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = p.Name()
	}
	return out
}
