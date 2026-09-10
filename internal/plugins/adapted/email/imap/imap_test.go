// imap_test.go — A.6 coverage baseline via fakeserver.ListenLoop.
// imap_test.go — 通过 fakeserver.ListenLoop 的 A.6 覆盖率基线。
//
// Wire format being faked (imap.go:48-71):
//
//	plugin dials TCP and reads a single line
//	server replies "* OK [CAPABILITY IMAP4rev1] ready\r\n"
//	plugin returns Result{Service: "imap"} if line starts with "* OK"
//
// The Identify path is a single ReadString('\n') + HasPrefix check;
// no protocol negotiation is required for Identify. The fake server
// only needs to write the canonical greeting. / 仿真的线协议格式
// (imap.go:48-71)：插件拨 TCP 并读一行；server 回 "* OK
// [CAPABILITY IMAP4rev1] ready\r\n"；插件在线以 "* OK" 开头时返
// Result{Service: "imap"}。Identify 路径只需 ReadString('\n') +
// HasPrefix 检查，识别不需协议协商。假 server 只需写规范 greeting。
package imap

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/fakeserver"
)

// imapGreetingResponder writes the canonical "* OK [CAPABILITY
// IMAP4rev1] ready\r\n" banner the IMAP Identify parser expects
// (imap.go:64 — strings.HasPrefix(line, "* OK")). / imapGreetingResponder
// 写 IMAP Identify 解析器期望的规范 "* OK [CAPABILITY IMAP4rev1]
// ready\r\n" banner。
func imapGreetingResponder(c net.Conn) {
	// IMAP Identify is server-greeting-first: the client dials and
	// reads the banner without writing anything. We must write
	// FIRST, then drain any stray client bytes (none expected,
	// but tolerated so the kernel doesn't RST on close). / IMAP
	// Identify 是 server-greeting-first：客户端拨入并读 banner
	// 不写任何东西。我们必须先写，再排空任何多余的客户端字节
	// （预期为空，但容错避免内核在关时 RST）。
	_, _ = c.Write([]byte("* OK [CAPABILITY IMAP4rev1] ready\r\n"))
	fakeserver.Discard(c)
}

// TestImap_IdentifyHit spins up a TCP fake server that answers
// "* OK [CAPABILITY IMAP4rev1] ready\r\n". The plugin must identify
// the service as "imap" and the banner must reference the greeting.
// / 启 TCP 假 server 回 "* OK [CAPABILITY IMAP4rev1] ready\r\n"。
// 插件应识别服务为 "imap"，banner 必须引用 greeting。
func TestImap_IdentifyHit(t *testing.T) {
	host, port := fakeserver.ListenLoop(t, imapGreetingResponder)
	p := New()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	r := p.Identify(ctx, host, port)
	if r == nil {
		t.Fatal("Identify returned nil for valid * OK greeting")
	}
	if r.Service != "imap" {
		t.Errorf("Service = %q, want %q", r.Service, "imap")
	}
	if !strings.Contains(r.Banner, "* OK") || !strings.Contains(r.Banner, "IMAP4rev1") {
		t.Errorf("Banner = %q, want substring %q and %q", r.Banner, "* OK", "IMAP4rev1")
	}
	if r.Host != host || r.Port != port {
		t.Errorf("Host/Port = %q:%d, want %q:%d", r.Host, r.Port, host, port)
	}
}

// TestImap_IdentifyMiss covers the negative case: the server replies
// with a non-"* OK" line (e.g. a "* BYE" or a generic banner). The
// plugin must return nil so we don't false-positive on a non-IMAP
// service. / 验证反向 case：server 返非 "* OK" 行（如 "* BYE" 或
// 通用 banner）。插件必须返 nil，避免在非 IMAP 服务上假阳性。
func TestImap_IdentifyMiss(t *testing.T) {
	host, port := fakeserver.ListenLoop(t, func(c net.Conn) {
		_, _ = c.Write([]byte("* BYE not an IMAP server\r\n"))
		fakeserver.Discard(c)
	})
	p := New()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if got := p.Identify(ctx, host, port); got != nil {
		t.Errorf("Identify with * BYE reply = %+v, want nil", got)
	}
}

// TestImap_CredentialHit is skipped: the imap plugin's Credential is
// a no-op stub (always returns nil) per imap.go:43-46 — actual
// credential testing lives in core/cred/protocols/imap.go
// (IMAPAuthenticator, RFC 3501 LOGIN command). / TestImap_CredentialHit
// 跳过：imap 插件的 Credential 是空 stub（始终返回 nil），见
// imap.go:43-46——真正的凭证测试在 core/cred/protocols/imap.go
// （IMAPAuthenticator，RFC 3501 LOGIN 命令）。
func TestImap_CredentialHit(t *testing.T) {
	t.Skip("imap.Credential is a no-op stub; see imap.go Credential()")
}
