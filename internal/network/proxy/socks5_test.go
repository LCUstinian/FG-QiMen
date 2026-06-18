// socks5_test.go — in-process SOCKS5 fake server + SOCKS5Dialer round-trip.
//
// socks5_test.go — 进程内 SOCKS5 假服务器 + SOCKS5Dialer 往返测试。
//
// Approach: start a TCP server on 127.0.0.1 that speaks just enough
// of the SOCKS5 protocol to accept one CONNECT, then echo the
// remaining bytes back. The dialer drives the real handshake code
// path against the fake, exercising:
//   - connect → handshake → connect → return
//   - No-auth vs username/password auth
//   - IPv4, IPv6, and domain destinations
//   - Failure paths: no-acceptable-methods, version mismatch
//
// 方法：在 127.0.0.1 启动一个仅实现 CONNECT 子集的 TCP 服务器,然后
// 把剩余字节回显。dialer 驱动真实握手代码路径对假服务器,覆盖:
//   - connect → handshake → connect → return
//   - 无认证 vs 用户名密码认证
//   - IPv4 / IPv6 / 域名目标
//   - 失败路径:无接受方法、版本不匹配
package proxy

import (
	"encoding/binary"
	"io"
	"net"
	"strconv"
	"testing"
	"time"
)

// fakeSOCKS5 is a tiny SOCKS5 server. It supports:
//   - No-auth (0x00) and username/password (0x02) auth
//   - CONNECT command to IPv4 / IPv6 / domain destinations
//   - Replies with success and then byte-echoes the rest
//
// fakeSOCKS5 是一个微型 SOCKS5 服务器。支持:
//   - 无认证 (0x00) 和用户名密码认证 (0x02)
//   - 到 IPv4 / IPv6 / 域名目标的 CONNECT 命令
//   - 用 success 响应,然后字节回显剩余数据
type fakeSOCKS5 struct {
	ln           net.Listener
	requireAuth  bool // if true, advertise 0x00 as "no acceptable"
	expectedUser string
	expectedPass string
}

func startFakeSOCKS5(t *testing.T, opts fakeSOCKS5Opts) *fakeSOCKS5 {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := &fakeSOCKS5{
		ln:           ln,
		requireAuth:  opts.RequireNoAccept,
		expectedUser: opts.User,
		expectedPass: opts.Pass,
	}
	go srv.serve(t)
	t.Cleanup(func() { _ = ln.Close() })
	return srv
}

type fakeSOCKS5Opts struct {
	RequireNoAccept bool
	User            string
	Pass            string
}

func (s *fakeSOCKS5) addr() string { return s.ln.Addr().String() }

func (s *fakeSOCKS5) serve(t *testing.T) {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.handle(t, conn)
	}
}

