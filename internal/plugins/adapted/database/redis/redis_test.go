// redis_test.go — A.6 coverage baseline for the redis plugin.
//
// redis_test.go — redis 插件的 A.6 覆盖率基线。
//
// Wire format being faked (redis.go:48-67):
//
//	client writes "PING\r\n"
//	server replies "+PONG\r\n"   (or "-NOAUTH\r\n" for a
//	                              password-protected instance)
//
// Plugin only inspects the response prefix after TrimSpace; any
// line starting with "+PONG" or "-NOAUTH" triggers a hit. The
// handler drains the client request bytes (so the kernel doesn't
// RST the connection) and writes the canonical RESP simple-string
// reply. / 仿真的线协议格式 (redis.go:48-67)：客户端写
// "PING\r\n"，服务端回 "+PONG\r\n"（或 "-NOAUTH\r\n" 表示带
// 密码实例）。插件只看 TrimSpace 后的前缀，任何 "+PONG" /
// "-NOAUTH" 开头的行都会触发命中。handler 消耗客户端写过来的
// 字节以防内核 RST，然后写 RESP simple-string 响应。
package redis

import (
	"bufio"
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/fakeserver"
)

// drainPING reads and discards the client's "PING\r\n" so the
// connection's receive buffer is drained before we write the
// response. Prevents RST on platforms that close the read side
// before write. / drainPING 读并丢弃客户端的 "PING\r\n"，避免
// 我们写响应时 read 侧被关导致 RST。
func drainPING(c net.Conn) {
	sc := bufio.NewScanner(c)
	// tolerate longer-than-PING payloads; cap buf to 1 KiB.
	sc.Buffer(make([]byte, 1024), 1024)
	for sc.Scan() {
		// Stop after the first line — IDENTIFY is a single PING.
		// / 第一行后就停——IDENTIFY 只发一行 PING。
		return
	}
}

// TestRedis_IdentifyHit spins up a TCP fake server that
// answers "+PONG\r\n". The plugin must identify the service as
// "redis" and the banner must reference PONG. / 启 TCP 假 server
// 回 "+PONG\r\n"。插件应识别服务为 "redis"，banner 必须包含
// PONG。
func TestRedis_IdentifyHit(t *testing.T) {
	host, port := fakeserver.ListenLoop(t, func(c net.Conn) {
		drainPING(c)
		_, _ = c.Write([]byte("+PONG\r\n"))
	})
	p := New()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	r := p.Identify(ctx, host, port)
	if r == nil {
		t.Fatal("Identify returned nil for valid +PONG response")
	}
	if r.Service != "redis" {
		t.Errorf("Service = %q, want %q", r.Service, "redis")
	}
	if !strings.Contains(r.Banner, "PONG") {
		t.Errorf("Banner = %q, want substring %q", r.Banner, "PONG")
	}
	if r.Host != host || r.Port != port {
		t.Errorf("Host/Port = %q:%d, want %q:%d", r.Host, r.Port, host, port)
	}
}

// TestRedis_IdentifyNoAuth covers the second accepted response
// shape at redis.go:59 — "-NOAUTH" prefix indicates a password-
// protected Redis. The plugin must still classify it as "redis"
// so we don't false-negative on locked-down instances. / 验证
// redis.go:59 接受的第二种响应形态："-NOAUTH" 前缀表示带密码
// Redis。插件仍应识别为 "redis"，避免在带密码实例上假阴性。
func TestRedis_IdentifyNoAuth(t *testing.T) {
	host, port := fakeserver.ListenLoop(t, func(c net.Conn) {
		drainPING(c)
		_, _ = c.Write([]byte("-NOAUTH Authentication required.\r\n"))
	})
	p := New()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	r := p.Identify(ctx, host, port)
	if r == nil {
		t.Fatal("Identify returned nil for -NOAUTH response")
	}
	if r.Service != "redis" {
		t.Errorf("Service = %q, want %q", r.Service, "redis")
	}
	if !strings.Contains(r.Banner, "NOAUTH") {
		t.Errorf("Banner = %q, want substring %q", r.Banner, "NOAUTH")
	}
}

// TestRedis_IdentifyMiss covers the negative case: the server
// replies with bytes that don't start with "+PONG" or "-NOAUTH".
// The plugin must return nil so we don't false-positive on a
// non-Redis TCP service (e.g. HTTP). / 验证反向 case：server 返
// 不以 "+PONG" / "-NOAUTH" 开头的字节。插件必须返 nil，避免
// 在非 Redis 服务（如 HTTP）上假阳性。
func TestRedis_IdentifyMiss(t *testing.T) {
	host, port := fakeserver.ListenLoop(t, func(c net.Conn) {
		drainPING(c)
		_, _ = c.Write([]byte("+OK some-other-protocol\r\n"))
	})
	p := New()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if got := p.Identify(ctx, host, port); got != nil {
		t.Errorf("Identify with non-PONG reply = %+v, want nil", got)
	}
}

// TestRedis_CredentialHit is skipped: the redis plugin's
// Credential is a no-op stub (always returns nil) per
// redis.go:40-45 — actual credential testing lives in
// core/cred/protocols/. / TestRedis_CredentialHit 跳过：redis
// 插件的 Credential 是空 stub（始终返回 nil），见
// redis.go:40-45——真正的凭证测试在 core/cred/protocols/。
func TestRedis_CredentialHit(t *testing.T) {
	t.Skip("redis.Credential is a no-op stub; see redis.go Credential()")
}
