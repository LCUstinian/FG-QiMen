// rdpnla_test.go — unit tests for the NTLM NEGOTIATE byte
// structure and X.224 CR framing + fake-server integration tests
// for the rdpnla Identify plugin.
//
// / rdpnla_test.go — NTLM NEGOTIATE 字节结构和 X.224 CR framing
// 的单元测试 + rdpnla Identify 插件的假服务器集成测试。
package rdpnla

import (
	"bytes"
	"context"
	"encoding/binary"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/fakeserver"
)

// ─── fake-server integration tests (v0.6.0 plan Tier 3) ──────────
//
// The rdpnla Identify is a stateful TCP plugin: the client
// speaks first (X.224 CR), reads an X.224 CC, sends an NTLMSSP
// NEGOTIATE, and reads the response. The CC the plugin expects
// has NO TPKT framing — the plugin reads raw X.224 bytes off the
// TCP stream. The server handler therefore:
//   1. Drains the TPKT-framed X.224 CR.
//   2. Replies with an UNWRAPPED X.224 CC (hdr[0]=11, hdr[1]=0xD0,
//      selectedProtocol at rest[6:8] little-endian uint16).
//   3. Drains the 32-byte NTLMSSP NEGOTIATE.
//   4. Either replies with a synthetic NTLMSSP CHALLENGE or
//      closes the connection (most NLA servers will).
//
// / 假服务器集成测试（v0.6.0 计划 Tier 3）。rdpnla Identify 是一
// 个有状态 TCP 插件：客户端先说话（X.224 CR），读 X.224 CC，发
// NTLMSSP NEGOTIATE，读响应。插件期待的 CC **不**带 TPKT 包裹
// ——直接从 TCP 流读裸 X.224 字节。服务端 handler 因此：1. 清
// 空 TPKT 包裹的 X.224 CR。2. 回一个不带 TPKT 的 X.224 CC
// （hdr[0]=11，hdr[1]=0xD0，selectedProtocol 在 rest[6:8] LE
// uint16）。3. 清空 32 字节 NTLMSSP NEGOTIATE。4. 回一个合成的
// NTLMSSP CHALLENGE 或关连接（多数 NLA server 会关）。
//
// Plan §11.2: NLA = CredSSP over TLS. The full TLS upgrade + NTLM
// AUTHENTICATE round-trip is out of scope for a happy-path fake
// (we'd need a TLS cert + NTLM hash). We cover Identify-only here;
// CredentialHit is a documented no-op stub (rdpnla.go L64-69) and
// is t.Skip()'d below. Coverage is accepted at whatever happy-path
// Identify + unit tests achieve.
//
// / 计划 §11.2：NLA = CredSSP over TLS。完整 TLS 升级 + NTLM
// AUTHENTICATE 往返超出 happy-path 假服务器范围（需要 TLS 证
// 书 + NTLM hash）。这里只覆盖 Identify；CredentialHit 是文档化
// 的 no-op stub（rdpnla.go L64-69），下面 t.Skip()。覆盖率接
// 受 happy-path Identify + 单元测试能达到的水平。

// drainTPKT consumes one TPKT-framed message from c. / drainTPKT
// 从 c 消耗一个 TPKT 包裹的消息。
func drainTPKT(c net.Conn) error {
	hdr := make([]byte, 4)
	if _, err := readFull(c, hdr); err != nil {
		return err
	}
	length := int(binary.BigEndian.Uint16(hdr[2:4]))
	if length < 4 {
		return nil
	}
	body := make([]byte, length-4)
	_, _ = readFull(c, body)
	return nil
}