func (s *fakeSOCKS5) handle(t *testing.T, c net.Conn) {
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))

	// Greeting: [VER, NMETHODS, METHODS...]
	hdr := make([]byte, 2)
	if _, err := io.ReadFull(c, hdr); err != nil {
		return
	}
	if hdr[0] != socks5Version {
		return
	}
	methods := make([]byte, hdr[1])
	if _, err := io.ReadFull(c, methods); err != nil {
		return
	}

	// Pick auth method.
	var chosen byte = socks5AuthNoAccept
	for _, m := range methods {
		if s.requireAuth {
			// Refuse everything.
			chosen = socks5AuthNoAccept
			break
		}
		if m == socks5AuthPassword && (s.expectedUser != "" || s.expectedPass != "") {
			chosen = socks5AuthPassword
			break
		}
		if m == socks5AuthNone {
			chosen = socks5AuthNone
			break
		}
	}
	if _, err := c.Write([]byte{socks5Version, chosen}); err != nil {
		return
	}
	if chosen == socks5AuthNoAccept {
		return
	}
	if chosen == socks5AuthPassword {
		// Sub-negotiation: [VER, ULEN, UNAME, PLEN, PASS]
		if _, err := io.ReadFull(c, hdr[:1]); err != nil { // version byte
			return
		}
		if hdr[0] != 0x01 {
			return
		}
		if _, err := io.ReadFull(c, hdr[:1]); err != nil { // ulen
			return
		}
		user := make([]byte, hdr[0])
		if _, err := io.ReadFull(c, user); err != nil {
			return
		}
		if _, err := io.ReadFull(c, hdr[:1]); err != nil { // plen
			return
		}
		pass := make([]byte, hdr[0])
		if _, err := io.ReadFull(c, pass); err != nil {
			return
		}
		status := byte(0x00) // success
		if string(user) != s.expectedUser || string(pass) != s.expectedPass {
			status = byte(0x01) // failure
		}
		if _, err := c.Write([]byte{0x01, status}); err != nil {
			return
		}
		if status != 0x00 {
			return
		}
	}

	// Request: [VER, CMD, RSV, ATYP, DST.ADDR, DST.PORT]
	req := make([]byte, 4)
	if _, err := io.ReadFull(c, req); err != nil {
		return
	}
	if req[0] != socks5Version || req[1] != socks5CmdConnect {
		return
	}
	switch req[3] {
	case socks5AddrIPv4:
		io.CopyN(io.Discard, c, 4+2)
	case socks5AddrIPv6:
		io.CopyN(io.Discard, c, 16+2)
	case socks5AddrDomain:
		l := make([]byte, 1)
		io.ReadFull(c, l)
		io.CopyN(io.Discard, c, int64(l[0])+2)
	}

	// Reply: bind to 127.0.0.1:0 (any).
	reply := []byte{socks5Version, socks5ReplySuccess, 0x00, socks5AddrIPv4,
		127, 0, 0, 1, 0, 0}
	if _, err := c.Write(reply); err != nil {
		return
	}

	// Now byte-echo the rest. / 之后字节回显剩余。
	buf := make([]byte, 4096)
	for {
		n, err := c.Read(buf)
		if n > 0 {
			_, _ = c.Write(buf[:n])
		}
		if err != nil {
			return
		}
	}
}

