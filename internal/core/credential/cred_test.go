// cred_test.go — unit tests for the cred package + its protocols.
// cred_test.go — cred 包及其 protocols 的单元测试。
//
// External test package (suffix _test) so we can import both cred
// and cred/protocols without an import cycle. / 外部测试包（_test 后缀）
// 这样能同时 import cred 和 cred/protocols 而不产生循环。
package credential_test

import (
	"bufio"
	"context"
	"database/sql"
	"fmt"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"github.com/LCUstinian/FG-QiMen/internal/core/credential"
	"github.com/LCUstinian/FG-QiMen/internal/core/credential/auth/database" // MySQL driver init
	"github.com/LCUstinian/FG-QiMen/internal/core/credential/auth/remote"   // SSH / FTP authenticators
)

// ─────────────────────────────────────────────────────────────────────
// Pool / Loader
// ─────────────────────────────────────────────────────────────────────

func TestPool_AddDedup(t *testing.T) {
	p := credential.NewPool()
	if !p.Add(credential.Cred{User: "a", Pass: "1", Method: credential.AuthPassword}) {
		t.Error("first Add should return true")
	}
	if p.Add(credential.Cred{User: "a", Pass: "1", Method: credential.AuthPassword}) {
		t.Error("duplicate Add should return false")
	}
	if p.Len() != 1 {
		t.Errorf("expected Len=1, got %d", p.Len())
	}
	if !p.Add(credential.Cred{User: "a", Pass: "2", Method: credential.AuthPassword}) {
		t.Error("different pass should Add")
	}
	if p.Len() != 2 {
		t.Errorf("expected Len=2, got %d", p.Len())
	}
}

func TestPool_LoadInline(t *testing.T) {
	p := credential.NewPool()
	n, err := credential.LoadInto(p, credential.LoadOptions{
		Users:  []string{"a", "b"},
		Passes: []string{"1", "2"},
	})
	if err != nil {
		t.Fatalf("LoadInto: %v", err)
	}
	if n != 4 {
		t.Errorf("expected 4 creds, got %d", n)
	}
}

func TestPool_LoadFile(t *testing.T) {
	dir := t.TempDir()
	uf := dir + "/users.txt"
	pf := dir + "/passes.txt"
	if err := os.WriteFile(uf, []byte("alice\n# bob's account\nbob\nalice\n"), 0o644); err != nil {
		t.Fatalf("write users: %v", err)
	}
	if err := os.WriteFile(pf, []byte("secret1\nsecret2\n"), 0o644); err != nil {
		t.Fatalf("write passes: %v", err)
	}
	p := credential.NewPool()
	n, err := credential.LoadInto(p, credential.LoadOptions{UserFile: uf, PassFile: pf})
	if err != nil {
		t.Fatalf("LoadInto: %v", err)
	}
	if n != 4 {
		t.Errorf("expected 4 creds, got %d", n)
	}
}

// ─────────────────────────────────────────────────────────────────────
// Scheduler
// ─────────────────────────────────────────────────────────────────────

type alwaysMissAuth struct{ name string }

func (a *alwaysMissAuth) Name() string        { return a.name }
func (a *alwaysMissAuth) DefaultPorts() []int { return []int{1} }
func (a *alwaysMissAuth) Authenticate(_ context.Context, _ string, _ int, _ []credential.Cred, _ time.Duration) (*credential.Hit, error) {
	return nil, nil
}

func TestScheduler_HitSink(t *testing.T) {
	var hits int
	sink := credential.FuncHitSink(func(*credential.Hit) { hits++ })
	s := credential.NewScheduler(credential.DefaultSchedulerOptions())
	auth := &alwaysMissAuth{name: "fake"}
	targets := []credential.Target{
		{Host: "127.0.0.1", Port: 1, Auth: auth, Creds: []credential.Cred{{User: "u", Pass: "p"}}},
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	s.Run(ctx, targets, sink)
	if hits != 0 {
		t.Errorf("expected 0 hits, got %d", hits)
	}
}

// ─────────────────────────────────────────────────────────────────────
// SSH authenticator
// ─────────────────────────────────────────────────────────────────────

func TestSSH_Miss(t *testing.T) {
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
	addr := ln.Addr().(*net.TCPAddr)
	auth := remote.NewSSHAuthenticator()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	hit, err := auth.Authenticate(ctx, addr.IP.String(), addr.Port,
		[]credential.Cred{{User: "u", Pass: "p", Method: credential.AuthPassword}}, time.Second)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if hit != nil {
		t.Errorf("expected nil hit, got %+v", hit)
	}
}

// ─────────────────────────────────────────────────────────────────────
// FTP authenticator (via jlaffaye/ftp)
// ─────────────────────────────────────────────────────────────────────

// startFakeFTP starts a tiny in-process FTP server that handles the
// full jlaffaye/ftp Login exchange (USER/PASS + FEAT + TYPE + OPTS UTF8).
// / startFakeFTP 启动一个进程内的 FTP 服务，完整覆盖 jlaffaye/ftp 的
// Login 交换（USER/PASS + FEAT + TYPE + OPTS UTF8）。
func startFakeFTP(t *testing.T, expectedPass string, hit bool) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				_ = c.SetDeadline(time.Now().Add(2 * time.Second))
				br := bufio.NewReader(c)
				// Greet / 打招呼
				_, _ = c.Write([]byte("220 fake-ftp ready\r\n"))
				// Read USER / 读 USER
				userLine, _ := br.ReadString('\n')
				_ = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(userLine), "USER"))
				_, _ = c.Write([]byte("331 password required\r\n"))
				// Read PASS / 读 PASS
				passLine, _ := br.ReadString('\n')
				pass := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(passLine), "PASS"))
				if !(hit && pass == expectedPass) {
					_, _ = c.Write([]byte("530 login incorrect\r\n"))
					return
				}
				_, _ = c.Write([]byte("230 login ok\r\n"))
				// Drain subsequent setup commands (FEAT / TYPE / OPTS UTF8
				// / PBSZ / PROT — the library issues several after
				// successful login). Respond with 200/211 so it can
				// finish its Login. / 排空后续设置命令（FEAT / TYPE /
				// OPTS UTF8 / PBSZ / PROT——库在登录成功后还会发几条）。
				// 用 200/211 应答让库完成 Login。
				for i := 0; i < 8; i++ {
					line, err := br.ReadString('\n')
					if err != nil {
						return
					}
					cmd := strings.ToUpper(strings.TrimSpace(strings.SplitN(line, " ", 2)[0]))
					switch cmd {
					case "FEAT":
						_, _ = c.Write([]byte("211 End\r\n"))
					case "TYPE":
						_, _ = c.Write([]byte("200 Type set\r\n"))
					case "OPTS":
						_, _ = c.Write([]byte("200 Options set\r\n"))
					case "PBSZ", "PROT":
						_, _ = c.Write([]byte("200 OK\r\n"))
					case "QUIT":
						_, _ = c.Write([]byte("221 Bye\r\n"))
						return
					default:
						// 200 OK for any other setup command. / 其他设置
						// 命令统一 200 OK。
						_, _ = c.Write([]byte("200 OK\r\n"))
					}
				}
			}(c)
		}
	}()
	t.Cleanup(func() { _ = ln.Close() })
	return ln
}

