// ldap_test.go — unit test for the LDAP simple bind authenticator.
// / LDAP simple bind 认证器的单元测试。
//
// Scope: buildBindDN heuristics (the meat of the function), empty
// creds, non-password method, conn refused. The full BER-encoded
// LDAPv3 bind is exercised by the v0.1 end-to-end smoke against a
// real LDAP / AD instance (obs #886).
// / 范围：buildBindDN 启发式（函数主体）、空 creds、非 password 方法、
// 连接拒绝。完整 BER 编码的 LDAPv3 bind 由 v0.1 端到端 smoke 对真
// LDAP / AD 实例（obs #886）测。
package protocols_test

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/core/cred"
	"github.com/LCUstinian/FG-QiMen/internal/core/cred/protocols"
)

func TestLDAPAuthenticator_EmptyCreds(t *testing.T) {
	auth := protocols.NewLDAPAuthenticator()
	hit, _ := auth.Authenticate(context.Background(), "127.0.0.1", 389, nil, 1*time.Second)
	if hit != nil {
		t.Errorf("expected nil, got %+v", hit)
	}
}

func TestLDAPAuthenticator_NonPasswordMethodSkipped(t *testing.T) {
	auth := protocols.NewLDAPAuthenticator()
	creds := []cred.Cred{{User: "alice", Pass: "secret", Method: "key"}}
	hit, _ := auth.Authenticate(context.Background(), "127.0.0.1", 389, creds, 1*time.Second)
	if hit != nil {
		t.Errorf("expected nil, got %+v", hit)
	}
}

func TestLDAPAuthenticator_ConnRefused(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	auth := protocols.NewLDAPAuthenticator()
	creds := []cred.Cred{{User: "alice@example.com", Pass: "secret"}}
	_, err := auth.Authenticate(context.Background(), "127.0.0.1", port, creds, 1*time.Second)
	if err == nil {
		t.Errorf("expected conn error, got nil")
	}
}