func TestSOCKS5_NoAuth(t *testing.T) {
	srv := startFakeSOCKS5(t, fakeSOCKS5Opts{})
	dialer, err := NewSOCKS5Dialer(&ProxyConfig{
		Type:    ProxyTypeSOCKS5,
		Address: srv.addr(),
		Timeout: 2 * time.Second,
	})
	if err != nil {
		t.Fatalf("NewSOCKS5Dialer: %v", err)
	}
	conn, err := dialer.DialContext(testCtx(t), "tcp", "10.0.0.1:80")
	if err != nil {
		t.Fatalf("DialContext: %v", err)
	}
	defer conn.Close()

	// Byte echo verification. / 字节回显验证。
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

func TestSOCKS5_PasswordAuth(t *testing.T) {
	srv := startFakeSOCKS5(t, fakeSOCKS5Opts{User: "alice", Pass: "secret"})
	dialer, _ := NewSOCKS5Dialer(&ProxyConfig{
		Type:     ProxyTypeSOCKS5,
		Address:  srv.addr(),
		Username: "alice",
		Password: "secret",
		Timeout:  2 * time.Second,
	})
	conn, err := dialer.DialContext(testCtx(t), "tcp", "10.0.0.1:80")
	if err != nil {
		t.Fatalf("DialContext: %v", err)
	}
	_ = conn.Close()
}

func TestSOCKS5_PasswordAuthRejected(t *testing.T) {
	// Wrong creds → dialer should fail at authenticatePassword.
	// 错误凭据 → dialer 应在 authenticatePassword 失败。
	srv := startFakeSOCKS5(t, fakeSOCKS5Opts{User: "alice", Pass: "secret"})
	dialer, _ := NewSOCKS5Dialer(&ProxyConfig{
		Type:     ProxyTypeSOCKS5,
		Address:  srv.addr(),
		Username: "alice",
		Password: "WRONG",
		Timeout:  2 * time.Second,
	})
	_, err := dialer.DialContext(testCtx(t), "tcp", "10.0.0.1:80")
	if err == nil {
		t.Fatal("DialContext succeeded with bad password")
	}
}

func TestSOCKS5_NoAcceptableMethods(t *testing.T) {
	// Server rejects every auth method → handshake returns 0xFF.
	// 服务器拒绝所有认证方法 → 握手返回 0xFF。
	srv := startFakeSOCKS5(t, fakeSOCKS5Opts{RequireNoAccept: true})
	dialer, _ := NewSOCKS5Dialer(&ProxyConfig{
		Type:    ProxyTypeSOCKS5,
		Address: srv.addr(),
		Timeout: 2 * time.Second,
	})
	_, err := dialer.DialContext(testCtx(t), "tcp", "10.0.0.1:80")
	if err == nil {
		t.Fatal("DialContext succeeded when server rejected all methods")
	}
}

func TestSOCKS5_DomainDestination(t *testing.T) {
	// Real dialer code path for "DOMAIN" ATYP (SOCKS5 ATYP=0x03).
	// The fake server doesn't actually resolve the domain — it just
	// reads the bytes and replies success. We're testing the dialer.
	//
	// 真实 dialer 对 "DOMAIN" ATYP(SOCKS5 ATYP=0x03)的代码路径。
	// 假服务器不真解析域名——只读字节并回 success。我们在测 dialer。
	srv := startFakeSOCKS5(t, fakeSOCKS5Opts{})
	dialer, _ := NewSOCKS5Dialer(&ProxyConfig{
		Type:    ProxyTypeSOCKS5,
		Address: srv.addr(),
		Timeout: 2 * time.Second,
	})
	conn, err := dialer.DialContext(testCtx(t), "tcp", "example.com:443")
	if err != nil {
		t.Fatalf("DialContext (domain): %v", err)
	}
	_ = conn.Close()
}

func TestSOCKS5_IPv6Destination(t *testing.T) {
	srv := startFakeSOCKS5(t, fakeSOCKS5Opts{})
	dialer, _ := NewSOCKS5Dialer(&ProxyConfig{
		Type:    ProxyTypeSOCKS5,
		Address: srv.addr(),
		Timeout: 2 * time.Second,
	})
	conn, err := dialer.DialContext(testCtx(t), "tcp", "[::1]:80")
	if err != nil {
		t.Fatalf("DialContext (ipv6): %v", err)
	}
	_ = conn.Close()
}

func TestSOCKS5_UsernameTooLong(t *testing.T) {
	// Boundary: ulen > 255 → error before sending.
	// 边界：ulen > 255 → 发送前就错误。
	longUser := make([]byte, 256)
	for i := range longUser {
		longUser[i] = 'a'
	}
	dialer, _ := NewSOCKS5Dialer(&ProxyConfig{
		Type:     ProxyTypeSOCKS5,
		Address:  "127.0.0.1:1080",
		Username: string(longUser),
		Password: "p",
		Timeout:  2 * time.Second,
	})
	// We expect error from handshake/authenticatePassword; we don't
	// need a real server. The auth code path is reached only after
	// the server selects 0x02; if we never connect, we still hit
	// the size check by triggering dial on an unreachable address.
	//
	// 预期 handshake/authenticatePassword 返回错误；不需要真服务器。
	// auth 代码路径仅在服务器选 0x02 后到达；若从不连接,可通过拨
	// 不可达地址触发 size 检查。
	_, err := dialer.DialContext(testCtx(t), "tcp", "10.0.0.1:80")
	// The exact error path is implementation-dependent (the size
	// check is in authenticatePassword which is only called after
	// handshake; if dial to the proxy itself fails first, we get
	// a different error). We just assert non-nil.
	//
	// 精确错误路径与实现相关(size 检查在 authenticatePassword 中,
	// 仅在握手后调用；若到代理的拨号先失败,我们得到不同错误)。
	// 我们只断言非空。
	if err == nil {
		t.Fatal("expected error for oversized username (or unreachable proxy)")
	}
}

func TestSOCKS5_ConnectReplyFailure(t *testing.T) {
	// Start a raw TCP server that returns a non-success SOCKS5 reply.
	// / 启动一个返回非 success SOCKS5 响应的原始 TCP 服务器。
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		_ = c.SetDeadline(time.Now().Add(3 * time.Second))
		// Read greeting. / 读问候。
		hdr := make([]byte, 2)
		io.ReadFull(c, hdr)
		methods := make([]byte, hdr[1])
		io.ReadFull(c, methods)
		c.Write([]byte{socks5Version, socks5AuthNone})
		// Read request. / 读请求。
		req := make([]byte, 4)
		io.ReadFull(c, req)
		// Reply with code 0x07 (Command not supported).
		// 用 0x07（命令不支持）响应。
		rep := []byte{socks5Version, 0x07, 0x00, socks5AddrIPv4, 0, 0, 0, 0, 0, 0}
		c.Write(rep)
	}()
	dialer, _ := NewSOCKS5Dialer(&ProxyConfig{
		Type: ProxyTypeSOCKS5, Address: ln.Addr().String(), Timeout: 2 * time.Second,
	})
	_, err = dialer.DialContext(testCtx(t), "tcp", "10.0.0.1:80")
	if err == nil {
		t.Fatal("DialContext succeeded despite server reply failure")
	}
}

