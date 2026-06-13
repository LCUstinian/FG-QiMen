// rsync_test.go — unit test for the Rsync credential authenticator.
// / Rsync 凭据认证器的单元测试。
//
// Rsync's authentication is "challenge-response": server sends 16-byte
// challenge; client XORs MD5 of (challenge || password) 16 times and
// returns the result. We test the success/miss boundary by mocking
// a server that accepts one user/pass pair. / Rsync 认证是"challenge-
// response"：服务器发 16 字节 challenge；客户端把 (challenge || password)
// 的 MD5 XOR 16 次后返结果。我们 mock 一个接受单组 user/pass 的服务
// 器来测 success/miss 边界。
package protocols_test

import (
	"bufio"
	"context"
	"crypto/md5"
	"encoding/binary"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/LCUstinian/FG-QiMen/core/cred"
	"github.com/LCUstinian/FG-QiMen/core/cred/protocols"
)

// fakeRsync starts a tiny in-process rsync daemon that accepts one
// (user, pass) pair. / fakeRsync 启一个最小 rsync 守护进程，接受
// 单组 (user, pass)。
func fakeRsync(t *testing.T, acceptUser, acceptPass string) net.Listener {
	t.Helper()
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go handleFakeRsync(c, acceptUser, acceptPass)
		}
	}()
	t.Cleanup(func() { _ = ln.Close() })
	return ln
}

func handleFakeRsync(c net.Conn, acceptUser, acceptPass string) {
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	br := bufio.NewReader(c)
	bw := bufio.NewWriter(c)
	// Server greeting: "@RSYNCD: 31.0\n". / 服务器 greeting。
	_, _ = bw.WriteString("@RSYNCD: 31.0\n")
	_ = bw.Flush()
	// Read client greeting. / 读客户端 greeting。
	greet, err := br.ReadString('\n')
	if err != nil {
		return
	}
	_ = greet
	// Server sends "USERNAME\n" or "PASSWORD\n" sequence. / 服务器发
	// "USERNAME\n" 或 "PASSWORD\n" 序列。
	_, _ = bw.WriteString("USERNAME\n")
	_ = bw.Flush()
	user, err := br.ReadString('\n')
	if err != nil {
		return
	}
	user = strings.TrimRight(user, "\r\n")
	if user != acceptUser {
		// Reject. / 拒绝。
		_, _ = bw.WriteString("@ERROR: auth failed\n")
		_ = bw.Flush()
		return
	}
	// Send 16-byte challenge. / 发 16 字节 challenge。
	challenge := make([]byte, 16)
	for i := range challenge {
		challenge[i] = byte(i)
	}
	_, _ = c.Write(challenge)
	// Read 16-byte response. / 读 16 字节响应。
	resp := make([]byte, 16)
	if _, err := readFullRS(c, resp); err != nil {
		return
	}
	// Verify: MD5(challenge || password), XOR'd 16 times.
	// / 验证：MD5(challenge || password)，XOR 16 次。
	expected := rsyncHash(acceptPass, challenge)
	for i := 0; i < 16; i++ {
		if resp[i] != expected[i] {
			_, _ = bw.WriteString("@ERROR: auth failed\n")
			_ = bw.Flush()
			return
		}
	}
	_, _ = bw.WriteString("@RSYNCD: OK\n")
	_ = bw.Flush()
}

func readFullRS(c net.Conn, buf []byte) (int, error) {
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

// rsyncHash implements the rsync MD5 challenge-response (matches
// the impl in core/cred/protocols/rsync.go). / rsyncHash 实现 rsync
// MD5 challenge-response（跟 core/cred/protocols/rsync.go 的实现一致）。
func rsyncHash(pass string, challenge []byte) []byte {
	h := md5.New()
	h.Write(challenge)
	h.Write([]byte(pass))
	sum := h.Sum(nil)
	swapped := make([]byte, 16)
	for i := 0; i < 4; i++ {
		v := binary.LittleEndian.Uint32(sum[i*4 : i*4+4])
		for j := 0; j < 4; j++ {
			swapped[i*4+j] = byte(v >> (24 - j*8))
		}
	}
	out := make([]byte, 16)
	out[0] = swapped[0]
	for i := 1; i < 16; i++ {
		out[i] = swapped[i] ^ swapped[i-1]
	}
	return out
}

func TestRsyncAuthenticator_RightCred(t *testing.T) {
	ln := fakeRsync(t, "alice", "secret")
	port := ln.Addr().(*net.TCPAddr).Port
	auth := protocols.NewRsyncAuthenticator()
	creds := []cred.Cred{{User: "alice", Pass: "secret"}}
	hit, err := auth.Authenticate(context.Background(), "127.0.0.1", port, creds, 3*time.Second)
	if err != nil {
		t.Fatalf("auth: %v", err)
	}
	if hit == nil {
		t.Fatalf("expected hit, got nil")
	}
}

func TestRsyncAuthenticator_WrongCred(t *testing.T) {
	ln := fakeRsync(t, "alice", "secret")
	port := ln.Addr().(*net.TCPAddr).Port
	auth := protocols.NewRsyncAuthenticator()
	creds := []cred.Cred{{User: "alice", Pass: "wrong"}}
	hit, _ := auth.Authenticate(context.Background(), "127.0.0.1", port, creds, 3*time.Second)
	if hit != nil {
		t.Errorf("expected nil, got %+v", hit)
	}
}

func TestRsyncAuthenticator_EmptyCreds(t *testing.T) {
	auth := protocols.NewRsyncAuthenticator()
	hit, _ := auth.Authenticate(context.Background(), "127.0.0.1", 873, nil, 1*time.Second)
	if hit != nil {
		t.Errorf("expected nil, got %+v", hit)
	}
}

func TestRsyncAuthenticator_ConnRefused(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	auth := protocols.NewRsyncAuthenticator()
	creds := []cred.Cred{{User: "alice", Pass: "secret"}}
	_, err := auth.Authenticate(context.Background(), "127.0.0.1", port, creds, 1*time.Second)
	if err == nil {
		t.Errorf("expected conn error, got nil")
	}
}

// Unused but keeps fmt import alive. / fmt 保留。
var _ = fmt.Sprintf
