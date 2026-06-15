// wire.go — RDP wire protocol package entry point.
//
// The actual implementation is split by protocol layer (RFC
// 1006 / TPKT, X.224, MCS, GCC, serverCore). The protocol
// builders (TPKTFrame, TPKTRead) live in wire_tpkt.go; X.224
// in wire_x224.go; MCS Connect-Initial in wire_mcs.go; GCC
// Conference Create Request in wire_gcc.go; serverCore parser
// in wire_servercore.go; BER primitives in wire_ber.go.
//
// This file holds only the package doc and the protocol
// identifier constants used by rdp.go (the caller).
//
// This is a hand-written, NO-fscan-source implementation of the
// minimum RDP 5.0+ handshake needed to extract the server's
// identity BEFORE authentication. We never do Attach / Login /
// Session Setup — the connection is closed as soon as the GCC
// Conference Create Response is parsed.
//
// References:
//   - MS-RDPBCGR §2.2.1 (X.224 / TPKT / MCS / GCC)
//   - MS-RDPBCGR §2.2.1.3.2 (serverCore data block)
//   - RFC 1006 (TPKT)
//
// wire.go — RDP wire 协议包入口。
//
// 实现按协议层拆分（RFC 1006 / TPKT、X.224、MCS、GCC、serverCore）。
// 协议构造器（TPKTFrame、TPKTRead）在 wire_tpkt.go；X.224 在
// wire_x224.go；MCS Connect-Initial 在 wire_mcs.go；GCC
// Conference Create Request 在 wire_gcc.go；serverCore 解析器在
// wire_servercore.go；BER 原语在 wire_ber.go。
//
// 本文件只持包文档和 rdp.go（调用方）用的协议标识符常量。
//
// 这是手写的、不引用 fscan 源码的 RDP 5.0+ 握手最小子集实现，在
// 认证之前抽取服务器身份。绝不执行 Attach / Login / Session
// Setup——GCC Conference Create Response 解析完即关闭连接。
//
// 引用：
//   - MS-RDPBCGR §2.2.1 (X.224 / TPKT / MCS / GCC)
//   - MS-RDPBCGR §2.2.1.3.2 (serverCore 数据块)
//   - RFC 1006 (TPKT)
package rdp

// Protocol identifiers for the X.224 requestedProtocols field.
// / X.224 requestedProtocols 字段的协议标识符。
const (
	ProtocolRDP       uint32 = 0x00000000 // legacy RDP security
	ProtocolSSL       uint32 = 0x00000001 // TLS upgrade (v0.2+)
	ProtocolHYBRID    uint32 = 0x00000002 // NLA (CredSSP)
	ProtocolHYBRID_EX uint32 = 0x00000008 // NLA extended
)