func TestFTP_Hit(t *testing.T) {
	ln := startFakeFTP(t, "secret", true)
	auth := remote.NewFTPAuthenticator()
	addr := ln.Addr().(*net.TCPAddr)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	creds := []credential.Cred{
		{User: "u", Pass: "wrong", Method: credential.AuthPassword},
		{User: "u", Pass: "secret", Method: credential.AuthPassword},
	}
	hit, err := auth.Authenticate(ctx, addr.IP.String(), addr.Port, creds, time.Second)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if hit == nil {
		t.Fatal("expected hit, got nil")
	}
	if hit.Cred.Pass != "secret" {
		t.Errorf("expected pass=secret, got %q", hit.Cred.Pass)
	}
}

func TestFTP_MissAll(t *testing.T) {
	ln := startFakeFTP(t, "right", false)
	auth := remote.NewFTPAuthenticator()
	addr := ln.Addr().(*net.TCPAddr)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	creds := []credential.Cred{
		{User: "u", Pass: "wrong1", Method: credential.AuthPassword},
		{User: "u", Pass: "wrong2", Method: credential.AuthPassword},
	}
	hit, err := auth.Authenticate(ctx, addr.IP.String(), addr.Port, creds, time.Second)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if hit != nil {
		t.Errorf("expected nil, got %+v", hit)
	}
}

// ─────────────────────────────────────────────────────────────────────
// MySQL authenticator (via go-sql-driver/mysql)
// ─────────────────────────────────────────────────────────────────────

// TestMySQL_ConnectionFails verifies that the authenticator returns
// no hit when the MySQL server (in any form) refuses auth. We use
// an unauthenticated fake server: the driver will fail to connect
// and the authenticator returns nil hit with no error.
//
// TestMySQL_ConnectionFails 验证 MySQL 服务器拒绝认证时 authenticator
// 不返回命中。我们用不接受任何认证的假 server：驱动连不上，authenticator
// 返回 nil hit 且无 error。
func TestMySQL_ConnectionFails(t *testing.T) {
	// Spin up a TCP listener that accepts and immediately closes. The
	// MySQL driver will fail to handshake. / 起一个 TCP listener 接
	// 进来就关。MySQL 驱动会握手失败。
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
	addr := ln.Addr().(*net.TCPAddr)
	auth := database.NewMySQLAuthenticator()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	creds := []credential.Cred{
		{User: "u", Pass: "wrong", Method: credential.AuthPassword},
	}
	hit, err := auth.Authenticate(ctx, addr.IP.String(), addr.Port, creds, time.Second)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if hit != nil {
		t.Errorf("expected nil hit, got %+v", hit)
	}
}

// TestMySQL_RealServer tests the authenticator against a real MySQL
// server if one is available at the configured address. Skips otherwise.
// TestMySQL_RealServer 在配置地址有真 MySQL 时测，否则跳过。
func TestMySQL_RealServer(t *testing.T) {
	addr := "127.0.0.1:13306"
	// First, check if there's a real MySQL listening. / 先检查是否有真 MySQL。
	conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
	if err != nil {
		t.Skipf("no real MySQL at %s: %v", addr, err)
	}
	_ = conn.Close()

	// Try to connect. If the test server doesn't accept the test
	// creds, we just skip — we want a hit, not a miss.
	// 试着连。如果不接受测试 creds，跳过。
	dsn := fmt.Sprintf("root:test@tcp(%s)/?timeout=1s", addr)
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Skipf("sql.Open: %v", err)
	}
	defer db.Close()
	if err := db.Ping(); err != nil {
		t.Skipf("MySQL doesn't accept test creds (expected for unrelated test servers): %v", err)
	}
	auth := database.NewMySQLAuthenticator()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	creds := []credential.Cred{
		{User: "root", Pass: "test", Method: credential.AuthPassword},
	}
	hit, err := auth.Authenticate(ctx, "127.0.0.1", 13306, creds, time.Second)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if hit == nil {
		t.Errorf("expected hit against real MySQL")
	}
}

// ─────────────────────────────────────────────────────────────────────
// helpers
// ─────────────────────────────────────────────────────────────────────
