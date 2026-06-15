// wire_tpkt.go — TPKT layer (RFC 1006). The smallest protocol
// unit in RDP: a 4-byte header (1 byte version, 1 reserved, 2
// big-endian length) followed by the inner PDU. Every other
// layer in this package builds on TPKTFrame / TPKTRead.
//
// Split out of wire.go as part of the v0.2.1 god-file refactor
// (per-protocol-layer files: tpkt → x224 → mcs → gcc →
// servercore). The RDP plugin only needs to construct an MCS
// Connect-Initial and parse a ServerCore, but each layer's
// framing/encode/decode lives in its own file for readability.
//
// wire_tpkt.go — TPKT 层（RFC 1006）。RDP 中最小的协议单元：4
// 字节头（1 字节版本，1 保留，2 字节大端长度）+ 内部 PDU。包
// 内其他层都基于 TPKTFrame / TPKTRead 构造。
//
// 拆自 wire.go，作为 v0.2.1 god-file 重构的一部分（按协议层拆
// 分文件：tpkt → x224 → mcs → gcc → servercore）。RDP 插件只
// 需构造 MCS Connect-Initial + 解析 ServerCore，但每层的
// framing/encode/decode 仍各放一份以保可读。
package rdp

import (
	"fmt"
	"io"
)

// TPKTFrame prepends a 4-byte TPKT header to `payload`.
//
// TPKT frame layout (RFC 1006):
//
//	+---+---+----------+----------+
//	| 03| 00| len (BE) | payload |
//	+---+---+----------+----------+
//
// Version is hard-coded to 3 (the only TPKT version). Length
// is the full TPDU size (header + payload), big-endian.
//
// TPKTFrame 在 `payload` 前加 4 字节 TPKT 头。
//
// TPKT frame 布局（RFC 1006）：
//
//	+---+---+----------+----------+
//	| 03| 00| len (BE) | payload |
//	+---+---+----------+----------+
//
// 版本硬编码 3（唯一的 TPKT 版本）。长度是完整 TPDU 大小（头 +
// payload），大端。
func TPKTFrame(payload []byte) []byte {
	length := uint16(len(payload) + 4)
	out := make([]byte, 4+len(payload))
	out[0] = 0x03
	out[1] = 0x00
	out[2] = byte(length >> 8)
	out[3] = byte(length)
	copy(out[4:], payload)
	return out
}

// TPKTRead reads one TPKT-framed PDU from r and returns the
// payload (everything after the 4-byte TPKT header).
//
// Errors:
//   - "short header" if r returns EOF before 4 bytes
//   - "bad version" if the first byte isn't 0x03
//   - "short payload" if the declared length exceeds what's
//     available on r
//
// TPKTRead 从 r 读一个 TPKT 框架的 PDU，返 payload（TPKT 4 字节
// 头后的所有内容）。
//
// 错误：
//   - "short header" — r 在 4 字节前 EOF
//   - "bad version" — 首字节非 0x03
//   - "short payload" — 声明长度超出 r 可供
func TPKTRead(r io.Reader) ([]byte, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, fmt.Errorf("rdp: TPKT short header: %w", err)
	}
	if hdr[0] != 0x03 {
		return nil, fmt.Errorf("rdp: TPKT bad version 0x%02x (want 0x03)", hdr[0])
	}
	length := int(hdr[2])<<8 | int(hdr[3])
	if length < 4 {
		return nil, fmt.Errorf("rdp: TPKT length %d < 4", length)
	}
	payload := make([]byte, length-4)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, fmt.Errorf("rdp: TPKT short payload: %w", err)
	}
	return payload, nil
}
