// jenkins_test.go — A.6 coverage baseline via fakeserver.StartHTTP.
// jenkins_test.go — 通过 fakeserver.StartHTTP 的 A.6 覆盖率基线。
package jenkins

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/fakeserver"
)

// cannedResponse replies to /api/json with the minimum response the
// Jenkins Identify probe needs to declare a hit: a 200 OK carrying the
// X-Jenkins response header. / cannedResponse 对 /api/json 回复让
// Jenkins Identify 探针宣告命中的最小响应：带 X-Jenkins 响应头的
// 200 OK。
func cannedResponse(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/api/json" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("X-Jenkins", "2.452.3")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"mode":"NORMAL"}`))
}

func TestJenkins_IdentifyHit(t *testing.T) {
	url := fakeserver.StartHTTP(t, http.HandlerFunc(cannedResponse))
	host, port := splitHostPort(url)
	p := New()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	r := p.Identify(ctx, host, port)
	if r == nil {
		t.Fatal("expected jenkins hit, got nil")
	}
	if r.Service != "jenkins" {
		t.Errorf("Service = %q, want %q", r.Service, "jenkins")
	}
}

// TestJenkins_CredentialHit is skipped: the Jenkins plugin's Credential
// is a no-op stub (always returns nil) per jenkins.go:37-39.
// / TestJenkins_CredentialHit 跳过：jenkins 插件的 Credential 是
// 空 stub（始终返回 nil），见 jenkins.go:37-39。
func TestJenkins_CredentialHit(t *testing.T) {
	t.Skip("jenkins.Credential is a no-op stub; see jenkins.go Credential()")
}

// splitHostPort strips the "http://" scheme from an httptest URL and
// returns the host and numeric port. / splitHostPort 去掉 httptest URL
// 的 "http://" 头，返回 host 与数字端口。
func splitHostPort(rawURL string) (string, int) {
	s := strings.TrimPrefix(rawURL, "http://")
	host, portStr, _ := strings.Cut(s, ":")
	port, _ := strconv.Atoi(portStr)
	return host, port
}
