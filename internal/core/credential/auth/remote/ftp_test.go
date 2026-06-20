// ftp_test.go — unit tests for the FTP authenticator.
//
// ftp_test.go — FTP 认证器的单元测试。
//
// Mirrors the memcached_test pattern: a tiny in-process FTP server
// that responds to USER/PASS/QUIT with RFC 959 codes, so the
// authenticator's real connect+login path is exercised without
// needing an external FTP server.
//
// 镜像 memcached_test 模式：一个进程内的假 FTP 服务，按 RFC 959
// 响应 USER/PASS/QUIT，让认证器的真实 connect+login 路径在不需要
// 外部 FTP 服务的情况下被覆盖。
package remote

import (
	"bufio"
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/core/credential"
)

// startFakeFTP starts a minimal RFC 959 server on 127.0.0.1.
// requirePass is the password that will succeed; everything else fails.
//
//   - 220 <welcome> on connect
//   - USER <u> → 331 Password required
//   - PASS <p> → 230 (if p==requirePass) else 530
//   - QUIT     → 221 Goodbye
//   - anything else → 500 Unknown command
//
// startFakeFTP 启动一个最小化的 RFC 959 服务在 127.0.0.1。requirePass
// 是会成功的密码，其他都失败。
func startFakeFTP(t *testing.T, requirePass string) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go handleFakeFTP(c, requirePass)
		}
	}()
	t.Cleanup(func() { _ = ln.Close() })
	return ln
}

func handleFakeFTP(c net.Conn, requirePass string) {
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	br := bufio.NewReader(c)
	// Greeting. / 问候。
	_, _ = c.Write([]byte("220 FG-QiMen fake FTP ready\r\n"))
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return
		}
		cmd := strings.TrimSpace(strings.ToUpper(line))
		switch {
		case strings.HasPrefix(cmd, "USER"):
			_, _ = c.Write([]byte("331 Password required\r\n"))
		case strings.HasPrefix(cmd, "PASS"):
			pass := strings.TrimSpace(line[4:])
			if pass == requirePass {
				_, _ = c.Write([]byte("230 Login successful\r\n"))
			} else {
				_, _ = c.Write([]byte("530 Login incorrect\r\n"))
			}
		case strings.HasPrefix(cmd, "QUIT"):
			_, _ = c.Write([]byte("221 Goodbye\r\n"))
			return
		case strings.HasPrefix(cmd, "TYPE"), strings.HasPrefix(cmd, "EPSV"),
			strings.HasPrefix(cmd, "FEAT"):
			// jlaffaye/ftp sends these pre-login. Reply with a
			// benign response so the client proceeds to USER.
			// jlaffaye/ftp 在登录前会发这些。返回良性响应让
			// 客户端继续走 USER。
			_, _ = c.Write([]byte("200 OK\r\n"))
		default:
			_, _ = c.Write([]byte("500 Unknown command\r\n"))
		}
	}
}

// TestFTP_NoCreds verifies the empty-creds short-circuit returns nil.
// / TestFTP_NoCreds 验证空凭据短路返回 nil。
func TestFTP_NoCreds(t *testing.T) {
	auth := NewFTPAuthenticator()
	hit, err := auth.Authenticate(context.Background(), "127.0.0.1", 21, nil, time.Second)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if hit != nil {
		t.Errorf("expected nil for empty creds, got %+v", hit)
	}
}

// TestFTP_HitWithCorrectPassword verifies a hit on the first matching
// cred. The first cred has the wrong password; the second matches.
// / TestFTP_HitWithCorrectPassword 验证首个匹配的 cred 命中。第一个
// 凭据密码错；第二个匹配。
func TestFTP_HitWithCorrectPassword(t *testing.T) {
	ln := startFakeFTP(t, "secret")
	auth := NewFTPAuthenticator()
	addr := ln.Addr().(*net.TCPAddr)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	creds := []credential.Cred{
		{User: "admin", Pass: "wrong", Method: credential.AuthPassword},
		{User: "admin", Pass: "secret", Method: credential.AuthPassword},
		// A later cred that should NOT be tried once we have a hit.
		// / 后面的凭据在命中后不应再尝试。
		{User: "admin", Pass: "later", Method: credential.AuthPassword},
	}
	hit, err := auth.Authenticate(ctx, addr.IP.String(), addr.Port, creds, time.Second)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if hit == nil {
		t.Fatal("expected hit")
	}
	if hit.Cred.Pass != "secret" {
		t.Errorf("expected pass=secret, got %q", hit.Cred.Pass)
	}
	if hit.Attempts != 2 {
		t.Errorf("expected attempts=2, got %d", hit.Attempts)
	}
}

// TestFTP_MissAll verifies all-wrong creds return nil with no error.
// / TestFTP_MissAll 验证全错凭据返回 nil 且无错误。
func TestFTP_MissAll(t *testing.T) {
	ln := startFakeFTP(t, "right")
	auth := NewFTPAuthenticator()
	addr := ln.Addr().(*net.TCPAddr)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	creds := []credential.Cred{
		{User: "admin", Pass: "wrong1", Method: credential.AuthPassword},
		{User: "admin", Pass: "wrong2", Method: credential.AuthPassword},
	}
	hit, err := auth.Authenticate(ctx, addr.IP.String(), addr.Port, creds, time.Second)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if hit != nil {
		t.Errorf("expected nil, got %+v", hit)
	}
}

// TestFTP_NotFTP verifies a server that doesn't speak FTP returns nil
// (no false positives). The authenticator should not panic, and the
// error should be surfaced (or hit should be nil) — never a "hit".
// / TestFTP_NotFTP 验证非 FTP 服务返回 nil（无假阳性）。认证器不
// 应 panic，错误应被暴露（或 hit 应为 nil）——绝不"命中"。
func TestFTP_NotFTP(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			// Reply with something that's not a 220 welcome.
			// / 用非 220 欢迎语回复。
			_ = c.SetWriteDeadline(time.Now().Add(2 * time.Second))
			_, _ = c.Write([]byte("HTTP/1.1 200 OK\r\n"))
			_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
			_, _ = c.Read(make([]byte, 1))
			_ = c.Close()
		}
	}()
	auth := NewFTPAuthenticator()
	addr := ln.Addr().(*net.TCPAddr)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	creds := []credential.Cred{{User: "u", Pass: "p", Method: credential.AuthPassword}}
	hit, err := auth.Authenticate(ctx, addr.IP.String(), addr.Port, creds, time.Second)
	// The exact behavior depends on jlaffaye/ftp's tolerance for
	// non-FTP banners, but we must NEVER report a hit.
	// / 确切行为取决于 jlaffaye/ftp 对非 FTP banner 的容差，但绝
	// 不应报告命中。
	if hit != nil {
		t.Errorf("expected nil for non-FTP server, got %+v", hit)
	}
	// err may be non-nil (e.g. "not an FTP server") — both are
	// acceptable as long as we didn't get a false hit.
	// / err 可能非 nil（如"非 FTP 服务"）——两者都可接受，只要
	// 没有假命中。
	_ = err
}
