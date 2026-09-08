// errors_test.go — ClassifyError unit tests.
// / ClassifyError 单元测试。
package core

import (
	"context"
	"errors"
	"fmt"
	"net"
	"syscall"
	"testing"
)

func TestClassifyError(t *testing.T) {
	// net.OpError with Timeout() == true should classify as "timeout".
	// / net.OpError 带 Timeout() == true 应归为 "timeout"。
	timeoutErr := &net.OpError{
		Op:  "dial",
		Net: "tcp",
		Err: timeoutTrue{}, // satisfies Timeout() bool
	}
	if got := ClassifyError(timeoutErr); got != "timeout" {
		t.Errorf("net.OpError{Timeout} = %q, want timeout", got)
	}

	// context.DeadlineExceeded wraps to "timeout".
	// / context.DeadlineExceeded 包装为 "timeout"。
	if got := ClassifyError(context.DeadlineExceeded); got != "timeout" {
		t.Errorf("context.DeadlineExceeded = %q, want timeout", got)
	}

	// syscall.ECONNREFUSED → "refused".
	// / syscall.ECONNREFUSED → "refused"。
	if got := ClassifyError(syscall.ECONNREFUSED); got != "refused" {
		t.Errorf("ECONNREFUSED = %q, want refused", got)
	}

	// syscall.ECONNRESET → "reset".
	// / syscall.ECONNRESET → "reset"。
	if got := ClassifyError(syscall.ECONNRESET); got != "reset" {
		t.Errorf("ECONNRESET = %q, want reset", got)
	}

	// syscall.EHOSTUNREACH → "unreach".
	// / syscall.EHOSTUNREACH → "unreach"。
	if got := ClassifyError(syscall.EHOSTUNREACH); got != "unreach" {
		t.Errorf("EHOSTUNREACH = %q, want unreach", got)
	}

	// syscall.ENETUNREACH → "unreach".
	// / syscall.ENETUNREACH → "unreach"。
	if got := ClassifyError(syscall.ENETUNREACH); got != "unreach" {
		t.Errorf("ENETUNREACH = %q, want unreach", got)
	}

	// Plain string fallback cases.
	// / 纯字符串回退。
	stringCases := []struct {
		msg, want string
	}{
		{"connection refused", "refused"},
		{"connection reset by peer", "reset"},
		{"no such host", "dns"},
		{"dns lookup failed", "dns"},
		{"permission denied", "perm"},
		{"operation not permitted", "perm"},
		{"auth failed", "auth"},
		{"tls handshake failure", "tls"},
		{"ssl handshake error", "tls"},
		{"no route to host", "unreach"},
		{"network is unreachable", "unreach"},
		{"i/o timeout", "timeout"},
		{"some random unrelated error", "other"},
	}
	for _, c := range stringCases {
		if got := ClassifyError(errors.New(c.msg)); got != c.want {
			t.Errorf("ClassifyError(%q) = %q, want %q", c.msg, got, c.want)
		}
	}

	// nil error → "".
	// / nil 错误 → ""。
	if got := ClassifyError(nil); got != "" {
		t.Errorf("ClassifyError(nil) = %q, want \"\"", got)
	}

	// Wrapped syscall error via fmt.Errorf with %w.
	// / 用 fmt.Errorf %w 包装的 syscall 错误。
	wrapped := fmt.Errorf("dial: %w", syscall.ECONNREFUSED)
	if got := ClassifyError(wrapped); got != "refused" {
		t.Errorf("wrapped ECONNREFUSED = %q, want refused", got)
	}
}

// timeoutTrue satisfies net.Error with Timeout() true.
// / timeoutTrue 满足 net.Error 且 Timeout() 返回 true。
type timeoutTrue struct{}

func (timeoutTrue) Error() string   { return "i/o timeout" }
func (timeoutTrue) Timeout() bool   { return true }
func (timeoutTrue) Temporary() bool { return true }
