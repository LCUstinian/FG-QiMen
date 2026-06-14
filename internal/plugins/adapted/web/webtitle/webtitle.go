// Copyright (c) 2026 LCUstinian
// SPDX-License-Identifier: MIT
//
// HTTP fingerprinting plugin. See the README's attribution section
// for upstream lineage.

// Package webtitle: HTTP fingerprinting plugin (Identify flow).
// Package webtitle：HTTP 指纹识别插件（Identify 流程）。
//
// The plugin struct + Plugin-interface methods + Identify live here.
// All HTTP machinery (protocol detection, client setup, redirect
// handling, header formatting, title extraction, favicon hash fetch,
// dedup, banner building) lives in helpers.go.
//
// 插件结构 + Plugin 接口方法 + Identify 在本文件。所有 HTTP 机器
//（协议探测、客户端搭建、重定向、头格式化、标题抽取、favicon
// 哈希、去重、banner 构造）都在 helpers.go。
//
// What this plugin does (Identify phase):
//   1. Smart protocol detection (HTTP vs HTTPS via cached service info
//      or active TLS probe).
//   2. HTTP GET with the no-redirect client; capture status / server
//      header / title.
//   3. If 3xx, follow the redirect and re-collect headers / body.
//   4. Fetch /favicon.ico and compute its mmh3 + MD5 hash.
//   5. Run fingerprint matching against:
//        - hardcoded rules (rules.go)
//        - FingerprintHub JSON (enhanced.go)
//   6. Return Banner with the matched fingerprints appended.
//
// 本插件做的事（Identify 阶段）：
//   1. 智能协议检测（HTTP vs HTTPS，靠缓存的服务信息或主动 TLS 探测）
//   2. HTTP GET（不跟随重定向），捕获状态码 / Server 头 / 标题
//   3. 如 3xx，跟随重定向并重新收 headers / body
//   4. 取 /favicon.ico 算 mmh3 + MD5 哈希
//   5. 跑指纹匹配：硬编码规则 + FingerprintHub JSON
//   6. 返回 Banner，附上匹配的指纹
//
// HARD RULE: this plugin does NOT run any POC. It only identifies.
// See the no-exploit policy in README.
//
// 硬性原则：本插件不跑 POC，只识别。参见 README 的"不做漏洞利用"
// 原则。
package webtitle

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/plugins"
	"github.com/LCUstinian/FG-QiMen/internal/plugins/adapted/web/webtitle/fingerprint"
	"github.com/LCUstinian/FG-QiMen/internal/types"
)

// WebTitlePlugin is the HTTP fingerprinting plugin.
// WebTitlePlugin 是 HTTP 指纹识别插件。
type WebTitlePlugin struct{}

// NewWebTitlePlugin returns a new plugin instance. / NewWebTitlePlugin 返回新实例。
func NewWebTitlePlugin() *WebTitlePlugin { return &WebTitlePlugin{} }

func init() { plugins.Register(NewWebTitlePlugin()) }

// Name implements plugins.Plugin. / Name 实现 plugins.Plugin。
func (p *WebTitlePlugin) Name() string { return "webtitle" }

// Ports returns the default web ports (HTTP and HTTPS).
// Ports 返回默认 web 端口。
func (p *WebTitlePlugin) Ports() []int { return []int{80, 443, 8080, 8443, 8000, 8888} }

// Modes returns Identify only. / Modes 仅返回 Identify。
func (p *WebTitlePlugin) Modes() plugins.Mode { return plugins.ModeIdentify }

// Credential is a no-op stub (v0.1 webtitle is identify-only).
// / Credential 空实现（v0.1 webtitle 仅识别）。
func (p *WebTitlePlugin) Credential(ctx context.Context, host string, port int, creds []types.Cred) *types.Result {
	return nil
}

// Identify implements plugins.Plugin. Performs the full webtitle
// dance: probe + collect + match.
//
// Identify 实现 plugins.Plugin。跑完整的 webtitle 流程：探测 + 收集 + 匹配。
func (p *WebTitlePlugin) Identify(ctx context.Context, host string, port int) *types.Result {
	timeout := 5 * time.Second
	if d, ok := ctx.Deadline(); ok {
		if left := time.Until(d); left > 0 && left < timeout {
			timeout = left
		}
	}

	scheme, baseURL := detectProtocol(ctx, host, port, timeout)
	displayURL := baseURL
	// Hide default ports in display. / 隐藏默认端口。
	if (scheme == "https" && port == 443) || (scheme == "http" && port == 80) {
		u, _ := url.Parse(baseURL)
		if u != nil {
			u.Host = host
			displayURL = u.String()
		}
	}

	// First request: no-redirect. / 第一次请求：不跟随重定向。
	req, err := http.NewRequestWithContext(ctx, "GET", baseURL, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	clientNR := newNoRedirectClient(timeout)
	resp, err := clientNR.Do(req)
	if err != nil {
		return nil
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	contentLen := len(body)

	// Build the per-source "CheckData" for the matcher. / 构造
	// 传给匹配器的 CheckData。
	checkData := fingerprint.CheckData{
		Body:    body,
		Headers: formatHeaders(resp.Header),
		Favicon: fetchFaviconHash(ctx, baseURL, timeout),
	}

	title := extractTitle(string(body))
	statusCode := resp.StatusCode
	server := resp.Header.Get("Server")

	// If 3xx, follow and re-collect. / 如 3xx，跟随重定向并重新收集。
	if statusCode >= 300 && statusCode < 400 {
		location := resp.Header.Get("Location")
		if redirectURL := resolveRedirect(baseURL, location); redirectURL != "" {
			if rd := fetchForRedirect(ctx, redirectURL, timeout); rd != nil {
				checkData.Body = append(checkData.Body, '\n')
				checkData.Body = append(checkData.Body, rd.Body...)
				merged := checkData.Headers
				if rd.Headers != "" {
					merged += "\n" + rd.Headers
				}
				checkData.Headers = merged
				if title == "" {
					title = extractTitle(string(rd.Body))
				}
			}
		}
	}

	// Run matching. / 跑匹配。
	matches := fingerprint.MatchAll(checkData)
	// Deduplicate + sort by name. / 去重 + 按名称排序。
	uniq := uniqSorted(matches)

	// Build the banner. / 构造 banner。
	banner := buildBanner(displayURL, statusCode, contentLen, title, server, uniq)
	return &types.Result{
		Host:    host,
		Port:    port,
		Service: "http",
		Banner:  banner,
		Time:    time.Now(),
	}
}
