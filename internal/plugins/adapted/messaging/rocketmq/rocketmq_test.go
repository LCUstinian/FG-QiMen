// rocketmq_test.go — unit tests for the RocketMQ Identify plugin.
// Uses fakeserver.ListenLoop to drive a real RemotingCommand
// handshake against a fake name-server. The plugin only declares
// ModeIdentify (Credential is a stub), so only IdentifyHit is
// exercised here.
//
// rocketmq_test.go — RocketMQ 识别插件的单元测试。用 fakeserver.ListenLoop
// 驱动真 RemotingCommand 握手去对假 name-server。插件只声明 ModeIdentify
// （Credential 是 stub），所以这里只测 IdentifyHit。
package rocketmq

import (
	"context"
	"encoding/binary"
	"net"
	"testing"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/fakeserver"
)

// respCode is the actual response code the fake server will claim
// (after the plugin strips the 0x80000000 response flag). RocketMQ
// name-server returns 0 for "success / no topics" on a default
// GetRouteInfoByTopic probe. / respCode 是假 server 声明的真响应
// code（剥 0x80000000 响应标志后）。RocketMQ name-server 对默认
// GetRouteInfoByTopic 探测返 0 = 成功 / 无 topic。
const respCode = uint32(0)

// buildRespHeader returns a 16-byte RocketMQ RemotingCommand
// response header with the high bit set (response flag) and the
// actual response code supplied. / buildRespHeader 返回 16 字节
// RocketMQ RemotingCommand 响应头，高位置位（响应标志），真响应
// code 由入参给出。
func buildRespHeader(code uint32) []byte {
	hdr := make([]byte, 16)
	binary.BigEndian.PutUint32(hdr[0:4], code|0x80000000) // response flag set
	binary.BigEndian.PutUint32(hdr[4:8], 0)               // language (Java)
	binary.BigEndian.PutUint32(hdr[8:12], 0)              // version
	binary.BigEndian.PutUint32(hdr[12:16], 1)             // opaque
	return hdr
}

// TestIdentifyHit spins up a fake name-server that replies with a
// well-formed RocketMQ RemotingCommand response header. The plugin
// should return a *Result with Service="rocketmq" and a banner
// that exposes the stripped response code.
//
// / TestIdentifyHit 启一个假 name-server，返格式正确的 RocketMQ
// RemotingCommand 响应头。插件应返 Service="rocketmq"、banner 露
// 出剥标志后的响应码的 *Result。
func TestIdentifyHit(t *testing.T) {
	host, port := fakeserver.ListenLoop(t, func(c net.Conn) {
		// Drain whatever the client sends (the plugin's 16-byte
		// request header). Then write our canned response header.
		// / 把客户端发的内容读完（插件的 16 字节请求头），然后写
		// 我们的固定响应头。
		_, _ = c.Read(make([]byte, 64))
		_, _ = c.Write(buildRespHeader(respCode))
	})

	p := New()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	res := p.Identify(ctx, host, port)
	if res == nil {
		t.Fatal("Identify returned nil for valid RocketMQ response")
	}
	if res.Service != "rocketmq" {
		t.Errorf("Service = %q, want %q", res.Service, "rocketmq")
	}
	wantBanner := "RocketMQ (resp_code=0)"
	if res.Banner != wantBanner {
		t.Errorf("Banner = %q, want %q", res.Banner, wantBanner)
	}
}

// TestIdentifyMissNoResponseFlag spins up a fake server that replies
// with a header that does NOT have the response flag bit set. The
// plugin must return nil (so it does not mistake a request-shaped
// packet for a RocketMQ server response).
//
// / TestIdentifyMissNoResponseFlag 启一个假 server 返响应标志位未
// 置的响应头。插件必须返 nil（不会把请求型包误认成 RocketMQ 响应）。
func TestIdentifyMissNoResponseFlag(t *testing.T) {
	host, port := fakeserver.ListenLoop(t, func(c net.Conn) {
		_, _ = c.Read(make([]byte, 64))
		// Send a header with bit 31 clear (looks like a request,
		// not a response). The plugin's first sanity check rejects
		// this. / 发 bit 31 未置的响应头（看起来像请求不像响应）。
		// 插件的首次健全检查会拒掉。
		bogus := make([]byte, 16)
		binary.BigEndian.PutUint32(bogus[0:4], 105) // code=GetRouteInfoByTopic, no response flag
		_, _ = c.Write(bogus)
	})

	p := New()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if got := p.Identify(ctx, host, port); got != nil {
		t.Errorf("Identify with no response flag = %+v, want nil", got)
	}
}

// TestIdentifyShortHeader covers the case where the server sends
// fewer than 16 bytes (e.g. connection closed mid-write). The
// plugin's Read returns n<16 and the function returns nil.
//
// / TestIdentifyShortHeader 覆盖 server 发的字节不到 16 字节的情况
// （如连接中途关闭）。插件 Read 返 n<16 时函数返 nil。
func TestIdentifyShortHeader(t *testing.T) {
	host, port := fakeserver.ListenLoop(t, func(c net.Conn) {
		_, _ = c.Read(make([]byte, 64))
		// Send only 8 bytes — half a header. / 只发 8 字节——半头。
		_, _ = c.Write([]byte{0x80, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00})
	})

	p := New()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if got := p.Identify(ctx, host, port); got != nil {
		t.Errorf("Identify with short header = %+v, want nil", got)
	}
}

// TestRequestFormat locks the wire format the plugin emits:
//   - 16 bytes
//   - code  = 105 (GET_ROUTEINFO_BY_TOPIC)
//   - language = 0 (Java)
//   - version  = 0
//   - opaque   = 1
//
// / TestRequestFormat 钉死插件发的线协议格式：
//   - 16 字节
//   - code  = 105（GET_ROUTEINFO_BY_TOPIC）
//   - language = 0（Java）
//   - version  = 0
//   - opaque   = 1
func TestRequestFormat(t *testing.T) {
	type capture struct{ frame []byte }
	got := make(chan capture, 1)
	_, port := fakeserver.ListenLoop(t, func(c net.Conn) {
		frame := make([]byte, 16)
		_, _ = c.Read(frame)
		got <- capture{frame: frame}
		// Reply with a valid response so the plugin doesn't hang.
		// / 返有效响应，避免插件卡住。
		_, _ = c.Write(buildRespHeader(respCode))
	})

	p := New()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = p.Identify(ctx, "127.0.0.1", port)

	select {
	case fr := <-got:
		if len(fr.frame) != 16 {
			t.Fatalf("frame length = %d, want 16 (frame=%x)", len(fr.frame), fr.frame)
		}
		wantCode := uint32(105)
		wantLang := uint32(0)
		wantVer := uint32(0)
		wantOpaque := uint32(1)
		if got := binary.BigEndian.Uint32(fr.frame[0:4]); got != wantCode {
			t.Errorf("code = %d, want %d", got, wantCode)
		}
		if got := binary.BigEndian.Uint32(fr.frame[4:8]); got != wantLang {
			t.Errorf("language = %d, want %d", got, wantLang)
		}
		if got := binary.BigEndian.Uint32(fr.frame[8:12]); got != wantVer {
			t.Errorf("version = %d, want %d", got, wantVer)
		}
		if got := binary.BigEndian.Uint32(fr.frame[12:16]); got != wantOpaque {
			t.Errorf("opaque = %d, want %d", got, wantOpaque)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("server did not receive request within 2s")
	}
}
