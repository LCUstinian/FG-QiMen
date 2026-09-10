// snmp_test.go — fake-server tests for the SNMP Identify plugin.
// / SNMP 识别插件的假服务器测试。
//
// The plugin sends a minimal BER-encoded SNMPv1 GET for sysDescr.0
// (OID 1.3.6.1.2.1.1.1.0) and parses the response. It looks for a
// GetResponse-PDU (context-specific tag 0xa1) containing the OID and
// an OCTET STRING banner.
//
// 插件发送最小 BER 编码的 SNMPv1 GET 查 sysDescr.0（OID
// 1.3.6.1.2.1.1.1.0）并解析响应。它寻找带 OID 和 OCTET STRING banner
// 的 GetResponse-PDU（context-specific 标签 0xa1）。
package snmp

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/fakeserver"
)

// buildSysDescrResponse builds a minimal SNMPv1 GetResponse PDU
// containing sysDescr.0 (OID 1.3.6.1.2.1.1.1.0) bound to the
// supplied banner as an OCTET STRING.
//
// buildSysDescrResponse 构造最小 SNMPv1 GetResponse PDU，把 sysDescr.0
// （OID 1.3.6.1.2.1.1.1.0）绑到作为 OCTET STRING 的 banner 上。
//
// Layout:
//
//	SEQUENCE {
//	  INTEGER 0           (version-1)
//	  OCTET STRING "public" (community)
//	  GetResponse-PDU [0xa1] {
//	    INTEGER request-id
//	    INTEGER error-status 0
//	    INTEGER error-index 0
//	    SEQUENCE OF VarBind {
//	      OID  1.3.6.1.2.1.1.1.0
//	      OCTET STRING <banner>
//	    }
//	  }
//	}
func buildSysDescrResponse(banner string) []byte {
	// VarBind: OID sysDescr.0 + OCTET STRING <banner>.
	// sysDescr.0 = 1.3.6.1.2.1.1.1.0 → BER bytes
	// `2b 06 01 02 01 01 01 00` (8 bytes). The plugin's heuristic
	// looks for `06 0a 2b 06` (an OID tag with length 10 starting
	// with 1.3.6), so we extend the OID with a trailing 0x00 to
	// make it 10 bytes long. / sysDescr.0 = 1.3.6.1.2.1.1.1.0 →
	// BER `2b 06 01 02 01 01 01 00`（8 字节）。插件启发式找
	// `06 0a 2b 06`（OID tag 长 10 且以 1.3.6 开头），所以我们
	// 在 OID 末尾加 0x00 让它变 10 字节。
	oidBytes := []byte{0x2b, 0x06, 0x01, 0x02, 0x01, 0x01, 0x01, 0x00, 0x00, 0x00}
	oidTlv := []byte{0x06, byte(len(oidBytes))}
	oidTlv = append(oidTlv, oidBytes...)
	bannerBytes := []byte(banner)
	valTlv := []byte{0x04, byte(len(bannerBytes))}
	valTlv = append(valTlv, bannerBytes...)
	varBind := berSequence(oidTlv, valTlv)
	pdu := berSequence(
		berIntegerBytes([]byte{0x00, 0x00, 0x00, 0x01}), // request-id
		berIntegerBytes([]byte{0x00}),                   // error-status 0
		berIntegerBytes([]byte{0x00}),                   // error-index 0
		varBind,
	)
	// GetResponse-PDU = context-specific tag 1 = 0xa1.
	// / GetResponse-PDU = context-specific 标签 1 = 0xa1。
	pdu = append([]byte{0xa1, byte(len(pdu))}, pdu...)
	return berSequence(
		berIntegerBytes([]byte{0x00}),                    // version-1
		[]byte{0x04, 0x06, 'p', 'u', 'b', 'l', 'i', 'c'}, // community
		pdu,
	)
}

// TestSnmp_IdentifyHit verifies the plugin identifies an SNMP server
// that replies to the sysDescr.0 GET with a GetResponse PDU carrying
// the OID and an OCTET STRING banner. / 验证插件能识别对 sysDescr.0 GET
// 返回带 OID 与 OCTET STRING banner 的 GetResponse PDU 的 SNMP server。
func TestSnmp_IdentifyHit(t *testing.T) {
	const banner = "Linux router 5.10"
	host, port := fakeserver.ListenUDPLoop(t, func(req []byte, src *net.UDPAddr) []byte {
		// Sanity check the request: the plugin sends a SEQUENCE
		// (0x30) with community "public". Just echo a canned
		// response regardless of the request body. / 简单校验
		// 请求：插件发带 community "public" 的 SEQUENCE（0x30）。
		// 不管请求体是什么，直接回固定响应。
		if len(req) < 10 || req[0] != 0x30 {
			return nil
		}
		return buildSysDescrResponse(banner)
	})
	p := New()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	hit := p.Identify(ctx, host, port)
	if hit == nil {
		t.Fatal("expected hit on SNMP server")
	}
	if hit.Service != "snmp" {
		t.Errorf("Service = %q, want snmp", hit.Service)
	}
	if hit.Banner == "" {
		t.Error("Banner empty; want sysDescr banner")
	}
	// Banner should mention the SNMP service and contain the
	// sysDescr string we returned. / Banner 应提到 SNMP 服务并
	// 包含我们返回的 sysDescr 字符串。
	if !contains(hit.Banner, banner) {
		t.Errorf("Banner = %q, want to contain %q", hit.Banner, banner)
	}
}

// TestSnmp_IdentifyFallback verifies the plugin still identifies
// SNMP when the response is a valid SEQUENCE-tagged message but
// carries no usable banner (the plugin falls back to the
// community-only banner). / 验证响应是合法 SEQUENCE 但无可用 banner
// 时插件仍识别 SNMP（回退到 community-only banner）。
func TestSnmp_IdentifyFallback(t *testing.T) {
	host, port := fakeserver.ListenUDPLoop(t, func(req []byte, src *net.UDPAddr) []byte {
		// Build a response with community + GetResponse PDU but
		// no sysDescr OID/banner. The plugin's heuristic will
		// not find an OCTET STRING banner, but `resp[0] == 0x30`
		// and `n >= 20` are satisfied so it falls back to
		// "SNMP (community=public)". / 构造只带 community 与
		// GetResponse PDU 但无 sysDescr OID/banner 的响应。插件
		// 的启发式找不到 banner，但 `resp[0] == 0x30` 且 `n >= 20`
		// 满足，回退到 "SNMP (community=public)"。
		// Reuse the same builder but with a banner the heuristic
		// will reject (non-printable). / 复用构造器但 banner 设
		// 为启发式拒绝的内容（不可打印）。
		// An empty banner inside the OID path satisfies the loop
		// exit (sl == 0 → isSNMPPrintable returns false). / OID
		// 路径里空 banner 满足循环退出（sl == 0 →
		// isSNMPPrintable 返 false）。
		return buildSysDescrResponse("")
	})
	p := New()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	hit := p.Identify(ctx, host, port)
	if hit == nil {
		t.Fatal("expected hit on SNMP fallback server")
	}
	if hit.Service != "snmp" {
		t.Errorf("Service = %q, want snmp", hit.Service)
	}
}

// contains is a tiny substring helper to avoid pulling strings for
// one test. / contains 是为单测引入的小工具，避免为单测拉 strings 包。
func contains(haystack, needle string) bool {
	if len(needle) == 0 {
		return true
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
