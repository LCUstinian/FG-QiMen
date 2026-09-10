// activemq_test.go — A.6 coverage baseline for the activemq plugin.
//
// activemq_test.go — activemq 插件的 A.6 覆盖率基线。
//
// Wire format being faked (activemq.go:6-9):
//
//	[4B magic: 0x00000000 or 0x01000000][1B minor][1B major][frame]
//
// The plugin only reads the 4-byte magic + 2-byte version. We
// replicate exactly those 6 bytes in the handler and let the
// plugin return.
// 仿真的线协议格式 (activemq.go:6-9)：
//
//	[4B magic: 0x00000000 或 0x01000000][1B minor][1B major][frame]
//
// 插件只读 4 字节 magic + 2 字节 version；handler 严格发这 6 字节。
package activemq

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/fakeserver"
)

// openWireV11 sends the canonical OpenWire v1 magic (4 zero bytes)
// plus version bytes for OpenWire v11 (minor=0, major=11 per
// activemq.go:77 — ver[1]=major, ver[0]=minor). We use the v11
// shape so the banner string is a recognisable literal.
// / openWireV11 发规范 OpenWire v1 magic（4 个零字节）+ OpenWire
// v11 的 version 字节（minor=0, major=11，按 activemq.go:77 —
// ver[1]=major, ver[0]=minor）。挑 v11 是为了让 banner 字面值
// 稳定可断言。
func openWireV11() []byte {
	return []byte{
		0x00, 0x00, 0x00, 0x00, // magic (v1)
		0x00, // ver[0] = minor = 0
		0x0b, // ver[1] = major = 11
	}
}

// openWireForkV1 sends the alternative magic (hdr[3]==1) accepted
// at activemq.go:69 for "some forks".
// / openWireForkV1 发 activemq.go:69 接受的另一种 magic
// （hdr[3]==1，用于"部分 fork"）。
func openWireForkV1() []byte {
	return []byte{
		0x00, 0x00, 0x00, 0x01, // fork magic
		0x01, // minor
		0x02, // major
	}
}

// TestActiveMQ_IdentifyHit spins up a TCP fake server that
// emits a valid OpenWire v11 header. The plugin must identify
// the service as "activemq" and the banner must reference
// OpenWire v11.0. / 启 TCP 假 server 发合法 OpenWire v11 头。
// 插件应识别服务为 "activemq"，banner 必须包含 "OpenWire
// v11.0"。
func TestActiveMQ_IdentifyHit(t *testing.T) {
	host, port := fakeserver.ListenLoop(t, func(c net.Conn) {
		_, _ = c.Write(openWireV11())
	})
	p := New()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	r := p.Identify(ctx, host, port)
	if r == nil {
		t.Fatal("Identify returned nil for valid OpenWire magic")
	}
	if r.Service != "activemq" {
		t.Errorf("Service = %q, want %q", r.Service, "activemq")
	}
	if !strings.Contains(r.Banner, "ActiveMQ") {
		t.Errorf("Banner = %q, want substring %q", r.Banner, "ActiveMQ")
	}
	if !strings.Contains(r.Banner, "OpenWire v11.0") {
		t.Errorf("Banner = %q, want substring %q", r.Banner, "OpenWire v11.0")
	}
}

// TestActiveMQ_IdentifyForkMagic covers the second accepted
// magic (0x01000000) at activemq.go:69 — the plugin must
// accept the fork magic and report the version as
// major=ver[1], minor=ver[0]. / 验证 activemq.go:69 接受的
// 第二个 magic（0x01000000）——插件必须接受 fork magic 并报
// major=ver[1], minor=ver[0] 的 version。
func TestActiveMQ_IdentifyForkMagic(t *testing.T) {
	host, port := fakeserver.ListenLoop(t, func(c net.Conn) {
		_, _ = c.Write(openWireForkV1())
	})
	p := New()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	r := p.Identify(ctx, host, port)
	if r == nil {
		t.Fatal("Identify returned nil for fork OpenWire magic")
	}
	if r.Service != "activemq" {
		t.Errorf("Service = %q, want %q", r.Service, "activemq")
	}
	if !strings.Contains(r.Banner, "OpenWire v2.1") {
		t.Errorf("Banner = %q, want substring %q", r.Banner, "OpenWire v2.1")
	}
}

// TestActiveMQ_IdentifyNonOpenWire covers the negative case:
// the server replies with bytes that don't match the OpenWire
// magic. The plugin must return nil so we don't false-positive
// on a non-ActiveMQ TCP service. / 验证反向 case：server 返
// 不匹配 OpenWire magic 的字节。插件必须返 nil，避免在
// 非 ActiveMQ TCP 服务上假阳性。
func TestActiveMQ_IdentifyNonOpenWire(t *testing.T) {
	host, port := fakeserver.ListenLoop(t, func(c net.Conn) {
		// Anything where byte 3 is neither 0 nor 1 — the
		// plugin rejects at activemq.go:69. / 第 4 字节既不是
		// 0 也不是 1——插件在 activemq.go:69 拒绝。
		_, _ = c.Write([]byte("HTTP/1.0 200 OK\r\n\r\n"))
	})
	p := New()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if got := p.Identify(ctx, host, port); got != nil {
		t.Errorf("Identify with non-OpenWire reply = %+v, want nil", got)
	}
}

// TestActiveMQ_CredentialHit is skipped: the activemq
// plugin's Credential is a no-op stub (always returns nil)
// per activemq.go:44-46 — actual credential testing lives in
// core/cred/protocols/. / TestActiveMQ_CredentialHit 跳过：
// activemq 插件的 Credential 是空 stub（始终返回 nil），见
// activemq.go:44-46——真正的凭证测试在 core/cred/protocols/。
func TestActiveMQ_CredentialHit(t *testing.T) {
	t.Skip("activemq.Credential is a no-op stub; see activemq.go Credential()")
}
