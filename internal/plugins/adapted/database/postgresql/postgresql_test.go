// postgresql_test.go — A.6 coverage baseline for the postgresql plugin.
//
// postgresql_test.go — postgresql 插件的 A.6 覆盖率基线。
//
// Wire format being faked (postgresql.go:62-103):
//
//	client writes StartupMessage:
//	  int32 length | int32 protocol(3,0) | "user\0postgres\0"
//	                              | "database\0postgres\0" | 0x00
//	server replies with a single message whose type byte is
//	  'R' → AuthenticationOk / AuthenticationCleartextPassword
//	  'E' → ErrorResponse
//
// The plugin inspects only resp[0]; the body is irrelevant to the
// hit/miss decision. The handler drains the client request bytes
// (so the kernel doesn't RST the connection) and writes the
// minimum canonical AuthenticationOk frame.
//
// / 仿真的线协议格式 (postgresql.go:62-103)：客户端写
// StartupMessage（int32 长度 | int32 协议 3,0 | "user\0postgres\0" |
// "database\0postgres\0" | 0x00），服务端回类型字节为 'R' 或 'E'
// 的单条消息。插件只看 resp[0]，body 不影响命中判断。handler
// 消耗客户端写过来的字节以防内核 RST，然后写最小的
// AuthenticationOk 帧。
package postgresql

import (
	"context"
	"encoding/binary"
	"net"
	"testing"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/fakeserver"
)

// drainStartup reads and discards the client's StartupMessage so
// the connection's receive buffer is drained before we write the
// response. Prevents RST on platforms that close the read side
// before write. The plugin may send up to ~100 bytes for a typical
// StartupMessage; we read with a 4 KiB buffer to be safe.
// / drainStartup 读并丢弃客户端的 StartupMessage，避免我们写响应
// 时 read 侧被关导致 RST。典型 StartupMessage 不超过 ~100 字节，
// 用 4 KiB 缓冲区以防万一。
func drainStartup(c net.Conn) {
	buf := make([]byte, 4096)
	_, _ = c.Read(buf)
}

// authOKFrame builds a minimum AuthenticationOk message: 'R' + 4
// zero bytes (the length field; the plugin ignores body bytes).
// Total = 5 bytes, which is exactly the n < 5 floor the plugin
// uses to reject truncated responses.
// / authOKFrame 构造最小的 AuthenticationOk：'R' + 4 字节零
// （长度字段；插件不解析 body）。共 5 字节，正好满足插件
// n < 5 的下限拒绝截断响应。
func authOKFrame() []byte {
	b := make([]byte, 5)
	b[0] = 'R'
	binary.BigEndian.PutUint32(b[1:], 0)
	return b
}

// errorResponseFrame builds a minimum ErrorResponse frame: 'E' +
// 4 zero bytes. Same total size as authOKFrame; the plugin only
// inspects the leading type byte. The body would normally hold
// field/tag/terminator pairs (ErrorResponse is variable-length)
// but the plugin does not parse it.
// / errorResponseFrame 构造最小的 ErrorResponse：'E' + 4 字节零。
// 与 authOKFrame 同长；插件只看类型字节。body 正常情况下承载
// field/tag/terminator 对（ErrorResponse 是变长），但插件不解析。
func errorResponseFrame() []byte {
	b := make([]byte, 5)
	b[0] = 'E'
	binary.BigEndian.PutUint32(b[1:], 0)
	return b
}

// TestPostgresql_IdentifyHit spins up a TCP fake server that
// answers with the AuthenticationOk type byte. The plugin must
// identify the service as "postgresql" with banner "PostgreSQL".
// / 启 TCP 假 server 回 AuthenticationOk。插件应识别服务为
// "postgresql"，banner 为 "PostgreSQL"。
func TestPostgresql_IdentifyHit(t *testing.T) {
	host, port := fakeserver.ListenLoop(t, func(c net.Conn) {
		drainStartup(c)
		_, _ = c.Write(authOKFrame())
	})
	p := New()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	r := p.Identify(ctx, host, port)
	if r == nil {
		t.Fatal("Identify returned nil for AuthenticationOk response")
	}
	if r.Service != "postgresql" {
		t.Errorf("Service = %q, want %q", r.Service, "postgresql")
	}
	if r.Banner != "PostgreSQL" {
		t.Errorf("Banner = %q, want %q", r.Banner, "PostgreSQL")
	}
	if r.Host != host || r.Port != port {
		t.Errorf("Host/Port = %q:%d, want %q:%d", r.Host, r.Port, host, port)
	}
}

// TestPostgresql_IdentifyAuthError covers the second accepted
// response shape at postgresql.go:92 — 'E' (ErrorResponse) is
// still a PostgreSQL hit, just with an auth-error banner. This
// matters because real Postgres servers that reject the
// user/database we sent still prove they're PG.
// / 验证 postgresql.go:92 接受的第二种响应形态：'E'
// （ErrorResponse）仍是 PG 命中，只是 banner 标 auth-error。
// 真实 PG 服务拒了我们发的 user/database 时仍证明是 PG。
func TestPostgresql_IdentifyAuthError(t *testing.T) {
	host, port := fakeserver.ListenLoop(t, func(c net.Conn) {
		drainStartup(c)
		_, _ = c.Write(errorResponseFrame())
	})
	p := New()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	r := p.Identify(ctx, host, port)
	if r == nil {
		t.Fatal("Identify returned nil for ErrorResponse response")
	}
	if r.Service != "postgresql" {
		t.Errorf("Service = %q, want %q", r.Service, "postgresql")
	}
	if r.Banner != "PostgreSQL (auth error)" {
		t.Errorf("Banner = %q, want %q", r.Banner, "PostgreSQL (auth error)")
	}
}

// TestPostgresql_IdentifyMiss covers the negative case: the
// server replies with bytes whose leading type byte is neither
// 'R' nor 'E'. The plugin must return nil so we don't
// false-positive on a non-PostgreSQL TCP service.
// / 验证反向 case：服务端返回首字节既非 'R' 也非 'E'。
// 插件必须返 nil，避免在非 PostgreSQL 服务上假阳性。
func TestPostgresql_IdentifyMiss(t *testing.T) {
	host, port := fakeserver.ListenLoop(t, func(c net.Conn) {
		drainStartup(c)
		// 'Z' = ReadyForQuery; not 'R' or 'E', so no hit.
		// / 'Z' = ReadyForQuery；不是 'R'/'E'，不命中。
		_, _ = c.Write([]byte{'Z', 0, 0, 0, 0})
	})
	p := New()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if got := p.Identify(ctx, host, port); got != nil {
		t.Errorf("Identify with non-PG reply = %+v, want nil", got)
	}
}

// TestPostgresql_CredentialHit is skipped: the postgresql plugin's
// Credential is a documented no-op stub (always returns nil) per
// postgresql.go:48-51 — actual credential testing lives in
// core/cred/protocols/postgresql.go via PostgreSQLAuthenticator
// (lib/pq).
// / TestPostgresql_CredentialHit 跳过：postgresql 插件的
// Credential 是有文档说明的空 stub（始终返回 nil），见
// postgresql.go:48-51——真正的凭证测试在
// core/cred/protocols/postgresql.go（PostgreSQLAuthenticator，
// lib/pq）。
func TestPostgresql_CredentialHit(t *testing.T) {
	t.Skip("postgresql.Credential is a no-op stub; see postgresql.go Credential()")
}