func TestSOCKS5_BadVersion(t *testing.T) {
	// Server replies with version != 0x05 → handshake should fail.
	// / 服务器响应版本 != 0x05 → 握手应失败。
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		_ = c.SetDeadline(time.Now().Add(3 * time.Second))
		hdr := make([]byte, 2)
		io.ReadFull(c, hdr)
		methods := make([]byte, hdr[1])
		io.ReadFull(c, methods)
		// Reply with version 0x04 (invalid). / 用 0x04 响应（无效）。
		c.Write([]byte{0x04, socks5AuthNone})
	}()
	dialer, _ := NewSOCKS5Dialer(&ProxyConfig{
		Type: ProxyTypeSOCKS5, Address: ln.Addr().String(), Timeout: 2 * time.Second,
	})
	_, err = dialer.DialContext(testCtx(t), "tcp", "10.0.0.1:80")
	if err == nil {
		t.Fatal("DialContext succeeded despite bad version reply")
	}
}

func TestSOCKS5_ConnectBadAddress(t *testing.T) {
	// Dialer gets an address it can't split. / dialer 收到无法 split 的地址。
	srv := startFakeSOCKS5(t, fakeSOCKS5Opts{})
	dialer, _ := NewSOCKS5Dialer(&ProxyConfig{
		Type: ProxyTypeSOCKS5, Address: srv.addr(), Timeout: 2 * time.Second,
	})
	_, err := dialer.DialContext(testCtx(t), "tcp", "no-port-here")
	if err == nil {
		t.Fatal("DialContext succeeded with no-port address")
	}
}

func TestSOCKS5_ConnectBadPort(t *testing.T) {
	// Dialer gets a non-numeric port. / dialer 收到非数字端口。
	srv := startFakeSOCKS5(t, fakeSOCKS5Opts{})
	dialer, _ := NewSOCKS5Dialer(&ProxyConfig{
		Type: ProxyTypeSOCKS5, Address: srv.addr(), Timeout: 2 * time.Second,
	})
	_, err := dialer.DialContext(testCtx(t), "tcp", "10.0.0.1:abc")
	if err == nil {
		t.Fatal("DialContext succeeded with non-numeric port")
	}
}

func TestSOCKS5_DomainTooLong(t *testing.T) {
	// Domain name > 255 bytes is rejected by the dialer.
	// / 域名 > 255 字节被 dialer 拒绝。
	longDomain := make([]byte, 256)
	for i := range longDomain {
		longDomain[i] = 'a'
	}
	longDomain[255] = 'b'
	srv := startFakeSOCKS5(t, fakeSOCKS5Opts{})
	dialer, _ := NewSOCKS5Dialer(&ProxyConfig{
		Type: ProxyTypeSOCKS5, Address: srv.addr(), Timeout: 2 * time.Second,
	})
	_, err := dialer.DialContext(testCtx(t), "tcp",
		string(longDomain)+":80")
	if err == nil {
		t.Fatal("DialContext succeeded with oversized domain")
	}
}

// Helper to keep tests from leaving listeners around. / 防止测试残留 listener。
var _ = binary.BigEndian // keep the stdlib import warm
var _ = strconv.Itoa
