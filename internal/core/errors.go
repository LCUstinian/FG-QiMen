// errors.go — error classification for TUI error-category
// breakdown (TUI v2 Spec A feature 5).
// / errors.go — TUI 错误分类断点（Spec A 第 5 个 feature）。
//
// ClassifyError maps an error from plugin execution to a stable
// category label. Uses errors.Is/As for known sentinel types
// (context, net.Error, syscall) before falling back to substring
// match on the lowercased error text. Categories are intentionally
// coarse; finer sub-classes can be added without changing the
// aggregation shape.

package core

import (
	"context"
	"errors"
	"net"
	"strings"
	"syscall"
)

// ClassifyError returns one of: "timeout", "refused", "reset",
// "dns", "perm", "auth", "tls", "unreach", "other", or "" for nil.
// / ClassifyError 返回类别标签，nil 返回 ""。
func ClassifyError(err error) string {
	if err == nil {
		return ""
	}
	// 1) Specific sentinel checks via errors.Is/As.
	// / 1) 通过 errors.Is/As 检查特定 sentinel。
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "timeout"
	}
	if errors.Is(err, syscall.ECONNREFUSED) {
		return "refused"
	}
	if errors.Is(err, syscall.ECONNRESET) {
		return "reset"
	}
	if errors.Is(err, syscall.EHOSTUNREACH) ||
		errors.Is(err, syscall.ENETUNREACH) {
		return "unreach"
	}
	// 2) Substring fallback on lowercased text.
	// / 2) 小写文本子串回退。
	s := strings.ToLower(err.Error())
	switch {
	case strings.Contains(s, "timeout"),
		strings.Contains(s, "i/o timeout"):
		return "timeout"
	case strings.Contains(s, "refused"):
		return "refused"
	case strings.Contains(s, "reset"):
		return "reset"
	case strings.Contains(s, "no such host"),
		strings.Contains(s, "dns"):
		return "dns"
	case strings.Contains(s, "permission"),
		strings.Contains(s, "denied"),
		strings.Contains(s, "perm"):
		return "perm"
	case strings.Contains(s, "auth"):
		return "auth"
	case strings.Contains(s, "tls"),
		strings.Contains(s, "ssl"),
		strings.Contains(s, "handshake"):
		return "tls"
	case strings.Contains(s, "unreachable"),
		strings.Contains(s, "no route"):
		return "unreach"
	}
	return "other"
}
