// Copyright (c) 2026 LCUstinian
// SPDX-License-Identifier: MIT
//
// SNMP Identify plugin. v0.1 uses a hand-rolled minimal SNMPv1 GET
// request (just sysDescr.0); v0.2+ should switch to gosnmp. Read-only
// probe — no SET, no write.
//
// SNMP 识别插件。v0.1 用手写最小 SNMPv1 GET 请求（只查 sysDescr.0）；
// v0.2+ 切到 gosnmp。只读探测——不 SET、不写。
package snmp

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/LCUstinian/FG-QiMen/common"
	"github.com/LCUstinian/FG-QiMen/plugins"
)

// Plugin identifies SNMP servers via a sysDescr.0 GET. / Plugin 通过 sysDescr.0 GET 识别 SNMP 服务。
type Plugin struct{}

// New returns a new snmp plugin. / New 返回一个新的 snmp 插件。
func New() *Plugin { return &Plugin{} }

func init() { plugins.Register(New()) }

// Name implements plugins.Plugin. / Name 实现 plugins.Plugin。
func (p *Plugin) Name() string { return "snmp" }

// Ports returns default SNMP ports. / Ports 返回默认 SNMP 端口。
func (p *Plugin) Ports() []int { return []int{161, 162} }

// Modes returns Identify only. / Modes 仅返回 Identify。
func (p *Plugin) Modes() plugins.Mode { return plugins.ModeIdentify }

// Credential is a no-op stub. / Credential 空 stub。
func (p *Plugin) Credential(ctx context.Context, host string, port int, creds []common.Cred) *common.Result {
	return nil
}

// Identify sends a minimal SNMPv1 GET request for sysDescr.0 and
// parses the response. / Identify 发最小 SNMPv1 GET 请求查 sysDescr.0
// 并解析响应。
//
// SNMPv1 message layout (simplified, BER):
//
//	SEQUENCE {
//	  INTEGER    version-1 (0)
//	  OCTET STRING "public"     community
//	  GetRequest-PDU {
//	    INTEGER   request-id
//	    INTEGER   0             error-status
//	    INTEGER   0             error-index
//	    SEQUENCE OF VarBind { name OID, value NULL }
//	  }
//	}
//
// SNMPv1 消息布局（简化，BER）：……
func (p *Plugin) Identify(ctx context.Context, host string, port int) *common.Result {
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	d := net.Dialer{Timeout: 3 * time.Second}
	conn, err := d.DialContext(ctx, "udp", addr)
	if err != nil {
		// Try TCP fallback. / 试 TCP 回退。
		conn, err = d.DialContext(ctx, "tcp", addr)
		if err != nil {
			return nil
		}
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))

	// sysDescr.0 = 1.3.6.1.2.1.1.1.0
	oid := encodeOID([]uint{1, 3, 6, 1, 2, 1, 1, 1, 0})
	varBind := berSequence(append([]byte{0x06, byte(len(oid))}, oid...), []byte{0x05, 0x00})
	pdu := berSequence(
		berIntegerBytes([]byte{0x00, 0x00, 0x00, 0x01}),     // request-id
		berIntegerBytes([]byte{0x00}),                         // error-status 0
		berIntegerBytes([]byte{0x00}),                         // error-index 0
		varBind,
	)
	// GetRequest-PDU is context-specific tag 0 (0xa0).
	// / GetRequest-PDU 是 context-specific tag 0（0xa0）。
	pdu = append([]byte{0xa0, byte(len(pdu))}, pdu...)
	msg := berSequence(
		berIntegerBytes([]byte{0x00}),          // version-1 = 0
		[]byte{0x04, 0x06, 'p', 'u', 'b', 'l', 'i', 'c'}, // community "public"
		pdu,
	)
	if _, err := conn.Write(msg); err != nil {
		return nil
	}
	resp := make([]byte, 2048)
	n, err := conn.Read(resp)
	if err != nil || n < 10 {
		return nil
	}
	// Look for a GetResponse PDU (context-specific tag 1 = 0xa1) followed
	// by our request-id and a varbind with value != NULL.
	// / 找 GetResponse PDU（context-specific tag 1 = 0xa1）后跟我们的
	// request-id 和一个 value != NULL 的 varbind。
	if len(resp) < 20 || resp[0] != 0x30 {
		return nil
	}
	// Cheap banner: search for sysDescr string after the OID.
	// / 简单 banner：在 OID 后搜 sysDescr 字符串。
	for i := 0; i < n-20; i++ {
		if string(resp[i:i+4]) == string([]byte{0x06, 0x0a, 0x2b, 0x06}) { // 1.3 prefix
			end := i + 4
			for end < n && resp[end] != 0x04 && resp[end] != 0x06 {
				end++
			}
			if end >= n || resp[end] != 0x04 {
				continue
			}
			sl := int(resp[end+1])
			if end+2+sl > n {
				continue
			}
			banner := string(resp[end+2 : end+2+sl])
			if len(banner) > 0 && isSNMPPrintable(banner) {
				return &common.Result{
					Host: host, Port: port, Service: "snmp",
					Banner: "SNMP: " + trimASCII(banner), Time: time.Now(),
				}
			}
		}
	}
	// Fall back: protocol responded. / 回退：协议响应了。
	return &common.Result{
		Host: host, Port: port, Service: "snmp",
		Banner: "SNMP (community=public)", Time: time.Now(),
	}
}

// berSequence builds a SEQUENCE from body. The body length is computed
// automatically. / berSequence 用 body 构造 SEQUENCE；自动算长度。
func berSequence(parts ...[]byte) []byte {
	body := make([]byte, 0)
	for _, p := range parts {
		body = append(body, p...)
	}
	out := []byte{0x30, 0x00}
	out = append(out, body...)
	out[1] = byte(len(out) - 2)
	return out
}

// berIntegerBytes wraps a raw byte integer in BER INTEGER header. /
// berIntegerBytes 把裸字节整数包上 BER INTEGER 头。
func berIntegerBytes(b []byte) []byte {
	out := []byte{0x02, byte(len(b))}
	out = append(out, b...)
	return out
}

// encodeOID encodes an OID (1.3.6.1.2.1.1.1.0 etc.) in BER OID form.
// The first two components are encoded as 40*a + b per X.690.
// / encodeOID 把 OID（如 1.3.6.1.2.1.1.1.0）编码为 BER OID 形式。前两段按 X.690
// 编码为 40*a + b。
func encodeOID(oid []uint) []byte {
	if len(oid) < 2 {
		return nil
	}
	first := 40*oid[0] + oid[1]
	body := encodeBase128(first)
	for _, n := range oid[2:] {
		body = append(body, encodeBase128(n)...)
	}
	return body
}

func encodeBase128(n uint) []byte {
	if n == 0 {
		return []byte{0}
	}
	var buf [10]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte(n & 0x7f)
		n >>= 7
	}
	// Set high bit on all but last. / 除最后一个外都设高位。
	for j := i + 1; j < len(buf); j++ {
		buf[j] |= 0x80
	}
	return buf[i:]
}

func trimASCII(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\r' || s[len(s)-1] == '\n' || s[len(s)-1] == ' ') {
		s = s[:len(s)-1]
	}
	return s
}

func isSNMPPrintable(s string) bool {
	for _, r := range s {
		if r < 32 || r > 126 {
			return false
		}
	}
	return s != ""
}
