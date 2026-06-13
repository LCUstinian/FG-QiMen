// pop3_test.go — unit test for the POP3 credential authenticator.
// / POP3 凭据认证器的单元测试。
package email

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/core/credential"
)

// fakePOP3 starts a tiny in-process POP3 server. If `acceptUser`/
// `acceptPass` match the client's USER/PASS it returns +OK; otherwise
// -ERR. / 启一个最小 POP3 server。凭据匹配返 +OK，否则 -ERR。
func fakePOP3(t *testing.T, acceptUser, acceptPass string) net.Listener {
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
			go handleFakePOP3(c, acceptUser, acceptPass)
		}
	}()
	t.Cleanup(func() { _ = ln.Close() })
	return ln
}

func handleFakePOP3(c net.Conn, acceptUser, acceptPass string) {
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	br := bufio.NewReader(c)
	bw := bufio.NewWriter(c)
	// Greeting. / Greeting。
	_, _ = bw.WriteString("+OK POP3 server ready\r\n")
	_ = bw.Flush()
	var user string
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		upper := strings.ToUpper(line)
		switch {
		case strings.HasPrefix(upper, "USER "):
			user = line[5:]
			_, _ = bw.WriteString("+OK\r\n")
		case strings.HasPrefix(upper, "PASS "):
			pass := line[5:]
			if user == acceptUser && pass == acceptPass {
				_, _ = bw.WriteString("+OK\r\n")
			} else {
				_, _ = bw.WriteString("-ERR invalid credentials\r\n")
				_ = bw.Flush()
				return
			}
		case strings.HasPrefix(upper, "QUIT"):
			_, _ = bw.WriteString("+OK bye\r\n")
			_ = bw.Flush()
			return
		default:
			_, _ = bw.WriteString("-ERR unknown command\r\n")
		}
		_ = bw.Flush()
	}
}

func TestPOP3Authenticator_RightCred(t *testing.T) {
	ln := fakePOP3(t, "alice", "secret")
	port := ln.Addr().(*net.TCPAddr).Port
	auth := NewPOP3Authenticator()
	creds := []credential.Cred{{User: "alice", Pass: "secret"}}
	hit, err := auth.Authenticate(context.Background(), "127.0.0.1", port, creds, 3*time.Second)
	if err != nil {
		t.Fatalf("auth: %v", err)
	}
	if hit == nil || hit.Cred.User != "alice" {
		t.Fatalf("expected hit for alice, got %+v", hit)
	}
}

func TestPOP3Authenticator_WrongCred(t *testing.T) {
	ln := fakePOP3(t, "alice", "secret")
	port := ln.Addr().(*net.TCPAddr).Port
	auth := NewPOP3Authenticator()
	creds := []credential.Cred{{User: "bob", Pass: "wrong"}}
	hit, err := auth.Authenticate(context.Background(), "127.0.0.1", port, creds, 3*time.Second)
	if err != nil {
		t.Fatalf("auth: %v", err)
	}
	if hit != nil {
		t.Errorf("expected nil, got %+v", hit)
	}
}

func TestPOP3Authenticator_EmptyCreds(t *testing.T) {
	auth := NewPOP3Authenticator()
	hit, err := auth.Authenticate(context.Background(), "127.0.0.1", 110, nil, 1*time.Second)
	if err != nil {
		t.Fatalf("auth: %v", err)
	}
	if hit != nil {
		t.Errorf("expected nil, got %+v", hit)
	}
}

func TestPOP3Authenticator_ConnRefused(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	auth := NewPOP3Authenticator()
	creds := []credential.Cred{{User: "alice", Pass: "secret"}}
	_, err := auth.Authenticate(context.Background(), "127.0.0.1", port, creds, 1*time.Second)
	if err == nil {
		t.Errorf("expected conn error, got nil")
	}
}

// Unused but keeps fmt import alive for future debug logging.
// / 保留 fmt 供将来 debug 用。
var _ = fmt.Sprintf
