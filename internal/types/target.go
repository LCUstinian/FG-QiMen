// target.go — target type and unique key derivation.
// target.go — 目标类型与唯一键派生。
package types

import "strings"

// Target represents a single network target (IP or hostname).
// Target 表示单个网络目标（IP 或主机名）。
type Target struct {
	// Addr is the network address (IPv4/IPv6 literal or hostname).
	// Addr 是网络地址（IPv4/IPv6 字面量或主机名）。
	Addr string
	// Tag is an optional label from the input (e.g. comment after IP).
	// Tag 是输入中的可选标签（如 IP 后的注释）。
	Tag string
}

// Key returns a stable identifier for the target (the address itself,
// trimmed, lowercased).
// Key 返回目标的稳定标识符（地址本身，去空白，标准化）。
func (t Target) Key() string {
	return strings.ToLower(strings.TrimSpace(t.Addr))
}
