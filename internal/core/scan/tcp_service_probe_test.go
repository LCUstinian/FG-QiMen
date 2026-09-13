// tcp_service_probe_test.go — drives TCPServiceProbe against in-process
// fake services: first-payload response, second-payload response, and
// all-silent.
// / tcp_service_probe_test.go — 用进程内假服务驱动 TCPServiceProbe：
// 首 payload 响应、第二 payload 响应、全部沉默。
package scan

import (
	"context"
	"io"
	"net"
	"testing"
	"time"
)

// startFakeTCPService runs a one-shot listener whose handler consumes
// every payload and answers with resp on the n-th payload (1-based;
// n == 0 means never answer). Returns the bound port.
// / startFakeTCPService 起一个一次性 listener，handler 收下每个
// payload，并在第 n 个 payload 时回 resp（n 从 1 计；n == 0 表示永
// 不回答）。返回绑定的端口。
func startFakeTCPService(t *testing.T, n int, resp []byte) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
		for i := 1; ; i++ {
			if _, err := io.ReadFull(conn, make([]byte, 4)); err != nil {
				return // client hung up / payload shorter than 4 — done
			}
			if n != 0 && i == n {
				if _, err := conn.Write(resp); err != nil {
					return
				}
			}
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port
}

func fixedPayloads(n int) func(int) [][]byte {
	return func(int) [][]byte {
		pl := make([][]byte, n)
		for i := range pl {
			pl[i] = []byte("TEST")
		}
		return pl
	}
}

func TestTCPServiceProbe_FirstPayloadResponse(t *testing.T) {
	port := startFakeTCPService(t, 1, []byte("RESP1"))
	p := NewTCPServiceProbe(fixedPayloads(3))
	p.DialTimeout = time.Second
	p.ReadTimeout = 500 * time.Millisecond
	got, err := p.ProbeBanner(context.Background(), "127.0.0.1", port, 2*time.Second)
	if err != nil {
		t.Fatalf("ProbeBanner: %v", err)
	}
	if string(got) != "RESP1" {
		t.Fatalf("banner = %q, want RESP1", got)
	}
}

func TestTCPServiceProbe_SecondPayloadResponse(t *testing.T) {
	port := startFakeTCPService(t, 2, []byte("RESP2"))
	p := NewTCPServiceProbe(fixedPayloads(3))
	p.DialTimeout = time.Second
	p.ReadTimeout = 500 * time.Millisecond
	got, err := p.ProbeBanner(context.Background(), "127.0.0.1", port, 2*time.Second)
	if err != nil {
		t.Fatalf("ProbeBanner: %v", err)
	}
	if string(got) != "RESP2" {
		t.Fatalf("banner = %q, want RESP2 (round-by-round sends must continue after silence)", got)
	}
}

func TestTCPServiceProbe_AllSilent(t *testing.T) {
	port := startFakeTCPService(t, 0, nil)
	p := NewTCPServiceProbe(fixedPayloads(2))
	p.DialTimeout = time.Second
	p.ReadTimeout = 200 * time.Millisecond
	got, err := p.ProbeBanner(context.Background(), "127.0.0.1", port, 2*time.Second)
	if err != nil {
		t.Fatalf("ProbeBanner: %v", err)
	}
	if got != nil {
		t.Fatalf("banner = %q, want nil on a fully silent service", got)
	}
}

func TestTCPServiceProbe_NoPayloads(t *testing.T) {
	p := NewTCPServiceProbe(func(int) [][]byte { return nil })
	got, err := p.ProbeBanner(context.Background(), "127.0.0.1", 1, time.Second)
	if err != nil || got != nil {
		t.Fatalf("no-payload probe must be a silent no-op, got (%q, %v)", got, err)
	}
}
