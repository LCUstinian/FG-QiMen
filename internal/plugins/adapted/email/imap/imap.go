// Copyright (c) 2026 LCUstinian
// SPDX-License-Identifier: MIT
//
// IMAP Identify plugin. Reads the server's untagged greeting
// (e.g. "* OK [CAPABILITY IMAP4rev1] ready") and reports the
// banner. Credential() is routed through core/cred/protocols/imap.go
// (IMAPAuthenticator, RFC 3501 LOGIN command).
//
// IMAP 识别插件。读服务器未打 tag 的 greeting（如 "* OK [CAPABILITY
// IMAP4rev1] ready"）并报告 banner。Credential() 走 core/cred/
// protocols/imap.go（IMAPAuthenticator，RFC 3501 LOGIN 命令）。
package imap

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

// Plugin identifies IMAP servers. / Plugin 识别 IMAP 服务。
type Plugin struct{}

// New returns a new imap plugin. / New 返回一个新的 imap 插件。
func New() *Plugin { return &Plugin{} }

func init() { plugins.Register(New()) }

// Name implements plugins.Plugin. / Name 实现 plugins.Plugin。
func (p *Plugin) Name() string { return "imap" }

// Ports returns default IMAP ports. / Ports 返回默认 IMAP 端口。
func (p *Plugin) Ports() []int { return []int{143, 993} }

// Modes returns Identify + Credential. / Modes 返回 Identify + Credential。
func (p *Plugin) Modes() plugins.Mode { return plugins.ModeIdentify | plugins.ModeCredential }

// Credential is a no-op stub. / Credential 空 stub。
func (p *Plugin) Credential(ctx context.Context, host string, port int, creds []common.Cred) *common.Result {
	return nil
}

// Identify reads the IMAP greeting. / Identify 读 IMAP greeting。
func (p *Plugin) Identify(ctx context.Context, host string, port int) *common.Result {
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
	if !strings.HasPrefix(line, "* OK") {
		return nil
	}
	return &common.Result{
		Host: host, Port: port, Service: "imap",
		Banner: "IMAP: " + line, Time: time.Now(),
	}
}
