// hostmatch.go — exclude-hosts matcher (borrowed from fscan's
// -eh/-ehf hostMatcher, minus the streaming iterator: FG-QiMen filters
// the materialized target list instead, which keeps ExpandTargets
// signature-stable).
//
// hostmatch.go — 排除主机匹配器（借鉴 fscan 的 -eh/-ehf hostMatcher，
// 去掉流式迭代器部分：FG-QiMen 直接过滤已展开的目标列表，保持
// ExpandTargets 签名不变）。
//
// Entry syntax (comma-separated, same as fscan):
//
//	192                → 192.168.0.0/16 (RFC1918 shortcut)
//	172                → 172.16.0.0/12  (RFC1918 shortcut)
//	10                 → 10.0.0.0/8     (RFC1918 shortcut)
//	192.168.1.0/24     → CIDR
//	192.168.1.1-192.168.1.9  → full IP range
//	192.168.1.1-9      → last-octet range
//	192.168.1.1        → exact IP
//	nas.local          → exact hostname (case-insensitive)
//
// 条目语法（逗号分隔，与 fscan 一致）：
//
//	192                → 192.168.0.0/16（RFC1918 快捷）
//	172                → 172.16.0.0/12（RFC1918 快捷）
//	10                 → 10.0.0.0/8（RFC1918 快捷）
//	192.168.1.0/24     → CIDR
//	192.168.1.1-192.168.1.9  → 完整 IP 范围
//	192.168.1.1-9      → 末八位组范围
//	192.168.1.1        → 精确 IP
//	nas.local          → 精确主机名（大小写不敏感）
package types

import (
	"bufio"
	"fmt"
	"net/netip"
	"os"
	"strconv"
	"strings"
)

// HostMatcher tests addresses against the exclusion set.
// HostMatcher 对地址做排除集匹配测试。
type HostMatcher struct {
	exact  map[netip.Addr]struct{} // exact IPv4/IPv6 / 精确 IPv4/IPv6
	ranges []ipRange               // inclusive uint32 ranges / 闭区间 uint32 范围
	cidrs  []netip.Prefix          // CIDR blocks / CIDR 块
	names  map[string]struct{}     // hostnames (lowercased) / 主机名（小写）
}

// ipRange is an inclusive [start, end] IPv4 span.
// ipRange 是闭区间 [start, end] 的 IPv4 区间。
type ipRange struct{ start, end uint32 }

// NewHostMatcher parses a comma-separated exclude spec. Unknown
// entries return an error (fail fast — a typo in an exclude list must
// not silently scan the host the operator meant to skip).
//
// NewHostMatcher 解析逗号分隔的排除 spec。无法识别的条目返回错误
// （快速失败——排除列表里的笔误绝不能导致静默扫描本想跳过的主机）。
func NewHostMatcher(spec string) (*HostMatcher, error) {
	m := &HostMatcher{
		exact: map[netip.Addr]struct{}{},
		names: map[string]struct{}{},
	}
	for _, entry := range strings.Split(spec, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if err := m.add(entry); err != nil {
			return nil, err
		}
	}
	return m, nil
}

// add parses one exclude entry. / add 解析一个排除条目。
func (m *HostMatcher) add(entry string) error {
	switch {
	// fscan's RFC1918 shortcuts. / fscan 的 RFC1918 快捷写法。
	case entry == "192":
		m.addCIDR(mustPrefix("192.168.0.0/16"))
	case entry == "172":
		m.addCIDR(mustPrefix("172.16.0.0/12"))
	case entry == "10":
		m.addCIDR(mustPrefix("10.0.0.0/8"))
	case strings.Contains(entry, "/"):
		p, err := netip.ParsePrefix(entry)
		if err != nil {
			return fmt.Errorf("exclude-hosts: invalid CIDR %q: %w", entry, err)
		}
		m.addCIDR(p)
	case strings.Contains(entry, "-") && !strings.Contains(entry, ":"):
		lo, hi, err := parseIPRange(entry)
		if err != nil {
			return fmt.Errorf("exclude-hosts: invalid range %q: %w", entry, err)
		}
		m.ranges = append(m.ranges, ipRange{lo, hi})
	default:
		if addr, err := netip.ParseAddr(entry); err == nil {
			m.exact[addr.Unmap()] = struct{}{}
		} else {
			// Not an IP literal → hostname. / 非 IP 字面量 → 主机名。
			m.names[strings.ToLower(entry)] = struct{}{}
		}
	}
	return nil
}

func mustPrefix(s string) netip.Prefix {
	p, _ := netip.ParsePrefix(s)
	return p
}

