// mqtt.go — MQTT 3.1.1 / 5.0 Identify plugin. Connects, sends a
// minimal CONNECT, parses CONNACK.
//
// mqtt.go — MQTT 3.1.1 / 5.0 识别插件。连接后发最小 CONNECT，
// 解析 CONNACK。
//
// Protocol (MQTT 3.1.1, OASIS Standard):
//
//	CONNECT (client → broker):
//	  fixed header:  0x10 (CONNECT) | remaining length
//	  variable:      "MQTT" (4B) | 0x04 (protocol level) | 0x02 (clean session)
//	                 | 0x00 0x3C (keep-alive 60s)
//	  payload:       0x00 0x04 'f' 'g' '-q' 'm' (client ID "fg-qm", 4B)
//
//	CONNACK (broker → client):
//	  fixed header:  0x20 (CONNACK) | remaining length (always 2)
//	  variable:      0x00 0x00 (session present | return code 0=ACCEPTED)
//
// We don't care about the connection persisting — we send CONNECT,
// read up to 4 bytes (the fixed header + 2-byte variable header),
// and close. If the broker replies with CONNACK + return code 0-5,
// it's an MQTT broker. Anything else (or no reply) returns nil.
//
// 我们不在乎连接持续——发 CONNECT，读最多 4 字节（fixed header +
// 2 字节 variable header），然后关。如果 broker 回 CONNACK + 返
// 回码 0-5，就是 MQTT broker。其它（或没回）返 nil。
//
// HARD rule: we do NOT subscribe to any topic, do NOT publish
// anything, do NOT do any post-auth action. The CONNACK is enough
// to fingerprint the broker.
// 硬性原则：不订阅任何 topic、不发任何消息、不做任何后认证动作。
// CONNACK 已足够指纹 broker。
package mqtt

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/plugins"
	"github.com/LCUstinian/FG-QiMen/internal/types"
)

// Plugin identifies MQTT brokers. / Plugin 识别 MQTT broker。
type Plugin struct{}

// New returns a new mqtt plugin. / New 返回一个新的 mqtt 插件。
func New() *Plugin { return &Plugin{} }

func init() { plugins.Register(New()) }

// Name implements plugins.Plugin. / Name 实现 plugins.Plugin。
func (p *Plugin) Name() string { return "mqtt" }

// Ports returns default MQTT ports. / Ports 返回默认 MQTT 端口。
//
//	1883 — plain MQTT (per OASIS MQTT 3.1.1 / 5.0 spec)
//	8883 — MQTT over TLS (MQTT TLS, per spec)
func (p *Plugin) Ports() []int { return []int{1883, 8883} }

// Modes returns Identify only (v0.4). Credential spraying on MQTT
// is rarely useful (most brokers use mTLS or device certs), and
// per the HARD rule we don't do post-auth actions. / Modes 仅返
// Identify（v0.4）。MQTT 上凭据喷洒用处不大（多数 broker 用 mTLS
// 或设备证书），按 HARD 规则不做后认证动作。
func (p *Plugin) Modes() plugins.Mode { return plugins.ModeIdentify }

// Credential is a no-op stub. / Credential 空 stub。
func (p *Plugin) Credential(ctx context.Context, host string, port int, creds []types.Cred) *types.Result {
	return nil
}

// Identify sends a minimal CONNECT and parses the CONNACK reply.
// / Identify 发最小 CONNECT 并解析 CONNACK 回包。
func (p *Plugin) Identify(ctx context.Context, host string, port int) *types.Result {
	return plugins.RawTCPIdentify(ctx, host, port, identifyMQTT, plugins.WithIdentifyTimeout(3*time.Second))
}

