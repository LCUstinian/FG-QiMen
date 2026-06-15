// Package scan: TCP-connect probe.
// Package scan: TCP-connect 探测。
//
// The default v0.1 probe. Performs a full 3-way TCP handshake; if the
// handshake completes the port is Open. We close the connection
// immediately after to avoid holding sockets.
//
// v0.1 默认探测。执行完整 TCP 三次握手；握手成功即视为 Open。完成后
// 立即关闭连接避免占着 socket。
package scan

import (
	"context"
	"fmt"
	"net"
	"time"
)

// TCPConnectProbe probes ports by completing a full TCP handshake.
// TCPConnectProbe 通过完成完整 TCP 握手探测端口。
type TCPConnectProbe struct {
	// BannerReader optionally reads the first banner bytes from an
	// accepted connection (e.g. SSH version string). Nil means
	// "no banner grab" (faster). / BannerReader 可选地从接受的连接
	// 读首个 banner 字节（如 SSH 版本字符串）。nil 表示"不抓 banner"（更快）。
	BannerReader BannerReader

	// BannerTimeout caps the time spent reading the banner. Zero =
	// 200ms default. / BannerTimeout 限制读 banner 的时间。零 = 200ms 默认。
	BannerTimeout time.Duration
}

// BannerReader is called on an accepted connection and returns the
// banner (or "" if none). Implementations should be quick; the
// connection is closed as soon as the reader returns.
//
// BannerReader 在接受的连接上调用，返回 banner（或 "" 表示无）。
// 实现应当快速；reader 返回后立即关闭连接。
type BannerReader func(conn net.Conn) string

// NewTCPConnectProbe returns a TCPConnectProbe without banner grabbing.
// NewTCPConnectProbe 返回不抓 banner 的 TCPConnectProbe。
func NewTCPConnectProbe() *TCPConnectProbe {
	return &TCPConnectProbe{BannerTimeout: 200 * time.Millisecond}
}

// NewTCPConnectProbeWithBanner returns a TCPConnectProbe that reads
// the first banner via the given reader. / NewTCPConnectProbeWithBanner
// 返回通过给定 reader 读首个 banner 的 TCPConnectProbe。
func NewTCPConnectProbeWithBanner(br BannerReader) *TCPConnectProbe {
	return &TCPConnectProbe{
		BannerReader:  br,
		BannerTimeout: 200 * time.Millisecond,
	}
}

// Name implements Probe. / Name 实现 Probe。
func (p *TCPConnectProbe) Name() string { return "tcp-connect" }

// Method implements Probe. / Method 实现 Probe。
func (p *TCPConnectProbe) Method() Method { return MethodTCPConnect }

// Available implements Probe. TCP-connect needs no special privileges.
// Available 实现 Probe。TCP-connect 不需要特殊权限。
func (p *TCPConnectProbe) Available() error { return nil }

// Probe implements Probe. / Probe 实现 Probe。
func (p *TCPConnectProbe) Probe(ctx context.Context, host string, port int, timeout time.Duration) (Result, error) {
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	d := net.Dialer{Timeout: timeout}
	start := time.Now()
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		// Distinguish "refused" (closed) from "timeout / unreachable" (filtered).
		// 区分"拒绝"（closed）和"超时/不可达"（filtered）。
		if isConnRefused(err) {
			return Result{
				Host: host, Port: port, State: StateClosed,
				Method: MethodTCPConnect, RTT: time.Since(start),
				Time: time.Now(),
			}, nil
		}
		return Result{
			Host: host, Port: port, State: StateFiltered,
			Method: MethodTCPConnect, RTT: time.Since(start),
			Time: time.Now(),
		}, nil
	}
	// Open! Close immediately. / Open！立即关闭。
	rtt := time.Since(start)
	_ = conn.Close()

	var banner string
	if p.BannerReader != nil {
		// P1#5: the previous code reused the outer dialer `d` for
		// the banner redial, which has Timeout = the full probe
		// timeout (typically 3s) — but p.BannerTimeout is meant
		// to be the per-attempt budget (default 200ms). The
		// redial would block for up to the full probe timeout
		// when the intent was a quick banner read.
		//
		// Fix: build a separate dialer for the redial with
		// BannerTimeout as its own Timeout AND carry ctx so
		// cancellation still works. The original socket is
		// already closed above; this is a fresh connection.
		//
		// P1#5：旧代码用外层 dialer `d`（Timeout = 完整 probe 超时，
		// 通常 3s）做 banner 重连，但 p.BannerTimeout 的设计意图
		// 是单次尝试预算（默认 200ms）。重连最多阻塞完整 probe
		// 超时，本意是快速读 banner。
		//
		// 修法：给重连单独建一个 dialer，用 BannerTimeout 作自身
		// Timeout，同时带 ctx 让取消照旧生效。原 socket 已经在上
		// 面关闭；这是新连接。
		bannerD := net.Dialer{Timeout: p.BannerTimeout}
		bConn, err := bannerD.DialContext(ctx, "tcp", addr)
		if err == nil {
			banner = readBanner(bConn, p.BannerTimeout)
			_ = bConn.Close()
		}
	}

	return Result{
		Host: host, Port: port, State: StateOpen,
		Method: MethodTCPConnect, Banner: banner, RTT: rtt,
		Time: time.Now(),
	}, nil
}

// readBanner reads up to 256 bytes from conn within timeout, trims
// whitespace, and returns it. / readBanner 在 timeout 内从 conn 读最多
// 256 字节，去空白后返回。
func readBanner(conn net.Conn, timeout time.Duration) string {
	if timeout <= 0 {
		timeout = 200 * time.Millisecond
	}
	_ = conn.SetReadDeadline(time.Now().Add(timeout))
	buf := make([]byte, 256)
	n, _ := conn.Read(buf)
	return trimASCII(buf[:n])
}

// trimASCII strips non-printable bytes and trims whitespace. Banner
// data can include CR/LF we want to drop. / trimASCII 去除非可打印
// 字节和首尾空白。banner 数据可能含 CR/LF 应去除。
func trimASCII(b []byte) string {
	out := make([]byte, 0, len(b))
	for _, c := range b {
		switch {
		case c == '\r' || c == '\n' || c == '\t':
			out = append(out, ' ')
		case c >= 32 && c < 127:
			out = append(out, c)
		}
	}
	// trim leading/trailing spaces / 去除首尾空格
	for len(out) > 0 && out[0] == ' ' {
		out = out[1:]
	}
	for len(out) > 0 && out[len(out)-1] == ' ' {
		out = out[:len(out)-1]
	}
	return string(out)
}
