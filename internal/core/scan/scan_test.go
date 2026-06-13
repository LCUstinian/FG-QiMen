// scan_test.go — unit tests for the scan package.
// scan_test.go — scan 包的单元测试。
package scan

import (
	"context"
	"net"
	"strconv"
	"testing"
	"time"
)

// startTCPListener opens a TCP listener on 127.0.0.1:<random> and
// returns its port. / startTCPListener 在 127.0.0.1:<随机> 打开一个
// TCP listener 并返回端口。
func startTCPListener(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()
	t.Cleanup(func() { _ = ln.Close() })
	return ln.Addr().(*net.TCPAddr).Port
}

// TestTCPConnect_Open verifies the probe returns StateOpen for a
// listening port. / TestTCPConnect_Open 验证 probe 对监听端口返回 StateOpen。
func TestTCPConnect_Open(t *testing.T) {
	port := startTCPListener(t)
	probe := NewTCPConnectProbe()
	res, err := probe.Probe(context.Background(), "127.0.0.1", port, time.Second)
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if res.State != StateOpen {
		t.Errorf("expected StateOpen, got %q", res.State)
	}
	if res.Method != MethodTCPConnect {
		t.Errorf("expected MethodTCPConnect, got %q", res.Method)
	}
}

// TestTCPConnect_Closed verifies the probe returns StateClosed for a
// port that actively refused. / TestTCPConnect_Closed 验证 probe 对
// 主动拒绝的端口返回 StateClosed。
func TestTCPConnect_Closed(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	probe := NewTCPConnectProbe()
	res, err := probe.Probe(context.Background(), "127.0.0.1", port, 500*time.Millisecond)
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if res.State != StateClosed {
		t.Errorf("expected StateClosed, got %q", res.State)
	}
}

// TestTCPConnect_Filtered verifies the probe returns StateFiltered
// for an unroutable address. / TestTCPConnect_Filtered 验证 probe 对
// 不可路由地址返回 StateFiltered。
func TestTCPConnect_Filtered(t *testing.T) {
	// 192.0.2.0/24 is RFC 5737 TEST-NET-1, never routable. The
	// dial should time out. / 192.0.2.0/24 是 RFC 5737 的 TEST-NET-1，
	// 永远不可路由。dial 应该超时。
	probe := NewTCPConnectProbe()
	res, err := probe.Probe(context.Background(), "192.0.2.1", 80, 200*time.Millisecond)
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if res.State != StateFiltered {
		t.Errorf("expected StateFiltered, got %q (RTT=%v)", res.State, res.RTT)
	}
}

// TestTCPConnect_Banner verifies that the banner reader is invoked
// and its return value lands in Result.Banner. / TestTCPConnect_Banner
// 验证 banner reader 被调用，其返回值落在 Result.Banner。
func TestTCPConnect_Banner(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_, _ = c.Write([]byte("HELLO\r\n"))
			_ = c.Close()
		}
	}()

	br := func(conn net.Conn) string {
		_ = conn.SetReadDeadline(time.Now().Add(time.Second))
		buf := make([]byte, 256)
		n, _ := conn.Read(buf)
		return string(buf[:n])
	}
	probe := NewTCPConnectProbeWithBanner(br)
	res, err := probe.Probe(context.Background(), "127.0.0.1", ln.Addr().(*net.TCPAddr).Port, time.Second)
	if err != nil {
		t.Fatalf("probe: %v", err)
	}
	if res.State != StateOpen {
		t.Errorf("expected StateOpen, got %q", res.State)
	}
	if res.Banner == "" {
		t.Errorf("expected non-empty banner")
	}
}

// TestIsConnRefused verifies the helper recognizes various refusal forms.
// / TestIsConnRefused 验证 helper 识别各种拒绝形式。
func TestIsConnRefused(t *testing.T) {
	if !isConnRefused(errRefused) {
		t.Error("expected true for errRefused")
	}
	if isConnRefused(nil) {
		t.Error("expected false for nil")
	}
	if isConnRefused(errTimeout) {
		t.Error("expected false for timeout")
	}
}

