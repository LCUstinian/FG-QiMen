// mongodb_test.go — A.6 coverage baseline via fakeserver.ListenLoop.
//
// mongodb_test.go — 通过 fakeserver.ListenLoop 的 A.6 覆盖率基线。
//
// Wire format being faked (mongodb.go:59-99):
//
//	client sends an OP_MSG (opCode 2013) wrapping a BSON
//	{hello: 1, $db: "admin"} body.
//	server replies with an OP_MSG (opCode 2013) or
//	OP_QUERY (opCode 2014) whose body is a BSON document. The
//	plugin extracts "version" via a custom byte-layout scan
//	(mongodb.go:159-181); when the scan misses it falls back to
//	the generic "MongoDB" banner.
//
// Test strategy: build the OP_MSG reply locally with a hand-rolled
// BSON encoder. We do NOT load the bson driver — the plugin itself
// only emits BSON bytes, and a fully valid reply is overkill for the
// happy-path branch we want to cover. The fake server just drains
// the client's request and writes the canned reply; the plugin's
// RawTCPIdentify helper does the rest.
//
// 测试策略：本地用手动 BSON 编码器构造 OP_MSG 响应。我们不加载
// bson 驱动——插件自身只发 BSON 字节，构造完整合法响应只是杀鸡用
// 牛刀。假 server 仅排空客户端请求后写固定响应；插件的
// RawTCPIdentify helper 接管后续 I/O。
package mongodb

import (
	"context"
	"encoding/binary"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/fakeserver"
)

// bsonKVInt32 emits a single BSON int32 element: type byte 0x10 +
// NUL-terminated key + 4-byte LE value. / bsonKVInt32 发出单个 BSON
// int32 元素：类型字节 0x10 + NUL 终止的 key + 4 字节 LE 值。
func bsonKVInt32(key string, v int32) []byte {
	out := []byte{0x10}
	out = append(out, key...)
	out = append(out, 0x00)
	tmp := make([]byte, 4)
	binary.LittleEndian.PutUint32(tmp, uint32(v))
	return append(out, tmp...)
}

// wrapBSON wraps elements in a length-prefixed BSON document (with
// the required 0x00 terminator). Named wrapBSON to avoid colliding
// with the production bsonDoc in mongodb.go (same package).
// / wrapBSON 把元素包成长度前缀的 BSON 文档（含必需的 0x00 终止
// 符）。命名 wrapBSON 以避免与同包 mongodb.go 中的生产 bsonDoc
// 冲突。
func wrapBSON(elements ...[]byte) []byte {
	body := make([]byte, 0, 64)
	for _, e := range elements {
		body = append(body, e...)
	}
	body = append(body, 0x00) // doc terminator
	out := make([]byte, 4+len(body))
	binary.LittleEndian.PutUint32(out[0:4], uint32(len(out)))
	copy(out[4:], body)
	return out
}

// opmsgReply wraps a BSON document in an OP_MSG (opCode 2013) reply:
// msgHeader (16) + flagBits (4) + section kind 0 (1) + BSON doc.
// / opmsgReply 把 BSON 文档包成 OP_MSG（opCode 2013）响应：msgHeader
// (16) + flagBits (4) + section kind 0 (1) + BSON doc。
func opmsgReply(doc []byte) []byte {
	msg := make([]byte, 16+4+1+len(doc))
	binary.LittleEndian.PutUint32(msg[0:4], uint32(len(msg))) // messageLength
	binary.LittleEndian.PutUint32(msg[4:8], 1)                // requestID
	binary.LittleEndian.PutUint32(msg[8:12], 0)               // responseTo
	binary.LittleEndian.PutUint32(msg[12:16], 2013)           // opCode = OP_MSG
	// flagBits already zero
	msg[20] = 0 // section kind 0 = body
	copy(msg[21:], doc)
	return msg
}

// okOnlyResponder drains the client's OP_MSG hello and replies with
// an OP_MSG containing only {ok: 1}. The plugin's
// extractBSONString heuristic misses "version" in this layout (it
// expects a non-standard "type byte after key" pattern — see
// mongodb.go:171), so the plugin falls back to the generic "MongoDB"
// banner. / okOnlyResponder 排空客户端 OP_MSG hello 并回复仅含
// {ok: 1} 的 OP_MSG。插件的 extractBSONString 启发式在此布局下
// 找不到 "version"（它期望非标准的"key 后类型字节"模式，见
// mongodb.go:171），所以插件回退到通用 "MongoDB" 横幅。
func okOnlyResponder(c net.Conn) {
	fakeserver.ReadAll(c)
	doc := wrapBSON(bsonKVInt32("ok", 1))
	_, _ = c.Write(opmsgReply(doc))
}

