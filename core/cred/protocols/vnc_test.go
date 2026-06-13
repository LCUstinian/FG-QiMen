// vnc_test.go — unit test for the VNC credential authenticator.
//
// Scope: empty-creds, non-password-method, conn-refused. The actual
// RFB handshake (with go-vnc library) is exercised by the v0.1 end-to-
// end smoke in test/ (obs #886); mocking DES challenge encryption
// in-process would be 50+ lines for no real coverage gain — go-vnc
// is the protocol authority.
//
// vnc_test.go — VNC 凭据认证器的单元测试。
// 范围：空 creds、非 password 方法、连接拒绝。真实 RFB 握手
// （用 go-vnc 库）由 v0.1 端到端 smoke（obs #886）测；进程内 mock
// DES challenge 加密要 50+ 行但对 go-vnc 自身没增加覆盖。
package protocols_test

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/LCUstinian/FG-QiMen/core/cred"
	"github.com/LCUstinian/FG-QiMen/core/cred/protocols"
)

func TestVNCAuthenticator_EmptyCreds(t *testing.T) {
	auth := protocols.NewVNCAuthenticator()
	hit, err := auth.Authenticate(context.Background(), "127.0.0.1", 5900, nil, 1*time.Second)
	if err != nil {
		t.Fatalf("auth: %v", err)
	}
	if hit != nil {
		t.Errorf("expected nil, got %+v", hit)
	}
}

func TestVNCAuthenticator_NonPasswordMethodSkipped(t *testing.T) {
	auth := protocols.NewVNCAuthenticator()
	creds := []cred.Cred{{User: "any", Pass: "secret", Method: "key"}}
	hit, err := auth.Authenticate(context.Background(), "127.0.0.1", 5900, creds, 1*time.Second)
	if err != nil {
		t.Fatalf("auth: %v", err)
	}
	if hit != nil {
		t.Errorf("expected nil, got %+v", hit)
	}
}

func TestVNCAuthenticator_ConnRefused(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	auth := protocols.NewVNCAuthenticator()
	creds := []cred.Cred{{User: "", Pass: "password"}}
	_, err = auth.Authenticate(context.Background(), "127.0.0.1", port, creds, 1*time.Second)
	if err == nil {
		t.Errorf("expected conn error, got nil")
	}
}
