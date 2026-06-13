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
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	d := net.Dialer{Timeout: timeout}
	start := time.Now()
	conn, err := d.DialContext(ctx, "udp", addr)
	if err != nil {
		// "no route to host" or "host unreachable" → filtered.
		// "refused" rarely happens on UDP since there's no listener
		// to refuse; if it does, treat as closed.
		// / "no route to host" 或 "host unreachable" → filtered。
		// UDP 上"refused"很少（无 listener 拒）；如果发生，视为
		// closed。
		if isConnRefused(err) {
			return Result{
				Host: host, Port: port, State: StateClosed,
				Method: MethodUDP, RTT: time.Since(start),
				Time: time.Now(),
			}, nil
		}
		return Result{
			Host: host, Port: port, State: StateFiltered,
			Method: MethodUDP, RTT: time.Since(start),
			Time: time.Now(),
		}, nil
	}
	defer conn.Close()

	// Send the payload (or empty). / 发 payload（或空）。
	payload := p.ReadPayload
	if payload == nil {
		payload = []byte{0x00} // 1-byte probe, indistinguishable from random noise
	}
	_ = conn.SetWriteDeadline(time.Now().Add(timeout))
	if _, err := conn.Write(payload); err != nil {
		// "network is unreachable" or similar → filtered. / 网络不可
		// 达或类似 → filtered。
		if isNetworkUnreachable(err) {
			return Result{
				Host: host, Port: port, State: StateFiltered,
				Method: MethodUDP, RTT: time.Since(start),
				Time: time.Now(),
			}, nil
		}
	}

	// Wait for response. / 等响应。
	btimeout := p.BannerTimeout
	if btimeout <= 0 {
		btimeout = 2 * time.Second
	}
	_ = conn.SetReadDeadline(time.Now().Add(btimeout))
	buf := make([]byte, 512)
	n, readErr := conn.Read(buf)
	if n > 0 {
		// Service responded! Open. / 服务响应了！Open。
		return Result{
			Host: host, Port: port, State: StateOpen,
			Method: MethodUDP, Banner: trimASCII(buf[:n]),
			RTT: time.Since(start), Time: time.Now(),
		}, nil
	}
	if readErr != nil {
		// "connection refused" on UDP usually means we got an ICMP
		// "port unreachable" — the port is closed. / UDP 上的
		// "connection refused" 通常意味着我们收到了 ICMP "port
		// unreachable"——端口是 closed。
		if isConnRefused(readErr) {
			return Result{
				Host: host, Port: port, State: StateClosed,
				Method: MethodUDP, RTT: time.Since(start),
				Time: time.Now(),
			}, nil
		}
		// Timeout / i/o timeout → open|filtered. UDP silence could
		// mean "open but quiet" or "filtered". We mark as Open so the
		// plugin pipeline still gets a chance to identify the service
		// if the user supplied a port list; the banner will be empty.
		// / 超时 → open|filtered。UDP 沉默可能是"open 但静默"或
		// "filtered"。我们标 Open 让 plugin 流水线仍有机会识别服
		// 务（如果用户给了端口列表）；banner 为空。
		if isTimeout(readErr) {
			return Result{
				Host: host, Port: port, State: StateOpen,
				Method: MethodUDP, Banner: "",
				RTT: time.Since(start), Time: time.Now(),
			}, nil
		}
	}
	// Fallthrough: ambiguous. / 兜底：模糊。
	return Result{
		Host: host, Port: port, State: StateOpen,
		Method: MethodUDP, Banner: "",
		RTT: time.Since(start), Time: time.Now(),
	}, nil
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
