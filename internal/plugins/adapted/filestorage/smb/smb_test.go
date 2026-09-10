// smb_test.go — fake-server test for the smb Identify plugin.
//
// Stateful TCP plugin (v0.6.0 plan Tier 3): the plugin writes a 36-byte
// SMB2 negotiate request (4-byte NetBIOS header + 32-byte SMB2 header),
// then reads up to 1024 bytes and scans for the SMB magic (\xFESMB or
// \xFFSMB) at position >= 4. The handler closure reads the request
// before replying so we exercise the full state-machine step rather
// than just one-shot writes.
//
// Pattern: fakeserver.ListenLoop binds 127.0.0.1:0, the handler
// drives the protocol greeting / state-machine step the plugin
// expects, and assertions verify Identify classifies the response.
//
// / smb_test.go — smb Identify 插件的假服务器测试。有状态 TCP 插件
// （v0.6.0 plan Tier 3）：plugin 写 36 字节 SMB2 negotiate 请求，然后
// 读 1024 字节扫 SMB magic（\xFESMB 或 \xFFSMB）于 position >= 4。
// handler 闭包先读 request 再 reply，跑完整状态机步骤而不是单次写。
package smb

import (
	"context"
	"encoding/binary"
	"net"
	"testing"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/fakeserver"
)

// buildSMB2NegotiateResponse builds a minimal but wire-accurate SMB2
// NEGOTIATE_PROTOCOL_RESPONSE: a 4-byte NetBIOS session message
// header, a 64-byte SMB2 header (with the \xFESMB magic at offset 4
// relative to the start of the read), and a tiny NEGOTIATE body. The
// plugin only inspects bytes for the magic, but a realistic layout
// keeps the test legible and would catch future parser regressions
// if the loop scan grows stricter.
//
// / buildSMB2NegotiateResponse 构造一个最小但线上准确的 SMB2
// NEGOTIATE_PROTOCOL_RESPONSE：4 字节 NetBIOS session message 头 +
// 64 字节 SMB2 头（\xFESMB magic 在相对读起始位置的 offset 4）+ 一
// 小段 NEGOTIATE body。plugin 只扫 magic，但真实布局可读性好，若
// loop scan 变严格也能抓住回归。
func buildSMB2NegotiateResponse() []byte {
	// SMB2 NEGOTIATE_PROTOCOL_RESPONSE body (post-header) — security
	// mode, capabilities, max sizes, server GUID, system time,
	// server start time, etc. Kept minimal since the plugin only
	// cares about the header magic. / SMB2 NEGOTIATE_PROTOCOL_RESPONSE
	// body（header 之后）—— security mode、capabilities、max sizes、
	// server GUID、system time、server start time 等。只保最小，因
	// plugin 只关心 header magic。
	body := []byte{
		0x03, 0x00, // SecurityMode: signing enabled + required
		0x07, 0x00, 0x00, 0x00, // Capabilities: DFS | LE | MULTI
		0x00, 0x10, 0x00, 0x00, // MaxTransSize
		0x00, 0x10, 0x00, 0x00, // MaxReadSize
		0x00, 0x10, 0x00, 0x00, // MaxWriteSize
		// ServerGuid (16 bytes) / ServerGuid（16 字节）
		0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,
		0x09, 0x0a, 0x0b, 0x0c, 0x0d, 0x0e, 0x0f, 0x10,
		// SystemTime / ServerStartTime (8 bytes each) — zeros OK
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	}

	// SMB2 header (64 bytes including the magic).
	// / SMB2 头（含 magic 共 64 字节）。
	hdr := make([]byte, 64)
	copy(hdr[0:4], []byte{0xFE, 'S', 'M', 'B'}) // magic
	binary.LittleEndian.PutUint16(hdr[4:6], 64) // HeaderLength
	binary.LittleEndian.PutUint16(hdr[6:8], 1)  // CreditCharge
	// hdr[8:12] = Status = SUCCESS (0)
	// hdr[12:14] = Command = NEGOTIATE (0)
	binary.LittleEndian.PutUint16(hdr[14:16], 1) // CreditsGranted
	// hdr[16:20] = Flags
	// hdr[20:24] = NextCommand
	binary.LittleEndian.PutUint64(hdr[24:32], 1) // MessageId
	// hdr[32:36] = Reserved
	// hdr[36:40] = TreeId
	// hdr[40:48] = SessionId
	// hdr[48:64] = Signature (zeros)

	payload := append(hdr, body...)

	// NetBIOS session message header: big-endian length of the SMB2
	// payload that follows. / NetBIOS session message 头：大端长度，
	// 表其后 SMB2 payload 字节数。
	out := make([]byte, 4+len(payload))
	binary.BigEndian.PutUint32(out[0:4], uint32(len(payload)))
	copy(out[4:], payload)
	return out
}

