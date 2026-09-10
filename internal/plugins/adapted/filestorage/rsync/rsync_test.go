// rsync_test.go — fake-server test for the rsync Identify plugin.
//
// rsync_test.go — rsync 识别插件的 fake-server 测试。
//
// Pattern established in v0.6.0 fake-server coverage plan Tier 3
// (stateful TCP plugins). The plugin's Identify path is a
// single ReadString('\n') + HasPrefix check on the server greeting
// ("@RSYNCD: <ver>\n"). No client-driven handshake is needed —
// the server-greeting-first shape mirrors POP3 / IMAP / SMTP.
// / v0.6.0 假服务器覆盖率计划 Tier 3（有状态 TCP 插件）模式。
// 插件的 Identify 路径是对 server greeting
// （"@RSYNCD: <ver>\n"）做单次 ReadString('\n') + HasPrefix 检
// 查。不需要客户端驱动的握手——server-greeting-first 形态与
// POP3 / IMAP / SMTP 一致。
//
// Wire format being faked (rsync.go:48-71):
//
//	plugin dials TCP and reads a single line
//	server replies "@RSYNCD: 31.0\n"
//	plugin returns Result{Service: "rsync", Banner: "Rsync 31.0"}
//	  if line starts with "@RSYNCD:"
//
// Credential is a documented no-op stub (rsync.go:42-45 —
// "Credential is a no-op stub."), so we skip CredentialHit
// rather than fabricate MD5 challenge-response bytes the plugin
// never consumes. The real rsync auth flow lives in
// internal/core/credential/protocols/rsync.go (RsyncAuthenticator).
// / Credential 是文档化的 no-op stub（rsync.go:42-45 —
// "Credential 是空 stub。"），所以我们跳过 CredentialHit，不为
// 它构造 MD5 challenge-response 字节。真正的 rsync 认证流程在
// internal/core/credential/protocols/rsync.go（RsyncAuthenticator）。
package rsync

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/fakeserver"
)

// rsyncGreetingResponder writes the canonical "@RSYNCD: <ver>\n"
// banner the rsync Identify parser expects (rsync.go:63 —
// strings.HasPrefix(line, "@RSYNCD:")). / rsyncGreetingResponder
// 写 rsync Identify 解析器期望的规范 "@RSYNCD: <ver>\n" banner。
func rsyncGreetingResponder(c net.Conn) {
	// rsync Identify is server-greeting-first: the client dials
	// and reads the banner without writing anything. We must
	// write FIRST, then drain any stray client bytes (none
	// expected, but tolerated so the kernel doesn't RST on
	// close). / rsync Identify 是 server-greeting-first：客户端
	// 拨入并读 banner 不写任何东西。我们必须先写，再排空任何
	// 多余的客户端字节（预期为空，但容错避免内核在关时 RST）。
	_, _ = c.Write([]byte("@RSYNCD: 31.0\n"))
	fakeserver.Discard(c)
}

// TestRsync_IdentifyHit spins up a TCP fake server that answers
// "@RSYNCD: 31.0\n". The plugin must identify the service as
// "rsync" and the banner must surface the version reported by
// the greeting. / 启 TCP 假 server 回 "@RSYNCD: 31.0\n"。插件应
// 识别服务为 "rsync"，banner 必须反映 greeting 报告的版本。
func TestRsync_IdentifyHit(t *testing.T) {
	host, port := fakeserver.ListenLoop(t, rsyncGreetingResponder)
	p := New()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	r := p.Identify(ctx, host, port)
	if r == nil {
		t.Fatal("Identify returned nil for valid @RSYNCD: greeting")
	}
	if r.Service != "rsync" {
		t.Errorf("Service = %q, want %q", r.Service, "rsync")
	}
	if !strings.Contains(r.Banner, "31.0") {
		t.Errorf("Banner = %q, want substring %q", r.Banner, "31.0")
	}
	if !strings.HasPrefix(r.Banner, "Rsync ") {
		t.Errorf("Banner = %q, want prefix %q", r.Banner, "Rsync ")
	}
	if r.Host != host || r.Port != port {
		t.Errorf("Host/Port = %q:%d, want %q:%d", r.Host, r.Port, host, port)
	}
}

// TestRsync_IdentifyMiss covers the negative case: the server
// sends a non-rsync banner (plain HTTP). The plugin must return
// nil so we don't false-positive on a service that happens to
// listen on the rsync ports. / 验证反向 case：server 发送非 rsync
// banner（普通 HTTP）。插件必须返 nil，避免在恰好监听 rsync 端口
// 的非 rsync 服务上假阳性。
func TestRsync_IdentifyMiss(t *testing.T) {
	host, port := fakeserver.ListenLoop(t, func(c net.Conn) {
		_, _ = c.Write([]byte("HTTP/1.1 200 OK\r\n"))
		fakeserver.Discard(c)
	})
	p := New()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if got := p.Identify(ctx, host, port); got != nil {
		t.Errorf("Identify with HTTP banner = %+v, want nil", got)
	}
}

// TestRsync_IdentifyConnRefused verifies the plugin returns nil
// when the dial fails outright (no listener on the port). /
// 验证当拨号直接失败（端口无监听）时，插件返 nil。
func TestRsync_IdentifyConnRefused(t *testing.T) {
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

// TestRsync_CredentialHit is skipped: rsync.Credential is a
// documented no-op stub (see rsync.go:42-45 — "Credential is a
// no-op stub."). The real rsync credential flow (MD5
// challenge-response) lives in
// internal/core/credential/protocols/rsync.go.
// / TestRsync_CredentialHit 跳过：rsync.Credential 是文档化的
// no-op stub（见 rsync.go:42-45 — "Credential 空 stub。"）。真
// 正的 rsync 凭证流程（MD5 challenge-response）在
// internal/core/credential/protocols/rsync.go。
func TestRsync_CredentialHit(t *testing.T) {
	t.Skip("rsync.Credential is a no-op stub; see rsync.go Credential()")
}
