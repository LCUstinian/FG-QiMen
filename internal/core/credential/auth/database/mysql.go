// Package protocols: MySQL authenticator.
// Package protocols: MySQL 认证器。
//
// Uses github.com/go-sql-driver/mysql (MIT, standard SQL driver).
// Just open a connection and Ping — the driver handles the
// native41 handshake, including auth-plugin-data parsing and the
// SHA1(salt) XOR SHA1(SHA1(SHA1(pw))) computation.
//
// 用 github.com/go-sql-driver/mysql（MIT，标准 SQL 驱动）。
// 仅需打开连接并 Ping——驱动处理 native41 握手，包括 auth-plugin-data
// 解析和 SHA1(salt) XOR SHA1(SHA1(SHA1(pw))) 计算。
//
// We do NOT run any SQL. Just authenticate.
// 我们不执行任何 SQL——只认证。
//
// Phase 1.9 (audit roadmap): *sql.DB is cached per
// (driver, host, port, user, pass) so repeated attempts on the
// same DB hit a warm pool. The cache is process-global via
// sqlcache.Global. / Phase 1.9（审计路线图）：*sql.DB 按
// (driver, host, port, user, pass) 缓存，同 DB 重复尝试走暖池。
// 缓存是进程全局（sqlcache.Global）。
package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/core/credential"
	"github.com/LCUstinian/FG-QiMen/internal/core/credential/auth/database/sqlcache"
	"github.com/go-sql-driver/mysql"
	_ "github.com/go-sql-driver/mysql" // register driver
)

// MySQLAuthenticator authenticates against MySQL servers.
// MySQLAuthenticator 对 MySQL 服务器进行认证。
type MySQLAuthenticator struct{}

// NewMySQLAuthenticator returns a default-configured MySQL authenticator.
// NewMySQLAuthenticator 返回默认配置的 MySQL 认证器。
func NewMySQLAuthenticator() *MySQLAuthenticator { return &MySQLAuthenticator{} }

func init() { credential.Register(NewMySQLAuthenticator()) }

// Name implements credential.Authenticator. / Name 实现 credential.Authenticator。
func (a *MySQLAuthenticator) Name() string { return "mysql" }

// DefaultPorts implements credential.Authenticator. / DefaultPorts 实现 credential.Authenticator。
func (a *MySQLAuthenticator) DefaultPorts() []int { return []int{3306, 33060, 3307} }

// Authenticate implements credential.Authenticator. Tries each cred in
// order; returns the first hit or nil.
//
// Authenticate 实现 credential.Authenticator。按顺序尝试每个 cred；首个命中
// 返回，否则返回 nil。
func (a *MySQLAuthenticator) Authenticate(ctx context.Context, host string, port int, creds []credential.Cred, timeout time.Duration) (*credential.Hit, error) {
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
		// Driver DSN: we connect to "information_schema" with a
		// connection-time timeout. We never issue a query — the
		// Ping that sql.Open does on first use is enough to validate
		// the credential.
		// 驱动 DSN：连 "information_schema"，设连接超时。我们不发任何
		// 查询——sql.Open 在首次使用时的 Ping 足以验证凭据。
		dsn := fmt.Sprintf("%s:%s@tcp(%s)/information_schema?charset=utf8&timeout=%ds",
			c.User, c.Pass, addr, timeoutSec)
		cacheKey := "mysql|" + addr + "|" + c.User + "|" + c.Pass
		db, created, err := sqlcache.Global.GetOrCreate(cacheKey, func() (*sql.DB, error) {
			return sql.Open("mysql", dsn)
		})
		if err != nil {
			// Invalid DSN format etc. — try next.
			// DSN 格式无效等——试下一个。
			continue
		}
		if created {
			// First time we see this (driver, host, port, user, pass).
			// Tune the pool for a credential-spray workload.
			// / 首次见此 (driver, host, port, user, pass)。为凭
			// 据喷洒调池。
			db.SetConnMaxLifetime(timeout)
			db.SetMaxOpenConns(1)
			db.SetMaxIdleConns(0)
		}
		// Ping (driver performs the handshake + auth) / Ping（驱动执行握手+认证）
		pingCtx, cancel := context.WithTimeout(ctx, timeout)
		err = db.PingContext(pingCtx)
		cancel()
		if err != nil {
			// P3-5 (audit): only invalidate the cache on an auth-
			// state error (1045 ER_ACCESS_DENIED). Network errors
			// (connection refused, timeout, server gone) leave the
			// cached *sql.DB's auth state intact — invalidating on
			// those tears down a warm pool on every flaky network.
			// / P3-5（审计）：仅在认证态错误（1045 ER_ACCESS_DENIED）
			// 时失效缓存。网络错误（连接拒、超时、服务下线）的
			// 缓存 *sql.DB 认证态不变——对这些错误失效会在每次网
			// 络抖动时拆掉暖池。
			var mysqlErr *mysql.MySQLError
			if errors.As(err, &mysqlErr) && mysqlErr.Number == 1045 {
				sqlcache.Global.Invalidate(cacheKey)
			}
		}
		if err == nil {
			return &credential.Hit{
				Cred:     c,
				Attempts: i + 1,
				Time:     time.Now(),
			}, nil
		}
	}
	return nil, nil
}
