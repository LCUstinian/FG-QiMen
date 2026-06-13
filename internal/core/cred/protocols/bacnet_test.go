// bacnet_test.go — unit test for the BACnet credential authenticator.
// / BACnet 凭据认证器的单元测试。
//
// BACnet is credential-less in v0.1 (the protocol itself has no
// auth layer; vendor-specific auth like BACnet/SC is out of scope).
// We just probe the device responds to a Who-Is request. / BACnet
// 在 v0.1 无凭据（协议本身无认证层；厂商特定认证如 BACnet/SC 不在
// 范围）。我们只探设备是否响应 Who-Is 请求。
package protocols_test

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/core/cred"
	"github.com/LCUstinian/FG-QiMen/internal/core/cred/protocols"
)

func TestBACnetAuthenticator_EmptyCreds(t *testing.T) {
	auth := protocols.NewBACnetAuthenticator()
	hit, err := auth.Authenticate(context.Background(), "127.0.0.1", 47808, nil, 1*time.Second)
	if err != nil {
		t.Fatalf("auth: %v", err)
	}
	if hit != nil {
		t.Errorf("expected nil, got %+v", hit)
	}
}

func TestBACnetAuthenticator_NonPasswordMethodSkipped(t *testing.T) {
	auth := protocols.NewBACnetAuthenticator()
	creds := []cred.Cred{{User: "admin", Pass: "secret", Method: "key"}}
	hit, _ := auth.Authenticate(context.Background(), "127.0.0.1", 47808, creds, 1*time.Second)
	if hit != nil {
		t.Errorf("expected nil, got %+v", hit)
	}
}

func TestBACnetAuthenticator_UDPNoResponse(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	auth := protocols.NewBACnetAuthenticator()
	creds := []cred.Cred{{User: "admin", Pass: "secret"}}
	hit, _ := auth.Authenticate(context.Background(), "127.0.0.1", port, creds, 1*time.Second)
	if hit != nil {
		t.Errorf("expected nil (UDP no response = miss), got %+v", hit)
	}
}