// buildSMB1NegotiateResponse builds a minimal SMB1
// NEGOTIATE_PROTOCOL_RESPONSE containing the \xFFSMB magic at offset 4
// relative to the read start. The plugin's loop checks `resp[i] == 0xff`
// and `tail == "SMB"` — we satisfy both. /
// buildSMB1NegotiateResponse 构造一个最小 SMB1
// NEGOTIATE_PROTOCOL_RESPONSE，在相对读起始的 offset 4 处放 \xFFSMB
// magic。plugin 的 loop 检查 `resp[i] == 0xff` 且 `tail == "SMB"`，我
// 们两条件都满足。
func buildSMB1NegotiateResponse() []byte {
	out := make([]byte, 64)
	binary.BigEndian.PutUint32(out[0:4], uint32(len(out)-4)) // NetBIOS length
	copy(out[4:8], []byte{0xFF, 'S', 'M', 'B'})              // SMB1 magic
	// Rest of an SMB1 negotiate response would include word count,
	// dialect index, security flags, capabilities, system time, etc.
	// The plugin doesn't parse it, so zeros suffice.
	return out
}

// TestSmb_IdentifyHit covers the SMB2/v3 happy path: handler reads
// the 36-byte SMB2 negotiate request, replies with a
// NEGOTIATE_PROTOCOL_RESPONSE carrying the \xFESMB magic at offset 4,
// and the plugin must classify it as Service="smb" / Banner="SMBv2/v3".
//
// / TestSmb_IdentifyHit 覆盖 SMB2/v3 happy path：handler 读 36 字节
// SMB2 negotiate 请求，返 \xFESMB magic 于 offset 4 的
// NEGOTIATE_PROTOCOL_RESPONSE；plugin 必须分类为 Service="smb" /
// Banner="SMBv2/v3"。
func TestSmb_IdentifyHit(t *testing.T) {
	host, port := fakeserver.ListenLoop(t, func(c net.Conn) {
		// Read the 36-byte SMB2 negotiate request the plugin sends
		// before it starts parsing the response. We don't validate
		// the bytes — just drain them — so the state machine advances
		// through the full request-then-response dance. /
		// 读 plugin 在开始解析响应前发的 36 字节 SMB2 negotiate 请
		// 求。我们不校验字节，只排空，让状态机走完 request →
		// response 全程。
		buf := make([]byte, 36)
		_, _ = c.Read(buf)

		_, _ = c.Write(buildSMB2NegotiateResponse())
	})

	p := New()
	res := p.Identify(context.Background(), host, port)
	if res == nil {
		t.Fatalf("Identify returned nil; expected SMBv2/v3 hit")
	}
	if res.Service != "smb" {
		t.Errorf("Service = %q, want %q", res.Service, "smb")
	}
	if res.Banner != "SMBv2/v3" {
		t.Errorf("Banner = %q, want %q", res.Banner, "SMBv2/v3")
	}
	if res.Host != host || res.Port != port {
		t.Errorf("Host/Port = %q/%d, want %q/%d", res.Host, res.Port, host, port)
	}
}