// addCIDR stores a CIDR, narrowing v6-mapped prefixes to pure IPv4
// where possible. / addCIDR 存储 CIDR，v4-mapped 前缀尽可能收窄为纯
// IPv4。
func (m *HostMatcher) addCIDR(p netip.Prefix) {
	if a := p.Addr(); a.Is4() || a.Is4In6() {
		if a.Is4In6() {
			p = netip.PrefixFrom(a.Unmap(), p.Bits()+96)
		}
	}
	m.cidrs = append(m.cidrs, p.Masked())
}

// parseIPRange parses "a.b.c.d-x.y.z.w" or the last-octet suffix form
// "a.b.c.d-n". Returns the inclusive uint32 bounds.
//
// parseIPRange 解析 "a.b.c.d-x.y.z.w" 或末八位组后缀形式
// "a.b.c.d-n"。返回闭区间的 uint32 边界。
func parseIPRange(s string) (uint32, uint32, error) {
	lo, hi, _ := strings.Cut(s, "-")
	loAddr, err := netip.ParseAddr(strings.TrimSpace(lo))
	if err != nil || !loAddr.Is4() {
		return 0, 0, fmt.Errorf("bad start address %q", lo)
	}
	hi = strings.TrimSpace(hi)
	hiAddr, err := netip.ParseAddr(hi)
	if err == nil {
		// Full-full form. / 完整-完整形式。
		if !hiAddr.Is4() {
			return 0, 0, fmt.Errorf("end %q is not IPv4", hi)
		}
		return toU32(loAddr), toU32(hiAddr), nil
	}
	// Suffix form "a.b.c.d-n": n replaces the last octet. / 后缀形式
	// "a.b.c.d-n"：n 替换末八位组。
	n, err := strconv.Atoi(hi)
	if err != nil || n < 0 || n > 255 {
		return 0, 0, fmt.Errorf("bad range end %q", hi)
	}
	octets := loAddr.As4()
	return toU32(loAddr), u32(octets[0], octets[1], octets[2], byte(n)), nil
}

func toU32(a netip.Addr) uint32 {
	b := a.Unmap().As4()
	return u32(b[0], b[1], b[2], b[3])
}

func u32(a, b, c, d byte) uint32 {
	return uint32(a)<<24 | uint32(b)<<16 | uint32(c)<<8 | uint32(d)
}

// Match reports whether addr (IP literal or hostname) is excluded.
// Match 报告 addr（IP 字面量或主机名）是否被排除。
func (m *HostMatcher) Match(addr string) bool {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return false
	}
	if a, err := netip.ParseAddr(addr); err == nil {
		a = a.Unmap()
		if _, ok := m.exact[a]; ok {
			return true
		}
		if a.Is4() {
			u := toU32(a)
			for _, r := range m.ranges {
				if u >= r.start && u <= r.end {
					return true
				}
			}
		}
		for _, p := range m.cidrs {
			if p.Contains(a) {
				return true
			}
		}
		return false
	}
	_, ok := m.names[strings.ToLower(addr)]
	return ok
}

// Len returns the number of parsed exclude entries (for logging).
// Len 返回已解析的排除条目数（用于日志）。
func (m *HostMatcher) Len() int {
	return len(m.exact) + len(m.ranges) + len(m.cidrs) + len(m.names)
}

// ApplyExcludeHosts filters targets against cfg's exclude spec and
// file. Returns the filtered list and how many targets were removed.
// The file format is one entry per line; blank lines and #-comments
// are ignored.
//
// ApplyExcludeHosts 按 cfg 的排除 spec 和文件过滤目标。返回过滤后
// 列表与被移除的目标数。文件格式每行一个条目；空行与 #-注释忽略。
func ApplyExcludeHosts(targets []Target, spec, file string) ([]Target, int, error) {
	if spec == "" && file == "" {
		return targets, 0, nil
	}
	var sb strings.Builder
	if spec != "" {
		sb.WriteString(spec)
	}
	if file != "" {
		f, err := os.Open(file)
		if err != nil {
			return nil, 0, fmt.Errorf("exclude-hosts-file: %w", err)
		}
		defer f.Close() //nolint:errcheck // read-only
		sc := bufio.NewScanner(f)
		first := sb.Len() == 0
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			if !first {
				sb.WriteByte(',')
			}
			sb.WriteString(line)
			first = false
		}
		if err := sc.Err(); err != nil {
			return nil, 0, fmt.Errorf("exclude-hosts-file read: %w", err)
		}
	}
	m, err := NewHostMatcher(sb.String())
	if err != nil {
		return nil, 0, err
	}
	filtered := make([]Target, 0, len(targets))
	removed := 0
	for _, t := range targets {
		if m.Match(t.Addr) {
			removed++
			continue
		}
		filtered = append(filtered, t)
	}
	return filtered, removed, nil
}
