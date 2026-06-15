// Package protocols: SOCKS5 authenticator.
//
// Strategy: SOCKS5 RFC 1928 + RFC 1929 user/pass auth. Greeting:
// client sends version 5 + N methods (we offer 0x02 = user/pass).
// Server selects method. We send username/password. Server replies
// 0x00 (success) or 0x01 (failure). On success we close — no
// CONNECT/BIND request follows.
//
// HARD RULE: on a hit we return. No CONNECT to remote hosts through
// the proxy.
//
// 包 protocols：SOCKS5 认证器。
// 策略：SOCKS5 RFC 1928 + RFC 1929 user/pass auth。Greeting：客户端
// 发 version 5 + N methods（提供 0x02 = user/pass）。服务器选 method。
// 客户端发 username/password。服务器回 0x00（成功）或 0x01（失败）。
// 成功即关连接——不发 CONNECT/BIND 请求。
//
// 硬性原则：命中即返回。不通过代理 CONNECT 远程主机。
package network

import (
	"context"
	"net"
	"strconv"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/core/credential"
)

// SOCKS5Authenticator authenticates against SOCKS5 proxies via RFC
// 1928/1929. / SOCKS5Authenticator 通过 RFC 1928/1929 对 SOCKS5 代理
// 认证。
//
// DefaultPorts returns 1080 (standard SOCKS5). / DefaultPorts 返 1080
// （标准 SOCKS5）。
type SOCKS5Authenticator struct{}

// NewSOCKS5Authenticator returns a default SOCKS5 authenticator.
// NewSOCKS5Authenticator 返回默认配置的 SOCKS5 认证器。
func NewSOCKS5Authenticator() *SOCKS5Authenticator { return &SOCKS5Authenticator{} }

// Name implements credential.Authenticator. / Name 实现 credential.Authenticator。
func (a *SOCKS5Authenticator) Name() string { return "socks5" }

// DefaultPorts implements credential.Authenticator. / DefaultPorts 实现 credential.Authenticator。
func (a *SOCKS5Authenticator) DefaultPorts() []int {
	return []int{1080}
}

// SOCKS5 wire constants. / SOCKS5 线常量。
const (
	socks5Version       byte = 0x05
	socks5MethodNoAuth  byte = 0x00
	socks5MethodUserPass byte = 0x02
	socks5MethodNoAccept byte = 0xFF
	socks5AuthVersion   byte = 0x01
	socks5AuthSuccess   byte = 0x00
	socks5AuthFailure   byte = 0x01
)

// Authenticate implements credential.Authenticator. / Authenticate 实现
// credential.Authenticator。
func (a *SOCKS5Authenticator) Authenticate(ctx context.Context, host string, port int, creds []credential.Cred, timeout time.Duration) (*credential.Hit, error) {
	if len(creds) == 0 {
		return nil, nil
	}
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	for i, c := range creds {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if c.Method != "" && c.Method != credential.AuthPassword {
			continue
		}
		ok, err := a.attempt(ctx, addr, c.User, c.Pass, timeout)
		if err != nil {
			return nil, err
		}
		if ok {
			return &credential.Hit{
				Cred:     c,
				Attempts: i + 1,
				Time:     time.Now(),
			}, nil
		}
	}
	return nil, nil
}

// attempt runs one SOCKS5 user/pass auth round. / attempt 跑一次
// SOCKS5 user/pass auth。
func (a *SOCKS5Authenticator) attempt(ctx context.Context, addr, user, pass string, timeout time.Duration) (bool, error) {
	d := net.Dialer{Timeout: timeout}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return false, err
	}
	defer conn.Close()
	// Greeting: VER 5, NMETHODS 1, METHODS = [0x02] (user/pass).
	// / Greeting：VER 5，NMETHODS 1，METHODS = [0x02]（user/pass）。
	if _, err := conn.Write([]byte{socks5Version, 0x01, socks5MethodUserPass}); err != nil {
		return false, err
	}
	// Server selection: VER, METHOD. / 服务器选 method：VER, METHOD。
	sel := make([]byte, 2)
	if _, err := readFull(conn, sel); err != nil {
		return false, err
	}
	if sel[0] != socks5Version {
		return false, nil
	}
	if sel[1] == socks5MethodNoAccept {
		return false, nil
	}
	// User/pass auth subnegotiation (RFC 1929). / User/pass auth
	// 子协商（RFC 1929）。
	// VER (1) + ULEN (1) + UNAME + PLEN (1) + PASSWD.
	// / VER (1) + ULEN (1) + UNAME + PLEN (1) + PASSWD。
	ub := []byte{socks5AuthVersion, byte(len(user))}
	ub = append(ub, []byte(user)...)
	ub = append(ub, byte(len(pass)))
	ub = append(ub, []byte(pass)...)
	if _, err := conn.Write(ub); err != nil {
		return false, err
	}
	// Server reply: VER, STATUS. / 服务器回：VER, STATUS。
	reply := make([]byte, 2)
	if _, err := readFull(conn, reply); err != nil {
		return false, err
	}
	return reply[0] == socks5AuthVersion && reply[1] == socks5AuthSuccess, nil
}

func readFull(c net.Conn, buf []byte) (int, error) {
	total := 0
	for total < len(buf) {
		n, err := c.Read(buf[total:])
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

// init registers the SOCKS5 authenticator. / init 注册 SOCKS5 认证器。
func init() {
	credential.Register(NewSOCKS5Authenticator())
}
