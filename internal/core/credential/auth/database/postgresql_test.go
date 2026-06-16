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
	"errors"
	"fmt"
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

// TestPostgreSQLAuthenticator_ServerBailsMidProtocol — verifies
// the *negative* boundary at the integration level. The fake
// server reads the StartupMessage then immediately closes the
// connection. lib/pq's Ping returns "unexpected EOF", which
// isPGProtocolError classifies as a network error (not a
// protocol error). The authenticator propagates the error to
// the caller — "host unreachable" / "connection broken" — so
// the operator sees the right signal rather than a silent miss
// for what is actually a broken connection.
//
// Why no positive boundary at integration level: getting lib/pq
// to surface a "pq: ..." error requires faking enough of the PG
// v3 protocol to drive the SCRAM-SHA-256 sub-flow through to its
// ErrorResponse phase — 80+ lines for no real coverage gain since
// lib/pq is the protocol authority. The "pq: ..." classification
// path itself is locked by TestIsPGProtocolError at the unit
// level, so the integration test only needs to verify that the
// classifier is actually invoked at the call site (this test).
//
// TestPostgreSQLAuthenticator_ServerBailsMidProtocol — 集成层
// 验证负向边界。假服务器读 StartupMessage 后立即关连接。
// lib/pq 的 Ping 返 "unexpected EOF"，isPGProtocolError 归类为
// 网络错（非协议错）。认证器把错传播给调用方——"主机不可达/
// 连接已断"——操作员看到正确信号而不是静默 miss。
func TestPostgreSQLAuthenticator_ServerBailsMidProtocol(t *testing.T) {
	srv := startFakePGServer(t)
	host, port := pgSplitHostPort(t, srv.Addr())
	defer srv.Close()

	auth := NewPostgreSQLAuthenticator()
	hit, err := auth.Authenticate(
		context.Background(),
		host, port,
		[]credential.Cred{
			{User: "alice", Pass: "any", Method: credential.AuthPassword},
		},
		2*time.Second,
	)
	// We expect a network-class error and no hit. We do NOT
	// assert on the specific error string — lib/pq's EOF message
	// has changed across versions ("unexpected EOF", "EOF",
	// "io: unexpected EOF"). What matters is: (a) err != nil and
	// (b) hit == nil.
	// / 我们期望网络类错且无 hit。不断言具体错字符串——lib/pq
	// 的 EOF 消息跨版本变（"unexpected EOF"、"EOF"、"io:
	// unexpected EOF"）。重要的是：(a) err != nil 且 (b) hit == nil。
	if err == nil {
		t.Errorf("Authenticate: server-bails-mid-protocol should surface as a network error (non-nil err); got nil err")
	}
	if hit != nil {
		t.Errorf("hit = %+v, want nil (server bailed; no auth result)", hit)
	}
}

// fakePGServer is a minimal in-process PG-protocol-ish server. The
// server doesn't actually need to speak enough PG to make lib/pq
// return a "pq: ..." error (that requires implementing SCRAM-SHA-256
// in full — 80+ lines). What it CAN do, and what we test here, is
// close the connection mid-protocol so lib/pq returns EOF, which
// isPGProtocolError correctly classifies as a network error.
//
// fakePGServer 是进程内最小 PG 风格服务器。服务器不需要真
// 讲够 PG 让 lib/pq 返 "pq: ..." 错（那需要全实现 SCRAM-SHA-256
// ——80+ 行）。它能做的也是我们在这里测的是：在协议中间关连接
// 让 lib/pq 返 EOF，isPGProtocolError 正确归类为网络错。
type fakePGServer struct {
	listener net.Listener
}

func startFakePGServer(t *testing.T) *fakePGServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &fakePGServer{listener: ln}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go srv.serveOne(c)
		}
	}()
	return srv
}

func (s *fakePGServer) Addr() string { return s.listener.Addr().String() }
func (s *fakePGServer) Close()      { _ = s.listener.Close() }