// identifyMQTT is the callback handed to RawTCPIdentify. It owns
// the wire-format encoding. / identifyMQTT 交给 RawTCPIdentify 的
// 回调。它负责线协议编码。
func identifyMQTT(conn net.Conn) *types.Result {
	// Client ID: "fg-qm" (4 bytes). Short and fixed so the packet
	// length is deterministic for the response parser below.
	// / 客户端 ID: "fg-qm"（4 字节）。短而固定，使包长度对下面的
	// 响应解析器是确定的。
	clientID := "fg-qm"

	// Variable header: "MQTT" (4B) | 0x04 (protocol level 4 = 3.1.1)
	// | 0x02 (Connect Flags: Clean Session) | 0x00 0x3C (Keep
	// Alive 60s). / 可变头: "MQTT"（4B）| 0x04（协议级别 4 = 3.1.1）
	// | 0x02（Connect Flags: Clean Session）| 0x00 0x3C（Keep
	// Alive 60s）。
	var varHeader [10]byte
	copy(varHeader[0:4], "MQTT")
	varHeader[4] = 0x04 // protocol level
	varHeader[5] = 0x02 // connect flags (clean session)
	binary.BigEndian.PutUint16(varHeader[6:8], 60) // keep alive

	// Payload: 2-byte length + UTF-8 client ID. / 负载: 2 字节
	// 长度 + UTF-8 客户端 ID。
	payload := make([]byte, 2+len(clientID))
	binary.BigEndian.PutUint16(payload[0:2], uint16(len(clientID)))
	copy(payload[2:], clientID)

	// Remaining length = len(varHeader) + len(payload). / Remaining
	// length = len(varHeader) + len(payload)。
	remaining := len(varHeader) + len(payload)
	// 1-byte fixed header (CONNECT type 0x10) + 1-byte length +
	// variable + payload. / 1 字节 fixed header（CONNECT type
	// 0x10）+ 1 字节长度 + variable + payload。
	// For lengths ≤ 127, the length field is 1 byte; larger
	// lengths use the 2/3/4 byte variable-length encoding per
	// MQTT spec §2.2.3. / 长度 ≤ 127 时长度字段 1 字节；更大的
	// 长度按 MQTT 规范 §2.2.3 用 2/3/4 字节可变长编码。
	if remaining > 127 {
		// Not expected for our minimal CONNECT (4B + 10B + 6B =
		// 20 bytes remaining). Bail rather than implement the
		// multi-byte length encoding for a path we never hit.
		// / 我们的最小 CONNECT 不会到这里（4B + 10B + 6B = 20 字节
		// remaining）。为不可能走到的路径实现多字节长度编码不值
		// 得。
		return nil
	}
	frame := make([]byte, 2+remaining)
	frame[0] = 0x10             // CONNECT (type 1, flags 0)
	frame[1] = byte(remaining)  // remaining length
	copy(frame[2:], varHeader[:])
	copy(frame[2+len(varHeader):], payload)

	if _, err := conn.Write(frame); err != nil {
		return nil
	}

	// Read 4 bytes: fixed header (2B) + variable header (2B). For
	// CONNACK the fixed header is always 0x20 0x02 (type 2, length
	// 2), and the variable header is the 2-byte return code area.
	// / 读 4 字节: fixed header（2B）+ variable header（2B）。
	// CONNACK 的 fixed header 总是 0x20 0x02（type 2, length 2），
	// variable header 是 2 字节返回码区。
	hdr := make([]byte, 4)
	if _, err := readFullMQTT(conn, hdr); err != nil {
		return nil
	}
	if hdr[0] != 0x20 {
		// Not a CONNACK. / 不是 CONNACK。
		return nil
	}
	if hdr[1] != 0x02 {
		// Length should be exactly 2 for CONNACK. / CONNACK 的长
		// 度恰好为 2。
		return nil
	}
	// session_present (bit 0 of hdr[2]) + return code (hdr[3]).
	// Return codes 0-5 are all valid MQTT responses (ACCEPTED
	// through NOT_AUTHORIZED). 6+ are reserved (protocol error per
	// spec §3.2.2.3). / session_present（hdr[2] bit 0）+ 返回码
	// （hdr[3]）。返回码 0-5 都是有效 MQTT 响应（ACCEPTED 到
	// NOT_AUTHORIZED）。6+ 是保留值（按规范 §3.2.2.3 是协议错误）。
	connAckCode := hdr[3]
	if connAckCode > 5 {
		return nil
	}
	return &types.Result{
		Host:    "", // filled by plugins.RawTCPIdentify
		Port:    0,  // filled by plugins.RawTCPIdentify
		Service: "mqtt",
		Banner:  fmt.Sprintf("MQTT (CONNACK code=%d)", connAckCode),
		Time:    time.Now(),
	}
}

// readFullMQTT reads exactly len(buf) bytes from conn or returns an
// error. Mirrors readAMQPFrame's readFullRMQ2 helper. / readFullMQTT
// 从 conn 读恰好 len(buf) 字节，失败则返错。对应 readAMQPFrame 的
// readFullRMQ2 helper。
func readFullMQTT(conn net.Conn, buf []byte) (int, error) {
	total := 0
	for total < len(buf) {
		n, err := conn.Read(buf[total:])
		total += n
		if err != nil {
			return total, err
		}
	}
	return total, nil
}
