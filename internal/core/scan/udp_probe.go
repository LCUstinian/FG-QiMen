// Package scan: UDP probe.
//
// UDP is connectionless, so "open" is ambiguous. We send 0 bytes
// (or one byte 0x00 for known probe patterns) and wait for a
// response. Service-specific probes (DNS query, NTP, SNMP GET, etc.)
// land in v0.2+; this v0.1 UDP probe is a generic "is anything
// there?" check.
//
// 三种结果：
//   - Service response received → StateOpen (with banner bytes if any).
//   - ICMP "port unreachable" received → StateClosed.
//   - Timeout / no response → StateOpen|filtered (UDP is connectionless;
//     silence could mean "open but idle" or "filtered by firewall").
//
// UDP 是无连接的，所以"open"是模糊的。我们发 0 字节（或单字节 0x00
// 作为已知 probe 模式）并等响应。服务级 probe（DNS query、NTP、
// SNMP GET 等）放 v0.2+；v0.1 的 UDP probe 是通用"是否有东西在"检测。
package scan

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"syscall"
	"time"
)

// UDPProbe probes UDP ports. / UDPProbe 探测 UDP 端口。
type UDPProbe struct {
	// BannerTimeout caps the wait for a response. Zero = 2s default.
	// / BannerTimeout 限制等响应时间。零 = 2s 默认。
	BannerTimeout time.Duration

	// ReadPayload, if non-empty, is sent instead of an empty packet.
	// v0.1 ships no service-specific probes; v0.2+ adds DNS / NTP /
	// SNMP specific payload builders. / ReadPayload 非空时发这个（替
	// 代空包）。v0.1 没有服务级 probe；v0.2+ 加 DNS / NTP / SNMP
	// 特定 payload。
	ReadPayload []byte
}

// NewUDPProbe returns a default UDP probe. / NewUDPProbe 返回默认 UDP
// 探测。
func NewUDPProbe() *UDPProbe {
	return &UDPProbe{BannerTimeout: 2 * time.Second}
}

// Name implements Probe. / Name 实现 Probe。
func (p *UDPProbe) Name() string { return "udp" }

// Method implements Probe. / Method 实现 Probe。
func (p *UDPProbe) Method() Method { return MethodUDP }

// Available implements Probe. UDP needs no special privileges.
// / Available 实现 Probe。UDP 不需要特殊权限。
func (p *UDPProbe) Available() error { return nil }

// Probe implements Probe. / Probe 实现 Probe。
func (p *UDPProbe) Probe(ctx context.Context, host string, port int, timeout time.Duration) (Result, error) {
	payload := p.ReadPayload
	if payload == nil {
		payload = []byte{0x00} // 1-byte probe, indistinguishable from random noise
	}
	btimeout := p.BannerTimeout
	if btimeout <= 0 {
		btimeout = 2 * time.Second
	}
	banner, state, rtt, perr := probeOnce(ctx, host, port, payload, timeout, btimeout)
	if perr != nil {
		// Resource-exhausted dial (etc.) — surface to the pool so the
		// AIMD controller shrinks and the operator sees the starvation.
		// / 资源耗尽 dial（等）——浮出给池，让 AIMD 控制器缩容、操作
		// 员看到饥饿。
		return Result{}, perr
	}
	if state == StateOpen && len(banner) == 0 {
		// Silent open|filtered: no response, no RTT. Zeroing keeps
		// the adaptive sampler (pool AdaptiveTimeout) free of
		// degenerate full-wait samples.
		// / 静默 open|filtered：无响应即无 RTT。置零让自适应采样器
		// （池 AdaptiveTimeout）免受"等满"退化样本污染。
		rtt = 0
	}
	return Result{
		Host: host, Port: port, State: state,
		Method: MethodUDP, Banner: string(banner),
		RTT: rtt, Time: time.Now(),
	}, nil
}

// udpDialVerdict maps a UDP dial error to a verdict. Resource
// exhaustion (EMFILE / socket starvation) MUST surface as an error —
// the pool turns it into the AIMD congestion signal and the operator
// sees the starvation instead of silently missing probes. Everything
// else keeps the classic verdict: refused → closed, unreachable →
// filtered.
//
// / udpDialVerdict 把 UDP dial 错误映射为裁决。资源耗尽（EMFILE /
// socket 饥饿）必须以错误浮出——池会把它变成 AIMD 拥塞信号，操作
// 员能看到饥饿而不是悄悄丢探测。其余保持经典裁决：refused →
// closed，unreachable → filtered。
func udpDialVerdict(err error) (State, error) {
	if isResourceExhaustedError(err) {
		return StateFiltered, err
	}
	if isConnRefused(err) {
		return StateClosed, nil
	}
	return StateFiltered, nil
}

