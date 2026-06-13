// network.go — target parsing (IP / CIDR / range / file).
// network.go — 目标解析（IP / CIDR / 范围 / 文件）。
package types

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"strings"
)

// ExpandTargets accepts a target spec string (IP / CIDR / range /
// comma-list) and a hosts file path, and returns the deduplicated list
// of Target structs.
//
// ExpandTargets 接受目标规格字符串（IP / CIDR / 范围 / 逗号列表）和
// 主机文件路径，返回去重后的 Target 列表。
//
// Supported forms / 支持的格式:
//   - "192.168.1.1"
//   - "192.168.1.0/24"
//   - "192.168.1.1-192.168.1.254"
//   - "192.168.1.1,10.0.0.0/24"
//   - "@/path/to/hosts.txt" (use -hf equivalent by passing via hostsFile)
func ExpandTargets(spec, hostsFile string) ([]Target, error) {
	var out []Target
	seen := make(map[string]struct{})

	add := func(t Target) {
		k := t.Key()
		if k == "" {
			return
		}
		if _, dup := seen[k]; dup {
			return
		}
		seen[k] = struct{}{}
		out = append(out, t)
	}

	if spec != "" {
		for _, piece := range strings.Split(spec, ",") {
			piece = strings.TrimSpace(piece)
			if piece == "" {
				continue
			}
			if err := expandOne(piece, add); err != nil {
				return nil, err
			}
		}
	}
	if hostsFile != "" {
		f, err := os.Open(hostsFile)
		if err != nil {
			return nil, fmt.Errorf("open hosts file: %w", err)
		}
		defer f.Close()
		scanner := bufio.NewScanner(f)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			// strip inline comment / 去掉行内注释
			if i := strings.IndexByte(line, '#'); i >= 0 {
				line = strings.TrimSpace(line[:i])
			}
			if line == "" {
				continue
			}
			if err := expandOne(line, add); err != nil {
				return nil, err
			}
		}
		if err := scanner.Err(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// expandOne dispatches a single token to the right expander based on
// whether it contains '-' (range) or '/' (CIDR) or is a bare IP/host.
// expandOne 把单个 token 根据 '-'（范围）/'/'（CIDR）/裸 IP/主机 分派到对应扩展器。
func expandOne(s string, add func(Target)) error {
	// Bare IP literal? / 裸 IP 字面量？
	if ip := net.ParseIP(s); ip != nil {
		add(Target{Addr: s})
		return nil
	}
	// CIDR? / CIDR？
	if strings.Contains(s, "/") {
		ip, ipnet, err := net.ParseCIDR(s)
		if err != nil {
			return fmt.Errorf("invalid CIDR %q: %w", s, err)
		}
		for ip := ip.Mask(ipnet.Mask); ipnet.Contains(ip); incIP(ip) {
			add(Target{Addr: ip.String()})
		}
		return nil
	}
	// Range? "a.b.c.x-y" or "a.b.c.x-a.b.c.y"
	// 范围：单段范围 "a.b.c.x-y" 或全段范围 "a.b.c.x-a.b.c.y"
	if strings.Contains(s, "-") {
		start, end, err := parseRange(s)
		if err != nil {
			return err
		}
		for cur := start; !cur.Equal(end); incIP(cur) {
			add(Target{Addr: cur.String()})
		}
		add(Target{Addr: end.String()})
		return nil
	}
	// Fallback: treat as hostname.
	// 回退：视为主机名。
	add(Target{Addr: s})
	return nil
}

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
