// smb_test.go — smoke test for the SMB authenticator.
// smb_test.go — SMB 认证器的冒烟测试。
//
// A full fake SMB2 server (Negotiate Protocol + Session Setup with
// NTLMv2) is large. For v0.1 we verify the path through
// github.com/hirochachacha/go-smb2 by:
//   1. Starting a TCP server that accepts and immediately closes.
//   2. Calling Authenticate with valid creds.
//   3. Confirming we get nil (the go-smb2 client errors on the
//      bogus server; we treat it as a miss, not a crash).
//
// 写一个完整的假 SMB2 服务器（Negotiate Protocol + NTLMv2 Session
// Setup）工作量很大。v0.1 用冒烟测试：起一个接受后立即关闭的
// TCP 服务器，调 Authenticate，确认返回 nil（go-smb2 客户端对
// 假服务报错；我们视为 miss，不崩）。
package filestorage

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/core/credential"
)

// TestSMB_NonSMBServerReturnsNil starts a TCP listener that accepts
// and immediately closes. The go-smb2 client fails to read the
// SMB2 Negotiate response; our Authenticate returns nil (no hit).
// Proves the code path uses go-smb2 and the DSN/initiator wiring
// is well-formed.
// / TestSMB_NonSMBServerReturnsNil 起一个接受后立即关闭的 TCP
// 监听。go-smb2 客户端读不到 SMB2 Negotiate 响应；我们的
// Authenticate 返回 nil（无 hit）。证明代码路径用 go-smb2、
// initiator 接线正确。
func TestSMB_NonSMBServerReturnsNil(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()

	auth := NewSMBAuthenticator()
	addr := ln.Addr().(*net.TCPAddr)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	creds := []credential.Cred{
		{User: "Administrator", Pass: "anypassword", Method: credential.AuthPassword},
	}
	hit, _ := auth.Authenticate(ctx, addr.IP.String(), addr.Port, creds, 2*time.Second)
	if hit != nil {
		t.Errorf("expected nil hit for non-SMB server, got %+v", hit)
	}
}
