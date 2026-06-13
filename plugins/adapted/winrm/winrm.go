// Copyright (c) 2026 LCUstinian
// SPDX-License-Identifier: MIT
//
// WinRM Identify plugin. Sends a GET /wsman probe; 401/200/405 from
// the WinRM listener indicates it's a WinRM endpoint (we treat 401
// as "auth required" = hit, 405 as "method not allowed but endpoint
// exists" = hit, anything else as miss).
//
// Credential() is routed through core/cred/protocols/winrm.go
// (WinRMAuthenticator, HTTP Basic auth probe).
//
// WinRM 识别插件。发 GET /wsman 探针；WinRM 监听器返 401/200/405 即
// 视为 WinRM 端点（401 = 需认证 = 命中，405 = 方法不允许但端点存在
// = 命中，其他 = miss）。
//
// Credential() 走 core/cred/protocols/winrm.go（WinRMAuthenticator，
// HTTP Basic 认证探测）。
package winrm

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/LCUstinian/FG-QiMen/common"
	"github.com/LCUstinian/FG-QiMen/plugins"
)

// Plugin identifies WinRM endpoints. / Plugin 识别 WinRM 端点。
type Plugin struct{}

// New returns a new winrm plugin. / New 返回一个新的 winrm 插件。
func New() *Plugin { return &Plugin{} }

func init() { plugins.Register(New()) }

// Name implements plugins.Plugin. / Name 实现 plugins.Plugin。
func (p *Plugin) Name() string { return "winrm" }

// Ports returns default WinRM ports. / Ports 返回默认 WinRM 端口。
func (p *Plugin) Ports() []int { return []int{5985, 5986} }

// Modes returns Identify + Credential. / Modes 返回 Identify + Credential。
func (p *Plugin) Modes() plugins.Mode { return plugins.ModeIdentify | plugins.ModeCredential }

// Credential is a no-op stub. / Credential 空 stub。
func (p *Plugin) Credential(ctx context.Context, host string, port int, creds []common.Cred) *common.Result {
	return nil
}

// Identify probes the WinRM endpoint with a GET /wsman. A 200, 401,
// or 405 indicates a WinRM listener. / Identify 用 GET /wsman 探 WinRM
// 端点。200 / 401 / 405 即 WinRM 监听器。
func (p *Plugin) Identify(ctx context.Context, host string, port int) *common.Result {
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	tr := &http.Transport{
		TLSClientConfig:       &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
		ResponseHeaderTimeout: 3 * time.Second,
		DisableKeepAlives:     true,
	}
	client := &http.Client{Transport: tr, Timeout: 3 * time.Second}
	// Try HTTP first. / 先 HTTP。
	for _, scheme := range []string{"http", "https"} {
		req, _ := http.NewRequestWithContext(ctx, "GET", scheme+"://"+addr+"/wsman", nil)
		req.Header.Set("User-Agent", "fg-qimen/0.1")
		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		// 200 (auth not required), 401 (auth required), 405
		// (method not allowed) all indicate a WinRM listener.
		// / 200（无需认证）、401（需认证）、405（方法不允许）都表示
		// WinRM 监听器存在。
		if resp.StatusCode == http.StatusOK ||
			resp.StatusCode == http.StatusUnauthorized ||
			resp.StatusCode == http.StatusMethodNotAllowed {
			return &common.Result{
				Host: host, Port: port, Service: "winrm",
				Banner: fmt.Sprintf("WinRM %s", scheme), Time: time.Now(),
			}
		}
	}
	return nil
}
