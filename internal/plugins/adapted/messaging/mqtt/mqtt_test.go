// mqtt_test.go — fake-server tests for the MQTT 3.1.1 Identify plugin.
//
// mqtt_test.go — MQTT 3.1.1 识别插件的 fake-server 测试。
//
// Pattern: shared fakeserver.ListenLoop spins up a TCP listener
// on 127.0.0.1:0 and dispatches each accepted connection to a
// per-test handler that speaks the wire protocol expected by
// identifyMQTT. / 模式：共享 fakeserver.ListenLoop 在 127.0.0.1:0
// 起 TCP listener，把每个 accepted conn 分发给每个测试自己的
// handler，后者按 identifyMQTT 期望的线协议回包。
//
// MQTT 3.1.1 (OASIS Standard) wire shape we exercise:
//
//	CONNECT (client → broker):
//	  0x10 | remaining_len | "MQTT" | 0x04 | 0x02 | 0x00 0x3C
//	  | 0x00 0x05 | "fg-qm"
//
//	CONNACK (broker → client, return code 0):
//	  0x20 | 0x02 | 0x00 | 0x00
//
// The minimal CONNECT is 19 bytes (1 type + 1 remaining_len +
// 10 var_header + 7 payload). The fake server reads exactly
// that many bytes (with a read deadline as belt-and-braces)
// before writing CONNACK — otherwise io.Copy would deadlock
// because the plugin never closes the connection itself.
package mqtt

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/fakeserver"
	"github.com/LCUstinian/FG-QiMen/internal/types"
)

// mqttConnectFrameLen is the byte length of the minimal
// CONNECT the plugin always sends (see identifyMQTT in mqtt.go):
// 1 type + 1 remaining_len(=17) + 10 var_header + 7 payload =
// 19. / mqttConnectFrameLen 是插件最小 CONNECT（见 mqtt.go 中
// identifyMQTT）的字节长度：1 type + 1 remaining_len(=17) + 10
// var_header + 7 payload = 19。
const mqttConnectFrameLen = 19

// drainConnect reads exactly the plugin's CONNECT frame (with
// a short read deadline so a buggy plugin can't deadlock the
// test). Returns true if it saw the full CONNECT, false on
// short read / timeout. / drainConnect 读恰好插件的 CONNECT 帧
// （用短读 deadline 防 bug 插件死锁测试）。读到完整 CONNECT
// 返 true，短读 / 超时返 false。
func drainConnect(c net.Conn) bool {
	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, mqttConnectFrameLen)
	_, err := readFull(c, buf)
	_ = c.SetReadDeadline(time.Time{})
	return err == nil
}

// readFull is a small helper: read exactly len(buf) bytes or
// return the last error. / readFull 小 helper：读恰好 len(buf)
// 字节，否则返最后的错误。
func readFull(c net.Conn, buf []byte) (int, error) {
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

// TestMqtt_IdentifyHit: broker replies with CONNACK + return
// code 0 (ACCEPTED). Plugin must identify it as MQTT. / broker
// 返 CONNACK + 返回码 0（ACCEPTED）。插件必须识别为 MQTT。
func TestMqtt_IdentifyHit(t *testing.T) {
	host, port := fakeserver.ListenLoop(t, func(c net.Conn) {
		// Drain the CONNECT frame the plugin sent so it doesn't
		// block on the write back. / 把插件发的 CONNECT 读完，
		// 避免插件回包时阻塞。
		drainConnect(c)
		// CONNACK fixed header (0x20 type, 0x02 remaining length)
		// + variable header (0x00 session present, 0x00 return
		// code ACCEPTED). / CONNACK fixed header（0x20 类型、
		// 0x02 remaining length）+ variable header（0x00
		// session present、0x00 返回码 ACCEPTED）。
		_, _ = c.Write([]byte{0x20, 0x02, 0x00, 0x00})
	})
	p := New()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	r := p.Identify(ctx, host, port)
	if r == nil {
		t.Fatal("expected hit (CONNACK code 0), got nil")
	}
	if r.Service != "mqtt" {
		t.Errorf("Service = %q, want %q", r.Service, "mqtt")
	}
	if r.Banner == "" {
		t.Errorf("Banner empty, want CONNACK summary")
	}
}

// TestMqtt_IdentifyMissBadConnAck: server replies with CONNACK
// but return code 6 (reserved per spec §3.2.2.3). Plugin must
// NOT report this as MQTT. / server 返 CONNACK 但返回码 6（按
// 规范 §3.2.2.3 是保留值）。插件不能报为 MQTT。
func TestMqtt_IdentifyMissBadConnAck(t *testing.T) {
	host, port := fakeserver.ListenLoop(t, func(c net.Conn) {
		drainConnect(c)
		// 0x06 = reserved / protocol error per MQTT 3.1.1 §3.2.2.3.
		_, _ = c.Write([]byte{0x20, 0x02, 0x00, 0x06})
	})
	p := New()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if r := p.Identify(ctx, host, port); r != nil {
		t.Errorf("expected nil (reserved CONNACK code 6), got %+v", r)
	}
}

// TestMqtt_IdentifyMissNonMQTT: server replies with bytes that
// don't have the CONNACK fixed-header type byte (0x20). Plugin
// must NOT report this as MQTT. / server 返的不是 CONNACK 起始
// 字节（0x20）的乱码。插件不能报为 MQTT。
func TestMqtt_IdentifyMissNonMQTT(t *testing.T) {
	host, port := fakeserver.ListenLoop(t, func(c net.Conn) {
		drainConnect(c)
		_, _ = c.Write([]byte("HTTP/1.0 200 OK\r\n\r\n"))
	})
	p := New()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if r := p.Identify(ctx, host, port); r != nil {
		t.Errorf("expected nil (non-MQTT reply), got %+v", r)
	}
}

// TestMqtt_CredentialNoOp: per mqtt.go's HARD rule we don't
// credential-spray MQTT brokers (most use mTLS / device certs,
// and we don't do post-auth actions either). Credential() is a
// documented no-op stub returning nil — pin that contract so
// any future change that turns it into a network call surfaces
// here. / 按 mqtt.go 的硬性原则，我们不对 MQTT broker 做凭据喷
// 洒（多数用 mTLS / 设备证书，也无后认证动作）。Credential()
// 是文档化的 no-op stub 返 nil——把这条契约钉死，将来若有改
// 动把它变成网络调用就会在这里浮出来。
func TestMqtt_CredentialNoOp(t *testing.T) {
	p := New()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	r := p.Credential(ctx, "127.0.0.1", 1883, []types.Cred{
		{User: "admin", Pass: "admin"},
	})
	if r != nil {
		t.Errorf("expected nil (Credential is no-op stub), got %+v", r)
	}
}
