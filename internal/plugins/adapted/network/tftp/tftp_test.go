// tftp_test.go — fake-server tests for the TFTP Identify plugin. /
// TFTP 识别插件的假服务器测试。
//
// The fake server speaks the RFC 1350 wire format directly: it reads
// the plugin's RRQ off a UDP socket and answers with a DATA or ERROR
// packet. / 假服务器直接说 RFC 1350 线格式：从 UDP socket 读插件的
// RRQ，回 DATA 或 ERROR 包。
package tftp

import (
	"bytes"
	"context"
	"net"
	"testing"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/fakeserver"
)

// wantRRQ is the exact Read Request the plugin emits: opcode 1,
// filename "x", mode "octet", each NUL-terminated (RFC 1350 §5). /
// wantRRQ 是插件发出的 Read Request：opcode 1、文件名 "x"、模式
// "octet"，各以 NUL 结尾（RFC 1350 §5）。
var wantRRQ = []byte{0x00, 0x01, 'x', 0x00, 'o', 'c', 't', 'e', 't', 0x00}

// dataPacket builds a TFTP DATA packet (opcode 3) for block with n
// payload bytes. A full 512-byte block is what a real server sends
// when the file is larger than one block. / dataPacket 构造 opcode 3
// 的 DATA 包，负载 n 字节。真实 server 在文件大于一个块时发满 512
// 字节。
func dataPacket(block uint16, n int) []byte {
	pkt := make([]byte, 4+n)
	pkt[0], pkt[1] = 0x00, 0x03
	pkt[2], pkt[3] = byte(block>>8), byte(block)
	for i := 4; i < len(pkt); i++ {
		pkt[i] = 'A'
	}
	return pkt
}

// errorPacket builds a TFTP ERROR packet (opcode 5) with code and a
// NUL-terminated message. / errorPacket 构造 opcode 5 的 ERROR 包，
// 带错误码和 NUL 结尾的消息。
func errorPacket(code uint16, msg string) []byte {
	pkt := []byte{0x00, 0x05, byte(code >> 8), byte(code)}
	pkt = append(pkt, msg...)
	return append(pkt, 0x00)
}

// serve starts a UDP fake server that replies with reply and records
// the first datagram it received. / serve 启动回 reply 的 UDP 假服务
// 器，并记录收到的第一个 datagram。
func serve(t *testing.T, reply []byte) (host string, port int, got <-chan []byte) {
	t.Helper()
	ch := make(chan []byte, 1)
	host, port = fakeserver.ListenUDPLoop(t, func(req []byte, _ *net.UDPAddr) []byte {
		select {
		case ch <- append([]byte(nil), req...):
		default: // only the first request is inspected / 只看第一个请求
		}
		return reply
	})
	return host, port, ch
}

// TestTftp_IdentifyHit drives the happy path: the server answers the
// RRQ with DATA block 1 carrying a full 512-byte payload, and the
// plugin reports the block number in its banner. /
// TestTftp_IdentifyHit 跑正常路径：server 用带 512 字节负载的 DATA
// block 1 回应 RRQ，插件在 banner 中报告块号。
func TestTftp_IdentifyHit(t *testing.T) {
	host, port, got := serve(t, dataPacket(1, 512))

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	hit := New().Identify(ctx, host, port)

	if hit == nil {
		t.Fatal("Identify = nil, want hit on TFTP DATA reply")
	}
	if hit.Service != "tftp" {
		t.Errorf("Service = %q, want tftp", hit.Service)
	}
	if hit.Banner != "TFTP (block 1)" {
		t.Errorf("Banner = %q, want %q", hit.Banner, "TFTP (block 1)")
	}
	if hit.Host != host || hit.Port != port {
		t.Errorf("Host:Port = %s:%d, want %s:%d", hit.Host, hit.Port, host, port)
	}
	if hit.Time.IsZero() {
		t.Error("Time is zero, want the identify timestamp")
	}

	// The request must be a well-formed RRQ — a server that only
	// echoes would otherwise pass the assertions above. / 请求必须是
	// 良构的 RRQ — 否则一个只做 echo 的 server 也能通过上面的断言。
	select {
	case req := <-got:
		if !bytes.Equal(req, wantRRQ) {
			t.Errorf("request = %q, want RRQ %q", req, wantRRQ)
		}
	default:
		t.Error("fake server never received a request")
	}
}

// TestTftp_IdentifyErrorReply covers the second valid TFTP banner:
// servers that reject an unknown filename with ERROR are still
// fingerprinted as tftp. / TestTftp_IdentifyErrorReply 覆盖第二种合
// 法 TFTP 响应：用 ERROR 拒绝未知文件名的 server 仍被识别为 tftp。
func TestTftp_IdentifyErrorReply(t *testing.T) {
	tests := []struct {
		name       string
		code       uint16
		msg        string
		wantBanner string
	}{
		// Code 0 also exercises the itoa zero fast path. / code 0 同
		// 时覆盖 itoa 的零值快速路径。
		{"not defined", 0, "not defined", "TFTP (err 0)"},
		{"file not found", 1, "File not found", "TFTP (err 1)"},
		{"no such user", 7, "No such user", "TFTP (err 7)"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			host, port, _ := serve(t, errorPacket(tc.code, tc.msg))

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			hit := New().Identify(ctx, host, port)

			if hit == nil {
				t.Fatalf("Identify = nil, want hit on ERROR code %d", tc.code)
			}
			if hit.Service != "tftp" {
				t.Errorf("Service = %q, want tftp", hit.Service)
			}
			if hit.Banner != tc.wantBanner {
				t.Errorf("Banner = %q, want %q", hit.Banner, tc.wantBanner)
			}
		})
	}
}

// TestTftp_IdentifyMiss verifies replies that are not TFTP-shaped are
// rejected instead of being reported as a false positive. /
// TestTftp_IdentifyMiss 验证非 TFTP 格式的响应被拒绝，不产生误报。
func TestTftp_IdentifyMiss(t *testing.T) {
	tests := []struct {
		name  string
		reply []byte
	}{
		// Shorter than the 4-byte TFTP header. / 短于 4 字节 TFTP 头。
		{"truncated", []byte{0x00, 0x03}},
		// Opcode 4 (ACK) is never a valid answer to an RRQ. /
		// opcode 4 (ACK) 不是 RRQ 的合法响应。
		{"ack opcode", []byte{0x00, 0x04, 0x00, 0x01}},
		// ERROR code 8 is outside the RFC 1350 range 0–7. /
		// ERROR code 8 超出 RFC 1350 的 0–7 范围。
		{"error code out of range", errorPacket(8, "bogus")},
		// A plain text banner from some other UDP service. /
		// 其他 UDP 服务的纯文本 banner。
		{"not tftp", []byte("HTTP/1.1 400 Bad Request")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			host, port, _ := serve(t, tc.reply)

			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if hit := New().Identify(ctx, host, port); hit != nil {
				t.Errorf("Identify = %+v, want nil", hit)
			}
		})
	}
}

// TestTftp_CredentialHit is skipped: tftp declares plugins.
// ModeIdentify only and its Credential is a documented no-op stub
// that unconditionally returns nil (see tftp.go), so there is no
// handshake for a fake server to drive. /
// TestTftp_CredentialHit 跳过：tftp 只声明 plugins.ModeIdentify，其
// Credential 是文档化的 no-op stub，无条件返回 nil（见 tftp.go），
// 没有可供假服务器驱动的握手。
func TestTftp_CredentialHit(t *testing.T) {
	t.Skip("tftp Credential is a documented no-op stub — Identify-only plugin")
}
