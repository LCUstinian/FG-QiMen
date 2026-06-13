// Package protocols: IMAP authenticator.
//
// Strategy: send LOGIN <user> <pass> per RFC 3501 §6.2.10. Success:
// tagged OK. Failure: tagged NO/BAD. We do NOT issue SELECT/EXAMINE/
// FETCH/SEARCH — cred test only.
//
// HARD RULE: on a hit we LOGOUT and close. No mailbox read.
//
// 包 protocols：IMAP 认证器。
// 策略：按 RFC 3501 §6.2.10 发 LOGIN <user> <pass>。成功：tagged OK。
// 失败：tagged NO/BAD。我们不发 SELECT/EXAMINE/FETCH/SEARCH——只测凭据。
//
// 硬性原则：命中即 LOGOUT 关连接。不读邮箱。
package email

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/core/credential"
)

// IMAPAuthenticator authenticates against IMAP servers via RFC 3501
// LOGIN command. / IMAPAuthenticator 通过 RFC 3501 LOGIN 命令对 IMAP
// 认证。
//
// DefaultPorts returns 143/993 (plaintext / IMAPS). / DefaultPorts
// 返 143/993（明文 / IMAPS）。
type IMAPAuthenticator struct{}

// NewIMAPAuthenticator returns a default IMAP authenticator.
// NewIMAPAuthenticator 返回默认配置的 IMAP 认证器。
func NewIMAPAuthenticator() *IMAPAuthenticator { return &IMAPAuthenticator{} }

// Name implements credential.Authenticator. / Name 实现 credential.Authenticator。
func (a *IMAPAuthenticator) Name() string { return "imap" }

// DefaultPorts implements credential.Authenticator. / DefaultPorts 实现 credential.Authenticator。
func (a *IMAPAuthenticator) DefaultPorts() []int {
	return []int{143, 993}
}

// Authenticate implements credential.Authenticator. / Authenticate 实现
// credential.Authenticator。
func (a *IMAPAuthenticator) Authenticate(ctx context.Context, host string, port int, creds []credential.Cred, timeout time.Duration) (*credential.Hit, error) {
	if len(creds) == 0 {
		return nil, nil
	}
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	for i, c := range creds {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if c.Method != "" && c.Method != credential.AuthPassword {
			continue
		}
		ok, err := a.attempt(ctx, addr, c.User, c.Pass, timeout)
		if err != nil {
			return nil, err
		}
		if ok {
			return &credential.Hit{
				Cred:     c,
				Attempts: i + 1,
				Time:     time.Now(),
			}, nil
		}
	}
	return nil, nil
}

// attempt runs one LOGIN try. / attempt 跑一次 LOGIN 试连。
func (a *IMAPAuthenticator) attempt(ctx context.Context, addr, user, pass string, timeout time.Duration) (bool, error) {
	d := net.Dialer{Timeout: timeout}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return false, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(timeout))
	br := bufio.NewReader(conn)
	bw := bufio.NewWriter(conn)
	// Read server greeting (untagged "* OK ..." or "* OK [CAPABILITY ...]").
	// / 读服务器 greeting（未打 tag 的 "* OK ..." 或 "* OK [CAPABILITY ...]"）。
	greet, err := br.ReadString('\n')
	if err != nil {
		return false, nil
	}
	_ = greet
	// LOGIN command. Tag is "A1" by convention. / LOGIN 命令。tag 用
	// 约定 "A1"。
	if _, err := bw.WriteString(fmt.Sprintf("A1 LOGIN %s %s\r\n", user, pass)); err != nil {
		return false, err
	}
	if err := bw.Flush(); err != nil {
		return false, err
	}
	// Read tagged response. / 读 tagged 响应。
	ok := false
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			break
		}
		trimmed := strings.TrimRight(line, "\r\n")
		if strings.HasPrefix(trimmed, "A1 OK") {
			ok = true
			break
		}
		if strings.HasPrefix(trimmed, "A1 NO") || strings.HasPrefix(trimmed, "A1 BAD") {
			break
		}
		// Untagged responses (e.g. CAPABILITY) — keep reading.
		// / 未打 tag 的响应（如 CAPABILITY）——继续读。
	}
	// LOGOUT regardless. / 无论结果都 LOGOUT。
	_, _ = bw.WriteString("A2 LOGOUT\r\n")
	_ = bw.Flush()
	return ok, nil
}

// init registers the IMAP authenticator. / init 注册 IMAP 认证器。
func init() {
	credential.Register(NewIMAPAuthenticator())
}
