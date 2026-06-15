// Copyright (c) 2026 LCUstinian
// SPDX-License-Identifier: MIT
//
// LDAP Identify plugin. Raw LDAP search request retrieves the
// server's namingContexts — no bind, no auth, no session state.
//
// LDAP 识别插件。用原生 LDAP search 请求拿 server 的 namingContexts。
// 不 bind、不认证、不维护 session。
package ldap

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/plugins"
	"github.com/LCUstinian/FG-QiMen/internal/types"
)

// Plugin identifies LDAP servers via raw BER. / Plugin 用原生 BER 识别 LDAP 服务。
type Plugin struct{}

// New returns a new ldap plugin. / New 返回一个新的 ldap 插件。
func New() *Plugin { return &Plugin{} }

func init() { plugins.Register(New()) }

// Name implements plugins.Plugin. / Name 实现 plugins.Plugin。
func (p *Plugin) Name() string { return "ldap" }

// Ports returns default LDAP ports. / Ports 返回默认 LDAP 端口。
func (p *Plugin) Ports() []int { return []int{389, 636} }

// Modes returns Identify + Credential. / Modes 返回 Identify + Credential。
//
// Credential() is implemented in core/cred/protocols/ldap.go
// (LDAPAuthenticator via go-ldap/ldap/v3 simple bind). The plugin's
// Credential method is a no-op stub because the pipeline routes
// cred testing through the central credential.Scheduler.
// / Credential() 实现在 core/cred/protocols/ldap.go（LDAPAuthenticator
// via go-ldap/ldap/v3 simple bind）。plugin 的 Credential 方法是空 stub，
// 因为管线把凭据测试路由到中央 credential.Scheduler。
func (p *Plugin) Modes() plugins.Mode { return plugins.ModeIdentify | plugins.ModeCredential }

// Credential is a no-op stub. / Credential 空 stub。
func (p *Plugin) Credential(ctx context.Context, host string, port int, creds []types.Cred) *types.Result {
	return nil
}

// Identify sends a BindRequest(0, "", "") followed by a SearchRequest
// and parses the SearchResultEntry to extract the naming context.
//
// Identify 发 BindRequest(0, "", "") 然后 SearchRequest，解析
// SearchResultEntry 取命名上下文。
func (p *Plugin) Identify(ctx context.Context, host string, port int) *types.Result {
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	d := net.Dialer{Timeout: 3 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))

	// LDAPMessage ::= SEQUENCE { messageID, BindRequest, controls }
	// BindRequest ::= [APPLICATION 0] { version, name, authentication }
	// We send a minimal bind with empty name + simple auth (empty).
	// / 最小 bind：空 name + 空 simple auth。
	bind := berSequence(0, berBindRequest(3, "", ""))
	if _, err := conn.Write(bind); err != nil {
		return nil
	}
	// Now send a SearchRequest for baseDSE to get server info.
	// / 发 SearchRequest 查 baseDSE 拿 server 信息。
	search := berSequence(1, berSearchRequest("", "objectClass=*", "namingContexts"))
	if _, err := conn.Write(search); err != nil {
		return nil
	}
	// Read both responses. / 读两个响应。
	resp := make([]byte, 4096)
	n, _ := conn.Read(resp)
	if n < 10 {
		return nil
	}
	// Check for BindResponse (tag 0x61) or SearchResultEntry (tag 0x64).
	// / 看 BindResponse（tag 0x61）或 SearchResultEntry（tag 0x64）。
	if !bytesHasAny(resp[:n], []byte{0x61, 0x64, 0x65, 0xa3}) {
		return nil
	}
	// Extract namingContexts from SearchResultEntry.
	// / 从 SearchResultEntry 抽 namingContexts。
	banner := "LDAP"
	if nc := extractAttribute(resp[:n], "namingContexts"); nc != "" {
		banner = "LDAP: " + nc
	}
	return &types.Result{
		Host: host, Port: port, Service: "ldap",
		Banner: banner, Time: time.Now(),
	}
}

// berSequence builds a minimal LDAPMessage envelope. / berSequence 构造最小 LDAPMessage 信封。
func berSequence(msgID int, body []byte) []byte {
	out := []byte{0x30, 0x00} // SEQUENCE placeholder
	out = append(out, berInteger(msgID)...)
	out = append(out, body...)
	out[1] = byte(len(out) - 2)
	return out
}

// berInteger encodes an int as a BER INTEGER. / berInteger 把 int 编码为 BER INTEGER。
func berInteger(n int) []byte {
	return []byte{0x02, 0x01, byte(n)}
}

// berBindRequest builds APPLICATION 0 with version=3 + name + auth.
// / berBindRequest 构造 APPLICATION 0，含 version=3 + name + auth。
func berBindRequest(version int, name, pass string) []byte {
	body := []byte{0x60, 0x07, 0x02, 0x01, byte(version)} // APPLICATION 0
	body = append(body, 0x04, byte(len(name)))
	body = append(body, name...)
	// Simple auth [0] OCTET STRING. / Simple auth [0] OCTET STRING。
	body = append(body, 0x80, byte(len(pass)))
	body = append(body, pass...)
	return body
}

// berSearchRequest builds APPLICATION 3 (SearchRequest). / berSearchRequest 构造 APPLICATION 3。
func berSearchRequest(baseDN, filter, attrs string) []byte {
	body := []byte{0x63} // APPLICATION 3
	content := []byte{0x04, byte(len(baseDN))}
	content = append(content, baseDN...)
	content = append(content, 0x0a, byte(len(filter)))
	content = append(content, filter...)
	// attributes: SEQUENCE OF OCTET STRING. / attributes：SEQUENCE OF OCTET STRING。
	content = append(content, 0x30, byte(len(attrs)+2))
	content = append(content, 0x04, byte(len(attrs)))
	content = append(content, attrs...)
	body = append(body, byte(len(content)))
	body = append(body, content...)
	return body
}

// bytesHasAny returns true if b contains any of needles. / bytesHasAny 在 b 含 needles 之一时返回 true。
func bytesHasAny(b []byte, needles []byte) bool {
	for _, n := range needles {
		for _, c := range b {
			if c == n {
				return true
			}
		}
	}
	return false
}

// extractAttribute finds a string value for an attribute name in a
// BER-encoded SearchResultEntry. / extractAttribute 在 BER 编码的
// SearchResultEntry 中按 attribute 名找字符串值。
func extractAttribute(b []byte, name string) string {
	// Search for the attribute name preceded by its length byte. We
	// don't do full BER parsing; just look for the printable value.
	// / 找 attribute 名（前面有长度字节）。不做完整 BER 解析；
	// 只找可打印值。
	needle := []byte(name)
	for i := 0; i+len(needle) < len(b); i++ {
		if string(b[i:i+len(needle)]) != string(needle) {
			continue
		}
		// Length byte before, then the value bytes (LDAP strings are
		// raw bytes, not null-terminated in BER).
		// / 前面是长度字节，然后是值字节（LDAP 字符串是裸字节，BER 中不 null 结尾）。
		if i < 1 {
			continue
		}
		ln := int(b[i-1])
		if i+len(needle)+ln > len(b) {
			continue
		}
		val := string(b[i+len(needle) : i+len(needle)+ln])
		if isPrintable(val) {
			return val
		}
	}
	return ""
}

func isPrintable(s string) bool {
	for _, r := range s {
		if r < 32 || r > 126 {
			return false
		}
	}
	return s != ""
}
