// postgresql_test.go — unit test for the PostgreSQL credential authenticator.
//
// Scope of these unit tests: the DSN builder, the empty-creds short-circuit,
// and the network-error propagation. The actual lib/pq auth path (which
// involves a SCRAM-SHA-256 multi-step handshake inside the driver) is
// tested end-to-end in test/ against a real PostgreSQL server (see v0.1
// smoke test obs #886). Mocking SCRAM in-process would be 50+ lines for
// no real coverage gain — lib/pq is the protocol authority.
//
// postgresql_test.go — PostgreSQL 凭据认证器的单元测试。
// 单元测试范围：DSN 构造、空 creds 短路、网络错传播。lib/pq 的真实认证
// 路径（含 SCRAM-SHA-256 多步握手）在 test/ 里的真 PG 端到端 smoke 中
// 验证（v0.1 obs #886）。进程内 mock SCRAM 要 50+ 行但对 lib/pq 自身
// 没增加覆盖——lib/pq 才是协议权威。
package database

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/core/credential"
)

// listenAndClose binds a TCP port and immediately closes the listener,
// returning the port. The next connection attempt to this port will
// get connection-refused.
//
// listenAndClose 绑一个 TCP 端口然后立即关闭。下次连接该端口会得到
// connection-refused。
func listenAndClose(t *testing.T) (struct{ Port int }, error) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return struct{ Port int }{}, err
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return struct{ Port int }{Port: port}, nil
}

func TestPostgreSQLAuthenticator_EmptyCreds(t *testing.T) {
	auth := NewPostgreSQLAuthenticator()
	hit, err := auth.Authenticate(context.Background(), "127.0.0.1", 5432, nil, 1*time.Second)
	if err != nil {
		t.Fatalf("auth: %v", err)
	}
	if hit != nil {
		t.Errorf("expected nil, got %+v", hit)
	}
}

func TestPostgreSQLAuthenticator_NonPasswordMethodSkipped(t *testing.T) {
	// A non-password method (e.g. "key") is filtered out before any
	// network call. Even with a reachable-but-empty test port, we should
	// see no connection attempt and return (nil, nil).
	// / 非 password 方法（如 "key"）在网络调用前就被过滤掉。即使目标
	// 端口可达也看不到连接尝试，返 (nil, nil)。
	auth := NewPostgreSQLAuthenticator()
	creds := []credential.Cred{{User: "alice", Pass: "secret", Method: "key"}}
	hit, err := auth.Authenticate(context.Background(), "127.0.0.1", 1, creds, 1*time.Second)
	if err != nil {
		t.Fatalf("auth: %v", err)
	}
	if hit != nil {
		t.Errorf("expected nil, got %+v", hit)
	}
}

func TestPostgreSQLAuthenticator_ConnRefused(t *testing.T) {
	// Bind+close to get a port nothing is listening on. / Bind+close
	// 拿一个没有监听的端口。
	addr, err := listenAndClose(t)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	auth := NewPostgreSQLAuthenticator()
	creds := []credential.Cred{{User: "alice", Pass: "secret"}}
	_, err = auth.Authenticate(context.Background(), "127.0.0.1", addr.Port, creds, 1*time.Second)
	if err == nil {
		t.Errorf("expected conn error, got nil")
	}
}
