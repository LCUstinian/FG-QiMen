// rdpnla.go — RDP Network Level Authentication (NLA / CredSSP)
// probe plugin. Phase 1.8 of the audit roadmap (the RDP NLA
// cred test that v0.3 README explicitly deferred to v0.3+).
//
// rdpnla.go — RDP 网络级认证（NLA / CredSSP）探测插件。审计路
// 线图 Phase 1.8（v0.3 README 明确 defer 到 v0.3+ 的 RDP NLA 凭
// 据测试）。
//
// What this plugin does:
//  1. Sends an X.224 CR with PROTOCOL_HYBRID (or PROTOCOL_HYBRID_EX).
//  2. Reads the server's CC and checks whether the server selected
//     HYBRID or HYBRID_EX — this is the basic "NLA enabled" signal.
//  3. Sends a minimal NTLM NEGOTIATE message in plaintext (no
//     TLS upgrade) and reads the response. Servers that
//     require TLS for NLA will close the connection; servers
//     that allow non-TLS NLA will respond with NTLM CHALLENGE.
//  4. Reports the server's NLA posture (enabled / required /
//     disabled).
//
// What this plugin does NOT do:
//   - Full CredSSP / TLS / NTLM authentication handshake (we
//     stop at the Challenge or at disconnect). Phase 1.8
//     reports the *presence* and *posture* of NLA — actual
//     credential spray still uses the per-protocol authenticator
//     in core/credential/auth/remote/ which can be extended
//     in v0.4+ with a full NTLM implementation.
//   - Post-authentication actions (HARD rule).
//
// HARD compliance: this plugin never executes a full NTLM
// authentication. It only sends a NEGOTIATE message and reads
// the response. No LMv2 / NTLMv2 hash is ever sent to the
// server. / HARD 合规：本插件从不执行完整 NTLM 认证，只发
// NEGOTIATE 消息并读响应。永不发 LMv2 / NTLMv2 hash 给 server。
package rdpnla

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/plugins"
	"github.com/LCUstinian/FG-QiMen/internal/types"
)

// Plugin identifies RDP NLA posture. / Plugin 识别 RDP NLA 状态。
type Plugin struct{}

// New returns a new rdpnla plugin. / New 返回一个新的 rdpnla 插件。
func New() *Plugin { return &Plugin{} }

func init() { plugins.Register(New()) }

// Name implements plugins.Plugin. / Name 实现 plugins.Plugin。
func (p *Plugin) Name() string { return "rdp-nla" }

// Ports returns the RDP port. / Ports 返回 RDP 端口。
func (p *Plugin) Ports() []int { return []int{3389} }

// Modes returns Identify only. / Modes 仅返回 Identify。
func (p *Plugin) Modes() plugins.Mode { return plugins.ModeIdentify }

// Credential is a no-op stub. Real NLA cred testing lives in
// core/credential/auth/remote (v0.4+). / Credential 空桩。NLA
// 真实凭据测试在 core/credential/auth/remote（v0.4+）。
func (p *Plugin) Credential(ctx context.Context, host string, port int, creds []types.Cred) *types.Result {
	return nil
}

