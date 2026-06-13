// ipmi_test.go — unit test for the IPMI credential authenticator.
// / IPMI 凭据认证器的单元测试。
//
// Scope: empty creds, non-password method, conn refused. The full
// IPMI v2.0 RAKP message exchange (with HMAC-SHA1) is exercised
// by the v0.1 end-to-end smoke against a real BMC (obs #886).
// / 范围：空 creds、非 password 方法、连接拒绝。完整 IPMI v2.0 RAKP
// 消息交换（带 HMAC-SHA1）由 v0.1 端到端 smoke 对真 BMC（obs #886）
// 测。
package remote

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/core/credential"
)

func TestIPMIAuthenticator_EmptyCreds(t *testing.T) {
	auth := NewIPMIAuthenticator()
	hit, err := auth.Authenticate(context.Background(), "127.0.0.1", 623, nil, 1*time.Second)
	if err != nil {
		t.Fatalf("auth: %v", err)
	}
	if hit != nil {
		t.Errorf("expected nil, got %+v", hit)
	}
}

func TestIPMIAuthenticator_NonPasswordMethodSkipped(t *testing.T) {
	auth := NewIPMIAuthenticator()
	creds := []credential.Cred{{User: "admin", Pass: "secret", Method: "key"}}
	hit, _ := auth.Authenticate(context.Background(), "127.0.0.1", 623, creds, 1*time.Second)
	if hit != nil {
		t.Errorf("expected nil, got %+v", hit)
	}
}

func TestIPMIAuthenticator_UDPNoResponse(t *testing.T) {
	// For UDP, "no service" looks identical to "wrong cred" — both
	// result in no response. The auth treats this as a miss.
	// / UDP 的"端口无服务"跟"凭据错"无法区分——都是无响应。auth
	// 把这视为 miss。
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	auth := NewIPMIAuthenticator()
	creds := []credential.Cred{{User: "admin", Pass: "secret"}}
	hit, _ := auth.Authenticate(context.Background(), "127.0.0.1", port, creds, 1*time.Second)
	if hit != nil {
		t.Errorf("expected nil (UDP no response = miss), got %+v", hit)
	}
}
