// Copyright (c) 2026 LCUstinian
// SPDX-License-Identifier: MIT
//
// SMB Identify plugin. Plain TCP connect + read; the SMB negotiate
// response begins with a 4-byte NetBIOS header + 0xFE 'SMB' magic,
// which is a reliable SMB v1+ marker. No payload / no shellcode /
// no upload.
//
// SMB 识别插件。普通 TCP 连 + 读——SMB 响应的 4 字节 NetBIOS 头 + 0xFE
// 'SMB' magic 是可靠的 SMB v1+ 标记。不传 payload、不打 shellcode、
// 不上传。
package smb

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/plugins"
	"github.com/LCUstinian/FG-QiMen/internal/types"
)

// Plugin identifies SMB servers via the NetBIOS+SMB magic header. /
// Plugin 通过 NetBIOS+SMB magic 头识别 SMB 服务。
type Plugin struct{}

// New returns a new smb plugin. / New 返回一个新的 smb 插件。
func New() *Plugin { return &Plugin{} }

func init() { plugins.Register(New()) }

// Name implements plugins.Plugin. / Name 实现 plugins.Plugin。
func (p *Plugin) Name() string { return "smb" }

// Ports returns default SMB ports. / Ports 返回默认 SMB 端口。
func (p *Plugin) Ports() []int { return []int{445, 139} }

// Modes returns Identify only. / Modes 仅返回 Identify。
func (p *Plugin) Modes() plugins.Mode { return plugins.ModeIdentify }

// Credential is a no-op stub. / Credential 空 stub。
func (p *Plugin) Credential(ctx context.Context, host string, port int, creds []types.Cred) *types.Result {
	return nil
}

// Identify opens a TCP connection and reads the initial SMB banner.
// Identify 开 TCP 连并读初始 SMB banner。
func (p *Plugin) Identify(ctx context.Context, host string, port int) *types.Result {
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	d := net.Dialer{Timeout: 3 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))

	// Send a minimal SMB2 negotiate. / 发最小 SMB2 negotiate。
	// 36 bytes: 4 (NetBIOS header) + 32 (SMB2 header)
	// / 36 字节：4 字节 NetBIOS 头 + 32 字节 SMB2 头
	negotiate := []byte{
		0x00, 0x00, 0x00, 0x2c, // NetBIOS session message (length=44)
		0xfe, 'S', 'M', 'B', // SMB2 magic
		0x00, 0x00, 0x00, 0x00, // header length
		0x00, 0x00, 0x01, 0x00, // credits
		0x00, 0x00, 0x00, 0x00, // flags
		0x00, 0x00, 0x00, 0x00, // next command
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // message id
		0x00, 0x00, 0x00, 0x00, // reserved
		0x00, 0x00, 0x00, 0x00, // tree id
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, // session id
	}
	if _, err := conn.Write(negotiate); err != nil {
		return nil
	}
	resp := make([]byte, 1024)
	n, _ := conn.Read(resp)
	if n < 8 {
		return nil
	}
	// Look for SMB2 magic (\xFESMB) or SMB1 magic (\xFFSMBr/\xFFSMBs).
	// / 找 SMB2 magic 或 SMB1 magic。
	for i := 4; i+4 < n; i++ {
		if resp[i] == 0xfe && string(resp[i+1:i+4]) == "SMB" {
			return &types.Result{
				Host: host, Port: port, Service: "smb",
				Banner: "SMBv2/v3", Time: time.Now(),
			}
		}
		if (resp[i] == 0xff) && i+4 < n {
			tail := string(resp[i+1 : i+4])
			if tail == "SMB" || strings.HasPrefix(tail, "SMB") {
				return &types.Result{
					Host: host, Port: port, Service: "smb",
					Banner: "SMBv1", Time: time.Now(),
				}
			}
		}
	}
	return nil
}
