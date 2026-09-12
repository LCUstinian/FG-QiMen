// udp_fingerprint_test.go — unit tests for the UDP fingerprint layer:
// port-hint parsing, the payload map, and MatchUDPBanner against the
// real embedded probe DB.
// / udp_fingerprint_test.go — UDP 指纹层的单元测试：端口提示解析、
// payload 映射，以及 MatchUDPBanner 对真实内嵌探针库的匹配。
package fingerprint_test

import (
	"bytes"
	"testing"

	"github.com/LCUstinian/FG-QiMen/internal/portscan/fingerprint"
)

// TestParsePortHint covers the ports-hint grammar: empty, single,
// comma list, ranges, whitespace tolerance, and the invalid-entry
// skip path (never an error — a bad hint just drops out).
// / TestParsePortHint 覆盖 ports 提示语法：空、单个、逗号列表、范
// 围、空白容忍，以及非法条目跳过路径（不报错——坏提示直接丢弃）。
func TestParsePortHint(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []int
	}{
		{name: "empty", in: "", want: nil},
		{name: "single", in: "53", want: []int{53}},
		{name: "list+range+spaces", in: "53,1967, 26000-26004", want: []int{53, 1967, 26000, 26001, 26002, 26003, 26004}},
		{name: "duplicates dedup", in: "53,53,80", want: []int{53, 80}},
		{name: "unsorted input sorted", in: "8080,22,443", want: []int{22, 443, 8080}},
		{name: "invalid entries dropped", in: "53,abc,-1,70000,0,99999", want: []int{53}},
		{name: "open range dropped", in: "80-, -90,161", want: []int{161}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := fingerprint.ParsePortHint(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("ParsePortHint(%q) = %v, want %v", tc.in, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("ParsePortHint(%q) = %v, want %v", tc.in, got, tc.want)
				}
			}
		})
	}
}

// TestVScan_UDPHintPorts verifies the hint union covers the
// well-known services the DB has UDP probes for.
// / TestVScan_UDPHintPorts 验证提示并集覆盖库里注册了 UDP probe 的
// 常见服务端口。
func TestVScan_UDPHintPorts(t *testing.T) {
	v := fingerprint.NewVScan()
	hints := v.UDPHintPorts()
	if len(hints) == 0 {
		t.Fatal("UDPHintPorts returned an empty set — UDP probes lost their ports hints?")
	}
	for _, port := range []int{53 /*DNS*/, 137 /*NBTStat*/, 161 /*SNMP*/, 11211 /*memcached*/} {
		if _, ok := hints[port]; !ok {
			t.Errorf("hint set missing port %d", port)
		}
	}
}

// TestVScan_UDPPayloadMap pins decode fidelity: probe Data is stored
// as ESCAPED text (`\0\x06\x01`), and the payload map must deliver the
// DECODED bytes — a literal-backslash payload would be sent as ASCII
// text and never elicit a DNS response.
// / TestVScan_UDPPayloadMap 钉死解码保真：probe Data 以转义文本存储
// （`\0\x06\x01`），payload 映射必须交出解码后的字节——按字面反斜杠
// 发送的是 ASCII 文本，永远引不出 DNS 响应。
func TestVScan_UDPPayloadMap(t *testing.T) {
	v := fingerprint.NewVScan()
	m := v.UDPPayloadMap()

	// DNS (port 53): DNSVersionBindReq payload decodes to
	// 00 06 01 00 00 01 ... / DNS（53）：DNSVersionBindReq payload 解
	// 码为 00 06 01 00 00 01 ...
	dnsPayloads := m[53]
	if len(dnsPayloads) == 0 {
		t.Fatal("no payloads for port 53")
	}
	dnsOK := false
	for _, p := range dnsPayloads {
		if len(p) >= 3 && p[0] == 0x00 && p[1] == 0x06 && p[2] == 0x01 {
			dnsOK = true
		}
	}
	if !dnsOK {
		t.Errorf("port 53 payloads lack the decoded DNSVersionBindReq prefix 00 06 01; got %d payloads", len(dnsPayloads))
		for i, p := range dnsPayloads {
			t.Logf("payload[%d] prefix: % x", i, p[:min(8, len(p))])
		}
	}

	// memcached (11211): payload must START with the decoded binary
	// header 00 01 00 00 00 01, then "stats\r\n".
	// / memcached（11211）：payload 必须以解码后的二进制头
	// 00 01 00 00 00 01 开头，后接 "stats\r\n"。
	mcPayloads := m[11211]
	if len(mcPayloads) == 0 {
		t.Fatal("no payloads for port 11211")
	}
	mcOK := false
	for _, p := range mcPayloads {
		if bytes.HasPrefix(p, []byte{0x00, 0x01, 0x00, 0x00, 0x00, 0x01}) && bytes.HasSuffix(p, []byte("stats\r\n")) {
			mcOK = true
		}
	}
	if !mcOK {
		t.Errorf("port 11211 payloads lack the decoded memcached header + stats payload; got %d payloads", len(mcPayloads))
	}
}

