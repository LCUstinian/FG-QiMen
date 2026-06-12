// Package alive implements host discovery (liveness probing).
// Package alive 实现主机发现（存活探测）。
//
// Three probe strategies are provided, in pluggable order:
//   - TCP-ping  : connect to a small set of well-known ports
//   - ICMP echo : raw socket ICMP_ECHO_REQUEST
//   - System    : shell out to the OS `ping` command
//
// The Discovery orchestrator runs the probes in the configured order
// and returns a Hit as soon as any one succeeds (first-match).
//
// 三种探测策略按可配置顺序运行：
//   - TCP-ping  : 连接一组常见端口
//   - ICMP echo : raw socket ICMP_ECHO_REQUEST
//   - System    : 调系统 `ping` 命令
//
// Discovery 调度器按顺序跑，任一成功即返回 Hit（first-match）。
package alive

import (
	"context"
	"errors"
	"time"
)

// Method identifies the probe technique that produced a Hit.
// Method 标识产生 Hit 的探测技术。
type Method string

const (
	MethodTCP    Method = "tcp"    // TCP-ping / TCP connect
	MethodICMP   Method = "icmp"   // ICMP echo via raw socket
	MethodSystem Method = "system" // system `ping` command
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
