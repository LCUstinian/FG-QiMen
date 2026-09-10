// winrm_test.go — fake-server tests for the winrm Identify plugin.
//
// Pattern: fakeserver.StartHTTP binds 127.0.0.1:0, the handler
// responds to GET /wsman with one of the three status codes that
// the plugin treats as a WinRM listener (200, 401, 405), and the
// plugin's Identify returns a non-nil Result with Service="winrm".
//
// / winrm_test.go — winrm Identify 插件的 fake-server 测试。
// fakeserver.StartHTTP 在 127.0.0.1:0 起 HTTP 监听；handler 对
// GET /wsman 回三种 WinRM 监听器状态码之一（200 / 401 / 405）；
// plugin Identify 返非 nil Result，Service="winrm"。
//
// The winrm plugin is HTTP-based (not raw TCP), so the natural
// fakeserver helper is StartHTTP — the same t.Cleanup contract as
// ListenLoop. The handler is a plain http.HandlerFunc; the plugin's
// http.Client reads the response, sees a 200/401/405, and returns a
// Result. fakeserver.StartHTTP returns the full URL
// ("http://127.0.0.1:<port>") which we parse for host+port.
//
// / winrm 插件是 HTTP 协议（不是裸 TCP），所以自然的 fakeserver 助
// 手是 StartHTTP——同样的 t.Cleanup 契约。handler 是普通
// http.HandlerFunc；plugin 的 http.Client 读到响应，看到
// 200/401/405，返 Result。fakeserver.StartHTTP 返回完整 URL
// （"http://127.0.0.1:<port>"），我们解析出 host+port。
//
// Per v0.6.0 fake-server plan: this is an HTTP plugin (not Tier 3
// stateful TCP). Credential() is a documented no-op stub
// (winrm.go:51-53); WinRM credential testing lives in
// core/cred/protocols/winrm.go (WinRMAuthenticator via HTTP Basic).
// We cover all three hit branches (200 / 401 / 405) plus a miss
// branch (500) for solid coverage of the identify switch.
//
// / 按 v0.6.0 假服务器计划：这是 HTTP 插件（不是 Tier 3 有状态
// TCP）。Credential() 是文档化的 no-op stub（winrm.go:51-53）；
// WinRM 凭据测试在 core/cred/protocols/winrm.go
// （WinRMAuthenticator via HTTP Basic）。我们覆盖三个 hit 分支
// （200 / 401 / 405）和一个 miss 分支（500），以稳覆盖 identify
// switch。
package winrm

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"

	"github.com/LCUstinian/FG-QiMen/internal/fakeserver"
)

// winrmHandler returns an http.HandlerFunc that responds to
// GET /wsman with the given status code (and an empty body). The
// winrm plugin's Identify reads resp.StatusCode only — body
// content is irrelevant. We register a wildcard fallback so any
// path on the test server that isn't /wsman still returns 404
// (a non-hit status), keeping miss-branch coverage honest.
//
// / winrmHandler 返一个对 GET /wsman 回指定状态码（和空 body）的
// http.HandlerFunc。winrm plugin Identify 只读 resp.StatusCode —
// body 内容无关紧要。我们注册一个 wildcard fallback，让测试服务
// 器上任何非 /wsman 路径也返 404（一个非 hit 状态），保证
// miss-branch 覆盖诚实。
func winrmHandler(code int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/wsman" {
			w.WriteHeader(code)
			return
		}
		// Anything else is a miss — return 404 so the plugin
		// cleanly returns nil from Identify.
		// / 其他都是 miss — 返 404，plugin 干净返 nil。
		w.WriteHeader(http.StatusNotFound)
	})
}

// hostPortFromURL extracts ("127.0.0.1", port) from the URL
// returned by fakeserver.StartHTTP ("http://127.0.0.1:<port>").
// The plugin's Identify takes host/port as separate args and
// internally joins them via net.JoinHostPort.
//
// / hostPortFromURL 从 fakeserver.StartHTTP 返回的 URL
// （"http://127.0.0.1:<port>"）中提取 ("127.0.0.1", port)。plugin
// Identify 把 host/port 作为独立参数，内部用 net.JoinHostPort 拼
// 接。
func hostPortFromURL(t *testing.T, rawURL string) (string, int) {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("fakeserver.StartHTTP returned unparseable URL %q: %v", rawURL, err)
	}
	host := u.Hostname()
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatalf("fakeserver.StartHTTP URL %q has unparseable port: %v", rawURL, err)
	}
	return host, port
}

// startWinrmServer is a thin wrapper around fakeserver.StartHTTP
// that returns host/port instead of a URL. Keeps the test bodies
// focused on plugin assertions, not URL plumbing.
//
// / startWinrmServer 是 fakeserver.StartHTTP 的薄包装，返
// host/port 而不是 URL。让测试 body 聚焦于 plugin 断言，而不是
// URL plumbing。
func startWinrmServer(t *testing.T, code int) (string, int) {
	t.Helper()
	u := fakeserver.StartHTTP(t, winrmHandler(code))
	return hostPortFromURL(t, u)
}

