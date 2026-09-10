// smtp_test.go — fake-server test for the SMTP Identify plugin.
//
// smtp_test.go — SMTP 识别插件的 fake-server 测试。
//
// The plugin uses net/smtp.NewClient to do the EHLO probe, so the
// fake server only needs to:
//  1. greet with a single 220 line,
//  2. drain the EHLO command the client sends,
//  3. reply with a valid 250 OK (single line — net/smtp's
//     readResponse accepts a bare "250 OK\r\n" as the end of a
//     multi-line group, since the code is followed by a space not
//     a hyphen).
//
// Plugin's Credential is a documented no-op stub (see source
// comment "Credential is a no-op stub.") so we skip that test
// rather than fabricate protocol bytes it never consumes.
// 插件的 Credential 是文档化的 no-op stub（见源码注释
// "Credential is a no-op stub."），所以我们跳过那个测试，不
// 为它构造永远不会被消费的协议字节。
//
// Coverage note: this file targets the happy paths (greeting +
// EHLO accepted, EHLO refused, connection refused). The trim()
// helper at the bottom of smtp.go is only reachable via the
// defensive fallback path that triggers when net/smtp.NewClient
// itself fails on a 220-shaped line — exercising it reliably
// would require sending a line longer than net/textproto's 65536-
// byte default limit, which is a fragile test of stdlib internals
// rather than plugin behaviour. Documented and shipped below 70%
// per the v0.6.0 "complex protocols" exception in the plan.
// / 覆盖说明：本文件瞄准快乐路径（接受问候 + EHLO、被拒 EHLO、
// 被拒连接）。smtp.go 底部的 trim() 助手只能通过防御性 fallback
// 路径触达——只有当 net/smtp.NewClient 在一个 220 形式的行上
// 失败时才会执行。可靠地触发它需要发送超过 net/textproto 默认
// 65536 字节行限制的行，那是对 stdlib 内部细节而非插件行为的脆
// 弱测试。按 v0.6.0 计划中"复杂协议"的例外条款，低于 70% 仍
// 接受并提交。
package smtp

import (
	"bufio"
	"context"
	"net"
	"testing"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/fakeserver"
)

// TestSmtp_IdentifyHit verifies the plugin accepts a minimal
// SMTP greeting + EHLO reply and returns a Result with Service
// "smtp". / 验证插件接受最小 SMTP 问候 + EHLO 回复并返回
// Service="smtp" 的 Result。
func TestSmtp_IdentifyHit(t *testing.T) {
	host, port := fakeserver.ListenLoop(t, func(c net.Conn) {
		_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
		br := bufio.NewReader(c)
		// 1) Greet with the 220 line net/smtp.NewClient expects.
		// / 1) net/smtp.NewClient 期望的 220 问候。
		if _, err := c.Write([]byte("220 fakeserver ESMTP\r\n")); err != nil {
			return
		}
		// 2) Drain the EHLO command the client writes.
		//    We don't need to parse it — just consume the bytes
		//    so the client's Write doesn't block. / 2) 把客户端
		//    发的 EHLO 命令读完。无需解析——只是把字节消耗掉避免
		//    客户端 Write 阻塞。
		_, _, _ = br.ReadLine()
		// 3) Reply 250 OK to the EHLO. Single line is enough —
		//    net/smtp's readResponse terminates on "250 " (space)
		//    instead of "250-" (hyphen). / 3) 回 250 OK 给 EHLO。
		//    单行就够——net/smtp 的 readResponse 在 "250 "（空格）
		//    而不是 "250-"（连字符）终止。
		_, _ = c.Write([]byte("250 OK\r\n"))
	})
	p := New()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	r := p.Identify(ctx, host, port)
	if r == nil {
		t.Fatal("expected hit, got nil")
	}
	if r.Service != "smtp" {
		t.Errorf("Service = %q, want %q", r.Service, "smtp")
	}
}

// TestSmtp_IdentifyEHLORefused verifies that when the server
// accepts the greeting but rejects the EHLO with a non-250 code,
// the plugin returns nil (it does NOT report a non-cooperative
// SMTP server as smtp). / 验证当 server 接受问候但以非 250 码
// 拒绝 EHLO 时，插件返 nil（不会把不配合的 SMTP server 报成
// smtp）。
func TestSmtp_IdentifyEHLORefused(t *testing.T) {
	host, port := fakeserver.ListenLoop(t, func(c net.Conn) {
		_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
		br := bufio.NewReader(c)
		if _, err := c.Write([]byte("220 fakeserver ESMTP\r\n")); err != nil {
			return
		}
		// Drain EHLO. / 把 EHLO 读完。
		_, _, _ = br.ReadLine()
		// Reply 421 — service not available, transient failure.
		// c.Hello propagates this as an error and the plugin
		// returns nil. / 回 421 — service not available，临时
		// 错误。c.Hello 把这个当错误往上抛，插件返 nil。
		_, _ = c.Write([]byte("421 Service not available\r\n"))
	})
	p := New()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if r := p.Identify(ctx, host, port); r != nil {
		t.Errorf("expected nil on EHLO refused, got %+v", r)
	}
}

// TestSmtp_IdentifyConnRefused verifies the plugin returns nil
// when the dial fails outright (no listener on the port). /
// 验证当拨号直接失败（端口无监听）时，插件返 nil。
func TestSmtp_IdentifyConnRefused(t *testing.T) {
	// Bind a listener just to grab a free port, then close it so
	// the subsequent dial gets connection-refused. / 绑一个
	// listener 仅为了拿一个空闲端口，然后关掉让后续拨号收到
	// connection-refused。
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	p := New()
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	if r := p.Identify(ctx, "127.0.0.1", port); r != nil {
		t.Errorf("expected nil on conn refused, got %+v", r)
	}
}

// TestSmtp_CredentialHit is skipped — Credential is a documented
// no-op stub in smtp.go (always returns nil). See the comment
// above the Plugin.Credential method in the source.
// / TestSmtp_CredentialHit 跳过——Credential 在 smtp.go 里是文档化
// 的 no-op stub（永远返 nil）。见源码中 Plugin.Credential 方法
// 上方的注释。
func TestSmtp_CredentialHit(t *testing.T) {
	t.Skip("Credential is a no-op stub (see smtp.go Plugin.Credential)")
}
