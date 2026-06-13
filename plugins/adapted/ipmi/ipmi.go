// Copyright (c) 2026 LCUstinian
// SPDX-License-Identifier: MIT
//
// IPMI Identify plugin. Sends an RMCP+ Session Open packet over
// UDP 623 and waits for a RAKP Message 1 response. If we get it,
// it's a BMC. / IPMI 识别插件。发 RMCP+ Session Open 包到 UDP 623
// 并等 RAKP Message 1 响应。收到即 BMC。
package ipmi

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/LCUstinian/FG-QiMen/common"
	"github.com/LCUstinian/FG-QiMen/plugins"
)

// Plugin identifies IPMI BMCs. / Plugin 识别 IPMI BMC。
type Plugin struct{}

// New returns a new ipmi plugin. / New 返回一个新的 ipmi 插件。
func New() *Plugin { return &Plugin{} }

func init() { plugins.Register(New()) }

// Name implements plugins.Plugin. / Name 实现 plugins.Plugin。
func (p *Plugin) Name() string { return "ipmi" }

// Ports returns default IPMI port. / Ports 返回默认 IPMI 端口。
func (p *Plugin) Ports() []int { return []int{623} }

// Modes returns Identify + Credential. / Modes 返回 Identify + Credential。
func (p *Plugin) Modes() plugins.Mode { return plugins.ModeIdentify | plugins.ModeCredential }

// Credential is a no-op stub. / Credential 空 stub。
func (p *Plugin) Credential(ctx context.Context, host string, port int, creds []common.Cred) *common.Result {
	return nil
}

// Identify probes IPMI via RMCP+ Session Open. / Identify 通过
// RMCP+ Session Open 探 IPMI。
func (p *Plugin) Identify(ctx context.Context, host string, port int) *common.Result {
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	d := net.Dialer{Timeout: 3 * time.Second}
	conn, err := d.DialContext(ctx, "udp", addr)
	if err != nil {
		return nil
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	// RMCP+ Session Open (class = IPMI). / RMCP+ Session Open
	//（class = IPMI）。
	pkt := []byte{0x04, 0x00, 0x00, 0x07, 0x81, 0x00, 0x10, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
	if _, err := conn.Write(pkt); err != nil {
		return nil
	}
	// Wait for RAKP Message 1. / 等 RAKP Message 1。
	buf := make([]byte, 256)
	if _, err := conn.Read(buf); err != nil {
		return nil
	}
	if buf[7] == 0x12 { // RAKP message tag
		return &common.Result{
			Host: host, Port: port, Service: "ipmi",
			Banner: "IPMI v2.0 (BMC)", Time: time.Now(),
		}
	}
	return nil
}
