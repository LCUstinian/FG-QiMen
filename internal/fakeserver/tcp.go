package fakeserver

import (
	"io"
	"net"
	"testing"
)

// ListenLoop starts a TCP listener on 127.0.0.1:0 and dispatches
// each accepted connection to handler in a goroutine. Returns
// ("127.0.0.1", port) so the test can pass them to Identify /
// Credential. Closes the listener via t.Cleanup when the test ends.
//
// handler is called once per accepted connection. It should read /
// write as needed and return; ListenLoop owns connection lifetime.
// Errors on handler I/O are ignored — fake servers are best-effort
// responders for happy-path tests.
//
// / ListenLoop 在 127.0.0.1:0 启 TCP 监听，把每个 accepted conn 分发
// 给 handler（在新 goroutine）。返回 ("127.0.0.1", port)。测试结束
// 时通过 t.Cleanup 关 listener。
func ListenLoop(t *testing.T, handler func(net.Conn)) (host string, port int) {
	t.Helper()
	addr, err := net.ResolveTCPAddr("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("fakeserver: resolve: %v", err)
	}
	ln, err := net.ListenTCP("tcp", addr)
	if err != nil {
		t.Fatalf("fakeserver: listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return // listener closed
			}
			go func(c net.Conn) {
				defer c.Close()
				handler(c)
			}(conn)
		}
	}()
	return "127.0.0.1", ln.Addr().(*net.TCPAddr).Port
}

// ReadAll is a convenience for handlers that just want to drain
// the request bytes before writing a fixed response. Returns
// io.EOF cleanly if the client closes without sending anything.
// / ReadAll 是给 handler 的便利函数：读空请求然后写固定响应。
func ReadAll(c net.Conn) []byte {
	buf := make([]byte, 4096)
	n, _ := c.Read(buf)
	return buf[:n]
}

// Discard reads until EOF or read error and discards. Used by
// handlers that don't need to inspect the request (e.g. fixed
// banner responders). / Discard 读到 EOF 或读错误并丢弃。
func Discard(c net.Conn) {
	_, _ = io.Copy(io.Discard, c)
}
