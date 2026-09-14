package fakeserver

import (
	"fmt"
	"net"
	"testing"
)

// UDPListener is a bench/production-grade fake UDP service: one bound
// socket, one dispatch loop, no read deadlines (absorbs datagrams for
// its whole lifetime). handler returns the response bytes — nil (or
// an empty slice) suppresses the reply, which is exactly the "silent
// / open|filtered" behavior the UDP farm topology models.
//
// / UDPListener 是 bench 级假 UDP 服务：一条绑定 socket、一个分发循
// 环、无读超时（整个生命周期都在吸收 datagram）。handler 返回响应字
// 节——nil（或空切片）不回复，正是 UDP farm 拓扑要模拟的"静默 /
// open|filtered"行为。
type UDPListener struct {
	conn *net.UDPConn
}

// ListenUDP binds host:port (host "0.0.0.0" for a wildcard bind, a
// specific loopback IP for a per-host service) and starts the
// dispatch loop. port 0 = ephemeral. Use Port() afterwards.
//
// / ListenUDP 绑定 host:port（host "0.0.0.0" 为 wildcard 绑定，具体
// 回环 IP 为单主机服务）并启动分发循环。port 0 = 临时端口。随后用
// Port() 取端口。
func ListenUDP(host string, port int, handler func(req []byte, src *net.UDPAddr) []byte) (*UDPListener, error) {
	addr := &net.UDPAddr{IP: net.ParseIP(host), Port: port}
	if addr.IP == nil {
		return nil, fmt.Errorf("fakeserver: bad udp host %q", host)
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return nil, fmt.Errorf("fakeserver: listen udp %s:%d: %w", host, port, err)
	}
	l := &UDPListener{conn: conn}
	go l.loop(handler)
	return l, nil
}

// loop dispatches datagrams until the socket errors (Close). No read
// deadline: silent listeners must keep absorbing for the whole scan.
// / loop 分发 datagram 直到 socket 出错（Close）。无读超时：静默监听
// 必须在整个扫描期间持续吸收。
func (l *UDPListener) loop(handler func([]byte, *net.UDPAddr) []byte) {
	buf := make([]byte, 64*1024)
	for {
		n, src, err := l.conn.ReadFromUDP(buf)
		if err != nil {
			return // listener closed
		}
		if resp := handler(buf[:n], src); len(resp) > 0 {
			_, _ = l.conn.WriteToUDP(resp, src)
		}
	}
}

// Port returns the bound port. / Port 返回绑定端口。
func (l *UDPListener) Port() int {
	if a, ok := l.conn.LocalAddr().(*net.UDPAddr); ok {
		return a.Port
	}
	return 0
}

// Close stops the dispatch loop and releases the socket. Close is
// idempotent. / Close 停止分发循环并释放 socket。幂等。
func (l *UDPListener) Close() error { return l.conn.Close() }

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
	l, err := ListenUDP("127.0.0.1", 0, handler)
	if err != nil {
		t.Fatalf("fakeserver: listen udp: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })
	return "127.0.0.1", l.Port()
}
