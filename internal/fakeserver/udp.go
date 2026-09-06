package fakeserver

import (
	"net"
	"testing"
	"time"
)

// ListenUDPLoop starts a UDP listener on 127.0.0.1:0 and dispatches
// each datagram to handler in a goroutine. handler returns the
// response bytes (nil to send nothing) and the source address.
// Closes the conn via t.Cleanup.
//
// handler signature: func(req []byte, src *net.UDPAddr) []byte.
// Returning nil suppresses the reply. Per-conn state lives in the
// handler closure; for stateful UDP protocols (rare), use a map
// keyed by src.String().
//
// / ListenUDPLoop 在 127.0.0.1:0 启 UDP conn，每个 datagram 分发给
// handler。handler 返回响应字节（nil 不回）。测试结束 t.Cleanup 关。
func ListenUDPLoop(t *testing.T, handler func([]byte, *net.UDPAddr) []byte) (host string, port int) {
	t.Helper()
	addr, err := net.ResolveUDPAddr("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("fakeserver: resolve udp: %v", err)
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		t.Fatalf("fakeserver: listen udp: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	go func() {
		buf := make([]byte, 64*1024)
		for {
			_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
			n, src, err := conn.ReadFromUDP(buf)
			if err != nil {
				return // listener closed
			}
			resp := handler(buf[:n], src)
			if resp != nil {
				_, _ = conn.WriteToUDP(resp, src)
			}
		}
	}()
	return "127.0.0.1", conn.LocalAddr().(*net.UDPAddr).Port
}
