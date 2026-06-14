// Package discovery: LAN-only host discovery probes (ARP + NBNS).
//
// These probes live outside the core/alive package because they only
// produce hits inside the broadcast domain (ARP) or on hosts running
// the NetBIOS service (NBNS) — useful complements to ICMP/TCP for
// internal-LAN scans, but useless across the internet.
//
// The package registers its probes via init() into alive's LAN-probe
// registry, so callers opt in with a single blank import:
//
//	import _ "github.com/LCUstinian/FG-QiMen/internal/discovery"
//
// and alive.DefaultOptions() then includes ARP + NBNS. Callers who
// want an internet-only scan simply omit the blank import.
//
// 包 discovery：LAN-only 主机发现 probe（ARP + NBNS）。
// 这两个 probe 单独成包，因为它们仅在广播域内（ARP）或运行 NetBIOS
// 服务的主机上（NBNS）才能产生 hit ——它们是内网扫描中对 ICMP/TCP 的
// 有益补充，但跨互联网无用。
//
// 包通过 init() 把自己注册到 alive 的 LAN-probe 注册表，
// 调用方只需一次 blank import 即可启用：
//
//	import _ "github.com/LCUstinian/FG-QiMen/internal/discovery"
//
// 之后 alive.DefaultOptions() 即包含 ARP + NBNS。
// 仅扫互联网的调用方不 import 即可禁用。
//
// arp.go — ARP probe (LAN host discovery via OS ARP table).
//
// Two strategies are used, picked at runtime per OS:
//   - Linux:  parse /proc/net/arp (free, no subprocess)
//   - macOS / Windows / others: run `arp -an` and grep the table
//
// If the target IP appears in the OS ARP table with a "complete" or
// "permanent" (Linux) / non-incomplete (macOS/Windows) entry, the
// host is considered alive. This is the most reliable LAN probe —
// ARP does not traverse firewalls, so a host that responds to ARP
// is almost certainly up and on-link.
//
// Note: this is a passive table lookup, not a gratuitous ARP
// sender. If the host is not yet in the table (e.g. very recent
// boot, or you've never talked to it), it won't be detected. For
// a true "wake-up" probe, layer ICMP/TCP-ping before ARP.
//
// arp.go — ARP 探测（通过 OS ARP 表做 LAN 主机发现）。
// 两种策略按 OS 选：Linux 解析 /proc/net/arp（无 subprocess），macOS /
// Windows / 其他跑 `arp -an` 然后 grep。
// 如果目标 IP 在 OS ARP 表中且条目状态为 "complete" 或 "permanent"
//（Linux）/ 非 incomplete（macOS/Windows），主机即视为存活。这是
// 最可靠的 LAN 探测——ARP 不过防火墙，所以响应 ARP 的主机几乎
// 一定在链路上。
//
// 注：这是被动表查询，不是免费 ARP 发送。如果主机尚未在表中
//（如刚开机，或从未与它通信），就探测不到。要做"唤醒"探测，
// 前面叠 ICMP / TCP-ping。
package discovery

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/core/alive"
)

// ARPProbe probes hosts by looking them up in the OS ARP table.
// ARPProbe 通过查 OS ARP 表来探测主机。
type ARPProbe struct {
	// ForceCmd, if true, skips /proc/net/arp and always shells out
	// to `arp`. Used by tests to exercise the macOS/Windows code
	// path. / ForceCmd 若为 true，跳过 /proc/net/arp 始终调 `arp`。
	// 测试用来跑 macOS/Windows 代码路径。
	ForceCmd bool
}

// NewARPProbe returns an ARPProbe that auto-selects the right
// strategy per OS. / NewARPProbe 返回按 OS 自动选策略的 ARPProbe。
func NewARPProbe() *ARPProbe { return &ARPProbe{} }

// Name implements alive.Probe. / Name 实现 alive.Probe。
func (p *ARPProbe) Name() string { return "arp" }

// Method implements alive.Probe. / Method 实现 alive.Probe。
func (p *ARPProbe) Method() alive.Method { return alive.MethodARP }

// Available implements alive.Probe. ARP table lookup works on all
// platforms but is most useful on LAN. On a host with no network
// interfaces (very rare), it returns an error.
// / Available 实现 alive.Probe。ARP 表查询所有平台都能工作但最有用的
// 是 LAN 上。在没有网络接口的主机（极罕见）上返 error。
func (p *ARPProbe) Available() error { return nil }

