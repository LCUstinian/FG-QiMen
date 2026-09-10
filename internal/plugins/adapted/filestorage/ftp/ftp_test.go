// ftp_test.go — fake-server test for the ftp Identify plugin.
//
// Pattern: fakeserver.ListenLoop binds 127.0.0.1:0, the handler
// speaks RFC 959 220 greeting, the plugin's Identify reads the
// greeting and classifies the server banner.
//
// / 用 fakeserver.ListenLoop 在 127.0.0.1:0 起 TCP 监听；handler
// 按 RFC 959 发 220 greeting；plugin Identify 读 greeting 并分类
// server banner。
package ftp

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/fakeserver"
)

// TestFtp_IdentifyHit covers the single-line 220 happy path: server
// speaks first on connect (no client bytes required), plugin reads
// the line and returns a *types.Result with Service=="ftp". /
// TestFtp_IdentifyHit 覆盖单行 220 happy path：connect 后 server
// 先发 greeting；plugin 读该行并返回 Service=="ftp"。
func TestFtp_IdentifyHit(t *testing.T) {
	host, port := fakeserver.ListenLoop(t, func(c net.Conn) {
		// Per RFC 959 the server speaks first on connect; nothing
		// to read. Send a single-line 220 greeting whose banner
		// classifies as vsftpd. / 按 RFC 959 server 在 connect 后
		// 先发 greeting；不需要读 client。发一行 220 greeting，
		// banner 被分类为 vsftpd。
		_, _ = c.Write([]byte("220 (vsftpd 3.0.3) FTP ready\r\n"))
	})

	p := New()
	res := p.Identify(context.Background(), host, port)
	if res == nil {
		t.Fatalf("Identify returned nil; expected vsftpd hit")
	}
	if res.Service != "ftp" {
		t.Errorf("Service = %q, want %q", res.Service, "ftp")
	}
	if !strings.Contains(res.Banner, "vsftpd") {
		t.Errorf("Banner = %q, want contains %q", res.Banner, "vsftpd")
	}
	if res.Host != host || res.Port != port {
		t.Errorf("Host/Port = %q/%d, want %q/%d", res.Host, res.Port, host, port)
	}
}

// TestFtp_ClassifyBranches drives every branch of classify() via a
// targeted single-line banner. / TestFtp_ClassifyBranches 通过定向
// 单行 banner 跑 classify() 的每个分支。
func TestFtp_ClassifyBranches(t *testing.T) {
	cases := []struct {
		banner  string
		contain string
	}{
		{"220 (vsftpd 3.0.3)\r\n", "vsftpd"},
		{"220 ProFTPD 1.3.5 Server\r\n", "ProFTPD"},
		{"220 Pure-FTPd 1.0.49\r\n", "Pure-FTPd"},
		{"220 FileZilla Server 0.9\r\n", "FileZilla"},
		{"220 Microsoft FTP Service\r\n", "IIS-FTP"},
		{"220 WU-FTPD 2.6.2\r\n", "wu-ftpd"},
		{"220 Serv-U FTP Server\r\n", "Serv-U"},
		{"220 Some Unknown Banner\r\n", "generic"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(strings.TrimSpace(tc.banner), func(t *testing.T) {
			host, port := fakeserver.ListenLoop(t, func(c net.Conn) {
				_, _ = c.Write([]byte(tc.banner))
			})
			res := New().Identify(context.Background(), host, port)
			if res == nil {
				t.Fatalf("Identify returned nil for banner %q", tc.banner)
			}
			if !strings.Contains(res.Banner, tc.contain) {
				t.Errorf("Banner = %q, want contains %q", res.Banner, tc.contain)
			}
		})
	}
}

// TestFtp_MultiLineGreeting exercises the 220- ... 220 continuation
// branch: plugin must keep reading until a line starting with "220 "
// (the final line of the multi-line greeting). /
// TestFtp_MultiLineGreeting 跑 220-... 220 续行分支：plugin 必须继
// 续读到以 "220 " 开头的行（多行 greeting 的最后一行）。
func TestFtp_MultiLineGreeting(t *testing.T) {
	host, port := fakeserver.ListenLoop(t, func(c net.Conn) {
		_, _ = c.Write([]byte("220-Welcome to the test FTP server.\r\n" +
			"220 Service closing connection.\r\n"))
	})

	res := New().Identify(context.Background(), host, port)
	if res == nil {
		t.Fatalf("Identify returned nil; expected multi-line hit")
	}
	if !strings.Contains(res.Banner, "Service closing connection") {
		t.Errorf("Banner = %q, want contains final line", res.Banner)
	}
}

// TestFtp_IdentifyMiss covers the negative branch: server greets
// with something that doesn't start with "220-" or "220 ". The
// plugin must return nil. / TestFtp_IdentifyMiss 覆盖反向分支：
// server greeting 不以 "220-" 或 "220 " 开头，plugin 必须返回
// nil。
func TestFtp_IdentifyMiss(t *testing.T) {
	host, port := fakeserver.ListenLoop(t, func(c net.Conn) {
		// 500 is not FTP — plugin should bail. / 500 不是 FTP 协议
		// 的合法 greeting；plugin 应直接返回 nil。
		_, _ = c.Write([]byte("500 Not an FTP server\r\n"))
	})

	if res := New().Identify(context.Background(), host, port); res != nil {
		t.Errorf("Identify = %+v, want nil for non-FTP greeting", res)
	}
}

// TestFtp_CredentialNoOp documents that the plugin's Credential is
// a documented no-op stub (see ftp.go) — FTP credential testing
// lives in internal/core/credential/auth/remote/ftp.go. Per the
// v0.6.0 fake-server plan we still wire a placeholder so the
// handler protocol bytes (USER 331 / PASS 230) are covered if a
// future contributor promotes the stub to a real implementation.
//
// / TestFtp_CredentialNoOp 标注 plugin 的 Credential 是文档化的
// no-op stub（见 ftp.go）；FTP 凭据测试在
// internal/core/credential/auth/remote/ftp.go。按 v0.6.0 fake-
// server 计划仍保留占位，方便未来贡献者将 stub 升级成真实实现
// 时补齐 USER 331 / PASS 230 的 handler 字节。
func TestFtp_CredentialNoOp(t *testing.T) {
	t.Skip("Credential is a documented no-op stub in ftp.go (L46-51); " +
		"FTP credential testing lives in internal/core/credential/auth/remote.")
}

// TestFtp_DialFailure exercises the dial-error branch in
// RawTCPIdentify: an unroutable address must yield nil without
// blocking past the caller's context deadline. /
// TestFtp_DialFailure 跑 RawTCPIdentify 的拨号失败分支：不可达
// 地址必须在 caller 的 context deadline 内返回 nil。
func TestFtp_DialFailure(t *testing.T) {
	// 192.0.2.1 is RFC 5737 TEST-NET-1 — guaranteed unroutable.
	// / 192.0.2.1 是 RFC 5737 TEST-NET-1，保证不可达。
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	if res := New().Identify(ctx, "192.0.2.1", 21); res != nil {
		t.Errorf("Identify(unroutable) = %+v, want nil", res)
	}
}
