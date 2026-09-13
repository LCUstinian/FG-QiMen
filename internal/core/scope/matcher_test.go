// matcher_test.go — in-scope predicate and expansion safety gates. The
// gate tests are the security-relevant core: expansion must never
// reach public IPs, operators' exclusions must hold, and the batch
// must stay bounded.
//
// matcher_test.go — 范围内谓词与扩展安全门。门测试是安全相关的核心：
// 扩展绝不能触达公网 IP、操作员排除必须生效、批次必须有界。
package scope

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/LCUstinian/FG-QiMen/internal/types"
)

func TestBuildInScopeMatcher(t *testing.T) {
	t.Run("empty spec yields nil matcher", func(t *testing.T) {
		m, err := BuildInScopeMatcher("", "")
		if err != nil {
			t.Fatalf("BuildInScopeMatcher: %v", err)
		}
		if m != nil {
			t.Fatalf("want nil matcher for empty spec, got non-nil")
		}
	})

	t.Run("CIDR spec", func(t *testing.T) {
		m, err := BuildInScopeMatcher("192.168.1.0/24", "")
		if err != nil {
			t.Fatalf("BuildInScopeMatcher: %v", err)
		}
		if !m("192.168.1.57") {
			t.Fatal("192.168.1.57 should be in 192.168.1.0/24")
		}
		if m("192.168.2.57") {
			t.Fatal("192.168.2.57 should be outside")
		}
	})

	t.Run("hosts-file with comments and blanks", func(t *testing.T) {
		dir := t.TempDir()
		f := filepath.Join(dir, "targets.txt")
		content := "# comment line\n\n10.0.0.5\n   \n10.0.0.0/24\n"
		if err := os.WriteFile(f, []byte(content), 0o600); err != nil {
			t.Fatalf("write hosts-file: %v", err)
		}
		m, err := BuildInScopeMatcher("", f)
		if err != nil {
			t.Fatalf("BuildInScopeMatcher: %v", err)
		}
		for _, ip := range []string{"10.0.0.5", "10.0.0.99"} {
			if !m(ip) {
				t.Fatalf("%s should be in scope", ip)
			}
		}
		if m("10.1.0.5") {
			t.Fatal("10.1.0.5 should be outside")
		}
	})

	t.Run("missing hosts-file is an error", func(t *testing.T) {
		if _, err := BuildInScopeMatcher("", filepath.Join(t.TempDir(), "nope.txt")); err == nil {
			t.Fatal("want error for missing hosts-file")
		}
	})
}

func TestIsRFC1918(t *testing.T) {
	tests := []struct {
		addr string
		want bool
	}{
		{"10.0.0.1", true},
		{"10.255.255.255", true},
		{"172.16.0.1", true},
		{"172.31.255.255", true},
		{"192.168.1.1", true},
		{"172.15.0.1", false},  // just below 172.16/12
		{"172.32.0.1", false},  // just above 172.16/12
		{"192.169.0.1", false}, // not 192.168/16
		{"8.8.8.8", false},
		{"11.0.0.1", false},
		{"2001:db8::1", false},
		{"not-an-ip", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := IsRFC1918(tt.addr); got != tt.want {
			t.Errorf("IsRFC1918(%q) = %v, want %v", tt.addr, got, tt.want)
		}
	}
}

func TestSameSubnet24(t *testing.T) {
	tests := []struct {
		a, b string
		want bool
	}{
		{"192.168.1.1", "192.168.1.254", true},
		{"192.168.1.1", "192.168.2.1", false},
		{"10.0.0.1", "10.0.0.1", true},
		{"10.0.0.1", "8.8.8.8", false},
		{"2001:db8::1", "2001:db8::2", false},
		{"bogus", "10.0.0.1", false},
	}
	for _, tt := range tests {
		if got := SameSubnet24(tt.a, tt.b); got != tt.want {
			t.Errorf("SameSubnet24(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.want)
		}
	}
}

// ev is a compact DiscoveryEvent builder for the gate tests.
// / ev 是门测试用的紧凑 DiscoveryEvent 构造器。
func ev(ip string) types.DiscoveryEvent {
	return types.DiscoveryEvent{IP: ip, SourceProtocol: "test"}
}

func TestExpansionCandidates_Gates(t *testing.T) {
	scanned := []string{"192.168.1.10"}
	excl, err := BuildHostMatcher("192.168.1.77", "")
	if err != nil {
		t.Fatalf("BuildHostMatcher: %v", err)
	}
	events := []types.DiscoveryEvent{
		ev("192.168.1.20"), // pass / 通过
		ev("192.168.1.10"), // already scanned / 已扫
		ev("8.8.8.8"),      // public — never / 公网——绝不
		ev("172.32.0.9"),   // not RFC1918 / 非 RFC1918
		ev("192.168.2.20"), // different /24 — rejected / 不同 /24——拒绝
		ev(""),             // no IP — cannot probe / 无 IP——无法探测
		ev("192.168.1.77"), // operator-excluded / 操作员排除
		ev("192.168.1.20"), // duplicate within batch / 批内去重
	}
	got := ExpansionCandidates(events, scanned, excl, 64)
	want := []string{"192.168.1.20"}
	if len(got) != 1 || got[0] != want[0] {
		t.Fatalf("ExpansionCandidates = %v, want %v", got, want)
	}
}

func TestExpansionCandidates_BoundedByMaxHosts(t *testing.T) {
	var events []types.DiscoveryEvent
	for i := 20; i < 40; i++ {
		events = append(events, ev(fmt.Sprintf("192.168.1.%d", i)))
	}
	got := ExpansionCandidates(events, []string{"192.168.1.1"}, nil, 5)
	if len(got) != 5 {
		t.Fatalf("want exactly 5 candidates, got %d: %v", len(got), got)
	}
}

func TestExpansionCandidates_NilExclusion(t *testing.T) {
	got := ExpansionCandidates([]types.DiscoveryEvent{ev("192.168.1.20")}, []string{"192.168.1.1"}, nil, 64)
	if len(got) != 1 {
		t.Fatalf("nil exclusion should not block candidates, got %v", got)
	}
}