// probeOnce is the single-payload UDP state machine shared by
// UDPProbe and UDPServiceProbe: dial a connected UDP socket, send one
// payload, wait up to readTimeout for a response.
//
//   - response → (banner, StateOpen)
//   - ICMP "port unreachable" surfacing as ECONNREFUSED → (nil, StateClosed)
//   - dial refused / unreachable / timeout with silence → (nil, StateFiltered)
//     or (nil, StateOpen) — UDP silence could mean "open but idle" or
//     "filtered by firewall"; we mark Open so the plugin pipeline still
//     gets a chance to identify the service if the user supplied a
//     port list.
//   - resource-exhausted dial → (nil, StateFiltered, err): the error is
//     the verdict the pool acts on (A4: raised UDP caps make this path
//     reachable; swallowing it used to lose probes without evidence).
//
// The banner is returned AS RECEIVED (raw bytes, no trimASCII): UDP
// responses are binary (DNS/SNMP/NBTStat), and trimASCII's
// space-substitution destroyed every byte outside printable ASCII —
// end-anchored and binary-anchored fingerprint rules could never
// match. Display paths re-sanitize.
//
// / probeOnce 是 UDPProbe 与 UDPServiceProbe 共用的单 payload UDP 状态
// 机：拨一条 connected UDP socket，发一个 payload，等 readTimeout 内
// 的响应。
//
//   - 有响应 → (banner, StateOpen)
//   - ICMP "port unreachable" 以 ECONNREFUSED 浮出 → (nil, StateClosed)
//   - dial refused / 网络不可达 / 静默超时 → (nil, StateFiltered) 或
//     (nil, StateOpen)——UDP 沉默可能是"open 但静默"或"filtered"；
//     我们标 Open 让插件流水线仍有机会识别服务（如果用户给了端口列
//     表）。
//   - dial 资源耗尽 → (nil, StateFiltered, err)：池据以行动的裁决就
//     是这个错误（A4：UDP 上限提高后该路径可达；旧版吞掉它导致探测
//     无证据丢失）。
//
// banner 按收到的原样返回（原始字节，不做 trimASCII）：UDP 响应是二
// 进制（DNS/SNMP/NBTStat），trimASCII 的空格替换会毁掉可打印 ASCII
// 之外的所有字节——行尾/二进制锚定的指纹规则永远无法匹配。显示路径
// 会自行收敛。
func probeOnce(ctx context.Context, host string, port int, payload []byte, dialTimeout, readTimeout time.Duration) (banner []byte, state State, rtt time.Duration, probeErr error) {
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	d := net.Dialer{Timeout: dialTimeout}
	start := time.Now()
	conn, err := d.DialContext(ctx, "udp", addr)
	if err != nil {
		// "no route to host" or "host unreachable" → filtered.
		// "refused" rarely happens on UDP since there's no listener
		// to refuse; if it does, treat as closed. Resource
		// exhaustion surfaces as the probe's error — never as a
		// silent Filtered result (A4: raised UDP caps make this
		// path reachable; swallowing it used to lose probes without
		// evidence).
		// / "no route to host" 或 "host unreachable" → filtered。
		// UDP 上"refused"很少（无 listener 拒）；如果发生，视为
		// closed。资源耗尽以 probe 错误浮出——绝不做静默 Filtered
		// 结果（A4：UDP 上限提高后该路径可达；旧版吞掉它导致探测
		// 无证据丢失）。
		state, perr := udpDialVerdict(err)
		return nil, state, time.Since(start), perr
	}
	defer conn.Close()

	// Send the payload. / 发 payload。
	_ = conn.SetWriteDeadline(time.Now().Add(dialTimeout))
	if _, err := conn.Write(payload); err != nil {
		// Buffer exhaustion on send is starvation too — surface it.
		// / 发送侧缓冲耗尽同样是饥饿——浮出。
		if isResourceExhaustedError(err) {
			return nil, StateFiltered, time.Since(start), err
		}
		// "network is unreachable" or similar → filtered. / 网络不可
		// 达或类似 → filtered。
		if isNetworkUnreachable(err) {
			return nil, StateFiltered, time.Since(start), nil
		}
	}

	// Wait for response. / 等响应。
	if readTimeout <= 0 {
		readTimeout = 2 * time.Second
	}
	_ = conn.SetReadDeadline(time.Now().Add(readTimeout))
	buf := make([]byte, 512)
	n, readErr := conn.Read(buf)
	if n > 0 {
		// Service responded! Open. / 服务响应了！Open。
		b := make([]byte, n)
		copy(b, buf[:n])
		return b, StateOpen, time.Since(start), nil
	}
	if readErr != nil {
		// "connection refused" on UDP usually means we got an ICMP
		// "port unreachable" — the port is closed. / UDP 上的
		// "connection refused" 通常意味着我们收到了 ICMP "port
		// unreachable"——端口是 closed。
		if isConnRefused(readErr) {
			return nil, StateClosed, time.Since(start), nil
		}
		// Resource starvation can also surface on recv (WSAENOBUFS
		// under load) — never launder it into a silent verdict.
		// / 资源饥饿也会在 recv 浮出（高负载下 WSAENOBUFS）——绝不
		// 洗成静默裁决。
		if isResourceExhaustedError(readErr) {
			return nil, StateFiltered, time.Since(start), readErr
		}
		// Timeout / i/o timeout → open|filtered (marked Open, see
		// above). / 超时 → open|filtered（标 Open，见上）。
		if isTimeout(readErr) {
			return nil, StateOpen, time.Since(start), nil
		}
	}
	// Fallthrough: ambiguous. / 兜底：模糊。
	return nil, StateOpen, time.Since(start), nil
}

// isNetworkUnreachable returns true if err indicates the network or
// host is unreachable (not a port-level refused). /
// isNetworkUnreachable 当 err 表示网络或主机不可达（非端口级 refused）
// 时返 true。
func isNetworkUnreachable(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, syscall.ENETUNREACH) || errors.Is(err, syscall.EHOSTUNREACH) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "no route") || strings.Contains(msg, "unreachable") || strings.Contains(msg, "network is unreachable")
}

// isTimeout returns true if err is a timeout. / isTimeout 当 err 是
// 超时时返 true。
func isTimeout(err error) bool {
	if err == nil {
		return false
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "timeout") || strings.Contains(msg, "i/o timeout")
}
