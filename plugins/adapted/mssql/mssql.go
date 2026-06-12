// Copyright (c) 2026 LCUstinian
// SPDX-License-Identifier: MIT
//
// MSSQL Identify plugin. Uses go-mssqldb (which performs the full
// TDS handshake including version exchange). No query is sent.
//
// MSSQL 识别插件。用 go-mssqldb（驱动执行完整 TDS 握手 + 版本交换）。
// 不发任何查询。
package mssql

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"strings"
	"time"

	_ "github.com/microsoft/go-mssqldb" // register driver / 注册驱动

	"github.com/LCUstinian/FG-QiMen/common"
	"github.com/LCUstinian/FG-QiMen/plugins"
)

// Plugin identifies MSSQL servers via go-mssqldb. / Plugin 通过 go-mssqldb 识别 MSSQL 服务。
type Plugin struct{}

// New returns a new mssql plugin. / New 返回一个新的 mssql 插件。
func New() *Plugin { return &Plugin{} }

func init() { plugins.Register(New()) }

// Name implements plugins.Plugin. / Name 实现 plugins.Plugin。
func (p *Plugin) Name() string { return "mssql" }

// Ports returns default MSSQL ports. / Ports 返回默认 MSSQL 端口。
func (p *Plugin) Ports() []int { return []int{1433, 1434} }

// Modes returns Identify only. / Modes 仅返回 Identify。
func (p *Plugin) Modes() plugins.Mode { return plugins.ModeIdentify }

// Credential is a no-op stub. / Credential 空 stub。
func (p *Plugin) Credential(ctx context.Context, host string, port int, creds []common.Cred) *common.Result {
	return nil
}

// Identify opens a TDS connection with invalid creds (the driver still
// returns the server version in the handshake). It does NOT actually
// run a query.
//
// Identify 用无效凭据开 TDS 连接（驱动握手时会返回服务器版本）。不实际跑查询。
func (p *Plugin) Identify(ctx context.Context, host string, port int) *common.Result {
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	// Disable encryption for the simple probe (login packet is
	// encrypted with self-signed certs otherwise). / 关加密（否则登录
	// 包会用自签证书加密，握手更复杂）。
	dsn := fmt.Sprintf("server=%s;port=%d;user id=invalid;password=invalid;encrypt=disable;timeout=%d",
		addr, port, int(3*time.Second/time.Second))
	db, err := sql.Open("sqlserver", dsn)
	if err != nil {
		return nil
	}
	defer db.Close()
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var ver string
	row := db.QueryRowContext(ctx, "SELECT @@VERSION")
	if err := row.Scan(&ver); err == nil {
		// Successful query = auth somehow worked. Still report.
		// / 成功 = 某种程度 auth 过了。仍上报。
		return &common.Result{
			Host: host, Port: port, Service: "mssql",
			Banner: "MSSQL: " + firstLine(ver), Time: time.Now(),
		}
	}
	// Query failed (auth), but the TDS PRELOGIN handshake returned
	// the server name. We can pull it from the error.
	// / 查询失败（auth），但 TDS PRELOGIN 握手返回了 server 名。可以
	// 从 error 抽出来。
	errStr := err.Error()
	if strings.Contains(errStr, "SQL Server") || strings.Contains(errStr, "mssql") ||
		strings.Contains(errStr, "denied") || strings.Contains(errStr, "login") {
		// Even on auth failure, we've confirmed it's MSSQL.
		// / 即使 auth 失败，也确认是 MSSQL。
		return &common.Result{
			Host: host, Port: port, Service: "mssql",
			Banner: "MSSQL", Time: time.Now(),
		}
	}
	return nil
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return strings.TrimSpace(s)
}
