// Package protocols: SSH authenticator.
// Package protocols: SSH 认证器。
//
// Implements password authentication against an SSH server. Does NOT
// call NewSession / Exec / Shell / etc. — on a hit it returns the
// first successful Cred and the caller is responsible for writing it
// to creds.txt.
//
// 实现对 SSH 服务器的密码认证。不调用 NewSession / Exec / Shell 等。
// 命中时返回首个成功的 Cred，调用方负责写入 creds.txt。
package remote

import (
	"context"
	"fmt"
	"net"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/LCUstinian/FG-QiMen/internal/core/credential"
)

// SSHAuthenticator authenticates against SSH servers.
// SSHAuthenticator 对 SSH 服务器进行认证。
type SSHAuthenticator struct {
	// HostKeyCallback is the SSH host-key verification callback. By
	// default we accept any host key (scanner needs to work against
	// unknown hosts). / HostKeyCallback 是 SSH 主机密钥验证回调。默认
	// 接受任何主机密钥（扫描器要能连未知主机）。
	HostKeyCallback ssh.HostKeyCallback
}

// NewSSHAuthenticator returns a default-configured SSH authenticator.
// NewSSHAuthenticator 返回默认配置的 SSH 认证器。
func NewSSHAuthenticator() *SSHAuthenticator {
	return &SSHAuthenticator{HostKeyCallback: ssh.InsecureIgnoreHostKey()} //nolint:gosec
}

// Name implements credential.Authenticator. / Name 实现 credential.Authenticator。
func (a *SSHAuthenticator) Name() string { return "ssh" }

// DefaultPorts implements credential.Authenticator. / DefaultPorts 实现 credential.Authenticator。
func (a *SSHAuthenticator) DefaultPorts() []int { return []int{22, 2222, 2200, 22222} }

// Authenticate implements credential.Authenticator. Tries each password
// credential in order; returns the first hit or nil.
//
// Authenticate 实现 credential.Authenticator。按顺序尝试每个密码凭据；
// 首个命中返回，否则返回 nil。
func (a *SSHAuthenticator) Authenticate(ctx context.Context, host string, port int, creds []credential.Cred, timeout time.Duration) (*credential.Hit, error) {
	if len(creds) == 0 {
		return nil, nil
	}
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	for i, c := range creds {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		// Only password is supported in v0.1; key-based auth is v0.2+.
		// v0.1 只支持密码认证；key-based 认证留到 v0.2+。
		if c.Method != "" && c.Method != credential.AuthPassword {
			continue
		}
		if hit := sshTry(ctx, addr, c, a.HostKeyCallback, timeout); hit {
			return &credential.Hit{
				Cred:     c,
				Attempts: i + 1,
				RTT:      0, // filled by Authenticate caller
				Time:     time.Now(),
			}, nil
		}
	}
	return nil, nil
}

// sshTry performs a single SSH password authentication. Returns true
// on success. The connection is closed before returning — we never
// open a session.
//
// sshTry 执行一次 SSH 密码认证。成功返回 true。返回前关闭连接——我们
// 从不打开 session。
func sshTry(ctx context.Context, addr string, c credential.Cred, hkcb ssh.HostKeyCallback, timeout time.Duration) bool {
	d := net.Dialer{Timeout: timeout}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return false
	}
	cfg := &ssh.ClientConfig{
		User:            c.User,
		Auth:            []ssh.AuthMethod{ssh.Password(c.Pass)},
		HostKeyCallback: hkcb,
		Timeout:         timeout,
	}
	sshConn, _, _, err := ssh.NewClientConn(conn, addr, cfg)
	if err != nil {
		_ = conn.Close()
		return false
	}
	// HARD: do not use this client for anything. Close immediately.
	// 硬性：不要用此 client 做任何事，立即关闭。
	_ = sshConn.Close()
	return true
}
