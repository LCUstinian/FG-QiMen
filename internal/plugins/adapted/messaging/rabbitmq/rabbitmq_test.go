// rabbitmq_test.go — fake-server test for the AMQP 0-9-1 Identify plugin.
// rabbitmq_test.go — AMQP 0-9-1 识别插件的 fake-server 测试。
package rabbitmq

import (
	"context"
	"encoding/binary"
	"net"
	"testing"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/fakeserver"
	"github.com/LCUstinian/FG-QiMen/internal/types"
)

// AMQP 0-9-1 Connection.Start frame (class=10, method=11).
// AMQP 0-9-1 Connection.Start 帧（class=10, method=11）。
//
// Frame layout:
//
//	byte 0:    frame type (1 = method)
//	byte 1-2:  channel id (0)
//	byte 3-6:  payload size (BE uint32, includes the 4-byte end sentinel)
//	byte 7-8:  class (10 = Connection)
//	byte 9-10: method (11 = Start)
//	byte 11+:  method args (we send a minimal server-properties dict)
//	last 4:    frame-end sentinel (0xCE)
//
// We pad the body to >= 11 bytes (the plugin's `size < 11 → nil` guard)
// and ensure the class/method match. / 把 body 凑到 ≥11 字节过插件 size
// 检查，class/method 对得上。
func rabbitmqStartFrame() []byte {
	// Minimal body: class(2) + method(2) + 7 bytes padding = 11 bytes body.
	body := make([]byte, 11)
	binary.BigEndian.PutUint16(body[0:2], 0x000a) // Connection
	binary.BigEndian.PutUint16(body[2:4], 0x000b) // Start
	// body[4:11] is zero — minimal server-properties.

	frame := make([]byte, 7+len(body)+4)                        // header + body + end sentinel
	frame[0] = 0x01                                             // method frame
	binary.BigEndian.PutUint16(frame[1:3], 0)                   // channel 0
	binary.BigEndian.PutUint32(frame[3:7], uint32(len(body)+4)) // size (incl. end)
	copy(frame[7:], body)
	// frame-end sentinel = 0xCE (already zeroed, which is fine — both 0x00
	// and 0xCE are accepted by the plugin's parser).
	return frame
}

func TestRabbitMQ_IdentifyHit(t *testing.T) {
	host, port := fakeserver.ListenLoop(t, func(c net.Conn) {
		// Drain the 8-byte AMQP protocol header sent by the plugin.
		drain := make([]byte, 8)
		_, _ = c.Read(drain)
		// Reply with a Connection.Start frame.
		_, _ = c.Write(rabbitmqStartFrame())
		_ = c.Close()
	})
	p := New()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	r := p.Identify(ctx, host, port)
	if r == nil {
		t.Fatal("expected hit, got nil")
	}
	if r.Service != "rabbitmq" {
		t.Errorf("Service = %q, want %q", r.Service, "rabbitmq")
	}
}

func TestRabbitMQ_IdentifyMissWrongClass(t *testing.T) {
	// Same frame layout but class=20 (Channel.Open) instead of 10 (Connection.Start).
	// Plugin should reject (class/method mismatch).
	host, port := fakeserver.ListenLoop(t, func(c net.Conn) {
		drain := make([]byte, 8)
		_, _ = c.Read(drain)
		body := make([]byte, 11)
		binary.BigEndian.PutUint16(body[0:2], 0x0014) // Channel class
		binary.BigEndian.PutUint16(body[2:4], 0x000b) // Open method
		frame := make([]byte, 7+len(body)+4)
		frame[0] = 0x01
		binary.BigEndian.PutUint16(frame[1:3], 0)
		binary.BigEndian.PutUint32(frame[3:7], uint32(len(body)+4))
		copy(frame[7:], body)
		_, _ = c.Write(frame)
		_ = c.Close()
	})
	p := New()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	r := p.Identify(ctx, host, port)
	if r != nil {
		t.Errorf("expected nil (wrong class), got %+v", r)
	}
}

func TestRabbitMQ_IdentifyMissShortBody(t *testing.T) {
	// Frame with body < 11 bytes — plugin's size check should reject.
	host, port := fakeserver.ListenLoop(t, func(c net.Conn) {
		drain := make([]byte, 8)
		_, _ = c.Read(drain)
		body := make([]byte, 5) // < 11
		frame := make([]byte, 7+len(body)+4)
		frame[0] = 0x01
		binary.BigEndian.PutUint16(frame[1:3], 0)
		binary.BigEndian.PutUint32(frame[3:7], uint32(len(body)+4))
		copy(frame[7:], body)
		_, _ = c.Write(frame)
		_ = c.Close()
	})
	p := New()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	r := p.Identify(ctx, host, port)
	if r != nil {
		t.Errorf("expected nil (short body), got %+v", r)
	}
}

func TestRabbitMQ_CredentialHit(t *testing.T) {
	// Credential() is a documented no-op stub returning nil — pin it.
	p := New()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	r := p.Credential(ctx, "127.0.0.1", 5672, []types.Cred{
		{User: "guest", Pass: "guest"},
	})
	if r != nil {
		t.Errorf("expected nil (Credential is no-op stub), got %+v", r)
	}
}
