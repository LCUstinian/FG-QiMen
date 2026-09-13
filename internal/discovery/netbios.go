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
	"fmt"
	"net"
	"strings"
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
// Hit on any response. Best-effort NBSTAT parsing: a response that
// fails to parse still yields a bare Hit (liveness semantics unchanged).
// / Probe 实现 alive.Probe。发单条 NBNS 状态查询，任何响应即返回
// Hit。NBSTAT 尽力解析：解析失败的响应仍产出裸 Hit（存活语义不变）。
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
	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	if err != nil {
		return alive.Hit{}, alive.ErrUnreachable
	}
	if n < 12 {
		return alive.Hit{}, alive.ErrUnreachable
	}
	// First 2 bytes are the transaction ID (echoed from request).
	// Bits in FLAGS determine success/error. We just check that
	// any reply came back. / 前 2 字节是 transaction ID（从请求回显）。
	// FLAGS 决定成功/错误。我们只检查有响应。
	hit := alive.Hit{
		Host:   host,
		Port:   137,
		Method: alive.MethodNetBIOS,
		RTT:    time.Since(start),
		Time:   time.Now(),
	}
	// Best-effort NBSTAT parse: names + MAC feed the scope tracker.
	// A hostile/garbage response must not fail the probe — parse
	// errors just leave Attrs empty. / 尽力解析 NBSTAT：名字 + MAC
	// 喂范围追踪器。恶意/垃圾响应不得让探测失败——解析错误只是
	// 让 Attrs 为空。
	if attrs := parseNBSTATAttrs(buf[:n], host); attrs != nil {
		hit.Attrs = attrs
	}
	return hit, nil
}

// nbstatEntry is one decoded NBSTAT name-table entry.
// / nbstatEntry 是一条解码后的 NBSTAT 名字表条目。
type nbstatEntry struct {
	Name   string
	Group  bool
	Suffix byte
	IP     string
}

// nbstatMaxEntries caps how many name entries we parse from one
// response — NBSTAT allows up to 255; real hosts register a handful.
// / nbstatMaxEntries 限制单响应解析的名字条目数——NBSTAT 最多允许
// 255 条；真实主机只注册几条。
const nbstatMaxEntries = 64

// parseNBSTATAttrs best-effort parses an NBNS NBSTAT response into the
// scope-tracker attribute set. Layout (RFC 1002 §4.2 header/question +
// answer RR; RDATA per the NBSTAT format — name table entries, then
// Windows' trailing 6-byte adapter MAC):
//
//	header(12) → question(1+32+1+4) → answer RR name(ptr or full) →
//	type(2)=0x0021 class(2) TTL(4) RDLENGTH(2) → RDATA:
//	  num_names(1) × {name(16) flags(2) addr(4)} … [MAC(6)]
//
// Returns nil when the packet does not look like a parseable NBSTAT
// reply. / parseNBSTATAttrs 尽力把 NBNS NBSTAT 响应解析为范围追踪器
// 属性集。返回 nil 表示包不像可解析的 NBSTAT 回复。
func parseNBSTATAttrs(pkt []byte, responder string) map[string]string {
	if len(pkt) < 12+38 {
		return nil
	}
	flags := binary.BigEndian.Uint16(pkt[2:4])
	if flags&0x8000 == 0 { // not a response / 不是响应
		return nil
	}
	ancount := binary.BigEndian.Uint16(pkt[6:8])
	if ancount == 0 {
		return nil
	}
	off := 12
	// Skip the question section (echoed name). A leading length byte
	// < 0x40 is a plain label; 0xC0+ is a compression pointer (then
	// there is no question to skip). / 跳过问题段（回显名）。首字节
	// < 0x40 是普通标签；0xC0+ 是压缩指针（此时无问题可跳）。
	qlen := int(pkt[off])
	if qlen < 0x40 {
		off += 1 + qlen + 1 // len + name + null
		off += 4            // qtype + qclass
	}
	if off+10 > len(pkt) {
		return nil
	}
	// Answer RR name: pointer (2 bytes) or full encoded label.
	// / 应答 RR 名：指针（2 字节）或完整编码标签。
	if pkt[off]&0xC0 == 0xC0 {
		off += 2
	} else {
		off += 1 + int(pkt[off]) + 1
	}
	if off+8 > len(pkt) {
		return nil
	}
	rrType := binary.BigEndian.Uint16(pkt[off : off+2])
	off += 2 + 2 + 4 // type + class + TTL / 类型 + 类 + TTL
	rdlen := int(binary.BigEndian.Uint16(pkt[off : off+2]))
	off += 2
	if rrType != 0x0021 || rdlen < 1 || off+rdlen > len(pkt) {
		return nil
	}
	rdata := pkt[off : off+rdlen]
	numNames := int(rdata[0])
	if numNames == 0 || numNames > nbstatMaxEntries {
		return nil
	}
	if len(rdata) < 1+numNames*22 {
		return nil
	}
	var entries []nbstatEntry
	for i := 0; i < numNames && i < nbstatMaxEntries; i++ {
		e := rdata[1+i*22 : 1+(i+1)*22]
		name := nbstatRawName(e[:16])
		if name == "" {
			continue
		}
		fl := binary.BigEndian.Uint16(e[16:18])
		ip := net.IP(e[18:22]).String()
		entries = append(entries, nbstatEntry{
			Name:   name,
			Group:  fl&0x8000 != 0,
			Suffix: e[15], // last byte of the 16-byte name / 16 字节名的末字节
			IP:     ip,
		})
	}
	if len(entries) == 0 {
		return nil
	}
	attrs := map[string]string{}
	own := make([]string, 0, len(entries))
	group := make([]string, 0, 1)
	alias := make([]string, 0, len(entries))
	names := make([]string, 0, len(entries))
	for _, en := range entries {
		names = append(names, en.Name+"@"+en.IP)
		if en.Group {
			if len(group) == 0 {
				group = append(group, en.Name)
			}
			continue
		}
		if len(own) == 0 {
			own = append(own, en.Name)
		}
		if en.IP != "" && en.IP != responder {
			alias = append(alias, en.IP+"="+en.Name)
		}
	}
	if len(own) > 0 {
		attrs["name"] = own[0]
	}
	if len(group) > 0 {
		attrs["domain"] = group[0]
	}
	attrs["names"] = strings.Join(names, ",")
	if len(alias) > 0 {
		attrs["aliases"] = strings.Join(alias, ",")
	}
	// Microsoft appends the adapter MAC as the last 6 bytes of RDATA
	// (not in the RFC; this is what nbtstat -A and nmap's nbstat.nse
	// read). / 微软把适配器 MAC 追加为 RDATA 末尾 6 字节（RFC 未定义
	// ——这正是 nbtstat -A 与 nmap nbstat.nse 读取的位置）。
	used := 1 + numNames*22
	if rest := len(rdata) - used; rest >= 6 {
		mac := rdata[len(rdata)-6:]
		attrs["mac"] = fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x",
			mac[0], mac[1], mac[2], mac[3], mac[4], mac[5])
	}
	return attrs
}

