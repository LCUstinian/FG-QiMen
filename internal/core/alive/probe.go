// Package alive implements host discovery (liveness probing).
// Package alive 实现主机发现（存活探测）。
//
// Built-in probe strategies:
//   - TCP-ping  : connect to a small set of well-known ports
//   - ICMP echo : raw socket ICMP_ECHO_REQUEST
//   - System    : shell out to the OS `ping` command
//
// Additional LAN-only probes (ARP, NetBIOS) live in the sibling
// `internal/discovery` package and register themselves into the
// LAN-probe registry below via init(). Callers wanting them in
// DefaultOptions() blank-import that package:
//
//	import _ "github.com/LCUstinian/FG-QiMen/internal/discovery"
//
// The Discovery orchestrator runs the configured probes in order
// and returns a Hit as soon as any one succeeds (first-match).
//
// 内置探测策略：
//   - TCP-ping  : 连接一组常见端口
//   - ICMP echo : raw socket ICMP_ECHO_REQUEST
//   - System    : 调系统 `ping` 命令
//
// 仅 LAN 可用的探测（ARP、NetBIOS）位于兄弟包 `internal/discovery`，
// 通过 init() 自动注册到下方 LAN-probe 注册表。希望 DefaultOptions()
// 包含它们的调用方做一次 blank import：
//
//	import _ "github.com/LCUstinian/FG-QiMen/internal/discovery"
//
// Discovery 调度器按顺序跑已配置 probe，任一成功即返回 Hit（first-match）。
package alive

import (
	"context"
	"errors"
	"sync"
	"time"
)

// Method identifies the probe technique that produced a Hit.
// Method 标识产生 Hit 的探测技术。
type Method string

const (
	MethodTCP     Method = "tcp"     // TCP-ping / TCP connect
	MethodICMP    Method = "icmp"    // ICMP echo via raw socket
	MethodSystem  Method = "system"  // system `ping` command
	MethodARP     Method = "arp"     // ARP table lookup (LAN-only)
	MethodNetBIOS Method = "netbios" // NetBIOS Name Service (UDP 137)
)

// Hit is the result of a successful probe — host is considered alive.
// Hit 是探测成功的结果——主机被视为存活。
type Hit struct {
	Host   string        // probed address
	Port   int           // 0 for ICMP / system-ping; the port that responded for TCP
	Method Method        // which probe succeeded
	RTT    time.Duration // round-trip time of the successful probe
	Time   time.Time     // when the hit was recorded
}

// ErrUnreachable is returned when a probe completes but the host does
// not respond (e.g. connection refused, ICMP unreachable, ping 100% loss).
//
// ErrUnreachable 在探测完成但主机未响应时返回。
var ErrUnreachable = errors.New("alive: host unreachable")

// Probe is a single host-aliveness probe strategy.
// Probe 是单个主机存活探测策略。
//
// Implementations must be safe for concurrent use across multiple
// goroutines; the orchestrator may invoke Probe from many workers.
//
// 实现必须支持跨 goroutine 并发调用；调度器会在多个 worker 里调用 Probe。
type Probe interface {
	// Name returns a short identifier for logs and config (e.g. "tcp", "icmp").
	// Name 返回用于日志和配置的短标识。
	Name() string

	// Method returns the Method enum this probe produces on hit.
	// Method 返回该 probe 命中时对应的 Method 枚举。
	Method() Method

	// Probe attempts to determine whether host is alive within timeout.
	// Returns (Hit, nil) on success; (Hit{}, ErrUnreachable) on a
	// clean miss; (Hit{}, err) on a real error.
	//
	// Probe 尝试在 timeout 内判断 host 是否存活。成功返回 (Hit, nil)；
	// 完全无响应返回 (Hit{}, ErrUnreachable)；其他错误返回 (Hit{}, err)。
	Probe(ctx context.Context, host string, timeout time.Duration) (Hit, error)

	// Available reports whether this probe is usable in the current
	// environment (e.g. ICMP raw socket requires admin on Windows).
	// A non-nil return is treated as "skip this probe" by the
	// orchestrator — it will not be invoked.
	//
	// Available 报告该 probe 在当前环境是否可用（如 Windows 上 ICMP raw
	// socket 需要 admin）。返回非 nil 视为"跳过此 probe"，调度器不会调用。
	Available() error
}

// lanProbes is the registry of LAN-only probes (ARP, NetBIOS) that
// sibling packages register into via RegisterLANProbe. DefaultOptions
// appends a snapshot to its probe list, so the registry behaves like
// a blank-import-driven plugin system.
//
// lanProbes 是 LAN-only probe 的注册表（ARP、NetBIOS），由兄弟包通过
// RegisterLANProbe 注册。DefaultOptions 将注册表快照追加到 probe 列表，
// 因此该注册表表现得像一个 blank-import 驱动的插件系统。
var (
	lanProbesMu sync.Mutex
	lanProbes   []Probe
)

// RegisterLANProbe adds p to the LAN-probe registry. Intended to be
// called from init() of probe implementations in sibling packages.
// Order of registration is preserved.
//
// RegisterLANProbe 把 p 加入 LAN-probe 注册表。预期在兄弟包的 probe
// 实现 init() 中调用。保留注册顺序。
func RegisterLANProbe(p Probe) {
	lanProbesMu.Lock()
	defer lanProbesMu.Unlock()
	lanProbes = append(lanProbes, p)
}

// RegisteredLANProbes returns a snapshot of the LAN-probe registry,
// in registration order. Safe for concurrent use.
//
// RegisteredLANProbes 返回 LAN-probe 注册表的快照（按注册顺序）。
// 并发安全。
func RegisteredLANProbes() []Probe {
	lanProbesMu.Lock()
	defer lanProbesMu.Unlock()
	out := make([]Probe, len(lanProbes))
	copy(out, lanProbes)
	return out
}

// alwaysOnProbes is the registry of always-on probes (ICMP, TCP-ping,
// system-ping) that the in-tree alive.probes package registers into
// via RegisterAlwaysOnProbe in its init(). Mirrors the LAN-probe
// registry so adding a new always-on probe does not require editing
// DefaultOptions — only init() registration.
//
// alwaysOnProbes 是始终启用 probe 的注册表（ICMP、TCP-ping、system-ping），
// 由内部 alive.probes 包在 init() 中通过 RegisterAlwaysOnProbe 注册。
// 与 LAN-probe 注册表对称，让新增 always-on probe 只需 init() 注册、
// 不必改 DefaultOptions。
var (
	alwaysOnProbesMu sync.Mutex
	alwaysOnProbes   []Probe
)

// RegisterAlwaysOnProbe adds p to the always-on probe registry.
// Intended to be called from init() of probe implementations.
// Order of registration is preserved.
//
// RegisterAlwaysOnProbe 把 p 加入 always-on probe 注册表。预期在
// probe 实现的 init() 中调用。保留注册顺序。
func RegisterAlwaysOnProbe(p Probe) {
	alwaysOnProbesMu.Lock()
	defer alwaysOnProbesMu.Unlock()
	alwaysOnProbes = append(alwaysOnProbes, p)
}

// RegisteredAlwaysOnProbes returns a snapshot of the always-on probe
// registry, in registration order. Safe for concurrent use.
//
// RegisteredAlwaysOnProbes 返回 always-on probe 注册表的快照
// （按注册顺序）。并发安全。
func RegisteredAlwaysOnProbes() []Probe {
	alwaysOnProbesMu.Lock()
	defer alwaysOnProbesMu.Unlock()
	out := make([]Probe, len(alwaysOnProbes))
	copy(out, alwaysOnProbes)
	return out
}
