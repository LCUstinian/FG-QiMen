// Package protocols: VNC authenticator.
//
// Uses github.com/mitchellh/go-vnc. The library handles the full
// RFB (Remote Framebuffer) protocol handshake, including the
// version exchange, security type negotiation, and DES password
// challenge. We do NOT send any framebuffer update request, do NOT
// open a display window, do NOT take a screenshot.
//
// HARD RULE: on a hit we return. No post-auth action.
//
// 包 protocols：VNC 认证器。
// 用 github.com/mitchellh/go-vnc。库处理完整 RFB（Remote Framebuffer）
// 协议握手——版本交换、安全类型协商、DES 密码 challenge。我们不发任何
// framebuffer 更新请求、不开显示窗口、不截屏。
//
// 硬性原则：命中即返回，不做任何认证后动作。
package remote

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"time"

	vnc "github.com/mitchellh/go-vnc"

	"github.com/LCUstinian/FG-QiMen/internal/core/credential"
)

// VNCAuthenticator authenticates against VNC servers via go-vnc.
//
// DefaultPorts returns 5900-5905 (the standard VNC display ports;
// :N means display N, offset from 5900). Same as fscan.
//
// VNCAuthenticator 通过 go-vnc 对 VNC 服务认证。
//
// DefaultPorts 返 5900-5905（标准 VNC 显示端口；:N 即 display N，
// 相对 5900 偏移）。与 fscan 一致。
type VNCAuthenticator struct{}

// NewVNCAuthenticator returns a default VNC authenticator.
// NewVNCAuthenticator 返回默认配置的 VNC 认证器。
func NewVNCAuthenticator() *VNCAuthenticator { return &VNCAuthenticator{} }

// Name implements credential.Authenticator. / Name 实现 credential.Authenticator。
func (a *VNCAuthenticator) Name() string { return "vnc" }

// DefaultPorts implements credential.Authenticator. / DefaultPorts 实现 credential.Authenticator。
func (a *VNCAuthenticator) DefaultPorts() []int {
	return []int{5900, 5901, 5902, 5903, 5904, 5905}
}

// Authenticate implements credential.Authenticator. Tries each cred;
// VNC auth is password-only (no user concept in classic RFB).
//
// Authenticate 实现 credential.Authenticator。按顺序尝试每个 cred；VNC 认证
// 只用密码（经典 RFB 没有 user 概念）。
//
// Strategy: open a TCP conn, run go-vnc with PasswordAuth. The library
// does the full RFB handshake including the DES challenge. On success
// we close the conn immediately and return Hit.
//
// 策略：开 TCP 连接，跑 go-vnc 的 PasswordAuth。库跑完整 RFB 握手含
// DES challenge。命中立即关连接并返 Hit。
func (a *VNCAuthenticator) Authenticate(ctx context.Context, host string, port int, creds []credential.Cred, timeout time.Duration) (*credential.Hit, error) {
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
		d := net.Dialer{Timeout: timeout}
		conn, err := d.DialContext(ctx, "tcp", addr)
		if err != nil {
			return nil, err
		}
		cfg := &vnc.ClientConfig{
			Auth: []vnc.ClientAuth{
				&vnc.PasswordAuth{Password: c.Pass},
			},
		}
		client, err := vnc.Client(conn, cfg)
		if err != nil {
			_ = conn.Close()
			// RFB errors during handshake look like "auth failed" /
			// "EOF" / "wrong password". All are misses — try next.
			// / RFB 握手错形如"auth failed" / "EOF" / "wrong password"。
			// 都是 miss——试下一个。
			_ = fmt.Sprintf("vnc: %v", err)
			continue
		}
		// Hit: close immediately, no framebuffer request. / 命中：立即
		// 关连接，不发 framebuffer 请求。
		_ = client.Close()
		_ = conn.Close()
		return &credential.Hit{
			Cred:     c,
			Attempts: i + 1,
			Time:     time.Now(),
		}, nil
	}
	return nil, nil
}

// init registers the VNC authenticator. / init 注册 VNC 认证器。
func init() {
	credential.Register(NewVNCAuthenticator())
}
