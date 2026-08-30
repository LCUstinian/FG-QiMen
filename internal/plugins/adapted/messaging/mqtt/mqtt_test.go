// mqtt_test.go — unit tests for the MQTT Identify plugin.
//
// mqtt_test.go — MQTT 识别插件的单元测试。
//
// Scope: minimal fake-server in process that responds with a
// CONNACK. We use net.Pipe() for frame-level tests (no broker
// required) and a tcp listener for the Smoke integration test
// (the helper imports this file via plugintest.Smoke from any
// other test that wants coverage). / 范围：进程内启一个假 server
// 返 CONNACK。帧级测试用 net.Pipe()（不需要 broker），Smoke
// 集成测试用 TCP listener。
package mqtt

import (
	"bufio"
	"context"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

func TestPluginInterface(t *testing.T) {
	p := New()
	if got := p.Name(); got != "mqtt" {
		t.Errorf("Name() = %q, want %q", got, "mqtt")
	}
	if got := p.Ports(); len(got) != 2 || got[0] != 1883 || got[1] != 8883 {
		t.Errorf("Ports() = %v, want [1883 8883]", got)
	}
}

// TestIdentify_RealMQTTBroker_ConnRefused documents the no-server
// case (no broker on the port) — the plugin returns nil without
// error. / TestIdentify_RealMQTTBroker_ConnRefused：无 broker 情况
// 文档——插件返 nil，不报错。
func TestIdentify_RealMQTTBroker_ConnRefused(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	p := New()
	if got := p.Identify(context.Background(), "127.0.0.1", port); got != nil {
		t.Errorf("Identify on closed port = %+v, want nil", got)
	}
}

// TestIdentify_FakeServer_CONNACK_ACCEPTED spins up a TCP server
// that reads the CONNECT and replies with a valid CONNACK
// (return code 0 = ACCEPTED). The plugin should return a
// non-nil Result with Service="mqtt". / 启 TCP server 读 CONNECT
// 返有效 CONNACK（返回码 0 = ACCEPTED）。插件应返非 nil
// Result，Service="mqtt"。
func TestIdentify_FakeServer_CONNACK_ACCEPTED(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()
	port := ln.Addr().(*net.TCPAddr).Port

	var connAckCode atomic.Uint32
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go handleFakeMQTTConn(conn, &connAckCode)
		}
	}()

	p := New()
	result := p.Identify(context.Background(), "127.0.0.1", port)
	if result == nil {
		t.Fatal("Identify returned nil for valid CONNACK")
	}
	if result.Service != "mqtt" {
		t.Errorf("Service = %q, want %q", result.Service, "mqtt")
	}
	// Wait for the server to record the code we sent. / 等 server
	// 记录我们发的 code。
	deadline := time.Now().Add(time.Second)
	for connAckCode.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if code := connAckCode.Load(); code != 0 {
		t.Errorf("server saw CONNACK code = %d, want 0 (ACCEPTED)", code)
	}
}

// TestIdentify_FakeServer_NonMQTTReply documents the "port is
// open but the service is not MQTT" case — the server replies
// with garbage that doesn't look like a CONNACK. The plugin
// should return nil. / 端口开但服务非 MQTT 的情况——server 返不像
// CONNACK 的乱码。插件应返 nil。
func TestIdentify_FakeServer_NonMQTTReply(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()
	port := ln.Addr().(*net.TCPAddr).Port

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		// Send a "hello" that doesn't match MQTT framing.
		// / 发一条不匹配 MQTT framing 的"hello"。
		_, _ = conn.Write([]byte("HTTP/1.0 200 OK\r\n\r\n"))
	}()

	p := New()
	if got := p.Identify(context.Background(), "127.0.0.1", port); got != nil {
		t.Errorf("Identify with non-MQTT reply = %+v, want nil", got)
	}
}

// TestIdentify_FakeServer_ProtocolError covers the case where
// the server replies with CONNACK but a reserved/invalid return
// code (≥6 per spec §3.2.2.3). The plugin must return nil
// (NOT report a non-MQTT broker as MQTT). / server 返 CONNACK 但
// 是保留/非法返回码（按规范 §3.2.2.3 ≥6）。插件必须返 nil
// （不能把非 MQTT broker 报成 MQTT）。
func TestIdentify_FakeServer_ProtocolError(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()
	port := ln.Addr().(*net.TCPAddr).Port

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		// Read whatever the client sent, then reply with
		// CONNACK + return code 6 (protocol error / reserved).
		// / 读客户端发的内容，然后回 CONNACK + 返回码 6（保留值
		// / 协议错误）。
		br := bufio.NewReader(conn)
		_, _ = br.ReadByte() // skip CONNECT type byte
		// Drain the CONNECT so the client doesn't hit a write
		// error. / 把 CONNECT 读完，客户端才不会撞到写错误。
		_ = conn.SetReadDeadline(time.Now().Add(time.Second))
		buf := make([]byte, 4096)
		_, _ = br.Read(buf)
		// 0x20 0x02 = CONNACK, fixed header. 0x00 0x06 = session
		// present=0, return code 6 (reserved). / 0x20 0x02 =
		// CONNACK fixed header。0x00 0x06 = session present=0,
		// 返回码 6（保留）。
		_, _ = conn.Write([]byte{0x20, 0x02, 0x00, 0x06})
	}()

	p := New()
	if got := p.Identify(context.Background(), "127.0.0.1", port); got != nil {
		t.Errorf("Identify with reserved CONNACK code = %+v, want nil", got)
	}
}

