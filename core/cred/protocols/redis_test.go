// redis_test.go — unit tests for the Redis authenticator.
// redis_test.go — Redis 认证器的单元测试。
package protocols_test

import (
	"bufio"
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/LCUstinian/FG-QiMen/core/cred"
	"github.com/LCUstinian/FG-QiMen/core/cred/protocols"
)

// startFakeRedis starts a tiny in-process Redis that responds to
// PING / AUTH according to `requirePass`. / startFakeRedis 启动一个
// 进程内的假 Redis，根据 `requirePass` 响应 PING / AUTH。
func startFakeRedis(t *testing.T, requirePass string) net.Listener {
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
			go handleFakeRedis(c, requirePass)
		}
	}()
	t.Cleanup(func() { _ = ln.Close() })
	return ln
}

func handleFakeRedis(c net.Conn, requirePass string) {
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	br := bufio.NewReader(c)
	for {
		args, err := readRESPArgs(br)
		if err != nil {
			return
		}
		if len(args) == 0 {
			continue
		}
		cmd := strings.ToUpper(args[0])
		switch cmd {
		case "PING":
			if requirePass == "" {
				_, _ = c.Write([]byte("+PONG\r\n"))
			} else {
				_, _ = c.Write([]byte("-NOAUTH Authentication required.\r\n"))
			}
		case "AUTH":
			if len(args) < 2 {
				_, _ = c.Write([]byte("-ERR wrong number of args\r\n"))
				continue
			}
			if args[1] == requirePass {
				_, _ = c.Write([]byte("+OK\r\n"))
			} else {
				_, _ = c.Write([]byte("-WRONGPASS invalid username-password pair\r\n"))
			}
		case "QUIT":
			_, _ = c.Write([]byte("+OK\r\n"))
			return
		default:
			_, _ = c.Write([]byte("+OK\r\n"))
		}
	}
}

// readRESPArgs reads one RESP array and returns its string args.
// readRESPArgs 读一个 RESP 数组并返回其字符串 args。
func readRESPArgs(br *bufio.Reader) ([]string, error) {
	line, err := br.ReadString('\n')
	if err != nil {
		return nil, err
	}
	if len(line) == 0 || line[0] != '*' {
		return nil, nil
	}
	// Parse *<count>\r\n
	count := 0
	cr := strings.IndexByte(line, '\r')
	if cr < 0 {
		return nil, nil
	}
	cs := line[1:cr]
	for _, c := range cs {
		if c < '0' || c > '9' {
			return nil, nil
		}
		count = count*10 + int(c-'0')
	}
	args := make([]string, 0, count)
	for i := 0; i < count; i++ {
		hl, err := br.ReadString('\n')
		if err != nil {
			return nil, err
		}
		if len(hl) < 4 || hl[0] != '$' {
			return nil, nil
		}
		ln := 0
		cr := strings.IndexByte(hl, '\r')
		if cr < 0 {
			return nil, nil
		}
		cs := hl[1:cr]
		for _, c := range cs {
			if c < '0' || c > '9' {
				return nil, nil
			}
			ln = ln*10 + int(c-'0')
		}
		data := make([]byte, ln)
		_, err = br.Read(data)
		if err != nil {
			return nil, err
		}
		// Consume trailing \r\n. / 消费尾部 \r\n。
		_, _ = br.ReadString('\n')
		args = append(args, string(data))
	}
	return args, nil
}

// TestRedis_NoPassword verifies that a server without AUTH returns
// a hit on the first try. / TestRedis_NoPassword 验证不需 AUTH 的
// 服务第一次就命中。
func TestRedis_NoPassword(t *testing.T) {
	ln := startFakeRedis(t, "")
	auth := protocols.NewRedisAuthenticator()
	addr := ln.Addr().(*net.TCPAddr)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	creds := []cred.Cred{{User: "", Pass: "anything", Method: cred.AuthPassword}}
	hit, err := auth.Authenticate(ctx, addr.IP.String(), addr.Port, creds, time.Second)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if hit == nil {
		t.Fatal("expected hit on no-password server")
	}
	if hit.Cred.Pass != "" {
		t.Errorf("expected empty pass for no-auth hit, got %q", hit.Cred.Pass)
	}
}

// TestRedis_HitWithCorrectPassword verifies a hit when the right
// password is in the creds list. / TestRedis_HitWithCorrectPassword
// 验证密码对时命中。
func TestRedis_HitWithCorrectPassword(t *testing.T) {
	ln := startFakeRedis(t, "secret")
	auth := protocols.NewRedisAuthenticator()
	addr := ln.Addr().(*net.TCPAddr)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	creds := []cred.Cred{
		{User: "", Pass: "wrong", Method: cred.AuthPassword},
		{User: "", Pass: "secret", Method: cred.AuthPassword},
	}
	hit, err := auth.Authenticate(ctx, addr.IP.String(), addr.Port, creds, time.Second)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if hit == nil {
		t.Fatal("expected hit")
	}
	if hit.Cred.Pass != "secret" {
		t.Errorf("expected pass=secret, got %q", hit.Cred.Pass)
	}
	if hit.Attempts < 2 {
		t.Errorf("expected Attempts>=2 (1 PING + at least 1 AUTH), got %d", hit.Attempts)
	}
}

// TestRedis_MissAll verifies that all-wrong creds return nil.
// / TestRedis_MissAll 验证全错密码返回 nil。
func TestRedis_MissAll(t *testing.T) {
	ln := startFakeRedis(t, "right")
	auth := protocols.NewRedisAuthenticator()
	addr := ln.Addr().(*net.TCPAddr)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	creds := []cred.Cred{
		{User: "", Pass: "wrong1", Method: cred.AuthPassword},
		{User: "", Pass: "wrong2", Method: cred.AuthPassword},
	}
	hit, err := auth.Authenticate(ctx, addr.IP.String(), addr.Port, creds, time.Second)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if hit != nil {
		t.Errorf("expected nil, got %+v", hit)
	}
}

// TestRedis_NotRedis verifies that a non-Redis server (no +PONG /
// -NOAUTH reply) returns nil. / TestRedis_NotRedis 验证非 Redis 服务
// 返回 nil。
func TestRedis_NotRedis(t *testing.T) {
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
			// Send HTTP-like response. Use a write deadline and let
			// the client close — avoids a Windows WSAECONNABORTED
			// race when the server FINs before the client reads.
			// / 发 HTTP 风格响应。设写 deadline 等客户端关闭——避免
			// Windows 上服务器先 FIN 客户端还没读完导致的
			// WSAECONNABORTED 竞态。
			_ = c.SetWriteDeadline(time.Now().Add(2 * time.Second))
			_, _ = c.Write([]byte("HTTP/1.1 200 OK\r\nServer: nginx\r\n\r\n"))
			// Wait for the client to read and close. / 等客户端读完并关。
			_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
			_, _ = c.Read(make([]byte, 1))
			_ = c.Close()
		}
	}()
	auth := protocols.NewRedisAuthenticator()
	addr := ln.Addr().(*net.TCPAddr)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	creds := []cred.Cred{{User: "", Pass: "p", Method: cred.AuthPassword}}
	hit, err := auth.Authenticate(ctx, addr.IP.String(), addr.Port, creds, time.Second)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if hit != nil {
		t.Errorf("expected nil for non-Redis server, got %+v", hit)
	}
}
