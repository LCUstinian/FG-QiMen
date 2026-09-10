// kibana_test.go — fake-server test for the kibana Identify plugin.
// / kibana 插件的假服务器测试。
package kibana

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/fakeserver"
)

// TestKibana_IdentifyHit verifies the kibana plugin identifies a
// server that responds to GET /api/status with the canonical Kibana
// JSON (name + version.number). The plugin speaks plain HTTP over a
// raw TCP conn, so an httptest server is a fine target: Go's HTTP
// stack is happy to parse the raw request bytes the plugin writes.
// / 验证 kibana 插件识别 /api/status 返规范 JSON 的服务。
func TestKibana_IdentifyHit(t *testing.T) {
	url := fakeserver.StartHTTP(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/status" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"name":"kibana","version":{"number":"8.10.0"}}`))
	}))
	host, port := hostPort(t, url)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	hit := New().Identify(ctx, host, port)
	if hit == nil {
		t.Fatal("expected kibana hit on /api/status responder")
	}
	if hit.Service != "kibana" {
		t.Errorf("Service = %q, want kibana", hit.Service)
	}
	if !strings.Contains(hit.Banner, "8.10.0") {
		t.Errorf("Banner = %q, want substring 8.10.0", hit.Banner)
	}
}

// TestKibana_CredentialHit is skipped: the kibana plugin's Credential
// method is a no-op stub that returns nil (Kibana's /api/status is
// unauthenticated by default and the v0.6.0 plan only covers the
// happy-path Identify branch for web plugins). / 跳过：kibana 的
// Credential 是返回 nil 的空 stub。
func TestKibana_CredentialHit(t *testing.T) {
	t.Skip("kibana.Credential is a no-op stub; nothing to cover")
}

// hostPort parses a "http://127.0.0.1:NNNN" URL into the
// (host, port) pair the Identify API wants. We avoid net/url so the
// test stays free of one-off parsing for a fixed-shape string.
// / hostPort 把 http URL 解析成 (host, port)，避免引 net/url。
func hostPort(t *testing.T, rawURL string) (string, int) {
	t.Helper()
	s := strings.TrimPrefix(rawURL, "http://")
	colon := strings.LastIndex(s, ":")
	if colon < 0 {
		t.Fatalf("no port in %q", rawURL)
	}
	port, err := strconv.Atoi(s[colon+1:])
	if err != nil {
		t.Fatalf("parse port from %q: %v", rawURL, err)
	}
	return s[:colon], port
}
