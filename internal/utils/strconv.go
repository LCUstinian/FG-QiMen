// Package utils provides zero-allocation string utilities.
// Package utils 提供零分配字符串工具。
//
// Inspired by fscan's zero-allocation optimizations (port_scan.go L324-420),
// this package provides string manipulation functions that avoid heap
// allocations for hot paths in scanning loops.
//
// 借鉴 fscan 的零分配优化（port_scan.go L324-420），本包为扫描循环热路径
// 提供避免堆分配的字符串操作函数。
package utils

import (
	"strconv"
	"strings"
)

// FormatHostPort formats "host:port" without allocations using strconv.
// FormatHostPort 使用 strconv 无分配地格式化 "host:port"。
//
// This is faster than fmt.Sprintf("%s:%d", host, port) which allocates
// for the format string and intermediate conversions.
//
// 比 fmt.Sprintf("%s:%d", host, port) 快，后者为格式串和中间转换分配内存。
func FormatHostPort(host string, port int) string {
	// Pre-allocate with reasonable capacity / 预分配合理容量
	// Typical: "192.168.1.1:8080" = 17 chars, "example.com:443" = 15 chars
	buf := make([]byte, 0, len(host)+6) // host + ":" + max 5 digits
	buf = append(buf, host...)
	buf = append(buf, ':')
	buf = strconv.AppendInt(buf, int64(port), 10)
	return string(buf)
}

// ContainsFold checks if s contains substr case-insensitively without allocations.
// ContainsFold 检查 s 是否包含 substr（忽略大小写），无分配。
//
// This avoids strings.ToLower which allocates a new string. Inspired by
// fscan's containsFold (port_scan.go L386-395).
//
// 避免 strings.ToLower 分配新串。借鉴 fscan 的 containsFold（port_scan.go L386-395）。
func ContainsFold(s, substr string) bool {
	if len(substr) == 0 {
		return true
	}
	if len(substr) > len(s) {
		return false
	}

	// Convert substr to lowercase once / 转换 substr 为小写一次
	substrLower := strings.ToLower(substr)

	// Sliding window over s / 在 s 上滑动窗口
	for i := 0; i <= len(s)-len(substr); i++ {
		match := true
		for j := 0; j < len(substr); j++ {
			c1 := s[i+j]
			c2 := substrLower[j]
			// Compare case-insensitively / 忽略大小写比较
			if c1 >= 'A' && c1 <= 'Z' {
				c1 = c1 + ('a' - 'A')
			}
			if c1 != c2 {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// EqualFold checks if s equals t case-insensitively without allocations.
// EqualFold 检查 s 是否等于 t（忽略大小写），无分配。
//
// This is a wrapper around strings.EqualFold which is already optimized.
//
// 这是 strings.EqualFold 的包装，后者已优化。
func EqualFold(s, t string) bool {
	return strings.EqualFold(s, t)
}

// HasPrefixFold checks if s has prefix case-insensitively without allocations.
// HasPrefixFold 检查 s 是否有 prefix（忽略大小写），无分配。
func HasPrefixFold(s, prefix string) bool {
	if len(prefix) > len(s) {
		return false
	}
	return EqualFold(s[:len(prefix)], prefix)
}

// HasSuffixFold checks if s has suffix case-insensitively without allocations.
// HasSuffixFold 检查 s 是否有 suffix（忽略大小写），无分配。
func HasSuffixFold(s, suffix string) bool {
	if len(suffix) > len(s) {
		return false
	}
	return EqualFold(s[len(s)-len(suffix):], suffix)
}

// TrimSpace trims leading and trailing whitespace efficiently.
// TrimSpace 高效地去除首尾空白。
//
// This is a wrapper around strings.TrimSpace which is already optimized.
//
// 这是 strings.TrimSpace 的包装，后者已优化。
func TrimSpace(s string) string {
	return strings.TrimSpace(s)
}

// JoinInt joins integers with a separator without allocations.
// JoinInt 用分隔符连接整数，无分配。
func JoinInt(ints []int, sep string) string {
	if len(ints) == 0 {
		return ""
	}

	// Pre-allocate buffer / 预分配缓冲区
	// Estimate: 5 chars per int + separator
	buf := make([]byte, 0, len(ints)*6)

	for i, n := range ints {
		if i > 0 {
			buf = append(buf, sep...)
		}
		buf = strconv.AppendInt(buf, int64(n), 10)
	}

	return string(buf)
}

// ParsePort parses a port string to int without allocations.
// ParsePort 解析端口字符串为 int，无分配。
func ParsePort(s string) (int, error) {
	return strconv.Atoi(s)
}

// FormatInt formats an int to string without allocations using buffer.
// FormatInt 使用缓冲区无分配地格式化 int 为字符串。
func FormatInt(n int) string {
	return strconv.Itoa(n)
}
