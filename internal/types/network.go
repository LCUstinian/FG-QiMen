// network.go — target parsing (IP / CIDR / range / file).
// network.go — 目标解析（IP / CIDR / 范围 / 文件）。
package types

import (
	"bytes"
	"fmt"
	"math/big"
	"net"
	"strings"
)

// MaxTargets is the upper bound on the number of targets a single
// scan can expand to. M5 audit fix: prevents OOM from huge CIDRs
// (e.g. 10.0.0.0/8 → 16M IPs, 0.0.0.0/0 → 4B IPs).
//
// MaxTargets 是单次扫描可展开目标数的上限。M5 审计修法：防止
// 巨大 CIDR（如 10.0.0.0/8 → 1600万 IP，0.0.0.0/0 → 43亿 IP）导致 OOM。
const MaxTargets = 65536

// ExpandTargets accepts a target spec string (IP / CIDR / range /
// comma-list) and a hosts file path, and returns the deduplicated list
// of Target structs.
//
// M5 audit fix: enforces MaxTargets upper bound to prevent OOM from
// huge CIDR expansions.
//
// v0.4: thin wrapper over ExpandTargetsStream — the streaming iterator
// does the real work; this function just collects into a slice for
// callers that want the full list up front.
//
// ExpandTargets 接受目标规格字符串（IP / CIDR / 范围 / 逗号列表）和
// 主机文件路径，返回去重后的 Target 列表。
//
// M5 审计修法：强制 MaxTargets 上限，防止巨大 CIDR 展开导致 OOM。
// v0.4：薄包装，内部走 ExpandTargetsStream；流式迭代器负责实际工作，
// 本函数只是把结果收集到 slice，给需要全表的调用方用。
//
// Supported forms / 支持的格式:
//   - "192.168.1.1" / "::1" (IPv6 first-class — Phase B of the
//     audit roadmap)
//   - "192.168.1.0/24" / "2001:db8::/64"
//   - "192.168.1.1-192.168.1.254" (IPv4 range; IPv6 ranges use
//     full-form "::1-::ffff" since the bare-octet shorthand
//     doesn't apply to v6)
//   - "192.168.1.1,10.0.0.0/24,::1,fe80::1" (comma list)
//   - "@/path/to/hosts.txt" (use -hf equivalent by passing via hostsFile)
func ExpandTargets(spec, hostsFile string) ([]Target, error) {
	it, err := ExpandTargetsStream(spec, hostsFile)
	if err != nil {
		return nil, err
	}
	out := make([]Target, 0)
	for t, ok := it.Next(); ok; t, ok = it.Next() {
		out = append(out, t)
	}
	if err := it.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// expandOne is removed — replaced by ExpandTargetsStream which
// inlines its dispatch logic directly. / expandOne 已删除——
// 被 ExpandTargetsStream 内联取代。

// parseRange parses "a.b.c.x-y" or "a.b.c.x-a.b.c.y" and returns the
// start and end IPs.
//
// Two malformed inputs are rejected up-front rather than letting the
// expansion loop spin:
//
//  1. end < start — possible when the end is a bare last octet that
//     expands below the start's last octet (e.g. "10.0.0.5-1" →
//     10.0.0.1). Without the guard, the expansion loop runs incIP ~2^32
//     times before wrapping back to end.
//  2. range size > MaxTargets — possible with syntactically valid
//     inputs like "::-8::" that resolve to start=::, end=8::
//     (2^123 addresses) or "0.0.0.0-255.255.255.255" (~2^32). The
//     expansion loop, the MaxTargets streaming guard in tryEmit, and
//     the size pre-count in initOne all become unbounded or useless
//     against such inputs.
//
// Surfacing these as normal parse errors keeps the cost of malformed
// input bounded and gives the caller a useful diagnostic.
//
// parseRange 解析 "a.b.c.x-y" 或 "a.b.c.x-a.b.c.y" 并返回起止 IP。
//
// 提前拒绝两类畸形输入，避免展开循环失控：
//  1. end < start —— 单段 end 展开后小于 start（如 "10.0.0.5-1" →
//     10.0.0.1）。没有护栏则展开循环要跑 ~2³² 次 incIP 才绕回。
//  2. 范围大小 > MaxTargets —— 语法合法但解析出巨大范围，如
//     "::-8::" → start=::、end=8::（2^123 个地址），或
//     "0.0.0.0-255.255.255.255"（~2^32）。对这种输入，展开循环、
//     tryEmit 的 MaxTargets 流式护栏、以及 initOne 的预计数都失去上界。
//
// 将其作为普通解析错误浮出，确保畸形输入开销可控并给出有意义的诊断。
func parseRange(s string) (net.IP, net.IP, error) {
	dash := strings.IndexByte(s, '-')
	if dash < 0 {
		return nil, nil, fmt.Errorf("invalid range %q", s)
	}
	startStr := strings.TrimSpace(s[:dash])
	endStr := strings.TrimSpace(s[dash+1:])

	startIP := net.ParseIP(startStr)
	if startIP == nil {
		return nil, nil, fmt.Errorf("invalid range start %q", startStr)
	}
	// End can be a bare last octet or a full IP.
	// 结束 IP 可以是最后一段数字或完整 IP。
	endIP := net.ParseIP(endStr)
	if endIP == nil {
		// Try expanding single-octet form: "192.168.1.1-254" → "192.168.1.254"
		// 尝试单段扩展："192.168.1.1-254" → "192.168.1.254"
		if idx := strings.LastIndexByte(startStr, '.'); idx >= 0 {
			endIP = net.ParseIP(startStr[:idx+1] + endStr)
		}
	}
	if endIP == nil {
		return nil, nil, fmt.Errorf("invalid range end %q", endStr)
	}
	if ipLess(endIP, startIP) {
		return nil, nil, fmt.Errorf("invalid range %q: end %s precedes start %s", s, endIP, startIP)
	}
	// Reject ranges whose address count exceeds MaxTargets. The
	// pre-count in initOne and the emit loop in emitOne would
	// otherwise iterate an astronomical number of times before the
	// MaxTargets streaming cap in tryEmit ever fires. / 拒绝地址数
	// 超过 MaxTargets 的范围。否则 initOne 的预计数和 emitOne 的发
	// 送循环会在 tryEmit 的 MaxTargets 上限生效前遍历天文级数。
	if size, ok := rangeSize(startIP, endIP); ok && size > MaxTargets {
		return nil, nil, fmt.Errorf("invalid range %q: %d addresses exceeds MaxTargets=%d (use a smaller range or split the scan)", s, size, MaxTargets)
	}
	return startIP, endIP, nil
}

// rangeSize returns the number of addresses in [start, end] inclusive.
// Returns (0, false) if the IPs are not the same family/length. Uses
// big.Int arithmetic so IPv6 ranges (up to 2^128) don't overflow.
// / rangeSize 返回 [start, end] 闭区间内的地址数。若两个 IP 不是同族
// 或长度不同则返回 (0, false)。用 big.Int 算术避免 IPv6 范围（最大
// 2^128）溢出。
func rangeSize(start, end net.IP) (int, bool) {
	a16, b16 := start.To16(), end.To16()
	if a16 == nil || b16 == nil || len(a16) != len(b16) {
		return 0, false
	}
	a := new(big.Int).SetBytes(a16)
	b := new(big.Int).SetBytes(b16)
	diff := new(big.Int).Sub(b, a)
	if !diff.IsInt64() {
		// Overflows int64 (i.e. > 2^63-1). Still safe to report
		// "exceeds MaxTargets" since MaxTargets is small.
		// / 超过 int64 上限（即 > 2^63-1）。但仍可报告"超
		// 过 MaxTargets"——上限小。
		return MaxTargets + 1, true
	}
	return int(diff.Int64()) + 1, true
}

// ipLess reports whether a is strictly less than b. Both are normalised
// to 16-byte form so v4 and v6 compare in a single byte ordering.
// / ipLess 报告 a 是否严格小于 b。两者均归一为 16 字节形式以便统一
// 比较。
func ipLess(a, b net.IP) bool {
	a16, b16 := a.To16(), b.To16()
	if a16 == nil || b16 == nil {
		// ParseIP always yields a To16-able IP; this is defensive.
		// ParseIP 总会产生可 To16 的 IP；此处为防御。
		return false
	}
	return bytes.Compare(a16, b16) < 0
}

// incIP increments an IP in place (handles both v4 and v6).
// incIP 原地递增一个 IP（同时处理 v4 和 v6）。
func incIP(ip net.IP) {
	for j := len(ip) - 1; j >= 0; j-- {
		ip[j]++
		if ip[j] > 0 {
			break
		}
	}
}
