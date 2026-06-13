// winrm_test.go — unit test for the WinRM credential authenticator.
//
// Scope: empty creds, non-password method, conn refused. The full
// HTTP+SOAP WSMan handshake is exercised by the v0.1 end-to-end
// smoke against a real Windows host with WinRM enabled (obs #886);
// in-process SOAP mocking is more ceremony than coverage.
//
// winrm_test.go — WinRM 凭据认证器的单元测试。
// 范围：空 creds、非 password 方法、连接拒绝。完整 HTTP+SOAP WSMan
// 握手由 v0.1 端到端 smoke 对启用了 WinRM 的真 Windows 主机（obs #886）
// 测；进程内 mock SOAP 形式大于内容。
package protocols_test

import (
	"context"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/LCUstinian/FG-QiMen/core/cred"
	"github.com/LCUstinian/FG-QiMen/core/cred/protocols"
)

func TestWinRMAuthenticator_EmptyCreds(t *testing.T) {
	auth := protocols.NewWinRMAuthenticator()
	hit, err := auth.Authenticate(context.Background(), "127.0.0.1", 5985, nil, 1*time.Second)
	if err != nil {
		t.Fatalf("auth: %v", err)
	}
	if hit != nil {
		t.Errorf("expected nil, got %+v", hit)
	}
}

func TestWinRMAuthenticator_NonPasswordMethodSkipped(t *testing.T) {
	auth := protocols.NewWinRMAuthenticator()
	creds := []cred.Cred{{User: "Administrator", Pass: "secret", Method: "key"}}
	hit, err := auth.Authenticate(context.Background(), "127.0.0.1", 5985, creds, 1*time.Second)
	if err != nil {
		t.Fatalf("auth: %v", err)
	}
	if hit != nil {
		t.Errorf("expected nil, got %+v", hit)
	}
}

func TestWinRMAuthenticator_ConnRefused(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	auth := protocols.NewWinRMAuthenticator()
	creds := []cred.Cred{{User: "Administrator", Pass: "secret"}}
	_, err = auth.Authenticate(context.Background(), "127.0.0.1", port, creds, 1*time.Second)
	if err == nil {
		t.Errorf("expected conn error, got nil")
	}
}

func TestWinRMAuthenticator_RightCred(t *testing.T) {
	// Server that accepts Basic auth for the right user/pass.
	// / 接受右 user/pass Basic auth 的服务器。
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/wsman" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		got, err := base64.StdEncoding.DecodeString(
			strings.TrimPrefix(r.Header.Get("Authorization"), "Basic "))
		if err != nil {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		user, pass, ok := strings.Cut(string(got), ":")
		if !ok || subtle.ConstantTimeCompare([]byte(user+":"+pass), []byte("Administrator:secret")) != 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("<s:Envelope/>"))
	}))
	t.Cleanup(srv.Close)
	host, port := splitTestHostPort(t, srv.URL)
	auth := protocols.NewWinRMAuthenticator()
	creds := []cred.Cred{{User: "Administrator", Pass: "secret"}}
	hit, err := auth.Authenticate(context.Background(), host, port, creds, 3*time.Second)
	if err != nil {
		t.Fatalf("auth: %v", err)
	}
	if hit == nil {
		t.Fatalf("expected hit, got nil")
	}
	if hit.Cred.User != "Administrator" || hit.Cred.Pass != "secret" {
		t.Errorf("hit.Cred = %+v, want Administrator/secret", hit.Cred)
	}
}

func TestWinRMAuthenticator_WrongCred(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)
	host, port := splitTestHostPort(t, srv.URL)
	auth := protocols.NewWinRMAuthenticator()
	creds := []cred.Cred{{User: "Administrator", Pass: "wrong"}}
	hit, err := auth.Authenticate(context.Background(), host, port, creds, 3*time.Second)
	if err != nil {
		t.Fatalf("auth: %v", err)
	}
	if hit != nil {
		t.Errorf("expected nil, got %+v", hit)
	}
}

func splitTestHostPort(t *testing.T, url string) (string, int) {
	return splitHostPortHelper(t, url)
}

// splitHostPortHelper extracts host and port from an http test
// server URL like "http://127.0.0.1:54321".
// splitHostPortHelper 从 http test server URL 抽 host 和 port。
func splitHostPortHelper(t *testing.T, url string) (string, int) {
	t.Helper()
	var host string
	var port int
	_, err := fmt.Sscanf(url, "http://%s", &host)
	if err != nil {
		t.Fatalf("parse url host: %v", err)
	}
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
