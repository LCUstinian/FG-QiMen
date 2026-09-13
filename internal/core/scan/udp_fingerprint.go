// Package scan: UDP service fingerprint probe.
// Package scan：UDP 服务指纹探针。
//
// UDPServiceProbe sends the nmap-service-probes UDP payloads for a
// port (the DNS version.bind query, the NBTStat name request, the
// memcached "stats", …) and reports the FIRST response as the port's
// banner. UDP is connectionless: a silent port is open|filtered, a
// refused write/read is closed, a response is open — the state
// machine mirrors probeOnce, with one difference: ALL payloads for
// the port are written on ONE connected socket before a single read,
// because serial payload-then-read rounds would stack one 2s timeout
// per payload (port 53 has three probes → 6s per host per port).
//
// UDPServiceProbe 对端口发送 nmap-service-probes 的 UDP payload（DNS
// version.bind 查询、NBTStat 名字请求、memcached "stats"……），并把
// 首个响应作为端口 banner。UDP 无连接：静默端口是 open|filtered，
// 写/读被拒是 closed，有响应是 open——状态机与 probeOnce 一致，唯一
// 区别是该端口的全部 payload 在同一条 connected socket 上写完再读一
// 次：逐个"发-读"会让每个 payload 各吃一次 2s 超时（53 端口三个
// probe → 每主机每端口 6 秒）。
package scan

import (
	"context"
	"fmt"
	"net"
	"time"
)

// UDPServiceProbe probes UDP ports with service-specific payloads.
// / UDPServiceProbe 用服务特定 payload 探测 UDP 端口。
type UDPServiceProbe struct {
	// Payloads returns the decoded nmap UDP probe payloads for a port.
	// Ports without a hint may return nil — the probe then degrades to
	// the generic single 0x00 byte probe (same as UDPProbe).
	// / Payloads 返回某端口解码后的 nmap UDP probe payload。无提示的
	// 端口可返回 nil——此时退化为通用单字节 0x00 探测（同 UDPProbe）。
	Payloads func(port int) [][]byte

	// DialTimeout caps socket setup and each write. Zero = 2s.
	// / DialTimeout 限制建连与每次写的超时。零 = 2s。
	DialTimeout time.Duration

	// ReadTimeout caps the wait for a response after all payloads are
	// sent. Zero = 2s. / ReadTimeout 限制全部 payload 发出后等响应的
	// 时间。零 = 2s。
	ReadTimeout time.Duration

	// Strict changes the silence verdict: a port that answered nothing
	// within its budget is reported Filtered instead of the
	// open|filtered Open convention. The plugin consumer drops
	// non-open results, so strict mode keeps silent segments out of
	// the output entirely — trading recall of idle-but-open services
	// for a noise-free result stream on firewalled networks.
	// / Strict 改变静默裁决：预算内没有任何应答的端口报 Filtered，
	// 而非 open|filtered 的 Open 约定。插件消费方丢弃非 open 结果，
	// 因此 strict 模式让静默网段完全不进入输出——用"漏掉空闲但开
	// 放的服务"换防火墙网络上的零噪声结果流。
	Strict bool
}

// NewUDPServiceProbe returns a UDPServiceProbe with the default 2s
// budgets. / NewUDPServiceProbe 返回默认 2s 预算的 UDPServiceProbe。
func NewUDPServiceProbe(payloads func(port int) [][]byte) *UDPServiceProbe {
	return &UDPServiceProbe{Payloads: payloads}
}

// Name implements Probe. / Name 实现 Probe。
func (p *UDPServiceProbe) Name() string { return "udp-fp" }

// Method implements Probe. / Method 实现 Probe。
func (p *UDPServiceProbe) Method() Method { return MethodUDP }

// Available implements Probe. UDP needs no special privileges.
// / Available 实现 Probe。UDP 不需要特殊权限。
func (p *UDPServiceProbe) Available() error { return nil }

