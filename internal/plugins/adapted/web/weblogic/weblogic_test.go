// weblogic_test.go — A.6 coverage baseline via fakeserver.StartHTTP.
// weblogic_test.go — 通过 fakeserver.StartHTTP 的 A.6 覆盖率基线。
package weblogic

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/fakeserver"
)

// cannedResponse replies to /console with the minimum identifying body
// the WebLogic Identify probe needs to declare a hit: a 200 OK carrying
// HTML that contains the "WebLogic Server" footer marker (plus a
// version line that matches versionRe). / cannedResponse 对 /console
// 回复让 WebLogic Identify 探针宣告命中的最小响应体：含 "WebLogic
// Server" 页脚标志的 200 OK HTML（外加匹配 versionRe 的版本行）。
func cannedResponse(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/console" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`<html><body>Oracle WebLogic Server Administration Console<br>WebLogic Server Version: 12.2.1.4.0</body></html>`))
}

func TestWeblogic_IdentifyHit(t *testing.T) {
	url := fakeserver.StartHTTP(t, http.HandlerFunc(cannedResponse))
	host, port := splitHostPort(url)
	p := New()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	r := p.Identify(ctx, host, port)
	if r == nil {
		t.Fatal("expected weblogic hit, got nil")
	}
	if r.Service != "weblogic" {
		t.Errorf("Service = %q, want %q", r.Service, "weblogic")
	}
}

// TestWeblogic_CredentialHit is skipped: the WebLogic plugin's
// Credential is a no-op stub (always returns nil) per
// weblogic.go:42-44.
// / TestWeblogic_CredentialHit 跳过：weblogic 插件的 Credential 是
// 空 stub（始终返回 nil），见 weblogic.go:42-44。
func TestWeblogic_CredentialHit(t *testing.T) {
	t.Skip("weblogic.Credential is a no-op stub; see weblogic.go Credential()")
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
