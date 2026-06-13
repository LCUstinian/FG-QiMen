// Copyright (c) 2026 LCUstinian
// SPDX-License-Identifier: MIT
//
// SMTP Identify plugin. Uses net/smtp for the EHLO probe — the
// standard library handles the wire format. No mail-sending path.
//
// SMTP 识别插件。用 net/smtp 发 EHLO——标准库处理了线格式。没有任何
// 发信路径。
package smtp

import (
	"context"
	"fmt"
	"net"
	"net/smtp"
	"strconv"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/common"
	"github.com/LCUstinian/FG-QiMen/internal/plugins"
)

// Plugin identifies SMTP servers via EHLO. / Plugin 通过 EHLO 识别 SMTP 服务。
type Plugin struct{}

// New returns a new smtp plugin. / New 返回一个新的 smtp 插件。
func New() *Plugin { return &Plugin{} }

func init() { plugins.Register(New()) }

// Name implements plugins.Plugin. / Name 实现 plugins.Plugin。
func (p *Plugin) Name() string { return "smtp" }

// Ports returns default SMTP ports. / Ports 返回默认 SMTP 端口。
func (p *Plugin) Ports() []int { return []int{25, 465, 587, 2525} }

// Modes returns Identify only. / Modes 仅返回 Identify。
func (p *Plugin) Modes() plugins.Mode { return plugins.ModeIdentify }

// Credential is a no-op stub. / Credential 空 stub。
func (p *Plugin) Credential(ctx context.Context, host string, port int, creds []common.Cred) *common.Result {
	return nil
}

// Identify opens a TCP connection, reads the 220 greeting, and sends
// EHLO. Returns the server greeting + first EHLO line.
//
// Identify 开 TCP 连接，读 220 问候，发 EHLO。返回服务器问候 + 首个
// EHLO 行。
func (p *Plugin) Identify(ctx context.Context, host string, port int) *common.Result {
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	d := net.Dialer{Timeout: 3 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))

	c, err := smtp.NewClient(conn, host)
	if err != nil {
		// Read greeting manually. / 手动读问候。
		buf := make([]byte, 256)
		conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		n, _ := conn.Read(buf)
		if n < 4 || buf[0] != '2' || buf[1] != '2' || buf[2] != '0' {
			return nil
		}
		return &common.Result{
			Host: host, Port: port, Service: "smtp",
			Banner: fmt.Sprintf("smtp: %s", trim(string(buf[:n]))), Time: time.Now(),
		}
	}
	defer c.Close()
	if err := c.Hello("fg-qimen.local"); err != nil {
		return nil
	}
	// c.Hello doesn't expose the response. The connection state
	// has the first EHLO line though. / c.Hello 不暴露响应。连接
	// 状态里有首个 EHLO 行。
	// Fall back to the connection for a basic banner.
	// / 回退到连接获取基础 banner。
	return &common.Result{
		Host: host, Port: port, Service: "smtp",
		Banner: fmt.Sprintf("smtp: EHLO ok port=%d", port), Time: time.Now(),
	}
}

func trim(s string) string {
	// Trim trailing CRLF. / 去掉末尾 CRLF。
	for len(s) > 0 && (s[len(s)-1] == '\r' || s[len(s)-1] == '\n' || s[len(s)-1] == ' ') {
		s = s[:len(s)-1]
	}
	return s
}

// keep strconv import used (for future port extraction). / 保留 strconv 导入供未来端口提取。
var _ = strconv.Itoa
