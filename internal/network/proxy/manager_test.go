// manager_test.go — round-trip tests for the proxy Manager and global
// singleton lifecycle. Per fscan's proxy_*.go test patterns but trimmed
// to the cases that exercise FG-QiMen's actual code paths.
//
// manager_test.go — proxy Manager 和全局单例生命周期的往返测试。
// 借鉴 fscan 的 proxy_*.go 测试模式，但裁剪到实际覆盖 FG-QiMen 代码
// 路径的用例。
package proxy

import (
	"net"
	"sync"
	"testing"
	"time"
)

// startEchoTCP starts a tiny TCP echo server on a random localhost port
// and returns its address. The server accepts connections, reads until
// EOF or newline, and writes everything back. Used as a fake SOCKS5 /
// HTTP proxy / arbitrary target by tests that just need "something on
// TCP that responds".
//
// startEchoTCP 在随机本地端口启动一个小型 TCP echo 服务并返回其地址。
// 服务接受连接，读到 EOF 或换行后回写所有数据。供那些只需"TCP 上有响应"
// 的测试作为假 SOCKS5 / HTTP 代理 / 任意目标使用。
func startEchoTCP(t *testing.T) (addr string, stop func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	stopCh := make(chan struct{})
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 4096)
				for {
					n, err := c.Read(buf)
					if n > 0 {
						_, _ = c.Write(buf[:n])
					}
					if err != nil {
						return
					}
				}
			}(conn)
		}
	}()
	stop = func() {
		close(stopCh)
		_ = ln.Close()
	}
	return ln.Addr().String(), stop
}

func TestManager_GetDialer_DirectDefault(t *testing.T) {
	// NewManager(nil) → direct dialer.
	// / NewManager(nil) → 直连拨号器。
	m := NewManager(nil)
	d, err := m.GetDialer()
	if err != nil {
		t.Fatalf("GetDialer: %v", err)
	}
	if _, ok := d.(*DirectDialer); !ok {
		t.Fatalf("default dialer is %T, want *DirectDialer", d)
	}
}

func TestManager_GetDialer_SingletonInit(t *testing.T) {
	// Manager.GetDialer is sync.Once-guarded. Calling it 100x concurrently
	// must yield the same instance and not race.
	//
	// Manager.GetDialer 受 sync.Once 保护。并发调 100 次必须返回同一
	// 实例且不数据竞争。
	m := NewManager(DefaultProxyConfig())
	const N = 100
	results := make([]Dialer, N)
	var wg sync.WaitGroup
	wg.Add(N)
	for i := 0; i < N; i++ {
		i := i
		go func() {
			defer wg.Done()
			d, err := m.GetDialer()
			if err != nil {
				t.Errorf("GetDialer: %v", err)
				return
			}
			results[i] = d
		}()
	}
	wg.Wait()
	for i := 1; i < N; i++ {
		if results[i] != results[0] {
			t.Fatalf("instance %d differs from instance 0", i)
		}
	}
}

