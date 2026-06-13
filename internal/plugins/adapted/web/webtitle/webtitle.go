// Copyright (c) 2026 LCUstinian
// SPDX-License-Identifier: MIT
//
// HTTP fingerprinting plugin. See the README's attribution section
// for upstream lineage.

// Package webtitle implements the HTTP fingerprinting plugin.
//
// Package webtitle 实现 HTTP 指纹识别插件。
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
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/LCUstinian/FG-QiMen/internal/common"
	"github.com/LCUstinian/FG-QiMen/internal/plugins"
	"github.com/LCUstinian/FG-QiMen/internal/plugins/adapted/web/webtitle/fingerprint"
)

var (
	titleRegex      = regexp.MustCompile(`(?i)<title[^>]*>([^<]+)</title>`)
	whitespaceRegex = regexp.MustCompile(`\s+`)
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
func (p *WebTitlePlugin) Credential(ctx context.Context, host string, port int, creds []common.Cred) *common.Result {
	return nil
}

// Identify implements plugins.Plugin. Performs the full webtitle
// dance: probe + collect + match.
//
// Identify 实现 plugins.Plugin。跑完整的 webtitle 流程：探测 + 收集 + 匹配。
func (p *WebTitlePlugin) Identify(ctx context.Context, host string, port int) *common.Result {
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
	return &common.Result{
		Host:    host,
		Port:    port,
		Service: "http",
		Banner:  banner,
		Time:    time.Now(),
	}
}

// ─────────────────────────────────────────────────────────────────────
// helpers
// ─────────────────────────────────────────────────────────────────────

// detectProtocol picks http or https based on a quick TLS probe
// (returns true if a TLS handshake completes within timeout). It also
// returns the full baseURL ("http://host:port" or "https://host:port").
//
// detectProtocol 基于快速 TLS 探测（timeout 内 TLS 握手成功则 https）选
// http 或 https。同时返回完整 baseURL。
func detectProtocol(ctx context.Context, host string, port int, timeout time.Duration) (scheme, baseURL string) {
	d := net.Dialer{Timeout: timeout}
	conn, err := d.DialContext(ctx, "tcp", fmt.Sprintf("%s:%d", host, port))
	if err != nil {
		// Fall back to http. / 回退到 http。
		return "http", fmt.Sprintf("http://%s:%d", host, port)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))
	tlsConn := tls.Client(conn, &tls.Config{InsecureSkipVerify: true}) //nolint:gosec
	if err := tlsConn.HandshakeContext(ctx); err == nil {
		_ = tlsConn.Close()
		return "https", fmt.Sprintf("https://%s:%d", host, port)
	}
	_ = tlsConn.Close()
	return "http", fmt.Sprintf("http://%s:%d", host, port)
}

// newNoRedirectClient returns an http.Client that does NOT follow
// redirects. / newNoRedirectClient 返回不跟随重定向的 http.Client。
func newNoRedirectClient(timeout time.Duration) *http.Client {
	tr := &http.Transport{
		TLSClientConfig:       &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
		TLSHandshakeTimeout:   timeout,
		ResponseHeaderTimeout: timeout,
		DisableKeepAlives:     true,
	}
	return &http.Client{
		Transport: tr,
		Timeout:   timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// newClient returns a standard client (follows redirects up to 5).
// / newClient 返回标准 client（最多跟随 5 次重定向）。
func newClient(timeout time.Duration) *http.Client {
	tr := &http.Transport{
		TLSClientConfig:       &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
		TLSHandshakeTimeout:   timeout,
		ResponseHeaderTimeout: timeout,
		DisableKeepAlives:     true,
	}
	return &http.Client{
		Transport: tr,
		Timeout:   timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}
}

// fetchForRedirect fetches the redirect target and returns the
// CheckData, or nil on error. / fetchForRedirect 取跳转目标并返回
// CheckData；错误返回 nil。
func fetchForRedirect(ctx context.Context, url string, timeout time.Duration) *fingerprint.CheckData {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	resp, err := newClient(timeout).Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return &fingerprint.CheckData{
		Body:    body,
		Headers: formatHeaders(resp.Header),
		Favicon: nil, // skip favicon on redirect (saves time)
	}
}

// resolveRedirect resolves a possibly-relative Location header
// against baseURL. / resolveRedirect 把可能相对的 Location 头相对
// baseURL 解析。
func resolveRedirect(baseURL, location string) string {
	if location == "" {
		return ""
	}
	if strings.HasPrefix(location, "http://") || strings.HasPrefix(location, "https://") {
		return location
	}
	base, err := url.Parse(baseURL)
	if err != nil {
		return ""
	}
	ref, err := url.Parse(location)
	if err != nil {
		return ""
	}
	return base.ResolveReference(ref).String()
}

// formatHeaders turns an http.Header into a single string for matching.
// / formatHeaders 把 http.Header 转成单字符串用于匹配。
func formatHeaders(h http.Header) string {
	var b strings.Builder
	for k, v := range h {
		for _, val := range v {
			fmt.Fprintf(&b, "%s: %s\n", k, val)
		}
	}
	return b.String()
}

// extractTitle returns the trimmed <title> from an HTML body, or "".
// / extractTitle 返回 HTML body 中裁剪后的 <title>，或 ""。
func extractTitle(html string) string {
	m := titleRegex.FindStringSubmatch(html)
	if len(m) < 2 {
		return ""
	}
	t := strings.TrimSpace(m[1])
	t = whitespaceRegex.ReplaceAllString(t, " ")
	if len(t) > 100 {
		t = t[:100] + "..."
	}
	if utf8.ValidString(t) {
		return t
	}
	return ""
}

// fetchFaviconHash downloads /favicon.ico and computes its mmh3 + MD5
// hashes. Returns nil on any error.
//
// fetchFaviconHash 拉 /favicon.ico 并算 mmh3 + MD5 哈希。出错返回 nil。
func fetchFaviconHash(ctx context.Context, baseURL string, timeout time.Duration) []int32 {
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil
	}
	iconURL := fmt.Sprintf("%s://%s/favicon.ico", u.Scheme, u.Host)
	req, err := http.NewRequestWithContext(ctx, "GET", iconURL, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	resp, err := newClient(timeout).Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // max 1MB
	if err != nil || len(data) == 0 {
		return nil
	}
	return fingerprint.CalculateFaviconHashes(data)
}

// uniqSorted returns the unique strings from in, sorted alphabetically.
// / uniqSorted 返回 in 中去重后按字典序排列的字符串。
func uniqSorted(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if _, dup := seen[s]; dup {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	// Sort for stable output. / 排序保证输出稳定。
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1] > out[j]; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}

// buildBanner formats the result line.
// buildBanner 格式化结果行。
func buildBanner(displayURL string, status, length int, title, server string, fps []string) string {
	titleStr := title
	if titleStr == "" {
		titleStr = "None"
	}
	b := fmt.Sprintf("code:%d len:%d title:%q", status, length, titleStr)
	if server != "" {
		b += fmt.Sprintf(" server:%q", server)
	}
	if len(fps) > 0 {
		b += " [" + strings.Join(fps, "|") + "]"
	}
	return displayURL + " " + b
}
