// matcher.go — in-scope predicate construction and the expansion
// safety gates. The in-scope matcher deliberately reuses
// types.HostMatcher: the exclude matcher already parses the exact
// same spec grammar the operator uses for targets (CIDR / range /
// exact IP / hostname / RFC1918 shortcuts), and Match() is generic
// set membership — nothing about it is exclude-specific.
//
// matcher.go — 范围内谓词构建与扩展安全门。范围内匹配器刻意复用
// types.HostMatcher：排除匹配器解析的语法与操作员表达目标的语法
// 完全一致（CIDR / 范围 / 精确 IP / 主机名 / RFC1918 快捷），而
// Match() 本就是通用集合成员判断——它并不专属于"排除"。
package scope

import (
	"bufio"
	"fmt"
	"net/netip"
	"os"
	"strings"

	"github.com/LCUstinian/FG-QiMen/internal/types"
)

// BuildInScopeMatcher compiles the operator's target spec (--host
// value and --hosts-file contents) into an in-scope predicate. An
// empty spec yields a nil matcher (callers then treat every discovery
// as out-of-scope). / BuildInScopeMatcher 把操作员的目标 spec
// （--host 值与 --hosts-file 内容）编译为范围内谓词。空 spec 产出
// nil 匹配器（调用方此时把一切发现视为范围外）。
func BuildInScopeMatcher(hostSpec, hostsFile string) (func(string) bool, error) {
	m, err := buildMatcher(hostSpec, hostsFile)
	if err != nil {
		return nil, err
	}
	if m == nil {
		return nil, nil
	}
	return m.Match, nil
}

// BuildHostMatcher compiles a spec + file into a *types.HostMatcher
// (nil when both are empty) — used for the expansion round's
// --exclude-hosts gate. / BuildHostMatcher 把 spec + 文件编译为
// *types.HostMatcher（两者皆空时为 nil）——扩展轮的 --exclude-hosts
// 安全门用它。
func BuildHostMatcher(hostSpec, hostsFile string) (*types.HostMatcher, error) {
	return buildMatcher(hostSpec, hostsFile)
}

// buildMatcher assembles the raw spec, mirroring ApplyExcludeHosts'
// file handling (one entry per line, blank lines and #-comments
// ignored). / buildMatcher 组装原始 spec，镜像 ApplyExcludeHosts 的
// 文件处理（每行一条，空行与 #-注释忽略）。
func buildMatcher(hostSpec, hostsFile string) (*types.HostMatcher, error) {
	if strings.TrimSpace(hostSpec) == "" && strings.TrimSpace(hostsFile) == "" {
		return nil, nil
	}
	var sb strings.Builder
	if hostSpec != "" {
		sb.WriteString(hostSpec)
	}
	if hostsFile != "" {
		f, err := os.Open(hostsFile)
		if err != nil {
			return nil, fmt.Errorf("hosts-file: %w", err)
		}
		defer f.Close() //nolint:errcheck // read-only
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			if sb.Len() > 0 {
				sb.WriteByte(',')
			}
			sb.WriteString(line)
		}
		if err := sc.Err(); err != nil {
			return nil, fmt.Errorf("hosts-file read: %w", err)
		}
	}
	if sb.Len() == 0 {
		return nil, nil
	}
	return types.NewHostMatcher(sb.String())
}

// IsRFC1918 reports whether addr is an RFC1918 private IPv4 address
// (10/8, 172.16/12, 192.168/16) — the first expansion safety gate.
// / IsRFC1918 报告 addr 是否为 RFC1918 私网 IPv4（10/8、172.16/12、
// 192.168/16）——扩展的第一道安全门。
func IsRFC1918(addr string) bool {
	a, err := netip.ParseAddr(strings.TrimSpace(addr))
	if err != nil {
		return false
	}
	a = a.Unmap()
	if !a.Is4() {
		return false
	}
	b := a.As4()
	switch {
	case b[0] == 10:
		return true
	case b[0] == 172 && b[1] >= 16 && b[1] <= 31:
		return true
	case b[0] == 192 && b[1] == 168:
		return true
	}
	return false
}

// SameSubnet24 reports whether two addresses share the same /24.
// Non-IPv4 inputs are never same-subnet. / SameSubnet24 报告两个地址
// 是否同属一个 /24。非 IPv4 输入永不同段。
func SameSubnet24(a, b string) bool {
	aa, err := netip.ParseAddr(strings.TrimSpace(a))
	if err != nil {
		return false
	}
	bb, err := netip.ParseAddr(strings.TrimSpace(b))
	if err != nil {
		return false
	}
	aa, bb = aa.Unmap(), bb.Unmap()
	if !aa.Is4() || !bb.Is4() {
		return false
	}
	pa := netip.PrefixFrom(aa, 24).Masked()
	pb := netip.PrefixFrom(bb, 24).Masked()
	return pa == pb
}

// ExpansionCandidates filters recorded discovery events down to the
// bounded second-round target list. All gates are AND-ed:
//
//   - the event carries an IP (hostname-only events cannot be probed)
//   - RFC1918 private space only (never expand to public IPs)
//   - same /24 as one of the already-scanned addresses
//   - not already scanned
//   - not excluded by the operator's --exclude-hosts matcher
//   - at most maxHosts hosts
//
// / ExpansionCandidates 把记录的发现事件过滤成有界二轮目标列表。所有
// 门 AND 组合：
//
//   - 事件带 IP（仅主机名的事件无法探测）
//   - 仅 RFC1918 私网（绝不扩展到公网 IP）
//   - 与某个已扫地址同 /24
//   - 未扫过
//   - 未被操作员的 --exclude-hosts 匹配器排除
//   - 至多 maxHosts 台
func ExpansionCandidates(events []types.DiscoveryEvent, scanned []string, excl *types.HostMatcher, maxHosts int) []string {
	scannedSet := make(map[string]struct{}, len(scanned))
	for _, s := range scanned {
		scannedSet[strings.ToLower(strings.TrimSpace(s))] = struct{}{}
	}
	out := make([]string, 0, maxHosts)
	for _, ev := range events {
		if len(out) >= maxHosts {
			break
		}
		ip := strings.TrimSpace(ev.IP)
		if ip == "" {
			continue
		}
		key := strings.ToLower(ip)
		if _, done := scannedSet[key]; done {
			continue
		}
		if !IsRFC1918(ip) {
			continue
		}
		if excl != nil && excl.Match(ip) {
			continue
		}
		nearby := false
		for s := range scannedSet {
			if SameSubnet24(s, ip) {
				nearby = true
				break
			}
		}
		if !nearby {
			continue
		}
		scannedSet[key] = struct{}{} // also dedupes within this batch
		out = append(out, ip)
	}
	return out
}
