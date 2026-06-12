// Package protocols: memcached authenticator.
// Package protocols：memcached 认证器。
//
// memcached has no built-in authentication (most deployments rely
// on network ACLs). However, newer versions support the optional
// "auth" command (ASCII: "auth <user> <pass>\r\n"). The plugin
// probes for auth support first, then tries each (user, pass) cred.
//
// memcached 默认无认证（多靠网络 ACL）。但新版支持可选的 "auth"
// 命令（ASCII："auth <user> <pass>\r\n"）。本插件先探测是否支持 auth，
// 再对每个 (user, pass) 凭据试。
//
// HARD RULE: no SET / GET / FLUSH — we only call `auth`. On a hit
// we return the cred; the pipeline writes to creds.txt.
//
// 硬性原则：不调 SET / GET / FLUSH——只调 `auth`。命中时返回 cred，
// 管线写到 creds.txt。
package protocols

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/LCUstinian/FG-QiMen/core/cred"
)

// MemcachedAuthenticator authenticates against memcached.
// MemcachedAuthenticator 对 memcached 进行认证。
type MemcachedAuthenticator struct{}

// NewMemcachedAuthenticator returns a default memcached authenticator.
// NewMemcachedAuthenticator 返回默认 memcached 认证器。
func NewMemcachedAuthenticator() *MemcachedAuthenticator { return &MemcachedAuthenticator{} }

// Name implements cred.Authenticator. / Name 实现 cred.Authenticator。
func (a *MemcachedAuthenticator) Name() string { return "memcached" }

// DefaultPorts implements cred.Authenticator. / DefaultPorts 实现 cred.Authenticator。
func (a *MemcachedAuthenticator) DefaultPorts() []int { return []int{11211, 11212} }

// Authenticate implements cred.Authenticator.
//
// Algorithm:
//   1. Connect.
//   2. Send `version\r\n` — confirms it's memcached.
//   3. Send `auth <user> <pass>\r\n` for each cred.
//      - "STORED" or empty reply → no auth supported (server is open).
//        We treat this as a hit (we don't escalate further).
//      - "OK\r\n" → auth succeeded.
//      - "CLIENT_ERROR ...\r\n" → wrong creds / SASL error.
//   4. If we hit a successful auth, return hit.
//
// Authenticate 实现 cred.Authenticator。
//
// 算法：
//   1. 连。
//   2. 发 `version\r\n` —— 确认是 memcached。
//   3. 对每个 cred 发 `auth <user> <pass>\r\n`。
//      - "STORED" 或空响应 → 服务不需认证（开）—— 视为命中（不再升级）。
//      - "OK\r\n" → 认证成功。
//      - "CLIENT_ERROR ...\r\n" → 错凭据 / SASL 错误。
//   4. 命中即返回。
func (a *MemcachedAuthenticator) Authenticate(ctx context.Context, host string, port int, creds []cred.Cred, timeout time.Duration) (*cred.Hit, error) {
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

	// Step 1: version probe. / Step 1: version 探针。
	if err := writeMemcCmd(conn, "version"); err != nil {
		return nil, err
	}
	verLine, err := br.ReadString('\n')
	if err != nil {
		// Couldn't read version response — could be a non-memcached
		// server (FIN/RST mid-handshake on Windows). Treat as
		// "not memcached" rather than bubbling up a network error.
		// / 读不到 version 响应——可能是非 memcached 服务（Windows 上
		// FIN/RST 中断握手）。视为"非 memcached"而非冒泡网络错误。
		return nil, nil
	}
	if !strings.HasPrefix(strings.TrimSpace(verLine), "VERSION ") {
		return nil, nil // not memcached
	}

	// Step 2: try each cred. / Step 2: 试每个 cred。
	for i, c := range creds {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if c.Method != "" && c.Method != cred.AuthPassword {
			continue
		}
		// memcached's auth command is "auth <user> <pass>". When no
		// user is set, just "auth <pass>" is accepted.
		// / memcached 的 auth 命令是 "auth <user> <pass>"。未设 user
		// 时只发 "auth <pass>"。
		cmd := "auth"
		if c.User != "" {
			cmd = "auth " + c.User
		}
		if err := writeMemcCmd(conn, cmd, c.Pass); err != nil {
			return nil, err
		}
		authLine, err := br.ReadString('\n')
		if err != nil {
			return nil, err
		}
		authLine = strings.TrimSpace(authLine)
		switch {
		case authLine == "":
			// Server didn't say anything — likely pre-1.6.x with no auth.
			// / 服务器没回应——可能是 1.6.x 前无 auth。
			return &cred.Hit{
				Cred:     c,
				Attempts: i + 1,
				Time:     time.Now(),
			}, nil
		case strings.HasPrefix(authLine, "STORED"):
			// Some memcached variants reply STORED to "auth".
			// / 部分变体对 "auth" 回复 STORED。
			return &cred.Hit{
				Cred:     c,
				Attempts: i + 1,
				Time:     time.Now(),
			}, nil
		case authLine == "OK":
			return &cred.Hit{
				Cred:     c,
				Attempts: i + 1,
				Time:     time.Now(),
			}, nil
		case strings.HasPrefix(authLine, "CLIENT_ERROR"):
			// Wrong creds. Try next. / 错凭据。试下一个。
			continue
		case strings.HasPrefix(authLine, "SERVER_ERROR"):
			// Server-side auth subsystem error. / 服务器端 auth 子系统错误。
			continue
		case strings.HasPrefix(authLine, "ERROR"):
			// Old-style error reply. / 老式错误响应。
			continue
		}
	}
	return nil, nil
}

// writeMemcCmd writes a memcached text command followed by \r\n.
// writeMemcCmd 写 memcached 文本命令后跟 \r\n。
func writeMemcCmd(conn net.Conn, args ...string) error {
	line := strings.Join(args, " ") + "\r\n"
	_, err := conn.Write([]byte(line))
	return err
}
