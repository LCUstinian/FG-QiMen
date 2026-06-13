// Copyright (c) 2026 LCUstinian
// SPDX-License-Identifier: MIT
//
// RabbitMQ Identify plugin. Sends the AMQP 0-9-1 protocol header
// and reads the server's Connection.Start frame; we only verify
// the class/method bytes, not the full auth. Credential() is
// routed through core/cred/protocols/rabbitmq.go (AMQP PLAIN auth).
//
// RabbitMQ 识别插件。发 AMQP 0-9-1 协议头并读服务器的 Connection.Start
// 帧；我们只验证 class/method 字节，不做完整认证。Credential() 走
// core/cred/protocols/rabbitmq.go（AMQP PLAIN 认证）。
package rabbitmq

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"time"

	"github.com/LCUstinian/FG-QiMen/common"
	"github.com/LCUstinian/FG-QiMen/plugins"
)

// Plugin identifies RabbitMQ / AMQP 0-9-1 brokers. / Plugin 识别
// RabbitMQ / AMQP 0-9-1 brokers。
type Plugin struct{}

// New returns a new rabbitmq plugin. / New 返回一个新的 rabbitmq 插件。
func New() *Plugin { return &Plugin{} }

func init() { plugins.Register(New()) }

// Name implements plugins.Plugin. / Name 实现 plugins.Plugin。
func (p *Plugin) Name() string { return "rabbitmq" }

// Ports returns default RabbitMQ port. / Ports 返回默认 RabbitMQ 端口。
func (p *Plugin) Ports() []int { return []int{5672} }

// Modes returns Identify + Credential. / Modes 返回 Identify + Credential。
func (p *Plugin) Modes() plugins.Mode { return plugins.ModeIdentify | plugins.ModeCredential }

// Credential is a no-op stub. / Credential 空 stub。
func (p *Plugin) Credential(ctx context.Context, host string, port int, creds []common.Cred) *common.Result {
	return nil
}

// Identify sends the AMQP protocol header and reads Connection.Start.
// / Identify 发 AMQP 协议头并读 Connection.Start。
func (p *Plugin) Identify(ctx context.Context, host string, port int) *common.Result {
	addr := net.JoinHostPort(host, fmt.Sprintf("%d", port))
	d := net.Dialer{Timeout: 3 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	// Send protocol header. / 发协议头。
	if _, err := conn.Write([]byte("AMQP\x00\x00\x09\x01")); err != nil {
		return nil
	}
	// Read 7-byte frame header. / 读 7 字节帧头。
	hdr := make([]byte, 7)
	if _, err := readFullRMQ3(conn, hdr); err != nil {
		return nil
	}
	size := binary.BigEndian.Uint32(hdr[3:7])
	if size < 11 {
		return nil
	}
	body := make([]byte, size-7)
	if _, err := readFullRMQ3(conn, body); err != nil {
		return nil
	}
	// class 10 method 11 = Connection.Start. / class 10 method 11
	// = Connection.Start。
	if len(body) < 4 {
		return nil
	}
	class := binary.BigEndian.Uint16(body[0:2])
	method := binary.BigEndian.Uint16(body[2:4])
	if class != 0x000a || method != 0x000b {
		return nil
	}
	return &common.Result{
		Host: host, Port: port, Service: "rabbitmq",
		Banner: "RabbitMQ AMQP 0-9-1", Time: time.Now(),
	}
}

func readFullRMQ3(c net.Conn, buf []byte) (int, error) {
	total := 0
	for total < len(buf) {
		n, err := c.Read(buf[total:])
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}
