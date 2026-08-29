// network.go — target parsing (IP / CIDR / range / file).
// network.go — 目标解析（IP / CIDR / 范围 / 文件）。
package types

import (
	"fmt"
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
// parseRange 解析 "a.b.c.x-y" 或 "a.b.c.x-a.b.c.y" 并返回起止 IP。
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
	return startIP, endIP, nil
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
