// telnet_test.go — unit tests for the telnet Identify plugin. /
// telnet 识别插件的单元测试。
//
// Pattern established in v0.6.0 fake-server coverage plan Tier 3
// (stateful TCP plugins). Each test binds 127.0.0.1:0 via
// fakeserver.ListenLoop, drives the protocol greeting the plugin
// expects, and asserts Identify returns the expected hit.
// / v0.6.0 假服务器覆盖率计划 Tier 3（有状态 TCP 插件）模式。每
// 个测试通过 fakeserver.ListenLoop 绑 127.0.0.1:0，驱动插件期望
// 的协议 greeting，然后断言 Identify 返预期 hit。
package telnet

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/fakeserver"
)

// Telnet control bytes per RFC 854. / RFC 854 定义的 Telnet 控制
// 字节。
const (
	telnetIAC   byte = 0xFF // Interpret As Command / 命令起始
	telnetWILL  byte = 0xFB // WILL: sender wants to enable opt / 发送方想启用选项
	telnetDO    byte = 0xFD // DO: sender wants peer to enable opt / 发送方希望对端启用选项
	telnetECHO  byte = 0x01 // ECHO option / 回显选项
	telnetTTYPE byte = 0x18 // Terminal-Type option / 终端类型选项
)

// TestTelnet_IdentifyHit verifies that a telnetd greeting with
// option negotiation (IAC WILL ECHO, IAC DO TTYPE) followed by a
// login prompt is identified as telnet. / 验证带选项协商（IAC
// WILL ECHO, IAC DO TTYPE）和 login 提示符的 telnetd greeting
// 能被识别为 telnet。
//
// The fake server's state-machine step:
//  1. On accept, immediately send IAC WILL ECHO + IAC DO TTYPE
//     (typical telnetd server-initiated negotiation).
//  2. Then send "\r\nlogin: " as the prompt.
//
// / 假服务器状态机步骤：
//  1. accept 时立即发 IAC WILL ECHO + IAC DO TTYPE（典型 telnetd
//     服务端发起协商）。
//  2. 然后发 "\r\nlogin: " 作为提示符。
//
// The plugin reads, strips IAC bytes (skip 2 bytes after each
// 0xFF per stripIAC), and matches the lowercased text against
// "login:". / 插件读后剥掉 IAC 字节（每个 0xFF 后跳 2 字节），
// 把小写文本匹配 "login:"。
func TestTelnet_IdentifyHit(t *testing.T) {
	host, port := fakeserver.ListenLoop(t, func(c net.Conn) {
		// Single combined Write so the plugin's single Read
		// sees the full negotiation + prompt payload (TCP
		// is a stream — separate Writes may fragment and the
		// plugin does only one Read of 512 bytes). / 单次合
		// 并 Write，让插件的单次 Read 看到完整的协商 + 提
		// 示符 payload（TCP 是字节流——分两次 Write 可能分
		// 片，而插件只做一次 512 字节 Read）。
		payload := []byte{
			telnetIAC, telnetWILL, telnetECHO,
			telnetIAC, telnetDO, telnetTTYPE,
		}
		payload = append(payload, "\r\nlogin: "...)
		_, _ = c.Write(payload)
		// Hold the conn open briefly so the plugin's Read
		// has time to return. / 短时保持 conn 打开，让插件
		// 的 Read 有时间返回。
		time.Sleep(100 * time.Millisecond)
	})

	p := New()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	hit := p.Identify(ctx, host, port)
	if hit == nil {
		t.Fatal("expected hit on telnetd greeting, got nil")
	}
	if hit.Service != "telnet" {
		t.Errorf("Service = %q, want telnet", hit.Service)
	}
	if hit.Host != host {
		t.Errorf("Host = %q, want %q", hit.Host, host)
	}
	if hit.Port != port {
		t.Errorf("Port = %d, want %d", hit.Port, port)
	}
	if hit.Banner == "" {
		t.Error("Banner is empty")
	}
}

// TestTelnet_IdentifyBannerHit exercises the isPrintableBanner
// fallback path — when no "login:" / "$ " / "# " trigger matches
// but the greeting is a printable welcome banner (e.g. kernel
// version string), the plugin should still identify it. / 验证
// isPrintableBanner 兜底分支——当没匹配 "login:" / "$ " / "# "
// 但 greeting 是可打印的 welcome banner（如内核版本字符串），
// 插件仍应识别。
//
// Server sends IAC DO NAWS (window-size request — another common
// negotiation byte) followed by a Debian-style kernel banner in a
// single Write so the plugin's single Read sees the full payload
// (TCP is a stream; separate Writes may fragment). / 服务端用一
// 次 Write 发 IAC DO NAWS（窗口尺寸请求——另一常见协商字节）然
// 后是 Debian 风格的内核 banner，让插件的单次 Read 看到完整
// payload（TCP 是字节流；分两次 Write 可能分片）。
func TestTelnet_IdentifyBannerHit(t *testing.T) {
	host, port := fakeserver.ListenLoop(t, func(c net.Conn) {
		// Combined: IAC DO NAWS (0xFF 0xFD 0x1F) + banner.
		// / 合并：IAC DO NAWS（0xFF 0xFD 0x1F）+ banner。
		payload := append([]byte{telnetIAC, telnetDO, 0x1F},
			"Debian GNU/Linux 12\r\n"...)
		_, _ = c.Write(payload)
		time.Sleep(100 * time.Millisecond)
	})

	p := New()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	hit := p.Identify(ctx, host, port)
	if hit == nil {
		t.Fatal("expected hit on printable banner greeting, got nil")
	}
	if hit.Service != "telnet" {
		t.Errorf("Service = %q, want telnet", hit.Service)
	}
	if hit.Banner == "" {
		t.Error("Banner is empty")
	}
}

// TestTelnet_IdentifyMiss verifies that a non-telnet greeting
// (no login prompt, no printable banner, just garbage) returns
// nil. / 验证非 telnet greeting（无 login 提示符、无可打印 banner、
// 只有垃圾数据）返 nil。
func TestTelnet_IdentifyMiss(t *testing.T) {
	host, port := fakeserver.ListenLoop(t, func(c net.Conn) {
		// Too short to count as a banner (< 5 printable chars)
		// and contains no prompt keyword. / 太短不算 banner
		// （< 5 可打印字符）也无提示符关键字。
		_, _ = c.Write([]byte("xx"))
		time.Sleep(100 * time.Millisecond)
	})

	p := New()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	hit := p.Identify(ctx, host, port)
	if hit != nil {
		t.Errorf("expected nil for non-telnet greeting, got %+v", hit)
	}
}

// TestTelnet_CredentialHit is skipped: telnet.Credential is a
// documented no-op stub (see telnet.go:47-50). The real telnet
// credential flow lives in
// internal/core/credential/protocols/telnet.go (TelnetAuthenticator,
// hand-rolled IAC + prompt flow). / TestTelnet_CredentialHit 跳
// 过：telnet.Credential 是文档化的 no-op stub（见
// telnet.go:47-50）。真正的 telnet 凭据流程在
// internal/core/credential/protocols/telnet.go（TelnetAuthenticator，
// 手写 IAC + 提示符流程）。
func TestTelnet_CredentialHit(t *testing.T) {
	t.Skip("telnet.Credential is a no-op stub; see telnet.go Credential()")
}
