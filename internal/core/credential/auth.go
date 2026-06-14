// auth.go — small helpers shared by every per-protocol authenticator
// in core/credential/auth/<cat>/. The full set of helpers is
// deliberately tiny: every protocol has different wire formats,
// framing, and state machines, so the only universally-shared
// plumbing is "open a TCP connection with a per-call deadline" and
// "wrap an error with the protocol name".
//
// New authenticators should call these helpers rather than re-rolling
// the dial / error-wrap pattern — it keeps the 27 protocols
// consistent and makes future changes (e.g. switch the dialer to
// happy-eyeballs DNS) land in one place.
//
// auth.go — core/credential/auth/<cat>/ 下各 authenticator 共享的
// 小工具集。各协议线格式、状态机差异大，唯一通用的是"按超时打开
// TCP 连接"和"用协议名注释错误"。
//
// 新 authenticator 应当调用这些 helper，而非重新展开 dial / 错误包装
// 模板——保证 27 个协议一致，让未来的改动（例如把 dialer 切到
// happy-eyeballs DNS）只需改一处。
package credential

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"time"
)

// DialTCP opens a TCP connection to host:port and sets a single
// read/write deadline equal to `timeout`. The deadline protects
// against auth handshakes that hang after the TCP three-way
// handshake completes (a common pattern in old or odd services that
// silently accept the connection then never reply).
//
// DialTCP 打开到 host:port 的 TCP 连接，并设置与 timeout 相等的单次
// 读写 deadline。该 deadline 防御 TCP 三次握手完成后认证握手挂起的
// 情况（旧服务或怪协议常见：连接后静默但不回包）。
//
// host may be an IP literal or a DNS name; the net.Dialer handles
// resolution. ctx is honoured for cancellation; the dial returns
// promptly with ctx.Err() if cancelled. / host 可以是 IP 字面量或
// DNS 名；net.Dialer 处理解析。ctx 用于取消；取消时 dial 立即返回
// ctx.Err()。
func DialTCP(ctx context.Context, host string, port int, timeout time.Duration) (net.Conn, error) {
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	d := net.Dialer{Timeout: timeout}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	_ = conn.SetDeadline(time.Now().Add(timeout))
	return conn, nil
}

// WrapError annotates err with the protocol name + operation so log
// lines like "redis: AUTH login: connection refused" are uniform
// across the auth tree. Returns nil when err is nil so callers can
// write `return WrapError("ssh", "NewClientConn", err)` directly
// at the end of an attempt.
//
// WrapError 用协议名 + 操作名注释 err，让日志行 "redis: AUTH login:
// connection refused" 在 auth 树下统一。err 为 nil 时返回 nil，
// 这样调用方可直接 `return WrapError("ssh", "NewClientConn", err)`
// 写在尝试结尾。
func WrapError(protocol, op string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %s: %w", protocol, op, err)
}
