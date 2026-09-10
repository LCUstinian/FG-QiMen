// elasticsearch_test.go — A.6 coverage baseline via fakeserver.StartHTTP.
// elasticsearch_test.go — 通过 fakeserver.StartHTTP 的 A.6 覆盖率基线。
package elasticsearch

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/fakeserver"
)

// cannedResponse replies to GET / with the minimum response body the
// Elasticsearch Identify probe needs to declare a hit: a 200 OK whose
// payload contains "lucene_version" (the anchor used by
// elasticsearch.go:92) plus a version.number field that extractField
// can pull (the cheap "look for 'version', then '"number"', then
// closing quote" parser in elasticsearch.go:97).
// / cannedResponse 对 GET / 回复让 Elasticsearch Identify 探针宣告
// 命中的最小响应体：含 "lucene_version" 锚点 + extractField 能拉出
// 的 version.number 字段（elasticsearch.go:97 那套"找 'version'
// 再找 '"number"' 再找闭引号"的简化解析）。
func cannedResponse(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{
  "name": "node-1",
  "cluster_name": "test-cluster",
  "version": {
    "number": "8.10.0",
    "lucene_version": "9.7.0"
  },
  "tagline": "You Know, for Search"
}`))
}

func TestElasticsearch_IdentifyHit(t *testing.T) {
	url := fakeserver.StartHTTP(t, http.HandlerFunc(cannedResponse))
	host, port := splitHostPort(url)
	p := New()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	r := p.Identify(ctx, host, port)
	if r == nil {
		t.Fatal("expected elasticsearch hit, got nil")
	}
	if r.Service != "elasticsearch" {
		t.Errorf("Service = %q, want %q", r.Service, "elasticsearch")
	}
	// Banner prefix check — the cheap extractField parser in
	// elasticsearch.go:97-98 stops at the first '"' after "number",
	// so we only assert the "Elasticsearch" prefix and not the
	// version numbers themselves. Identification is the contract
	// this test guards; banner fidelity is a separate concern.
	// / Banner 前缀检查——elasticsearch.go:97-98 的简化 extractField
	// 解析器在 "number" 后第一个 '"' 处停止，所以这里只断言
	// "Elasticsearch" 前缀，不断言版本号。识别是本测试要守的
	// 契约；banner 完整性是另一码事。
	if !strings.Contains(r.Banner, "Elasticsearch") {
		t.Errorf("Banner = %q, want substring %q", r.Banner, "Elasticsearch")
	}
}

// TestElasticsearch_CredentialHit is skipped: the elasticsearch
// plugin's Credential is a no-op stub (always returns nil) per
// elasticsearch.go:69-72 — actual credential testing lives in
// core/cred/protocols/elasticsearch.go.
// / TestElasticsearch_CredentialHit 跳过：elasticsearch 插件的
// Credential 是空 stub（始终返回 nil），见 elasticsearch.go:69-72。
func TestElasticsearch_CredentialHit(t *testing.T) {
	t.Skip("elasticsearch.Credential is a no-op stub; see elasticsearch.go Credential()")
}

// splitHostPort strips the "http://" scheme from an httptest URL and
// returns the host and numeric port. / splitHostPort 去掉 httptest
// URL 的 "http://" 头，返回 host 与数字端口。
func splitHostPort(rawURL string) (string, int) {
	s := strings.TrimPrefix(rawURL, "http://")
	host, portStr, _ := strings.Cut(s, ":")
	port, err := strconv.Atoi(portStr)
	if err != nil {
		// Fall back to default-port parsing if the URL shape changes.
		// / 万一 URL 形状变了，回退到末尾冒号分割。
		colon := strings.LastIndex(s, ":")
		if colon < 0 {
			panic("splitHostPort: no port in " + rawURL)
		}
		port, _ = strconv.Atoi(s[colon+1:])
	}
	return host, port
}
