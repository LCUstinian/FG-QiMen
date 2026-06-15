// Package protocols: SMB authenticator.
// Package protocols：SMB 认证器。
//
// Uses github.com/hirochachacha/go-smb2 (pure-Go SMB2/3 client) to
// perform an SMB2 Session Setup — the SMB equivalent of "Login".
// The Initiator does NTLMv2. We do NOT mount any share, list files,
// or take any post-auth action. On a hit we return the cred; the
// pipeline writes to creds.txt.
//
// 用 github.com/hirochachacha/go-smb2（纯 Go SMB2/3 客户端）做
// SMB2 Session Setup——SMB 的 "Login" 等价动作。Initiator 走 NTLMv2。
// 我们不挂载任何 share、不列文件、不做认证后动作。命中时返回
// cred，由管线写入 creds.txt。
package filestorage

import (
	"context"
	"time"

	smb2 "github.com/hirochachacha/go-smb2"

	"github.com/LCUstinian/FG-QiMen/internal/core/credential"
)

// SMBAuthenticator authenticates against SMB2/3 servers via NTLMv2.
// SMBAuthenticator 通过 NTLMv2 对 SMB2/3 服务器进行认证。
type SMBAuthenticator struct{}

// NewSMBAuthenticator returns a default-configured SMB authenticator.
// NewSMBAuthenticator 返回默认配置的 SMB 认证器。
func NewSMBAuthenticator() *SMBAuthenticator { return &SMBAuthenticator{} }

func init() { credential.Register(NewSMBAuthenticator()) }

// Name implements credential.Authenticator. / Name 实现 credential.Authenticator。
func (a *SMBAuthenticator) Name() string { return "smb" }

// DefaultPorts implements credential.Authenticator. / DefaultPorts 实现 credential.Authenticator。
func (a *SMBAuthenticator) DefaultPorts() []int { return []int{445, 139} }

// Authenticate implements credential.Authenticator. Tries each cred in
// order; returns the first hit or nil.
//
// Authenticate 实现 credential.Authenticator。按顺序尝试每个 cred；首个
// 命中返回，否则返回 nil。
//
// Strategy: for each (user, pass), dial TCP, run go-smb2's
// Session Setup via Dialer.DialContext (which uses the configured
// NTLMInitiator), and treat a non-error result as a hit. We call
// Logoff and close the conn immediately — no share is mounted.
// / 策略：每个 (user, pass) 都 dial TCP，通过 Dialer.DialContext 跑
// go-smb2 的 Session Setup（用配置的 NTLMInitiator），无错即命中。
// 立即 Logoff 并关闭 conn——不挂载任何 share。
func (a *SMBAuthenticator) Authenticate(ctx context.Context, host string, port int, creds []credential.Cred, timeout time.Duration) (*credential.Hit, error) {
	if len(creds) == 0 {
		return nil, nil
	}
	for i, c := range creds {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if c.Method != "" && c.Method != credential.AuthPassword {
			continue
		}
		// SMB usernames are conventionally "DOMAIN\user" or
		// "user@domain". We accept both: if the cred has a `\`,
		// split into domain / user; if it has `@`, split on `@`.
		// Otherwise domain is empty (workstation-only).
		// / SMB 用户名惯例是 "DOMAIN\user" 或 "user@domain"。
		// 两种都接受：cred 含 `\` 就拆 domain/user；含 `@` 就按 `@` 拆；
		// 否则 domain 为空（仅本地工作站）。
		domain, user := splitSMBUser(c.User)
		if user == "" {
			// go-smb2 requires a non-empty user. / go-smb2 要求非空 user。
			user = c.User
		}

		// Dial TCP with the per-attempt timeout (via shared helper).
		// / 用单次超时 dial TCP（共享 helper）。
		tcpConn, err := credential.DialTCP(ctx, host, port, timeout)
		if err != nil {
			// Connection refused / timeout — bail for this host.
			// / 连接拒/超时——该 host 中止。
			return nil, err
		}

		// Build a Dialer with NTLMv2 initiator and run Session
		// Setup. / 构造带 NTLMv2 initiator 的 Dialer 并跑 Session Setup。
		dialer := &smb2.Dialer{
			Initiator: &smb2.NTLMInitiator{
				User:     user,
				Password: c.Pass,
				Domain:   domain,
			},
		}
		sess, err := dialer.DialContext(ctx, tcpConn)
		if err != nil {
			// Session Setup failed — wrong creds OR not an SMB
			// server. Try the next pair. / Session Setup 失败——
			// 错凭据或非 SMB 服务。试下一个。
			_ = tcpConn.Close()
			continue
		}
		// Hard rule: never use the session beyond auth. Logoff
		// and close immediately. / 硬性原则：session 仅用于认证。
		// 立即 Logoff 并关闭。
		_ = sess.Logoff()
		_ = tcpConn.Close()
		return &credential.Hit{
			Cred:     c,
			Attempts: i + 1,
			Time:     time.Now(),
		}, nil
	}
	return nil, nil
}

// splitSMBUser splits "DOMAIN\user" → ("DOMAIN", "user") or
// "user@DOMAIN" → ("DOMAIN", "user"). Returns ("", s) for
// unqualified names. / splitSMBUser 把 "DOMAIN\user" 拆成
// ("DOMAIN", "user")，或 "user@DOMAIN" → ("DOMAIN", "user")。
// 无域的返回 ("", s)。
func splitSMBUser(s string) (domain, user string) {
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' {
			return s[:i], s[i+1:]
		}
	}
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '@' {
			return s[i+1:], s[:i]
		}
	}
	return "", s
}