// TestWinrm_IdentifyHit_OK covers the 200 branch: server replies
// with 200 to GET /wsman (auth not required). Plugin must return
// a non-nil Result with Service="winrm" and Banner containing the
// scheme ("http").
//
// / TestWinrm_IdentifyHit_OK 覆盖 200 分支：server 对 GET /wsman
// 返 200（无需认证）。plugin 必须返非 nil Result，Service="winrm"，
// Banner 含 scheme（"http"）。
func TestWinrm_IdentifyHit_OK(t *testing.T) {
	host, port := startWinrmServer(t, http.StatusOK)

	p := New()
	res := p.Identify(context.Background(), host, port)
	if res == nil {
		t.Fatal("Identify returned nil; expected winrm hit on 200")
	}
	if res.Service != "winrm" {
		t.Errorf("Service = %q, want %q", res.Service, "winrm")
	}
	if res.Host != host || res.Port != port {
		t.Errorf("Host/Port = %q/%d, want %q/%d", res.Host, res.Port, host, port)
	}
	if res.Banner == "" {
		t.Error("Banner is empty")
	}
}

// TestWinrm_IdentifyHit_Unauthorized covers the 401 branch
// (auth required — most common real-world WinRM posture). Plugin
// must still return a hit.
//
// / TestWinrm_IdentifyHit_Unauthorized 覆盖 401 分支（需认证——
// 最常见的真实 WinRM 状态）。plugin 必须仍返 hit。
func TestWinrm_IdentifyHit_Unauthorized(t *testing.T) {
	host, port := startWinrmServer(t, http.StatusUnauthorized)

	p := New()
	res := p.Identify(context.Background(), host, port)
	if res == nil {
		t.Fatal("Identify returned nil; expected winrm hit on 401")
	}
	if res.Service != "winrm" {
		t.Errorf("Service = %q, want %q", res.Service, "winrm")
	}
}

// TestWinrm_IdentifyHit_MethodNotAllowed covers the 405 branch
// (method not allowed but endpoint exists). Plugin must still
// return a hit — this is how WinRM listeners respond to GET on
// /wsman when they only accept POST.
//
// / TestWinrm_IdentifyHit_MethodNotAllowed 覆盖 405 分支（方法
// 不允许但端点存在）。plugin 必须仍返 hit — WinRM 监听器对
// /wsman 的 GET 就是这么回应的（它们只接受 POST）。
func TestWinrm_IdentifyHit_MethodNotAllowed(t *testing.T) {
	host, port := startWinrmServer(t, http.StatusMethodNotAllowed)

	p := New()
	res := p.Identify(context.Background(), host, port)
	if res == nil {
		t.Fatal("Identify returned nil; expected winrm hit on 405")
	}
	if res.Service != "winrm" {
		t.Errorf("Service = %q, want %q", res.Service, "winrm")
	}
}

// TestWinrm_IdentifyMiss covers the negative branch: server
// returns 500 to GET /wsman. The plugin's switch matches 200/401/
// 405 only; 500 (and anything else) must yield nil.
//
// / TestWinrm_IdentifyMiss 覆盖反向分支：server 对 GET /wsman 返
// 500。plugin 的 switch 只匹配 200/401/405；500（和其他任何状态）
// 必须返 nil。
func TestWinrm_IdentifyMiss(t *testing.T) {
	host, port := startWinrmServer(t, http.StatusInternalServerError)

	if res := New().Identify(context.Background(), host, port); res != nil {
		t.Errorf("Identify = %+v, want nil on non-WinRM status", res)
	}
}

// TestWinrm_IdentifyDialFailure exercises the dial-error branch:
// the plugin's http.Client fails to connect. We point it at an
// unroutable RFC 5737 TEST-NET-1 address so the call fails fast
// on both http and https schemes without blocking past the
// caller's context deadline.
//
// / TestWinrm_IdentifyDialFailure 跑 Identify 的拨号失败分支：
// plugin 的 http.Client 连不上。我们指向一个不可达的 RFC 5737
// TEST-NET-1 地址，让 http / https 双 scheme 都快速失败，不超
// 过 caller 的 context deadline。
func TestWinrm_IdentifyDialFailure(t *testing.T) {
	// Use httptest.NewServer + .Close() trick to get a port
	// nobody is listening on. / 用 httptest.NewServer + .Close()
	// 拿到一个没人监听的 port。
	closed := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	closed.Close()
	_, port, err := net.SplitHostPort(closed.Listener.Addr().String())
	if err != nil {
		t.Fatalf("parse closed server addr: %v", err)
	}
	portNum, _ := strconv.Atoi(port)

	if res := New().Identify(context.Background(), "127.0.0.1", portNum); res != nil {
		t.Errorf("Identify on closed port = %+v, want nil", res)
	}
}

// TestWinrm_CredentialNoOp documents that winrm.Credential is a
// documented no-op stub (see winrm.go:51-53). WinRM credential
// testing lives in internal/core/cred/protocols/winrm.go
// (WinRMAuthenticator via HTTP Basic auth probe). Per the v0.6.0
// fake-server plan we still wire a placeholder so a future
// contributor who promotes the stub can drop in a WinRM SOAP
// POST + HTTP Basic round-trip test against fakeserver.StartHTTP.
//
// / TestWinrm_CredentialNoOp 标注 winrm.Credential 是文档化的
// no-op stub（见 winrm.go:51-53）。WinRM 凭据测试在
// internal/core/cred/protocols/winrm.go（WinRMAuthenticator via
// HTTP Basic 探针）。按 v0.6.0 假服务器计划仍保留占位，方便未
// 来贡献者将 stub 升级时直接补上 WinRM SOAP POST + HTTP Basic
// 往返测试（基于 fakeserver.StartHTTP）。
func TestWinrm_CredentialNoOp(t *testing.T) {
	t.Skip("winrm.Credential is a documented no-op stub (winrm.go:51-53); " +
		"WinRM credential testing lives in internal/core/cred/protocols/winrm.go " +
		"(WinRMAuthenticator via HTTP Basic).")
}
