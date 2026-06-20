// ntp_test.go — unit tests for the NTP Identify plugin. / NTP 识
// 别插件的单元测试。
package ntp

import (
	"context"
	"net"
	"testing"
	"time"
)

// startFakeNTP starts an in-process UDP NTP server. Returns the
// host:port the client should connect to. / startFakeNTP 启动一个
// 进程内的 UDP NTP 假服务，返回客户端应连接的 host:port。
func startFakeNTP(t *testing.T, stratum byte) (string, int) {
	t.Helper()
	addr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	go func() {
		buf := make([]byte, 48)
		for {
			_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
			_, raddr, err := conn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			resp := make([]byte, 48)
			// LI=0, VN=4, Mode=4 (server) = 0b00100100 = 0x24.
			// / LI=0, VN=4, Mode=4 (server) = 0b00100100 = 0x24。
			resp[0] = 0x24
			resp[1] = stratum
			_, _ = conn.WriteToUDP(resp, raddr)
		}
	}()
	return "127.0.0.1", conn.LocalAddr().(*net.UDPAddr).Port
}

// TestNTP_Hit verifies a stratum-2 server is identified. /
// 验证 stratum-2 server 被识别。
func TestNTP_Hit(t *testing.T) {
	host, port := startFakeNTP(t, 2)
	auth := New()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	hit := auth.Identify(ctx, host, port)
	if hit == nil {
		t.Fatal("expected hit on NTP server")
	}
	if hit.Service != "ntp" {
		t.Errorf("Service = %q, want ntp", hit.Service)
	}
	if hit.Banner != "NTP (stratum 2)" {
		t.Errorf("Banner = %q, want \"NTP (stratum 2)\"", hit.Banner)
	}
}

// TestNTP_NotNTP verifies a non-NTP server (e.g. echo port)
// returns nil. / 验证非 NTP 服务（echo）返回 nil。
func TestNTP_NotNTP(t *testing.T) {
	// Listen on a TCP port that does nothing. UDP dial succeeds
	// but the response will not be NTP-shaped. / 监听一个无操
	// 作的 TCP 端口。UDP 拨号成功但响应不是 NTP 格式。
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
		// Reply with garbage. / 回垃圾数据。
		_, _ = c.Write([]byte("not ntp"))
		_ = c.Close()
	}()
	// NTP's UDP dial to a TCP-only port will fail. We expect nil
	// (dial failure returns nil from RawUDPIdentify). / NTP 的
	// UDP 拨号到仅 TCP 的端口会失败。我们期望 nil（拨号失败
	// 让 RawUDPIdentify 返回 nil）。
	tcpAddr := ln.Addr().(*net.TCPAddr)
	auth := New()
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	hit := auth.Identify(ctx, tcpAddr.IP.String(), tcpAddr.Port)
	if hit != nil {
		t.Errorf("expected nil for TCP-only port, got %+v", hit)
	}
}
