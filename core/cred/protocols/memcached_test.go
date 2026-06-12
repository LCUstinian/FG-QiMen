// memcached_test.go — unit tests for the memcached authenticator.
// memcached_test.go — memcached 认证器的单元测试。
package protocols_test

import (
	"bufio"
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/LCUstinian/FG-QiMen/core/cred"
	"github.com/LCUstinian/FG-QiMen/core/cred/protocols"
)

// startFakeMemcached starts a tiny in-process memcached. It supports
// `version` and `auth`. The `auth` command:
//   - if requirePass is empty → returns "" (empty reply, treated as no-auth).
//   - if requirePass matches the supplied pass → "OK".
//   - else → "CLIENT_ERROR invalid password".
//
// startFakeMemcached 启动一个进程内的假 memcached。支持 version 和 auth。
// auth 命令的行为：
//   - requirePass 为空 → 返回 ""（空响应，视为无 auth）。
//   - requirePass 匹配 pass → "OK"。
//   - 否则 → "CLIENT_ERROR invalid password"。
func startFakeMemcached(t *testing.T, requirePass string) net.Listener {
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
			go handleFakeMemcached(c, requirePass)
		}
	}()
	t.Cleanup(func() { _ = ln.Close() })
	return ln
}

func handleFakeMemcached(c net.Conn, requirePass string) {
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	br := bufio.NewReader(c)
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		switch strings.ToLower(fields[0]) {
		case "version":
			_, _ = c.Write([]byte("VERSION 1.6.0\r\n"))
		case "auth":
			// "auth [user] pass"
			// / "auth [user] pass"
			var pass string
			if len(fields) == 2 {
				pass = fields[1]
			} else if len(fields) >= 3 {
				pass = fields[2]
			}
			if requirePass == "" {
				// No-auth server. Reply empty (no \r\n). Some
				// memcached versions reply with STORED.
				// / 无 auth 服务。返回空响应（无 \r\n）。部分版本
				// 会回 STORED。
				_, _ = c.Write([]byte("\r\n"))
			} else if pass == requirePass {
				_, _ = c.Write([]byte("OK\r\n"))
			} else {
				_, _ = c.Write([]byte("CLIENT_ERROR invalid password\r\n"))
			}
		case "quit":
			return
		default:
			// Unknown command — keep server quiet.
			// / 未知命令——保持安静。
		}
	}
}

// TestMemcached_NoAuth verifies a server with no auth returns a hit
// on the first try. / TestMemcached_NoAuth 验证不需 auth 的服务
// 第一次就命中。
func TestMemcached_NoAuth(t *testing.T) {
	ln := startFakeMemcached(t, "")
	auth := protocols.NewMemcachedAuthenticator()
	addr := ln.Addr().(*net.TCPAddr)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	creds := []cred.Cred{{User: "", Pass: "any", Method: cred.AuthPassword}}
	hit, err := auth.Authenticate(ctx, addr.IP.String(), addr.Port, creds, time.Second)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if hit == nil {
		t.Fatal("expected hit on no-auth server")
	}
}

// TestMemcached_HitWithCorrectPassword verifies a hit when the
// password is correct. / TestMemcached_HitWithCorrectPassword 验证
// 密码对时命中。
func TestMemcached_HitWithCorrectPassword(t *testing.T) {
	ln := startFakeMemcached(t, "secret")
	auth := protocols.NewMemcachedAuthenticator()
	addr := ln.Addr().(*net.TCPAddr)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	creds := []cred.Cred{
		{User: "", Pass: "wrong", Method: cred.AuthPassword},
		{User: "", Pass: "secret", Method: cred.AuthPassword},
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
}

// TestMemcached_MissAll verifies that all-wrong creds return nil.
// / TestMemcached_MissAll 验证全错密码返回 nil。
func TestMemcached_MissAll(t *testing.T) {
	ln := startFakeMemcached(t, "right")
	auth := protocols.NewMemcachedAuthenticator()
	addr := ln.Addr().(*net.TCPAddr)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	creds := []cred.Cred{
		{User: "", Pass: "wrong1", Method: cred.AuthPassword},
		{User: "", Pass: "wrong2", Method: cred.AuthPassword},
	}
	hit, err := auth.Authenticate(ctx, addr.IP.String(), addr.Port, creds, time.Second)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if hit != nil {
		t.Errorf("expected nil, got %+v", hit)
	}
}

// TestMemcached_NotMemcached verifies a non-memcached server returns
// nil. / TestMemcached_NotMemcached 验证非 memcached 服务返回 nil。
func TestMemcached_NotMemcached(t *testing.T) {
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
			// Reply with "NO_VERSION" — not "VERSION ...". Wait
			// for the client to read before closing (avoids
			// WSAECONNABORTED on Windows). / 回 "NO_VERSION"——
			// 非 "VERSION ..."。等客户端读完再关（避免 Windows
			// WSAECONNABORTED）。
			_ = c.SetWriteDeadline(time.Now().Add(2 * time.Second))
			_, _ = c.Write([]byte("NO_VERSION\r\n"))
			_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
			_, _ = c.Read(make([]byte, 1))
			_ = c.Close()
		}
	}()
	auth := protocols.NewMemcachedAuthenticator()
	addr := ln.Addr().(*net.TCPAddr)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	creds := []cred.Cred{{User: "", Pass: "p", Method: cred.AuthPassword}}
	hit, err := auth.Authenticate(ctx, addr.IP.String(), addr.Port, creds, time.Second)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if hit != nil {
		t.Errorf("expected nil for non-memcached, got %+v", hit)
	}
}
