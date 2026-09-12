// Copyright (c) 2026 LCUstinian
// SPDX-License-Identifier: MIT
//
// Modbus Identify plugin. Sends a Read Device Identification
// request (function code 43/14). If the device responds with the
// expected function code, it's a Modbus endpoint. / Modbus 识别
// 插件。发 Read Device Identification 请求（function code 43/14）。
// 如果设备以预期 function code 响应，即 Modbus 端点。
package modbus

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/plugins"
	"github.com/LCUstinian/FG-QiMen/internal/types"
)

// modbusRespBytes is the minimum number of bytes the plugin needs
// from the server reply: 7-byte MBAP header + 1-byte function
// code + 1-byte MEI type. We deliberately keep this small so a
// real-world server that closes the connection right after writing
// the function-code byte (or TCP fragmentation that delivers <256
// bytes) is still identified correctly. The previous version
// read 256 bytes via a custom readFullMBP loop; any short read
// returned io.EOF, which made the plugin reject the hit even
// when the actual Modbus response bytes were already in the
// kernel buffer. / modbusRespBytes 是插件需要从服务器响应读
// 到的最少字节数：7 字节 MBAP header + 1 字节 function code +
// 1 字节 MEI 类型。故意保持小一点，让真实服务器在写完 function-
// code 字节后立刻关连接（或 TCP 段化只交付 <256 字节）也能被
// 正确识别。旧版本用自定义 readFullMBP 循环读 256 字节，任
// 何短读都返 io.EOF，让插件拒绝命中，即使 Modbus 响应字节已
// 经在内核 buffer 里。
const modbusRespBytes = 9

// Plugin identifies Modbus TCP devices. / Plugin 识别 Modbus TCP 设备。
type Plugin struct{}

// New returns a new modbus plugin. / New 返回一个新的 modbus 插件。
func New() *Plugin { return &Plugin{} }

func init() { plugins.Register(New()) }

// Name implements plugins.Plugin. / Name 实现 plugins.Plugin。
func (p *Plugin) Name() string { return "modbus" }

// Ports returns default Modbus port. / Ports 返回默认 Modbus 端口。
func (p *Plugin) Ports() []int { return []int{502} }

// Modes returns Identify + Credential. / Modes 返回 Identify + Credential。
func (p *Plugin) Modes() plugins.Mode { return plugins.ModeIdentify | plugins.ModeCredential }

// Credential is a no-op stub. / Credential 空 stub。
func (p *Plugin) Credential(ctx context.Context, host string, port int, creds []types.Cred) *types.Result {
	return nil
}

// Identify probes Modbus TCP via Read Device Identification.
// / Identify 通过 Read Device Identification 探 Modbus TCP。
func (p *Plugin) Identify(ctx context.Context, host string, port int) *types.Result {
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	d := net.Dialer{Timeout: 3 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	pdu := []byte{0x2b, 0x0e, 0x01, 0x00}
	length := uint16(1 + 1 + 1 + 1 + len(pdu))
	header := make([]byte, 7)
	binary.BigEndian.PutUint16(header[0:2], 1)
	binary.BigEndian.PutUint16(header[2:4], 0)
	binary.BigEndian.PutUint16(header[4:6], length)
	header[6] = 1
	out := append(header, pdu...)
	if _, err := conn.Write(out); err != nil {
		return nil
	}
	resp := make([]byte, modbusRespBytes)
	// Use io.ReadFull (not a custom 256-byte loop) so a short read
	// + EOF only fails the request when we genuinely didn't get
	// enough bytes. A real Modbus server that writes the 9 bytes
	// and immediately closes returns ErrUnexpectedEOF — that's fine,
	// we still got the function code + MEI type. / 用 io.ReadFull
	//（不是自定义 256 字节循环），这样短读 + EOF 只在真的没拿到
	// 足够字节时才让请求失败。真实 Modbus 服务器写 9 字节后立刻
	// 关会返 ErrUnexpectedEOF——没问题，我们已经拿到 function code
	// + MEI 类型。
	n, err := io.ReadFull(conn, resp)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return nil
	}
	if n < modbusRespBytes {
		return nil
	}
	if resp[7] != 0x2b || resp[8] != 0x0e {
		return nil
	}
	return &types.Result{
		Host: host, Port: port, Service: "modbus",
		Banner: "Modbus TCP", Time: time.Now(),
	}
}