func TestDirectDialer_DialContext_Loopback(t *testing.T) {
	addr, stop := startEchoTCP(t)
	defer stop()

	d := &DirectDialer{timeout: 2 * time.Second}
	conn, err := d.DialContext(testCtx(t), "tcp", addr)
	if err != nil {
		t.Fatalf("DialContext: %v", err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte("ping")); err != nil {
		t.Fatalf("write: %v", err)
	}
	buf := make([]byte, 4)
	if _, err := conn.Read(buf); err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(buf) != "ping" {
		t.Errorf("echo got %q, want %q", buf, "ping")
	}
}

func TestDirectDialer_DialContext_RejectsUnreachable(t *testing.T) {
	// Reserved test-only address (TEST-NET-1, RFC 5737) — guaranteed
	// not to route. Connection should fail.
	//
	// 保留测试地址（TEST-NET-1，RFC 5737）— 保证不可路由。连接应失败。
	d := &DirectDialer{timeout: 200 * time.Millisecond}
	_, err := d.DialContext(testCtx(t), "tcp", "192.0.2.1:9")
	if err == nil {
		t.Fatal("DialContext succeeded against unreachable address")
	}
}

func TestDirectDialer_LocalAddrBinding(t *testing.T) {
	// --iface with a real local IP should be reflected into the dialer's
	// LocalAddr (we can't easily verify the kernel actually bound
	// without a custom listener, but we can verify the parsed IP
	// doesn't error and the dial proceeds).
	//
	// --iface 用真实本地 IP 应反映到 dialer 的 LocalAddr（不挂自定义
	// listener 不易验证 kernel 是否真绑定，但能验证解析 IP 不报错且
	// 拨号继续）。
	addr, stop := startEchoTCP(t)
	defer stop()
	d := &DirectDialer{timeout: 2 * time.Second, localAddr: "127.0.0.1"}
	conn, err := d.DialContext(testCtx(t), "tcp", addr)
	if err != nil {
		t.Fatalf("DialContext with localAddr: %v", err)
	}
	_ = conn.Close()
}

func TestDirectDialer_LocalAddrInvalidIgnored(t *testing.T) {
	// Garbage --iface is non-fatal: parser returns nil ip → skip
	// the LocalAddr assignment, fall through to default.
	//
	// 乱填 --iface 不致命：解析返回 nil ip → 跳过 LocalAddr 赋值，
	// 退回默认。
	addr, stop := startEchoTCP(t)
	defer stop()
	d := &DirectDialer{timeout: 2 * time.Second, localAddr: "not-an-ip"}
	conn, err := d.DialContext(testCtx(t), "tcp", addr)
	if err != nil {
		t.Fatalf("DialContext: %v", err)
	}
	_ = conn.Close()
}

func TestNewHTTPDialer_RejectsEmpty(t *testing.T) {
	_, err := NewHTTPDialer(&ProxyConfig{Type: ProxyTypeHTTP, Address: ""})
	if err == nil {
		t.Fatal("NewHTTPDialer(empty) accepted; want error")
	}
}

func TestNewSOCKS5Dialer_RejectsEmpty(t *testing.T) {
	_, err := NewSOCKS5Dialer(&ProxyConfig{Type: ProxyTypeSOCKS5, Address: ""})
	if err == nil {
		t.Fatal("NewSOCKS5Dialer(empty) accepted; want error")
	}
}

func TestCreateDialer_Unsupported(t *testing.T) {
	// Unknown proxy type → error from createDialer (called by
	// GetDialer).
	//
	// 未知代理类型 → createDialer 返回错误（由 GetDialer 调用）。
	m := NewManager(&ProxyConfig{Type: ProxyType("garbage"), Address: "1.2.3.4:5"})
	// Note: GetDialer uses sync.Once — once it errors, retries
	// return the same error. This is fine for production; the
	// error is the same in both cases.
	//
	// 注意：GetDialer 用 sync.Once —— 一旦错误,重试返回同样错误。
	// 生产中没问题；两次错误相同。
	_, err := m.GetDialer()
	if err == nil {
		t.Fatal("createDialer accepted garbage proxy type")
	}
}

func TestResetGlobalManager_Reinit(t *testing.T) {
	// ResetGlobalManager must let us re-init with a different config
	// (this is the path tests use to avoid leaking state between
	// subtests).
	//
	// ResetGlobalManager 必须允许用不同 config 重新初始化（这是测试
	// 在子测试间避免状态泄漏的路径）。
	InitGlobalManager(DefaultProxyConfig())
	d1, err := GetGlobalDialer()
	if err != nil {
		t.Fatalf("GetGlobalDialer 1: %v", err)
	}
	ResetGlobalManager()
	InitGlobalManager(&ProxyConfig{Type: ProxyTypeSOCKS5, Address: "127.0.0.1:1080"})
	d2, err := GetGlobalDialer()
	if err != nil {
		t.Fatalf("GetGlobalDialer 2: %v", err)
	}
	if d1 == d2 {
		t.Errorf("expected different dialer instances after Reset")
	}
	// Cleanup for downstream tests. / 为下游测试清理。
	ResetGlobalManager()
}

func TestProxyConfig_IsEnabled(t *testing.T) {
	// IsEnabled is a method on *ProxyConfig, so a nil receiver panics
	// (Go semantics). Callers must not pass nil — DefaultProxyConfig()
	// always returns a valid value. We only test the value cases.
	//
	// IsEnabled 是 *ProxyConfig 的方法,nil 接收者会 panic(Go 语义)。
	// 调用方不应传 nil —— DefaultProxyConfig() 总返回合法值。
	// 只测值情况。
	cases := []struct {
		name string
		c    *ProxyConfig
		want bool
	}{
		{"none", &ProxyConfig{Type: ProxyTypeNone}, false},
		{"none-with-addr", &ProxyConfig{Type: ProxyTypeNone, Address: "1.2.3.4:5"}, false},
		{"http-no-addr", &ProxyConfig{Type: ProxyTypeHTTP}, false},
		{"http-with-addr", &ProxyConfig{Type: ProxyTypeHTTP, Address: "1.2.3.4:5"}, true},
		{"socks5-with-addr", &ProxyConfig{Type: ProxyTypeSOCKS5, Address: "1.2.3.4:5"}, true},
		{"https-with-addr", &ProxyConfig{Type: ProxyTypeHTTPS, Address: "1.2.3.4:5"}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.c.IsEnabled(); got != c.want {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}

func TestValidateHTTPAddress(t *testing.T) {
	// HTTP validator is intentionally lenient (port range not checked;
	// any "host:port" that splits is accepted). This matches how most
	// HTTP libraries parse — port validation is left to the connect
	// attempt. The strict SOCKS5 validator is the one that rejects
	// out-of-range ports.
	//
	// HTTP 验证器刻意宽松（不检查端口范围；任何能 split 的
	// "host:port" 都接受）。这与多数 HTTP 库一致——端口校验交给连接
	// 尝试。严格 SOCKS5 验证器才是拒绝越界端口的。
	cases := []struct {
		in      string
		wantErr bool
	}{
		{"", true},
		{"1.2.3.4:8080", false},
		{"proxy.example.com:3128", false},
		{"noport", true},      // missing colon → no port
		{":8080", true},       // empty host
		{"host:99999", false}, // split succeeds; range unchecked
		{"host:abc", false},   // split succeeds; numeric unchecked
		{"host:0", false},     // split succeeds
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			err := ValidateHTTPAddress(c.in)
			if (err != nil) != c.wantErr {
				t.Errorf("ValidateHTTPAddress(%q) err=%v, wantErr=%v", c.in, err, c.wantErr)
			}
		})
	}
}

func TestValidateSOCKS5Address(t *testing.T) {
	cases := []struct {
		in      string
		wantErr bool
	}{
		{"", true},
		{"1.2.3.4:1080", false},
		{"noport", true},
		{":1080", true},
		{"host:abc", true},
		{"host:0", true}, // SOCKS5 validates port range 1..65535
		{"host:99999", true},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			err := ValidateSOCKS5Address(c.in)
			if (err != nil) != c.wantErr {
				t.Errorf("ValidateSOCKS5Address(%q) err=%v, wantErr=%v", c.in, err, c.wantErr)
			}
		})
	}
}

