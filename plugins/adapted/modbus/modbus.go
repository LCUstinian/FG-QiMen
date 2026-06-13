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
	"fmt"
	"net"
	"time"

	"github.com/LCUstinian/FG-QiMen/common"
	"github.com/LCUstinian/FG-QiMen/plugins"
)

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
func (p *Plugin) Credential(ctx context.Context, host string, port int, creds []common.Cred) *common.Result {
	return nil
}

// Identify probes Modbus TCP via Read Device Identification.
// / Identify 通过 Read Device Identification 探 Modbus TCP。
func (p *Plugin) Identify(ctx context.Context, host string, port int) *common.Result {
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
	resp := make([]byte, 256)
	n, err := readFullMBP(conn, resp)
	if err != nil || n < 10 {
		return nil
	}
	if resp[7] != 0x2b || resp[8] != 0x0e {
		return nil
	}
	return &common.Result{
		Host: host, Port: port, Service: "modbus",
		Banner: "Modbus TCP", Time: time.Now(),
	}
}

func readFullMBP(c net.Conn, buf []byte) (int, error) {
	total := 0
	for total < len(buf) {
		n, err := c.Read(buf[total:])
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}
