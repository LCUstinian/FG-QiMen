// memcached_test.go — A.6 coverage baseline via fakeserver.ListenLoop.
// memcached_test.go — 通过 fakeserver.ListenLoop 的 A.6 覆盖率基线。
package memcached

import (
	"bufio"
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/fakeserver"
)

// versionResponder reads the "version\r\n" probe from the plugin and
// replies with the minimum "VERSION <token>\r\n" payload the
// memcached Identify parser accepts (memcached.go:57 —
// strings.HasPrefix(line, "VERSION ")).
// / versionResponder 读取插件的 "version\r\n" 探针并回复 memcached
// Identify 解析器接受的最小 "VERSION <token>\r\n"（memcached.go:57
// 的 strings.HasPrefix(line, "VERSION ")）。
func versionResponder(c net.Conn) {
	br := bufio.NewReader(c)
	// Drain the client's request line(s); fake server is best-effort,
	// so we don't fail the test on read error.
	// / 排空客户端的请求行；假服务器是尽力而为，不因读错误让测试失败。
	_, _, _ = br.ReadLine()
	_, _ = c.Write([]byte("VERSION 1.6.18\r\n"))
}

func TestMemcached_IdentifyHit(t *testing.T) {
	host, port := fakeserver.ListenLoop(t, versionResponder)
	p := New()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	r := p.Identify(ctx, host, port)
	if r == nil {
		t.Fatal("expected memcached hit, got nil")
	}
	if r.Service != "memcached" {
		t.Errorf("Service = %q, want %q", r.Service, "memcached")
	}
	if !strings.HasPrefix(r.Banner, "memcached ") {
		t.Errorf("Banner = %q, want prefix %q", r.Banner, "memcached ")
	}
}

// TestMemcached_CredentialHit is skipped: the memcached plugin's
// Credential is a no-op stub (always returns nil) per
// memcached.go:39-42 — actual credential testing lives in
// core/cred/protocols/memcached.go if/when it lands.
// / TestMemcached_CredentialHit 跳过：memcached 插件的 Credential
// 是空 stub（始终返回 nil），见 memcached.go:39-42。
func TestMemcached_CredentialHit(t *testing.T) {
	t.Skip("memcached.Credential is a no-op stub; see memcached.go Credential()")
}