// Probe implements alive.Probe. / Probe 实现 alive.Probe。
func (p *ARPProbe) Probe(ctx context.Context, host string, timeout time.Duration) (alive.Hit, error) {
	start := time.Now()
	found := false
	var err error
	if p.ForceCmd || runtime.GOOS != "linux" {
		found, err = p.lookupViaCmd(ctx, host)
	} else {
		found, err = p.lookupViaProcNetArp(host)
	}
	if err != nil {
		return alive.Hit{}, err
	}
	if !found {
		return alive.Hit{}, alive.ErrUnreachable
	}
	return alive.Hit{
		Host:   host,
		Port:   0,
		Method: alive.MethodARP,
		RTT:    time.Since(start),
		Time:   time.Now(),
	}, nil
}

// lookupViaProcNetArp parses /proc/net/arp on Linux. Each line:
//
//	IP  HWType  Flags  HWAddress  Mask  Device
//
// where Flags is "0x0" (incomplete), "0x2" (complete), "0x4" (permanent), ...
//
// / lookupViaProcNetArp 解析 Linux 的 /proc/net/arp。每行：
//
//	IP  HWType  Flags  HWAddress  Mask  Device
//
// Flags 是 "0x0"（incomplete）、"0x2"（complete）、"0x4"（permanent）等。
func (p *ARPProbe) lookupViaProcNetArp(host string) (bool, error) {
	f, err := os.Open("/proc/net/arp")
	if err != nil {
		return false, fmt.Errorf("arp: open /proc/net/arp: %w", err)
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	first := true
	for scanner.Scan() {
		line := scanner.Text()
		if first {
			// Skip header. / 跳标题行。
			first = false
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}
		// fields[0] = IP, fields[2] = Flags (hex).
		// / fields[0] = IP，fields[2] = Flags（hex）。
		if fields[0] != host {
			continue
		}
		// "0x0" = incomplete (entry exists but no reply yet). Treat
		// as miss; we want only "0x2" (complete) or "0x4" (permanent).
		// / "0x0" = incomplete（条目在但无响应）。视为 miss；只要
		// "0x2"（complete）或 "0x4"（permanent）。
		if fields[2] == "0x0" {
			continue
		}
		return true, nil
	}
	if err := scanner.Err(); err != nil {
		return false, fmt.Errorf("arp: scan /proc/net/arp: %w", err)
	}
	return false, nil
}

// lookupViaCmd runs `arp -an` and greps for the host. The "-a" flag
// asks for binary-style output, "-n" for numeric IPs. Output lines
// look like: "? (10.0.0.1) at 00:11:22:33:44:55 on en0 ifscope [ethernet]"
// or "(10.0.0.1) at (incomplete) on en0". / lookupViaCmd 跑 `arp -an`
// 并 grep 主机。`-a` 请求二进制式输出，`-n` 数字 IP。
func (p *ARPProbe) lookupViaCmd(ctx context.Context, host string) (bool, error) {
	// On Windows, `arp -a` shows the table without -n (which is
	// unrecognized). On macOS / Linux, `arp -an` is the standard
	// numeric form. / Windows 上 `arp -a` 直接显示表，无 -n（不识别）。
	// macOS / Linux 上 `arp -an` 是标准数字式。
	args := []string{"-an"}
	if runtime.GOOS == "windows" {
		args = []string{"-a"}
	}
	cmd := exec.CommandContext(ctx, "arp", args...)
	out, err := cmd.Output()
	if err != nil {
		return false, fmt.Errorf("arp: %v: %w", "arp cmd", err)
	}
	// Grep the output for "(host) " or "host " patterns. The -n
	// flag makes the host always appear as "(1.2.3.4)" so we can
	// match the bracketed form. / grep 输出找 "(host) " 或 "host "
	// 模式。-n 让 host 总以 "(1.2.3.4)" 形式出现，所以匹配括号式。
	needle := "(" + host + ")"
	scanner := bufio.NewScanner(strings.NewReader(string(out)))
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.Contains(line, needle) {
			continue
		}
		// Skip "(incomplete)" entries (no reply yet).
		// / 跳 "(incomplete)" 条目（无响应）。
		if strings.Contains(line, "incomplete") {
			continue
		}
		return true, nil
	}
	return false, nil
}

// init registers the ARP probe with the alive package so callers
// who blank-import this package get it in alive.DefaultOptions().
// init 把 ARP probe 注册到 alive 包，使 blank-import 本包的调用方
// 在 alive.DefaultOptions() 中拿到它。
func init() {
	alive.RegisterLANProbe(NewARPProbe())
}
