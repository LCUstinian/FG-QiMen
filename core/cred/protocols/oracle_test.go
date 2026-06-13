// oracle_test.go — unit test for the Oracle credential authenticator.
//
// Scope: empty creds, non-password method, conn refused. The actual
// TNS handshake (handled by go-ora) is exercised by the v0.1
// end-to-end smoke against a real Oracle instance (obs #886);
// in-process TNS mocking would be 200+ lines of TNS Connect /
// Accept / Refuse packet encoding for no real coverage gain —
// go-ora is the protocol authority.
//
// oracle_test.go — Oracle 凭据认证器的单元测试。
// 范围：空 creds、非 password 方法、连接拒绝。真实 TNS 握手
// （由 go-ora 处理）由 v0.1 端到端 smoke 对真 Oracle 实例（obs #886）
// 测；进程内 mock TNS 要 200+ 行 Connect/Accept/Refuse 包编码，对
// go-ora 自身没增加覆盖。
package protocols_test

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/LCUstinian/FG-QiMen/core/cred"
	"github.com/LCUstinian/FG-QiMen/core/cred/protocols"
)

func TestOracleAuthenticator_EmptyCreds(t *testing.T) {
	auth := protocols.NewOracleAuthenticator()
	hit, err := auth.Authenticate(context.Background(), "127.0.0.1", 1521, nil, 1*time.Second)
	if err != nil {
		t.Fatalf("auth: %v", err)
	}
	if hit != nil {
		t.Errorf("expected nil, got %+v", hit)
	}
}

func TestOracleAuthenticator_NonPasswordMethodSkipped(t *testing.T) {
	auth := protocols.NewOracleAuthenticator()
	creds := []cred.Cred{{User: "sys", Pass: "secret", Method: "key"}}
	hit, err := auth.Authenticate(context.Background(), "127.0.0.1", 1521, creds, 1*time.Second)
	if err != nil {
		t.Fatalf("auth: %v", err)
	}
	if hit != nil {
		t.Errorf("expected nil, got %+v", hit)
	}
}

func TestOracleAuthenticator_ConnRefused(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	auth := protocols.NewOracleAuthenticator()
	creds := []cred.Cred{{User: "sys", Pass: "secret"}}
	_, err = auth.Authenticate(context.Background(), "127.0.0.1", port, creds, 1*time.Second)
	if err == nil {
		t.Errorf("expected conn error, got nil")
	}
}