// Probe implements Probe. / Probe 实现 Probe。
func (p *UDPServiceProbe) Probe(ctx context.Context, host string, port int, timeout time.Duration) (Result, error) {
	dialTimeout := p.DialTimeout
	if dialTimeout <= 0 {
		dialTimeout = 2 * time.Second
	}
	readTimeout := p.ReadTimeout
	if readTimeout <= 0 {
		readTimeout = 2 * time.Second
	}
	if timeout > 0 && timeout < dialTimeout {
		dialTimeout = timeout
	}
	// The pool timeout is the probe's WHOLE budget: cap the read too,
	// or an adaptive shrink would still pay the full default read
	// wait on every silent port.
	// / 池超时是 probe 的总预算：读也要 cap，否则自适应收缩后每个
	// 静默端口仍要付满默认读等待。
	if timeout > 0 && timeout < readTimeout {
		readTimeout = timeout
	}

	var payloads [][]byte
	if p.Payloads != nil {
		payloads = p.Payloads(port)
	}
	if len(payloads) == 0 {
		// No hint for this port: degrade to the generic 1-byte probe.
		// / 该端口无提示：退化为通用单字节探测。
		banner, state, rtt := probeOnce(ctx, host, port, []byte{0x00}, dialTimeout, readTimeout)
		if state == StateOpen && len(banner) == 0 {
			// Silence: no response, no RTT (zero keeps the adaptive
			// ring clean); strict mode reports Filtered.
			// / 静默：无响应即无 RTT（置零保持自适应采样环干净）；
			// strict 模式报 Filtered。
			rtt = 0
			if p.Strict {
				state = StateFiltered
			}
		}
		return Result{
			Host: host, Port: port, State: state,
			Method: MethodUDP, Banner: string(banner),
			RTT: rtt, Time: time.Now(),
		}, nil
	}

	start := time.Now()
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	d := net.Dialer{Timeout: dialTimeout}
	conn, err := d.DialContext(ctx, "udp", addr)
	if err != nil {
		if isConnRefused(err) {
			return Result{Host: host, Port: port, State: StateClosed, Method: MethodUDP, RTT: time.Since(start), Time: time.Now()}, nil
		}
		return Result{Host: host, Port: port, State: StateFiltered, Method: MethodUDP, RTT: time.Since(start), Time: time.Now()}, nil
	}
	defer conn.Close()

	// Write ALL payloads on the one connected socket. A ICMP "port
	// unreachable" triggered by an earlier payload surfaces as
	// ECONNREFUSED on a later write — early-exit closed.
	// / 在同一条 connected socket 上写完全部 payload。较早 payload 触
	// 发的 ICMP "port unreachable" 会在后续写上以 ECONNREFUSED 浮出
	// ——早退 closed。
	_ = conn.SetWriteDeadline(time.Now().Add(dialTimeout))
	for _, payload := range payloads {
		if err := ctx.Err(); err != nil {
			return Result{Host: host, Port: port, State: StateFiltered, Method: MethodUDP, RTT: time.Since(start), Time: time.Now()}, nil
		}
		if _, err := conn.Write(payload); err != nil {
			if isConnRefused(err) {
				return Result{Host: host, Port: port, State: StateClosed, Method: MethodUDP, RTT: time.Since(start), Time: time.Now()}, nil
			}
			if isNetworkUnreachable(err) {
				return Result{Host: host, Port: port, State: StateFiltered, Method: MethodUDP, RTT: time.Since(start), Time: time.Now()}, nil
			}
			// Other write errors: keep going — the read below decides.
			// / 其它写错误：继续——由下面的读来裁决。
		}
	}

	// Single read for the first response (raw bytes, no trimming —
	// see probeOnce). / 单次读首个响应（原始字节，不裁剪——见
	// probeOnce）。
	_ = conn.SetReadDeadline(time.Now().Add(readTimeout))
	buf := make([]byte, 512)
	n, readErr := conn.Read(buf)
	if n > 0 {
		b := make([]byte, n)
		copy(b, buf[:n])
		return Result{Host: host, Port: port, State: StateOpen, Method: MethodUDP, Banner: string(b), RTT: time.Since(start), Time: time.Now()}, nil
	}
	if readErr != nil {
		if isConnRefused(readErr) {
			return Result{Host: host, Port: port, State: StateClosed, Method: MethodUDP, RTT: time.Since(start), Time: time.Now()}, nil
		}
		if isTimeout(readErr) {
			// Silence → open|filtered, marked Open so plugins get a
			// chance (Filtered under Strict). No response, no RTT —
			// the elapsed wait would poison the adaptive ring.
			// / 静默 → open|filtered，标 Open 给插件机会（Strict 下报
			// Filtered）。无响应即无 RTT——等满的时间会毒化自适应采样环。
			state := StateOpen
			if p.Strict {
				state = StateFiltered
			}
			return Result{Host: host, Port: port, State: state, Method: MethodUDP, RTT: 0, Time: time.Now()}, nil
		}
	}
	// Fallthrough: ambiguous, same verdict as the timeout branch.
	// / 兜底：模糊，与超时分支同一裁决。
	state := StateOpen
	if p.Strict {
		state = StateFiltered
	}
	return Result{Host: host, Port: port, State: state, Method: MethodUDP, RTT: 0, Time: time.Now()}, nil
}
