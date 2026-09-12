// hostmatch_test.go — tests for the exclude-hosts matcher.
// hostmatch_test.go — 排除主机匹配器的测试。
package types

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHostMatcherMatch(t *testing.T) {
	m, err := NewHostMatcher("192,172.16.5.0/24,10.0.0.5-10.0.0.9,172.20.1.1-9,192.168.1.1,nas.local")
	if err != nil {
		t.Fatalf("NewHostMatcher: %v", err)
	}
	cases := []struct {
		addr string
		want bool
	}{
		{"192.168.5.5", true},  // 192 shortcut / 192 快捷
		{"192.0.2.1", false},   // outside 192.168/16 / 192.168/16 之外
		{"172.16.5.77", true},  // CIDR
		{"172.16.6.77", false}, // outside CIDR / CIDR 之外
		{"10.0.0.5", true},     // full range start / 完整范围起点
		{"10.0.0.9", true},     // full range end / 完整范围终点
		{"10.0.0.10", false},   // just past range / 范围外
		{"172.20.1.1", true},   // suffix range start / 后缀范围起点
		{"172.20.1.9", true},   // suffix range end / 后缀范围终点
		{"172.20.1.10", false}, // past suffix range / 后缀范围外
		{"192.168.1.1", true},  // exact (also inside 192 shortcut) / 精确（同时在 192 快捷内）
		{"172.16.6.2", false},  // outside the /24 and other entries / /24 与其他条目之外
		{"NAS.LOCAL", true},    // hostname case-insensitive / 主机名大小写不敏感
		{"other.host", false},  // unrelated hostname / 无关主机名
		{"2001:db8::1", false}, // IPv6 passthrough / IPv6 直通
		{"", false},            // empty never matches / 空不匹配
	}
	for _, c := range cases {
		if got := m.Match(c.addr); got != c.want {
			t.Errorf("Match(%q) = %v, want %v", c.addr, got, c.want)
		}
	}
	if m.Len() != 6 {
		t.Errorf("Len() = %d, want 6 entries", m.Len())
	}
}

func TestHostMatcherRejectsBadEntries(t *testing.T) {
	for _, spec := range []string{"10.0.0.0/33", "10.0.0.5-nas", "300.1.1.1-2", "10.0.0.5-banana"} {
		if _, err := NewHostMatcher(spec); err == nil {
			t.Errorf("NewHostMatcher(%q) = nil error, want error", spec)
		}
	}
}

func TestApplyExcludeHosts(t *testing.T) {
	targets := []Target{
		{Addr: "192.168.1.1"},
		{Addr: "192.168.1.2"},
		{Addr: "10.0.0.1"},
		{Addr: "nas.local"},
	}

	// Spec only. / 仅 spec。
	got, removed, err := ApplyExcludeHosts(targets, "192.168.1.1,10", "")
	if err != nil {
		t.Fatalf("ApplyExcludeHosts: %v", err)
	}
	if removed != 2 || len(got) != 2 || got[0].Addr != "192.168.1.2" || got[1].Addr != "nas.local" {
		t.Errorf("spec-only filter: removed=%d got=%v", removed, got)
	}

	// Empty spec+file is a no-op returning the same slice. / 空 spec+file
	// 是 no-op，返回原 slice。
	same, removed, err := ApplyExcludeHosts(targets, "", "")
	if err != nil || removed != 0 || len(same) != len(targets) {
		t.Errorf("no-op filter: removed=%d len=%d err=%v", removed, len(same), err)
	}

	// File with comments + spec combined. / 文件（含注释）与 spec 组合。
	file := filepath.Join(t.TempDir(), "excl.txt")
	content := "# infrastructure\n10.0.0.1\n\nnas.local\n"
	if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	got, removed, err = ApplyExcludeHosts(targets, "192.168.1.2", file)
	if err != nil {
		t.Fatalf("ApplyExcludeHosts with file: %v", err)
	}
	if removed != 3 || len(got) != 1 || got[0].Addr != "192.168.1.1" {
		t.Errorf("file+spec filter: removed=%d got=%v, want 3/[192.168.1.1]", removed, got)
	}

	// Missing file must fail loudly. / 文件缺失必须显式报错。
	if _, _, err := ApplyExcludeHosts(targets, "", filepath.Join(t.TempDir(), "nope.txt")); err == nil {
		t.Error("missing exclude file: want error, got nil")
	}
}
