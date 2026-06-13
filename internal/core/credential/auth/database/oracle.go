// Package protocols: Oracle authenticator.
//
// Uses github.com/sijms/go-ora/v2 (a pure-Go Oracle database driver).
// Strategy: build a go-ora DSN, sql.Open, PingContext. The driver
// performs the TNS Connect / Accept / auth handshake internally. On
// success the connection is ready; we close it. We do NOT run any
// query (no SELECT banner, no SELECT version, no SHOW).
//
// HARD RULE: on a hit we return. We do NOT run any post-auth command.
//
// 包 protocols：Oracle 认证器。
// 用 github.com/sijms/go-ora/v2（纯 Go Oracle 数据库驱动）。
// 策略：构造 go-ora DSN，sql.Open，PingContext。驱动内部跑 TNS
// Connect/Accept/auth 握手。命中即关连接。我们不跑任何 query（不
// SELECT banner、不 SELECT version、不 SHOW）。
//
// 硬性原则：命中即返回，不跑任何认证后命令。
package database

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	_ "github.com/sijms/go-ora/v2" // Oracle driver (registers "oracle" with database/sql)

	"github.com/LCUstinian/FG-QiMen/internal/core/credential"
)

// OracleAuthenticator authenticates against Oracle via go-ora.
//
// DefaultPorts returns 1521/1526/2483 (TNS / SSL TNS / MTS).
//
// OracleAuthenticator 通过 go-ora 对 Oracle 认证。
//
// DefaultPorts 返 1521/1526/2483（TNS / SSL TNS / MTS）。
type OracleAuthenticator struct{}

// NewOracleAuthenticator returns a default Oracle authenticator.
// NewOracleAuthenticator 返回默认配置的 Oracle 认证器。
func NewOracleAuthenticator() *OracleAuthenticator { return &OracleAuthenticator{} }

// Name implements credential.Authenticator. / Name 实现 credential.Authenticator。
func (a *OracleAuthenticator) Name() string { return "oracle" }

// DefaultPorts implements credential.Authenticator. / DefaultPorts 实现 credential.Authenticator。
func (a *OracleAuthenticator) DefaultPorts() []int {
	return []int{1521, 1526, 2483}
}

// Authenticate implements credential.Authenticator. Tries each cred in
// order; returns the first hit or nil.
//
// Authenticate 实现 credential.Authenticator。按顺序尝试每个 cred；首个命中
// 返回 Hit，否则返回 nil。
//
// Strategy: build a go-ora DSN, open via database/sql, call
// PingContext. The driver's first network use performs the TNS
// Connect/Accept + auth — successful Ping = successful auth. Close
// immediately. We never issue a query.
//
// 策略：构造 go-ora DSN，通过 database/sql 打开，调 PingContext。
// 驱动首次网络使用时跑 TNS Connect/Accept + auth——Ping 成功 =
// auth 成功。立即关连接。不发任何 query。
func (a *OracleAuthenticator) Authenticate(ctx context.Context, host string, port int, creds []credential.Cred, timeout time.Duration) (*credential.Hit, error) {
	if len(creds) == 0 {
		return nil, nil
	}
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	timeoutSec := int64(timeout.Seconds())
	if timeoutSec < 1 {
		timeoutSec = 1
	}
	for i, c := range creds {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if c.Method != "" && c.Method != credential.AuthPassword {
			continue
		}
		// Fall back to "system" (Oracle default admin) if c.User is empty.
		// / c.User 空时回退到 "system"（Oracle 默认管理员）。
		user := c.User
		if user == "" {
			user = "system"
		}
		// Build DSN. The go-ora format is "oracle://user:pass@host:port/service".
		// / 构造 DSN。go-ora 格式是 "oracle://user:pass@host:port/service"。
		// url.UserPassword escapes special chars. / url.UserPassword 转义特殊字符。
		u := &url.URL{
			Scheme: "oracle",
			User:   url.UserPassword(user, c.Pass),
			Host:   addr,
			Path:   "ORCL", // default service name; can be overridden via opts later
		}
		// go-ora also supports "dial_timeout" and "connect_timeout"
		// in its options. We pass a 30s server-side timeout; the
		// per-attempt ctx timeout is enforced by the driver. / go-ora
		// 也支持 "dial_timeout" 和 "connect_timeout"。我们传 30s
		// 服务器端超时；单次 ctx 超时由驱动强制。
		q := u.Query()
		q.Set("connect_timeout", strconv.FormatInt(timeoutSec, 10))
		u.RawQuery = q.Encode()
		db, err := sql.Open("oracle", u.String())
		if err != nil {
			continue
		}
		db.SetConnMaxLifetime(timeout)
		db.SetMaxOpenConns(1)
		db.SetMaxIdleConns(0)
		pingCtx, cancel := context.WithTimeout(ctx, timeout)
		err = db.PingContext(pingCtx)
		cancel()
		_ = db.Close()
		if err == nil {
			return &credential.Hit{
				Cred:     c,
				Attempts: i + 1,
				Time:     time.Now(),
			}, nil
		}
		// Classify: protocol errors (ORA-xxxxx codes) are auth misses,
		// try next. Network errors mean the host is unreachable — bail
		// out so the caller sees "host down" not a silent miss. / 错误
		// 分类：协议错（ORA-xxxxx 码）是认证 miss，试下一个。网络错意味
		// 主机不可达——退出，让调用方看到"主机不通"而不是静默 miss。
		if !isOracleProtocolError(err) {
			return nil, err
		}
		_ = fmt.Sprintf("oracle: %v", err)
	}
	return nil, nil
}

// init registers the Oracle authenticator. / init 注册 Oracle 认证器。
func init() {
	credential.Register(NewOracleAuthenticator())
}

// isOracleProtocolError returns true if err is a server-replied
// error (ORA-xxxxx) rather than a network failure. Oracle errors
// from go-ora typically look like "ORA-01017: invalid username/
// password" or contain "ORA-" prefix.
//
// isOracleProtocolError 当 err 是服务器错误（ORA-xxxxx）而非网络错
// 时返 true。go-ora 的 Oracle 错误形如 "ORA-01017: invalid username/
// password" 或含 "ORA-" 前缀。
func isOracleProtocolError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	if strings.Contains(msg, "ORA-") {
		return true
	}
	return false
}
