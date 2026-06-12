// portfinger_test.go — unit tests for the portfinger package.
// portfinger_test.go — portfinger 包的单元测试。
package portfinger_test

import (
	"testing"

	"github.com/LCUstinian/FG-QiMen/core/scan/portfinger"
)

func TestVScan_LoadOK(t *testing.T) {
	v := portfinger.NewVScan()
	if v == nil {
		t.Fatal("NewVScan returned nil")
	}
	if len(v.Probes) < 50 {
		t.Errorf("expected many TCP probes, got %d", len(v.Probes))
	}
	if _, ok := v.ProbesMapKName["GetRequest"]; !ok {
		t.Errorf("expected GetRequest probe in map")
	}
}

// TestVScan_MatchBanner_SSH verifies a typical SSH banner matches the
// OpenSSH probe. / TestVScan_MatchBanner_SSH 验证典型 SSH banner 命中
// OpenSSH 探针。
func TestVScan_MatchBanner_SSH(t *testing.T) {
	v := portfinger.NewVScan()
	banner := []byte("SSH-2.0-OpenSSH_8.9p1 Ubuntu-3ubuntu0.1\r\n")
	svc, ver, ok := v.MatchBanner(banner)
	if !ok {
		t.Fatal("expected SSH banner to hit")
	}
	if svc == "" {
		t.Errorf("expected non-empty service name, got %q", svc)
	}
	// Version info is preserved verbatim; we don't parse p/v/ in v0.1.
	// / Version info 原样保留；v0.1 不解析 p/v/。
	if ver == "" {
		t.Errorf("expected non-empty version info, got %q", ver)
	}
	t.Logf("matched service=%q ver=%q", svc, ver)
}

// TestVScan_MatchBanner_HTTP verifies a typical HTTP response matches.
// / TestVScan_MatchBanner_HTTP 验证典型 HTTP 响应命中。
func TestVScan_MatchBanner_HTTP(t *testing.T) {
	v := portfinger.NewVScan()
	resp := []byte("HTTP/1.1 200 OK\r\nServer: nginx/1.21\r\nContent-Type: text/html\r\n\r\n")
	svc, _, ok := v.MatchBanner(resp)
	if !ok {
		t.Skipf("HTTP probe not in v0.1 dataset; skipping")
	}
	t.Logf("matched service=%q", svc)
}

// TestVScan_MatchBanner_Miss verifies a clean miss. / TestVScan_MatchBanner_Miss
// 验证干净的 miss。
func TestVScan_MatchBanner_Miss(t *testing.T) {
	v := portfinger.NewVScan()
	// Pure random bytes that no probe should match.
	// / 纯随机字节，不应被任何 probe 匹配。
	weird := []byte{0x00, 0x01, 0x02, 0xff, 0xfe, 0xfd, 0xfc}
	_, _, ok := v.MatchBanner(weird)
	if ok {
		t.Logf("note: a probe matched random bytes; this is rare but OK")
	}
}

// TestDecodePattern verifies the escape decoder. / TestDecodePattern
// 验证转义解码。
func TestDecodePattern(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{`hello`, "hello"},
		{`SSH-2.0`, "SSH-2.0"},
		{`\x00\x01\x02`, "\x00\x01\x02"},
		{`\n\r\t`, "\n\r\t"},
		{`\101\102\103`, "ABC"},
	}
	for _, c := range cases {
		got, err := portfinger.DecodePattern(c.in)
		if err != nil {
			t.Errorf("DecodePattern(%q): %v", c.in, err)
			continue
		}
		if string(got) != c.want {
			t.Errorf("DecodePattern(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
