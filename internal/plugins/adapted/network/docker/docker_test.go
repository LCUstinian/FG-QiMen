// docker_test.go — fake-server tests for the docker Identify plugin.
//
// Pattern: fakeserver.StartHTTP binds 127.0.0.1:0, the handler
// responds to GET /_ping with "OK" / 200 and GET /info with a
// JSON body containing the Version field, and the plugin's
// Identify returns a non-nil Result with Service="docker" and
// Banner="Docker <Version>".
//
// / docker_test.go — docker Identify 插件的 fake-server 测试。
// / fakeserver.StartHTTP 在 127.0.0.1:0 起 HTTP 监听；handler 对
// / GET /_ping 回 "OK" / 200，对 GET /info 回含 Version 字段的
// / JSON；plugin Identify 返非 nil Result，Service="docker"，
// / Banner="Docker <Version>"。
//
// Per v0.6.0 fake-server plan Tier 4: HTTP plugin, so the natural
// fakeserver helper is StartHTTP — same t.Cleanup contract as
// ListenLoop. fakeserver.StartHTTP returns the full URL
// ("http://127.0.0.1:<port>") which we parse for host+port.
// The plugin's Identify takes host/port separately and internally
// joins them via net.JoinHostPort.
//
// / 按 v0.6.0 假服务器计划 Tier 4：HTTP 插件，所以自然的
// / fakeserver 助手是 StartHTTP——同样的 t.Cleanup 契约。
// / fakeserver.StartHTTP 返回完整 URL（"http://127.0.0.1:<port>"），
// / 我们解析出 host+port。plugin Identify 把 host/port 作为独立参数，
// / 内部用 net.JoinHostPort 拼接。
package docker

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/fakeserver"
)

// cannedDockerResponse handles both Docker probe endpoints:
//   - GET /_ping  → "OK" / 200 (always no-auth on real daemons).
//   - GET /info   → JSON with Version + APIVersion so the plugin
//     can populate Banner as "Docker <Version>".
//
// / cannedDockerResponse 处理两个 Docker 探针端点：
// /   - GET /_ping  → "OK" / 200（真实 daemon 上始终无 auth）。
// /   - GET /info   → 含 Version + APIVersion 的 JSON，让 plugin
// /                   把 Banner 填为 "Docker <Version>"。
func cannedDockerResponse(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/_ping":
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	case "/info":
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"APIVersion":"1.43","Version":"20.10.21","Os":"linux","Arch":"x86_64"}`))
	default:
		http.NotFound(w, r)
	}
}

// TestDocker_IdentifyHit verifies the plugin identifies a Docker
// daemon replying "OK" to /_ping + a version-bearing JSON to /info,
// and reports Service="docker" with a Banner carrying the version.
// / TestDocker_IdentifyHit 验证插件识别一个对 /_ping 返 "OK"、对
// / /info 返带版本 JSON 的 Docker daemon，报告 Service="docker" 且
// / Banner 含版本。
func TestDocker_IdentifyHit(t *testing.T) {
	url := fakeserver.StartHTTP(t, http.HandlerFunc(cannedDockerResponse))
	host, port := splitHostPort(url)

	p := New()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	r := p.Identify(ctx, host, port)
	if r == nil {
		t.Fatal("expected docker hit, got nil")
	}
	if r.Service != "docker" {
		t.Errorf("Service = %q, want %q", r.Service, "docker")
	}
	if r.Banner != "Docker 20.10.21" {
		t.Errorf("Banner = %q, want %q", r.Banner, "Docker 20.10.21")
	}
}

// TestDocker_CredentialNoOp documents that docker.Credential is a
// no-op stub (always returns nil) per docker.go:44-47. Real Docker
// credential testing lives in
// internal/core/cred/protocols/docker.go (DockerAuthenticator via
// HTTP Basic auth probe to /images/json). Per the v0.6.0
// fake-server plan we still wire a placeholder so a future
// contributor who promotes the stub can drop in an HTTP Basic
// round-trip test against fakeserver.StartHTTP.
//
// / TestDocker_CredentialNoOp 标注 docker.Credential 是文档化的
// / no-op stub（始终返回 nil），见 docker.go:44-47。真实 Docker 凭
// / 据测试在 internal/core/cred/protocols/docker.go
// /（DockerAuthenticator via HTTP Basic 认证探测 /images/json）。按
// / v0.6.0 假服务器计划仍保留占位，方便未来贡献者将 stub 升级时直
// / 接补上 HTTP Basic 往返测试（基于 fakeserver.StartHTTP）。
func TestDocker_CredentialNoOp(t *testing.T) {
	t.Skip("docker.Credential is a documented no-op stub (docker.go:44-47); " +
		"Docker credential testing lives in internal/core/cred/protocols/docker.go " +
		"(DockerAuthenticator via HTTP Basic auth probe to /images/json)")
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
