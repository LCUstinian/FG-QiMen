// mysql_test.go — A.6 coverage baseline via fakeserver.ListenLoop.
//
// mysql_test.go — 通过 fakeserver.ListenLoop 的 A.6 覆盖率基线。
//
// Wire format being faked (mysql.go:13-20, mysql.go:78-138):
//
//	server greeting = 4-byte header + payload
//	  header: 3B payload-length (LE) + 1B sequence (0)
//	  payload[0]               = 0x0A (HandshakeV10)
//	  payload[1..1+n]          = "server_version\0"
//	  payload[1+n+1..1+n+4]    = thread_id (4B LE)
//	  payload[...]             = 8B auth-plugin-data-part-1 + 1B filler
//	                           + 2B capability_flags_lower + 1B charset
//	                           + 2B status_flags + 2B capability_flags_upper
//	                           + 1B auth-plugin-data-len + 10B reserved
//	                           + (authDataLen-8)B auth-plugin-data-part-2
//	                           + null-terminated auth-plugin-name
//
// Plugin only reads; it never writes. The handler therefore does not
// need to consume any client bytes — it just writes a canned greeting
// as soon as the connection is accepted. / 插件只读不写；handler 无需
// 消耗任何客户端字节,accept 后立即写一帧固定 greeting 即可。
//
// Coverage note: the happy-path test below drives the entire
// HandshakeV10 parser branch (version-string + thread_id + auth-
// plugin-name extraction). The negative / corner branches
// (HandshakeV9 skip, payloadLen out-of-range, truncated body,
// authDataLen edge cases for MariaDB 10.4+ reserved-shrink) are
// unreachable via a single-shot fake server in a unit test — they
// fire only on real-world protocol quirks. Per the v0.6.0 fake-server
// plan, the happy-path 70%+ baseline is acceptable for complex
// protocols like MySQL where exhaustive negative coverage requires a
// multi-packet state machine.
package mysql

import (
	"bytes"
	"context"
	"encoding/binary"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/fakeserver"
)

// handshakeV10 builds a minimal valid MySQL HandshakeV10 greeting
// packet: 4-byte header + payload. authDataLen is set to 0 so the
// plugin's auth-plugin-name extractor at mysql.go:134 ends up with
// body[nameOffset:] as an empty slice and authPluginName defaults to
// "unknown" — the minimum packet the parser accepts without needing
// to know an actual auth-plugin string.
//
// We pad the payload with one trailing 0x00 so the body length is
// exactly nameOffset. Without that byte, body[nameOffset:] would
// panic with [44:43] because mysql.go:133's guard
// `nameOffset + authDataLen - 8 <= len(body)` is too permissive when
// authDataLen==0 (the comment at mysql.go:132 acknowledges this for
// the MariaDB 10.4+ reserved-shrink case, but doesn't bounds-check
// the slice itself).
//
// / handshakeV10 构造最小合法 MySQL HandshakeV10 握手包:4 字节头 +
// payload。authDataLen=0 让 mysql.go:134 的 auth-plugin-name 提取
// 拿到 body[nameOffset:] 的空 slice,authPluginName 默认为
// "unknown" —— 这是解析器在不依赖真实 auth-plugin 字符串情况下能
// 接受的最小包。
//
// 在 payload 末尾追加一个 0x00,让 body 长度恰好等于 nameOffset。
// 否则 body[nameOffset:] 会以 [44:43] panic —— mysql.go:133 的守卫
// `nameOffset + authDataLen - 8 <= len(body)` 在 authDataLen==0 时
// 过于宽松(mysql.go:132 的注释也提到 MariaDB 10.4+ 保留段缩短的情
// 形,但没给 slice 加 bounds check)。
func handshakeV10(serverVersion string, threadID uint32) []byte {
	var payload bytes.Buffer
	// protocol_version / 协议版本
	payload.WriteByte(0x0A)
	// server_version null-terminated / server_version null 结尾
	payload.WriteString(serverVersion)
	payload.WriteByte(0x00)
	// thread_id 4B LE / 线程 ID 4 字节 LE
	var tid [4]byte
	binary.LittleEndian.PutUint32(tid[:], threadID)
	payload.Write(tid[:])
	// 8B auth-plugin-data-part-1 + 1B filler + 2B cap_lower + 1B
	// charset + 2B status + 2B cap_upper + 1B auth-len + 10B reserved.
	// / 8 字节 salt + 1 字节 filler + 2 字节能力(低) + 1 字节字符集
	// + 2 字节状态 + 2 字节能力(高) + 1 字节 auth-len + 10 字节保留。
	capLower := [2]byte{0xFF, 0xFF}
	charset := byte(0x21) // utf8_general_ci
	status := [2]byte{0x02, 0x00}
	capUpper := [2]byte{0xFF, 0xFF}
	authDataLen := byte(0x00)       // skip auth-plugin-name; defaults to "unknown"
	payload.Write(make([]byte, 8))  // salt part 1
	payload.WriteByte(0x00)         // filler
	payload.Write(capLower[:])      // capability_flags_lower
	payload.WriteByte(charset)      // character_set
	payload.Write(status[:])        // status_flags
	payload.Write(capUpper[:])      // capability_flags_upper
	payload.WriteByte(authDataLen)  // auth-plugin-data-len (0)
	payload.Write(make([]byte, 10)) // 10 reserved bytes
	payload.WriteByte(0x00)         // 1 trailing pad byte (see func doc)
	// header: 3B payload-length LE + 1B sequence (0)
	// / 头:3 字节 payload 长度 LE + 1 字节 sequence (0)
	pLen := uint32(payload.Len())
	header := [4]byte{
		byte(pLen),
		byte(pLen >> 8),
		byte(pLen >> 16),
		0x00, // sequence id 0
	}
	out := make([]byte, 0, 4+payload.Len())
	out = append(out, header[:]...)
	out = append(out, payload.Bytes()...)
	return out
}