// nbstatRawName sanitizes the RAW 16-byte name field of an NBSTAT
// RDATA entry (first-level encoding applies only to DNS-style labels —
// RDATA carries the name verbatim: up to 15 chars space-padded, byte
// 16 = suffix). Returns the printable name, or "" when nothing
// printable remains. / nbstatRawName 清洗 NBSTAT RDATA 条目的原始
// 16 字节名字段（一级编码只用于 DNS 式标签——RDATA 原样携带名字：
// ≤15 字符空格填充，第 16 字节 = 后缀）。返回可打印名，无可打印字
// 符时返回空串。
func nbstatRawName(field []byte) string {
	if len(field) != 16 {
		return ""
	}
	end := 14 // bytes 0..14 = name, byte 15 = suffix / 0..14 是名字，15 是后缀
	for end >= 0 && (field[end] == ' ' || field[end] == 0) {
		end--
	}
	var sb strings.Builder
	for i := 0; i <= end; i++ {
		if field[i] >= 0x20 && field[i] < 0x7F {
			sb.WriteByte(field[i])
		}
	}
	return sb.String()
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
// split into two nibbles, each mapped to the character 'A'+nibble
// (0x41..0x50). This is the wire-standard first-level encoding per
// RFC 1001 §14.1 — the same form nbtstat and nmap's nbstat.nse emit
// (the wildcard name encodes to the canonical "CKAAAA…" prefix).
//
// / encodeNetBIOSName 把 16 字节 ASCII 名编码成 32 字节"一级编码"
// NetBIOS 名。每个输入字节拆成两个半字节，各映射为 'A'+半字节
// （0x41..0x50）。RFC 1001 §14.1 线上标准一级编码——与 nbtstat、
// nmap nbstat.nse 的形式一致（通配名编码为规范的 "CKAAAA…" 前缀）。
func encodeNetBIOSName(name string) []byte {
	const label = 'A' // 0x41 — RFC 1001 first-level encoding base / 一级编码基
	in := make([]byte, 16)
	copy(in, []byte(name))
	out := make([]byte, 32)
	for i := 0; i < 16; i++ {
		// ADD, not OR: with an 'A' base the low nibble of the label is
		// 1, so OR would corrupt odd nibbles ('P' must stay 'P', not
		// become 'O'). / 用加法不用或：'A' 基的低半字节是 1，或运算会
		// 破坏奇数半字节（'P' 必须还是 'P'，不能变 'O'）。
		out[2*i] = label + (in[i] >> 4) // high nibble
		out[2*i+1] = label + (in[i] & 0x0F)
	}
	return out
}

// init registers the NBNS probe with the alive package so callers
// who blank-import this package get it in alive.DefaultOptions().
// init 把 NBNS probe 注册到 alive 包，使 blank-import 本包的调用方
// 在 alive.DefaultOptions() 中拿到它。
func init() {
	alive.RegisterLANProbe(NewNBNSProbe())
}
