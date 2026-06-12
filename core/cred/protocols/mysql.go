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
package protocols

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/LCUstinian/FG-QiMen/core/cred"
	_ "github.com/go-sql-driver/mysql" // register driver
)

// MySQLAuthenticator authenticates against MySQL servers.
// MySQLAuthenticator 对 MySQL 服务器进行认证。
type MySQLAuthenticator struct{}

// NewMySQLAuthenticator returns a default-configured MySQL authenticator.
// NewMySQLAuthenticator 返回默认配置的 MySQL 认证器。
func NewMySQLAuthenticator() *MySQLAuthenticator { return &MySQLAuthenticator{} }

// Name implements cred.Authenticator. / Name 实现 cred.Authenticator。
func (a *MySQLAuthenticator) Name() string { return "mysql" }

// DefaultPorts implements cred.Authenticator. / DefaultPorts 实现 cred.Authenticator。
func (a *MySQLAuthenticator) DefaultPorts() []int { return []int{3306, 33060, 3307} }

// Authenticate implements cred.Authenticator. Tries each cred in
// order; returns the first hit or nil.
//
// Authenticate 实现 cred.Authenticator。按顺序尝试每个 cred；首个命中
// 返回，否则返回 nil。
func (a *MySQLAuthenticator) Authenticate(ctx context.Context, host string, port int, creds []cred.Cred, timeout time.Duration) (*cred.Hit, error) {
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
		if c.Method != "" && c.Method != cred.AuthPassword {
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
		db, err := sql.Open("mysql", dsn)
		if err != nil {
			// Invalid DSN format etc. — try next.
			// DSN 格式无效等——试下一个。
			continue
		}
		db.SetConnMaxLifetime(timeout)
		db.SetMaxOpenConns(1)
		db.SetMaxIdleConns(0)
		// Ping (driver performs the handshake + auth) / Ping（驱动执行握手+认证）
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
	}
	return nil, nil
}