func TestDefaultProxyConfig(t *testing.T) {
	c := DefaultProxyConfig()
	if c == nil {
		t.Fatal("DefaultProxyConfig returned nil")
	}
	if c.Type != ProxyTypeNone {
		t.Errorf("Type = %q, want none", c.Type)
	}
	if c.Timeout <= 0 {
		t.Errorf("Timeout = %v, want > 0", c.Timeout)
	}
	if c.IsEnabled() {
		t.Errorf("default config IsEnabled = true, want false")
	}
}

func TestNewValidator_DefaultTimeout(t *testing.T) {
	v := NewValidator(nil, 0) // 0 → 5s default
	if v.timeout != 5*time.Second {
		t.Errorf("default timeout = %v, want 5s", v.timeout)
	}
}

func TestNewValidator_ExplicitTimeout(t *testing.T) {
	v := NewValidator(nil, 250*time.Millisecond)
	if v.timeout != 250*time.Millisecond {
		t.Errorf("timeout = %v, want 250ms", v.timeout)
	}
}

func TestValidator_Validate_Stage1Fails(t *testing.T) {
	// Unreachable address → IsAlive=false, Stage="banner", Error non-nil.
	// 不可达地址 → IsAlive=false, Stage="banner", Error 非空。
	v := NewValidator(&DirectDialer{timeout: 200 * time.Millisecond}, 200*time.Millisecond)
	res := v.Validate(testCtx(t), "192.0.2.1", 9)
	if res.IsAlive {
		t.Errorf("IsAlive = true, want false")
	}
	if res.Stage != "banner" {
		t.Errorf("Stage = %q, want banner", res.Stage)
	}
	if res.Error == nil {
		t.Errorf("Error = nil, want non-nil")
	}
}

func TestIsProxyReliable_Shortcuts(t *testing.T) {
	// IsProxyReliable is the convenience function. We can't easily
	// get a "real proxy" in a unit test, but we can verify the path
	// returns false (the unreachable case) without crashing.
	//
	// IsProxyReliable 是便捷函数。单元测试中不易获得"真代理",
	// 但能验证不可达路径返回 false 且不崩溃。
	if IsProxyReliable(testCtx(t), &DirectDialer{timeout: 100 * time.Millisecond}, "192.0.2.1", 9, 100*time.Millisecond) {
		t.Errorf("IsProxyReliable returned true for unreachable target")
	}
}
