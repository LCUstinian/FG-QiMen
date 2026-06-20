// Copyright (c) 2026 LCUstinian
// SPDX-License-Identifier: MIT
//
// SSH Identify plugin. Reads the SSH-2.0 server banner. We do NOT
// authenticate; the credential auth lives in
// internal/core/credential/auth/remote/ssh.go.
//
// SSH 识别插件。读 SSH-2.0 server banner。我们不做认证；凭据认
// 证在 internal/core/credential/auth/remote/ssh.go。
//
// Wire format (RFC 4253 §4.2):
//   - server → client: SSH-protoversion-softwareversion SP comments CR LF
//   - client → server: same format
//   - then key exchange init (8+ bytes of packet)
//
// / 协议格式 (RFC 4253 §4.2)：server 立刻发 banner，client 回
// 自己的 banner，然后开始 key exchange。
package ssh

import (
	"bufio"
	"context"
	"net"
	"strings"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/plugins"
	"github.com/LCUstinian/FG-QiMen/internal/types"
)

// Plugin identifies SSH servers. / Plugin 识别 SSH 服务。
type Plugin struct{}

// New returns a new ssh plugin. / New 返回一个新的 ssh 插件。
func New() *Plugin { return &Plugin{} }

func init() { plugins.Register(New()) }

// Name implements plugins.Plugin. / Name 实现 plugins.Plugin。
func (p *Plugin) Name() string { return "ssh" }

// Ports returns default SSH port. / Ports 返回默认 SSH 端口。
func (p *Plugin) Ports() []int { return []int{22, 2222} }

// Modes returns Identify only. / Modes 仅返回 Identify。
func (p *Plugin) Modes() plugins.Mode { return plugins.ModeIdentify }

// Credential is a no-op stub; SSH credential testing lives in
// core/cred/auth/remote in v0.2+. / Credential 空 stub；SSH 凭
// 据测试在 v0.2+ 的 core/cred/auth/remote。
func (p *Plugin) Credential(ctx context.Context, host string, port int, creds []types.Cred) *types.Result {
	return nil
}

// Identify opens the connection, reads the server banner, and
// classifies the SSH implementation. / Identify 打开连接、读
// server banner、分类 SSH 实现。
func (p *Plugin) Identify(ctx context.Context, host string, port int) *types.Result {
	return plugins.RawTCPIdentify(ctx, host, port, func(conn net.Conn) *types.Result {
		br := bufio.NewReader(conn)
		line, err := br.ReadString('\n')
		if err != nil {
			return nil
		}
		line = strings.TrimRight(line, "\r\n")
		// Banner format: SSH-protoversion-softwareversion [comments]
		// Example: SSH-2.0-OpenSSH_8.9p1 Ubuntu-3ubuntu0.1
		// / Banner 格式：SSH-协议版本-软件版本 [注释]
		if !strings.HasPrefix(line, "SSH-") {
			return nil
		}
		// Send a minimal client banner so the server doesn't
		// disconnect before we can read any further data. The
		// server banner is the only thing we need. / 发一个最
		// 小 client banner 让 server 不要在我们读完前断开。
		// server banner 是我们唯一需要的东西。
		// (Most servers don't disconnect on missing client
		// banner — they wait for the timeout — but sending
		// politely is friendly.) / 多数 server 不会因 client
		// banner 缺失断开（等超时），但礼貌地发一条更友好。
		_, _ = conn.Write([]byte("SSH-2.0-FG-QiMen_0.3.1\r\n"))

		// Parse "SSH-2.0-softwareversion [comments]". / 解析
		// "SSH-2.0-软件版本 [注释]"。
		rest := strings.TrimPrefix(line, "SSH-")
		// "2.0-OpenSSH_8.9p1 ..." → ["2.0", "OpenSSH_8.9p1 ..."]
		parts := strings.SplitN(rest, "-", 2)
		if len(parts) < 2 {
			return nil
		}
		// protocol + software. / 协议 + 软件。
		proto := parts[0]
		software := strings.TrimSpace(parts[1])
		cls := classify(software)
		return &types.Result{
			Host:    host,
			Port:    port,
			Service: "ssh",
			Banner:  fmtSSH(cls, proto, software),
			Time:    time.Now(),
		}
	})
}

// classify returns a short tag for the SSH implementation. /
// classify 返回 SSH 实现的短标签。
func classify(software string) string {
	l := strings.ToLower(software)
	switch {
	case strings.HasPrefix(l, "openssh"):
		return "OpenSSH"
	case strings.HasPrefix(l, "dropbear"):
		return "Dropbear"
	case strings.Contains(l, "libssh"):
		return "libssh"
	case strings.Contains(l, "paramiko"):
		return "paramiko"
	case strings.Contains(l, "putty"):
		return "PuTTY"
	case strings.Contains(l, "winscp"):
		return "WinSCP"
	case strings.Contains(l, "cisco"):
		return "Cisco-IOS"
	case strings.Contains(l, "ruijie"):
		return "Ruijie"
	case strings.Contains(l, "h3c"):
		return "H3C"
	case strings.Contains(l, "huawei"):
		return "Huawei"
	case strings.Contains(l, "dropbear"):
		return "Dropbear"
	default:
		return "SSH"
	}
}

// fmtSSH formats the Identify banner. / fmtSSH 格式化 Identify banner。
func fmtSSH(cls, proto, software string) string {
	if cls == "SSH" {
		return "SSH-" + proto + " " + truncate(software, 80)
	}
	return cls + " (SSH-" + proto + ", " + truncate(software, 80) + ")"
}

// truncate limits banner display length. / truncate 限制 banner 显
// 示长度。
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
