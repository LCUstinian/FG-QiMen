// snmp_test.go — unit test for the SNMP community authenticator.
// / SNMP community 认证器的单元测试。
//
// Scope: empty creds, non-password method, conn refused. The full
// SNMPv2c PDU exchange is exercised by the v0.1 end-to-end smoke
// against a real snmpd (obs #886). / 范围：空 creds、非 password
// 方法、连接拒绝。完整 SNMPv2c PDU 交互由 v0.1 端到端 smoke 对真
// snmpd（obs #886）测。
package protocols_test

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/core/cred"
	"github.com/LCUstinian/FG-QiMen/internal/core/cred/protocols"
)

func TestSNMPAuthenticator_EmptyCreds(t *testing.T) {
	auth := protocols.NewSNMPAuthenticator()
	hit, _ := auth.Authenticate(context.Background(), "127.0.0.1", 161, nil, 1*time.Second)
	if hit != nil {
		t.Errorf("expected nil, got %+v", hit)
	}
}

func TestSNMPAuthenticator_NonPasswordMethodSkipped(t *testing.T) {
	auth := protocols.NewSNMPAuthenticator()
	creds := []cred.Cred{{User: "any", Pass: "public", Method: "key"}}
	hit, _ := auth.Authenticate(context.Background(), "127.0.0.1", 161, creds, 1*time.Second)
	if hit != nil {
		t.Errorf("expected nil, got %+v", hit)
	}
}

func TestSNMPAuthenticator_UDPNoResponse(t *testing.T) {
	// For UDP, "no service on this port" looks identical to "wrong
	// community" — both result in no response. The auth treats this
	// as a miss (nil, nil) rather than an error. / UDP 的"端口无服
	// 务"跟"community 错"无法区分——都是无响应。auth 把这视为 miss
	//（nil, nil）而非 error。
	// Bind+close a TCP port (no UDP service there). / 绑+关一个 TCP
	// 端口（UDP 不会有服务）。
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	auth := protocols.NewSNMPAuthenticator()
	creds := []cred.Cred{{User: "", Pass: "public"}}
	hit, _ := auth.Authenticate(context.Background(), "127.0.0.1", port, creds, 1*time.Second)
	if hit != nil {
		t.Errorf("expected nil (no UDP response = miss), got %+v", hit)
	}
}
