// Package protocols: Elasticsearch authenticator.
//
// Strategy: HTTP GET / with `Authorization: Basic <base64(user:pass)>`
// header. 200 + body containing "elasticsearch" / "cluster_name" /
// "lucene_version" is a hit. 401 / 403 / non-ES body is a miss. We
// do NOT run any query (no GET _cluster, no GET _cat, no version()).
//
// HARD RULE: on a hit we return. We do NOT run any post-auth API call.
//
// 包 protocols：Elasticsearch 认证器。
// 策略：HTTP GET /，加 `Authorization: Basic <base64(user:pass)>` 头。
// 200 + body 含 "elasticsearch" / "cluster_name" / "lucene_version" 视为
// 命中。401 / 403 / 非 ES body 视为不命中。我们不跑任何 query（不
// GET _cluster、不 GET _cat、不 version()）。
//
// 硬性原则：命中即返回，不跑任何认证后 API。
package database

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/core/credential"
	"github.com/LCUstinian/FG-QiMen/internal/transport"
)

// ElasticsearchAuthenticator authenticates against Elasticsearch via
// HTTP Basic auth probe.
//
// DefaultPorts returns 9200/9300 (same as the existing Identify plugin).
// We don't introduce the fscan 9443 port convention — it adds a corner
// case for no real value.
//
// Note: HTTP by default; port 9243 uses HTTPS (M15 fix). ES
// installations with TLS on other ports are not auto-detected — the
// Identify plugin handles HTTPS detection for fingerprinting but for
// credential testing we route by port. / 注意：默认 HTTP；端口 9243
// 用 HTTPS（M15 修复）。其他端口上的 TLS ES 不自动探测——Identify 插
// 件处理 HTTPS 检测用于指纹，但凭据测试按端口路由。
//
// ElasticsearchAuthenticator 通过 HTTP Basic 认证探测对 ES 认证。
//
// DefaultPorts 返 9200/9300（与现有 Identify 插件一致）。不引入 fscan
// 的 9443 端口惯例。
type ElasticsearchAuthenticator struct{}

// NewElasticsearchAuthenticator returns a default Elasticsearch authenticator.
// NewElasticsearchAuthenticator 返回默认配置的 ES 认证器。
func NewElasticsearchAuthenticator() *ElasticsearchAuthenticator {
	return &ElasticsearchAuthenticator{}
}

// Name implements credential.Authenticator. / Name 实现 credential.Authenticator。
func (a *ElasticsearchAuthenticator) Name() string { return "elasticsearch" }

// DefaultPorts implements credential.Authenticator. / DefaultPorts 实现 credential.Authenticator。
//
// M15: added 9243 (HTTPS ES). Port 9243 uses https://, others use
// http://. / M15：加了 9243（HTTPS ES）。端口 9243 用 https://，其他
// 用 http://。
func (a *ElasticsearchAuthenticator) DefaultPorts() []int {
	return []int{9200, 9300, 9243}
}

// Authenticate implements credential.Authenticator. Tries each cred in order;
// returns the first hit or nil.
//
// Authenticate 实现 credential.Authenticator。按顺序尝试每个 cred；首个命中
// 返回 Hit。
func (a *ElasticsearchAuthenticator) Authenticate(ctx context.Context, host string, port int, creds []credential.Cred, timeout time.Duration) (*credential.Hit, error) {
	if len(creds) == 0 {
		return nil, nil
	}
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	for i, c := range creds {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if c.Method != "" && c.Method != credential.AuthPassword {
			continue
		}
		// Fall back to "elastic" (ES install default) when c.User is empty.
		// / c.User 空时回退到 "elastic"（ES 安装默认）。
		user := c.User
		if user == "" {
			user = "elastic"
		}
		hit, err := a.probe(ctx, addr, user, c.Pass, timeout)
		if err != nil {
			// Network-level error: bail out — same logic as PG. / 网络级
			// 错：退出——和 PG 一样。
			return nil, err
		}
		if hit {
			return &credential.Hit{
				Cred:     c,
				Attempts: i + 1,
				Time:     time.Now(),
			}, nil
		}
	}
	return nil, nil
}

// probe sends one GET / request. Returns (true, nil) on a hit,
// (false, nil) on a miss, (false, err) on network failure.
//
// probe 跑一次 GET / 请求。命中返 (true, nil)，不命中返 (false, nil)，
// 网络错返 (false, err)。
func (a *ElasticsearchAuthenticator) probe(ctx context.Context, addr, user, pass string, timeout time.Duration) (bool, error) {
	tr := &http.Transport{
		TLSClientConfig:       transport.TLSConfig(false),
		ResponseHeaderTimeout: timeout,
		DisableKeepAlives:     true,
	}
	client := &http.Client{Transport: tr, Timeout: timeout}
	// M15: port 9243 is HTTPS ES — use https://. Other ports stay
	// plaintext http://. / M15：端口 9243 是 HTTPS ES——用 https://。
	// 其他端口保持明文 http://。
	scheme := "http"
	if strings.HasSuffix(addr, ":9243") {
		scheme = "https"
	}
	req, err := http.NewRequestWithContext(ctx, "GET", scheme+"://"+addr+"/", nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("User-Agent", "fg-qimen/0.1")
	if user != "" || pass != "" {
		req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(user+":"+pass)))
	}
	resp, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, nil
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	// Cheap body guard: must look like an ES response. / 简单 body 守卫：
	// 必须像 ES 响应。
	text := string(body)
	if !strings.Contains(text, "elasticsearch") &&
		!strings.Contains(text, "cluster_name") &&
		!strings.Contains(text, "lucene_version") {
		return false, nil
	}
	return true, nil
}

// init registers the Elasticsearch authenticator with core/cred.
// init 把 ES 认证器注册到 core/cred。
func init() {
	credential.Register(NewElasticsearchAuthenticator())
}
