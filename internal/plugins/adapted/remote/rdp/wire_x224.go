// wire_x224.go — X.224 / ISO 8073 Connection-Oriented Transport.
// The CR (Connection Request) is what we send on the wire; the
// CC (Connection Confirm) is what the server replies with. The
// "requested protocol" field in the CR advertises RDP's PROTOCOL_S
// (0x00000000) plus PROTOCOL_HYBRID (0x00000001) and similar.
//
// Split out of wire.go as part of the v0.2.1 god-file refactor.
//
// wire_x224.go — X.224 / ISO 8073 面向连接的传输。我们在网上发
// CR（Connection Request）；服务器回 CC（Connection Confirm）。
// CR 的"requested protocol"字段声明 RDP 的 PROTOCOL_S
// （0x00000000）加 PROTOCOL_HYBRID（0x00000001）等。
//
// 拆自 wire.go，作为 v0.2.1 god-file 重构的一部分。
package rdp

import (
	"encoding/binary"
	"fmt"
	"io"
)

// X224Confirm is the parsed X.224 Connection Confirm.
//
// CC TPDU layout (after TPKT header):
//
//	1 byte: length (of remaining PDU; we ignore — trust TPKT)
//	1 byte: CC (0xD0)
//	2 bytes: DST-REF (LE) — connection reference, RDP ignores
//	2 bytes: SRC-REF (LE) — connection reference, RDP ignores
//	1 byte: Class Option (0 = accepted, etc.)
//	4 bytes: selectedProtocol (LE)
//
// X224Confirm 是解析后的 X.224 Connection Confirm。
//
// CC TPDU 布局（TPKT 头之后）：
//
//	1 字节：length（剩余 PDU 长度；忽略——信 TPKT 即可）
//	1 字节：CC（0xD0）
//	2 字节：DST-REF（LE）——连接引用，RDP 不用
//	2 字节：SRC-REF（LE）——连接引用，RDP 不用
//	1 字节：Class Option（0=接受 等）
//	4 字节：selectedProtocol（LE）
type X224Confirm struct {
	// SelectedProtocol is the 32-bit little-endian protocol
	// identifier the server picked from the requestedProtocols
	// field of our CR (one of ProtocolRDP / ProtocolSSL /
	// ProtocolHYBRID / ProtocolHYBRID_EX). rdp.go uses this to
	// decide whether to bail for TLS / NLA.
	//
	// / SelectedProtocol 是服务器从我们 CR 的 requestedProtocols
	// 字段里挑的 32 位小端协议标识（ProtocolRDP / ProtocolSSL /
	// ProtocolHYBRID / ProtocolHYBRID_EX 之一）。rdp.go 据此决定
	// 是否因 TLS / NLA 退出。
	SelectedProtocol uint32
}

// EncodeX224CR builds a TPKT-framed X.224 Connection Request.
//
// X.224 CR TPDU layout (after TPKT header):
//
//	1 byte: length (of remaining PDU)
//	1 byte: CR (0xE0)
//	2 bytes: DST-REF (0x0000)
//	2 bytes: SRC-REF (any nonzero, we use 0x1234)
//	1 byte: Class Option (0x00)
//	1 byte: cookie length
//	N bytes: cookie
//	1 byte: 0x0D
//	1 byte: 0x0A   ← RDP negotiation cookie terminator
//	4 bytes: requestedProtocols (LE)
//
// The cookie is typically "mstshash=<username>" or empty. The
// 0x0D0A terminator is required even when the cookie is empty.
//
// EncodeX224CR 构造一个 TPKT 包裹的 X.224 Connection Request。
//
// X.224 CR TPDU 布局（TPKT 头之后）：
//
//	1 字节：length（剩余 PDU 长度）
//	1 字节：CR（0xE0）
//	2 字节：DST-REF（0x0000）
//	2 字节：SRC-REF（任意非零，这里用 0x1234）
//	1 字节：Class Option（0x00）
//	1 字节：cookie 长度
//	N 字节：cookie
//	1 字节：0x0D
//	1 字节：0x0A   ← RDP 协商 cookie 结束符
//	4 字节：requestedProtocols（LE）
//
// cookie 通常是 "mstshash=<用户名>" 或空。0x0D0A 结束符是必需的，
// 即使 cookie 为空。
func EncodeX224CR(cookie string, requestedProtocols uint32) []byte {
	// Build the X.224 PDU body (after length byte). Note: SRC-REF
	// is non-zero (0x1234) to follow the original fscan grdp
	// behaviour; some servers reject all-zero SRC-REF.
	//
	// / 构造 X.224 PDU body（长度字节之后）。注：SRC-REF 非零
	//（0x1234），沿用原 fscan grdp 行为；某些服务器拒绝全零 SRC-REF。
	body := make([]byte, 0, 64)
	body = append(body, 0xE0)              // CR
	body = append(body, 0x00, 0x00)        // DST-REF
	body = append(body, 0x34, 0x12)        // SRC-REF (any nonzero)
	body = append(body, 0x00)              // Class Option
	body = append(body, byte(len(cookie))) // cookie length
	body = append(body, []byte(cookie)...)
	body = append(body, 0x0D, 0x0A) // terminator
	// requestedProtocols (4 bytes LE). / requestedProtocols（4 字节 LE）。
	var proto [4]byte
	binary.LittleEndian.PutUint32(proto[:], requestedProtocols)
	body = append(body, proto[:]...)
	// Prepend length byte. / 前置长度字节。
	pdu := make([]byte, 0, 1+len(body))
	pdu = append(pdu, byte(len(body)))
	pdu = append(pdu, body...)
	// Wrap in TPKT. / TPKT 包裹。
	return TPKTFrame(pdu)
}

// DecodeX224CC reads a TPKT-framed X.224 Connection Confirm.
//
// CC TPDU layout (after TPKT header):
//
//	+---+---+---+---+---+----+----+----+----+
//	| D0|len|cdt|dstRef    |srcRef    |cls|...|
//	+---+---+---+---+---+----+----+----+----+
//
// DecodeX224CC 读一个 TPKT 包裹的 X.224 Connection Confirm。
//
// CC TPDU 布局（TPKT 头之后）：
//
//	+---+---+---+---+---+----+----+----+----+
//	| D0|len|cdt|dstRef    |srcRef    |cls|...|
//	+---+---+---+---+---+----+----+----+----+
func DecodeX224CC(r io.Reader) (*X224Confirm, error) {
	pdu, err := TPKTRead(r)
	if err != nil {
		return nil, err
	}
	// pdu[0] is the length byte (we ignore it — we trust TPKT).
	// / pdu[0] 是长度字节（信 TPKT 即可，忽略）。
	if len(pdu) < 2 || pdu[1] != 0xD0 {
		return nil, fmt.Errorf("rdp: expected CC (0xD0), got 0x%02x (len %d)", pdu[1], len(pdu))
	}
	if len(pdu) < 11 {
		return nil, fmt.Errorf("rdp: CC too short for selectedProtocol (%d bytes)", len(pdu))
	}
	// pdu[2:4]   = DST-REF
	// pdu[4:6]   = SRC-REF
	// pdu[6]     = Class Option
	// pdu[7:11]  = selectedProtocol (4 bytes, LE)
	// / pdu[2:4] = DST-REF
	// / pdu[4:6] = SRC-REF
	// / pdu[6]   = Class Option
	// / pdu[7:11] = selectedProtocol（4 字节，LE）
	return &X224Confirm{
		SelectedProtocol: binary.LittleEndian.Uint32(pdu[7:11]),
	}, nil
}
