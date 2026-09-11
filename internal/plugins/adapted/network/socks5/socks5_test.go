// Copyright (c) 2026 LCUstinian
// SPDX-License-Identifier: MIT
//
// Fake-server test for the socks5 plugin. Drives the SOCKS5 greeting
// (VER 5, NMETHODS 1, METHODS=[0x00]) and replies with VER 5 + METHOD
// 0x00 so the plugin recognises the service. / socks5 插件的假服务器
// 测试。驱动 SOCKS5 greeting 并回复 VER 5 + METHOD 0x00。
package socks5

import (
	"context"
	"io"
	"net"
	"testing"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/fakeserver"
	"github.com/LCUstinian/FG-QiMen/internal/types"
)

// TestSocks5_IdentifyHit verifies that when a TCP peer performs the
// SOCKS5 no-auth greeting handshake (client writes
// {0x05,0x01,0x00}; server replies {0x05,0x00}), the plugin's
// Identify returns a non-nil result tagged with Service="socks5".
// / 验证 SOCKS5 no-auth 握手成功后插件返回 Service="socks5"。
func TestSocks5_IdentifyHit(t *testing.T) {
	host, port := fakeserver.ListenLoop(t, func(c net.Conn) {
		// Drain the 3-byte greeting from the plugin.
		// / 读掉插件发的 3 字节 greeting。
		if _, err := io.Copy(io.Discard, io.LimitReader(c, 3)); err != nil {
			return
		}
		// Reply: VER 5, METHOD = NO AUTH (0x00).
		// / 回复：VER 5，METHOD = 无鉴权（0x00）。
		_, _ = c.Write([]byte{0x05, 0x00})
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	p := New()
	res := p.Identify(ctx, host, port)
	if res == nil {
		t.Fatalf("Identify: expected hit, got nil (host=%s port=%d)", host, port)
	}
	if res.Service != "socks5" {
		t.Errorf("Identify: Service=%q, want %q", res.Service, "socks5")
	}
	if res.Host != host || res.Port != port {
		t.Errorf("Identify: got host=%s port=%d, want %s:%d", res.Host, res.Port, host, port)
	}
	if res.Banner == "" {
		t.Errorf("Identify: expected non-empty Banner")
	}
}

// TestSocks5_CredentialHit documents that the plugin's Credential is
// currently a documented no-op stub (returns nil). Per the plugin
// source's godoc on Credential: 空 stub. Per the v0.6.0 fake-server
// plan §11.2 we t.Skip when the plugin itself is the no-op rather
// than the test being unable to drive a multi-step handshake.
// / 说明插件 Credential 是文档化的空 stub。
func TestSocks5_CredentialHit(t *testing.T) {
	t.Skip("socks5.Credential is a documented no-op stub (returns nil); covered by plugin godoc, not by fake-server test.")
	// The following code is intentionally unreachable but kept so
	// future maintainers can flip t.Skip() once Credential is
	// implemented and see the shape immediately.
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	creds := []types.Cred{{User: "u", Pass: "p"}}
	if r := New().Credential(ctx, "127.0.0.1", 1, creds); r != nil {
		t.Errorf("Credential: expected nil no-op stub, got %+v", r)
	}
}
