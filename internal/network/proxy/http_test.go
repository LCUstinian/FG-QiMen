// http_test.go — in-process fake HTTP CONNECT proxy + HTTPDialer round-trip.
//
// http_test.go — 进程内假 HTTP CONNECT 代理 + HTTPDialer 往返测试。
package proxy

import (
	"bufio"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

// fakeHTTPProxy is a minimal HTTP CONNECT proxy. On connect it
// reads the CONNECT line + headers, optionally validates
// Proxy-Authorization, and replies 200 to "tunnel" — after which
// it byte-echoes.
//
// fakeHTTPProxy 是最小化 HTTP CONNECT 代理。连接后读 CONNECT 行 +
// 头,可选地校验 Proxy-Authorization,响应 200 "建立隧道"——之后
// 字节回显。
type fakeHTTPProxy struct {
	ln              net.Listener
	expectBasicAuth string // "user:pass" or "" for none
}

func startFakeHTTPProxy(t *testing.T, expectBasicAuth string) *fakeHTTPProxy {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := &fakeHTTPProxy{ln: ln, expectBasicAuth: expectBasicAuth}
	go srv.serve(t)
	t.Cleanup(func() { _ = ln.Close() })
	return srv
}

func (s *fakeHTTPProxy) addr() string { return s.ln.Addr().String() }

func (s *fakeHTTPProxy) serve(t *testing.T) {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.handle(t, conn)
	}
}

