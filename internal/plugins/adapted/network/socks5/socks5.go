// Copyright (c) 2026 LCUstinian
// SPDX-License-Identifier: MIT
//
// SOCKS5 Identify plugin. Sends the SOCKS5 greeting (VER 5, 1 method
// = NO_AUTH). If the server replies with VER 5 + METHOD 0x00 (no
// auth) we know it's SOCKS5. / SOCKS5 识别插件。发 SOCKS5 greeting
// (VER 5, 1 method = NO_AUTH)。服务器回 VER 5 + METHOD 0x00 即
// SOCKS5。
package socks5

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/plugins"
	"github.com/LCUstinian/FG-QiMen/internal/types"
)

// Plugin identifies SOCKS5 proxies. / Plugin 识别 SOCKS5 代理。
type Plugin struct{}

// New returns a new socks5 plugin. / New 返回一个新的 socks5 插件。
func New() *Plugin { return &Plugin{} }

func init() { plugins.Register(New()) }

// Name implements plugins.Plugin. / Name 实现 plugins.Plugin。
func (p *Plugin) Name() string { return "socks5" }

// Ports returns default SOCKS5 port. / Ports 返回默认 SOCKS5 端口。
func (p *Plugin) Ports() []int { return []int{1080} }

// Modes returns Identify + Credential. / Modes 返回 Identify + Credential。
func (p *Plugin) Modes() plugins.Mode { return plugins.ModeIdentify | plugins.ModeCredential }

// Credential is a no-op stub. / Credential 空 stub。
func (p *Plugin) Credential(ctx context.Context, host string, port int, creds []types.Cred) *types.Result {
	return nil
}

// Identify sends the SOCKS5 greeting and checks for a 5.0 response.
// / Identify 发 SOCKS5 greeting 并检查 5.0 响应。
func (p *Plugin) Identify(ctx context.Context, host string, port int) *types.Result {
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	d := net.Dialer{Timeout: 3 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	// Greeting: VER 5, NMETHODS 1, METHODS = [0x00] (no auth).
	// / Greeting：VER 5，NMETHODS 1，METHODS = [0x00]（no auth）。
	if _, err := conn.Write([]byte{0x05, 0x01, 0x00}); err != nil {
		return nil
	}
	sel := make([]byte, 2)
	if _, err := readFullS5(conn, sel); err != nil {
		return nil
	}
	if sel[0] != 0x05 {
		return nil
	}
	return &types.Result{
		Host: host, Port: port, Service: "socks5",
		Banner: "SOCKS5", Time: time.Now(),
	}
}

func readFullS5(c net.Conn, buf []byte) (int, error) {
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
