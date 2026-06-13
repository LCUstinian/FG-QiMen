// Copyright (c) 2026 LCUstinian
// SPDX-License-Identifier: MIT
//
// VNC Identify plugin. Reads the 12-byte RFB ProtocolVersion banner
// (RFB xxx.yyy\n) and reports the version. Credential() is routed
// through core/cred/protocols/vnc.go (VNCAuthenticator via go-vnc).
//
// VNC 识别插件。读 12 字节 RFB ProtocolVersion banner（RFB xxx.yyy\n），
// 报告版本。Credential() 走 core/cred/protocols/vnc.go（VNCAuthenticator
// via go-vnc）。
package vnc

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/common"
	"github.com/LCUstinian/FG-QiMen/internal/plugins"
)

// Plugin identifies VNC servers. / Plugin 识别 VNC 服务。
type Plugin struct{}

// New returns a new vnc plugin. / New 返回一个新的 vnc 插件。
func New() *Plugin { return &Plugin{} }

func init() { plugins.Register(New()) }

// Name implements plugins.Plugin. / Name 实现 plugins.Plugin。
func (p *Plugin) Name() string { return "vnc" }

// Ports returns default VNC display ports. / Ports 返回默认 VNC 显示端口。
func (p *Plugin) Ports() []int { return []int{5900, 5901, 5902, 5903, 5904, 5905} }

// Modes returns Identify + Credential. / Modes 返回 Identify + Credential。
//
// Credential() lives in core/cred/protocols/vnc.go. The plugin's
// Credential method is a no-op stub; pipeline routes via cred.Scheduler.
// / Credential() 在 core/cred/protocols/vnc.go。plugin 的 Credential
// 方法是空 stub；管线走 cred.Scheduler。
func (p *Plugin) Modes() plugins.Mode { return plugins.ModeIdentify | plugins.ModeCredential }

// Credential is a no-op stub. / Credential 空 stub。
func (p *Plugin) Credential(ctx context.Context, host string, port int, creds []common.Cred) *common.Result {
	return nil
}

// Identify reads the RFB ProtocolVersion banner (12 bytes:
// "RFB xxx.yyy\n"). / Identify 读 RFB ProtocolVersion banner（12 字节：
// "RFB xxx.yyy\n"）。
func (p *Plugin) Identify(ctx context.Context, host string, port int) *common.Result {
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	d := net.Dialer{Timeout: 3 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	buf := make([]byte, 12)
	n, err := conn.Read(buf)
	if err != nil || n < 12 {
		return nil
	}
	if string(buf[0:4]) != "RFB " {
		return nil
	}
	return &common.Result{
		Host: host, Port: port, Service: "vnc",
		Banner: fmt.Sprintf("VNC %s", string(buf[4:11])),
		Time:   time.Now(),
	}
}
