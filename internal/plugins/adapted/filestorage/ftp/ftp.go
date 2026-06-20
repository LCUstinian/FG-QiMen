// Copyright (c) 2026 LCUstinian
// SPDX-License-Identifier: MIT
//
// FTP Identify plugin. Reads the 220 server greeting and parses
// the FTP server banner. We do NOT authenticate; the credential
// auth lives in internal/core/credential/auth/remote/ftp.go.
//
// FTP 识别插件。读 220 server greeting 并解析 FTP server banner。
// 我们不做认证；凭据认证在 internal/core/credential/auth/remote/ftp.go。
//
// Wire format (RFC 959):
//   - client → server: nothing (server speaks first on connect)
//   - server → client: 220 <hostname> <server-ident> <system-type>\r\n
//
// / 协议格式 (RFC 959)：server 在 connect 后立刻发 220 greeting。
package ftp

import (
	"bufio"
	"context"
	"net"
	"strings"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/plugins"
	"github.com/LCUstinian/FG-QiMen/internal/types"
)

// Plugin identifies FTP servers. / Plugin 识别 FTP 服务。
type Plugin struct{}

// New returns a new ftp plugin. / New 返回一个新的 ftp 插件。
func New() *Plugin { return &Plugin{} }

func init() { plugins.Register(New()) }

// Name implements plugins.Plugin. / Name 实现 plugins.Plugin。
func (p *Plugin) Name() string { return "ftp" }

// Ports returns default FTP port. / Ports 返回默认 FTP 端口。
func (p *Plugin) Ports() []int { return []int{21, 2121} }

// Modes returns Identify only. / Modes 仅返回 Identify。
func (p *Plugin) Modes() plugins.Mode { return plugins.ModeIdentify }

// Credential is a no-op stub; FTP credential testing lives in
// core/cred/auth/remote in v0.2+. / Credential 空 stub；FTP 凭
// 据测试在 v0.2+ 的 core/cred/auth/remote。
func (p *Plugin) Credential(ctx context.Context, host string, port int, creds []types.Cred) *types.Result {
	return nil
}

// Identify opens the connection, reads the 220 greeting, and
// classifies the FTP server (vsftpd / ProFTPD / Pure-FTPd / etc).
// / Identify 打开连接、读 220 greeting、分类 FTP server（vsftpd
// / ProFTPD / Pure-FTPd 等）。
func (p *Plugin) Identify(ctx context.Context, host string, port int) *types.Result {
	return plugins.RawTCPIdentify(ctx, host, port, func(conn net.Conn) *types.Result {
		br := bufio.NewReader(conn)
		line, err := br.ReadString('\n')
		if err != nil {
			return nil
		}
		line = strings.TrimRight(line, "\r\n")
		// 220 <hostname-or-banner> / 220 <hostname-or-banner>
		if !strings.HasPrefix(line, "220 ") {
			// Some servers use multi-line greetinings. Subsequent
			// lines must still be 220. / 部分 server 用多行
			// greeting。后续行仍须 220。
			if !strings.HasPrefix(line, "220-") {
				return nil
			}
			// Read until a line starting with "220 " (the final
			// line of the multi-line greeting). / 读到以 "220 "
			// 开头的行（多行 greeting 的最后一行）。
			for {
				more, err := br.ReadString('\n')
				if err != nil {
					return nil
				}
				more = strings.TrimRight(more, "\r\n")
				if strings.HasPrefix(more, "220 ") {
					line = more
					break
				}
			}
		}
		// Banner is everything after "220 " (or "220-...220 "). / Banner 是
		// "220 " 之后的所有内容（或多行最后一行）。
		banner := strings.TrimPrefix(line, "220 ")
		banner = strings.TrimPrefix(banner, "220-")
		banner = strings.TrimSpace(banner)
		// Classify common implementations. / 分类常见实现。
		cls := classify(banner)
		return &types.Result{
			Host:    host,
			Port:    port,
			Service: "ftp",
			Banner:  "FTP " + cls + " (" + truncate(banner, 80) + ")",
			Time:    time.Now(),
		}
	})
}

// classify returns a short tag for the FTP server class. /
// classify 返回 FTP server 类型的短标签。
func classify(banner string) string {
	l := strings.ToLower(banner)
	switch {
	case strings.Contains(l, "vsftpd"):
		return "vsftpd"
	case strings.Contains(l, "proftpd"):
		return "ProFTPD"
	case strings.Contains(l, "pure-ftpd"):
		return "Pure-FTPd"
	case strings.Contains(l, "filezilla"):
		return "FileZilla"
	case strings.Contains(l, "microsoft"):
		return "IIS-FTP"
	case strings.Contains(l, "wu-ftp"):
		return "wu-ftpd"
	case strings.Contains(l, "serv-u"):
		return "Serv-U"
	default:
		return "generic"
	}
}

// truncate limits banner display length. / truncate 限制 banner 显
// 示长度。
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