// Identify probes NLA posture via a minimal X.224 + NTLM NEGOTIATE
// sequence. / Identify 通过最小 X.224 + NTLM NEGOTIATE 序列探测
// NLA 状态。
func (p *Plugin) Identify(ctx context.Context, host string, port int) *types.Result {
	return plugins.RawTCPIdentify(ctx, host, port, func(conn net.Conn) *types.Result {
		_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
		// Step 1: X.224 Connection Request with PROTOCOL_HYBRID.
		// / Step 1：X.224 CR 用 PROTOCOL_HYBRID。
		cr := buildX224CR("fgqimen")
		if _, err := conn.Write(cr); err != nil {
			return nil
		}
		// Step 2: read Connection Confirm. Format: 1B length,
		// 1B CR (0x0D = CC), 1B flags + cookie, then protocol.
		// / Step 2：读 CC。格式：1B 长度，1B CR (0x0D=CC)，
		// 1B flags + cookie，再协议。
		hdr := make([]byte, 4)
		if _, err := readFull(conn, hdr); err != nil {
			return nil
		}
		if hdr[0] < 11 || hdr[1] != 0xD0 {
			// Not a CC. / 不是 CC。
			return nil
		}
		// 11 bytes: length(1) + CC(1) + dst-ref(2) + src-ref(2) +
		// class(1) + cookie(1) + protocol(2) + (varies). The
		// actual length is hdr[0]+1+1+1 (TPKT). Read the rest.
		// / 11 字节。实际长度是 hdr[0]+1+1+1（TPKT）。读剩下的。
		rest := make([]byte, hdr[0]-3)
		if _, err := readFull(conn, rest); err != nil {
			return nil
		}
		// selectedProtocol is at offset 9 of CC (after
		// dst-ref(2) + src-ref(2) + class(1) + cookie(1) +
		// length+type=2). Actually the structure is:
		//   hdr[0]=len, hdr[1]=0x0D
		//   rest[0..2]=dst-ref, rest[2..4]=src-ref,
		//   rest[4]=class, rest[5]=cookie
		//   rest[6..8]=selectedProtocol
		//   rest[8..]=cookie variable
		// / selectedProtocol 在 CC offset 9 附近。
		if len(rest) < 8 {
			return nil
		}
		selected := binary.LittleEndian.Uint16(rest[6:8])
		const (
			protocolRDP    = 0x00000000
			protocolSSL    = 0x00000001
			protocolHYBRID = 0x00000002
		)
		var nlaState string
		switch selected {
		case protocolHYBRID:
			nlaState = "HYBRID (NLA via CredSSP)"
		case protocolSSL:
			nlaState = "SSL (TLS upgrade required — likely NLA)"
		case protocolRDP:
			nlaState = "RDP (legacy, no NLA)"
		default:
			nlaState = fmt.Sprintf("protocol=0x%04x", selected)
		}
		// Step 3: send minimal NTLM NEGOTIATE. Most NLA-required
		// servers will reject this (they want TLS first). Servers
		// that allow NLA without TLS (rare) will respond with
		// CHALLENGE. We just record which it was.
		// / Step 3：发最小 NTLM NEGOTIATE。多数要求 NLA 的
		// server 会拒（它们要先 TLS）。允许无 TLS NLA 的 server
		// （少见）会回 CHALLENGE。我们只记录。
		//
		// NTLMSSP NEGOTIATE: signature "NTLMSSP\x00", type 1,
		// flags 0x00088207 (negotiate Unicode / NTLM / OEM /
		// always-sign / NTLM2), workstation + domain empty.
		// / NTLMSSP NEGOTIATE：签名 + type 1 + flags + 0 长
		// workstation/domain。
		ntlm := buildNTLMNegotiate()
		if _, err := conn.Write(ntlm); err != nil {
			return &types.Result{
				Host: host, Port: port, Service: "rdp-nla",
				Banner: "RDP NLA: " + nlaState + " (write failed)",
				Time:   time.Now(),
			}
		}
		// Read response (or wait for disconnect). / 读响应（或等断）。
		resp := make([]byte, 64)
		_ = conn.SetReadDeadline(time.Now().Add(1 * time.Second))
		n, _ := conn.Read(resp)
		var ntlmsspResult string
		if n == 0 {
			ntlmsspResult = "disconnect (TLS required or refused)"
		} else if n >= 8 && string(resp[:7]) == "NTLMSSP" {
			// NTLMSSP signature — server replied with Challenge.
			// / NTLMSSP 签名——server 回了 Challenge。
			ntlmsspResult = fmt.Sprintf("NEGOTIATE→CHALLENGE (%d bytes)", n)
		} else {
			ntlmsspResult = fmt.Sprintf("unrecognized (%d bytes)", n)
		}
		return &types.Result{
			Host: host, Port: port, Service: "rdp-nla",
			Banner: fmt.Sprintf("RDP NLA: %s; NTLM probe: %s", nlaState, ntlmsspResult),
			Time:   time.Now(),
		}
	})
}

