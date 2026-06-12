// Package scan implements port scanning (the producer of the pipeline).
// Package scan 实现端口扫描（管线的 producer）。
//
// Architecture:
//   - Probe      : one technique for testing whether a port is open
//   - Iterator   : stream of (host, port) work items
//   - Pool       : bounded-concurrency worker with adaptive sizing
//   - Scanner    : orchestrator that wires Iterator → Pool → results
//
// The default v0.1 Probe is TCP-connect. SYN and other techniques land
// in v0.2 (SYN needs raw socket, gated by privileges).
//
// 架构：
//   - Probe      : 探测一个端口是否开放的一种技术
//   - Iterator   : (host, port) 工作项流
//   - Pool       : 有界并发 worker，可自适应调并发
//   - Scanner    : 调度器：Iterator → Pool → result 流
//
// v0.1 默认 Probe 是 TCP-connect。SYN 和其他技术留到 v0.2（SYN 需要
// raw socket，依赖权限）。
package scan

import (
	"context"
	"time"
)

// Method identifies the probe technique that produced a Result.
// Method 标识产生 Result 的探测技术。
type Method string

const (
	MethodTCPConnect Method = "tcp-connect" // full TCP handshake
	MethodTCPSYN     Method = "tcp-syn"     // SYN scan (v0.2; needs raw socket)
	MethodUDP        Method = "udp"         // UDP probe (v0.2)
)

// State is the result state of a port. / State 是端口的结果状态。
type State string

const (
	StateOpen     State = "open"     // port accepted a connection
	StateClosed   State = "closed"   // port actively refused (RST)
	StateFiltered State = "filtered" // no response (timeout / unreachable)
)

// Result is the outcome of a single (host, port) probe.
// Result 是单个 (host, port) 探测的结果。
type Result struct {
	Host   string        // probed address
	Port   int           // probed port
	State  State         // open / closed / filtered
	Method Method        // which technique produced this result
	Banner string        // optional service banner (e.g. SSH version)
	RTT    time.Duration // round-trip time
	Time   time.Time     // when the result was recorded
}

// Probe is a single port-state probe technique. Implementations must
// be safe for concurrent invocation across multiple goroutines.
//
// Probe 是单个端口状态探测技术。实现必须支持跨 goroutine 并发调用。
type Probe interface {
	// Name returns a short identifier ("tcp-connect", "tcp-syn").
	// Name 返回短标识。
	Name() string
	// Method returns the Method enum this probe produces. / Method 返回该 probe 产出的 Method 枚举。
	Method() Method
	// Available reports whether the probe can run in the current
	// environment (e.g. SYN probe needs CAP_NET_RAW).
	// Available 报告该 probe 在当前环境是否可用。
	Available() error
	// Probe attempts one connect/syn/etc. Returns a Result on success
	// (including closed — we want to know it refused) or a
	// filtered result on error/timeout. ctx is honored.
	// Probe 尝试一次 connect/syn。返回 Result（包括 closed——我们要知道
	// 它拒绝了）或 error/timeout 时的 filtered 结果。ctx 可取消。
	Probe(ctx context.Context, host string, port int, timeout time.Duration) (Result, error)
}