// TestVScan_MatchUDPBanner_Memcached verifies a hard match against the
// real memcached UDP rule, including $N version expansion. The
// response format is the binary-header + "STAT ..." body the rule
// anchors on. / TestVScan_MatchUDPBanner_Memcached 验证对真实
// memcached UDP 规则的硬匹配，含 $N 版本展开。响应格式是该规则锚定
// 的"二进制头 + STAT ... 主体"。
func TestVScan_MatchUDPBanner_Memcached(t *testing.T) {
	v := fingerprint.NewVScan()
	banner := append([]byte{0x00, 0x01, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00}, []byte("STAT pid 1\r\nSTAT uptime 2\r\nSTAT time 3\r\nSTAT version 1.6.9\r\n")...)
	m, ok := v.MatchUDPBanner(banner)
	if !ok {
		t.Fatal("expected memcached UDP response to hard-match")
	}
	if m.Soft {
		t.Fatalf("expected HARD match, got soft %q", m.Service)
	}
	if m.Service != "memcached" {
		t.Errorf("Service = %q, want %q", m.Service, "memcached")
	}
	if m.Product != "Memcached" {
		t.Errorf("Product = %q, want %q", m.Product, "Memcached")
	}
	if m.Version != "1.6.9" {
		t.Errorf("Version = %q, want %q ($N expansion)", m.Version, "1.6.9")
	}
}

// TestVScan_MatchUDPBanner_TCPRuleDoesNotLeak pins the protocol
// isolation: an SSH banner (a TCP-probe signature) must NOT be
// hard-matched by the UDP rule set. / TestVScan_MatchUDPBanner_TCPRuleDoesNotLeak
// 钉死协议隔离：SSH banner（TCP probe 签名）绝不能被 UDP 规则集硬
// 匹配。
func TestVScan_MatchUDPBanner_TCPRuleDoesNotLeak(t *testing.T) {
	v := fingerprint.NewVScan()
	m, ok := v.MatchUDPBanner([]byte("SSH-2.0-OpenSSH_8.9p1 Ubuntu-3ubuntu0.1\r\n"))
	if ok && !m.Soft {
		t.Errorf("UDP rules hard-matched a TCP signature: service=%q", m.Service)
	}
}

// TestVScan_MatchUDPBanner_Noise re-runs the garbage-lab contract on
// the UDP rule set: pseudo-random binary noise (shaped like a real UDP
// response — non-printable bytes) must never HARD-match. Soft hits are
// tolerated but logged (soft rules are protocol-shape hints, not
// fingerprints). / TestVScan_MatchUDPBanner_Noise 在 UDP 规则集上复跑
// 垃圾字节实验契约：伪随机二进制噪声（形似真实 UDP 响应——含不可打
// 印字节）绝不能硬匹配。soft 命中可容忍但记录（soft 规则是协议形态
// 提示，不是指纹）。
func TestVScan_MatchUDPBanner_Noise(t *testing.T) {
	v := fingerprint.NewVScan()
	// Deterministic LCG so failures reproduce.
	// / 确定性 LCG 保证失败可复现。
	seed := uint32(0x5EED1234)
	for i := 0; i < 300; i++ {
		buf := make([]byte, 64)
		for j := range buf {
			seed = seed*1664525 + 1013904223
			buf[j] = byte(seed >> 16)
		}
		m, ok := v.MatchUDPBanner(buf)
		if ok && !m.Soft {
			t.Fatalf("noise banner #%d hard-matched %q — new phantom UDP rule?", i, m.Service)
		}
		if ok && m.Soft {
			t.Logf("noise banner #%d soft-matched %q (tolerated)", i, m.Service)
		}
	}
}
