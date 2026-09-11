// ldap_test.go — fake-server test for the LDAP Identify plugin. /
// LDAP 识别插件的假服务器测试。
//
// LDAP is TCP. The plugin sends a BindRequest(0, "", "") followed by
// a SearchRequest for baseDSE and parses the SearchResultEntry for
// the "namingContexts" attribute. We accept the connection, drain
// the two writes, and reply with a hand-crafted BER blob that
// contains the SearchResultEntry tag (0x64) plus a printable
// "namingContexts" attribute value. The plugin's bytesHasAny +
// extractAttribute are permissive enough that we don't need a fully
// spec-compliant SearchResultEntry.
//
// LDAP 是 TCP。插件发 BindRequest(0, "", "") 后跟一个查 baseDSE
// 的 SearchRequest，并解析 SearchResultEntry 的 "namingContexts"
// 属性。我们接受连接、读掉两次 write，然后回一个手写 BER 包，
// 含 SearchResultEntry 标签（0x64）和可打印的 "namingContexts"
// 值。plugin 的 bytesHasAny + extractAttribute 足够宽松，所以不
// 需要完全合规的 SearchResultEntry。
package ldap

import (
	"bytes"
	"context"
	"net"
	"testing"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/fakeserver"
)

// fakeSearchResultEntry builds the smallest blob that satisfies the
// plugin's bytesHasAny check (one of {0x61, 0x64, 0x65, 0xa3}) and
// extractAttribute scan (length byte, attribute name, printable
// value). / fakeSearchResultEntry 构造满足 plugin 的 bytesHasAny
// 校验（含 {0x61, 0x64, 0x65, 0xa3} 之一）和 extractAttribute 扫描
// （长度字节、属性名、可打印值）的最小 blob。
//
// Layout: 0x64 (SearchResultEntry tag) + <len> <"namingContexts">
// + <value>. extractAttribute looks for the ASCII name with a
// preceding length byte and reads exactly that many bytes after it.
// / 布局：0x64（SearchResultEntry 标签）+ <len> <"namingContexts">
// + <value>。extractAttribute 找 ASCII 名，前置长度字节，后面读
// 正好这么多字节的值。
func fakeSearchResultEntry(value string) []byte {
	name := []byte("namingContexts")
	buf := []byte{0x64}                 // SearchResultEntry APPLICATION 4
	buf = append(buf, byte(len(name)))  // length byte for attribute name
	buf = append(buf, name...)          // attribute name
	buf = append(buf, []byte(value)...) // printable value
	return buf
}

