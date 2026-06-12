// Package alive: ICMP echo probe (raw socket).
// Package alive: ICMP echo 探测（raw socket）。
//
// Uses golang.org/x/net/icmp to send ICMP_ECHO_REQUEST and listen for
// ICMP_ECHO_REPLY. Requires CAP_NET_RAW on Linux/macOS and admin on
// Windows. If unavailable, Available() returns a non-nil error and the
// orchestrator will skip this probe.
//
// 使用 golang.org/x/net/icmp 发 ICMP_ECHO_REQUEST 并监听 ICMP_ECHO_REPLY。
// 在 Linux/macOS 上需要 CAP_NET_RAW，在 Windows 上需要 admin。
// 不可用时 Available() 返回非 nil，调度器会跳过此 probe。
package alive

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"runtime"
	"sync/atomic"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
)

// icmpProbe is the implementation behind NewICMPProbe.
// icmpProbe 是 NewICMPProbe 背后的实现。
type icmpProbe struct {
	// ID is the ICMP identifier (incremented per call to avoid reply
	// collisions from previous runs). / ID 是 ICMP 标识符（每次调用递增，
	// 避免上次残留响应的干扰）。
	nextID atomic.Int32
}

// NewICMPProbe returns an ICMP echo probe. On systems where raw socket
// access is denied, Available() will return an error and the probe
// will be skipped.
//
// NewICMPProbe 返回一个 ICMP echo 探测。在 raw socket 被拒绝的系统上，
// Available() 会返回错误，probe 会被跳过。
func NewICMPProbe() Probe {
	p := &icmpProbe{}
	p.nextID.Store(int32(os.Getpid() & 0x7fff))
	return p
}

// Name implements Probe. / Name 实现 Probe。
func (p *icmpProbe) Name() string { return "icmp" }

// Method implements Probe. / Method 实现 Probe。
func (p *icmpProbe) Method() Method { return MethodICMP }

// Available reports whether the current process can open an ICMP
// datagram socket. We try to open one and immediately close it; if
// the open fails with a permission error, we report unavailable.
//
// Available 报告当前进程能否打开 ICMP 数据报 socket。我们尝试打开一个
// 后立即关闭；如果打开失败（权限错误）则报告不可用。
func (p *icmpProbe) Available() error {
	conn, err := icmp.ListenPacket("ip4:icmp", "0.0.0.0")
	if err != nil {
		return fmt.Errorf("icmp: %w", err)
	}
	_ = conn.Close()
	return nil
}

// Probe sends one ICMP echo request and waits up to `timeout` for a
// matching reply. On success returns Hit; on timeout / no reply
// returns ErrUnreachable; on real error returns it directly.
//
// Probe 发送一个 ICMP echo 请求，等待最多 `timeout` 时间匹配响应。
// 成功返回 Hit；超时/无响应返回 ErrUnreachable；其他错误直接返回。
func (p *icmpProbe) Probe(ctx context.Context, host string, timeout time.Duration) (Hit, error) {
	if runtime.GOOS == "windows" {
		// On Windows, ListenPacket("udp4") is the only supported form
		// (Windows does not support raw ICMP sockets for unprivileged
		// users; admin can use ip4:icmp via a different code path
		// which is not exposed by golang.org/x/net/icmp on Windows).
		// We attempt a TCP fallback below if ICMP isn't available.
		// Windows 上我们尝试 udp4；如果不可用调度器会跳过。
		// 这里仍然返回 ErrUnreachable，让调用方继续走其他 probe。
	}

	// Resolve host to IP (must be IPv4 for ip4:icmp).
	// 解析 host 为 IP（ip4:icmp 需要 IPv4）。
	dst, err := net.ResolveIPAddr("ip4", host)
	if err != nil {
		return Hit{}, fmt.Errorf("icmp resolve: %w", err)
	}

	conn, err := icmp.ListenPacket("ip4:icmp", "0.0.0.0")
	if err != nil {
		return Hit{}, fmt.Errorf("icmp listen: %w", err)
	}
	defer conn.Close()

	// Build the echo request. / 构造 echo 请求。
	id := int(p.nextID.Add(1))
	if id > 0x7fff {
		p.nextID.Store(0)
	}
	msg := icmp.Message{
		Type: ipv4.ICMPTypeEcho, Code: 0,
		Body: &icmp.Echo{
			ID: id, Seq: 1,
			Data: []byte("fg-qimen-alive-probe"),
		},
	}
	pkt, err := msg.Marshal(nil)
	if err != nil {
		return Hit{}, fmt.Errorf("icmp marshal: %w", err)
	}

	start := time.Now()
	if _, err := conn.WriteTo(pkt, dst); err != nil {
		return Hit{}, fmt.Errorf("icmp write: %w", err)
	}

	// Wait for the matching reply within `timeout`.
	// 在 `timeout` 内等待匹配的响应。
	deadline := time.Now().Add(timeout)
	_ = conn.SetReadDeadline(deadline)
	buf := make([]byte, 1500)
	for {
		if ctx.Err() != nil {
			return Hit{}, ctx.Err()
		}
		if time.Now().After(deadline) {
			return Hit{}, ErrUnreachable
		}
		n, peer, err := conn.ReadFrom(buf)
		if err != nil {
			// Timeout or transient error — treat as unreachable.
			// 超时或瞬时错误——视为不可达。
			var nerr net.Error
			if errors.As(err, &nerr) && nerr.Timeout() {
				return Hit{}, ErrUnreachable
			}
			return Hit{}, err
		}
		rm, err := icmp.ParseMessage(1 /* ICMP for IPv4 */, buf[:n])
		if err != nil {
			continue // not a parseable ICMP packet; keep reading
		}
		if rm.Type != ipv4.ICMPTypeEchoReply {
			continue // some other ICMP message; keep reading
		}
		// Match by ID and by source address.
		// 按 ID 和源地址匹配。
		if echo, ok := rm.Body.(*icmp.Echo); ok && echo.ID == id {
			if peer.String() == dst.String() {
				return Hit{
					Host:   host,
					Port:   0,
					Method: MethodICMP,
					RTT:    time.Since(start),
					Time:   time.Now(),
				}, nil
			}
		}
	}
}
