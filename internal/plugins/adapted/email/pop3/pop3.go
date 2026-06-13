// Copyright (c) 2026 LCUstinian
// SPDX-License-Identifier: MIT
//
// POP3 Identify plugin. Reads the "+OK" greeting and reports the
// banner. Credential() is routed through core/cred/protocols/pop3.go.
//
// POP3 识别插件。读 "+OK" greeting 并报告 banner。Credential() 走
// core/cred/protocols/pop3.go。
package pop3

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/plugins"
	"github.com/LCUstinian/FG-QiMen/internal/types"
)

// Plugin identifies POP3 servers. / Plugin 识别 POP3 服务。
type Plugin struct{}

// New returns a new pop3 plugin. / New 返回一个新的 pop3 插件。
func New() *Plugin { return &Plugin{} }

func init() { plugins.Register(New()) }

// Name implements plugins.Plugin. / Name 实现 plugins.Plugin。
func (p *Plugin) Name() string { return "pop3" }

// Ports returns default POP3 ports. / Ports 返回默认 POP3 端口。
func (p *Plugin) Ports() []int { return []int{110, 995} }

// Modes returns Identify + Credential. / Modes 返回 Identify + Credential。
func (p *Plugin) Modes() plugins.Mode { return plugins.ModeIdentify | plugins.ModeCredential }

// Credential is a no-op stub. / Credential 空 stub。
func (p *Plugin) Credential(ctx context.Context, host string, port int, creds []types.Cred) *types.Result {
	return nil
}

// Identify reads the POP3 greeting. / Identify 读 POP3 greeting。
func (p *Plugin) Identify(ctx context.Context, host string, port int) *types.Result {
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	d := net.Dialer{Timeout: 3 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	br := bufio.NewReader(conn)
	line, err := br.ReadString('\n')
	if err != nil {
		return nil
	}
	line = strings.TrimRight(line, "\r\n")
	if !strings.HasPrefix(line, "+OK") {
		return nil
	}
	return &types.Result{
		Host: host, Port: port, Service: "pop3",
		Banner: "POP3: " + line, Time: time.Now(),
	}
}
