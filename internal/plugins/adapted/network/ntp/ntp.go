// ntp.go — NTP Identify plugin. Sends a 48-byte NTPv4 client
// request and verifies the response Mode bits. / NTP 识别插件。发
// 48 字节 NTPv4 client 请求并校验响应 Mode 位。
//
// Wire format (RFC 5905 §7.3):
//
//	bytes 0:    LI(2)|VN(3)|Mode(3)  → request: 0b00011011 = 0x1B
//	                                    response: 0b00100100 = 0x24
//	bytes 1-47: 47 bytes of stratum / poll / precision / timestamps
//
// We only check the first byte; if Mode == 4 (server), the host
// spoke NTP. We don't parse the timestamps (we're a scanner, not
// a time daemon). / 我们只检查首字节；Mode == 4 (server) 即视为
// NTP。不解析时间戳（我们是扫描器，不是时间服务）。
package ntp

import (
	"context"
	"net"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/plugins"
	"github.com/LCUstinian/FG-QiMen/internal/types"
)

// Plugin identifies NTP servers. / Plugin 识别 NTP 服务。
type Plugin struct{}

// New returns a new ntp plugin. / New 返回一个新的 ntp 插件。
func New() *Plugin { return &Plugin{} }

func init() { plugins.Register(New()) }

// Name implements plugins.Plugin. / Name 实现 plugins.Plugin。
func (p *Plugin) Name() string { return "ntp" }

// Ports returns default NTP port. / Ports 返回默认 NTP 端口。
func (p *Plugin) Ports() []int { return []int{123} }

// Modes returns Identify only. / Modes 仅返回 Identify。
func (p *Plugin) Modes() plugins.Mode { return plugins.ModeIdentify }

// Credential is a no-op stub; NTP doesn't have user/pass auth in
// the v0.2 era. / Credential 空 stub；NTP 在 v0.2 时代无 user/pass
// 认证。
func (p *Plugin) Credential(context.Context, string, int, []types.Cred) *types.Result {
	return nil
}

// Identify sends an NTPv4 client request and parses the response.
// / Identify 发 NTPv4 client 请求并解析响应。
func (p *Plugin) Identify(ctx context.Context, host string, port int) *types.Result {
	return plugins.RawUDPIdentify(ctx, host, port, func(conn net.Conn) *types.Result {
		// LI=0, VN=4, Mode=3 (client) = 0b00011011 = 0x1B.
		// Remaining 47 bytes are zero. / LI=0, VN=4, Mode=3
		// (client) = 0b00011011 = 0x1B。剩 47 字节为零。
		req := make([]byte, 48)
		req[0] = 0x1B
		if _, err := conn.Write(req); err != nil {
			return nil
		}
		resp := make([]byte, 48)
		n, err := conn.Read(resp)
		if err != nil || n < 1 {
			return nil
		}
		// Mode is the low 3 bits of the first byte. / Mode 是
		// 首字节低 3 位。
		mode := resp[0] & 0x07
		if mode != 4 {
			// 4 = server. Anything else (3=client echo, 0=reserved)
			// isn't a real NTP response. / 4 = server。其他
			// （3=client 回声、0=保留）不是真 NTP 响应。
			return nil
		}
		// Stratum is bits 0-7 of byte 1. 0 = unspecified, 16 =
		// unsynchronized. Anything 1-15 is a sync'd server. / Stra
		// tum 是 byte 1 的 0-7 位。0 = 未指定，16 = 未同步。
		// 1-15 都是同步 server。
		stratum := ""
		if n >= 2 {
			s := resp[1]
			if s == 0 {
				stratum = " (stratum unspecified)"
			} else if s < 16 {
				stratum = " (stratum " + itoa(int(s)) + ")"
			} else {
				stratum = " (unsynchronized)"
			}
		}
		return &types.Result{
			Host:    host,
			Port:    port,
			Service: "ntp",
			Banner:  "NTP" + stratum,
			Time:    time.Now(),
		}
	})
}

// itoa is a small int→string helper to avoid fmt import in the
// hot path. / itoa 是避免热路径引入 fmt 的小工具。
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [4]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
