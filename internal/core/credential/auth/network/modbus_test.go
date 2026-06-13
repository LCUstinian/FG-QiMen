// modbus_test.go — unit test for the Modbus credential authenticator.
// / Modbus 凭据认证器的单元测试。
//
// Modbus TCP is technically credential-less (no built-in auth), but
// many gateways (e.g. Schneider, Siemens) wrap it with HTTP/Basic
// or vendor-proprietary auth. v0.1 tests the TCP-level probe only
// (no auth negotiation) — same as a vendor-neutral scanner. /
// Modbus TCP 本身无凭据（无内置认证），但很多网关（施耐德、西门子）
// 套了 HTTP/Basic 或厂商私有认证。v0.1 只测 TCP 级探针（不做认证
// 协商）——跟厂商中立扫描器一致。
package network

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/core/credential"
)

func TestModbusAuthenticator_EmptyCreds(t *testing.T) {
	auth := NewModbusAuthenticator()
	hit, err := auth.Authenticate(context.Background(), "127.0.0.1", 502, nil, 1*time.Second)
	if err != nil {
		t.Fatalf("auth: %v", err)
	}
	if hit != nil {
		t.Errorf("expected nil, got %+v", hit)
	}
}

func TestModbusAuthenticator_NonPasswordMethodSkipped(t *testing.T) {
	auth := NewModbusAuthenticator()
	creds := []credential.Cred{{User: "admin", Pass: "secret", Method: "key"}}
	hit, _ := auth.Authenticate(context.Background(), "127.0.0.1", 502, creds, 1*time.Second)
	if hit != nil {
		t.Errorf("expected nil, got %+v", hit)
	}
}

func TestModbusAuthenticator_ConnRefused(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	auth := NewModbusAuthenticator()
	creds := []credential.Cred{{User: "admin", Pass: "secret"}}
	_, err := auth.Authenticate(context.Background(), "127.0.0.1", port, creds, 1*time.Second)
	if err == nil {
		t.Errorf("expected conn error, got nil")
	}
}
