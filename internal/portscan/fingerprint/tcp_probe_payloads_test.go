// tcp_probe_payloads_test.go — pins the TCP active-probe payload
// selectors: hint coverage, rarity ordering + cap, NULL/SSL exclusion,
// and the generic trio.
// / tcp_probe_payloads_test.go — 钉死 TCP 主动探针 payload 选择器：
// hint 覆盖、rarity 排序 + 上限、NULL/SSL 排除、通用三件套。
package fingerprint

import (
	"bytes"
	"testing"
)

func TestVScan_TCPPayloadMap(t *testing.T) {
	v := NewVScan()
	m := v.TCPPayloadMap()
	if len(m) == 0 {
		t.Fatal("TCPPayloadMap returned an empty map — TCP probes lost their ports hints?")
	}
	// Port 80 is hinted by GetRequest (and friends) in the upstream
	// database; the entry must survive the NULL/SSL exclusions.
	// / 上游库里 80 被 GetRequest（及同好）提示；条目必须在 NULL/SSL
	// 排除后仍存活。
	if pl, ok := m[80]; !ok || len(pl) == 0 {
		t.Fatalf("port 80 missing from TCPPayloadMap (have %d hinted ports)", len(m))
	}
	for port, payloads := range m {
		if len(payloads) > MaxTCPProbesPerPort {
			t.Fatalf("port %d carries %d payloads, cap is %d", port, len(payloads), MaxTCPProbesPerPort)
		}
		for _, p := range payloads {
			if len(p) == 0 {
				t.Fatalf("port %d carries an empty payload — NULL probe leaked into the map", port)
			}
		}
	}
}

func TestVScan_TCPGenericPayloads(t *testing.T) {
	v := NewVScan()
	g := v.TCPGenericPayloads()
	// GenericLines, GetRequest, Help — all rarity 1, all with decodable
	// payloads in the embedded database.
	// / GenericLines、GetRequest、Help——全是 rarity 1，嵌入库里都有
	// 可解码 payload。
	if len(g) != 3 {
		t.Fatalf("TCPGenericPayloads = %d entries, want 3 (GenericLines/GetRequest/Help)", len(g))
	}
	for i, p := range g {
		if len(p) == 0 {
			t.Fatalf("generic payload %d is empty", i)
		}
	}
	// GetRequest must be present: look for the "GET / HTTP" prefix.
	// / 必须有 GetRequest：找 "GET / HTTP" 前缀。
	var hasGet bool
	for _, p := range g {
		if bytes.HasPrefix(p, []byte("GET / HTTP")) {
			hasGet = true
		}
	}
	if !hasGet {
		t.Fatal("TCPGenericPayloads missing the GetRequest payload")
	}
}