// buildX224CC constructs the UNWRAPPED X.224 CC the plugin's
// Identify expects. The plugin reads 4 bytes (hdr) then hdr[0]-3
// more bytes (rest), and reads selectedProtocol from rest[6:8] as
// little-endian uint16.
//
// / buildX224CC 构造插件 Identify 期望的不带 TPKT 的 X.224 CC。
// 插件读 4 字节（hdr）再 hdr[0]-3 字节（rest），从 rest[6:8]
// 读 selectedProtocol LE uint16。
func buildX224CC(selectedProto uint16) []byte {
	var protoBuf [2]byte
	binary.LittleEndian.PutUint16(protoBuf[:], selectedProto)
	// Layout (12 bytes total, hdr[0]=11):
	//   hdr[0]=11 (length), hdr[1]=0xD0 (CC)
	//   hdr[2:4]=dst-ref
	//   rest[0:2]=src-ref, rest[2]=class, rest[3]=cookie
	//   rest[4:6]=padding, rest[6:8]=selectedProtocol
	// / 布局（12 字节总，hdr[0]=11）。
	return []byte{
		11, 0xD0,
		0x00, 0x00, // dst-ref
		0x00, 0x00, // src-ref
		0x00,       // class
		0x00,       // cookie
		0x00, 0x00, // padding
		protoBuf[0], protoBuf[1], // selectedProtocol (LE uint16)
	}
}

// buildNTLMSSPChallenge constructs a minimal NTLMSSP CHALLENGE
// (just the signature + type=2). Plugin only checks the 8-byte
// "NTLMSSP\x00" signature to decide NEGOTIATE→CHALLENGE vs
// disconnect. / buildNTLMSSPChallenge 构造最小 NTLMSSP CHALLENGE
// （仅签名 + type=2）。插件只检查 8 字节 "NTLMSSP\x00" 签名来
// 判定 NEGOTIATE→CHALLENGE 还是断连。
func buildNTLMSSPChallenge() []byte {
	msg := []byte("NTLMSSP\x00")
	return binary.LittleEndian.AppendUint32(msg, 2)
}

// fakeRDPServer drives one rdpnla probe:
//  1. drains the X.224 CR (TPKT-wrapped).
//  2. replies with X.224 CC selecting selectedProto.
//  3. drains the 32-byte NTLMSSP NEGOTIATE.
//  4. if replyChallenge, sends a synthetic NTLMSSP CHALLENGE;
//     else closes the connection (server wants TLS first).
//
// / fakeRDPServer 跑一次 rdpnla 探测：1. 清空 X.224 CR。2. 回
// X.224 CC 选 selectedProto。3. 清空 32 字节 NTLMSSP NEGOTIATE。
// 4. 若 replyChallenge 发合成 NTLMSSP CHALLENGE；否则关连接
// （server 要先 TLS）。
func fakeRDPServer(t *testing.T, selectedProto uint16, replyChallenge bool) (host string, port int) {
	t.Helper()
	return fakeserver.ListenLoop(t, func(c net.Conn) {
		_ = c.SetDeadline(time.Now().Add(3 * time.Second))
		// 1. Drain the TPKT-framed X.224 CR. / 清空 TPKT 包裹的
		// X.224 CR。
		_ = drainTPKT(c)
		// 2. Reply with X.224 CC. / 回 X.224 CC。
		if _, err := c.Write(buildX224CC(selectedProto)); err != nil {
			return
		}
		// 3. Drain NTLMSSP NEGOTIATE (32 bytes). / 清空 NTLMSSP
		// NEGOTIATE（32 字节）。
		ntlm := make([]byte, 32)
		_, _ = readFull(c, ntlm)
		// 4. Either reply with CHALLENGE or close. / 回 CHALLENGE
		// 或关连接。
		if replyChallenge {
			_, _ = c.Write(buildNTLMSSPChallenge())
			// Hold the conn open briefly so the plugin's Read
			// has time to return before the test tears down.
			// / 短时保持 conn 打开，让插件的 Read 有时间返回。
			time.Sleep(100 * time.Millisecond)
		} else {
			_ = c.Close()
		}
	})
}