// serveOne is a non-PG server: it reads the StartupMessage (which
// lib/pq sends on every connect), then immediately closes the
// connection. The driver surfaces this as an EOF / unexpected-EOF
// error, which isPGProtocolError classifies as a network error
// (not a protocol error) — so the authenticator returns the error
// to the caller as "host unreachable / connection broken".
//
// This tests the *negative* boundary at the integration level
// (network-error propagation). The corresponding *positive*
// boundary (server replied with auth failure → "pq: ..." →
// classified as miss) is locked by TestIsPGProtocolError at the
// unit level. We don't try to test it at integration level
// because properly faking the PG server to that depth requires
// implementing SCRAM-SHA-256 in full — 80+ lines for no real
// coverage gain since lib/pq is the protocol authority.
//
// serveOne 是非 PG 服务器：读 StartupMessage（lib/pq 每次连
// 都发）然后立即关连接。驱动把它浮上来是 EOF / unexpected-EOF
// 错，isPGProtocolError 归类为网络错（非协议错）——所以认证器
// 把错返给调用方作为"主机不可达/连接已断"。
func (s *fakePGServer) serveOne(c net.Conn) {
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(2 * time.Second))

	// Drain the StartupMessage (length-prefixed; up to 4KB to be
	// safe with all the kv pairs lib/pq puts in there). We don't
	// care about the body — we just need to receive it before
	// closing so the client doesn't get a write-side error.
	// / 接干 StartupMessage（长度前缀；最多 4KB 防 lib/pq 塞很多
	// kv 对）。body 我们不关心——关前必须收到，避免客户端错在
	// 写侧。
	hdr := make([]byte, 4)
	if err := readPGFull(c, hdr); err != nil {
		return
	}
	length := int(hdr[0])<<24 | int(hdr[1])<<16 | int(hdr[2])<<8 | int(hdr[3])
	if length < 8 || length > 4096 {
		return
	}
	body := make([]byte, length-4)
	if err := readPGFull(c, body); err != nil {
		return
	}
	// No response. Closing the conn makes lib/pq see EOF on its
	// next read.
	// / 无响应。关连接让 lib/pq 下次读看到 EOF。
}

// readPGFull reads exactly len(buf) bytes from c, looping
// until done or error. net.Conn.Read can return short reads.
// / readPGFull 从 c 正好读 len(buf) 字节，循环到完成或出错。
// net.Conn.Read 可能短读。
func readPGFull(c net.Conn, buf []byte) error {
	off := 0
	for off < len(buf) {
		n, err := c.Read(buf[off:])
		if err != nil {
			return err
		}
		off += n
	}
	return nil
}

// pgSplitHostPort is a tiny test helper, kept separate from
// the SSH one to avoid shared-test-file coupling.
// / pgSplitHostPort 是小测试 helper，与 SSH 那个分开以免测试
// 文件互相耦合。
func pgSplitHostPort(t *testing.T, addr string) (string, int) {
	t.Helper()
	host, p, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("splitHostPort(%q): %v", addr, err)
	}
	var port int
	for _, c := range p {
		port = port*10 + int(c-'0')
	}
	return host, port
}

// TestIsPGProtocolError — pin the audit-finding error classifier.
// "pq: ..." errors are auth misses; net errors are host-unreachable.
// Mixing them causes the operator to see "host down" instead of a
// silent miss for a wrong password (or vice versa).
//
// TestIsPGProtocolError — 锁定审计发现的错误分类器。"pq: ..."
// 错是认证 miss；net 错是主机不可达。混了会让操作员看到"主机
// 不通"而不是错口令的静默 miss（反之亦然）。
func TestIsPGProtocolError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil",
			err:  nil,
			want: false,
		},
		{
			name: "pg server-replied FATAL (auth failure)",
			err:  fmt.Errorf(`pq: FATAL: password authentication failed for user "alice"`),
			want: true,
		},
		{
			name: "pg server-replied ERROR (other auth fail)",
			err:  fmt.Errorf(`pq: ERROR: role "ghost" does not exist`),
			want: true,
		},
		{
			name: "net dial refused (host down)",
			err:  &net.OpError{Op: "dial", Err: errors.New("connect refused")},
			want: false,
		},
		{
			name: "context deadline exceeded",
			err:  context.DeadlineExceeded,
			want: false,
		},
		{
			name: "random error string",
			err:  errors.New("some random thing"),
			want: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isPGProtocolError(c.err); got != c.want {
				t.Errorf("isPGProtocolError(%v) = %v, want %v", c.err, got, c.want)
			}
		})
	}
}
