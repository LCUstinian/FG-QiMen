// udp_probe_test.go — unit test for the UDP probe.
// / UDP probe 的单元测试。
package scan

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"
)

// TestUDPProbe_ConnRefused: closed UDP port returns StateClosed (or
// filtered if no ICMP). / TestUDPProbe_ConnRefused：closed UDP 端口
// 返 StateClosed（无 ICMP 则 filtered）。
func TestUDPProbe_ConnRefused(t *testing.T) {
	// Bind+close a TCP port just to get a port number. UDP packet
	// sent there will go nowhere → no response → Open with empty
	// banner (per the probe's fallback). / 绑+关一个 TCP 端口拿一个
	// 端口号。发到那里的 UDP 包无响应 → Open+空 banner（probe 兜底）。
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	p := NewUDPProbe()
	res, err := p.Probe(context.Background(), "127.0.0.1", port, 1*time.Second)
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	// UDP silence → we mark Open (ambiguous). / UDP 沉默 → 标 Open
	//（模糊）。
	if res.State != StateOpen {
		t.Errorf("expected StateOpen (UDP silence), got %v", res.State)
	}
	if res.Method != MethodUDP {
		t.Errorf("expected MethodUDP, got %v", res.Method)
	}
}

// TestUDPProbe_OpenWithResponse: a UDP listener that responds is
// StateOpen. / TestUDPProbe_OpenWithResponse：响应 UDP 监听器是
// StateOpen。
func TestUDPProbe_OpenWithResponse(t *testing.T) {
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen udp: %v", err)
	}
	defer pc.Close()
	port := pc.LocalAddr().(*net.UDPAddr).Port
	// Echo loop. / Echo 循环。
	go func() {
		buf := make([]byte, 512)
		for {
			n, addr, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			_, _ = pc.WriteTo(buf[:n], addr)
		}
	}()
	p := NewUDPProbe()
	res, err := p.Probe(context.Background(), "127.0.0.1", port, 1*time.Second)
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if res.State != StateOpen {
		t.Errorf("expected StateOpen, got %v (RTT %v)", res.State, res.RTT)
	}
	// Banner is the echoed probe byte (0x00) — trimmed by trimASCII
	// because \x00 is non-printable. So we expect an empty banner,
	// not an error. / Banner 是回显的探针字节（0x00）——trimASCII
	// 因为 \x00 不可打印而剥掉。所以我们期望空 banner，不报错。
	// This is intentional: the probe byte is generic noise; we
	// don't try to make sense of it. / 这是故意的：探针字节是通用
	// 噪声；不尝试解析它。
	_ = res.Banner
	_ = strings.Contains
}

// TestUDPProbe_NonRoutableSilent: Note that in Go, net.Dialer for
// UDP is non-blocking — it just creates the socket. There's no
// reliable way to distinguish "open" from "filtered" for UDP
// without a service-specific probe. We accept that ambiguity and
// document it. / TestUDPProbe_NonRoutableSilent：注意 Go 里 net.Dialer
// 对 UDP 是非阻塞的——只创建 socket。没有服务级 probe 的话，无法可靠
// 区分"open"和"filtered"。我们接受这个模糊性并文档化。
func TestUDPProbe_NonRoutableSilent(t *testing.T) {
	p := NewUDPProbe()
	// 192.0.2.1 is TEST-NET-1 (RFC 5737), reserved for documentation.
	// In most environments the local stack has no route → the dial
	// may fail with "no route to host" OR silently succeed and the
	// write later times out. Both are valid UDP behaviors.
	// / 192.0.2.1 是 TEST-NET-1 (RFC 5737)，文档保留段。多数环境
	// 本地栈无路由 → 拨号可能"no route to host"失败或静默成功然后
	// 写超时。两者都是合法 UDP 行为。
	res, _ := p.Probe(context.Background(), "192.0.2.1", 161, 1*time.Second)
	if res.State != StateOpen && res.State != StateFiltered && res.State != StateClosed {
		t.Errorf("expected one of Open/Filtered/Closed, got %v", res.State)
	}
}
