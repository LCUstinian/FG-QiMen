// Copyright (c) 2026 LCUstinian
// SPDX-License-Identifier: MIT
//
// MongoDB Identify plugin. Raw OP_MSG hello probe — no driver, no
// session, no exploit path (no find / insert / update).
//
// MongoDB 识别插件。用原生 OP_MSG hello 探测——不加载驱动、不维护
// session、不发任何 find / insert / update。
package mongodb

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/common"
	"github.com/LCUstinian/FG-QiMen/internal/plugins"
)

// Plugin identifies MongoDB servers via the OP_MSG hello. / Plugin 通过 OP_MSG hello 识别 MongoDB 服务。
type Plugin struct{}

// New returns a new mongodb plugin. / New 返回一个新的 mongodb 插件。
func New() *Plugin { return &Plugin{} }

func init() { plugins.Register(New()) }

// Name implements plugins.Plugin. / Name 实现 plugins.Plugin。
func (p *Plugin) Name() string { return "mongodb" }

// Ports returns default MongoDB ports. / Ports 返回默认 MongoDB 端口。
func (p *Plugin) Ports() []int { return []int{27017, 27018} }

// Modes returns Identify only. / Modes 仅返回 Identify。
func (p *Plugin) Modes() plugins.Mode { return plugins.ModeIdentify }

// Credential is a no-op stub. / Credential 空 stub。
func (p *Plugin) Credential(ctx context.Context, host string, port int, creds []common.Cred) *common.Result {
	return nil
}

// Identify sends an OP_MSG hello and parses the version.
// Identify 发 OP_MSG hello 并解析版本。
//
// Wire format (simplified):
//   - msgHeader: 16 bytes (messageLength, requestID, responseTo, opCode)
//   - OP_MSG (opCode = 2013):
//     - flagBits: 4 bytes
//     - section: 1 byte (0 = body)
//     - body: BSON document
//
// 线格式（简化）：
//   - msgHeader: 16 字节（messageLength, requestID, responseTo, opCode）
//   - OP_MSG（opCode = 2013）：
//     - flagBits: 4 字节
//     - section: 1 字节（0 = body）
//     - body: BSON 文档
func (p *Plugin) Identify(ctx context.Context, host string, port int) *common.Result {
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	d := net.Dialer{Timeout: 3 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))

	// Build OP_MSG with hello doc. {hello: 1}.
	// 构造 OP_MSG + hello 文档。
	body := bsonDoc(map[string]any{"hello": int32(1), "$db": "admin"})
	msg := make([]byte, 16+4+1+len(body))
	binary.LittleEndian.PutUint32(msg[12:16], 2013) // opCode
	msg[16] = 0                                      // flagBits
	msg[20] = 0                                      // section type
	copy(msg[21:], body)
	binary.LittleEndian.PutUint32(msg[0:4], uint32(len(msg)))

	if _, err := conn.Write(msg); err != nil {
		return nil
	}
	resp := make([]byte, 4096)
	n, err := conn.Read(resp)
	if err != nil || n < 20 {
		return nil
	}
	// Parse response header. / 解析响应头。
	respOpcode := binary.LittleEndian.Uint32(resp[12:16])
	if respOpcode != 2013 && respOpcode != 2014 {
		return nil
	}
	// Find "version" string in body. / 在 body 中找 "version" 字符串。
	// Simplified: scan the body for the byte sequence "version\0".
	// / 简化：在 body 中扫 "version\0" 字节序列。
	bs := resp[20:n]
	if v := extractBSONString(bs, "version"); v != "" {
		return &common.Result{
			Host: host, Port: port, Service: "mongodb",
			Banner: "MongoDB " + v, Time: time.Now(),
		}
	}
	return &common.Result{
		Host: host, Port: port, Service: "mongodb",
		Banner: "MongoDB", Time: time.Now(),
	}
}

// bsonDoc encodes a simple BSON document. Keys must be strings. / bsonDoc 编码简单 BSON 文档。
// Supports int32 and string values only. / 只支持 int32 和 string 值。
func bsonDoc(m map[string]any) []byte {
	// Compute total body size. / 计算 body 总大小。
	body := make([]byte, 0, 128)
	for k, v := range m {
		body = append(body, bsonType(v))
		body = append(bsonCString(k), 0)
		switch x := v.(type) {
		case int32:
			tmp := make([]byte, 4)
			binary.LittleEndian.PutUint32(tmp, uint32(x))
			body = append(body, tmp...)
		case string:
			body = append(body, bsonCString(x)...)
			body = append(body, 0)
		}
	}
	body = append(body, 0) // null terminator
	// Prepend length. / 前置长度。
	out := make([]byte, 4+len(body))
	binary.LittleEndian.PutUint32(out[0:4], uint32(len(out)))
	copy(out[4:], body)
	return out
}

// bsonType returns the BSON type byte for a Go value. / bsonType 返回 Go 值的 BSON 类型字节。
func bsonType(v any) byte {
	switch v.(type) {
	case int32:
		return 0x10
	case string:
		return 0x02
	}
	return 0x00
}

// bsonCString encodes a Go string as a null-terminated C string. / bsonCString 把 Go 字符串编码为 null 结尾 C 字符串。
func bsonCString(s string) []byte { return []byte(s) }

// extractBSONString finds a string value in a BSON body by key. / extractBSONString 在 BSON body 中按 key 找字符串。
// Returns "" if not found. / 未找到返回 ""。
// Very simple scan; only correct for short, flat documents. / 极简扫描；只对短平文档正确。
func extractBSONString(body []byte, key string) string {
	want := []byte(key)
	want = append(want, 0)
	for i := 0; i+len(want) < len(body); i++ {
		if string(body[i:i+len(want)]) != string(want) {
			continue
		}
		// Key found; next byte is type, then string length (4 bytes LE), then bytes.
		// / 找到 key；下一字节是类型，再 4 字节 LE 长度，再字符串字节。
		if i+len(want)+1+4 >= len(body) {
			return ""
		}
		if body[i+len(want)] != 0x02 { // 0x02 = string
			continue
		}
		sl := binary.LittleEndian.Uint32(body[i+len(want)+1 : i+len(want)+5])
		if i+len(want)+5+int(sl) > len(body) {
			return ""
		}
		return string(body[i+len(want)+5 : i+len(want)+5+int(sl)])
	}
	return ""
}
