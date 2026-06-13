// Copyright (c) 2026 LCUstinian
// SPDX-License-Identifier: MIT
//
// Elasticsearch Identify plugin. Plain HTTP GET / (Elasticsearch
// always responds to this on any port it's bound to). No indexing
// / search / write paths.
//
// Elasticsearch 识别插件。普通 HTTP GET /（ES 在任何绑定端口都会
// 响应这个）。无索引 / 搜索 / 写路径。
package elasticsearch

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/common"
	"github.com/LCUstinian/FG-QiMen/internal/plugins"
)

// Plugin identifies Elasticsearch via HTTP GET /.
// Plugin 通过 HTTP GET / 识别 Elasticsearch。
type Plugin struct{}

// New returns a new elasticsearch plugin. / New 返回一个新的 elasticsearch 插件。
func New() *Plugin { return &Plugin{} }

func init() { plugins.Register(New()) }

// Name implements plugins.Plugin. / Name 实现 plugins.Plugin。
func (p *Plugin) Name() string { return "elasticsearch" }

// Ports returns default Elasticsearch ports. / Ports 返回默认 ES 端口。
func (p *Plugin) Ports() []int { return []int{9200, 9300} }

// Modes returns Identify + Credential. / Modes 返回 Identify + Credential。
//
// Credential() is implemented in core/cred/protocols/elasticsearch.go
// (ElasticsearchAuthenticator via HTTP Basic). The plugin's Credential
// method stays as a no-op stub because the pipeline routes cred testing
// through the central credential.Scheduler. / Credential() 实现在 core/cred/
// protocols/elasticsearch.go (ElasticsearchAuthenticator via HTTP Basic)。
// plugin 的 Credential 方法是空 stub，因为管线把凭据测试路由到中央
// credential.Scheduler。
func (p *Plugin) Modes() plugins.Mode { return plugins.ModeIdentify | plugins.ModeCredential }

// Credential is a no-op stub. / Credential 空 stub。
func (p *Plugin) Credential(ctx context.Context, host string, port int, creds []common.Cred) *common.Result {
	return nil
}

// Identify does HTTP GET / and parses the JSON response's
// "version.number" / "lucene_version" fields. / Identify 跑 HTTP GET /
// 并解析 JSON 响应的 "version.number" / "lucene_version" 字段。
func (p *Plugin) Identify(ctx context.Context, host string, port int) *common.Result {
	tr := &http.Transport{
		TLSClientConfig:       &tls.Config{InsecureSkipVerify: true}, //nolint:gosec
		ResponseHeaderTimeout: 3 * time.Second,
		DisableKeepAlives:     true,
	}
	client := &http.Client{Transport: tr, Timeout: 3 * time.Second}
	// Smart protocol: try HTTPS first, fall back to HTTP. / 智能协议：
	// 先试 HTTPS，回退 HTTP。
	hosts := []string{"https", "http"}
	for _, scheme := range hosts {
		req, _ := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("%s://%s/", scheme, net.JoinHostPort(host, fmt.Sprintf("%d", port))), nil)
		req.Header.Set("User-Agent", "fg-qimen/0.1")
		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		body := make([]byte, 4096)
		n, _ := resp.Body.Read(body)
		_ = resp.Body.Close()
		text := string(body[:n])
		if !strings.Contains(text, "lucene_version") {
			continue
		}
		// Cheap parse: just regex out the version numbers.
		// / 简化解析：只正则出版本号。
		esVer := extractField(text, "version", `"number"`)
		lucene := extractField(text, "lucene_version", `"`)
		banner := "Elasticsearch " + esVer
		if lucene != "" {
			banner += " (lucene " + lucene + ")"
		}
		return &common.Result{
			Host: host, Port: port, Service: "elasticsearch",
			Banner: banner, Time: time.Now(),
		}
	}
	return nil
}

// extractField pulls a JSON string value after a field name. / extractField
// 在 field 名后拉 JSON 字符串值。
func extractField(s, anchor, delim string) string {
	idx := strings.Index(s, anchor)
	if idx < 0 {
		return ""
	}
	rest := s[idx+len(anchor):]
	q := strings.Index(rest, delim)
	if q < 0 {
		return ""
	}
	rest = rest[q+len(delim):]
	// Read until closing quote. / 读到闭引号。
	end := strings.IndexByte(rest, '"')
	if end < 0 {
		return ""
	}
	return rest[:end]
}

// satisfy the json import (kept for future structured parsing).
// 满足 json 导入（保留以便未来结构化解析）。
var _ = json.Valid
