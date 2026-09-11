// modbus_test.go — fake-server tests for the Modbus Identify plugin.
// / Modbus 识别插件的假服务器测试。
//
// The plugin speaks Modbus TCP: a 7-byte MBAP header followed by a
// PDU with function code 0x2b (Encapsulated Interface Transport) and
// MEI type 0x0e (Read Device Identification). It only inspects two
// response bytes (function code + MEI), so the fake server just has
// to echo those back and close the connection — readFullMBP loops
// until EOF, so a short reply + close is required to unblock it.
//
// 插件说 Modbus TCP：7 字节 MBAP header 加 function code 0x2b
// （Encapsulated Interface Transport）和 MEI type 0x0e（Read Device
// Identification）的 PDU。它只检响应里两字节（function code + MEI），
// 所以假服务器只需回这两字节并关连接 — readFullMBP 会循环到 EOF，
// 必须用短响应 + close 才能解开阻塞。
package modbus

import (
	"context"
	"encoding/binary"
	"net"
	"testing"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/fakeserver"
)

// replyMBAP builds a Modbus TCP MBAP response padded to 256 bytes
// (the exact size of the plugin's read buffer) so readFullMBP returns
// no error. Bytes 7 and 8 carry the function code 0x2b and MEI type
// 0x0e that the plugin inspects.
//
// replyMBAP 构造一个填充到 256 字节的 Modbus TCP MBAP 响应（与插件
// 读缓冲同大），让 readFullMBP 不带 error 返回。第 7、8 字节带插件
// 检的 function code 0x2b 和 MEI type 0x0e。
func replyMBAP() []byte {
	resp := make([]byte, 256)
	binary.BigEndian.PutUint16(resp[0:2], 1) // transaction id
	binary.BigEndian.PutUint16(resp[2:4], 0) // protocol id (Modbus)
	binary.BigEndian.PutUint16(resp[4:6], 3) // length = unit_id + 2 PDU bytes
	resp[6] = 0x01                           // unit id
	resp[7] = 0x2b                           // function code (ENI)
	resp[8] = 0x0e                           // MEI type (Read Device ID)
	resp[9] = 0x01                           // object id
	return resp
}

// TestModbus_IdentifyHit drives the happy path: a fake Modbus TCP
// server that replies to the plugin's Read Device Identification
// request with the expected function + MEI bytes. The plugin must
// return a non-nil *types.Result tagged with Service="modbus".
//
// / TestModbus_IdentifyHit 跑正常路径：假 Modbus TCP server 对插件
// 的 Read Device Identification 请求回期望的 function + MEI 字节。
// 插件必须返回带 Service="modbus" 的非 nil *types.Result。
func TestModbus_IdentifyHit(t *testing.T) {
	host, port := fakeserver.ListenLoop(t, func(c net.Conn) {
		// The plugin's readFullMBP reads up to 256 bytes and
		// returns nil from Identify if any Read errors — so we
		// must send the FULL 256 bytes the plugin expects, then
		// close cleanly. readFullMBP returns (256, nil) only when
		// every Read succeeds; EOF short-circuits to (n, io.EOF)
		// and the plugin's `err != nil` check rejects the hit.
		// / 插件的 readFullMBP 最多读 256 字节，任何 Read 出错
		// 就让 Identify 返 nil — 所以必须发齐插件期望的 256 字
		// 节再正常关。readFullMBP 只有每个 Read 都成功才返
		// (256, nil)；EOF 会让它返 (n, io.EOF)，触发插件
		// `err != nil` 拒绝命中。
		_ = c.SetWriteDeadline(time.Now().Add(2 * time.Second))
		_, _ = c.Write(replyMBAP())
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	hit := New().Identify(ctx, host, port)
	if hit == nil {
		t.Fatalf("Identify = nil, want hit on Modbus reply (host=%s port=%d)", host, port)
	}
	if hit.Service != "modbus" {
		t.Errorf("Service = %q, want modbus", hit.Service)
	}
	if hit.Banner != "Modbus TCP" {
		t.Errorf("Banner = %q, want %q", hit.Banner, "Modbus TCP")
	}
	if hit.Host != host || hit.Port != port {
		t.Errorf("Host:Port = %s:%d, want %s:%d", hit.Host, hit.Port, host, port)
	}
	if hit.Time.IsZero() {
		t.Error("Time is zero, want the identify timestamp")
	}
}

// TestModbus_CredentialHit documents that modbus.Credential is a
// documented no-op stub returning nil (see modbus.go godoc:
// "Credential 空 stub"). Per the v0.6.0 fake-server plan §11.2 we
// t.Skip when the plugin itself is the no-op rather than the test
// being unable to drive a multi-step handshake.
//
// / TestModbus_CredentialHit 说明 modbus.Credential 是文档化的空
// stub 返回 nil（见 modbus.go godoc: "Credential 空 stub"）。
func TestModbus_CredentialHit(t *testing.T) {
	t.Skip("modbus.Credential is a documented no-op stub (returns nil); see plugin godoc 'Credential 空 stub'.")
}
