// docker_test.go — unit test for the Docker API authenticator.
// / Docker API 认证器的单元测试。
package protocols_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/LCUstinian/FG-QiMen/core/cred"
	"github.com/LCUstinian/FG-QiMen/core/cred/protocols"
)

// fakeDockerServer returns an httptest.Server that mimics Docker
// daemon /_ping (no auth: 200 OK) and /info (no auth: 200 OK with
// {"ApiVersion":"1.43"}); with X-Registry-Auth for the right user,
// it returns 200; otherwise 401. / fakeDockerServer 返一个 httptest
// server，模拟 Docker daemon：/_ping 无 auth 返 200；/info 无 auth 返
// 200 + {"ApiVersion":"1.43"}；带 X-Registry-Auth 是对的用户返 200
// 否则 401。
func fakeDockerServer(t *testing.T, acceptUser, acceptPass string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// /_ping and /info are always no-auth. / /_ping 和 /info 总是
		// 无 auth。
		if r.URL.Path == "/_ping" || r.URL.Path == "/info" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			if r.URL.Path == "/info" {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"Containers": 0, "Images": 0, "Driver": "overlay2",
					"APIVersion": "1.43", "Version": "20.10.21",
				})
			}
			return
		}
		// Other endpoints require auth (we treat as ImagePull-like).
		// / 其他端点需要 auth（视为类 ImagePull）。
		user, pass, ok := r.BasicAuth()
		if !ok || user != acceptUser || pass != acceptPass {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestDockerAuthenticator_RightCred(t *testing.T) {
	srv := fakeDockerServer(t, "alice", "secret")
	host, port := splitTestHostPort(t, srv.URL)
	auth := protocols.NewDockerAuthenticator()
	creds := []cred.Cred{{User: "alice", Pass: "secret"}}
	hit, err := auth.Authenticate(context.Background(), host, port, creds, 3*time.Second)
	if err != nil {
		t.Fatalf("auth: %v", err)
	}
	if hit == nil {
		t.Fatalf("expected hit, got nil")
	}
	if hit.Cred.User != "alice" {
		t.Errorf("hit.Cred.User = %q, want alice", hit.Cred.User)
	}
}

func TestDockerAuthenticator_WrongCred(t *testing.T) {
	srv := fakeDockerServer(t, "alice", "secret")
	host, port := splitTestHostPort(t, srv.URL)
	auth := protocols.NewDockerAuthenticator()
	creds := []cred.Cred{{User: "alice", Pass: "wrong"}}
	hit, _ := auth.Authenticate(context.Background(), host, port, creds, 3*time.Second)
	if hit != nil {
		t.Errorf("expected nil, got %+v", hit)
	}
}

func TestDockerAuthenticator_EmptyCreds(t *testing.T) {
	auth := protocols.NewDockerAuthenticator()
	hit, _ := auth.Authenticate(context.Background(), "127.0.0.1", 2375, nil, 1*time.Second)
	if hit != nil {
		t.Errorf("expected nil, got %+v", hit)
	}
}

func TestDockerAuthenticator_ConnRefused(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	auth := protocols.NewDockerAuthenticator()
	creds := []cred.Cred{{User: "alice", Pass: "secret"}}
	_, err := auth.Authenticate(context.Background(), "127.0.0.1", port, creds, 1*time.Second)
	if err == nil {
		t.Errorf("expected conn error, got nil")
	}
}

// Keep fmt import alive for future debug. / fmt 保留。
var _ = fmt.Sprintf
