// ipmi_test.go — unit tests for the IPMI Identify plugin via the
// shared fakeserver package. / ipmi 识别插件的单元测试（基于
// fakeserver）。
//
// IPMI / RMCP+ runs over UDP 623. The plugin sends an RMCP+
// Session Open packet and waits for a RAKP Message 1 reply — the
// fake server hands it one with the RAKP message tag (0x12) at
// buf[7], the plugin identifies the host as a BMC, and we assert
// the Result. / IPMI / RMCP+ 跑在 UDP 623 上。插件发 RMCP+
// Session Open 包，等 RAKP Message 1 响应；假服务器在 buf[7] 返
// 回 RAKP message tag (0x12)，插件即识别为 BMC。
package ipmi

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/fakeserver"
)

// TestIpmi_IdentifyHit drives the RMCP+ Session Open → RAKP
// Message 1 happy path. The fake server returns 16 bytes with the
// RAKP message tag (0x12) at offset 7, which is what
// (*Plugin).Identify keys off of. / TestIpmi_IdentifyHit 跑 RMCP+
// Session Open → RAKP Message 1 的成功路径。假服务器在 buf[7] 返
// 回 RAKP message tag (0x12)，即 (*Plugin).Identify 的命中条件。
func TestIpmi_IdentifyHit(t *testing.T) {
	host, port := fakeserver.ListenUDPLoop(t, func(req []byte, src *net.UDPAddr) []byte {
		// 16-byte RAKP Message 1 reply. Only byte 7 (the message
		// tag) is checked by the plugin; the rest just need to be
		// present so conn.Read returns successfully.
		// / 16 字节 RAKP Message 1 响应。插件只检查 byte 7（message
		// tag），其它字节只要存在让 conn.Read 成功即可。
		resp := make([]byte, 16)
		resp[7] = 0x12 // RAKP message tag / RAKP 消息标签
		return resp
	})

	p := New()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	hit := p.Identify(ctx, host, port)
	if hit == nil {
		t.Fatal("expected hit on IPMI BMC, got nil")
	}
	if hit.Service != "ipmi" {
		t.Errorf("Service = %q, want %q", hit.Service, "ipmi")
	}
	if hit.Banner != "IPMI v2.0 (BMC)" {
		t.Errorf("Banner = %q, want %q", hit.Banner, "IPMI v2.0 (BMC)")
	}
}

// TestIpmi_IdentifyMiss verifies a non-RAKP reply yields nil. The
// fake server returns bytes without the 0x12 message tag at offset
// 7, so (*Plugin).Identify returns nil. / TestIpmi_IdentifyMiss 验
// 证非 RAKP 响应返回 nil：假服务器在 offset 7 不放 0x12 tag。
func TestIpmi_IdentifyMiss(t *testing.T) {
	host, port := fakeserver.ListenUDPLoop(t, func(req []byte, src *net.UDPAddr) []byte {
		// 16-byte reply with the WRONG message tag at offset 7
		// (0x00 instead of 0x12). Plugin must return nil.
		// / 16 字节响应但 offset 7 放错 tag（0x00 而非 0x12）。
		resp := make([]byte, 16)
		resp[7] = 0x00
		return resp
	})

	p := New()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	hit := p.Identify(ctx, host, port)
	if hit != nil {
		t.Errorf("expected nil for non-RAKP reply, got %+v", hit)
	}
}

// TestIpmi_CredentialNoop: the plugin's Credential is a documented
// no-op stub (always returns nil). Real RAKP authentication is a
// deep multi-step state machine with HMAC-SHA1 over the BMC's
// session challenge — out of scope for a happy-path fake-server
// test, and acknowledged in plan §11.2 as a known limitation for
// complex protocols. / 插件的 Credential 是文档化的空 stub（总返
// 回 nil）。真正的 RAKP 认证是多步状态机 + HMAC-SHA1，超出假服务
// 器 happy-path 测试范围；plan §11.2 已记录此限制。
func TestIpmi_CredentialNoop(t *testing.T) {
	t.Skip("ipmi.Credential is a documented no-op stub; full RAKP auth is a deep state machine out of scope (plan §11.2)")
}
