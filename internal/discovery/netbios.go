// netbios.go — NetBIOS (NBNS) probe.
//
// Sends a NetBIOS Name Service (NBNS) status query to UDP 137 and
// treats any valid response as proof of liveness. The query asks
// for the target's own name; the response carries the NBSTAT
// structure with names + MAC.
//
// We don't actually need to parse the response — the goal of
// aliveness probing is just "did anything reply?". A target that
// answers an NBNS query is on the LAN and running the NetBIOS
// service. A non-NetBIOS host will still drop the packet
// silently (UDP), and the read will time out.
//
// netbios.go — NetBIOS（NBNS）探测。
// 发 NetBIOS Name Service（NBNS）状态查询到 UDP 137 并把任何有效响应
// 视为存活证据。查询请求目标自己的名字；响应带 NBSTAT 结构（名字 + MAC）。
//
// 我们其实不解析响应——存活探测的目标只是"有东西响应了吗？"。响应
// NBNS 查询的目标在 LAN 上且跑 NetBIOS 服务。非 NetBIOS 主机直接
// 丢弃包（UDP），读超时。
package discovery

import (
	"context"
	"encoding/binary"
	"net"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/core/alive"
)

// DefaultNBNSName is the wildcard name we encode for status queries.
// Encoding "*" (one byte 0x2A) as a NetBIOS name produces the
// 32-byte name "*               \x00" used in NBNS wildcard queries.
//
// / DefaultNBNSName 是状态查询用的通配符名。把 "*"（单字节 0x2A）
// 当 NetBIOS 名编码产生 32 字节名 "*               \x00"，用于 NBNS 通配符查询。
const DefaultNBNSName = "*\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00\x00"

// NBNSProbe probes hosts via NetBIOS Name Service (UDP 137).
// NBNSProbe 通过 NetBIOS Name Service（UDP 137）探测主机。
type NBNSProbe struct {
	// NameBytes is the encoded NetBIOS name to query (32 bytes).
	// Defaults to DefaultNBNSName if empty. / NameBytes 是查询用
	// 的编码后 NetBIOS 名（32 字节）。空则用 DefaultNBNSName。
	NameBytes []byte
}

// NewNBNSProbe returns an NBNSProbe with the wildcard name.
// NewNBNSProbe 返回使用通配符名的 NBNSProbe。
func NewNBNSProbe() *NBNSProbe { return &NBNSProbe{NameBytes: encodeNetBIOSName(DefaultNBNSName)} }

// Name implements alive.Probe. / Name 实现 alive.Probe。
func (p *NBNSProbe) Name() string { return "netbios" }

// Method implements alive.Probe. / Method 实现 alive.Probe。
func (p *NBNSProbe) Method() alive.Method { return alive.MethodNetBIOS }

// Available implements alive.Probe. NBNS uses raw UDP — works everywhere.
// / Available 实现 alive.Probe。NBNS 用裸 UDP——所有平台都能用。
func (p *NBNSProbe) Available() error { return nil }

// Probe implements alive.Probe. Sends a single NBNS status query, returns
// Hit on any response. / Probe 实现 alive.Probe。发单条 NBNS 状态查询，
// 任何响应即返回 Hit。
func (p *NBNSProbe) Probe(ctx context.Context, host string, timeout time.Duration) (alive.Hit, error) {
	start := time.Now()
	addr := net.JoinHostPort(host, "137")
	d := net.Dialer{Timeout: timeout}
	conn, err := d.DialContext(ctx, "udp", addr)
	if err != nil {
		return alive.Hit{}, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))
	pkt := buildNBNSStatusQuery(p.NameBytes)
	if _, err := conn.Write(pkt); err != nil {
		return alive.Hit{}, err
	}
	// Read response: NBNS response is at least 12 bytes
	// (header: NAME_TRN_ID(2) + FLAGS(2) + QDCOUNT(2) + ANCOUNT(2) +
	// NSCOUNT(2) + ARCOUNT(2)). / 读响应：NBNS 响应至少 12 字节
	//（头：NAME_TRN_ID(2) + FLAGS(2) + QDCOUNT(2) + ANCOUNT(2) +
	// NSCOUNT(2) + ARCOUNT(2)）。
	buf := make([]byte, 512)
	if _, err := conn.Read(buf); err != nil {
		return alive.Hit{}, alive.ErrUnreachable
	}
	if len(buf) < 12 {
		return alive.Hit{}, alive.ErrUnreachable
	}
	// First 2 bytes are the transaction ID (echoed from request).
	// Bits in FLAGS determine success/error. We just check that
	// any reply came back. / 前 2 字节是 transaction ID（从请求回显）。
	// FLAGS 决定成功/错误。我们只检查有响应。
	return alive.Hit{
		Host:   host,
		Port:   137,
		Method: alive.MethodNetBIOS,
		RTT:    time.Since(start),
		Time:   time.Now(),
	}, nil
}

