// http.go — HTTP client setup, redirect handling, scheme detect.
//
// All the network plumbing for the webtitle plugin lives here:
//   - detectProtocol: pick http vs https via a quick TLS handshake
//   - newClient: standard client with up-to-5 redirect follow
//   - newNoRedirectClient: same but redirects are errors (used for
//     the credential-probe path so we don't accidentally auth
//     against a 302 hop)
//   - fetchForRedirect: GET a URL, return headers + body for
//     the redirect-resolution logic
//   - resolveRedirect: turn a Location header into an absolute URL
//
// http.go — HTTP 客户端搭建、重定向处理、协议探测。webtitle
// 插件的所有网络配置都在此：
//   - detectProtocol：通过快速 TLS 握手选 http 或 https
//   - newClient：标准客户端，最多跟随 5 次重定向
//   - newNoRedirectClient：同上但重定向是错误（凭据探测用，避免
//     误对 302 跳认证）
//   - fetchForRedirect：GET 一个 URL，返头+体供重定向解析
//   - resolveRedirect：把 Location 头转成绝对 URL
package webtitle

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/plugins/adapted/web/webtitle/fingerprint"
	"github.com/LCUstinian/FG-QiMen/internal/transport"
)

// detectProtocol picks http or https based on a quick TLS probe
// (returns true if a TLS handshake completes within timeout). It
// also returns the full baseURL ("http://host:port" or "https://host:port").
//
// detectProtocol 基于快速 TLS 探测（timeout 内 TLS 握手成功则 https）选
// http 或 https。同时返回完整 baseURL。
func detectProtocol(ctx context.Context, host string, port int, timeout time.Duration) (scheme, baseURL string) {
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	d := net.Dialer{Timeout: timeout}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return "http", "http://" + addr
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))
	tlsConn := tls.Client(conn, transport.TLSConfig(false))
	if err := tlsConn.HandshakeContext(ctx); err == nil {
		return "https", "https://" + addr
	}
	return "http", "http://" + addr
}

// newNoRedirectClient returns an http.Client that does NOT follow
// redirects (each redirect is reported as http.ErrUseLastResponse).
// Used by the credential-probe path so a 302 hop doesn't accidentally
// have Basic auth applied to a different host.
//
// newNoRedirectClient 返回不跟随重定向的 http.Client。供凭据探测
// 用，避免 302 跳把 Basic auth 误打到不同主机。
func newNoRedirectClient(timeout time.Duration) *http.Client {
	tr := &http.Transport{
		TLSClientConfig:       transport.TLSConfig(false),
		TLSHandshakeTimeout:   timeout,
		ResponseHeaderTimeout: timeout,
		DialContext: (&net.Dialer{
			Timeout: timeout,
		}).DialContext,
	}
	return &http.Client{Transport: tr, Timeout: timeout}
}

// newClient returns the standard client (up to 5 redirects).
// / newClient 返回标准 client（最多跟随 5 次重定向）。
func newClient(timeout time.Duration) *http.Client {
	tr := &http.Transport{
		TLSClientConfig:       transport.TLSConfig(false),
		TLSHandshakeTimeout:   timeout,
		ResponseHeaderTimeout: timeout,
		DialContext: (&net.Dialer{
			Timeout: timeout,
		}).DialContext,
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

// formatHeaders turns an http.Header into a single string for
// matching. / formatHeaders 把 http.Header 转成单字符串用于匹配。
func formatHeaders(h http.Header) string {
	var b strings.Builder
	for k, v := range h {
		for _, val := range v {
			fmt.Fprintf(&b, "%s: %s\n", k, val)
		}
	}
	return b.String()
}

