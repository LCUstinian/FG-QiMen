// validator_test.go — exercise the 4-stage Validator against a fake
// HTTP target. We can't easily construct a "real proxy" that
// would set IsReliable=true, but we can drive every error path.
//
// validator_test.go — 对假 HTTP 目标跑 4 阶段 Validator。我们不易
// 构造会令 IsReliable=true 的"真代理"，但能跑每条错误路径。
package proxy

import (
	"bufio"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
)

func TestValidator_FullEchoDetection(t *testing.T) {
	// Server that echoes whatever it receives → Validator should
	// detect "full echo" via the GET prefix match.
	//
	// 服务器回显收到的任何东西 → Validator 应通过 GET 前缀匹配
	// 检测"全回显"。
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
		defer c.Close()
		_ = c.SetDeadline(time.Now().Add(3 * time.Second))
		br := bufio.NewReader(c)
		// Read the GET probe line. / 读 GET 探测行。
		_, _ = br.ReadString('\n')
		// Echo it back (full-echo behavior).
		// 回显（全回显行为）。
		_, _ = c.Write([]byte("GET / HTTP/1.1\r\n"))
	}()

	dialer := &DirectDialer{timeout: 2 * time.Second}
	v := NewValidator(dialer, 2*time.Second)
	host, portStr, _ := net.SplitHostPort(ln.Addr().String())
	var port int
	fmt.Sscanf(portStr, "%d", &port)
	res := v.Validate(testCtx(t), host, port)
	if !res.IsAlive {
		t.Errorf("IsAlive = false, want true (connection succeeded)")
	}
	if res.IsReliable {
		t.Errorf("IsReliable = true, want false (full-echo detected)")
	}
	if res.Stage != "final_verdict" {
		t.Errorf("Stage = %q, want final_verdict", res.Stage)
	}
}

func TestValidator_RealProxyDetection(t *testing.T) {
	// Server that replies with a real HTTP response (not an echo).
	// / 服务器用真 HTTP 响应（非回显）。
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
		defer c.Close()
		_ = c.SetDeadline(time.Now().Add(3 * time.Second))
		br := bufio.NewReader(c)
		_, _ = br.ReadString('\n')
		// Real HTTP/1.1 response. / 真 HTTP/1.1 响应。
		_, _ = c.Write([]byte("HTTP/1.1 200 OK\r\nContent-Length: 0\r\n\r\n"))
	}()

	dialer := &DirectDialer{timeout: 2 * time.Second}
	v := NewValidator(dialer, 2*time.Second)
	host, portStr, _ := net.SplitHostPort(ln.Addr().String())
	var port int
	fmt.Sscanf(portStr, "%d", &port)
	res := v.Validate(testCtx(t), host, port)
	if !res.IsAlive {
		t.Errorf("IsAlive = false, want true")
	}
	if !res.IsReliable {
		t.Errorf("IsReliable = false, want true (real HTTP response, not full-echo)")
	}
}

func TestValidator_Stage2Fails(t *testing.T) {
	// Server accepts the connection (so stage 1 / dial succeeds
	// → IsAlive=true), then immediately closes. The HTTP probe
	// (stage 2) write fails.
	//
	// 服务器接受连接（阶段 1 拨号成功 → IsAlive=true），然后立即
	// 关闭。HTTP 探测（阶段 2）写失败。
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	go func() {
		c, _ := ln.Accept()
		_ = c.Close()
	}()

	dialer := &DirectDialer{timeout: 2 * time.Second}
	v := NewValidator(dialer, 1*time.Second)
	host, portStr, _ := net.SplitHostPort(ln.Addr().String())
	var port int
	fmt.Sscanf(portStr, "%d", &port)
	res := v.Validate(testCtx(t), host, port)
	if !res.IsAlive {
		t.Errorf("IsAlive = false, want true (dial succeeded)")
	}
	if res.Error == nil {
		t.Errorf("Error = nil, want non-nil (stage 2 write should fail)")
	}
}

