// Copyright (c) 2026 LCUstinian
// SPDX-License-Identifier: MIT
//
// Rsync Identify plugin. Reads "@RSYNCD: <ver>\n" greeting and
// reports the version. Credential() is routed through core/cred/
// protocols/rsync.go (RsyncAuthenticator, MD5 challenge-response).
//
// Rsync 识别插件。读 "@RSYNCD: <ver>\n" greeting 并报告版本。
// Credential() 走 core/cred/protocols/rsync.go（RsyncAuthenticator，
// MD5 challenge-response）。
package rsync

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

// Plugin identifies rsync daemons. / Plugin 识别 rsync 守护进程。
type Plugin struct{}

// New returns a new rsync plugin. / New 返回一个新的 rsync 插件。
func New() *Plugin { return &Plugin{} }

func init() { plugins.Register(New()) }

// Name implements plugins.Plugin. / Name 实现 plugins.Plugin。
func (p *Plugin) Name() string { return "rsync" }

// Ports returns default rsync ports. / Ports 返回默认 rsync 端口。
func (p *Plugin) Ports() []int { return []int{873, 8873} }

// Modes returns Identify + Credential. / Modes 返回 Identify + Credential。
func (p *Plugin) Modes() plugins.Mode { return plugins.ModeIdentify | plugins.ModeCredential }

// Credential is a no-op stub. / Credential 空 stub。
func (p *Plugin) Credential(ctx context.Context, host string, port int, creds []types.Cred) *types.Result {
	return nil
}

// Identify reads the rsync greeting. / Identify 读 rsync greeting。
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
	if !strings.HasPrefix(line, "@RSYNCD:") {
		return nil
	}
	return &types.Result{
		Host: host, Port: port, Service: "rsync",
		Banner: "Rsync " + strings.TrimPrefix(line, "@RSYNCD: "),
		Time:   time.Now(),
	}
}
