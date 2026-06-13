// elasticsearch_test.go — unit test for the Elasticsearch credential
// authenticator.
//
// The mock server is an httptest.Server that mimics the bits of ES we
// care about: 200 + JSON body with "elasticsearch" / "cluster_name" /
// "lucene_version" for a hit, 401 for a miss, anything else for a miss.
//
// elasticsearch_test.go — Elasticsearch 凭据认证器的单元测试。
// Mock server 是 httptest.Server，模拟 ES 的相关部分：200 + JSON body
// 含 "elasticsearch" / "cluster_name" / "lucene_version" 视为命中，
// 401 视为不命中，其他视为不命中。
package database

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/core/credential"
)

// fakeESServer starts an httptest.Server that accepts basic auth.
//
// On /:
//   - Authorization: Basic <right> → 200 + ES-like JSON
//   - else → 401
//
// The "right" base64 is the rightUser:rightPass base64.
//
// fakeESServer 启一个 httptest.Server，接收 basic auth。
// / 路径：
//   - Authorization: Basic <right> → 200 + ES 风格 JSON
//   - 其他 → 401
// "right" 是 rightUser:rightPass 的 base64。
func fakeESServer(t *testing.T, rightUser, rightPass string) *httptest.Server {
	t.Helper()
	expected := "Basic " + base64.StdEncoding.EncodeToString([]byte(rightUser+":"+rightPass))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := r.Header.Get("Authorization")
		if got != expected {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"name":          "test-cluster",
			"cluster_name":  "elasticsearch",
			"version":       map[string]any{"number": "8.10.0", "lucene_version": "9.7.0"},
			"tagline":       "You Know, for Search",
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestElasticsearchAuthenticator_RightCred(t *testing.T) {
	srv := fakeESServer(t, "elastic", "changeme")
	host, port := splitHostPort(t, srv.URL)
	auth := NewElasticsearchAuthenticator()
	creds := []credential.Cred{{User: "elastic", Pass: "changeme"}}
	hit, err := auth.Authenticate(context.Background(), host, port, creds, 3*time.Second)
	if err != nil {
		t.Fatalf("auth: %v", err)
	}
	if hit == nil {
		t.Fatalf("expected hit, got nil")
	}
	if hit.Cred.User != "elastic" || hit.Cred.Pass != "changeme" {
		t.Errorf("hit.Cred = %+v, want elastic/changeme", hit.Cred)
	}
}

func TestElasticsearchAuthenticator_WrongCred(t *testing.T) {
	srv := fakeESServer(t, "elastic", "changeme")
	host, port := splitHostPort(t, srv.URL)
	auth := NewElasticsearchAuthenticator()
	// Pure miss list — no cred that matches the server's right cred. / 纯错
	// 凭据列表——不包含服务器 right cred。
	credsMiss := []credential.Cred{{User: "elastic", Pass: "wrong"}}
	hit, err := auth.Authenticate(context.Background(), host, port, credsMiss, 3*time.Second)
	if err != nil {
		t.Fatalf("auth: %v", err)
	}
	if hit != nil {
		t.Errorf("expected nil, got %+v", hit)
	}
}

func TestElasticsearchAuthenticator_EmptyCreds(t *testing.T) {
	auth := NewElasticsearchAuthenticator()
	hit, err := auth.Authenticate(context.Background(), "127.0.0.1", 9200, nil, 1*time.Second)
	if err != nil {
		t.Fatalf("auth: %v", err)
	}
	if hit != nil {
		t.Errorf("expected nil, got %+v", hit)
	}
}

func TestElasticsearchAuthenticator_NonESBody(t *testing.T) {
	// Server returns 200 but body is not ES. / 服务器返 200 但 body 不是 ES。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("hello world"))
	}))
	t.Cleanup(srv.Close)
	host, port := splitHostPort(t, srv.URL)
	auth := NewElasticsearchAuthenticator()
	creds := []credential.Cred{{User: "elastic", Pass: "changeme"}}
	hit, err := auth.Authenticate(context.Background(), host, port, creds, 3*time.Second)
	if err != nil {
		t.Fatalf("auth: %v", err)
	}
	if hit != nil {
		t.Errorf("expected nil (200 but not ES body), got %+v", hit)
	}
}

// splitHostPort extracts host and port from an http test server URL.
// splitHostPort 从 http test server URL 抽 host 和 port。
func splitHostPort(t *testing.T, url string) (string, int) {
	t.Helper()
	// url is like "http://127.0.0.1:54321"
	// / url 形如 "http://127.0.0.1:54321"
	var host string
	var port int
	_, err := fmt.Sscanf(url, "http://%s", &host)
	if err != nil {
		t.Fatalf("parse url host: %v", err)
	}
	// Find last ':' for port. / 找最后一个 ':' 拿 port。
	for i := len(host) - 1; i >= 0; i-- {
		if host[i] == ':' {
			_, err := fmt.Sscanf(host[i+1:], "%d", &port)
			if err != nil {
				t.Fatalf("parse port: %v", err)
			}
			host = host[:i]
			return host, port
		}
	}
	t.Fatalf("no port in %q", url)
	return "", 0
}
