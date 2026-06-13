// telnet_test.go — unit test for the Telnet credential authenticator.
//
// Scope: empty creds, non-password method, conn refused. The full
// IAC negotiation + prompt-send + response-parse is exercised by
// the v0.1 end-to-end smoke against a real telnetd (obs #886);
// in-process mocking a telnet server is more ceremony than
// coverage.
//
// telnet_test.go — Telnet 凭据认证器的单元测试。
// 范围：空 creds、非 password 方法、连接拒绝。完整 IAC 协商 + 提示符
// 发送 + 响应解析由 v0.1 端到端 smoke 对真 telnetd（obs #886）测；
// 进程内 mock telnet server 形式大于内容。
package protocols_test

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/core/cred"
	"github.com/LCUstinian/FG-QiMen/internal/core/cred/protocols"
)

func TestTelnetAuthenticator_EmptyCreds(t *testing.T) {
	auth := protocols.NewTelnetAuthenticator()
	hit, err := auth.Authenticate(context.Background(), "127.0.0.1", 23, nil, 1*time.Second)
	if err != nil {
		t.Fatalf("auth: %v", err)
	}
	if hit != nil {
		t.Errorf("expected nil, got %+v", hit)
	}
}

func TestTelnetAuthenticator_NonPasswordMethodSkipped(t *testing.T) {
	auth := protocols.NewTelnetAuthenticator()
	creds := []cred.Cred{{User: "root", Pass: "secret", Method: "key"}}
	hit, err := auth.Authenticate(context.Background(), "127.0.0.1", 23, creds, 1*time.Second)
	if err != nil {
		t.Fatalf("auth: %v", err)
	}
	if hit != nil {
		t.Errorf("expected nil, got %+v", hit)
	}
}

func TestTelnetAuthenticator_ConnRefused(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	auth := protocols.NewTelnetAuthenticator()
	creds := []cred.Cred{{User: "root", Pass: "secret"}}
	_, err = auth.Authenticate(context.Background(), "127.0.0.1", port, creds, 1*time.Second)
	if err == nil {
		t.Errorf("expected conn error, got nil")
	}
}
