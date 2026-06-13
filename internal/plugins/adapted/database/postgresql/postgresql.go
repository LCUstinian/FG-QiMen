// Copyright (c) 2026 LCUstinian
// SPDX-License-Identifier: MIT
//
// PostgreSQL Identify plugin. Raw StartupMessage probe — no lib/pq
// dependency, no driver load at identify time. Credential testing
// lives in core/cred in v0.2+.
//
// PostgreSQL 识别插件。用原生 StartupMessage 探测——不依赖 lib/pq，
// identify 时不加载驱动。凭据测试在 v0.2+ 走 core/cred。
package postgresql

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/plugins"
	"github.com/LCUstinian/FG-QiMen/internal/types"
)

// Plugin identifies PostgreSQL servers. / Plugin 识别 PostgreSQL 服务。
type Plugin struct{}

// New returns a new postgresql plugin. / New 返回一个新的 postgresql 插件。
func New() *Plugin { return &Plugin{} }

func init() { plugins.Register(New()) }

// Name implements plugins.Plugin. / Name 实现 plugins.Plugin。
func (p *Plugin) Name() string { return "postgresql" }

// Ports returns default PostgreSQL ports. / Ports 返回默认 PostgreSQL 端口。
func (p *Plugin) Ports() []int { return []int{5432, 5433} }

// Modes returns Identify + Credential. / Modes 返回 Identify + Credential。
//
// Credential() is implemented in core/cred/protocols/postgresql.go
// (PostgreSQLAuthenticator via lib/pq). The plugin's Credential method
// is a no-op stub because the pipeline routes cred testing through
// the central credential.Scheduler (see core/pipeline.go dispatchCred).
// / Credential() 实现在 core/cred/protocols/postgresql.go
// (PostgreSQLAuthenticator via lib/pq)。plugin 的 Credential 方法是
// 空 stub，因为管线把凭据测试路由到中央 credential.Scheduler
// (见 core/pipeline.go dispatchCred)。
func (p *Plugin) Modes() plugins.Mode { return plugins.ModeIdentify | plugins.ModeCredential }

// Credential is a no-op stub. / Credential 空 stub。
func (p *Plugin) Credential(ctx context.Context, host string, port int, creds []types.Cred) *types.Result {
	return nil
}

// Identify sends a StartupMessage and reads the response. / Identify 发 StartupMessage 并读响应。
//
// PostgreSQL v3 wire format:
//   - frontend → backend StartupMessage: int32 length + int32 protocol(3,0) + kv pairs
//   - backend → frontend: 'R' AuthenticationOk, 'E' ErrorResponse
//
// PostgreSQL v3 线格式：
//   - 前端 → 后端 StartupMessage：int32 长度 + int32 协议(3,0) + kv 对
//   - 后端 → 前端：'R' 认证成功，'E' 错误响应
func (p *Plugin) Identify(ctx context.Context, host string, port int) *types.Result {
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	d := net.Dialer{Timeout: 3 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))

	// Build StartupMessage: length, protocol=3.0, user, database, \0.
	// / 构造 StartupMessage：长度、协议=3.0、user、database、\0。
	body := []byte{}
	body = append(body, 0, 0, 0, 0)             // placeholder length
	body = append(body, 0x00, 0x03, 0x00, 0x00) // protocol 3.0
	body = append(body, "user\x00postgres\x00"...)
	body = append(body, "database\x00postgres\x00"...)
	body = append(body, 0x00) // terminator
	binary.BigEndian.PutUint32(body[0:4], uint32(len(body)))
	if _, err := conn.Write(body); err != nil {
		return nil
	}
	resp := make([]byte, 1024)
	n, err := conn.Read(resp)
	if err != nil || n < 5 {
		return nil
	}
	switch resp[0] {
	case 'R':
		// AuthenticationOk (no body) or AuthenticationCleartextPassword
		// (body int32=3 then 0x00). We don't care about the body,
		// just the type byte proves it's PostgreSQL.
		// / AuthenticationOk（无 body）或 AuthenticationCleartextPassword
		// （body int32=3 然后 0x00）。我们不关心 body，仅类型字节证明是 PG。
		return &types.Result{
			Host: host, Port: port, Service: "postgresql",
			Banner: "PostgreSQL", Time: time.Now(),
		}
	case 'E':
		// ErrorResponse — could be a server that rejected the user/db.
		// Still proves it's PostgreSQL. / ErrorResponse——可能是服务
		// 拒了 user/db。仍证明是 PostgreSQL。
		return &types.Result{
			Host: host, Port: port, Service: "postgresql",
			Banner: "PostgreSQL (auth error)", Time: time.Now(),
		}
	}
	return nil
}
