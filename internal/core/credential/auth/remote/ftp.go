// Package protocols: FTP authenticator.
// Package protocols: FTP 认证器。
//
// Based on github.com/jlaffaye/ftp (MIT). Connects, attempts Login,
// and returns the result. We do NOT list / transfer / change
// directory — just authentication.
//
// 基于 github.com/jlaffaye/ftp（MIT）。连接，尝试 Login，返回结果。
// 我们不 list / transfer / cd——只做认证。
package remote

import (
	"context"
	"fmt"
	"net"
	"time"

	ftplib "github.com/jlaffaye/ftp"

	"github.com/LCUstinian/FG-QiMen/internal/core/credential"
)

// FTPAuthenticator authenticates against FTP servers.
// FTPAuthenticator 对 FTP 服务器进行认证。
type FTPAuthenticator struct{}

// NewFTPAuthenticator returns a default-configured FTP authenticator.
// NewFTPAuthenticator 返回默认配置的 FTP 认证器。
func NewFTPAuthenticator() *FTPAuthenticator { return &FTPAuthenticator{} }

func init() { credential.Register(NewFTPAuthenticator()) }

// Name implements credential.Authenticator. / Name 实现 credential.Authenticator。
func (a *FTPAuthenticator) Name() string { return "ftp" }

// DefaultPorts implements credential.Authenticator. / DefaultPorts 实现 credential.Authenticator。
func (a *FTPAuthenticator) DefaultPorts() []int { return []int{21, 2121} }

// Authenticate implements credential.Authenticator. Tries each cred in
// order; returns the first hit or nil.
//
// Authenticate 实现 credential.Authenticator。按顺序尝试每个 cred；首个命中
// 返回，否则返回 nil。
func (a *FTPAuthenticator) Authenticate(ctx context.Context, host string, port int, creds []credential.Cred, timeout time.Duration) (*credential.Hit, error) {
	if len(creds) == 0 {
		return nil, nil
	}
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	for i, c := range creds {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if c.Method != "" && c.Method != credential.AuthPassword {
			continue
		}
		if hit, err := ftpTry(ctx, addr, c, timeout); err == nil && hit {
			return &credential.Hit{
				Cred:     c,
				Attempts: i + 1,
				Time:     time.Now(),
			}, nil
		}
	}
	return nil, nil
}

// ftpTry performs one FTP connect+login. We do NOT use any returned
// connection for anything beyond auth — close immediately.
//
// ftpTry 执行一次 FTP 连接+登录。我们不把返回的连接用于认证以外的事
// ——立即关闭。
func ftpTry(ctx context.Context, addr string, c credential.Cred, timeout time.Duration) (bool, error) {
	dialCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	conn, err := ftplib.Dial(addr, ftplib.DialWithContext(dialCtx), ftplib.DialWithTimeout(timeout))
	if err != nil {
		return false, err
	}
	// HARD: do not use the connection for transfer / list. Close now.
	// 硬性：不把连接用于传输/列目录。立即关闭。
	defer func() { _ = conn.Quit() }()
	if err := conn.Login(c.User, c.Pass); err != nil {
		return false, nil
	}
	return true, nil
}