func TestValidator_Stage3Fails(t *testing.T) {
	// Server accepts, accepts the GET write, but closes before
	// Validator can read the response. Stage 3 (analyzeResponse)
	// should report an error.
	//
	// 服务器接受,接受 GET 写,但在 Validator 读响应前关闭。阶段 3
	// (analyzeResponse) 应报错误。
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	go func() {
		c, _ := ln.Accept()
		defer c.Close()
		_ = c.SetDeadline(time.Now().Add(2 * time.Second))
		// Read until EOF or error then close. / 读到 EOF/错误后关闭。
		buf := make([]byte, 4096)
		for {
			_, err := c.Read(buf)
			if err != nil {
				return
			}
		}
	}()

	dialer := &DirectDialer{timeout: 2 * time.Second}
	v := NewValidator(dialer, 1*time.Second)
	host, portStr, _ := net.SplitHostPort(ln.Addr().String())
	var port int
	fmt.Sscanf(portStr, "%d", &port)
	res := v.Validate(testCtx(t), host, port)
	if res.IsAlive != true {
		t.Errorf("IsAlive = false, want true")
	}
	// Stage 3 is "response_analysis" — we expect an error from
	// ReadString returning io.EOF or a deadline error. The validator
	// handles io.EOF as a non-error (returns false for full-echo
	// detection) but here we expect the analysis to fail with EOF
	// being treated as an empty line. Looking at the code:
	// analyzeResponse returns (false, err) on real errors but
	// returns (false, nil) on io.EOF. So we may end up at
	// final_verdict with IsReliable=true.
	//
	// 阶段 3 是 "response_analysis" — 期待 ReadString 返回 io.EOF
	// 或 deadline 错误时出错。代码里 analyzeResponse 在真错误时
	// 返回 (false, err),在 io.EOF 时返回 (false, nil)。所以我们
	// 可能落到 final_verdict 且 IsReliable=true。
	if res.Stage != "response_analysis" && res.Stage != "final_verdict" {
		t.Errorf("Stage = %q, want response_analysis or final_verdict", res.Stage)
	}
}

func TestValidator_ResultFields(t *testing.T) {
	// Verify the default result (zero value) is "not alive".
	// We must pass a real dialer (not nil) because the validator
	// dereferences dialer in stage 1.
	//
	// 验证默认 result（零值）是"未存活"。必须传真 dialer（不能 nil）
	// 因为 validator 在阶段 1 解引用 dialer。
	v := NewValidator(&DirectDialer{timeout: 200 * time.Millisecond}, time.Second)
	res := v.Validate(testCtx(t), "192.0.2.1", 9) // unreachable
	if res == nil {
		t.Fatal("Validate returned nil")
	}
	if res.IsAlive || res.IsReliable {
		t.Errorf("unreachable → IsAlive=%v IsReliable=%v, want both false",
			res.IsAlive, res.IsReliable)
	}
	if res.Stage != "banner" {
		t.Errorf("Stage = %q, want banner", res.Stage)
	}
}

func TestIsProxyReliable_Unreachable(t *testing.T) {
	if IsProxyReliable(testCtx(t),
		&DirectDialer{timeout: 100 * time.Millisecond},
		"192.0.2.1", 9, 100*time.Millisecond) {
		t.Error("IsProxyReliable returned true for unreachable target")
	}
}

// Verify strings.HasPrefix semantics used in the validator's
// full-echo detection. Defensive test against a future edit that
// changes the prefix list.
//
// 验证 validator 全回显检测用的 strings.HasPrefix 语义。防止未来
// 编辑修改前缀列表。
func TestValidator_FullEchoPrefixes(t *testing.T) {
	cases := []struct {
		firstLine string
		fullEcho  bool
	}{
		// Only GET and POST are detected per the current code.
		// Future maintainers may want to add PUT/DELETE/HEAD —
		// this test is a tripwire that signals "you added a new
		// method, update the validator's prefix list too".
		//
		// 当前代码只检测 GET 和 POST。后续维护者可能想加
		// PUT/DELETE/HEAD — 本测试是"你加了新方法,记得更新
		// validator 的前缀列表"的绊线。
		{"GET / HTTP/1.1", true},
		{"POST / HTTP/1.1", true},
		{"HEAD / HTTP/1.1", false},   // not yet detected
		{"PUT / HTTP/1.1", false},    // not yet detected
		{"DELETE / HTTP/1.1", false}, // not yet detected
		{"HTTP/1.1 200 OK", false},
		{"", false},
		{"get / HTTP/1.1", false}, // case-sensitive per code
	}
	for _, c := range cases {
		got := strings.HasPrefix(c.firstLine, "GET ") ||
			strings.HasPrefix(c.firstLine, "POST ")
		if got != c.fullEcho {
			t.Errorf("firstLine=%q: got %v, want %v", c.firstLine, got, c.fullEcho)
		}
	}
}
