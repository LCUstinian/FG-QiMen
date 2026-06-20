// dns_test.go — minimal smoke test for the DNS plugin. / DNS 插件
// 的最小冒烟测试。
package dns

import (
	"context"
	"net"
	"testing"
	"time"
)

// startFakeDNS starts an in-process UDP DNS server that always
// answers with a fixed-format response. / startFakeDNS 启动一个
// 进程内的 UDP DNS 服务，以固定格式响应所有查询。
func startFakeDNS(t *testing.T) (string, int) {
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
		buf := make([]byte, 512)
		for {
			_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
			_, raddr, err := conn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			// Build a minimal response: echo the ID, set QR=1,
			// RCODE=0, QDCOUNT=1, ANCOUNT=1. / 构造最小响应：
			// 回显 ID，置 QR=1，RCODE=0，QDCOUNT=1，ANCOUNT=1。
			resp := make([]byte, 32)
			copy(resp[:2], buf[:2]) // ID echo
			resp[2] = 0x81          // QR=1, RD=1
			resp[3] = 0x80          // RA=1, RCODE=0
			resp[4] = 0x00
			resp[5] = 0x01 // QDCOUNT=1
			resp[6] = 0x00
			resp[7] = 0x01 // ANCOUNT=1
			_, _ = conn.WriteToUDP(resp, raddr)
		}
	}()
	return "127.0.0.1", conn.LocalAddr().(*net.UDPAddr).Port
}

func TestDNS_Hit(t *testing.T) {
	host, port := startFakeDNS(t)
	auth := New()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	hit := auth.Identify(ctx, host, port)
	if hit == nil {
		t.Fatal("expected hit on DNS server")
	}
	if hit.Service != "dns" {
		t.Errorf("Service = %q, want dns", hit.Service)
	}
}