// TestRdpnla_IdentifyHit covers the happy path: server selects
// PROTOCOL_HYBRID and replies with a CHALLENGE. Identify must
// return non-nil with Service="rdp-nla" and Banner containing
// "HYBRID" + "CHALLENGE".
//
// / TestRdpnla_IdentifyHit 覆盖 happy path：server 选 PROTOCOL_HYBRID
// 并回 CHALLENGE。Identify 必须返非 nil，Service="rdp-nla"，
// Banner 含 "HYBRID" + "CHALLENGE"。
func TestRdpnla_IdentifyHit(t *testing.T) {
	host, port := fakeRDPServer(t, 0x0002 /* PROTOCOL_HYBRID */, true /* CHALLENGE */)

	p := New()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	res := p.Identify(ctx, host, port)
	if res == nil {
		t.Fatalf("Identify returned nil; expected rdp-nla hit")
	}
	if res.Service != "rdp-nla" {
		t.Errorf("Service = %q, want rdp-nla", res.Service)
	}
	if res.Host != host || res.Port != port {
		t.Errorf("Host/Port = %q/%d, want %q/%d", res.Host, res.Port, host, port)
	}
	if !strings.Contains(res.Banner, "HYBRID") {
		t.Errorf("Banner = %q, want contains 'HYBRID'", res.Banner)
	}
	if !strings.Contains(res.Banner, "CHALLENGE") {
		t.Errorf("Banner = %q, want contains 'CHALLENGE'", res.Banner)
	}
}

// TestRdpnla_IdentifyDisconnected covers the more common real-world
// outcome: server selects PROTOCOL_HYBRID but closes the connection
// after the NTLMSSP NEGOTIATE because it requires TLS upgrade first.
// Identify must still return a non-nil Result (it's a positive
// NLA-posture signal) with Banner containing "HYBRID" + "disconnect".
//
// / TestRdpnla_IdentifyDisconnected 覆盖更常见的真实世界结果：
// server 选 PROTOCOL_HYBRID 但在 NTLMSSP NEGOTIATE 后关连接（要
// 求先 TLS 升级）。Identify 必须仍返非 nil Result（这是一个正
// 面的 NLA 状态信号），Banner 含 "HYBRID" + "disconnect"。
func TestRdpnla_IdentifyDisconnected(t *testing.T) {
	host, port := fakeRDPServer(t, 0x0002 /* PROTOCOL_HYBRID */, false /* disconnect */)

	p := New()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	res := p.Identify(ctx, host, port)
	if res == nil {
		t.Fatalf("Identify returned nil; expected hit on NLA-required server (disconnect is still a posture signal)")
	}
	if res.Service != "rdp-nla" {
		t.Errorf("Service = %q, want rdp-nla", res.Service)
	}
	if !strings.Contains(res.Banner, "HYBRID") {
		t.Errorf("Banner = %q, want contains 'HYBRID'", res.Banner)
	}
	if !strings.Contains(res.Banner, "disconnect") {
		t.Errorf("Banner = %q, want contains 'disconnect'", res.Banner)
	}
}

// TestRdpnla_IdentifySSL covers the PROTOCOL_SSL branch in the
// switch — server selects SSL (TLS upgrade required, likely NLA).
// The plugin should still return a hit with Banner mentioning
// "SSL". / TestRdpnla_IdentifySSL 覆盖 switch 的 PROTOCOL_SSL
// 分支——server 选 SSL（要求 TLS 升级，可能 NLA）。插件应仍返
// hit，Banner 提到 "SSL"。
func TestRdpnla_IdentifySSL(t *testing.T) {
	host, port := fakeRDPServer(t, 0x0001 /* PROTOCOL_SSL */, false /* disconnect */)

	p := New()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	res := p.Identify(ctx, host, port)
	if res == nil {
		t.Fatalf("Identify returned nil; expected hit on SSL-selected NLA")
	}
	if !strings.Contains(res.Banner, "SSL") {
		t.Errorf("Banner = %q, want contains 'SSL'", res.Banner)
	}
}

