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
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/core/credential"
)

// ElasticsearchAuthenticator authenticates against Elasticsearch via
// HTTP Basic auth probe.
//
// DefaultPorts returns 9200/9300 (same as the existing Identify plugin).
// We don't introduce the fscan 9443 port convention — it adds a corner
// case for no real value.
//
// Note: this is HTTP only. ES installations with TLS on 9243 are a
// v0.2+ slice — the Identify plugin handles HTTPS detection for
// fingerprinting but for credential testing we want a single, simple
// path. / 注意：只走 HTTP。带 TLS 的 9243 是 v0.2+ 切片——Identify 插件
// 处理 HTTPS 检测用于指纹，但凭据测试我们要单一简单路径。
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
func (a *ElasticsearchAuthenticator) DefaultPorts() []int {
	return []int{9200, 9300}
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
		TLSClientConfig:       &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
		ResponseHeaderTimeout: timeout,
		DisableKeepAlives:     true,
	}
	client := &http.Client{Transport: tr, Timeout: timeout}
	req, err := http.NewRequestWithContext(ctx, "GET", "http://"+addr+"/", nil)
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