// TestSmb_IdentifySMB1Hit covers the SMB1 branch of the magic scan:
// the loop also matches \xFFSMB and returns Banner="SMBv1".
// / TestSmb_IdentifySMB1Hit 覆盖 magic scan 的 SMB1 分支：loop 同
// 时匹配 \xFFSMB 并返 Banner="SMBv1"。
func TestSmb_IdentifySMB1Hit(t *testing.T) {
	host, port := fakeserver.ListenLoop(t, func(c net.Conn) {
		buf := make([]byte, 36)
		_, _ = c.Read(buf)
		_, _ = c.Write(buildSMB1NegotiateResponse())
	})

	p := New()
	res := p.Identify(context.Background(), host, port)
	if res == nil {
		t.Fatalf("Identify returned nil; expected SMBv1 hit")
	}
	if res.Service != "smb" {
		t.Errorf("Service = %q, want %q", res.Service, "smb")
	}
	if res.Banner != "SMBv1" {
		t.Errorf("Banner = %q, want %q", res.Banner, "SMBv1")
	}
}

// TestSmb_IdentifyMiss covers the loop-exhaustion branch: server
// sends a long enough payload that n>=8 holds, but no SMB magic is
// present. Plugin must return nil. /
// TestSmb_IdentifyMiss 覆盖 loop 耗尽分支：server 发 n>=8 但不含
// SMB magic 的 payload；plugin 必须返 nil。
func TestSmb_IdentifyMiss(t *testing.T) {
	host, port := fakeserver.ListenLoop(t, func(c net.Conn) {
		buf := make([]byte, 36)
		_, _ = c.Read(buf)
		// 16 bytes of non-SMB data — enough to pass the n>=8 check
		// but won't match the magic scan. / 16 字节非 SMB 数据——
		// 过 n>=8 检查，但不匹配 magic scan。
		_, _ = c.Write([]byte("NOTANSMBMAGIC!!"))
	})

	if res := New().Identify(context.Background(), host, port); res != nil {
		t.Errorf("Identify = %+v, want nil for non-SMB payload", res)
	}
}

// TestSmb_ShortResponse covers the `n < 8` early-return branch:
// server sends fewer than 8 bytes, plugin must return nil. /
// TestSmb_ShortResponse 覆盖 `n < 8` 早返分支：server 发少于 8 字
// 节，plugin 必须返 nil。
func TestSmb_ShortResponse(t *testing.T) {
	host, port := fakeserver.ListenLoop(t, func(c net.Conn) {
		buf := make([]byte, 36)
		_, _ = c.Read(buf)
		_, _ = c.Write([]byte{0x01, 0x02, 0x03}) // 3 bytes < 8
	})

	if res := New().Identify(context.Background(), host, port); res != nil {
		t.Errorf("Identify = %+v, want nil for short response", res)
	}
}

// TestSmb_CredentialNoOp documents that the plugin's Credential is
// a documented no-op stub (see smb.go L44-46) — SMB credential
// testing lives in internal/core/credential/auth/remote/smb.go. Per
// the v0.6.0 fake-server plan we still wire a placeholder so the
// handler protocol bytes (SessionSetupResponse) are covered if a
// future contributor promotes the stub to a real implementation. /
// TestSmb_CredentialNoOp 标注 plugin 的 Credential 是文档化的 no-op
// stub（见 smb.go L44-46）；SMB 凭据测试在
// internal/core/credential/auth/remote/smb.go。按 v0.6.0 fake-server
// 计划仍保留占位，方便未来贡献者将 stub 升级成真实实现时补齐
// SessionSetupResponse 的 handler 字节。
func TestSmb_CredentialNoOp(t *testing.T) {
	t.Skip("Credential is a documented no-op stub in smb.go (L44-46); " +
		"SMB credential testing lives in internal/core/credential/auth/remote.")
}

// TestSmb_DialFailure exercises the dial-error branch in Identify:
// an unroutable address must yield nil without blocking past the
// caller's context deadline. / TestSmb_DialFailure 跑 Identify 的
// 拨号失败分支：不可达地址必须在 caller 的 context deadline 内返
// nil。
func TestSmb_DialFailure(t *testing.T) {
	// 192.0.2.1 is RFC 5737 TEST-NET-1 — guaranteed unroutable. /
	// 192.0.2.1 是 RFC 5737 TEST-NET-1，保证不可达。
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	if res := New().Identify(ctx, "192.0.2.1", 445); res != nil {
		t.Errorf("Identify(unroutable) = %+v, want nil", res)
	}
}
