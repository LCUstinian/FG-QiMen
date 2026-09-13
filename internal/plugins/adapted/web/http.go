// http.go — HTTP plugin (Identify only in v0.1).
// http.go — HTTP 插件（v0.1 仅识别）。
//
// Performs a simple HTTP GET against the target and extracts:
//   - Status code
//   - Server header
//   - HTML <title>
//
// v0.1 implementation: hand-written from scratch. v0.2+ will replace
// this with a port of the upstream webtitle.go framework (CMS / WAF /
// favicon matching).
//
// v0.1 实现：从零手写。v0.2+ 会替换为移植上游 webtitle.go 框架
// （CMS / WAF / favicon 匹配）。
package adapted

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/plugins"
	"github.com/LCUstinian/FG-QiMen/internal/types"
)

// HTTPPlugin implements Identify (HTTP probe + title + Server header).
// HTTPPlugin 实现 Identify（HTTP 探测 + title + Server 头）。
type HTTPPlugin struct{}

// NewHTTPPlugin returns a fresh HTTPPlugin. Registers via init().
// NewHTTPPlugin 返回一个新的 HTTPPlugin。通过 init() 注册。
func NewHTTPPlugin() *HTTPPlugin { return &HTTPPlugin{} }

func init() { plugins.Register(NewHTTPPlugin()) }

// webHTTPClient is a process-wide HTTP client. Hoisting avoids
// allocating a fresh http.Transport (TCP+TLS pool) per Identify
// call; 200 workers × 100 web ports = 20k allocation sites removed
// per scan. The transport has DisableKeepAlives=true to keep
// behaviour identical to the legacy per-call design (no idle
// connection reuse — each probe is to a different target). Per-call
// deadline is supplied via http.NewRequestWithContext, not via
// http.Client.Timeout, so the package-level client can be reused
// across Identify calls with different ctx deadlines.
// webHTTPClient 是进程级 HTTP client。提升为包级避免每次 Identify
// 调用都分配新 http.Transport（TCP+TLS pool）；200 worker × 100 web
// 端口 = 单次扫描省 20k 次分配。transport 设 DisableKeepAlives=true
// 保持与旧 per-call 行为一致（不重用空闲连接——每次探测目标不同）。
// per-call deadline 通过 http.NewRequestWithContext 传入，而非
// http.Client.Timeout，所以包级 client 可跨 Identify 调用复用，
// 各自带不同 ctx deadline。
var webHTTPClient = &http.Client{
	Transport: &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   3 * time.Second,
			KeepAlive: 0,
		}).DialContext,
		TLSHandshakeTimeout:   3 * time.Second,
		ResponseHeaderTimeout: 3 * time.Second,
		ExpectContinueTimeout: 500 * time.Millisecond,
		DisableKeepAlives:     true,
	},
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	},
}

// Name implements plugins.Plugin. / Name 实现 plugins.Plugin。
func (p *HTTPPlugin) Name() string { return "http" }

// Ports returns default HTTP / HTTPS ports. / Ports 返回默认 HTTP / HTTPS 端口。
func (p *HTTPPlugin) Ports() []int { return []int{80, 443, 8080, 8443, 8000, 8888} }

// Modes returns Identify only in v0.1. Credential testing of HTTP basic
// auth is planned for v0.2+.
// Modes 在 v0.1 仅返回 Identify。HTTP basic auth 凭据测试计划 v0.2+。
func (p *HTTPPlugin) Modes() plugins.Mode { return plugins.ModeIdentify }

// titleRegex extracts <title>...</title>. Case-insensitive. dotall so
// the title can span lines.
// titleRegex 提取 <title>...</title>。大小写不敏感，dotall 允许跨行。
var titleRegex = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)

// Identify performs HTTP GET and returns a *Result with title, server,
// and status code on success.
//
// Identify 执行 HTTP GET，成功时返回带 title、server、状态码的 *Result。
func (p *HTTPPlugin) Identify(ctx context.Context, host string, port int) *types.Result {
	// Try plain HTTP first; if HTTPS port, try HTTPS. We do a quick
	// port-based guess for v0.1; v0.2 will auto-detect via TLS probe.
	// v0.1 先按端口猜协议；v0.2 通过 TLS 探测自动判断。
	scheme := "http"
	if port == 443 || port == 8443 {
		scheme = "https"
	}

	url := fmt.Sprintf("%s://%s/", scheme, net.JoinHostPort(host, fmt.Sprintf("%d", port)))
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("User-Agent", "fg-qimen/0.1")
	req.Header.Set("Accept", "*/*")

	resp, err := webHTTPClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	// Read up to 64 KiB of body for title extraction. We don't
	// decompress gzipped bodies in v0.1.
	// 最多读 64 KiB body 用于 title 提取。v0.1 不解压 gzip。
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))

	// Build the Banner: "status <code> | server=<Server> | title=<title>".
	// / Banner 格式："status <code> | server=<Server> | title=<title>"。
	var b strings.Builder
	fmt.Fprintf(&b, "status %d", resp.StatusCode)
	if server := resp.Header.Get("Server"); server != "" {
		fmt.Fprintf(&b, " | server=%q", server)
	}
	if m := titleRegex.FindSubmatch(body); m != nil {
		title := strings.TrimSpace(string(m[1]))
		title = collapseWS(title)
		if title != "" {
			fmt.Fprintf(&b, " | title=%q", title)
		}
	}

	return &types.Result{
		Host:    host,
		Port:    port,
		Service: "http",
		Banner:  b.String(),
		Time:    time.Now(),
	}
}

// collapseWS collapses runs of whitespace into a single space and trims.
// collapseWS 把连续空白折叠成单个空格并去除首尾空白。
func collapseWS(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	var b strings.Builder
	prevSpace := false
	for _, r := range s {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' {
			if !prevSpace {
				b.WriteByte(' ')
				prevSpace = true
			}
			continue
		}
		b.WriteRune(r)
		prevSpace = false
	}
	return b.String()
}

// Credential is a no-op stub in v0.1. v0.2+ may add HTTP Basic auth
// testing; for now we return nil.
//
// Credential 在 v0.1 是空实现。v0.2+ 可能加 HTTP Basic auth 测试；
// 当前返回 nil。
func (p *HTTPPlugin) Credential(ctx context.Context, host string, port int, creds []types.Cred) *types.Result {
	return nil
}
