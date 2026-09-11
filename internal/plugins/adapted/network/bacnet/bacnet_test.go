// Copyright (c) 2026 LCUstinian
// SPDX-License-Identifier: MIT
//
// Fake-server tests for the bacnet plugin. Uses the shared
// fakeserver.ListenUDPLoop helper to drive a Who-Is -> I-Am
// handshake on a loopback UDP socket. / bacnet 插件的假服务器测试。
// 使用共享的 fakeserver.ListenUDPLoop 助手在 loopback UDP 套接字上
// 驱动 Who-Is -> I-Am 握手。
package bacnet

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/fakeserver"
)

// iamReply is the minimal I-Am PDU the plugin's Identify validates
// against. Layout:
// / iamReply 是插件 Identify 校验所需的最小 I-Am PDU。结构：
//
//	byte 0      BVLC type                 = 0x0a
//	byte 1      BVLC function (unicast)   = 0x10
//	bytes 2-3   BVLC length (big endian)  = 0x0007
//	byte 4      NPDU version              = 0x01
//	byte 5      PDU type (confirmed)      = 0x10
//	byte 6      service choice (I-Am)     = 0x10
var iamReply = []byte{0x0a, 0x10, 0x00, 0x07, 0x01, 0x10, 0x10}

// TestBacnet_IdentifyHit verifies that when the fake server replies
// with a valid I-Am PDU, the plugin's Identify returns a non-nil
// Result carrying Service "bacnet" and Banner "BACnet/IP".
// / TestBacnet_IdentifyHit 验证当假服务器以合法 I-Am PDU 应答时，插件
// 的 Identify 返回带 Service "bacnet" 与 Banner "BACnet/IP" 的非 nil
// Result。
func TestBacnet_IdentifyHit(t *testing.T) {
	host, port := fakeserver.ListenUDPLoop(t, func(req []byte, src *net.UDPAddr) []byte {
		// Echo a valid I-Am for every incoming Who-Is. The handler
		// is intentionally stateless: the BACnet Who-Is broadcast is
		// connectionless and the plugin's reply validator only
		// inspects the payload bytes, not the source address. / 对
		// 每个收到的 Who-Is 都回一份合法 I-Am。Handler 故意无状态：
		// BACnet Who-Is 广播是无连接的，插件的应答校验器只检查负载
		// 字节，不检查源地址。
		_ = req
		_ = src
		return iamReply
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	p := New()
	got := p.Identify(ctx, host, port)
	if got == nil {
		t.Fatalf("Identify returned nil; expected a bacnet Result")
	}
	if got.Service != "bacnet" {
		t.Errorf("Service = %q, want %q", got.Service, "bacnet")
	}
	if got.Banner != "BACnet/IP" {
		t.Errorf("Banner = %q, want %q", got.Banner, "BACnet/IP")
	}
	if got.Host != host || got.Port != port {
		t.Errorf("Host/Port = %q/%d, want %q/%d", got.Host, got.Port, host, port)
	}
}

// TestBacnet_CredentialHit is skipped: the plugin's Credential is a
// documented no-op stub that always returns nil (see bacnet.go lines
// 40-42). Multi-step credential handshakes are out of scope for this
// plugin; no server-side state machine to drive.
// / TestBacnet_CredentialHit 跳过：插件的 Credential 是文档化的 no-op
// stub，始终返回 nil（见 bacnet.go 第 40-42 行）。该插件不涉及多步凭据
// 握手，没有服务端状态机需要驱动。
func TestBacnet_CredentialHit(t *testing.T) {
	t.Skip("bacnet.Credential is a documented no-op stub; nothing to drive")
}
