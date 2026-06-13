// imap_test.go — unit test for the IMAP credential authenticator.
// / IMAP 凭据认证器的单元测试。
package protocols_test

import (
	"bufio"
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/core/cred"
	"github.com/LCUstinian/FG-QiMen/internal/core/cred/protocols"
)

// fakeIMAP starts a tiny in-process IMAP server. Matches a single
// user/pass. / 启一个最小 IMAP server。匹配单组 user/pass。
func fakeIMAP(t *testing.T, acceptUser, acceptPass string) net.Listener {
	t.Helper()
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go handleFakeIMAP(c, acceptUser, acceptPass)
		}
	}()
	t.Cleanup(func() { _ = ln.Close() })
	return ln
}

func handleFakeIMAP(c net.Conn, acceptUser, acceptPass string) {
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	br := bufio.NewReader(c)
	bw := bufio.NewWriter(c)
	// Greeting. / Greeting。
	_, _ = bw.WriteString("* OK [CAPABILITY IMAP4rev1] ready\r\n")
	_ = bw.Flush()
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		upper := strings.ToUpper(line)
		if strings.HasPrefix(upper, "A1 LOGIN ") {
			rest := line[9:] // skip "A1 LOGIN " (9 chars)
			parts := strings.SplitN(rest, " ", 2)
			if len(parts) == 2 && parts[0] == acceptUser && parts[1] == acceptPass {
				_, _ = bw.WriteString("A1 OK LOGIN completed\r\n")
			} else {
				_, _ = bw.WriteString("A1 NO LOGIN failed\r\n")
			}
			_ = bw.Flush()
		} else if strings.HasPrefix(upper, "A2 LOGOUT") {
			_, _ = bw.WriteString("* BYE\r\nA2 OK LOGOUT completed\r\n")
			_ = bw.Flush()
			return
		}
	}
}

func TestIMAPAuthenticator_RightCred(t *testing.T) {
	ln := fakeIMAP(t, "alice", "secret")
	port := ln.Addr().(*net.TCPAddr).Port
	auth := protocols.NewIMAPAuthenticator()
	creds := []cred.Cred{{User: "alice", Pass: "secret"}}
	hit, err := auth.Authenticate(context.Background(), "127.0.0.1", port, creds, 3*time.Second)
	if err != nil {
		t.Fatalf("auth: %v", err)
	}
	if hit == nil || hit.Cred.User != "alice" {
		t.Fatalf("expected hit for alice, got %+v", hit)
	}
}

func TestIMAPAuthenticator_WrongCred(t *testing.T) {
	ln := fakeIMAP(t, "alice", "secret")
	port := ln.Addr().(*net.TCPAddr).Port
	auth := protocols.NewIMAPAuthenticator()
	creds := []cred.Cred{{User: "alice", Pass: "wrong"}}
	hit, err := auth.Authenticate(context.Background(), "127.0.0.1", port, creds, 3*time.Second)
	if err != nil {
		t.Fatalf("auth: %v", err)
	}
	if hit != nil {
		t.Errorf("expected nil, got %+v", hit)
	}
}

func TestIMAPAuthenticator_EmptyCreds(t *testing.T) {
	auth := protocols.NewIMAPAuthenticator()
	hit, _ := auth.Authenticate(context.Background(), "127.0.0.1", 143, nil, 1*time.Second)
	if hit != nil {
		t.Errorf("expected nil, got %+v", hit)
	}
}

func TestIMAPAuthenticator_ConnRefused(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	auth := protocols.NewIMAPAuthenticator()
	creds := []cred.Cred{{User: "alice", Pass: "secret"}}
	_, err := auth.Authenticate(context.Background(), "127.0.0.1", port, creds, 1*time.Second)
	if err == nil {
		t.Errorf("expected conn error, got nil")
	}
}
