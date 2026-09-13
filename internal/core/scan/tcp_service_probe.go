// Package scan: TCP service fingerprint probe.
//
// TCPServiceProbe is the TCP counterpart of UDPServiceProbe: for an
// OPEN port whose passive banner grab came up empty, it sends the
// nmap-service-probes payloads round-by-round on ONE connection and
// returns the FIRST non-empty response as the port's banner. Unlike
// UDP (where silence is ambiguous), a TCP port is already known open —
// the only question is what it answers when spoken to, so every
// payload gets its own short read before the next one fires.
//
// / Package scan：TCP 服务指纹探针。
//
// TCPServiceProbe 是 UDPServiceProbe 的 TCP 对应物：对被动 banner 抓
// 取空手的 OPEN 端口，在一条连接上逐轮发送 nmap-service-probes
// payload，把首个非空响应作为端口 banner。与 UDP 不同（UDP 静默是
// 模糊的），TCP 端口已确认 open——唯一的问题是搭话后它答什么，所以
// 每个 payload 各配一次短读，读完再发下一个。
package scan

import (
	"context"
	"fmt"
	"net"
	"time"
)

// TCPServiceProbe sends service-specific payloads to a silent open TCP
// port and captures the first response.
// / TCPServiceProbe 对沉默的开放 TCP 端口发送服务专属 payload 并捕
// 获首个响应。
type TCPServiceProbe struct {
	// Payloads returns the probe payloads for a port (hint probes, or
	// the generic rarity-1 trio for unhinted ports). May return nil —
	// the caller should then skip probing entirely.
	// / Payloads 返回端口的探针 payload（hint 探针，或无提示端口的
	// rarity-1 通用三件套）。可返回 nil——调用方此时应完全跳过探测。
	Payloads func(port int) [][]byte

	// DialTimeout caps socket setup and each write. Zero = 2s.
	// / DialTimeout 限制建连与每次写。零 = 2s。
	DialTimeout time.Duration

	// ReadTimeout caps the wait for a response after EACH payload.
	// Zero = 1500ms. / ReadTimeout 限制每个 payload 发出后等响应的时
	// 间。零 = 1500ms。
	ReadTimeout time.Duration
}

// NewTCPServiceProbe returns a TCPServiceProbe with the default
// budgets (2s dial, 1.5s per-probe read). Not a pooled Probe — plugin
// workers invoke ProbeBanner directly, one connection per silent item.
// / NewTCPServiceProbe 返回默认预算（2s 建连、每探针 1.5s 读）的
// TCPServiceProbe。不是池化 Probe——plugin worker 直接调
// ProbeBanner，每个沉默 item 一条连接。
func NewTCPServiceProbe(payloads func(port int) [][]byte) *TCPServiceProbe {
	return &TCPServiceProbe{Payloads: payloads}
}

// ProbeBanner dials host:port and sends each payload round-by-round,
// returning the first non-empty response (raw bytes, no trimming —
// binary protocols like RPC/DB2 answer with non-ASCII that
// end-anchored rules need intact). A nil banner means the port stayed
// silent through the whole payload list; err is non-nil only on
// context cancellation. Connection-level failures (RST, refused
// mid-probe) return whatever was collected so far — the port was
// already known open, so a reset after a probe is a service quirk,
// not a scan error.
// / ProbeBanner 拨号 host:port 并逐轮发送 payload，返回首个非空响应
// （原始字节，不裁剪——RPC/DB2 这类二进制协议以非 ASCII 回应，行尾
// 锚定的规则需要完整字节）。nil banner 表示整个 payload 列表都沉默
// ；err 仅在 ctx 取消时非 nil。连接级失败（探针中 RST、refused）返
// 回已收集的部分——端口本已确认 open，探针后被重置是服务怪癖，不
// 是扫描错误。
func (p *TCPServiceProbe) ProbeBanner(ctx context.Context, host string, port int, timeout time.Duration) ([]byte, error) {
	var payloads [][]byte
	if p.Payloads != nil {
		payloads = p.Payloads(port)
	}
	if len(payloads) == 0 {
		return nil, nil
	}
	dialTimeout := p.DialTimeout
	if dialTimeout <= 0 {
		dialTimeout = 2 * time.Second
	}
	readTimeout := p.ReadTimeout
	if readTimeout <= 0 {
		readTimeout = 1500 * time.Millisecond
	}
	if timeout > 0 && timeout < dialTimeout {
		dialTimeout = timeout
	}

	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	d := net.Dialer{Timeout: dialTimeout}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, nil // open minutes ago; treat dial loss as silence
	}
	defer conn.Close()

	buf := make([]byte, 1024)
	for _, payload := range payloads {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		_ = conn.SetWriteDeadline(time.Now().Add(dialTimeout))
		if _, werr := conn.Write(payload); werr != nil {
			// A refused/reset write kills the connection — no point
			// trying further payloads. / 写被拒/重置意味着连接已死，
			// 换 payload 无意义。
			return nil, nil
		}
		_ = conn.SetReadDeadline(time.Now().Add(readTimeout))
		n, _ := conn.Read(buf)
		if n > 0 {
			b := make([]byte, n)
			copy(b, buf[:n])
			return b, nil
		}
	}
	return nil, nil
}
