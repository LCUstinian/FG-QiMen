// Package scan: shared helpers.
// Package scan: 共享工具。
package scan

import (
	"errors"
	"net"
	"strings"
)

// isConnRefused reports whether the error looks like an active refusal
// (TCP RST received). On all major platforms, a closed port returns
// ECONNREFUSED. We also catch a few aliases.
//
// isConnRefused 报告错误是否像主动拒绝（收到 TCP RST）。在所有主要
// 平台上，关闭的端口返回 ECONNREFUSED。我们也捕获几个别名。
func isConnRefused(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	if strings.Contains(s, "refused") ||
		strings.Contains(s, "ECONNREFUSED") ||
		strings.Contains(s, "connection refused") ||
		strings.Contains(s, "WSAECONNREFUSED") {
		return true
	}
	// Some platforms wrap the syscall errno. Try unwrapping.
	var sysErr *net.OpError
	if errors.As(err, &sysErr) {
		return isConnRefused(sysErr.Err)
	}
	return false
}
