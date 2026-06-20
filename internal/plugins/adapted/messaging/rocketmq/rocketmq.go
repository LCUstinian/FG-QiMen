// rocketmq.go — Apache RocketMQ Identify plugin. Sends a
// minimal GetRouteInfoByTopic request and parses the response
// header. / rocketmq.go — Apache RocketMQ 识别插件。发最小
// GetRouteInfoByTopic 请求并解析响应头。
//
// Wire format (RocketMQ RemotingCommand v1):
//
//	request header (4 ints, 16 bytes):
//	  [4B code][4B language][4B version][4B opaque]
//	request body: variable
//	response header: same as request
//	response body: variable
//
// / 请求头 (4 个 int, 16 字节)：[4B code][4B language][4B version]
// [4B opaque]。
package rocketmq

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/plugins"
	"github.com/LCUstinian/FG-QiMen/internal/types"
)

// Plugin identifies RocketMQ brokers. / Plugin 识别 RocketMQ broker。
type Plugin struct{}

// New returns a new rocketmq plugin. / New 返回一个新的 rocketmq 插件。
func New() *Plugin { return &Plugin{} }

func init() { plugins.Register(New()) }

// Name implements plugins.Plugin. / Name 实现 plugins.Plugin。
func (p *Plugin) Name() string { return "rocketmq" }

// Ports returns default RocketMQ name-server port. / Ports 返回默认
// RocketMQ name-server 端口。
func (p *Plugin) Ports() []int { return []int{9876} }

// Modes returns Identify only. / Modes 仅返回 Identify。
func (p *Plugin) Modes() plugins.Mode { return plugins.ModeIdentify }

// Credential is a no-op stub. / Credential 空 stub。
func (p *Plugin) Credential(ctx context.Context, host string, port int, creds []types.Cred) *types.Result {
	return nil
}

// Identify sends a minimal RemotingCommand header. RocketMQ
// name-server replies with a response that starts with a similar
// 16-byte header. / Identify 发最小 RemotingCommand 头。
// RocketMQ name-server 返以类似 16 字节头开头的响应。
func (p *Plugin) Identify(ctx context.Context, host string, port int) *types.Result {
	return plugins.RawTCPIdentify(ctx, host, port, func(conn net.Conn) *types.Result {
		// Request: code=GetRouteInfoByTopic(105), language=Java(0),
		// version=0, opaque=0. / 请求：code=GetRouteInfoByTopic
		// (105), language=Java(0), version=0, opaque=0。
		req := make([]byte, 16)
		binary.BigEndian.PutUint32(req[0:4], 105) // GET_ROUTEINFO_BY_TOPIC
		binary.BigEndian.PutUint32(req[4:8], 0)   // JAVA
		binary.BigEndian.PutUint32(req[8:12], 0)  // version
		binary.BigEndian.PutUint32(req[12:16], 1) // opaque
		if _, err := conn.Write(req); err != nil {
			return nil
		}
		// Read response header. / 读响应头。
		hdr := make([]byte, 16)
		n, err := conn.Read(hdr)
		if err != nil || n < 16 {
			return nil
		}
		// The response code field has the high bit set
		// (0x80000000) for "response" vs "request". / 响应 code
		// 字段高位被置（0x80000000）以区分响应 vs 请求。
		code := binary.BigEndian.Uint32(hdr[0:4])
		// remotingCommand bit 31 is the response flag. /
		// remotingCommand bit 31 是响应标志。
		if code&0x80000000 == 0 {
			return nil
		}
		// Strip the response flag to read the actual code. /
		// 剥掉响应标志读真 code。
		actual := code &^ 0x80000000
		return &types.Result{
			Host:    host,
			Port:    port,
			Service: "rocketmq",
			Banner:  fmt.Sprintf("RocketMQ (resp_code=%d)", actual),
			Time:    time.Now(),
		}
	})
}
