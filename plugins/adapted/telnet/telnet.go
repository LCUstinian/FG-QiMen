// Copyright (c) 2026 LCUstinian
// SPDX-License-Identifier: MIT
//
// Telnet Identify plugin. Opens a raw TCP connection, reads the
// initial banner (handling IAC bytes), and reports whether the
// server looks like a telnetd (login prompt, shell prompt, or
// printable banner).
//
// Credential() is routed through core/cred/protocols/telnet.go
// (TelnetAuthenticator, hand-rolled IAC + prompt flow).
//
// Telnet 识别插件。开裸 TCP 连接，读初始 banner（处理 IAC 字节），
// 报告服务像不像 telnetd（登录提示符 / shell 提示符 / 可打印 banner）。
//
// Credential() 走 core/cred/protocols/telnet.go（TelnetAuthenticator，
// 手写 IAC + 提示符流程）。
package telnet

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/LCUstinian/FG-QiMen/common"
	"github.com/LCUstinian/FG-QiMen/plugins"
)

// Plugin identifies telnetd servers. / Plugin 识别 telnetd 服务。
type Plugin struct{}

// New returns a new telnet plugin. / New 返回一个新的 telnet 插件。
func New() *Plugin { return &Plugin{} }

func init() { plugins.Register(New()) }

// Name implements plugins.Plugin. / Name 实现 plugins.Plugin。
func (p *Plugin) Name() string { return "telnet" }

// Ports returns default Telnet ports. / Ports 返回默认 Telnet 端口。
func (p *Plugin) Ports() []int { return []int{23, 2323} }

// Modes returns Identify + Credential. / Modes 返回 Identify + Credential。
func (p *Plugin) Modes() plugins.Mode { return plugins.ModeIdentify | plugins.ModeCredential }

// Credential is a no-op stub. / Credential 空 stub。
func (p *Plugin) Credential(ctx context.Context, host string, port int, creds []common.Cred) *common.Result {
	return nil
}

// Identify opens a TCP connection, reads the initial banner, and
// reports whether the server looks like a telnetd.
//
// Identify 开 TCP 连接，读初始 banner，报告服务像不像 telnetd。
func (p *Plugin) Identify(ctx context.Context, host string, port int) *common.Result {
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	d := net.Dialer{Timeout: 3 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	buf := make([]byte, 512)
	n, _ := conn.Read(buf)
	if n == 0 {
		return nil
	}
	text := stripIAC(buf[:n])
	lower := strings.ToLower(text)
	if strings.Contains(lower, "login:") ||
		strings.Contains(lower, "username:") ||
		strings.Contains(lower, "password:") ||
		strings.Contains(lower, "$ ") ||
		strings.Contains(lower, "# ") ||
		// Many telnetd's start with a printable welcome banner
		// (kernel version, OS release, etc.) before prompting. / 很多
		// telnetd 在提示符前会先发可打印 welcome banner（内核版本、
		// OS 版本等）。
		isPrintableBanner(text) {
		banner := text
		if len(banner) > 100 {
			banner = banner[:100] + "..."
		}
		return &common.Result{
			Host: host, Port: port, Service: "telnet",
			Banner: "Telnet: " + banner, Time: time.Now(),
		}
	}
	return nil
}

// stripIAC removes Telnet IAC (0xFF) commands from a byte slice and
// returns the cleaned text. / stripIAC 从字节切片剥掉 Telnet IAC (0xFF)
// 命令，返清洗后的文本。
func stripIAC(b []byte) string {
	var out strings.Builder
	for i := 0; i < len(b); i++ {
		c := b[i]
		if c == 0xFF {
			i += 2
			continue
		}
		if c >= 32 && c <= 126 || c == '\r' || c == '\n' || c == '\t' {
			out.WriteByte(c)
		}
	}
	return strings.TrimSpace(out.String())
}

// isPrintableBanner returns true if the cleaned text looks like a
// readable welcome banner (>= 5 printable characters).
//
// isPrintableBanner 当清洗后文本看起来像可读 welcome banner（>= 5
// 个可打印字符）时返 true。
func isPrintableBanner(s string) bool {
	count := 0
	for _, c := range s {
		if c >= 32 && c <= 126 {
			count++
		}
	}
	return count >= 5
}
