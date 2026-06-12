// Package protocols: MSSQL authenticator.
// Package protocols：MSSQL 认证器。
//
// Uses github.com/microsoft/go-mssqldb (Microsoft's official Go
// driver for SQL Server / Azure SQL). Just open a connection and
// Ping — the driver handles the full TDS 7.x handshake including
// the Login7 packet, encryption negotiation, and auth.
//
// 用 github.com/microsoft/go-mssqldb（Microsoft 官方 Go 驱动）。
// 仅需打开连接并 Ping——驱动处理完整 TDS 7.x 握手，包括 Login7 包、
// 加密协商和认证。
//
// We do NOT run any query (no SELECT / USE / EXEC). Just authenticate.
// 我们不执行任何查询（不 SELECT / USE / EXEC）——只认证。
package protocols

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	mssqllib "github.com/microsoft/go-mssqldb"

	"github.com/LCUstinian/FG-QiMen/core/cred"
)

// MSSQLAuthenticator authenticates against Microsoft SQL Server /
// Azure SQL via the official go-mssqldb driver.
// MSSQLAuthenticator 通过官方 go-mssqldb 驱动对 Microsoft SQL Server
// / Azure SQL 进行认证。
type MSSQLAuthenticator struct{}

// NewMSSQLAuthenticator returns a default-configured MSSQL authenticator.
// NewMSSQLAuthenticator 返回默认配置的 MSSQL 认证器。
func NewMSSQLAuthenticator() *MSSQLAuthenticator { return &MSSQLAuthenticator{} }

// Name implements cred.Authenticator. / Name 实现 cred.Authenticator。
func (a *MSSQLAuthenticator) Name() string { return "mssql" }

// DefaultPorts implements cred.Authenticator. / DefaultPorts 实现 cred.Authenticator。
func (a *MSSQLAuthenticator) DefaultPorts() []int { return []int{1433, 1434, 2433} }

// Authenticate implements cred.Authenticator. Tries each cred in
// order; returns the first hit or nil.
//
// Authenticate 实现 cred.Authenticator。按顺序尝试每个 cred；首个命中
// 返回，否则返回 nil。
//
// Strategy: build a sqlserver:// DSN, open via database/sql, and call
// PingContext. The driver's first call to the underlying connection
// performs the TDS Login7 — successful Ping = successful Login. We
// close the connection immediately; we never issue a query.
// / 策略：构造 sqlserver:// DSN，通过 database/sql 打开，调
// PingContext。驱动第一次用底层连接时执行 TDS Login7——Ping 成功 =
// Login 成功。立即关闭连接；不发任何查询。
func (a *MSSQLAuthenticator) Authenticate(ctx context.Context, host string, port int, creds []cred.Cred, timeout time.Duration) (*cred.Hit, error) {
	if len(creds) == 0 {
		return nil, nil
	}
	addr := fmt.Sprintf("%s:%d", host, port)
	timeoutSec := int64(timeout.Seconds())
	if timeoutSec < 1 {
		timeoutSec = 1
	}
	for i, c := range creds {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if c.Method != "" && c.Method != cred.AuthPassword {
			continue
		}
		// Build DSN. We pin the login timeout (driver-level) and the
		// dial timeout (connect-level). The driver performs Login7
		// when the first query is sent — PingContext is the trigger.
		// / 构造 DSN。设登录超时（驱动级）和拨号超时（连接级）。驱动在
		// 首次发查询时执行 Login7——PingContext 是触发点。
		//
		// escape="none" lets us pass arbitrary chars in user/pass
		// without the driver trying to URL-encode them again. We
		// control the DSN, so this is safe.
		// / escape="none" 让任意字符的用户名/密码不再被驱动 URL 编码。
		// 我们控制 DSN，是安全的。
		query := fmt.Sprintf("?database=master&dial+timeout=%d&login+timeout=%d&encrypt=disable&trustservercertificate=true&escape=none",
			timeoutSec, timeoutSec)
		dsn := fmt.Sprintf("sqlserver://%s:%s@%s%s",
			urlUser(c.User), urlPass(c.Pass), addr, query)
		db, err := sql.Open("sqlserver", dsn)
		if err != nil {
			// Invalid DSN format etc. — try next.
			// / DSN 格式无效等——试下一个。
			continue
		}
		db.SetConnMaxLifetime(timeout)
		db.SetMaxOpenConns(1)
		db.SetMaxIdleConns(0)
		// Ping triggers the TDS Login7 handshake via go-mssqldb.
		// / Ping 触发 go-mssqldb 的 TDS Login7 握手。
		pingCtx, cancel := context.WithTimeout(ctx, timeout)
		err = db.PingContext(pingCtx)
		cancel()
		_ = db.Close()
		if err == nil {
			return &cred.Hit{
				Cred:     c,
				Attempts: i + 1,
				Time:     time.Now(),
			}, nil
		}
		// On Login failure, the driver returns an mssql.Error or
		// net error. We treat all of them as "wrong cred" and try
		// the next pair. / Login 失败时驱动返 mssql.Error 或网络错。
		// 全部视为"错凭据"并试下一个。
		var mssqlErr *mssqllib.Error
		_ = mssqlErr // not branching on type — any error is a miss
	}
	return nil, nil
}

// urlUser / urlPass return a URL-safe form of the credentials. The
// mssql driver's url package is unexported, so we re-implement the
// minimal escaping (percent-encoding for the small set of characters
// the TDS parser cares about).
//
// urlUser / urlPass 返回凭据的 URL 安全形式。mssql 驱动的 url 包未导出，
// 所以我们重做最小转义（对 TDS 解析器关心的小集合字符做 percent-encoding）。
func urlUser(s string) string { return urlEscape(s) }
func urlPass(s string) string { return urlEscape(s) }
func urlEscape(s string) string {
	// From net/url.QueryEscape semantics, but for username/password
	// we don't need to escape '+' and '=' which the driver would
	// otherwise decode as space. / 同 net/url.QueryEscape 语义，但
	// 用户名/密码不需要把 '+' 和 '=' 转义（驱动会解成空格）。
	const upperhex = "0123456789ABCDEF"
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case 'a' <= c && c <= 'z', 'A' <= c && c <= 'Z', '0' <= c && c <= '9',
			c == '-' || c == '_' || c == '.' || c == '~':
			out = append(out, c)
		default:
			out = append(out, '%', upperhex[c>>4], upperhex[c&15])
		}
	}
	return string(out)
}
