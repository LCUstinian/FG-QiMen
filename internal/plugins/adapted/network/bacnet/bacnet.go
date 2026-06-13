// Copyright (c) 2026 LCUstinian
// SPDX-License-Identifier: MIT
//
// BACnet Identify plugin. Sends a Who-Is request and waits for
// I-Am. Any device that responds with the BACnet PDU type + I-Am
// service choice is a BACnet device. / BACnet 识别插件。发 Who-Is
// 请求并等 I-Am。任何以 BACnet PDU type + I-Am service choice 响应的
// 设备即 BACnet 设备。
package bacnet

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/plugins"
	"github.com/LCUstinian/FG-QiMen/internal/types"
)

// Plugin identifies BACnet/IP devices. / Plugin 识别 BACnet/IP 设备。
type Plugin struct{}

// New returns a new bacnet plugin. / New 返回一个新的 bacnet 插件。
func New() *Plugin { return &Plugin{} }

func init() { plugins.Register(New()) }

// Name implements plugins.Plugin. / Name 实现 plugins.Plugin。
func (p *Plugin) Name() string { return "bacnet" }

// Ports returns default BACnet port. / Ports 返回默认 BACnet 端口。
func (p *Plugin) Ports() []int { return []int{47808} }

// Modes returns Identify + Credential. / Modes 返回 Identify + Credential。
func (p *Plugin) Modes() plugins.Mode { return plugins.ModeIdentify | plugins.ModeCredential }

// Credential is a no-op stub. / Credential 空 stub。
func (p *Plugin) Credential(ctx context.Context, host string, port int, creds []types.Cred) *types.Result {
	return nil
}

// Identify sends Who-Is and waits for I-Am. / Identify 发 Who-Is
// 并等 I-Am。
func (p *Plugin) Identify(ctx context.Context, host string, port int) *types.Result {
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	d := net.Dialer{Timeout: 3 * time.Second}
	conn, err := d.DialContext(ctx, "udp", addr)
	if err != nil {
		return nil
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	apdu := []byte{0x10, 0x10} // PDU type 0x10 + service choice 0x10 (Who-Is)
	npdu := []byte{0x01}
	body := append(npdu, apdu...)
	length := uint16(4 + len(body))
	hdr := []byte{0x0a, 0x10, 0x00, 0x00}
	binary.BigEndian.PutUint16(hdr[2:4], length)
	pkt := append(hdr, body...)
	if _, err := conn.Write(pkt); err != nil {
		return nil
	}
	buf := make([]byte, 512)
	if _, err := conn.Read(buf); err != nil {
		return nil
	}
	if len(buf) < 7 || buf[0] != 0x0a || buf[4] != 0x01 || buf[5] != 0x10 || buf[6] != 0x10 {
		return nil
	}
	return &types.Result{
		Host: host, Port: port, Service: "bacnet",
		Banner: "BACnet/IP", Time: time.Now(),
	}
}
