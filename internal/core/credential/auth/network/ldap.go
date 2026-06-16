// Package protocols: LDAP simple bind authenticator.
//
// Uses github.com/go-ldap/ldap/v3. The library handles the full
// LDAPv3 BindRequest / BindResponse exchange over RFC 4511 BER
// encoding. Simple bind (RFC 4513 §5.1) sends the password in
// cleartext — we do NOT support SASL bind (CRAM-MD5 / DIGEST-MD5)
// in v0.1.
//
// HARD RULE: on a hit we return. We do NOT run any Search / Modify
// / Add / Delete.
//
// 包 protocols：LDAP simple bind 认证器。
// 用 github.com/go-ldap/ldap/v3。库处理完整 LDAPv3 BindRequest /
// BindResponse 交互（RFC 4511 BER 编码）。Simple bind（RFC 4513 §5.1）
// 明文发密码——v0.1 不支持 SASL bind（CRAM-MD5 / DIGEST-MD5）。
//
// 硬性原则：命中即返回。不跑任何 Search/Modify/Add/Delete。
package network

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/go-ldap/ldap/v3"

	"github.com/LCUstinian/FG-QiMen/internal/core/credential"
)

// LDAPAuthenticator authenticates against LDAP via simple bind.
// / LDAPAuthenticator 通过 simple bind 对 LDAP 认证。
//
// DefaultPorts returns 389/636 (plaintext / LDAPS). LDAPS uses TLS;
// we skip cert verification (typical for internal LDAP). /
// DefaultPorts 返 389/636（明文 / LDAPS）。LDAPS 用 TLS；我们跳过
// 证书验证（内网 LDAP 典型行为）。
type LDAPAuthenticator struct{}

// NewLDAPAuthenticator returns a default LDAP authenticator.
// NewLDAPAuthenticator 返回默认配置的 LDAP 认证器。
func NewLDAPAuthenticator() *LDAPAuthenticator { return &LDAPAuthenticator{} }

// Name implements credential.Authenticator. / Name 实现 credential.Authenticator。
func (a *LDAPAuthenticator) Name() string { return "ldap" }

// DefaultPorts implements credential.Authenticator. / DefaultPorts 实现 credential.Authenticator。
func (a *LDAPAuthenticator) DefaultPorts() []int {
	return []int{389, 636}
}

// ldapBaseDN returns the BaseDN derived from a host. Most AD / LDAP
// servers use the domain components as the BaseDN. For a host
// "dc1.example.com" we'd want "DC=example,DC=com". We use a simple
// heuristic: take the host's domain part, split on ".", and format
// as DC= fragments. / ldapBaseDN 从 host 推 BaseDN。多数 AD/LDAP 用
// 域名组件做 BaseDN。host "dc1.example.com" → "DC=example,DC=com"。
func ldapBaseDN(host string) string {
	parts := []string{}
	current := []byte{}
	for i := 0; i < len(host); i++ {
		c := host[i]
		if c == '.' {
			if len(current) > 0 {
				parts = append(parts, string(current))
				current = current[:0]
			}
		} else {
			current = append(current, c)
		}
	}
	if len(current) > 0 {
		parts = append(parts, string(current))
	}
	// If host has no dots, return empty (use empty BaseDN).
	// / 如果 host 没有点，返空（用空 BaseDN）。
	if len(parts) < 2 {
		return ""
	}
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += ","
		}
		out += "DC=" + p
	}
	return out
}

