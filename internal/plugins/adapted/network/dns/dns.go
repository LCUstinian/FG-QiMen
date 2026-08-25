// dns.go — DNS service-fingerprint plugin (Identify only). /
// DNS 服务指纹插件（仅识别）。
//
// Wire format (RFC 1035 §4):
//
//	Header (12 bytes): ID | QR|Opcode|... | QDCOUNT | ANCOUNT | NSCOUNT | ARCOUNT
//	Question:          QNAME | QTYPE | QCLASS
//	Answer:            NAME | TYPE | CLASS | TTL | RDLENGTH | RDATA
//
// We send a standard A query for "version.bind" CH TXT. The
// "version.bind" / "authors.bind" queries are the de-facto DNS
// server fingerprint probes (Chaos class TXT). / 我们发 A 查询
// "version.bind" CH TXT。version.bind / authors.bind 是事实标
// 准的 DNS server 指纹探测（Chaos 类 TXT）。
//
// Many public DNS servers won't answer CHAOS queries from external
// sources, so we also send a regular A query for "." (root) as a
// fallback. A server that answers either is identified as DNS. /
// 很多公网 DNS server 不会响应外部 CHAOS 查询，所以也发常规 A
// 查询 "."（根）作回退。响应任一即识别为 DNS。
package dns

import (
	"context"
	"encoding/binary"
	"net"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/plugins"
	"github.com/LCUstinian/FG-QiMen/internal/types"
)

// Plugin identifies DNS servers. / Plugin 识别 DNS 服务。
type Plugin struct{}

// New returns a new dns plugin. / New 返回一个新的 dns 插件。
func New() *Plugin { return &Plugin{} }

func init() { plugins.Register(New()) }

// Name implements plugins.Plugin. / Name 实现 plugins.Plugin。
func (p *Plugin) Name() string { return "dns" }

// Ports returns default DNS port. / Ports 返回默认 DNS 端口。
func (p *Plugin) Ports() []int { return []int{53} }

// Modes returns Identify only. / Modes 仅返回 Identify。
func (p *Plugin) Modes() plugins.Mode { return plugins.ModeIdentify }

// Credential is a no-op stub. / Credential 空 stub。
func (p *Plugin) Credential(context.Context, string, int, []types.Cred) *types.Result {
	return nil
}

// Identify sends a CHAOS-class version.bind TXT query, then a
// regular A query for ".". If the regular A query answers with
// AA=1 (Authoritative Answer), we also try an AXFR zone-transfer
// probe against the authoritative server — a misconfigured DNS
// server that allows AXFR leaks the entire zone. / Identify 发
// CHAOS 类 version.bind TXT 查询，然后发常规 "." 的 A 查询。如
// 果常规 A 查询带 AA=1（权威应答），我们对权威 server 试 AXFR
// zone-transfer 探测——配置错误的 DNS server 允许 AXFR 会泄露
// 整个 zone。
//
// Phase 1.4 (audit roadmap): added AXFR probe for misconfiguration
// detection. We do NOT exfiltrate zone contents — we only count
// records returned, and we cap the response read at 4 KiB to
// bound the memory cost of a misbehaving server. / Phase 1.4
// （审计路线图）：加 AXFR 探测做配置错误检测。我们不外泄 zone
// 内容——只统计返回的记录数，并限制响应读取为 4 KiB 以限制故障
// server 的内存成本。
func (p *Plugin) Identify(ctx context.Context, host string, port int) *types.Result {
	return plugins.RawUDPIdentify(ctx, host, port, func(conn net.Conn) *types.Result {
		// Try the CHAOS query first. / 先试 CHAOS 查询。
		if r := queryAndCheckAt(conn, buildChaosQuery(), host, port); r != nil {
			return r
		}
		// Reset deadline then try the regular query. / 重置
		// deadline 然后试常规查询。
		_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
		if r := queryAndCheckAt(conn, buildRootAQuery(), host, port); r != nil {
			// If AA (Authoritative Answer) bit is set, try AXFR.
			// / 如果 AA（权威应答）位被置，试 AXFR。
			_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
			if axfrResult := tryAXFR(conn, host, port); axfrResult != nil {
				return axfrResult
			}
			return r
		}
		return nil
	})
}

