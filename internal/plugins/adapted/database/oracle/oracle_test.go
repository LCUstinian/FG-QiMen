// oracle_test.go — A.6 coverage baseline for the oracle plugin.
//
// oracle_test.go — oracle 插件的 A.6 覆盖率基线。
//
// Wire format being faked (oracle.go:131-173):
//
//	client writes TNS Connect packet (header 23 bytes + service data):
//	  uint16 length (BE) | uint16 checksum | uint8 type=1 | uint8 reserved
//	  | uint16 hdr-checksum | ... version / SDU / MTU / flags ...
//	  | uint8 len+1 | 0x01 | service bytes
//	server replies with an 8-byte TNS header whose type byte at
//	  index 4 is:
//	    2 → Accept  (hit, banner "Oracle TNS (Accept)")
//	    4 → Refuse  (hit, banner "Oracle TNS (Refuse)")
//	    else → miss (nil)
//
// The plugin inspects only hdr[4]; the body is irrelevant to the
// hit/miss decision. The handler drains the client request bytes
// (so the kernel doesn't RST the connection) and writes the
// minimum canonical 8-byte header with the chosen type byte.
//
// / 仿真的线协议格式 (oracle.go:131-173)：客户端写 TNS Connect 包
// （23 字节头 + service data），服务端回 8 字节 TNS 头，其中
// 索引 4 的类型字节为 2=Accept（命中，banner "Oracle TNS (Accept)"）
// 或 4=Refuse（命中，banner "Oracle TNS (Refuse)"），其余返回
// nil。插件只看 hdr[4]，body 不影响命中判断。handler 消耗客户端
// 写过来的字节以防内核 RST，然后写最小的 8 字节头 + 类型字节。
package oracle

import (
	"context"
	"encoding/binary"
	"net"
	"testing"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/fakeserver"
)

// drainTNSConnect reads and discards the client's TNS Connect packet
// so the connection's receive buffer is drained before we write the
// response. Prevents RST on platforms that close the read side
// before write. The plugin sends a ~28 byte packet for service
// "ORCL" (23-byte header + 5-byte connect data); we read with a
// 4 KiB buffer to be safe.
// / drainTNSConnect 读并丢弃客户端的 TNS Connect 包，避免我们写
// 响应时 read 侧被关导致 RST。service "ORCL" 时包约 28 字节
// （23 字节头 + 5 字节 connect data），用 4 KiB 缓冲区以防万一。
func drainTNSConnect(c net.Conn) {
	buf := make([]byte, 4096)
	_, _ = c.Read(buf)
}

// tnsReplyHeader builds an 8-byte TNS response header with the given
// packet type byte at index 4 (Accept=2, Refuse=4). The first two
// bytes are the big-endian packet length (8), bytes 2-3 are the
// checksum (zeroed — the plugin ignores them), byte 4 is pktType,
// byte 5 is reserved, bytes 6-7 are the header checksum (zeroed).
// The plugin only reads up to hdr[4] before deciding hit/miss.
// / tnsReplyHeader 构造 8 字节 TNS 响应头，索引 4 是给定的包类型
// 字节（Accept=2、Refuse=4）。前 2 字节是大端包长（8），2-3 是
// checksum（置零——插件忽略），4 是 pktType，5 是 reserved，6-7
// 是头 checksum（置零）。插件读取到 hdr[4] 就做命中判断。
func tnsReplyHeader(pktType byte) []byte {
	hdr := make([]byte, 8)
	binary.BigEndian.PutUint16(hdr[0:2], 8) // length (BE)
	hdr[2] = 0                              // checksum
	hdr[3] = 0
	hdr[4] = pktType
	hdr[5] = 0 // reserved
	hdr[6] = 0 // header checksum
	hdr[7] = 0
	return hdr
}

