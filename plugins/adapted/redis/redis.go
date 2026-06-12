// Copyright (c) 2026 LCUstinian
// SPDX-License-Identifier: MIT
//
// Redis Identify plugin. HARD rule: no unauthorized-access probes,
// no write paths (no CONFIG SET / SET / SLAVEOF / MODULE LOAD), no
// post-auth API calls — only the PING / PONG handshake is used to
// fingerprint the service.
//
// Redis 识别插件。硬性原则：不探测未授权访问、不写命令、不调任何
// 认证后 API——只靠 PING / PONG 握手识别服务。
package redis

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/LCUstinian/FG-QiMen/common"
	"github.com/LCUstinian/FG-QiMen/plugins"
)

// Plugin identifies Redis servers via PING / PONG. / Plugin 通过 PING / PONG 识别 Redis 服务。
type Plugin struct{}

// New returns a new redis plugin. / New 返回一个新的 redis 插件。
func New() *Plugin { return &Plugin{} }

func init() { plugins.Register(New()) }

// Name implements plugins.Plugin. / Name 实现 plugins.Plugin。
func (p *Plugin) Name() string { return "redis" }

// Ports returns default Redis ports. / Ports 返回默认 Redis 端口。
func (p *Plugin) Ports() []int { return []int{6379, 6380} }

// Modes returns Identify only. / Modes 仅返回 Identify。
func (p *Plugin) Modes() plugins.Mode { return plugins.ModeIdentify }

// Credential is a no-op stub; Redis credential testing lives in
// core/cred/protocols in v0.2+. / Credential 空 stub；Redis 凭据
// 测试在 v0.2+ 的 core/cred/protocols 里。
func (p *Plugin) Credential(ctx context.Context, host string, port int, creds []common.Cred) *common.Result {
	return nil
}

// Identify sends PING and parses the response. / Identify 发 PING 并解析响应。
func (p *Plugin) Identify(ctx context.Context, host string, port int) *common.Result {
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	d := net.Dialer{Timeout: 3 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err := conn.Write([]byte("PING\r\n")); err != nil {
		return nil
	}
	buf := make([]byte, 256)
	n, err := conn.Read(buf)
	if err != nil || n == 0 {
		return nil
	}
	resp := strings.TrimSpace(string(buf[:n]))
	if !strings.HasPrefix(resp, "+PONG") && !strings.HasPrefix(resp, "-NOAUTH") {
		return nil
	}
	return &common.Result{
		Host: host, Port: port, Service: "redis",
		Banner: "redis: " + resp, Time: time.Now(),
	}
}