// versionResponder drains the client's OP_MSG hello and replies
// with an OP_MSG body containing the exact byte sequence the
// plugin's extractor is designed to match (mongodb.go:166-178):
//
//	"version\0" + 0x02 + <LE uint32 length> + <content>
//
// This is NOT a valid BSON element layout (real BSON puts the
// type byte BEFORE the key, not after). The plugin's extractor
// uses a custom heuristic that reads the byte AFTER the key as a
// "type tag" and the next 4 bytes as a length. To exercise the
// extraction branch without a real MongoDB server we emit the
// raw byte sequence the heuristic expects. / versionResponder
// 排空客户端 OP_MSG hello 并回复一个 OP_MSG body，其中包含插件
// 提取器（mongodb.go:166-178）所期望的精确字节序列："version\0"
// + 0x02 + LE uint32 长度 + 内容。这不是合法的 BSON 元素布局
// （真实 BSON 把类型字节放在 key 之前而非之后）。插件提取器使
// 用自定义启发式，把 key 后的字节读作"类型标签"，把接下来 4
// 字节读作长度。为在不依赖真实 MongoDB 的情况下覆盖提取分
// 支，我们直接发出该启发式所期望的原始字节序列。
func versionResponder(c net.Conn) {
	fakeserver.ReadAll(c)
	// Build a minimal body slice: [section kind 0] + arbitrary
	// padding + the magic "version\0\x02\x02\x00\x00\x00OK"
	// sequence + trailing byte so the length check at
	// mongodb.go:175 does not reject us. / 构建最小 body slice：
	// [section kind 0] + 任意 padding + 魔数序列
	// "version\0\x02\x02\x00\x00\x00OK" + 尾部字节，使
	// mongodb.go:175 的长度检查不拒绝。
	body := []byte{
		0x00,                                    // section kind 0 = body
		0x00,                                    // padding byte (any value)
		'v', 'e', 'r', 's', 'i', 'o', 'n', 0x00, // "version\0" at body[2:10]
		0x02,                   // body[10] = extractor's "string type tag"
		0x02, 0x00, 0x00, 0x00, // body[11:15] = LE uint32 length = 2
		'O', 'K', // body[15:17] = extracted content
		0x00, // body[17] = trailing terminator so length check passes
	}
	// We still need a msgHeader + flagBits + section around the body.
	// / 仍需要 msgHeader + flagBits + section 包裹 body。
	msg := make([]byte, 16+4+len(body))
	binary.LittleEndian.PutUint32(msg[0:4], uint32(len(msg)))
	binary.LittleEndian.PutUint32(msg[12:16], 2013) // opCode OP_MSG
	copy(msg[20:], body)
	_, _ = c.Write(msg)
}

// TestMongodb_IdentifyHit is the basic happy-path test: a fake
// server replies with an OP_MSG {ok: 1} body. The plugin must
// identify the service as "mongodb" and return a non-nil result.
// / TestMongodb_IdentifyHit 是基本 happy-path 测试：假 server 回
// 复 OP_MSG {ok: 1} body。插件必须识别服务为 "mongodb" 并返回非
// nil 结果。
func TestMongodb_IdentifyHit(t *testing.T) {
	host, port := fakeserver.ListenLoop(t, okOnlyResponder)
	p := New()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	r := p.Identify(ctx, host, port)
	if r == nil {
		t.Fatal("Identify returned nil for valid OP_MSG {ok:1} reply")
	}
	if r.Service != "mongodb" {
		t.Errorf("Service = %q, want %q", r.Service, "mongodb")
	}
	if r.Host != host || r.Port != port {
		t.Errorf("Host/Port = %q:%d, want %q:%d", r.Host, r.Port, host, port)
	}
	// Banner should be the generic "MongoDB" because the extractor
	// cannot match "version" in the standard BSON layout (see
	// mongodb.go:171 — it expects 0x02 immediately after the key).
	// / Banner 应为通用 "MongoDB"，因为提取器在标准 BSON 布局下无
	// 法匹配 "version"（见 mongodb.go:171——它期望 key 后面紧跟
	// 0x02）。
	if r.Banner != "MongoDB" {
		t.Errorf("Banner = %q, want %q", r.Banner, "MongoDB")
	}
}

// TestMongodb_IdentifyWithVersion exercises the version-extraction
// branch in the plugin (mongodb.go:84-93). The fake response uses a
// 2-byte "version" string so the byte after the key in the BSON
// body is 0x02 (the string-length LSB), satisfying the extractor's
// heuristic. The plugin must then return Banner "MongoDB OK".
// / TestMongodb_IdentifyWithVersion 测试插件的 version 提取分支
// （mongodb.go:84-93）。假响应使用 2 字节 "version" 字符串，使
// BSON body 中 key 后的字节为 0x02（字符串长度 LSB），满足提取
// 器的启发式。插件必须返回 Banner "MongoDB OK"。
func TestMongodb_IdentifyWithVersion(t *testing.T) {
	host, port := fakeserver.ListenLoop(t, versionResponder)
	p := New()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	r := p.Identify(ctx, host, port)
	if r == nil {
		t.Fatal("Identify returned nil for valid OP_MSG reply with version")
	}
	if r.Service != "mongodb" {
		t.Errorf("Service = %q, want %q", r.Service, "mongodb")
	}
	if !strings.HasPrefix(r.Banner, "MongoDB ") {
		t.Errorf("Banner = %q, want prefix %q", r.Banner, "MongoDB ")
	}
	if r.Banner != "MongoDB OK" {
		t.Errorf("Banner = %q, want %q (extractor should pull 'OK')", r.Banner, "MongoDB OK")
	}
}

// TestMongodb_CredentialHit is skipped: the mongodb plugin's
// Credential is a no-op stub (always returns nil) per
// mongodb.go:38-41 — actual credential testing lives in
// core/cred/protocols/. / TestMongodb_CredentialHit 跳过：
// mongodb 插件的 Credential 是空 stub（始终返回 nil），见
// mongodb.go:38-41——真正的凭证测试在 core/cred/protocols/。
func TestMongodb_CredentialHit(t *testing.T) {
	t.Skip("mongodb.Credential is a no-op stub; see mongodb.go Credential()")
}
