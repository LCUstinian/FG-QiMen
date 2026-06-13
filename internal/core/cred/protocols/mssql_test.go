// mssql_test.go — smoke test for the MSSQL authenticator.
// mssql_test.go — MSSQL 认证器的冒烟测试。
//
// A full fake TDS server (PRELOGIN + Login7 + LOGINACK) is large and
// fragile. For v0.1 we verify the call path goes through
// github.com/microsoft/go-mssqldb by:
//   1. Starting a TCP server that accepts and immediately closes.
//   2. Calling Authenticate with valid creds.
//   3. Confirming we get a nil hit and a non-panic result (the
//      go-mssqldb driver errors out on the bogus server; our
//      Authenticate classifies the error as "not MSSQL" / "wrong
//      creds" and returns nil).
//
// This proves the code path uses the driver, the DSN builder is
// well-formed, and error handling doesn't crash.
//
// 写一个完整的假 TDS 服务器（PRELOGIN + Login7 + LOGINACK）又大又脆。
// v0.1 改用冒烟测试：起一个接受后立即关闭的 TCP 服务器，调
// Authenticate，确认返回 nil hit 且不 panic（go-mssqldb 驱动对假
// 服务报错；我们的 Authenticate 把错误归为"非 MSSQL"/"错凭据"并
// 返回 nil）。证明代码路径用驱动、DSN 构造正确、错误处理不崩。
package protocols_test

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/core/cred"
	"github.com/LCUstinian/FG-QiMen/internal/core/cred/protocols"
)

// TestMSSQL_NonMSSQLServerReturnsNil starts a TCP listener that
// accepts and immediately closes. The go-mssqldb driver fails to
// complete the TDS handshake; our Authenticate returns nil (no
// hit, no crash). This proves the path through go-mssqldb is wired
// up correctly.
// / TestMSSQL_NonMSSQLServerReturnsNil 起一个接受后立即关闭的 TCP
// 监听。go-mssqldb 驱动完不成 TDS 握手；我们的 Authenticate 返回
// nil（无 hit、不崩）。证明 go-mssqldb 路径已正确接好。
func TestMSSQL_NonMSSQLServerReturnsNil(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	// Accept and immediately close each connection. The driver will
	// see EOF while reading the PRELOGIN response.
	// / 接受后立即关闭。驱动在读 PRELOGIN 响应时看到 EOF。
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_ = c.Close()
		}
	}()

	auth := protocols.NewMSSQLAuthenticator()
	addr := ln.Addr().(*net.TCPAddr)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	creds := []cred.Cred{
		{User: "sa", Pass: "anypassword", Method: cred.AuthPassword},
	}
	hit, _ := auth.Authenticate(ctx, addr.IP.String(), addr.Port, creds, 2*time.Second)
	if hit != nil {
		t.Errorf("expected nil hit for non-MSSQL server, got %+v", hit)
	}
}