// TestOracle_IdentifyHit spins up a TCP fake server that answers
// with the TNS Accept type byte (2). The plugin must identify the
// service as "oracle" with banner "Oracle TNS (Accept)".
// / 启 TCP 假 server 回 TNS Accept（类型字节 2）。插件应识别服务
// 为 "oracle"，banner 为 "Oracle TNS (Accept)"。
func TestOracle_IdentifyHit(t *testing.T) {
	host, port := fakeserver.ListenLoop(t, func(c net.Conn) {
		drainTNSConnect(c)
		_, _ = c.Write(tnsReplyHeader(2)) // 2 = Accept
	})
	p := New()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	r := p.Identify(ctx, host, port)
	if r == nil {
		t.Fatal("Identify returned nil for TNS Accept response")
	}
	if r.Service != "oracle" {
		t.Errorf("Service = %q, want %q", r.Service, "oracle")
	}
	if r.Banner != "Oracle TNS (Accept)" {
		t.Errorf("Banner = %q, want %q", r.Banner, "Oracle TNS (Accept)")
	}
	if r.Host != host || r.Port != port {
		t.Errorf("Host/Port = %q:%d, want %q:%d", r.Host, r.Port, host, port)
	}
}

// TestOracle_IdentifyRefuse covers the second accepted response
// shape at oracle.go:116-123 — TNS Refuse (type 4) is still an
// Oracle TNS hit, just with a refuse banner. This matters because
// real Oracle listeners that reject our connect still prove they're
// Oracle TNS endpoints.
// / 验证 oracle.go:116-123 接受的第二种响应形态：TNS Refuse
// （类型字节 4）仍是 oracle 命中，只是 banner 标 refuse。真实
// Oracle 监听器拒了我们连接时仍证明它是 Oracle TNS 端点。
func TestOracle_IdentifyRefuse(t *testing.T) {
	host, port := fakeserver.ListenLoop(t, func(c net.Conn) {
		drainTNSConnect(c)
		_, _ = c.Write(tnsReplyHeader(4)) // 4 = Refuse
	})
	p := New()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	r := p.Identify(ctx, host, port)
	if r == nil {
		t.Fatal("Identify returned nil for TNS Refuse response")
	}
	if r.Service != "oracle" {
		t.Errorf("Service = %q, want %q", r.Service, "oracle")
	}
	if r.Banner != "Oracle TNS (Refuse)" {
		t.Errorf("Banner = %q, want %q", r.Banner, "Oracle TNS (Refuse)")
	}
}

// TestOracle_IdentifyMiss covers the negative case: the server
// replies with bytes whose type byte at index 4 is neither Accept
// (2) nor Refuse (4). The plugin must return nil so we don't
// false-positive on a non-Oracle TCP service.
// / 验证反向 case：服务端 hdr[4] 既非 Accept（2）也非 Refuse（4）。
// 插件必须返 nil，避免在非 Oracle 服务上假阳性。
func TestOracle_IdentifyMiss(t *testing.T) {
	host, port := fakeserver.ListenLoop(t, func(c net.Conn) {
		drainTNSConnect(c)
		// pktType=7 is neither Accept (2) nor Refuse (4) — no hit.
		// / pktType=7 既不是 Accept 也不是 Refuse，不命中。
		_, _ = c.Write(tnsReplyHeader(7))
	})
	p := New()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if got := p.Identify(ctx, host, port); got != nil {
		t.Errorf("Identify with non-Oracle reply = %+v, want nil", got)
	}
}

// TestOracle_CredentialHit is skipped: the oracle plugin's
// Credential is a documented no-op stub (always returns nil) per
// oracle.go:75-78 — actual credential testing lives in
// core/cred/protocols/oracle.go (OracleAuthenticator via go-ora).
// / TestOracle_CredentialHit 跳过：oracle 插件的 Credential 是
// 有文档说明的空 stub（始终返回 nil），见 oracle.go:75-78——真正
// 的凭证测试在 core/cred/protocols/oracle.go（OracleAuthenticator，
// go-ora）。
func TestOracle_CredentialHit(t *testing.T) {
	t.Skip("oracle.Credential is a no-op stub; see oracle.go Credential()")
}