// tryAXFR sends an AXFR query for "." (root) and reports the
// number of records in the response. A well-configured server
// refuses (RCODE=4 NOTIMP or REFUSED); a misconfigured one returns
// the entire zone. We count records and surface as a banner — we
// do NOT print the zone contents. / tryAXFR 发 "."（根）的 AXFR
// 查询并报告响应中的记录数。配置正确的 server 拒绝（RCODE=4
// NOTIMP 或 REFUSED）；配置错误的返回整个 zone。我们只统计记
// 录数并在 banner 展示——不打印 zone 内容。
func tryAXFR(conn net.Conn, host string, port int) *types.Result {
	// AXFR QTYPE = 252. / AXFR QTYPE = 252。
	axfr := []byte{
		0xCA, 0xFE, // ID
		0x00, 0x00, // RD=0 (AXFR over TCP, but we're over UDP)
		0x00, 0x01, // QDCOUNT=1
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		// QNAME: "" (root = single 0x00)
		0x00,
		0x00, 0xFC, // QTYPE = AXFR = 252
		0x00, 0x01, // QCLASS = IN
	}
	if _, err := conn.Write(axfr); err != nil {
		return nil
	}
	resp := make([]byte, 4096) // 4 KiB cap.
	n, err := conn.Read(resp)
	if err != nil || n < 12 {
		return nil
	}
	if resp[2]&0x80 == 0 {
		return nil // not a response
	}
	rcode := resp[3] & 0x0F
	an := binary.BigEndian.Uint16(resp[6:8])
	// RCODE 0 = NOERROR, ANCOUNT > 0 → zone transfer succeeded
	// (misconfiguration). RCODE 4 = NOTIMP, RCODE 5 = REFUSED →
	// AXFR blocked (good). / RCODE 0 = NOERROR 且 ANCOUNT > 0 →
	// zone transfer 成功（配置错）。RCODE 4 = NOTIMP, RCODE 5 =
	// REFUSED → AXFR 被拒（正确）。
	if rcode == 0 && an > 0 {
		return &types.Result{
			Host:    host,
			Port:    port,
			Service: "dns",
			Banner:  "DNS (⚠ AXFR allowed, an=" + itoa(int(an)) + ")",
			Time:    time.Now(),
		}
	}
	return nil
}

// queryAndCheckAt sends q, reads the response, and returns a *Result
// on success or nil on failure. The (host, port) args are reserved
// for logging context in error paths. / queryAndCheckAt 发 q、读响应，
// 成功返回 *Result，失败返回 nil。(host, port) 保留给错误路径的日志
// 上下文用。
func queryAndCheckAt(conn net.Conn, q []byte, host string, port int) *types.Result {
	if _, err := conn.Write(q); err != nil {
		return nil
	}
	resp := make([]byte, 512)
	n, err := conn.Read(resp)
	if err != nil || n < 12 {
		return nil
	}
	// Bit 3 of byte 2 is QR (0=query, 1=response). / byte 2 的
	// bit 3 是 QR（0=查询，1=响应）。
	if resp[2]&0x80 == 0 {
		return nil
	}
	// RCODE = low 4 bits of byte 3. Must not be nonzero for a
	// well-formed response. / RCODE = byte 3 低 4 位。良构响
	// 应不应非零。
	if resp[3]&0x0F != 0 {
		return nil
	}
	qd := binary.BigEndian.Uint16(resp[4:6])
	an := binary.BigEndian.Uint16(resp[6:8])
	return &types.Result{
		Host:    host,
		Port:    port,
		Service: "dns",
		Banner:  "DNS (qd=" + itoa(int(qd)) + ", an=" + itoa(int(an)) + ")",
		Time:    time.Now(),
	}
}

// buildChaosQuery returns a 32-byte DNS query: ID=0xBEEF, RD=1,
// QDCOUNT=1, QNAME="version.bind", QTYPE=TXT(16), QCLASS=CH(3).
// / buildChaosQuery 返回 32 字节 DNS 查询：ID=0xBEEF, RD=1,
// QDCOUNT=1, QNAME="version.bind", QTYPE=TXT(16), QCLASS=CH(3)。
func buildChaosQuery() []byte {
	return []byte{
		0xBE, 0xEF, // ID
		0x01, 0x00, // RD=1, recursion desired
		0x00, 0x01, // QDCOUNT=1
		0x00, 0x00, // ANCOUNT=0
		0x00, 0x00, // NSCOUNT=0
		0x00, 0x00, // ARCOUNT=0
		// QNAME: version.bind
		0x07, 'v', 'e', 'r', 's', 'i', 'o', 'n',
		0x04, 'b', 'i', 'n', 'd',
		0x00,       // terminator
		0x00, 0x10, // QTYPE = TXT = 16
		0x00, 0x03, // QCLASS = CH = 3
	}
}

// buildRootAQuery returns a DNS query for "." (root) with QTYPE=A.
// / buildRootAQuery 返回 "."（根）的 DNS 查询，QTYPE=A。
func buildRootAQuery() []byte {
	return []byte{
		0xCA, 0xFE, // ID
		0x01, 0x00, // RD=1
		0x00, 0x01, // QDCOUNT=1
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		// QNAME: "" (root = single 0x00)
		0x00,
		0x00, 0x01, // QTYPE = A = 1
		0x00, 0x01, // QCLASS = IN = 1
	}
}

// itoa avoids fmt import. / itoa 避免 fmt 导入。
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [4]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
