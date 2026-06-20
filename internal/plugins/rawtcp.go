// rawtcp.go — shared TCP probe helper for protocol Identify plugins.
//
// rawtcp.go — 协议 Identify 插件共享的 TCP 探测助手。
//
// Most Identify plugins open a TCP connection, set a deadline, run a
// short request/response handshake, and return a *types.Result. The
// boilerplate (net.JoinHostPort + net.Dialer{Timeout} + conn.SetDeadline
// + defer conn.Close) was duplicated across 7+ plugins. RawTCPIdentify
// factors it out so each plugin's Identify is just the protocol-
// specific I/O.
//
// 多数 Identify 插件都开 TCP 连接、设 deadline、跑一个短请求/响应
// 握手、返回 *types.Result。样板代码（net.JoinHostPort +
// net.Dialer{Timeout} + conn.SetDeadline + defer conn.Close）在 7+
// 插件里重复。RawTCPIdentify 把这层抽出来，每个插件的 Identify 只
// 剩协议特有的 I/O。
package plugins

import (
	"context"
	"net"
	"strconv"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/types"
)

// DefaultIdentifyTimeout is the per-call deadline applied by
// RawTCPIdentify when fn is nil. Plugin authors can override by
// passing a custom timeout via the variadic option.
//
// DefaultIdentifyTimeout 是 RawTCPIdentify 在 fn 为 nil 时应用的
// per-call deadline。插件作者可通过 variadic option 覆盖。
const DefaultIdentifyTimeout = 3 * time.Second

// RawTCPOption customises RawTCPIdentify behaviour. / RawTCPOption
// 自定义 RawTCPIdentify 行为。
type RawTCPOption func(*rawTCPConfig)

type rawTCPConfig struct {
	timeout time.Duration
}

// WithIdentifyTimeout overrides the per-call deadline. / WithIdentifyTimeout
// 覆盖 per-call deadline。
func WithIdentifyTimeout(d time.Duration) RawTCPOption {
	return func(c *rawTCPConfig) { c.timeout = d }
}

// RawTCPIdentify opens host:port, sets a deadline, and invokes fn
// with the live conn. fn's return value becomes the Identify result;
// a fn error is treated as "couldn't identify" and silently returns
// nil (consistent with the previous per-plugin behaviour). On dial
// failure, RawTCPIdentify also returns nil.
//
// The boilerplate elimination saves ~10 lines per plugin and centralises
// the 3-second default (matching the legacy inline magic constant).
//
// RawTCPIdentify 打开 host:port、设 deadline、调 fn 传入活跃 conn。
// fn 的返回值作为 Identify 结果；fn 错误视为"无法识别"并静默返回
// nil（与旧 per-plugin 行为一致）。拨号失败时 RawTCPIdentify 也返
// 回 nil。
//
// 样板消除每个插件省 ~10 行，把 3 秒默认集中到一处（匹配旧的内联
// magic 常量）。
func RawTCPIdentify(
	ctx context.Context,
	host string,
	port int,
	fn func(net.Conn) *types.Result,
	opts ...RawTCPOption,
) *types.Result {
	return identifyWithNet(ctx, host, port, "tcp", fn, opts)
}

// RawUDPIdentify is the UDP sibling of RawTCPIdentify. The deadline
// is set on the conn like the TCP version; UDP-specific quirks
// (no FIN, no RST) are the caller's problem. / RawUDPIdentify 是
// RawTCPIdentify 的 UDP 同伴。deadline 设置同 TCP 版本；UDP 特
// 有怪癖（无 FIN、无 RST）由调用方处理。
func RawUDPIdentify(
	ctx context.Context,
	host string,
	port int,
	fn func(net.Conn) *types.Result,
	opts ...RawTCPOption,
) *types.Result {
	return identifyWithNet(ctx, host, port, "udp", fn, opts)
}

// identifyWithNet is the shared implementation for RawTCPIdentify
// and RawUDPIdentify. / identifyWithNet 是 RawTCPIdentify 和
// RawUDPIdentify 的共享实现。
func identifyWithNet(
	ctx context.Context,
	host string,
	port int,
	network string,
	fn func(net.Conn) *types.Result,
	opts []RawTCPOption,
) *types.Result {
	if fn == nil {
		return nil
	}
	cfg := rawTCPConfig{timeout: DefaultIdentifyTimeout}
	for _, opt := range opts {
		opt(&cfg)
	}
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	conn, err := (&net.Dialer{Timeout: cfg.timeout}).DialContext(ctx, network, addr)
	if err != nil {
		return nil
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(cfg.timeout))
	return fn(conn)
}
