// rawtcp_test.go — unit tests for the RawTCPIdentify helper.
//
// rawtcp_test.go — RawTCPIdentify 助手的单元测试。
package plugins

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/types"
)

// TestRawTCPIdentify_NilFnReturnsNil verifies the nil-fn contract.
// / TestRawTCPIdentify_NilFnReturnsNil 验证 nil-fn 契约。
func TestRawTCPIdentify_NilFnReturnsNil(t *testing.T) {
	got := RawTCPIdentify(context.Background(), "127.0.0.1", 1, nil)
	if got != nil {
		t.Errorf("expected nil for nil fn, got %+v", got)
	}
}

// TestRawTCPIdentify_DialFailureReturnsNil verifies a closed port
// surfaces as nil (not an error). / TestRawTCPIdentify_DialFailureReturnsNil
// 验证关闭端口返回 nil（而非 error）。
func TestRawTCPIdentify_DialFailureReturnsNil(t *testing.T) {
	// Reserve a port and immediately close it so the next dial fails.
	// / 占用一个端口后立即关闭，让下一次拨号失败。
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().(*net.TCPAddr)
	_ = ln.Close()
	got := RawTCPIdentify(context.Background(), addr.IP.String(), addr.Port,
		func(c net.Conn) *types.Result {
			t.Fatal("fn should not be called when dial fails")
			return nil
		},
		WithIdentifyTimeout(500*time.Millisecond))
	if got != nil {
		t.Errorf("expected nil on dial failure, got %+v", got)
	}
}

// TestRawTCPIdentify_PassesConnAndResult verifies the fn is called
// with a live conn and its *Result is returned as-is. /
// TestRawTCPIdentify_PassesConnAndResult 验证 fn 被传入活跃 conn 调
// 用，其 *Result 原样返回。
func TestRawTCPIdentify_PassesConnAndResult(t *testing.T) {
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
		// Echo back "hello" so the test client can verify the conn
		// is live. / 回 "hello" 让测试 client 验证 conn 是活跃的。
		_, _ = c.Write([]byte("hello"))
		_ = c.Close()
	}()
	addr := ln.Addr().(*net.TCPAddr)
	got := RawTCPIdentify(context.Background(), addr.IP.String(), addr.Port,
		func(c net.Conn) *types.Result {
			buf := make([]byte, 5)
			n, err := c.Read(buf)
			if err != nil || n != 5 || string(buf) != "hello" {
				t.Errorf("conn read = (%q, %v), want (\"hello\", nil)", buf, err)
			}
			return &types.Result{Host: "h", Port: 1, Service: "test", Banner: "ok"}
		},
		WithIdentifyTimeout(2*time.Second))
	if got == nil {
		t.Fatal("expected non-nil result")
	}
	if got.Banner != "ok" || got.Service != "test" {
		t.Errorf("result = %+v, want Banner=ok Service=test", got)
	}
}

// TestRawTCPIdentify_DefaultTimeout verifies the default kicks in
// when no option is passed. / TestRawTCPIdentify_DefaultTimeout 验证
// 不传 option 时的默认值。
func TestRawTCPIdentify_DefaultTimeout(t *testing.T) {
	if DefaultIdentifyTimeout != 3*time.Second {
		t.Errorf("DefaultIdentifyTimeout = %v, want 3s", DefaultIdentifyTimeout)
	}
}