// TestLdap_IdentifyHit drives the happy path: a TCP server that
// replies with a SearchResultEntry carrying "namingContexts" =
// "dc=example,dc=com". The plugin must report Service="ldap" and
// a Banner that mentions LDAP. /
// TestLdap_IdentifyHit 跑正常路径：TCP server 用带 "namingContexts"
// = "dc=example,dc=com" 的 SearchResultEntry 回应。plugin 必须报
// Service="ldap" 且 Banner 含 LDAP。
func TestLdap_IdentifyHit(t *testing.T) {
	const wantValue = "dc=example,dc=com"
	got := make(chan []byte, 1)
	host, port := fakeserver.ListenLoop(t, func(c net.Conn) {
		// Drain the two writes (BindRequest + SearchRequest) — we
		// don't inspect them, just confirm both arrived.
		// / 读掉两次 write（BindRequest + SearchRequest）—— 不
		// 解析，只确认都到了。
		req := make([]byte, 1024)
		n, _ := c.Read(req)
		select {
		case got <- append([]byte(nil), req[:n]...):
		default:
		}
		_, _ = c.Write(fakeSearchResultEntry(wantValue))
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	hit := New().Identify(ctx, host, port)
	if hit == nil {
		t.Fatal("Identify = nil, want hit on LDAP SearchResultEntry reply")
	}
	if hit.Service != "ldap" {
		t.Errorf("Service = %q, want ldap", hit.Service)
	}
	if hit.Host != host || hit.Port != port {
		t.Errorf("Host:Port = %s:%d, want %s:%d", hit.Host, hit.Port, host, port)
	}
	if hit.Time.IsZero() {
		t.Error("Time is zero, want the identify timestamp")
	}
	// Banner must mention LDAP. It will either be the bare "LDAP"
	// (if extractAttribute failed to find a printable value) or
	// "LDAP: <namingContext>" on success. We accept either as long
	// as the bare string is present. / Banner 必须提到 LDAP。要么
	// 是裸 "LDAP"（extractAttribute 找不到可打印值时），要么是
	// "LDAP: <namingContext>"。只要裸字符串存在即可。
	if !bytes.Contains([]byte(hit.Banner), []byte("LDAP")) {
		t.Errorf("Banner = %q, want substring %q", hit.Banner, "LDAP")
	}

	// The fake server must have actually received both protocol
	// writes — otherwise an echo-only server would also pass.
	// / 假 server 必须真收到两次协议 write，否则纯 echo 也能通过。
	select {
	case req := <-got:
		// BindRequest (APPLICATION 0) + SearchRequest (APPLICATION 3)
		// are identifiable by their inner application tags 0x60
		// and 0x63. / BindRequest（APPLICATION 0）和
		// SearchRequest（APPLICATION 3）可通过内层 app 标签 0x60
		// 和 0x63 区分。
		if !bytes.Contains(req, []byte{0x60}) {
			t.Errorf("request missing BindRequest tag 0x60: %x", req)
		}
		if !bytes.Contains(req, []byte{0x63}) {
			t.Errorf("request missing SearchRequest tag 0x63: %x", req)
		}
	default:
		t.Error("fake server never received a request")
	}
}

// TestLdap_IdentifyMiss verifies the plugin rejects responses that
// lack any LDAP-shape tag (the bytesHasAny check). /
// TestLdap_IdentifyMiss 验证 plugin 拒绝无 LDAP 形态标签（bytesHasAny
// 校验失败）的响应。
func TestLdap_IdentifyMiss(t *testing.T) {
	host, port := fakeserver.ListenLoop(t, func(c net.Conn) {
		// Drain bind+search so the plugin's Write calls succeed,
		// then reply with bytes that contain NONE of the accepted
		// LDAP tags 0x61/0x64/0x65/0xa3. NOTE: bytesHasAny checks
		// individual bytes, so we must avoid lowercase 'a'(0x61),
		// 'd'(0x64), 'e'(0x65) as well as 0xa3. We use uppercase
		// letters + digits + spaces, all of which are outside the
		// needle set. / 读掉 bind+search 让 plugin 的 Write 成功，
		// 然后回不含任何 LDAP 标签 0x61/0x64/0x65/0xa3 的字节。
		// 注意 bytesHasAny 按单字节校验，所以必须避免小写 'a'(0x61)、
		// 'd'(0x64)、'e'(0x65) 以及 0xa3。用大写字母+数字+空格，
		// 都避开 needle 集合。
		req := make([]byte, 1024)
		_, _ = c.Read(req)
		_, _ = c.Write([]byte("THIS IS NOT LDAP PROTO 12345"))
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if hit := New().Identify(ctx, host, port); hit != nil {
		t.Errorf("Identify = %+v, want nil for non-LDAP reply", hit)
	}
}

// TestLdap_CredentialHit is skipped: ldap declares plugins.
// ModeIdentify | plugins.ModeCredential but its Credential method
// is a documented no-op stub that unconditionally returns nil
// (see ldap.go); real LDAP auth is implemented in
// core/cred/protocols/ldap.go via go-ldap/ldap/v3 simple bind,
// outside the plugin's Credential surface. No handshake for a
// fake server to drive. /
// TestLdap_CredentialHit 跳过：ldap 声明 plugins.ModeIdentify |
// plugins.ModeCredential 但 Credential 是文档化的 no-op stub，无
// 条件返回 nil（见 ldap.go）；真正的 LDAP auth 在 core/cred/
// protocols/ldap.go 用 go-ldap/ldap/v3 simple bind 实现，不在
// plugin 的 Credential 表面。没有可供假服务器驱动的握手。
func TestLdap_CredentialHit(t *testing.T) {
	t.Skip("ldap Credential is a documented no-op stub — Identify-only on the plugin surface (real auth via core/cred/protocols/ldap.go)")
}