func (s *fakeHTTPProxy) handle(t *testing.T, c net.Conn) {
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	br := bufio.NewReader(c)

	// Read request line + headers until blank line.
	// 读请求行 + 头直到空行。
	reqLine, err := br.ReadString('\n')
	if err != nil {
		return
	}
	if !strings.HasPrefix(reqLine, "CONNECT ") {
		fmt.Fprintf(c, "HTTP/1.1 405 Method Not Allowed\r\n\r\n")
		return
	}
	var sawAuth bool
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		if strings.HasPrefix(strings.ToLower(line), "proxy-authorization: basic ") {
			sawAuth = true
			encoded := strings.TrimSpace(line[len("Proxy-Authorization: Basic "):])
			decoded, _ := base64.StdEncoding.DecodeString(encoded)
			if s.expectBasicAuth != "" && string(decoded) != s.expectBasicAuth {
				fmt.Fprintf(c, "HTTP/1.1 407 Proxy Auth Required\r\n\r\n")
				return
			}
		}
	}

	if s.expectBasicAuth != "" && !sawAuth {
		fmt.Fprintf(c, "HTTP/1.1 407 Proxy Auth Required\r\n\r\n")
		return
	}

	// Send 200 — the "tunnel" is now established.
	// 发送 200 —— "隧道"建立。
	if _, err := fmt.Fprintf(c, "HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		return
	}

	// Now byte-echo whatever follows on either side. We do a
	// simultaneous copy in this goroutine — read from c, write
	// back to c. The bufio.Reader has already consumed up to the
	// blank line, so further reads on the underlying conn are
	// unbuffered (well, buffered by net.Conn's read buffer).
	//
	// 之后双向字节回显。我们在本 goroutine 做同时复制——从 c 读,
	// 写回 c。bufio.Reader 已读到空行为止,后续对底层 conn 的读
	// 不再缓冲（好吧,被 net.Conn 读缓冲）。
	buf := make([]byte, 4096)
	for {
		n, err := c.Read(buf)
		if n > 0 {
			if _, werr := c.Write(buf[:n]); werr != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}

func TestHTTPProxy_NoAuth(t *testing.T) {
	srv := startFakeHTTPProxy(t, "")
	dialer, err := NewHTTPDialer(&ProxyConfig{
		Type: ProxyTypeHTTP, Address: srv.addr(), Timeout: 2 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewHTTPDialer: %v", err)
	}
	conn, err := dialer.DialContext(testCtx(t), "tcp", "10.0.0.1:80")
	if err != nil {
		t.Fatalf("DialContext: %v", err)
	}
	defer conn.Close()

	if _, err := conn.Write([]byte("ping")); err != nil {
		t.Fatalf("write: %v", err)
	}
	buf := make([]byte, 4)
	if _, err := conn.Read(buf); err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(buf) != "ping" {
		t.Errorf("echo got %q, want ping", buf)
	}
}

func TestHTTPProxy_BasicAuth(t *testing.T) {
	srv := startFakeHTTPProxy(t, "alice:secret")
	dialer, _ := NewHTTPDialer(&ProxyConfig{
		Type: ProxyTypeHTTP, Address: srv.addr(),
		Username: "alice", Password: "secret", Timeout: 2 * time.Second,
	})
	conn, err := dialer.DialContext(testCtx(t), "tcp", "10.0.0.1:80")
	if err != nil {
		t.Fatalf("DialContext: %v", err)
	}
	_ = conn.Close()
}

func TestHTTPProxy_BasicAuthRejected(t *testing.T) {
	srv := startFakeHTTPProxy(t, "alice:secret")
	dialer, _ := NewHTTPDialer(&ProxyConfig{
		Type: ProxyTypeHTTP, Address: srv.addr(),
		Username: "alice", Password: "WRONG", Timeout: 2 * time.Second,
	})
	_, err := dialer.DialContext(testCtx(t), "tcp", "10.0.0.1:80")
	if err == nil {
		t.Fatal("DialContext succeeded with bad password")
	}
}

func TestHTTPProxy_BadStatus(t *testing.T) {
	// Server returns 500 instead of 200. / 服务器返回 500 而非 200。
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	go func() {
		c, _ := ln.Accept()
		defer c.Close()
		_ = c.SetDeadline(time.Now().Add(3 * time.Second))
		br := bufio.NewReader(c)
		_, _ = br.ReadString('\n')
		for {
			line, _ := br.ReadString('\n')
			if strings.TrimRight(line, "\r\n") == "" {
				break
			}
		}
		fmt.Fprintf(c, "HTTP/1.1 500 Server Error\r\n\r\n")
	}()
	dialer, _ := NewHTTPDialer(&ProxyConfig{
		Type: ProxyTypeHTTP, Address: ln.Addr().String(), Timeout: 2 * time.Second,
	})
	_, err = dialer.DialContext(testCtx(t), "tcp", "10.0.0.1:80")
	if err == nil {
		t.Fatal("DialContext succeeded despite 500 from proxy")
	}
}

func TestHTTPProxy_ConnectFailure(t *testing.T) {
	// Proxy server is unreachable. / 代理服务器不可达。
	dialer, _ := NewHTTPDialer(&ProxyConfig{
		Type: ProxyTypeHTTP, Address: "192.0.2.1:9", Timeout: 200 * time.Millisecond,
	})
	_, err := dialer.DialContext(testCtx(t), "tcp", "10.0.0.1:80")
	if err == nil {
		t.Fatal("DialContext succeeded with unreachable proxy")
	}
}

func TestHTTPProxy_HTTPSUpgrade(t *testing.T) {
	// Stand up an HTTPS server with a self-signed cert, then have
	// HTTPDialer connect to it via the proxy. The dialer should:
	//   1. Connect TCP to proxy
	//   2. Speak CONNECT
	//   3. Upgrade to TLS
	// We use transport.TLSConfig(false) to make verification fail,
	// but that path is still exercised. Easier path: just verify
	// the dialer doesn't panic on HTTPS config and the proxy
	// request happens (look at the bytes the proxy saw).
	//
	// 起一个自签证书的 HTTPS 服务,然后让 HTTPDialer 通过代理连接。
	// dialer 应: 1) TCP 连接到代理 2) 讲 CONNECT 3) 升级到 TLS
	// 我们用 transport.TLSConfig(false) 让验证失败,但代码路径仍
	// 跑过。更简易:仅验证 dialer 对 HTTPS 配置不 panic 且代理
	// 请求确实发生(看代理收到的字节)。
	//
	// For the test, we just check the dialer attempts TLS upgrade
	// (proxy will see CONNECT, then TLS ClientHello). To keep the
	// test simple, we use the regular HTTP path and switch types
	// to HTTPS — but then the proxy upgrade fails. So instead, we
	// set the proxy type to HTTPS and expect the dialer to either
	// succeed (if our fake is HTTPS) or fail with TLS handshake.
	//
	// 为简化,设代理 type 为 HTTPS,期待 dialer 要么成功(若假
	// 服务器是 HTTPS)要么 TLS 握手失败。
	srv := startFakeHTTPProxy(t, "")
	dialer, _ := NewHTTPDialer(&ProxyConfig{
		Type: ProxyTypeHTTPS, Address: srv.addr(), Timeout: 2 * time.Second,
	})
	_, err := dialer.DialContext(testCtx(t), "tcp", "10.0.0.1:80")
	// We expect an error because the fake HTTP proxy doesn't speak
	// TLS — but the path through `if d.config.Type == ProxyTypeHTTPS`
	// is exercised, which is what we want.
	if err == nil {
		t.Fatal("DialContext unexpectedly succeeded (TLS upgrade over plain HTTP proxy should fail)")
	}
	// Confirm we got past dial + TLS-attempt and only failed on
	// the TLS handshake.
	//
	// 确认 dial + TLS 尝试都跑了,只在 TLS 握手失败。
	if !strings.Contains(err.Error(), "TLS handshake") &&
		!strings.Contains(err.Error(), "first record does not look like a TLS handshake") {
		t.Errorf("error doesn't look like a TLS handshake failure: %v", err)
	}
}

// Compile-time check that x509 is still used (some imports may be
// pruned by linters; this keeps the import honest).
//
// 编译时检查 x509 仍被使用（linter 可能裁剪掉某些 import;这让 import
// 保持诚实）。
var _ = x509.MarshalPKCS8PrivateKey
var _ = tls.Config{}
var _ = http.StatusOK
var _ = net.Conn(nil)
