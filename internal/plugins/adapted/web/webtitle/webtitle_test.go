// webtitle_test.go — A.6 coverage baseline via fakeserver.StartHTTP.
// webtitle_test.go — 通过 fakeserver.StartHTTP 的 A.6 覆盖率基线。
package webtitle

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/fakeserver"
	"github.com/LCUstinian/FG-QiMen/internal/transport"
	"github.com/LCUstinian/FG-QiMen/internal/types"
)

// cannedResponse replies to / with the minimum identifying response
// the webtitle Identify probe needs to declare a hit: a 200 OK
// carrying an HTML body with a <title> tag. /favicon.ico gets a
// short 200 so fetchFaviconHash takes its happy path; a 404 would
// also be tolerated (fetchFaviconHash returns nil on error).
//
// cannedResponse 对 / 返回 webtitle Identify 探针宣告命中的最小响
// 应：200 OK + 带 <title> 标签的 HTML。/favicon.ico 给一个短
// 200 让 fetchFaviconHash 走快乐路径；404 也行（fetchFaviconHash
// 错误时返 nil）。
func cannedResponse(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/favicon.ico":
		w.Header().Set("Content-Type", "image/x-icon")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("FAKEICO"))
	default:
		w.Header().Set("Server", "nginx/1.25.4")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`<!doctype html><html><head><title>Test Page</title></head><body>hi</body></html>`))
	}
}

// TestWebtitle_IdentifyHit spins up an httptest server via
// fakeserver.StartHTTP, parses host:port, calls Identify, and
// asserts the plugin returned a non-nil Result. /
// TestWebtitle_IdentifyHit 通过 fakeserver.StartHTTP 起一个 httptest
// 服务器，解析 host:port，调用 Identify 并断言插件返回非 nil
// 结果。
func TestWebtitle_IdentifyHit(t *testing.T) {
	url := fakeserver.StartHTTP(t, http.HandlerFunc(cannedResponse))
	host, port := splitHostPort(url)
	p := NewWebTitlePlugin()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	r := p.Identify(ctx, host, port)
	if r == nil {
		t.Fatal("expected webtitle hit, got nil")
	}
	if r.Service != "http" {
		t.Errorf("Service = %q, want %q", r.Service, "http")
	}
	// Extra must carry the structured payload; over plain http the
	// TLS fields stay empty. / Extra 必须带结构化 payload；纯 http
	// 下 TLS 字段保持为空。
	webFP, ok := r.Extra.(*types.WebFingerprint)
	if !ok {
		t.Fatalf("Extra type = %T, want *types.WebFingerprint", r.Extra)
	}
	if webFP.StatusCode != http.StatusOK {
		t.Errorf("webFP.StatusCode = %d, want %d", webFP.StatusCode, http.StatusOK)
	}
	if webFP.Title != "Test Page" {
		t.Errorf("webFP.Title = %q, want %q", webFP.Title, "Test Page")
	}
	if webFP.Server != "nginx/1.25.4" {
		t.Errorf("webFP.Server = %q, want %q", webFP.Server, "nginx/1.25.4")
	}
	if webFP.Scheme != "http" {
		t.Errorf("webFP.Scheme = %q, want %q", webFP.Scheme, "http")
	}
	if webFP.CertSubject != "" || webFP.TLSVersion != "" {
		t.Errorf("http target must leave TLS fields empty, got subject=%q tls=%q",
			webFP.CertSubject, webFP.TLSVersion)
	}
	// Note: coverage of webtitle's title-extraction / fingerprint
	// branches tends to land in the 60-70% band because a single
	// happy-path handler only exercises one title regex branch and
	// the favicon-mmh3 path; the v0.6.0 plan documents this as
	// "expected for identify-only HTTP plugins with deep branch
	// trees". We still get a useful regression net from this test:
	// any future change that breaks the HTTP GET / title extraction
	// / banner build pipeline will flip Identify to return nil.
	//
	// 注：webtitle 的标题抽取 / 指纹分支覆盖率往往落在 60-70%
	// 区间，因为单一快乐路径处理器只覆盖一个标题正则分支和
	// favicon-mmh3 路径；v0.6.0 计划里把这记作"识别型 HTTP 插件
	// 的预期"。但本测试仍有价值：任何破坏 HTTP GET / 标题抽取
	// / banner 构造管道的改动都会让 Identify 返回 nil。
}

// TestWebtitle_IdentifyTLS drives Identify against an httptest TLS
// server and asserts the payload carries the leaf-certificate
// identity harvested from resp.TLS. / TestWebtitle_IdentifyTLS 用
// httptest TLS 服务器跑 Identify，断言 payload 带上从 resp.TLS
// 收割的叶子证书身份。
func TestWebtitle_IdentifyTLS(t *testing.T) {
	// httptest's cert is self-signed; the transport verifies by
	// default (project design — operators opt in via --insecure-tls),
	// so flip the gate for this test and restore it after.
	// / httptest 证书是自签的；transport 默认验证（项目设计——
	// 操作员经 --insecure-tls 主动开启），所以本测试打开该门，
	// 结束后恢复。
	transport.InsecureTLS.Store(true)
	defer transport.InsecureTLS.Store(false)
	srv := httptest.NewTLSServer(http.HandlerFunc(cannedResponse))
	defer srv.Close()
	raw := strings.TrimPrefix(srv.URL, "https://")
	host, portStr, _ := strings.Cut(raw, ":")
	port, _ := strconv.Atoi(portStr)

	p := NewWebTitlePlugin()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	r := p.Identify(ctx, host, port)
	if r == nil {
		t.Fatal("expected webtitle https hit, got nil")
	}
	webFP, ok := r.Extra.(*types.WebFingerprint)
	if !ok {
		t.Fatalf("Extra type = %T, want *types.WebFingerprint", r.Extra)
	}
	if webFP.Scheme != "https" {
		t.Errorf("webFP.Scheme = %q, want https", webFP.Scheme)
	}
	if webFP.CertSubject == "" || webFP.CertIssuer == "" || webFP.TLSVersion == "" {
		t.Errorf("https target must fill TLS identity, got subject=%q issuer=%q tls=%q",
			webFP.CertSubject, webFP.CertIssuer, webFP.TLSVersion)
	}
}

// TestWebtitle_CredentialHit is skipped: webtitle.Credential is a
// no-op stub (always returns nil) per webtitle.go:79-81 — v0.1
// webtitle is identify-only. / TestWebtitle_CredentialHit 跳过：
// webtitle.Credential 是空 stub（始终返回 nil），见
// webtitle.go:79-81，v0.1 webtitle 仅识别。
func TestWebtitle_CredentialHit(t *testing.T) {
	t.Skip("webtitle.Credential is a no-op stub; see webtitle.go Credential()")
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
