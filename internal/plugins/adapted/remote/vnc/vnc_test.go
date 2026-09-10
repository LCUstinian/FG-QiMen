// vnc_test.go — fake-server test for the vnc Identify plugin.
//
// Pattern: fakeserver.ListenLoop binds 127.0.0.1:0, the handler
// sends the 12-byte RFB ProtocolVersion banner ("RFB xxx.yyy\n")
// on connect, and the plugin's Identify reads exactly 12 bytes,
// validates the "RFB " prefix, and returns Service="vnc".
//
// / vnc_test.go — vnc Identify 插件的 fake-server 测试。
// fakeserver.ListenLoop 在 127.0.0.1:0 起 TCP 监听；handler 在
// connect 后发 12 字节 RFB ProtocolVersion banner（"RFB xxx.yyy\n"）；
// plugin Identify 读正好 12 字节，校验 "RFB " 前缀，返回 Service="vnc"。
//
// This is a stateful TCP plugin (Tier 3 in the v0.6.0 fake-server
// plan). The plugin's Identify is single-state — server speaks
// first on connect and the plugin reads exactly 12 bytes. The
// deeper RFB handshake (ClientProtocolVersion reply → ClientInit
// → ServerInit) is not exercised by Identify; it would be the
// Credential path through core/cred/protocols/vnc.go (VNCAuthenticator
// via go-vnc), not this plugin's Identify.
//
// / 这是一个 stateful TCP 插件（v0.6.0 fake-server 计划的 Tier 3）。
// plugin Identify 只读单态 12 字节 — server connect 后先说话。深
// 层 RFB 握手（ClientProtocolVersion 应答 → ClientInit →
// ServerInit）不走 Identify，而是走 Credential，通过
// core/cred/protocols/vnc.go（VNCAuthenticator via go-vnc）。
package vnc

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/fakeserver"
)

// vncBanner is the 12-byte RFB ProtocolVersion handshake the
// plugin's Identify expects on connect. The plugin reads 12
// bytes, checks the "RFB " prefix, and formats "VNC <xxx.yyy>"
// from bytes [4:11].
//
// / vncBanner 是 plugin Identify 在 connect 后期待的 12 字节 RFB
// ProtocolVersion 握手串。plugin 读 12 字节，校验 "RFB " 前缀，
// 然后从 [4:11] 拼出 "VNC <xxx.yyy>"。
const vncBanner = "RFB 003.008\n"

// TestVnc_IdentifyHit covers the happy path: server speaks first
// with a valid RFB banner; plugin reads 12 bytes, validates the
// "RFB " prefix, returns Service="vnc" and Banner="VNC 003.008".
//
// / TestVnc_IdentifyHit 覆盖 happy path：server 先发合法 RFB banner；
// plugin 读 12 字节，校验 "RFB " 前缀，返回 Service="vnc"、
// Banner="VNC 003.008"。
func TestVnc_IdentifyHit(t *testing.T) {
	host, port := fakeserver.ListenLoop(t, func(c net.Conn) {
		// Per RFB spec the server speaks the ProtocolVersion first
		// on connect. / 按 RFB 协议，server 在 connect 后先发
		// ProtocolVersion。
		_, _ = c.Write([]byte(vncBanner))
	})

	p := New()
	res := p.Identify(context.Background(), host, port)
	if res == nil {
		t.Fatalf("Identify returned nil; expected vnc hit")
	}
	if res.Service != "vnc" {
		t.Errorf("Service = %q, want %q", res.Service, "vnc")
	}
	if res.Banner != "VNC 003.008" {
		t.Errorf("Banner = %q, want %q", res.Banner, "VNC 003.008")
	}
	if res.Host != host || res.Port != port {
		t.Errorf("Host/Port = %q/%d, want %q/%d", res.Host, res.Port, host, port)
	}
}

// TestVnc_IdentifyMiss covers the negative branch: server
// greets with something that doesn't start with "RFB ". The
// plugin must return nil.
//
// / TestVnc_IdentifyMiss 覆盖反向分支：server greeting 不以
// "RFB " 开头，plugin 必须返回 nil。
func TestVnc_IdentifyMiss(t *testing.T) {
	host, port := fakeserver.ListenLoop(t, func(c net.Conn) {
		// "SSH-2.0-OpenSSH_8.0" is the SSH banner; plugin should
		// reject it because bytes[0:4] != "RFB ". /
		// "SSH-2.0-OpenSSH_8.0" 是 SSH banner；plugin 应拒
		// 绝（bytes[0:4] != "RFB "）。
		_, _ = c.Write([]byte("SSH-2.0-OpenSSH_8.0\r\n"))
	})

	if res := New().Identify(context.Background(), host, port); res != nil {
		t.Errorf("Identify = %+v, want nil for non-VNC greeting", res)
	}
}

// TestVnc_ShortBanner covers the short-read branch: server sends
// fewer than 12 bytes before closing (or just never finishes the
// banner). The plugin must return nil (read < 12).
//
// / TestVnc_ShortBanner 覆盖短读分支：server 发 < 12 字节就关闭
// （或没发完）。plugin 必须返回 nil（read < 12）。
func TestVnc_ShortBanner(t *testing.T) {
	host, port := fakeserver.ListenLoop(t, func(c net.Conn) {
		// Only 4 bytes — plugin must bail at the n < 12 check. /
		// 仅 4 字节 — plugin 必须在 n < 12 处直接返回。
		_, _ = c.Write([]byte("RFB "))
	})

	if res := New().Identify(context.Background(), host, port); res != nil {
		t.Errorf("Identify = %+v, want nil for short banner", res)
	}
}

// TestVnc_DialFailure exercises the dial-error branch in
// Identify: an unroutable address must yield nil without blocking
// past the caller's context deadline.
//
// / TestVnc_DialFailure 跑 Identify 的拨号失败分支：不可达地址
// 必须在 caller 的 context deadline 内返回 nil。
func TestVnc_DialFailure(t *testing.T) {
	// 192.0.2.1 is RFC 5737 TEST-NET-1 — guaranteed unroutable. /
	// 192.0.2.1 是 RFC 5737 TEST-NET-1，保证不可达。
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	if res := New().Identify(ctx, "192.0.2.1", 5900); res != nil {
		t.Errorf("Identify(unroutable) = %+v, want nil", res)
	}
}

// TestVnc_CredentialNoOp documents that the plugin's Credential
// is a documented no-op stub (see vnc.go L46-48) — VNC credential
// testing lives in internal/core/credential/protocols/vnc.go
// (VNCAuthenticator via go-vnc). Per the v0.6.0 fake-server plan
// we still wire a placeholder so the deeper RFB handshake bytes
// (ClientProtocolVersion reply / ClientInit / ServerInit) can be
// covered if a future contributor promotes the stub to a real
// implementation.
//
// / TestVnc_CredentialNoOp 标注 plugin 的 Credential 是文档化的
// no-op stub（见 vnc.go L46-48）；VNC 凭据测试在
// internal/core/credential/protocols/vnc.go（VNCAuthenticator
// via go-vnc）。按 v0.6.0 fake-server 计划仍保留占位，方便未来
// 贡献者将 stub 升级成真实实现时补齐深层 RFB 握手的字节
// （ClientProtocolVersion 应答 / ClientInit / ServerInit）。
func TestVnc_CredentialNoOp(t *testing.T) {
	t.Skip("Credential is a documented no-op stub in vnc.go (L46-48); " +
		"VNC credential testing lives in internal/core/credential/protocols/vnc.go.")
}
