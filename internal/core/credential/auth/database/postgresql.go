// Package protocols: PostgreSQL authenticator.
//
// Uses github.com/lib/pq (the canonical PostgreSQL driver for database/sql).
// Strategy: build a DSN, sql.Open, call PingContext. The driver performs
// the full StartupMessage + SCRAM-SHA-256/MD5/cleartext auth handshake
// internally. We do NOT run any query (no SELECT version(), no
// SELECT current_database()).
//
// HARD RULE: on a hit we return. We do NOT run any post-auth command.
//
// 包 protocols：PostgreSQL 认证器。
// 用 github.com/lib/pq（database/sql 的规范 PG 驱动）。
// 策略：构造 DSN，sql.Open，调 PingContext。驱动内部跑完 StartupMessage
// + SCRAM-SHA-256/MD5/cleartext 认证。我们不跑任何 query（不 SELECT
// version()、不 SELECT current_database()）。
//
// 硬性原则：命中即返回，不跑任何认证后命令。
package database

import (
	"context"
	"database/sql"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"

	_ "github.com/lib/pq" // PostgreSQL driver (registers "postgres" with database/sql)

	"github.com/LCUstinian/FG-QiMen/internal/core/credential"
	"github.com/LCUstinian/FG-QiMen/internal/transport"
)

// PostgreSQLAuthenticator authenticates against PostgreSQL via lib/pq.
//
// DefaultPorts returns 5432/5433/5434. 5434 covers PG replication
// manager / backup port; the existing Identify plugin only lists 5432/5433.
//
// PostgreSQLAuthenticator 通过 lib/pq 对 PostgreSQL 认证。
//
// DefaultPorts 返 5432/5433/5434。5434 覆盖 PG 复制管理/备份端口；现有
// Identify 插件只列了 5432/5433。
type PostgreSQLAuthenticator struct{}

// NewPostgreSQLAuthenticator returns a default-configured PostgreSQL authenticator.
// NewPostgreSQLAuthenticator 返回默认配置的 PostgreSQL 认证器。
func NewPostgreSQLAuthenticator() *PostgreSQLAuthenticator { return &PostgreSQLAuthenticator{} }

// Name implements credential.Authenticator. / Name 实现 credential.Authenticator。
func (a *PostgreSQLAuthenticator) Name() string { return "postgresql" }

// DefaultPorts implements credential.Authenticator. / DefaultPorts 实现 credential.Authenticator。
func (a *PostgreSQLAuthenticator) DefaultPorts() []int {
	return []int{5432, 5433, 5434}
}

// Authenticate implements credential.Authenticator. Tries each cred in order;
// returns the first hit or nil.
//
// Authenticate 实现 credential.Authenticator。按顺序尝试每个 cred；首个命中
// 返回 Hit，否则返回 nil。
//
// Strategy: build a postgres:// DSN, open via database/sql, call
// PingContext. The driver's first network use performs the StartupMessage
// + auth — successful Ping = successful auth. We close the connection
// immediately; we never issue a query.
//
// 策略：构造 postgres:// DSN，通过 database/sql 打开，调 PingContext。
// 驱动首次网络使用时跑 StartupMessage + auth——Ping 成功 = 认证成功。
// 立即关闭连接；不发任何 query。
func (a *PostgreSQLAuthenticator) Authenticate(ctx context.Context, host string, port int, creds []credential.Cred, timeout time.Duration) (*credential.Hit, error) {
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
		// Fall back to "postgres" (PG install default superuser) if c.User is empty.
		// / c.User 空时回退到 "postgres"（PG 安装默认超级用户）。
		user := c.User
		if user == "" {
			user = "postgres"
		}
		// Build DSN. / 构造 DSN。
		// net.JoinHostPort already wraps IPv6 hosts in [...]. The url.UserPassword
		// call escapes special chars in user/pass per RFC 3986.
		// / net.JoinHostPort 已经把 IPv6 host 包成 "[...]"。url.UserPassword
		// 按 RFC 3986 转义 user/pass 的特殊字符。
		u := &url.URL{
			Scheme: "postgres",
			User:   url.UserPassword(user, c.Pass),
			Host:   addr,
			Path:   "postgres",
		}
		q := u.Query()
		// M15: negotiate TLS instead of forcing plaintext.
		// - Default (verify): sslmode=prefer — try TLS with cert
		//   verification, fall back to plaintext if TLS unavailable.
		// - --insecure-tls: sslmode=require — require TLS but skip CA
		//   verification (lib/pq's "require" doesn't verify the chain).
		// / M15：协商 TLS 而非强制明文。
		// - 默认（校验）：sslmode=prefer——试 TLS 带证书校验，TLS 不可
		//   用则退到明文。
		// - --insecure-tls：sslmode=require——要求 TLS 但跳过 CA 校验
		//   （lib/pq 的 "require" 不校验链）。
		sslmode := "prefer"
		if transport.InsecureTLS.Load() {
			sslmode = "require"
		}
		q.Set("sslmode", sslmode)
		q.Set("connect_timeout", strconv.FormatInt(timeoutSec, 10))
		u.RawQuery = q.Encode()

		db, err := sql.Open("postgres", u.String())
		if err != nil {
			// Invalid DSN format etc. — try next. / DSN 格式无效等——试下一个。
			continue
		}
		db.SetConnMaxLifetime(timeout)
		db.SetMaxOpenConns(1)
		db.SetMaxIdleConns(0)
		// Ping triggers the full StartupMessage + auth handshake via lib/pq.
		// / Ping 触发 lib/pq 跑 StartupMessage + auth 完整握手。
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
		// Classify the error: protocol errors (server replied with an
		// ErrorResponse, prefixed "pq:") are auth misses — try next.
		// Network errors (dial failures, EOF, connection reset) mean the
		// host is unreachable — no point trying more creds. Return the
		// error so the caller sees "host down" rather than a silent miss.
		// / 错误分类：协议错（服务器回 ErrorResponse，前缀 "pq:"）是认证
		// miss——试下一个。网络错（拨号失败、EOF、连接重置）意味着主机不可达
		// ——再多凭据也是徒劳。返错让调用方看到"主机不通"而不是静默 miss。
		if !isPGProtocolError(err) {
			return nil, err
		}
	}
	return nil, nil
}

// init registers the PostgreSQL authenticator with the core/cred registry.
// init 把 PostgreSQL 认证器注册到 core/cred 注册表。
func init() {
	credential.Register(NewPostgreSQLAuthenticator())
}

// isPGProtocolError returns true if err is a server-replied error (auth
// failure) rather than a network failure. lib/pq prefixes all server
// errors with "pq:"; network errors (dial/EOF/reset) come from the
// net package and are returned as *net.OpError or context.DeadlineExceeded.
//
// isPGProtocolError 当 err 是服务器错误（认证失败）而非网络错时返 true。
// lib/pq 给所有服务器错误加 "pq:" 前缀；网络错（dial/EOF/reset）来自
// net 包，返 *net.OpError 或 context.DeadlineExceeded。
func isPGProtocolError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	// Server-side errors are formatted as "pq: <severity>: <message>".
	// / 服务器侧错误格式为 "pq: <severity>: <message>"。
	if strings.HasPrefix(msg, "pq:") {
		return true
	}
	// pq sometimes wraps the network error in pq.Error with a code;
	// the safest test is the prefix. / pq 有时把网络错包在 pq.Error 里
	// 带 code；最稳的判断还是前缀。
	return false
}