// buildX224CR constructs a minimal TPKT + X.224 CR with
// PROTOCOL_HYBRID. / buildX224CR 构造最小 TPKT + X.224 CR
// 用 PROTOCOL_HYBRID。
func buildX224CR(cookie string) []byte {
	// X.224 CR payload (after TPKT):
	//   1B length (of remaining)
	//   1B CR code (0x0E)
	//   2B dst-ref (0)
	//   2B src-ref (0)
	//   1B class (0)
	//   variable: routing cookie (we use ASCII "fgqimen" + 0x00)
	// / X.224 CR payload（TPKT 后）：1B 长度，1B CR code (0x0E)，
	// 2B dst-ref，2B src-ref，1B class，0+cookie。
	cookieBytes := append([]byte(cookie), 0x00)
	// requestedProtocols (4 bytes, little-endian): PROTOCOL_HYBRID
	// (0x02) selected. / requestedProtocols (4 字节 LE)：选
	// PROTOCOL_HYBRID (0x02)。
	reqProto := []byte{0x02, 0x00, 0x00, 0x00}
	// X.224 CR length (excluding the length byte itself, per
	// ITU-T X.224 §13.3). / X.224 CR 长度（不含 length 字节）。
	x224Len := 1 + 2 + 2 + 1 + len(cookieBytes) + 4
	x224 := []byte{byte(x224Len), 0x0E, 0x00, 0x00, 0x00, 0x00, 0x00}
	x224 = append(x224, cookieBytes...)
	x224 = append(x224, reqProto...)
	// TPKT header: 1B version (3), 1B reserved (0), 2B length
	// (TPKT length including itself = 4 + X.224 len). / TPKT
	// 头：1B version (3)，1B 保留 (0)，2B 长度。
	tpktLen := 4 + len(x224)
	tpkt := []byte{0x03, 0x00, byte(tpktLen >> 8), byte(tpktLen)}
	return append(tpkt, x224...)
}

// buildNTLMNegotiate constructs a minimal NTLMSSP NEGOTIATE
// message. We DO NOT include any actual credentials. The point
// is only to elicit a CHALLENGE response from servers that
// allow non-TLS NLA. / buildNTLMNegotiate 构造最小 NTLMSSP
// NEGOTIATE 消息。我们**不**包含任何真实凭据。目的只是从允许
// 无 TLS NLA 的 server 触发出 CHALLENGE 响应。
func buildNTLMNegotiate() []byte {
	// NTLMSSP NEGOTIATE structure (per MS-NLMP):
	//   8B signature "NTLMSSP\x00"
	//   4B MessageType = 1 (LE)
	//   4B NegotiateFlags
	//   8B DomainNameFields (length + maxlength + offset)
	//   8B WorkstationFields (length + maxlength + offset)
	//   (optional) data: domain + workstation
	// / NTLMSSP NEGOTIATE 结构（MS-NLMP）：8B 签名，4B type=1，
	// 4B flags，8B domain fields，8B workstation fields，可选 data。
	flags := uint32(0x00088207) // NTLM2 / Unicode / NTLM / OEM / always-sign
	msg := make([]byte, 0, 32)
	// Signature
	msg = append(msg, []byte("NTLMSSP\x00")...)
	// Type
	msg = binary.LittleEndian.AppendUint32(msg, 1)
	// Flags
	msg = binary.LittleEndian.AppendUint32(msg, flags)
	// Domain length / maxlength / offset (all zero — we don't send a domain)
	msg = binary.LittleEndian.AppendUint16(msg, 0)
	msg = binary.LittleEndian.AppendUint16(msg, 0)
	msg = binary.LittleEndian.AppendUint32(msg, 32) // offset = end of header
	// Workstation length / maxlength / offset (all zero)
	msg = binary.LittleEndian.AppendUint16(msg, 0)
	msg = binary.LittleEndian.AppendUint16(msg, 0)
	msg = binary.LittleEndian.AppendUint32(msg, 32)
	return msg
}

// readFull reads exactly len(buf) bytes from c. / readFull 从 c
// 读正好 len(buf) 字节。
func readFull(c net.Conn, buf []byte) (int, error) {
	total := 0
	for total < len(buf) {
		n, err := c.Read(buf[total:])
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}