var (
	errRefused = &net.OpError{Op: "dial", Err: errStr("connect: connection refused")}
	errTimeout = &net.OpError{Op: "dial", Err: errStr("i/o timeout")}
)

type errStr string

func (e errStr) Error() string   { return string(e) }
func (e errStr) Timeout() bool   { return e == "i/o timeout" }
func (e errStr) Temporary() bool { return false }

// TestCrossIterator verifies the Cartesian product layout.
// / TestCrossIterator 验证笛卡尔积布局。
func TestCrossIterator(t *testing.T) {
	it := NewCrossIterator([]string{"a", "b"}, []int{1, 2, 3})
	if it.Estimated() != 6 {
		t.Errorf("expected Estimated=6, got %d", it.Estimated())
	}
	var items []Item
	for {
		i, ok := it.Next()
		if !ok {
			break
		}
		items = append(items, i)
	}
	if len(items) != 6 {
		t.Fatalf("expected 6 items, got %d", len(items))
	}
	want := []Item{
		{"a", 1}, {"a", 2}, {"a", 3},
		{"b", 1}, {"b", 2}, {"b", 3},
	}
	for i := range want {
		if items[i] != want[i] {
			t.Errorf("item %d: got %+v, want %+v", i, items[i], want[i])
		}
	}
}

// TestScanner_EndToEnd wires Iterator + Pool + Scanner against a
// real listening port and confirms an Open result lands on the out
// channel. / TestScanner_EndToEnd 把 Iterator + Pool + Scanner 串到
// 真实监听端口，确认 Open 结果落到 out channel。
func TestScanner_EndToEnd(t *testing.T) {
	port := startTCPListener(t)
	probe := NewTCPConnectProbe()
	s := NewScanner(ScanOptions{
		Probe:      probe,
		Timeout:    time.Second,
		Threads:    4,
		MinThreads: 1,
		MaxThreads: 8,
	})
	iter := NewCrossIterator([]string{"127.0.0.1"}, []int{port, port + 1})
	out := make(chan Result, 8)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := s.Run(ctx, iter, out); err != nil {
		t.Fatalf("Run: %v", err)
	}
	// Channel is closed by Scanner; drain to count.
	// Scanner 关闭 channel；排空计数。
	var opens int
	for r := range out {
		if r.State == StateOpen {
			opens++
		}
	}
	if opens != 1 {
		t.Errorf("expected 1 open result, got %d", opens)
	}
}

// TestScanner_CancelMidway verifies that a canceled context stops
// the scan without hanging. / TestScanner_CancelMidway 验证已取消的
// context 让 scan 停止而不挂起。
func TestScanner_CancelMidway(t *testing.T) {
	probe := NewTCPConnectProbe()
	s := NewScanner(ScanOptions{
		Probe:      probe,
		Timeout:    3 * time.Second,
		Threads:    4,
		MinThreads: 1,
		MaxThreads: 4,
	})
	// Use a non-routable target so each probe takes ~200ms. With
	// 100 ports the test will take 100/4 * 200ms = 5s if not canceled.
	// 用不可路由目标让每次 probe 耗时约 200ms。100 个端口不取消的话
	// 要 100/4 * 200ms = 5s。
	ports := make([]int, 100)
	for i := range ports {
		ports[i] = 1 + i
	}
	iter := NewCrossIterator([]string{"192.0.2.1"}, ports)
	out := make(chan Result, 16)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	done := make(chan struct{})
	go func() {
		_ = s.Run(ctx, iter, out)
		close(done)
	}()

	select {
	case <-done:
		// good — Run returned after ctx cancel
	case <-time.After(2 * time.Second):
		t.Fatal("Scanner did not honor ctx cancellation within 2s")
	}
	// Drain the channel. / 排空 channel。
	for range out {
	}
	// Sanity-check helper to silence unused import warning.
	_ = strconv.Itoa(0)
}