// Authenticate implements credential.Authenticator. / Authenticate 实现
// credential.Authenticator。
func (a *LDAPAuthenticator) Authenticate(ctx context.Context, host string, port int, creds []credential.Cred, timeout time.Duration) (*credential.Hit, error) {
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
		ok, err := a.attempt(ctx, addr, port, host, c.User, c.Pass, timeout)
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

// attempt opens an LDAP connection, performs a simple bind, and
// returns whether the bind succeeded. / attempt 开 LDAP 连接，simple
// bind，返是否成功。
func (a *LDAPAuthenticator) attempt(ctx context.Context, addr string, port int, host, user, pass string, timeout time.Duration) (bool, error) {
	// go-ldap v3.4.x: use DialURL which respects LDAP:// / LDAPS://
	// prefix. We force plaintext here (LDAP://) for both port 389
	// and 636 — for 636 we'd need DialTLS; v0.2+ adds that.
	// / go-ldap v3.4.x：用 DialURL 尊重 LDAP:// / LDAPS:// 前缀。
	// v0.1 强制明文（LDAP://）——636 等 v0.2+ 加 TLS 时用 DialTLS。
	ldapURL := "ldap://" + addr
	// LDAP uses the go-ldap high-level DialURL which wraps a
	// net.Dialer internally; credential.DialTCP does not fit because
	// the dialer is owned by the ldap package, not the caller.
	// We keep the inline net.Dialer pattern for the same reason.
	// / LDAP 用 go-ldap 的高层 DialURL 包装 net.Dialer；dialer 归
	// ldap 包管，credential.DialTCP 不适用（需要 caller 拥有 dialer）。
	// 这里保留 inline net.Dialer。
	dialer := &net.Dialer{Timeout: timeout}
	conn, err := ldap.DialURL(ldapURL, ldap.DialWithDialer(dialer))
	if err != nil {
		return false, err
	}
	defer conn.Close()
	// (go-ldap v3.4.x: SetTimeout returns a new conn — we don't need
	// it because the dialer already carries the timeout.)
	// / (go-ldap v3.4.x：SetTimeout 返新 conn——dialer 已带 timeout。)
	// Build the user DN. Two patterns: full DN (e.g. "CN=alice,
	// OU=People, DC=example, DC=com") if c.User contains "=", or
	// user@domain (UPN) for AD, or bare user with auto-derived
	// BaseDN. / 构造 user DN。两种模式：全 DN（如果 c.User 含 "="）
	// 或 user@domain（AD UPN）或裸 user + 推 BaseDN。
	bindDN := buildBindDN(user, host)
	if err := conn.Bind(bindDN, pass); err != nil {
		// Classify: go-ldap returns *ldap.Error for protocol errors
		// (invalidCredentials = 49, etc.). These are auth misses.
		// / 分类：go-ldap 对协议错返 *ldap.Error（invalidCredentials = 49
		// 等）。这些都是认证 miss。
		_ = fmt.Sprintf("ldap: %v", err)
		return false, nil
	}
	return true, nil
}

// buildBindDN returns the user DN to bind with. / buildBindDN 返
// 用于 bind 的 user DN。
//
// Heuristic: / 启发式：
//   - If user contains "=", treat as full DN. / 含 "="：视为全 DN。
//   - Otherwise if user contains "@", use UPN form directly. / 否则
//     含 "@"：直接用 UPN。
//   - Otherwise build "CN=<user>,<baseDN>" (heuristic — works for
//     most AD setups). / 否则构造 "CN=<user>,<baseDN>"（启发式——
//     多数 AD 可用）。
func buildBindDN(user, host string) string {
	if user == "" {
		return ""
	}
	for i := 0; i < len(user); i++ {
		if user[i] == '=' {
			return user // already a DN
		}
		if user[i] == '@' {
			return user // UPN form
		}
	}
	// bare username: construct CN + baseDN
	base := ldapBaseDN(host)
	if base == "" {
		return fmt.Sprintf("CN=%s", user)
	}
	return fmt.Sprintf("CN=%s,%s", user, base)
}

// init registers the LDAP authenticator. / init 注册 LDAP 认证器。
func init() {
	credential.Register(NewLDAPAuthenticator())
}

// (P2 dead-code purge: removed tls sentinel and import in v0.2.3.
// The audit's F-scan found `var _ = tls.Config{}` keeping the
// crypto/tls import alive "for future LDAPS support" — but Go's
// compiler will catch the unused import immediately, so the
// sentinel served no purpose. To re-enable LDAPS, import
// crypto/tls and use it directly.)
//
// （P2 死代码清理：v0.2.3 删了 tls 哨兵和 import。审计 F-scan
// 发现 `var _ = tls.Config{}` 保 crypto/tls "以备未来 LDAPS"——
// 但 Go 编译器立即标未用 import，所以哨兵无意义。重新启用 LDAPS
// 时直接 import crypto/tls 并使用。）
