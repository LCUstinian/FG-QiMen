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
	"sync"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/LCUstinian/FG-QiMen/internal/core/credential"
	"github.com/LCUstinian/FG-QiMen/internal/transport"
)

// SSHAuthenticator authenticates against SSH servers.
// SSHAuthenticator 对 SSH 服务器进行认证。
type SSHAuthenticator struct {
	// hostKeyCB is the SSH host-key verification callback, lazily
	// resolved via transport.SSHHostKeyCallback() on first use so
	// the "no known_hosts" warning only fires when SSH is actually
	// probed (i.e., during a real scan with flags parsed), not
	// during package init in tests that merely import this
	// package. Memoised with sync.Once so the known_hosts file
	// is loaded once per Authenticator, not once per connection.
	// / hostKeyCB 是 SSH 主机密钥验证回调，在首次使用时通过
	// transport.SSHHostKeyCallback() 懒解析，让"无 known_hosts"
	// 警告只在 SSH 实际探测时（也就是带 flag 的真扫描时）出现，
	// 不在仅 import 此包的测试的 init() 期间出现。sync.Once
	// 记忆化，确保 known_hosts 文件每 Authenticator 只加载一次，
	// 不是每次连接加载一次。
	hostKeyCB     ssh.HostKeyCallback
	hostKeyCBOnce sync.Once
}

// NewSSHAuthenticator returns a default-configured SSH authenticator.
// The host-key callback is resolved lazily on first Authenticate
// call, not here — see SSHAuthenticator.hostKeyCB doc for why.
// / NewSSHAuthenticator 返回默认配置的 SSH 认证器。主机密钥回调
// 在首次 Authenticate 调用时懒解析，而不是在这里——见
// SSHAuthenticator.hostKeyCB 文档的说明。
func NewSSHAuthenticator() *SSHAuthenticator {
	return &SSHAuthenticator{}
}

// hostKey returns the SSH host-key callback, resolving it on first
// use. Subsequent calls return the same callback. / hostKey 返回
// SSH 主机密钥回调，首次使用时解析。后续调用返回同一回调。
func (a *SSHAuthenticator) hostKey() ssh.HostKeyCallback {
	a.hostKeyCBOnce.Do(func() {
		a.hostKeyCB = transport.SSHHostKeyCallback()
	})
	return a.hostKeyCB
}

func init() { credential.Register(NewSSHAuthenticator()) }

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
		if hit := sshTry(ctx, addr, c, a.hostKey(), timeout); hit {
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
//
// sshTry takes a pre-built host:port `addr` string (constructed by
// the caller to share the address computation across the
// try-each-cred loop), so it cannot use credential.DialTCP which
// takes (host, port) separately. We use the raw net.Dialer pattern
// here for that one reason; the per-cred try loop still cancels via
// ctx and applies the timeout in the dialer.
//
// sshTry 接收已构造的 host:port `addr` 字符串（由调用方构造以跨
// 凭据循环共享），所以无法用 credential.DialTCP（后者接收 host+port）。
// 本函数因该原因保留 raw net.Dialer；每凭据试连仍通过 ctx 取消
// 并由 dialer 携带 timeout。
func sshTry(ctx context.Context, addr string, c credential.Cred, hkcb ssh.HostKeyCallback, timeout time.Duration) bool {
	// v0.4 Phase 2.2: route through credential.DialTCPAddr so the
	// global --proxy / --socks5 settings apply automatically.
	// Falls back to direct dial if no proxy manager is
	// initialised. / v0.4 Phase 2.2：走 credential.DialTCPAddr 让
	// 全局 --proxy / --socks5 自动生效。无 proxy manager 时回退到
	// 直连。
	conn, err := credential.DialTCPAddr(ctx, addr, timeout)
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
