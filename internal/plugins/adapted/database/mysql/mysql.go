// Copyright (c) 2026 LCUstinian
// SPDX-License-Identifier: MIT
//
// MySQL Identify plugin. Reads the MySQL HandshakeV10 greeting to
// extract server_version + thread_id + auth-plugin-data. We do NOT
// authenticate; the credential auth lives in
// internal/core/credential/auth/database/mysql.go.
//
// MySQL 识别插件。读 MySQL HandshakeV10 握手，提取 server_version
// + thread_id + auth-plugin-data。我们不做认证；凭据认证在
// internal/core/credential/auth/database/mysql.go。
//
// Wire format (MySQL Protocol / MySQL Client/Server Protocol 5.7+):
//   - server greeting: 4-byte header (payload_len, seq=0) + payload
//   - payload: 1B protocol_version (0x0A) + null-terminated
//     server_version + 4B thread_id + 8B auth-plugin-data-part-1
//   - 1B filler 0x00 + 2B capability_flags_lower + 1B charset
//   - 2B status_flags + 2B capability_flags_upper
//   - 1B auth-plugin-data-len + 10B reserved (0x00) + ...
//   - client auth: 4-byte header + payload (login credentials).
//
// / 协议格式 (MySQL Client/Server Protocol 5.7+)：4 字节头 + payload。
package mysql

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/plugins"
	"github.com/LCUstinian/FG-QiMen/internal/types"
)

// Plugin identifies MySQL servers via the handshake greeting. /
// Plugin 通过握手 greeting 识别 MySQL 服务。
type Plugin struct{}

// New returns a new mysql plugin. / New 返回一个新的 mysql 插件。
func New() *Plugin { return &Plugin{} }

func init() { plugins.Register(New()) }

// Name implements plugins.Plugin. / Name 实现 plugins.Plugin。
func (p *Plugin) Name() string { return "mysql" }

// Ports returns default MySQL ports. / Ports 返回默认 MySQL 端口。
func (p *Plugin) Ports() []int { return []int{3306, 33060, 3307} }

// Modes returns Identify only. / Modes 仅返回 Identify。
func (p *Plugin) Modes() plugins.Mode { return plugins.ModeIdentify }

// Credential is a no-op stub; MySQL credential testing lives in
// core/cred/auth/database in v0.2+. / Credential 空 stub；MySQL
// 凭据测试在 v0.2+ 的 core/cred/auth/database。
func (p *Plugin) Credential(ctx context.Context, host string, port int, creds []types.Cred) *types.Result {
	return nil
}

// Identify connects, reads the server greeting, and parses server_version
// from HandshakeV10. / Identify 连接、读 server greeting、从
// HandshakeV10 解析 server_version。
func (p *Plugin) Identify(ctx context.Context, host string, port int) *types.Result {
	return plugins.RawTCPIdentify(ctx, host, port, func(conn net.Conn) *types.Result {
		// Trigger a server greeting by sending a minimal client auth
		// packet with username=anonymous. The server responds with
		// an ERROR packet (1045: access denied) — but FIRST sends
		// the HandshakeV10 greeting to any new TCP connection
		// before processing client auth. / 用 username=anonymous
		// 触发 server greeting。server 返 ERROR 包（1045: 拒访），
		// 但**首先**在处理 client auth 前发 HandshakeV10 握手。
		// Actually MySQL sends the greeting immediately on connect,
		// before any client input. / 实际上 MySQL 在 connect 后
		// 立刻发 greeting，client 无需发任何东西。
		hdr := make([]byte, 4)
		if _, err := conn.Read(hdr); err != nil {
			return nil
		}
		// payload length = first 3 bytes (LE), seq = 4th byte.
		// / payload 长度 = 前 3 字节 (LE)，seq = 第 4 字节。
		payloadLen := int(hdr[0]) | int(hdr[1])<<8 | int(hdr[2])<<16
		if payloadLen < 1 || payloadLen > 4096 {
			return nil
		}
		body := make([]byte, payloadLen)
		if _, err := conn.Read(body); err != nil {
			return nil
		}
		if body[0] != 0x0A {
			// Not HandshakeV10 (e.g. HandshakeV9 from very old
			// MySQL < 3.21). Skip. / 不是 HandshakeV10（极老
			// MySQL 的 HandshakeV9）。跳过。
			return nil
		}
		// server_version: null-terminated string starting at offset 1.
		// / server_version: 从 offset 1 起的 null 结尾字符串。
		verEnd := bytes.IndexByte(body[1:], 0x00)
		if verEnd < 0 {
			return nil
		}
		ver := string(body[1 : 1+verEnd])
		// thread_id: 4 bytes LE starting after server_version's null.
		// / thread_id: server_version null 之后 4 字节 LE。
		// The exact offset depends on the version string length, so
		// compute it dynamically. / 确切 offset 取决于版本字符串长
		// 度，所以动态算。
		threadIDOffset := 1 + verEnd + 1
		if threadIDOffset+4 > len(body) {
			return nil
		}
		threadID := binary.LittleEndian.Uint32(body[threadIDOffset : threadIDOffset+4])
		// auth-plugin-data-len: skip the 8-byte salt + 1 filler + 2
		// capability_flags + 1 charset + 2 status + 2 cap + 1 auth-len
		// to read the auth-plugin name. / auth-plugin-data-len: 跳
		// 8 字节 salt + 1 filler + 2 capability + 1 charset + 2
		// status + 2 cap + 1 auth-len 读 auth-plugin 名。
		authDataLenOffset := threadIDOffset + 4 + 8 + 1 + 2 + 1 + 2 + 2 + 1
		if authDataLenOffset+1 > len(body) {
			return nil
		}
		authDataLen := body[authDataLenOffset]
		// auth-plugin-name starts after 10 bytes reserved (all zero).
		// / auth-plugin-name 在 10 字节 reserved 后开始。
		nameOffset := authDataLenOffset + 1 + 10
		// 10 bytes reserved is the default but some servers (MariaDB
		// 10.4+) leave it at 6. Cap at body end. / 10 字节 reserved
		// 是默认值，但部分 server（MariaDB 10.4+）只有 6。封顶
		// body 尾。
		authPluginName := "unknown"
		if int(nameOffset)+int(authDataLen)-8 <= len(body) {
			end := bytes.IndexByte(body[nameOffset:], 0x00)
			if end > 0 {
				authPluginName = string(body[nameOffset : nameOffset+end])
			}
		}
		return &types.Result{
			Host:    host,
			Port:    port,
			Service: "mysql",
			Banner:  fmt.Sprintf("MySQL %s (thread_id=%d, auth=%s)", ver, threadID, authPluginName),
			Time:    time.Now(),
		}
	})
}

// _ keeps the strconv import alive in case future code uses it.
// strconv is currently used inside the file via %d formatting. /
// _ 保 strconv 导入；当前用 %d 格式。
var _ = strconv.Itoa
