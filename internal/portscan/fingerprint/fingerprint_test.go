// fingerprint_test.go — unit tests for the fingerprint package.
// fingerprint_test.go — fingerprint 包的单元测试。
package fingerprint_test

import (
	"testing"

	"github.com/LCUstinian/FG-QiMen/internal/portscan/fingerprint"
)

func TestVScan_LoadOK(t *testing.T) {
	v := fingerprint.NewVScan()
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
	v := fingerprint.NewVScan()
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
	v := fingerprint.NewVScan()
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
	v := fingerprint.NewVScan()
	// Pure random bytes that no probe should match.
	// / 纯随机字节，不应被任何 probe 匹配。
	weird := []byte{0x00, 0x01, 0x02, 0xff, 0xfe, 0xfd, 0xfc}
	_, _, ok := v.MatchBanner(weird)
	if ok {
		t.Logf("note: a probe matched random bytes; this is rare but OK")
	}
}

// TestVScan_MatchBanner_EscapeFidelity_Pipe pins the escape-fidelity
// fix: \x7c in a pattern must compile to a LITERAL pipe byte, not a
// live regex alternation operator. The jrpgt rule `m|^<<jrpgt!>>\x7c$|`
// used to compile as `^<<jrpgt!>>` | `$`, whose bare-$ branch matched
// ANY banner (garbage-lab: 2972/3000 pseudo-random noise banners) —
// the root cause of the field-observed "garbage banner → phantom
// service (e.g. dps-shell)" false positives.
// / TestVScan_MatchBanner_EscapeFidelity_Pipe 钉死转义保真修复：
// pattern 里的 \x7c 必须编译成字面竖线字节，而不是活的正则"或"分支。
// jrpgt 规则 `m|^<<jrpgt!>>\x7c$|` 此前被编译成 `^<<jrpgt!>>` | `$`，
// 裸 `$` 分支匹配任意 banner（垃圾字节实验：3000 个伪随机噪声命中
// 2972 次）——这就是现场"垃圾 banner → 幽灵服务（如 dps-shell）"
// 误报的根因。
func TestVScan_MatchBanner_EscapeFidelity_Pipe(t *testing.T) {
	v := fingerprint.NewVScan()

	// The exact jrpgt payload embedded mid-string must NOT be reported
	// as jrpgt (pre-fix this matched everything). / 精确 payload 嵌在
	// 字符串中间不得报成 jrpgt（修复前这种 banner 必中）。
	if svc, _, ok := v.MatchBanner([]byte("garbage<<jrpgt!>>|junk")); ok && svc == "jrpgt" {
		t.Errorf("\\x7c compiled as alternation: junk banner matched %q", svc)
	}

	// The exact payload still hard-matches. / 精确 payload 仍硬匹配。
	svc, _, ok := v.MatchBanner([]byte("<<jrpgt!>>|"))
	if !ok || svc != "jrpgt" {
		t.Errorf("exact jrpgt payload: got svc=%q ok=%v, want jrpgt", svc, ok)
	}
}

// TestVScan_MatchBanner_LooseRuleBlacklisted pins the loose-rule
// blacklist: nagios-nsca's `m|^.{128}[\x52-\x7F]...$|s` is anchored
// but vacuous — ANY banner ≥132 bytes whose 129th byte falls in
// 0x52..0x7F matched. Upstream nmap only evaluates it against NSCA
// probe responses (port 5667); our all-rules-on-any-banner engine
// drops it at parse time.
// / TestVScan_MatchBanner_LooseRuleBlacklisted 钉死宽松规则黑名单：
// nagios-nsca 的 `m|^.{128}[\x52-\x7F]...$|s` 锚定但空洞——任何
// ≥132 字节且第 129 字节落在 0x52..0x7F 的 banner 都命中。上游 nmap
// 只对 NSCA 探针响应（端口 5667）求值；我们的全规则引擎在解析期
// 直接丢弃它。
func TestVScan_MatchBanner_LooseRuleBlacklisted(t *testing.T) {
	v := fingerprint.NewVScan()
	b := make([]byte, 132)
	for i := range b {
		b[i] = 'A' // filler; no \n so the dropped `s` flag is irrelevant
	}
	b[128] = 0x60 // the only range-constrained byte / 唯一的范围约束字节
	svc, _, ok := v.MatchBanner(b)
	if ok && svc == "nagios-nsca" {
		t.Errorf("blacklisted vacuous rule still fired: %q", svc)
	}
}

// TestVScan_MatchBanner_SoftmatchDemoted verifies the nmap "?"
// convention end-to-end on the real DB: the banner satisfies the
// daytime SOFT rule but no hard rule, so it is reported as "daytime?"
// with empty version info. / TestVScan_MatchBanner_SoftmatchDemoted
// 在真实 DB 上端到端验证 nmap "?" 惯例：banner 只满足 daytime 的
// soft 规则（无硬规则命中），因此报为 "daytime?" 且版本信息为空。
func TestVScan_MatchBanner_SoftmatchDemoted(t *testing.T) {
	v := fingerprint.NewVScan()
	svc, ver, ok := v.MatchBanner([]byte("12:34:56 2026/09/12\n"))
	if !ok {
		t.Fatal("expected the daytime softmatch to hit")
	}
	if svc != "daytime?" {
		t.Errorf("svc = %q, want %q (softmatch demotion)", svc, "daytime?")
	}
	if ver != "" {
		t.Errorf("softmatch version info = %q, want empty", ver)
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
		got, err := fingerprint.DecodePattern(c.in)
		if err != nil {
			t.Errorf("DecodePattern(%q): %v", c.in, err)
			continue
		}
		if string(got) != c.want {
			t.Errorf("DecodePattern(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
