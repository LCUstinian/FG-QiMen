// socks5_test.go — unit test for the SOCKS5 credential authenticator.
// / SOCKS5 凭据认证器的单元测试。
package network

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/core/credential"
)

// fakeSOCKS5 starts a tiny in-process SOCKS5 proxy that accepts
// user/pass auth for one (user, pass) pair. / 启一个最小 SOCKS5 代理，
// 接受单组 user/pass。
func fakeSOCKS5(t *testing.T, acceptUser, acceptPass string) net.Listener {
	t.Helper()
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go handleFakeSOCKS5(c, acceptUser, acceptPass)
		}
	}()
	t.Cleanup(func() { _ = ln.Close() })
	return ln
}

func handleFakeSOCKS5(c net.Conn, acceptUser, acceptPass string) {
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	// Greeting selection. / Greeting 选择。
	greet := make([]byte, 3)
	if _, err := readFullS5(c, greet); err != nil {
		return
	}
	if greet[0] != 0x05 || greet[1] != 0x01 {
		return
	}
	// Pick the requested method (assume 0x02 user/pass).
	// / 选请求的 method（假设 0x02 user/pass）。
	if greet[2] == 0x02 {
		_, _ = c.Write([]byte{0x05, 0x02})
	} else {
		_, _ = c.Write([]byte{0x05, 0xFF})
		return
	}
	// User/pass auth. / User/pass auth。
	hdr := make([]byte, 2)
	if _, err := readFullS5(c, hdr); err != nil {
		return
	}
	if hdr[0] != 0x01 {
		return
	}
	ulen := int(hdr[1])
	uname := make([]byte, ulen)
	if _, err := readFullS5(c, uname); err != nil {
		return
	}
	plenByte := make([]byte, 1)
	if _, err := readFullS5(c, plenByte); err != nil {
		return
	}
	plen := int(plenByte[0])
	pwd := make([]byte, plen)
	if _, err := readFullS5(c, pwd); err != nil {
		return
	}
	if string(uname) == acceptUser && string(pwd) == acceptPass {
		_, _ = c.Write([]byte{0x01, 0x00})
	} else {
		_, _ = c.Write([]byte{0x01, 0x01})
	}
}

func readFullS5(c net.Conn, buf []byte) (int, error) {
	total := 0
	for total < len(buf) {
		n, err := c.Read(buf[total:])
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}

func TestSOCKS5Authenticator_RightCred(t *testing.T) {
	ln := fakeSOCKS5(t, "alice", "secret")
	port := ln.Addr().(*net.TCPAddr).Port
	auth := NewSOCKS5Authenticator()
	creds := []credential.Cred{{User: "alice", Pass: "secret"}}
	hit, err := auth.Authenticate(context.Background(), "127.0.0.1", port, creds, 3*time.Second)
	if err != nil {
		t.Fatalf("auth: %v", err)
	}
	if hit == nil || hit.Cred.User != "alice" {
		t.Fatalf("expected hit for alice, got %+v", hit)
	}
}

func TestSOCKS5Authenticator_WrongCred(t *testing.T) {
	ln := fakeSOCKS5(t, "alice", "secret")
	port := ln.Addr().(*net.TCPAddr).Port
	auth := NewSOCKS5Authenticator()
	creds := []credential.Cred{{User: "alice", Pass: "wrong"}}
	hit, _ := auth.Authenticate(context.Background(), "127.0.0.1", port, creds, 3*time.Second)
	if hit != nil {
		t.Errorf("expected nil, got %+v", hit)
	}
}

func TestSOCKS5Authenticator_EmptyCreds(t *testing.T) {
	auth := NewSOCKS5Authenticator()
	hit, _ := auth.Authenticate(context.Background(), "127.0.0.1", 1080, nil, 1*time.Second)
	if hit != nil {
		t.Errorf("expected nil, got %+v", hit)
	}
}

func TestSOCKS5Authenticator_ConnRefused(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	auth := NewSOCKS5Authenticator()
	creds := []credential.Cred{{User: "alice", Pass: "secret"}}
	_, err := auth.Authenticate(context.Background(), "127.0.0.1", port, creds, 1*time.Second)
	if err == nil {
		t.Errorf("expected conn error, got nil")
	}
}
