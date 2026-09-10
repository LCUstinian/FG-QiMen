// azure_test.go — fake-server test for the azure Identify plugin.
//
// azure_test.go — azure Identify 插件的假服务器测试。
//
// Note on the host guard: azure.go:62-67 short-circuits Identify when
// host != "169.254.169.254" — by design, the plugin refuses to probe
// arbitrary hosts (link-local IMDS is a security boundary, not a
// service exposed to the open internet). That guard makes the
// happy-path of Identify untestable against an httptest server bound
// to 127.0.0.1. We split coverage across two test functions:
//  1. TestAzure_IdentifyRejectsNonIMDSHost exercises the guard by
//     calling Identify with a non-IMDS host and expecting nil.
//  2. TestAzure_ProbeIMDSHit calls the unexported probeIMDS directly
//     (same package) so we can target the local fakeserver and still
//     drive the real JSON-decoding / hit-declaration logic.
//
// / azure.go:62-67 在 host != "169.254.169.254" 时短路 Identify —
// 这是设计：插件拒绝探测任意主机。链接本地 IMDS 是安全边界，不是
// 暴露在公网的服务。该守卫使 Identify 的命中路径无法对绑在
// 127.0.0.1 的 httptest 服务器做测试。我们把覆盖率拆到两个测试
// 函数里。
package azure

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/fakeserver"
)

// cannedIMDSResponse replies to GET /metadata/instance with the
// minimum identifying body the azure probeIMDS needs to declare a
// hit: a 200 OK carrying JSON whose compute.vmSize is non-empty.
// / cannedIMDSResponse 对 GET /metadata/instance 回复让 azure
// probeIMDS 宣告命中的最小响应体：compute.vmSize 非空的 200 OK
// JSON。
func cannedIMDSResponse(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/metadata/instance" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{
		"compute": {
			"vmSize": "Standard_D2s_v5",
			"location": "eastus",
			"osProfile": {"computerName": "ignored-for-HARD-rule"}
		}
	}`))
}

// TestAzure_ProbeIMDSHit drives the real probeIMDS against a local
// fakeserver, since the IMDS host guard in Identify prevents using
// 127.0.0.1 as a target. We bypass the guard by calling probeIMDS
// (same package access) and assert the hit is declared with the
// expected Service / Banner.
// / 由于 Identify 中的 IMDS 主机守卫，不允许用 127.0.0.1 做目标。
// 我们用同包访问绕过守卫直接调 probeIMDS，断言命中并验证 Service
// / Banner。
func TestAzure_ProbeIMDSHit(t *testing.T) {
	url := fakeserver.StartHTTP(t, http.HandlerFunc(cannedIMDSResponse))
	host, port := splitHostPort(url)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	r := probeIMDS(ctx, host, port)
	if r == nil {
		t.Fatal("expected azure IMDS hit on /metadata/instance responder, got nil")
	}
	if r.Service != "azure-imds" {
		t.Errorf("Service = %q, want %q", r.Service, "azure-imds")
	}
	if !strings.Contains(r.Banner, "Standard_D2s_v5") {
		t.Errorf("Banner = %q, want substring Standard_D2s_v5", r.Banner)
	}
	if !strings.Contains(r.Banner, "eastus") {
		t.Errorf("Banner = %q, want substring eastus", r.Banner)
	}
}

// TestAzure_ProbeIMDSRejectsEmptyVMSize asserts the probe rejects an
// IMDS-style JSON body whose compute.vmSize is empty (the check at
// azure.go:108-110). / 断言 probe 在 compute.vmSize 为空时拒绝命中。
func TestAzure_ProbeIMDSRejectsEmptyVMSize(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"compute": {"vmSize": "", "location": "eastus"}}`))
	}))
	t.Cleanup(srv.Close)

	host, port := splitHostPort(srv.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if r := probeIMDS(ctx, host, port); r != nil {
		t.Errorf("expected nil for empty vmSize, got %+v", r)
	}
}

// TestAzure_IdentifyRejectsNonIMDSHost exercises the host guard at
// azure.go:63-65: Identify must return nil for any host that isn't
// the well-known IMDS link-local IP. / 跑 azure.go:63-65 的主机
// 守卫。
func TestAzure_IdentifyRejectsNonIMDSHost(t *testing.T) {
	url := fakeserver.StartHTTP(t, http.HandlerFunc(cannedIMDSResponse))
	host, port := splitHostPort(url)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	p := New()
	if r := p.Identify(ctx, host, port); r != nil {
		t.Errorf("expected nil for non-IMDS host %q, got %+v", host, r)
	}
}

// TestAzure_CredentialHit is skipped: the azure plugin's Credential
// is a no-op stub (always returns nil) per azure.go:54-57.
// / TestAzure_CredentialHit 跳过：azure 插件的 Credential 是空
// stub（始终返回 nil），见 azure.go:54-57。
func TestAzure_CredentialHit(t *testing.T) {
	t.Skip("azure.Credential is a no-op stub; see azure.go Credential()")
}

// splitHostPort strips the "http://" scheme from an httptest URL and
// returns the host and numeric port. / splitHostPort 去掉 httptest
// URL 的 "http://" 头，返回 host 与数字端口。
func splitHostPort(rawURL string) (string, int) {
	s := strings.TrimPrefix(rawURL, "http://")
	host, portStr, _ := strings.Cut(s, ":")
	port, _ := strconv.Atoi(portStr)
	return host, port
}
