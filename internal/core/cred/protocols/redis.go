// Package protocols: Redis authenticator.
// Package protocols: Redis 认证器。
//
// Implements password authentication against a Redis server using
// the raw RESP protocol. We DO NOT call any post-auth command
// (no CONFIG SET, no SET, no SLAVEOF, no MODULE LOAD) — on a
// hit we return the first successful AuthMethod and the pipeline
// writes the (user, pass) to creds.txt.
//
// 用原生 RESP 协议对 Redis 服务器做密码认证。我们不调任何认证后
// 命令（不 CONFIG SET / SET / SLAVEOF / MODULE LOAD）——命中时
// 返回首个成功的 AuthMethod，管线把 (user, pass) 写入 creds.txt。
//
// RESP protocol (simplified):
//   - client → server: *<count>\r\n$<len>\r\n<arg>\r\n...
//   - server → client: +<simple>\r\n (simple string)
//                       -<error>\r\n   (error)
//                       :<integer>\r\n (integer)
//                       $<len>\r\n<data>\r\n (bulk string)
//                       *<count>\r\n... (array)
//
// RESP 协议（简化）：
//   - 客户端 → 服务器：*<count>\r\n$<len>\r\n<arg>\r\n...
//   - 服务器 → 客户端：+<simple>\r\n / -<error>\r\n / :<integer>\r\n / $<len>\r\n / *<count>\r\n
package protocols

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/core/cred"
)

// RedisAuthenticator authenticates against Redis servers.
// RedisAuthenticator 对 Redis 服务器进行认证。
type RedisAuthenticator struct{}

// NewRedisAuthenticator returns a default Redis authenticator.
// NewRedisAuthenticator 返回默认 Redis 认证器。
func NewRedisAuthenticator() *RedisAuthenticator { return &RedisAuthenticator{} }

// Name implements cred.Authenticator. / Name 实现 cred.Authenticator。
func (a *RedisAuthenticator) Name() string { return "redis" }

// DefaultPorts implements cred.Authenticator. / DefaultPorts 实现 cred.Authenticator。
func (a *RedisAuthenticator) DefaultPorts() []int { return []int{6379, 6380} }

// Authenticate implements cred.Authenticator.
//
// Algorithm:
//   1. Connect.
//   2. Send PING.
//      - +PONG → no password required, return hit with empty cred.
//      - -NOAUTH → password required; continue to step 3.
//      - -ERR (other) → not a Redis server, return nil.
//   3. Try each cred with AUTH.
//      - +OK → hit.
//      - -WRONGPASS → wrong password, try next.
//      - timeout / network err → bail out for this host.
//
// Authenticate 实现 cred.Authenticator。
//
// 算法：
//   1. 连。
//   2. 发 PING。+PONG → 不需密码，返回命中（空 cred）。
//      -NOAUTH → 需密码，进 step 3。其他 -ERR → 不是 Redis，返回 nil。
//   3. 用每个 cred 试 AUTH。+OK → 命中。-WRONGPASS → 错密码，try next。
//      超时/网络错 → 该 host 中止。
func (a *RedisAuthenticator) Authenticate(ctx context.Context, host string, port int, creds []cred.Cred, timeout time.Duration) (*cred.Hit, error) {
	if len(creds) == 0 {
		return nil, nil
	}
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	d := net.Dialer{Timeout: timeout}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))
	br := bufio.NewReader(conn)

	// Step 1: PING. / Step 1: PING。
	if err := writeRESP(conn, "PING"); err != nil {
		return nil, err
	}
	pingLine, err := readRESP(br)
	if err != nil {
		// Could not read PING response — could be a non-Redis
		// server (e.g. HTTP banner, FIN/RST mid-handshake on Windows).
		// Treat as "not Redis" rather than bubbling up a network
		// error so the scheduler moves to the next target.
		// / 读不到 PING 响应——可能是非 Redis 服务（如 HTTP banner、
		// Windows 上 FIN/RST 中断握手）。视为"非 Redis"而非冒泡
		// 网络错误，让调度器转到下一个 target。
		return nil, nil
	}
	switch {
	case strings.HasPrefix(pingLine, "+PONG"):
		// No password. / 不需密码。
		return &cred.Hit{
			Cred:     cred.Cred{User: "", Pass: "", Method: cred.AuthPassword},
			Attempts: 1,
			Time:     time.Now(),
		}, nil
	case strings.HasPrefix(pingLine, "-NOAUTH"):
		// Need password — fall through. / 需密码——继续。
	case strings.HasPrefix(pingLine, "-ERR"):
		// Other error — not a Redis. / 其他错误——不是 Redis。
		return nil, nil
	default:
		return nil, nil
	}

	// Step 2: AUTH with each cred. / Step 2：用每个 cred 试 AUTH。
	for i, c := range creds {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if c.Method != "" && c.Method != cred.AuthPassword {
			continue
		}
		if err := writeRESP(conn, "AUTH", c.Pass); err != nil {
			return nil, err
		}
		authLine, err := readRESP(br)
		if err != nil {
			return nil, err
		}
		if strings.HasPrefix(authLine, "+OK") {
			return &cred.Hit{
				Cred:     c,
				Attempts: i + 2, // 1 for PING + i for AUTH tries
				Time:     time.Now(),
			}, nil
		}
		// -WRONGPASS or other: try next. / -WRONGPASS 或其他：试下一个。
	}
	return nil, nil
}

// writeRESP writes a RESP command of the form `args...` to conn.
// writeRESP 写 RESP 命令 `args...` 到 conn。
func writeRESP(conn net.Conn, args ...string) error {
	buf := make([]byte, 0, 64)
	buf = append(buf, fmt.Sprintf("*%d\r\n", len(args))...)
	for _, a := range args {
		buf = append(buf, fmt.Sprintf("$%d\r\n%s\r\n", len(a), a)...)
	}
	_, err := conn.Write(buf)
	return err
}

// readRESP reads one RESP reply (simple string / error / bulk /
// integer / array — we only look at the first line for our needs).
//
// readRESP 读一条 RESP 响应（简单字符串 / 错误 / bulk / 整数 / 数组——
// 我们只看首行）。
func readRESP(br *bufio.Reader) (string, error) {
	line, err := br.ReadString('\n')
	if err != nil {
		return "", err
	}
	line = strings.TrimRight(line, "\r\n")
	// For simple strings/errors, the line is "+X\r\n" / "-X\r\n" — the
	// whole line is the answer. For bulk strings we'd need to read
	// the next $<len> header, but AUTH/PING return simple/error.
	// / 简单字符串/错误的 line 是 "+X\r\n" / "-X\r\n"——整 line 是答案。
	// bulk 要读下个 $<len> 头，但 AUTH/PING 返回 simple/error。
	return line, nil
}
