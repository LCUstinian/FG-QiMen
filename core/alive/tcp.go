// Package alive: TCP-ping probe.
// Package alive: TCP-ping 探测。
package alive

import (
	"context"
	"fmt"
	"net"
	"time"
)

// DefaultTCPProbePorts is the default set of "almost-always-open"
// ports the TCP probe tries in order. A response on any of them is
// considered proof of liveness.
//
// DefaultTCPProbePorts 是 TCP probe 默认尝试的"几乎一定开放"端口集。
// 任一端口响应即视为存活证据。
var DefaultTCPProbePorts = []int{80, 443, 22, 8080}

// TCPProbe probes hosts by opening a TCP connection to each port in
// the configured set, in order, until one accepts.
//
// TCPProbe 通过按顺序打开 TCP 连接到配置端口集中的每一个，直到一个
// 接受连接为止来探测主机。
type TCPProbe struct {
	// Ports is the ordered list of ports to try. Defaults to
	// DefaultTCPProbePorts if empty. / Ports 是按顺序尝试的端口列表；
	// 为空则用 DefaultTCPProbePorts。
	Ports []int
}

// NewTCPProbe returns a TCPProbe with the default port set.
// NewTCPProbe 返回使用默认端口集的 TCPProbe。
func NewTCPProbe() *TCPProbe { return &TCPProbe{Ports: DefaultTCPProbePorts} }

// NewTCPProbeWithPorts returns a TCPProbe with a custom port set.
// NewTCPProbeWithPorts 返回使用自定义端口集的 TCPProbe。
func NewTCPProbeWithPorts(ports []int) *TCPProbe {
	p := make([]int, len(ports))
	copy(p, ports)
	return &TCPProbe{Ports: p}
}

// Name implements Probe. / Name 实现 Probe。
func (p *TCPProbe) Name() string { return "tcp" }

// Method implements Probe. / Method 实现 Probe。
func (p *TCPProbe) Method() Method { return MethodTCP }

// Available implements Probe. TCP-ping needs no special privileges.
// Available 实现 Probe。TCP-ping 不需要特殊权限。
func (p *TCPProbe) Available() error { return nil }

// Probe implements Probe. Returns Hit on first port that accepts; returns
// ErrUnreachable if all ports refused/timed out.
//
// Probe 实现 Probe。任一端口接受则返回 Hit；全部拒绝/超时返回 ErrUnreachable。
func (p *TCPProbe) Probe(ctx context.Context, host string, timeout time.Duration) (Hit, error) {
	ports := p.Ports
	if len(ports) == 0 {
		ports = DefaultTCPProbePorts
	}
	d := net.Dialer{Timeout: timeout}
	for _, port := range ports {
		if ctx.Err() != nil {
			return Hit{}, ctx.Err()
		}
		addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
		start := time.Now()
		conn, err := d.DialContext(ctx, "tcp", addr)
		if err != nil {
			// Try next port. / 试下一个端口。
			continue
		}
		_ = conn.Close()
		return Hit{
			Host:   host,
			Port:   port,
			Method: MethodTCP,
			RTT:    time.Since(start),
			Time:   time.Now(),
		}, nil
	}
	return Hit{}, ErrUnreachable
}