// TestRdpnla_CredentialNoOp documents that rdpnla.Credential is a
// documented no-op stub (see rdpnla.go L64-69). Real NLA credential
// testing lives in core/credential/auth/remote (v0.4+). Per the
// v0.6.0 fake-server plan §11.2 we accept Identify-only coverage
// for this complex CredSSP-over-TLS plugin.
//
// / TestRdpnla_CredentialNoOp 标注 rdpnla.Credential 是文档化的
// no-op stub（见 rdpnla.go L64-69）。真实 NLA 凭据测试在
// core/credential/auth/remote（v0.4+）。按 v0.6.0 假服务器计划
// §11.2，对这个复杂 CredSSP-over-TLS 插件我们接受只覆盖
// Identify。
func TestRdpnla_CredentialNoOp(t *testing.T) {
	t.Skip("rdpnla.Credential is a documented no-op stub (rdpnla.go L64-69); " +
		"real NLA cred testing lives in core/credential/auth/remote (v0.4+). " +
		"Plan §11.2 accepts Identify-only coverage for complex CredSSP/TLS protocols.")
}

func TestBuildX224CR_Structure(t *testing.T) {
	cr := buildX224CR("test")
	// TPKT header: version=3, reserved=0, length=4+X.224len. / TPKT 头。
	if cr[0] != 0x03 || cr[1] != 0x00 {
		t.Errorf("TPKT version/reserved = %02x %02x, want 03 00", cr[0], cr[1])
	}
	tpktLen := binary.BigEndian.Uint16(cr[2:4])
	if int(tpktLen) != len(cr) {
		t.Errorf("TPKT length %d, actual %d", tpktLen, len(cr))
	}
	// X.224 CR: length byte, code=0x0E. / X.224 CR：长度字节，
	// code=0x0E。
	if cr[4] == 0 {
		t.Error("X.224 length byte is 0")
	}
	if cr[5] != 0x0E {
		t.Errorf("X.224 CR code = 0x%02x, want 0x0E", cr[5])
	}
	// Cookie is "test\x00" (5 bytes) starting at offset 11
	// (after TPKT(4) + length(1) + CR(1) + dst-ref(2) + src-ref(2) +
	// class(1)).
	// / Cookie 是 "test\x00"（5 字节）从 offset 11 开始。
	if !bytes.Equal(cr[11:16], []byte("test\x00")) {
		t.Errorf("cookie = %q, want %q", cr[11:16], "test\x00")
	}
	// requestedProtocols (4 bytes LE) = 0x00000002 (HYBRID). /
	// requestedProtocols（4 字节 LE）= 0x00000002（HYBRID）。
	if !bytes.Equal(cr[16:20], []byte{0x02, 0x00, 0x00, 0x00}) {
		t.Errorf("requestedProtocols = %x, want 02000000", cr[16:20])
	}
}

func TestBuildNTLMNegotiate_Structure(t *testing.T) {
	ntlm := buildNTLMNegotiate()
	// Signature "NTLMSSP\x00" (8 bytes). / 签名。
	if !bytes.Equal(ntlm[:8], []byte("NTLMSSP\x00")) {
		t.Errorf("signature = %q, want NTLMSSP\\0", ntlm[:8])
	}
	// Type = 1 (LE). / Type = 1（LE）。
	if got := binary.LittleEndian.Uint32(ntlm[8:12]); got != 1 {
		t.Errorf("type = %d, want 1", got)
	}
	// Flags = 0x00088207. / Flags.
	if got := binary.LittleEndian.Uint32(ntlm[12:16]); got != 0x00088207 {
		t.Errorf("flags = 0x%08x, want 0x00088207", got)
	}
	// Total length = 32 (header only, no domain/workstation data). /
	// 总长 32（仅头，无 domain/workstation data）。
	if len(ntlm) != 32 {
		t.Errorf("ntlm len = %d, want 32", len(ntlm))
	}
}

func TestBuildNTLMNegotiate_NoCredentials(t *testing.T) {
	// HARD-rule regression guard: the NEGOTIATE message MUST NOT
	// contain any credential material. / HARD 规则回归防护：
	// NEGOTIATE 消息**绝不**包含任何凭据材料。
	ntlm := buildNTLMNegotiate()
	// No LMv2 / NTLMv2 hash (which would appear after the
	// 32-byte header). / 无 LMv2 / NTLMv2 hash（出现在 32
	// 字节头之后）。
	if len(ntlm) > 32 {
		t.Errorf("NEGOTIATE should be header-only; got %d bytes (credential data present?)", len(ntlm))
	}
}