// buildNBNSStatusQuery builds a NetBIOS Name Service "Status"
// query packet (opcode = 0x00, RD = 1). Layout:
//   - 2 bytes: NAME_TRN_ID = 0x1234 (arbitrary)
//   - 2 bytes: FLAGS = 0x0110 (standard query, RD=1)
//   - 2 bytes: QDCOUNT = 0x0001 (1 question)
//   - 2 bytes: ANCOUNT = 0x0000
//   - 2 bytes: NSCOUNT = 0x0000
//   - 2 bytes: ARCOUNT = 0x0000
//   - Question: encoded name (32 bytes) + null terminator (1 byte) +
//     type (2 bytes) + class (2 bytes) = 37 bytes
//
// / buildNBNSStatusQuery 构造 NetBIOS Name Service "Status" 查询包
// （opcode = 0x00，RD = 1）。布局：
//   - 2 字节：NAME_TRN_ID = 0x1234（任意）
//   - 2 字节：FLAGS = 0x0110（标准查询，RD=1）
//   - 2 字节：QDCOUNT = 0x0001（1 个问题）
//   - 2 字节：ANCOUNT = 0x0000
//   - 2 字节：NSCOUNT = 0x0000
//   - 2 字节：ARCOUNT = 0x0000
//   - 问题：编码名（32 字节）+ 空结束符（1 字节）+
//     type（2 字节）+ class（2 字节）= 37 字节
func buildNBNSStatusQuery(nameBytes []byte) []byte {
	if len(nameBytes) != 32 {
		// Re-encode to ensure 32 bytes. / 重新编码确保 32 字节。
		nameBytes = encodeNetBIOSName(DefaultNBNSName)
	}
	pkt := make([]byte, 0, 12+33+4)
	// Header (12 bytes). / 头（12 字节）。
	pkt = append(pkt, 0x12, 0x34) // NAME_TRN_ID
	pkt = append(pkt, 0x01, 0x10) // FLAGS: standard query + RD
	pkt = append(pkt, 0x00, 0x01) // QDCOUNT
	pkt = append(pkt, 0x00, 0x00) // ANCOUNT
	pkt = append(pkt, 0x00, 0x00) // NSCOUNT
	pkt = append(pkt, 0x00, 0x00) // ARCOUNT
	// Question name (length-prefixed 32 bytes + null).
	// / 问题名（长度前缀 32 字节 + 空）。
	pkt = append(pkt, 0x20)         // 32 (length of encoded name)
	pkt = append(pkt, nameBytes...) // 32 bytes
	pkt = append(pkt, 0x00)         // null terminator
	// Type (NBSTAT = 0x0021) + Class (IN = 0x0001).
	// / Type（NBSTAT = 0x0021）+ Class（IN = 0x0001）。
	var nbstatType [2]byte
	binary.BigEndian.PutUint16(nbstatType[:], 0x0021)
	pkt = append(pkt, nbstatType[:]...)
	var inClass [2]byte
	binary.BigEndian.PutUint16(inClass[:], 0x0001)
	pkt = append(pkt, inClass[:]...)
	return pkt
}

// encodeNetBIOSName encodes a 16-byte ASCII name into the
// 32-byte "first-level encoded" NetBIOS name. Each input byte is
// split into two nibbles, each shifted into a separate output
// byte with the high bit set. This is the standard NetBIOS name
// encoding per RFC 1001 §14.1.
//
// / encodeNetBIOSName 把 16 字节 ASCII 名编码成 32 字节"一级编码"
// NetBIOS 名。每个输入字节拆成两个半字节，各移位后写入独立输出字节
// 高位。RFC 1001 §14.1 标准 NetBIOS 名编码。
func encodeNetBIOSName(name string) []byte {
	const (
		label      = 0x20 // 0b00100000 = 0x20 (RFC 1001 encoded label)
		lowNibble  = 0x0F
		highNibble = 0xF0
	)
	in := make([]byte, 16)
	copy(in, []byte(name))
	out := make([]byte, 32)
	for i := 0; i < 16; i++ {
		out[2*i] = label | (in[i] >> 4) // high nibble, shifted and OR'd
		out[2*i+1] = label | (in[i] & lowNibble)
	}
	_ = highNibble // kept for readability; both nibbles used
	return out
}

// init registers the NBNS probe with the alive package so callers
// who blank-import this package get it in alive.DefaultOptions().
// init 把 NBNS probe 注册到 alive 包，使 blank-import 本包的调用方
// 在 alive.DefaultOptions() 中拿到它。
func init() {
	alive.RegisterLANProbe(NewNBNSProbe())
}
