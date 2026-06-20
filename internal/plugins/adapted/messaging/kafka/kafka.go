// kafka.go — Apache Kafka Identify plugin. Sends an
// ApiVersions request (key=18) and parses the response.
// / kafka.go — Apache Kafka 识别插件。发 ApiVersions 请求
// （key=18）并解析响应。
//
// Wire format (Kafka protocol 0.10+):
//
//	request:  [4B length][2B api_key][2B api_ver][4B corr_id][1B client_id]
//	response: [4B length][4B corr_id][2B error][4B array_len][per-array]
//	          where each entry is [2B api_key][2B min_ver][2B max_ver]
//
// We only care about the response being well-formed (i.e. a Kafka
// broker is listening). / 我们只关心响应格式良构（即 Kafka
// broker 在听）。
package kafka

import (
	"context"
	"encoding/binary"
	"net"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/plugins"
	"github.com/LCUstinian/FG-QiMen/internal/types"
)

// Plugin identifies Kafka brokers. / Plugin 识别 Kafka broker。
type Plugin struct{}

// New returns a new kafka plugin. / New 返回一个新的 kafka 插件。
func New() *Plugin { return &Plugin{} }

func init() { plugins.Register(New()) }

// Name implements plugins.Plugin. / Name 实现 plugins.Plugin。
func (p *Plugin) Name() string { return "kafka" }

// Ports returns default Kafka port. / Ports 返回默认 Kafka 端口。
func (p *Plugin) Ports() []int { return []int{9092} }

// Modes returns Identify only. / Modes 仅返回 Identify。
func (p *Plugin) Modes() plugins.Mode { return plugins.ModeIdentify }

// Credential is a no-op stub. / Credential 空 stub。
func (p *Plugin) Credential(ctx context.Context, host string, port int, creds []types.Cred) *types.Result {
	return nil
}

// Identify sends an ApiVersions request. / Identify 发 ApiVersions
// 请求。
func (p *Plugin) Identify(ctx context.Context, host string, port int) *types.Result {
	return plugins.RawTCPIdentify(ctx, host, port, func(conn net.Conn) *types.Result {
		// ApiVersions request: api_key=18, api_ver=0, corr_id=1,
		// client_id="fg-qimen". / ApiVersions 请求：api_key=18,
		// api_ver=0, corr_id=1, client_id="fg-qimen"。
		clientID := "fg-qimen"
		reqBody := make([]byte, 0, 2+2+4+2+len(clientID))
		reqBody = binary.BigEndian.AppendUint16(reqBody, 18) // api_key
		reqBody = binary.BigEndian.AppendUint16(reqBody, 0)  // api_ver
		reqBody = binary.BigEndian.AppendUint32(reqBody, 1)  // corr_id
		reqBody = binary.BigEndian.AppendUint16(reqBody, uint16(len(clientID)))
		reqBody = append(reqBody, clientID...)
		// Prepend 4-byte length. / 前置 4 字节长度。
		full := make([]byte, 4+len(reqBody))
		binary.BigEndian.PutUint32(full, uint32(len(reqBody)))
		copy(full[4:], reqBody)
		if _, err := conn.Write(full); err != nil {
			return nil
		}
		// Read 4-byte length + 4-byte corr_id + at least 2-byte
		// error_code. / 读 4 字节长度 + 4 字节 corr_id + 至少
		// 2 字节 error_code。
		hdr := make([]byte, 10)
		if _, err := conn.Read(hdr); err != nil {
			return nil
		}
		respLen := binary.BigEndian.Uint32(hdr[:4])
		if respLen < 6 || respLen > 4<<20 {
			return nil // 4 MiB cap
		}
		corrID := binary.BigEndian.Uint32(hdr[4:8])
		_ = corrID
		// error_code is hdr[8:10] (LE int16). / error_code 是
		// hdr[8:10]（LE int16）。
		// 0 = NO_ERROR. Anything else is also a Kafka response.
		// / 0 = NO_ERROR。其他也是 Kafka 响应。
		// We don't validate the body — just that the framing
		// is correct. / 不验证 body——只验证 framing 正确。
		return &types.Result{
			Host:    host,
			Port:    port,
			Service: "kafka",
			Banner:  "Kafka (ApiVersions, resp_len=" + itoa(int(respLen)) + ")",
			Time:    time.Now(),
		}
	})
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [10]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
