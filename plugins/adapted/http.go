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

	"github.com/LCUstinian/FG-QiMen/common"
	"github.com/LCUstinian/FG-QiMen/plugins"
)

// HTTPPlugin implements Identify (HTTP probe + title + Server header).
// HTTPPlugin 实现 Identify（HTTP 探测 + title + Server 头）。
type HTTPPlugin struct{}

// NewHTTPPlugin returns a fresh HTTPPlugin. Registers via init().
// NewHTTPPlugin 返回一个新的 HTTPPlugin。通过 init() 注册。
func NewHTTPPlugin() *HTTPPlugin { return &HTTPPlugin{} }

func init() { plugins.Register(NewHTTPPlugin()) }

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
func (p *HTTPPlugin) Identify(ctx context.Context, host string, port int) *common.Result {
	timeout := 3 * time.Second
	if d, ok := ctx.Deadline(); ok {
		if left := time.Until(d); left > 0 && left < timeout {
			timeout = left
		}
	}

	// Try plain HTTP first; if HTTPS port, try HTTPS. We do a quick
	// port-based guess for v0.1; v0.2 will auto-detect via TLS probe.
	// v0.1 先按端口猜协议；v0.2 通过 TLS 探测自动判断。
	scheme := "http"
	if port == 443 || port == 8443 {
		scheme = "https"
	}

	// Custom transport with a per-request dial timeout. We DO NOT
	// follow redirects in Identify — caller can rescan if needed.
	// 自定义 transport，每次拨号有超时。Identify 阶段不跟随重定向。
	tr := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   timeout,
			KeepAlive: 0,
		}).DialContext,
		TLSHandshakeTimeout:   timeout,
		ResponseHeaderTimeout: timeout,
		ExpectContinueTimeout: 500 * time.Millisecond,
		DisableKeepAlives:     true,
	}
	client := &http.Client{
		Transport: tr,
		Timeout:   timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	url := fmt.Sprintf("%s://%s/", scheme, net.JoinHostPort(host, fmt.Sprintf("%d", port)))
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("User-Agent", "fg-qimen/0.1")
	req.Header.Set("Accept", "*/*")

	resp, err := client.Do(req)
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

	return &common.Result{
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
func (p *HTTPPlugin) Credential(ctx context.Context, host string, port int, creds []common.Cred) *common.Result {
	return nil
}
