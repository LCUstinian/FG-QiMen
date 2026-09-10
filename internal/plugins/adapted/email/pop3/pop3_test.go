// pop3_test.go — A.6 coverage baseline via fakeserver.ListenLoop.
// pop3_test.go — 通过 fakeserver.ListenLoop 的 A.6 覆盖率基线。
//
// Wire format being faked (pop3.go:46-67):
//
//	plugin dials TCP and reads a single line
//	server replies "+OK POP3 ready\r\n"
//	plugin returns Result{Service: "pop3"} if line starts with "+OK"
//
// The Identify path is a single ReadString('\n') + HasPrefix check;
// no protocol negotiation is required for Identify. The fake server
// only needs to write the canonical greeting. / 仿真的线协议格式
// (pop3.go:46-67)：插件拨 TCP 并读一行；server 回 "+OK POP3
// ready\r\n"；插件在线以 "+OK" 开头时返 Result{Service: "pop3"}。
// Identify 路径只需 ReadString('\n') + HasPrefix 检查，识别不需
// 协议协商。假 server 只需写规范 greeting。
package pop3

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/fakeserver"
)

// pop3GreetingResponder writes the canonical "+OK POP3 ready\r\n"
// banner the POP3 Identify parser expects (pop3.go:61 —
// strings.HasPrefix(line, "+OK")). / pop3GreetingResponder 写 POP3
// Identify 解析器期望的规范 "+OK POP3 ready\r\n" banner。
func pop3GreetingResponder(c net.Conn) {
	// POP3 Identify is server-greeting-first: the client dials and
	// reads the banner without writing anything. We must write
	// FIRST, then drain any stray client bytes (none expected,
	// but tolerated so the kernel doesn't RST on close).
	// / POP3 Identify 是 server-greeting-first：客户端拨入并读
	// banner 不写任何东西。我们必须先写，再排空任何多余的客户端
	// 字节（预期为空，但容错避免内核在关时 RST）。
	_, _ = c.Write([]byte("+OK POP3 ready\r\n"))
	fakeserver.Discard(c)
}

// TestPop3_IdentifyHit spins up a TCP fake server that answers
// "+OK POP3 ready\r\n". The plugin must identify the service as
// "pop3" and the banner must reference the greeting. / 启 TCP 假
// server 回 "+OK POP3 ready\r\n"。插件应识别服务为 "pop3"，
// banner 必须引用 greeting。
func TestPop3_IdentifyHit(t *testing.T) {
	host, port := fakeserver.ListenLoop(t, pop3GreetingResponder)
	p := New()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	r := p.Identify(ctx, host, port)
	if r == nil {
		t.Fatal("Identify returned nil for valid +OK greeting")
	}
	if r.Service != "pop3" {
		t.Errorf("Service = %q, want %q", r.Service, "pop3")
	}
	if !strings.Contains(r.Banner, "+OK") || !strings.Contains(r.Banner, "POP3 ready") {
		t.Errorf("Banner = %q, want substring %q and %q", r.Banner, "+OK", "POP3 ready")
	}
	if r.Host != host || r.Port != port {
		t.Errorf("Host/Port = %q:%d, want %q:%d", r.Host, r.Port, host, port)
	}
}

// TestPop3_IdentifyMiss covers the negative case: the server
// replies with an "-ERR" line (the POP3 error reply shape). The
// plugin must return nil so we don't false-positive on a non-POP3
// service or a POP3 server in an error state. / 验证反向 case：
// server 返 "-ERR" 行（POP3 错误响应形态）。插件必须返 nil，避免
// 在非 POP3 服务或处于错误状态的 POP3 server 上假阳性。
func TestPop3_IdentifyMiss(t *testing.T) {
	host, port := fakeserver.ListenLoop(t, func(c net.Conn) {
		_, _ = c.Write([]byte("-ERR not a POP3 server\r\n"))
		fakeserver.Discard(c)
	})
	p := New()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if got := p.Identify(ctx, host, port); got != nil {
		t.Errorf("Identify with -ERR reply = %+v, want nil", got)
	}
}

// TestPop3_CredentialHit is skipped: the pop3 plugin's Credential
// is a no-op stub (always returns nil) per pop3.go:40-43 — actual
// credential testing lives in core/cred/protocols/pop3.go.
// TestPop3_CredentialHit 跳过：pop3 插件的 Credential 是空 stub
// （始终返回 nil），见 pop3.go:40-43——真正的凭证测试在
// core/cred/protocols/pop3.go。
func TestPop3_CredentialHit(t *testing.T) {
	t.Skip("pop3.Credential is a no-op stub; see pop3.go Credential()")
}
