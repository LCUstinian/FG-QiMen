// Copyright (c) 2026 LCUstinian
// SPDX-License-Identifier: MIT
//
// memcached Identify plugin. The text-protocol "version\r\n" probe
// is enough for identify — no SET / GET / FLUSH / session state.
//
// memcached 识别插件。用文本协议 "version\r\n" 探针就够识别——不
// 调 SET / GET / FLUSH，不维护 session。
package memcached

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/common"
	"github.com/LCUstinian/FG-QiMen/internal/plugins"
)

// Plugin identifies memcached via the text protocol. / Plugin 通过文本协议识别 memcached。
type Plugin struct{}

// New returns a new memcached plugin. / New 返回一个新的 memcached 插件。
func New() *Plugin { return &Plugin{} }

func init() { plugins.Register(New()) }

// Name implements plugins.Plugin. / Name 实现 plugins.Plugin。
func (p *Plugin) Name() string { return "memcached" }

// Ports returns default memcached ports. / Ports 返回默认 memcached 端口。
func (p *Plugin) Ports() []int { return []int{11211, 11212} }

// Modes returns Identify only. / Modes 仅返回 Identify。
func (p *Plugin) Modes() plugins.Mode { return plugins.ModeIdentify }

// Credential is a no-op stub. / Credential 空 stub。
func (p *Plugin) Credential(ctx context.Context, host string, port int, creds []common.Cred) *common.Result {
	return nil
}

// Identify sends "version\r\n" and parses the VERSION response. /
// Identify 发 "version\r\n" 并解析 VERSION 响应。
func (p *Plugin) Identify(ctx context.Context, host string, port int) *common.Result {
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	d := net.Dialer{Timeout: 3 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err := conn.Write([]byte("version\r\n")); err != nil {
		return nil
	}
	br := bufio.NewReader(conn)
	line, err := br.ReadString('\n')
	if err != nil {
		return nil
	}
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "VERSION ") {
		return nil
	}
	return &common.Result{
		Host: host, Port: port, Service: "memcached",
		Banner: "memcached " + strings.TrimPrefix(line, "VERSION "),
		Time:   time.Now(),
	}
}