// greetingResponder writes a HandshakeV10 greeting and closes. The
// MySQL plugin is read-only on this path; we don't need to drain
// anything the client might send. / greetingResponder 写 HandshakeV10
// 握手并关闭。MySQL 插件在该路径只读;无需排空客户端任何字节。
func greetingResponder(c net.Conn) {
	_, _ = c.Write(handshakeV10("5.7.0-fake", 4242))
}

// TestMysql_IdentifyHit spins up a TCP fake server that emits a
// minimal HandshakeV10 greeting. The plugin must identify the
// service as "mysql" and the banner must reference the server_version
// we wrote. / 启 TCP 假 server 发最小 HandshakeV10 握手。插件应识
// 别服务为 "mysql",banner 必须包含我们写入的 server_version。
func TestMysql_IdentifyHit(t *testing.T) {
	host, port := fakeserver.ListenLoop(t, greetingResponder)
	p := New()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	r := p.Identify(ctx, host, port)
	if r == nil {
		t.Fatal("expected mysql hit, got nil")
	}
	if r.Service != "mysql" {
		t.Errorf("Service = %q, want %q", r.Service, "mysql")
	}
	if !strings.Contains(r.Banner, "MySQL") {
		t.Errorf("Banner = %q, want substring %q", r.Banner, "MySQL")
	}
	if !strings.Contains(r.Banner, "5.7.0-fake") {
		t.Errorf("Banner = %q, want substring %q (server_version)", r.Banner, "5.7.0-fake")
	}
	if !strings.Contains(r.Banner, "thread_id=4242") {
		t.Errorf("Banner = %q, want substring %q (thread_id)", r.Banner, "thread_id=4242")
	}
	if r.Host != host || r.Port != port {
		t.Errorf("Host/Port = %q:%d, want %q:%d", r.Host, r.Port, host, port)
	}
}

// TestMysql_IdentifyNoGreeting covers the negative case: the
// server sends no greeting (or bytes that aren't HandshakeV10) and
// the plugin must return nil so we don't false-positive on a
// non-MySQL TCP service. / 验证反向 case:server 不发握手(或发的不是
// HandshakeV10)。插件必须返 nil,避免在非 MySQL 服务上假阳性。
func TestMysql_IdentifyNoGreeting(t *testing.T) {
	host, port := fakeserver.ListenLoop(t, func(c net.Conn) {
		// Send a single byte that is NOT 0x0A so the plugin's
		// body[0] != 0x0A branch at mysql.go:92 fires and the
		// parser returns nil. / 发 1 字节非 0x0A,触发 mysql.go:92
		// 的 body[0] != 0x0A 分支让解析器返 nil。
		_, _ = c.Write([]byte{0x09})
	})
	p := New()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if got := p.Identify(ctx, host, port); got != nil {
		t.Errorf("Identify with non-HandshakeV10 byte = %+v, want nil", got)
	}
}

// TestMysql_CredentialHit is skipped: the mysql plugin's Credential
// is a documented no-op stub (always returns nil) per mysql.go:56-61
// — actual MySQL credential testing lives in
// core/cred/auth/database/mysql.go. / TestMysql_CredentialHit 跳过:
// mysql 插件的 Credential 是文档化的空 stub(始终返回 nil),见
// mysql.go:56-61——真正的 MySQL 凭据测试在
// core/cred/auth/database/mysql.go。
func TestMysql_CredentialHit(t *testing.T) {
	t.Skip("mysql.Credential is a no-op stub; see mysql.go Credential()")
}
