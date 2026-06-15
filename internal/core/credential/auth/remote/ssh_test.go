// ssh_test.go — unit tests for the SSH authenticator.
//
// The v0.2 audit (P3 / F02) flagged this as the only authenticator
// with NO positive-hit test. The only pre-existing coverage
// (TestSSH_Miss in cred_test.go) verified the negative path
// against a server that immediately closed; it never proved the
// happy path actually returns a hit on a real authentication
// success. This file adds the missing positive test using a
// minimal in-process SSH server (golang.org/x/crypto/ssh).
//
// ssh_test.go — SSH 认证器的单元测试。
//
// v0.2 审计（P3 / F02）把这里标为唯一没有正命中测试的认证器。
// 原有覆盖（cred_test.go 的 TestSSH_Miss）只验负命中路径（服务器
// 立即关）；从未证明正路径在真实认证成功时返命中。本文件用进程内
// 最小 SSH 服务器（golang.org/x/crypto/ssh）补正命中测试。
package remote

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"net"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/LCUstinian/FG-QiMen/internal/core/credential"
)

// startFakeSSHServer runs a minimal SSH server in a goroutine.
// PasswordCallback returns nil-permissions (allow) for the matching
// cred, and an error otherwise. The test should call
// fakeSSHServer.WaitReady before issuing the client call to
// avoid a connect-refused race.
//
// startFakeSSHServer 在 goroutine 里跑最小 SSH 服务器。
// PasswordCallback 对匹配凭据返 nil-permissions（允许），否则返错。
// 测试应先调 fakeSSHServer.WaitReady 再发客户端调，避免
// connect-refused 竞争。
type fakeSSHServer struct {
	listener net.Listener
	hostKey  ssh.Signer

	// acceptedCreds: passwords the server will accept. / 服务器接
	// 受的口令集合。
	acceptedCreds map[string]string // user -> password
}

func startFakeSSHServer(t *testing.T, creds map[string]string) *fakeSSHServer {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatalf("rsa.GenerateKey: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		t.Fatalf("ssh.NewSignerFromKey: %v", err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	srv := &fakeSSHServer{listener: ln, hostKey: signer, acceptedCreds: creds}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			// Per-connection server config. Host key is fixed
			// (we want every probe to hit a key we can verify).
			// / 每条连接的 server config。Host key 固定（我们希望
			// 每次探测都打到我们能验的 key）。
			go func(c net.Conn) {
				cfg := &ssh.ServerConfig{
					PasswordCallback: func(meta ssh.ConnMetadata, pass []byte) (*ssh.Permissions, error) {
						if want, ok := srv.acceptedCreds[meta.User()]; ok && string(pass) == want {
							return &ssh.Permissions{}, nil
						}
						return nil, &ssh.PartialSuccessError{} // a non-nil error denies
					},
				}
				cfg.AddHostKey(srv.hostKey)
				if _, _, _, err := ssh.NewServerConn(c, cfg); err != nil {
					_ = c.Close()
				}
			}(c)
		}
	}()
	t.Cleanup(func() { _ = ln.Close() })
	return srv
}

// Addr returns the listening address. The fake server is ready
// to accept connections as soon as the listener is open, so no
// explicit WaitReady is needed.
//
// Addr 返回监听地址。fake server 监听打开就 ready 接连接，所以无
// 需显式 WaitReady。
func (s *fakeSSHServer) Addr() string { return s.listener.Addr().String() }

// TestSSHAuthenticator_PositiveHit — the missing positive path
// from the v0.2 audit. Spins up a real SSH server that accepts
// "alice" / "hunter2" and any other cred is rejected.
//
// TestSSHAuthenticator_PositiveHit — v0.2 审计缺的正命中路径。开
// 真实 SSH 服务器接受 "alice" / "hunter2"，任何其它凭据都拒。
func TestSSHAuthenticator_PositiveHit(t *testing.T) {
	srv := startFakeSSHServer(t, map[string]string{
		"alice": "hunter2",
		"bob":   "correct horse battery staple",
	})
	host, port := splitHostPort(t, srv.Addr())

	a := NewSSHAuthenticator()
	// Probe 1: alice / hunter2 → hit. / 探针 1：alice / hunter2 → 命中。
	hit, err := a.Authenticate(
		context.Background(),
		host, port,
		[]credential.Cred{
			{User: "alice", Pass: "hunter2", Method: credential.AuthPassword},
		},
		3*time.Second,
	)
	if err != nil {
		t.Fatalf("Authenticate(alice/hunter2): %v", err)
	}
	if hit == nil {
		t.Fatal("Authenticate(alice/hunter2) returned nil hit; want non-nil")
	}
	if hit.Cred.User != "alice" || hit.Cred.Pass != "hunter2" {
		t.Errorf("hit.Cred = %+v, want alice/hunter2", hit.Cred)
	}

	// Probe 2: alice / WRONG → nil hit, no error. / 探针 2：alice / 错
	// → nil hit，无错。
	hit, err = a.Authenticate(
		context.Background(),
		host, port,
		[]credential.Cred{
			{User: "alice", Pass: "WRONG-PASSWORD", Method: credential.AuthPassword},
		},
		3*time.Second,
	)
	if err != nil {
		t.Errorf("Authenticate(alice/WRONG) returned err = %v, want nil", err)
	}
	if hit != nil {
		t.Errorf("Authenticate(alice/WRONG) returned hit = %+v, want nil", hit)
	}
}

// TestSSHAuthenticator_MultipleCredsFirstHit — the Authenticate
// loop returns the FIRST hit (per the implementation comment);
// verify the ordering contract with a 3-cred list where cred[1]
// is the right password.
//
// TestSSHAuthenticator_MultipleCredsFirstHit — Authenticate 循
// 环返首个命中（按实现注释）；用 3 条凭据验证顺序契约，cred[1]
// 是对的口令。
func TestSSHAuthenticator_MultipleCredsFirstHit(t *testing.T) {
	srv := startFakeSSHServer(t, map[string]string{
		"bob": "right-pw",
	})
	host, port := splitHostPort(t, srv.Addr())

	a := NewSSHAuthenticator()
	hit, err := a.Authenticate(
		context.Background(),
		host, port,
		[]credential.Cred{
			{User: "bob", Pass: "wrong-1", Method: credential.AuthPassword},
			{User: "bob", Pass: "right-pw", Method: credential.AuthPassword},
			{User: "bob", Pass: "wrong-2", Method: credential.AuthPassword},
		},
		3*time.Second,
	)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if hit == nil {
		t.Fatal("hit = nil")
	}
	if hit.Attempts != 2 {
		t.Errorf("hit.Attempts = %d, want 2 (first match at cred[1])", hit.Attempts)
	}
}

// splitHostPort is a tiny test helper to extract host/port from
// a "host:port" string. Lives next to the test (rather than in
// a shared helper file) because it's the same shape as the
// existing rdp_test.go / winrm_test.go helpers, but each test
// file in this package duplicates it — Go's testing convention
// is to keep helper files minimal.
//
// splitHostPort 是从 "host:port" 字符串提 host/port 的小测试
// helper。放在测试旁（而不是共享 helper 文件）是因为它与现有
// rdp_test.go / winrm_test.go helper 同形，但本包各测试文件都重复
// 它——Go 测试惯例是让 helper 文件最小化。
func splitHostPort(t *testing.T, addr string) (string, int) {
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
