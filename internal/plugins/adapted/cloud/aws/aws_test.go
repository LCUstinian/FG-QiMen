// aws_test.go — fake-server test for the aws Identify plugin.
//
// aws_test.go — aws Identify 插件的假服务器测试。
//
// Note on the host guard: aws.go:55-58 short-circuits Identify when
// host != "169.254.169.254" — by design, the plugin refuses to probe
// arbitrary hosts (link-local IMDS is a security boundary, not a
// service exposed to the open internet). That guard makes the
// happy-path of Identify untestable against an httptest server bound
// to 127.0.0.1. We split coverage across multiple test functions:
//  1. TestAWS_ProbeAWSHit drives the real probeAWS against a local
//     fakeserver that returns the canonical IMDSv1 path list
//     (containing "instance-id") and asserts a hit.
//  2. TestAWS_ProbeAWSRejectsStatusNot200 asserts the probe rejects
//     a non-200 response.
//  3. TestAWS_ProbeAWSRejectsMissingInstanceID asserts the probe
//     rejects a 200 body that lacks the "instance-id" marker.
//  4. TestAWS_IdentifyRejectsNonIMDSHost exercises the host guard.
//
// / aws.go:55-58 在 host != "169.254.169.254" 时短路 Identify —
// 这是设计：插件拒绝探测任意主机。链接本地 IMDS 是安全边界，不是
// 暴露在公网的服务。该守卫使 Identify 的命中路径无法对绑在
// 127.0.0.1 的 httptest 服务器做测试。我们把覆盖率拆到多个测试
// 函数里。
package aws

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

// cannedIMDSResponse replies to GET /latest/meta-data/ with the
// minimum body the aws probeAWS needs to declare a hit: a 200 OK
// whose payload is a newline-separated list of IMDSv1 metadata
// paths that includes the "instance-id" marker (the anchor at
// aws.go:78). / cannedIMDSResponse 对 GET /latest/meta-data/ 回复
// 让 aws probeAWS 宣告命中的最小响应体：含 "instance-id" 锚点的
// 换行分隔 IMDSv1 metadata 路径列表（aws.go:78 检查点）。
func cannedIMDSResponse(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/latest/meta-data/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ami-id\ninstance-id\nhostname\nlocal-ipv4\n"))
}

// TestAWS_ProbeAWSHit drives the real probeAWS against a local
// fakeserver, since the IMDS host guard in Identify prevents using
// 127.0.0.1 as a target. We bypass the guard by calling probeAWS
// (same package access) and assert the hit is declared with the
// expected Service / Banner. / 由于 Identify 中的 IMDS 主机守卫，
// 不允许用 127.0.0.1 做目标。我们用同包访问绕过守卫直接调 probeAWS，
// 断言命中并验证 Service / Banner。
func TestAWS_ProbeAWSHit(t *testing.T) {
	url := fakeserver.StartHTTP(t, http.HandlerFunc(cannedIMDSResponse))
	host, port := splitHostPort(url)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	r := probeAWS(ctx, host, port)
	if r == nil {
		t.Fatal("expected aws IMDS hit on /latest/meta-data/ responder, got nil")
	}
	if r.Service != "aws-imds" {
		t.Errorf("Service = %q, want %q", r.Service, "aws-imds")
	}
	if !strings.Contains(r.Banner, "paths=") {
		t.Errorf("Banner = %q, want substring paths=", r.Banner)
	}
}

// TestAWS_ProbeAWSRejectsStatusNot200 asserts the probe rejects a
// non-200 response (the check at aws.go:73-75). / 断言 probe 在状态
// 码非 200 时拒绝命中。
func TestAWS_ProbeAWSRejectsStatusNot200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	t.Cleanup(srv.Close)

	host, port := splitHostPort(srv.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if r := probeAWS(ctx, host, port); r != nil {
		t.Errorf("expected nil for status 403, got %+v", r)
	}
}

// TestAWS_ProbeAWSRejectsMissingInstanceID asserts the probe rejects
// a 200 body that lacks the "instance-id" marker (the check at
// aws.go:78-80). / 断言 probe 在 body 不含 "instance-id" 时拒绝命中。
func TestAWS_ProbeAWSRejectsMissingInstanceID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ami-id\nhostname\nlocal-ipv4\n"))
	}))
	t.Cleanup(srv.Close)

	host, port := splitHostPort(srv.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if r := probeAWS(ctx, host, port); r != nil {
		t.Errorf("expected nil for body without instance-id, got %+v", r)
	}
}

// TestAWS_IdentifyRejectsNonIMDSHost exercises the host guard at
// aws.go:55-58: Identify must return nil for any host that isn't
// the well-known IMDS link-local IP. / 跑 aws.go:55-58 的主机守卫。
func TestAWS_IdentifyRejectsNonIMDSHost(t *testing.T) {
	url := fakeserver.StartHTTP(t, http.HandlerFunc(cannedIMDSResponse))
	host, port := splitHostPort(url)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	p := New()
	if r := p.Identify(ctx, host, port); r != nil {
		t.Errorf("expected nil for non-IMDS host %q, got %+v", host, r)
	}
}

// TestAWS_CredentialHit is skipped: the aws plugin's Credential is a
// no-op stub (always returns nil) per aws.go:47-50. / TestAWS_CredentialHit
// 跳过：aws 插件的 Credential 是空 stub（始终返回 nil），见 aws.go:47-50。
func TestAWS_CredentialHit(t *testing.T) {
	t.Skip("aws.Credential is a no-op stub; see aws.go Credential()")
}

// splitHostPort strips the "http://" scheme from an httptest URL and
// returns the host and numeric port. / splitHostPort 去掉 httptest
// URL 的 "http://" 头，返回 host 与数字端口。
func splitHostPort(rawURL string) (string, int) {
	s := strings.TrimPrefix(rawURL, "http://")
	host, portStr, _ := strings.Cut(s, ":")
	port, err := strconv.Atoi(portStr)
	if err != nil {
		colon := strings.LastIndex(s, ":")
		if colon < 0 {
			panic("splitHostPort: no port in " + rawURL)
		}
		port, _ = strconv.Atoi(s[colon+1:])
	}
	return host, port
}