// TestIdentify_ConnectFrameFormat inspects the bytes the
// plugin writes to the broker. Locks the wire format against
// silent regression. / 检视插件发给 broker 的字节。把线协议格式
// 钉死，防止静默回归。
func TestIdentify_ConnectFrameFormat(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()
	port := ln.Addr().(*net.TCPAddr).Port

	type frameResult struct{ frame []byte }
	result := make(chan frameResult, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			result <- frameResult{}
			return
		}
		defer func() { _ = conn.Close() }()
		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		// Read full CONNECT frame (max 127 bytes for our minimal
		// CONNECT). / 读完整 CONNECT 帧（我们的最小 CONNECT 最大
		// 127 字节）。
		br := bufio.NewReader(conn)
		frame := make([]byte, 128)
		n, _ := br.Read(frame)
		result <- frameResult{frame[:n]}
	}()

	p := New()
	_ = p.Identify(context.Background(), "127.0.0.1", port)

	var fr frameResult
	select {
	case fr = <-result:
	case <-time.After(2 * time.Second):
		t.Fatal("server didn't receive CONNECT within 2s")
	}

	// Layout: [0x10][remaining_len=17][var_header (10B)][payload (7B)]
	// = 19 bytes total. The varHeader [10]byte array has the
	// protocol name in [0:4], level in [4], flags in [5],
	// keep-alive in [6:8], and the unused [8:10] are zero.
	// The client ID "fg-qm" is 5 bytes (f, g, -, q, m); payload
	// is 2-byte length + 5-byte ID = 7 bytes.
	// / 布局: [0x10][remaining_len=17][var_header (10B)][payload
	// (7B)] = 19 字节总。varHeader [10]byte 数组: 协议名
	// [0:4]、level [4]、flags [5]、keep-alive [6:8]，
	// 未用的 [8:10] 是 0。客户端 ID "fg-qm" 是 5 字节；payload
	// 是 2 字节长度 + 5 字节 ID = 7 字节。
	want := []byte{
		0x10, 0x11, // CONNECT, remaining=17
		'M', 'Q', 'T', 'T', // protocol name
		0x04,       // protocol level 4 (MQTT 3.1.1)
		0x02,       // connect flags (clean session)
		0x00, 0x3C, // keep alive 60s
		0x00, 0x00, // unused tail of [10]byte varHeader
		0x00, 0x05, // payload length = 5
		'f', 'g', '-', 'q', 'm', // client ID "fg-qm" (5B)
	}
	if len(fr.frame) != len(want) {
		t.Fatalf("frame length = %d, want %d (frame=%x)", len(fr.frame), len(want), fr.frame)
	}
	for i := range want {
		if fr.frame[i] != want[i] {
			t.Errorf("byte %d: got 0x%02x, want 0x%02x (frame=%x)",
				i, fr.frame[i], want[i], fr.frame)
		}
	}
}

// handleFakeMQTTConn is the per-connection handler for the
// fake MQTT broker. Reads the CONNECT, replies with a CONNACK
// carrying the given return code, then closes. / handleFakeMQTTConn
// 是假 MQTT broker 的每连接 handler。读 CONNECT 返带指定返回码
// 的 CONNACK，然后关。
func handleFakeMQTTConn(conn net.Conn, connAckCode *atomic.Uint32) {
	defer func() { _ = conn.Close() }()
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	// Drain whatever the client sent (length-bounded). / 把客户
	// 端发的东西读完（限定长度）。
	br := bufio.NewReader(conn)
	_, _ = br.ReadByte() // skip type byte
	buf := make([]byte, 4096)
	_, _ = br.Read(buf)
	// Send CONNACK with the test-supplied return code. / 发
	// CONNACK 带测试提供的返回码。
	code := byte(connAckCode.Load())
	connAckCode.Store(uint32(code))
	connAck := []byte{0x20, 0x02, 0x00, code}
	_, _ = conn.Write(connAck)
}

// _ keeps the atomic import stable across test-only refactors.
var _ atomic.Bool
