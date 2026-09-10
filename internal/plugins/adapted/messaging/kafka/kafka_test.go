// kafka_test.go — A.6 coverage baseline via fakeserver.ListenLoop.
//
// kafka_test.go — 通过 fakeserver.ListenLoop 的 A.6 覆盖率基线。
//
// Wire format being faked (kafka.go:6-10):
//
//	request:  [4B length][2B api_key][2B api_ver][4B corr_id][2B client_id_len][client_id]
//	response: [4B length][4B corr_id][2B error_code][4B array_len][per-array: 2B api_key + 2B min_ver + 2B max_ver]
//
// Test strategy: drain the client's ApiVersions request and reply
// with a hand-crafted ApiVersionsResponse listing a couple of
// supported API keys. The plugin only validates framing (respLen
// within [6, 4 MiB]) per kafka.go:78-80 — it does NOT parse the
// array body. So we just need a well-shaped response of any
// respLen >= 6.
//
// 测试策略：排空客户端 ApiVersions 请求后，回复一个手写的
// ApiVersionsResponse，列出几个支持的 API key。插件只校验
// framing（respLen 在 [6, 4 MiB] 内），见 kafka.go:78-80——不
// 解析 array body。所以我们只需要 respLen >= 6 的良构响应。
package kafka

import (
	"context"
	"encoding/binary"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/fakeserver"
)

// apiVersionsEntry emits one api_key/min_ver/max_ver triple in the
// response array layout (6 bytes, big-endian). / apiVersionsEntry
// 按响应数组布局发出一个 api_key/min_ver/max_ver 三元组（6 字
// 节，大端）。
func apiVersionsEntry(apiKey, minVer, maxVer uint16) []byte {
	out := make([]byte, 6)
	binary.BigEndian.PutUint16(out[0:2], apiKey)
	binary.BigEndian.PutUint16(out[2:4], minVer)
	binary.BigEndian.PutUint16(out[4:6], maxVer)
	return out
}

// apiVersionsResponse builds a well-formed Kafka ApiVersionsResponse:
// corr_id=1, error_code=0 (NO_ERROR), array of 2 entries (Produce +
// ApiVersions). Returns the response body WITHOUT the 4-byte length
// prefix — the caller prepends it. / apiVersionsResponse 构造合法
// 的 Kafka ApiVersionsResponse：corr_id=1，error_code=0
// （NO_ERROR），含 2 个 entry（Produce + ApiVersions）。返回不含
// 4 字节长度前缀的响应 body，由调用方前置。
func apiVersionsResponse() []byte {
	entries := [][]byte{
		apiVersionsEntry(0, 0, 9),  // Produce
		apiVersionsEntry(18, 0, 3), // ApiVersions
	}
	var body []byte
	// corr_id (4) + error_code (2) + array_len (4) + entries
	body = binary.BigEndian.AppendUint32(body, 1) // corr_id echoes client's 1
	body = binary.BigEndian.AppendUint16(body, 0) // error_code = NO_ERROR
	body = binary.BigEndian.AppendUint32(body, uint32(len(entries)))
	for _, e := range entries {
		body = append(body, e...)
	}
	// Prepend 4-byte length. / 前置 4 字节长度。
	full := make([]byte, 4+len(body))
	binary.BigEndian.PutUint32(full, uint32(len(body)))
	copy(full[4:], body)
	return full
}

// apiVersionsResponder drains the client's ApiVersions request and
// replies with a canned ApiVersionsResponse listing 2 supported APIs.
// / apiVersionsResponder 排空客户端 ApiVersions 请求后，回复
// 列出 2 个支持 API 的固定 ApiVersionsResponse。
func apiVersionsResponder(c net.Conn) {
	fakeserver.ReadAll(c)
	_, _ = c.Write(apiVersionsResponse())
}

// TestKafka_IdentifyHit is the basic happy-path test: a fake Kafka
// broker replies with a well-formed ApiVersionsResponse. The
// plugin must identify the service as "kafka" and return a
// non-nil result. / TestKafka_IdentifyHit 是基本 happy-path
// 测试：假 Kafka broker 回复合法的 ApiVersionsResponse。插件
// 必须识别服务为 "kafka" 并返回非 nil 结果。
func TestKafka_IdentifyHit(t *testing.T) {
	host, port := fakeserver.ListenLoop(t, apiVersionsResponder)
	p := New()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	r := p.Identify(ctx, host, port)
	if r == nil {
		t.Fatal("Identify returned nil for valid ApiVersionsResponse")
	}
	if r.Service != "kafka" {
		t.Errorf("Service = %q, want %q", r.Service, "kafka")
	}
	if r.Host != host || r.Port != port {
		t.Errorf("Host/Port = %q:%d, want %q:%d", r.Host, r.Port, host, port)
	}
	// Banner format from kafka.go:93 — should mention Kafka and
	// the resp_len we sent (10 + 2*6 = 22 bytes).
	// / Banner 格式见 kafka.go:93——应提到 Kafka 和我们发送的
	// resp_len（10 + 2*6 = 22 字节）。
	if !strings.HasPrefix(r.Banner, "Kafka (ApiVersions, resp_len=") {
		t.Errorf("Banner = %q, want prefix %q", r.Banner, "Kafka (ApiVersions, resp_len=")
	}
	if !strings.HasSuffix(r.Banner, ")") {
		t.Errorf("Banner = %q, want trailing %q", r.Banner, ")")
	}
}

// TestKafka_IdentifyMalformedLen covers the framing rejection branch
// at kafka.go:78-80: a server that sends a too-short response (respLen
// < 6) must cause Identify to return nil. / TestKafka_IdentifyMalformedLen
// 覆盖 kafka.go:78-80 的 framing 拒绝分支：发太短响应（respLen <
// 6）的 server 必须让 Identify 返回 nil。
func TestKafka_IdentifyMalformedLen(t *testing.T) {
	host, port := fakeserver.ListenLoop(t, func(c net.Conn) {
		fakeserver.ReadAll(c)
		// respLen = 4 (< 6 threshold in kafka.go:78) — plugin must reject.
		// / respLen = 4（小于 kafka.go:78 的 6 阈值）——插件必须
		// 拒绝。
		hdr := make([]byte, 4)
		binary.BigEndian.PutUint32(hdr, 4)
		_, _ = c.Write(hdr)
	})
	p := New()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	r := p.Identify(ctx, host, port)
	if r != nil {
		t.Errorf("Identify returned %+v for malformed respLen; want nil", r)
	}
}

// TestKafka_CredentialHit is skipped: the kafka plugin's
// Credential is a no-op stub (always returns nil) per
// kafka.go:44-47 — actual credential testing lives in
// core/cred/protocols/. / TestKafka_CredentialHit 跳过：
// kafka 插件的 Credential 是空 stub（始终返回 nil），见
// kafka.go:44-47——真正的凭证测试在 core/cred/protocols/。
func TestKafka_CredentialHit(t *testing.T) {
	t.Skip("kafka.Credential is a no-op stub; see kafka.go Credential()")
}
