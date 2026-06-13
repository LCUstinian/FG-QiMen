// Copyright (c) 2026 LCUstinian
// SPDX-License-Identifier: MIT
//
// Docker Identify plugin. GET /_ping (always no-auth) returns
// "OK" / 200; we report the version from /info (also no-auth).
// Credential() is routed through core/cred/protocols/docker.go
// (DockerAuthenticator, HTTP Basic auth probe to /images/json).
//
// Docker 识别插件。GET /_ping（始终无 auth）返 "OK" / 200；我们从
// /info（也无 auth）拿版本。Credential() 走 core/cred/protocols/
// docker.go（DockerAuthenticator，HTTP Basic 认证探测 /images/json）。
package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/common"
	"github.com/LCUstinian/FG-QiMen/internal/plugins"
)

// Plugin identifies Docker daemons. / Plugin 识别 Docker 守护进程。
type Plugin struct{}

// New returns a new docker plugin. / New 返回一个新的 docker 插件。
func New() *Plugin { return &Plugin{} }

func init() { plugins.Register(New()) }

// Name implements plugins.Plugin. / Name 实现 plugins.Plugin。
func (p *Plugin) Name() string { return "docker" }

// Ports returns default Docker ports. / Ports 返回默认 Docker 端口。
func (p *Plugin) Ports() []int { return []int{2375, 2376} }

// Modes returns Identify + Credential. / Modes 返回 Identify + Credential。
func (p *Plugin) Modes() plugins.Mode { return plugins.ModeIdentify | plugins.ModeCredential }

// Credential is a no-op stub. / Credential 空 stub。
func (p *Plugin) Credential(ctx context.Context, host string, port int, creds []common.Cred) *common.Result {
	return nil
}

// Identify GETs /_ping and /info to confirm Docker daemon presence
// + version. / Identify GET /_ping 和 /info 确认 Docker daemon 在 + 版本。
func (p *Plugin) Identify(ctx context.Context, host string, port int) *common.Result {
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	tr := &http.Transport{DisableKeepAlives: true}
	client := &http.Client{Transport: tr, Timeout: 3 * time.Second}
	// /_ping (always no-auth). / /_ping（始终无 auth）。
	req, _ := http.NewRequestWithContext(ctx, "GET", "http://"+addr+"/_ping", nil)
	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(body) != "OK" {
		return nil
	}
	// /info (also no-auth). Parse APIVersion. / /info（也无 auth）。
	// 解析 APIVersion。
	req, _ = http.NewRequestWithContext(ctx, "GET", "http://"+addr+"/info", nil)
	resp2, err := client.Do(req)
	if err != nil {
		// Still a hit, just no version. / 仍是命中，只是无版本。
		return &common.Result{
			Host: host, Port: port, Service: "docker",
			Banner: "Docker", Time: time.Now(),
		}
	}
	body2, _ := io.ReadAll(resp2.Body)
	_ = resp2.Body.Close()
	var info struct {
		APIVersion string `json:"APIVersion"`
		Version    string `json:"Version"`
	}
	_ = json.Unmarshal(body2, &info)
	banner := "Docker"
	if info.Version != "" {
		banner = "Docker " + info.Version
	} else if info.APIVersion != "" {
		banner = "Docker API " + info.APIVersion
	}
	return &common.Result{
		Host: host, Port: port, Service: "docker",
		Banner: banner, Time: time.Now(),
	}
}
