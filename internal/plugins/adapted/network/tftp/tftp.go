// tftp.go — TFTP service-fingerprint plugin (service Identify only).
// / TFTP 服务指纹插件（仅服务识别）。
//
// Wire format (RFC 1350):
//
//	RRQ:  \x00\x01 | filename | \x00 | "octet" | \x00
//	Resp: \x00\x03 (DATA) | block# (2B) | data
//	      \x00\x05 (ERROR) | errcode (2B) | msg | \x00
//
// Some servers reply with ERROR code 1 (File not found) to unknown
// filenames; both DATA and ERROR are valid TFTP banners. / 部分
// server 对未知文件回 ERROR code 1；DATA 和 ERROR 都是合法 TFTP
// 响应。
package tftp

import (
	"context"
	"net"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/plugins"
	"github.com/LCUstinian/FG-QiMen/internal/types"
)

// Plugin identifies TFTP servers. / Plugin 识别 TFTP 服务。
type Plugin struct{}

// New returns a new tftp plugin. / New 返回一个新的 tftp 插件。
func New() *Plugin { return &Plugin{} }

func init() { plugins.Register(New()) }

// Name implements plugins.Plugin. / Name 实现 plugins.Plugin。
func (p *Plugin) Name() string { return "tftp" }

// Ports returns default TFTP port. / Ports 返回默认 TFTP 端口。
func (p *Plugin) Ports() []int { return []int{69} }

// Modes returns Identify only. / Modes 仅返回 Identify。
func (p *Plugin) Modes() plugins.Mode { return plugins.ModeIdentify }

// Credential is a no-op stub. / Credential 空 stub。
func (p *Plugin) Credential(context.Context, string, int, []types.Cred) *types.Result {
	return nil
}

// Identify sends a TFTP RRQ and parses the response. / Identify 发
// TFTP RRQ 并解析响应。
func (p *Plugin) Identify(ctx context.Context, host string, port int) *types.Result {
	return plugins.RawUDPIdentify(ctx, host, port, func(conn net.Conn) *types.Result {
		rrq := []byte{0x00, 0x01, 'x', 0x00, 'o', 'c', 't', 'e', 't', 0x00}
		if _, err := conn.Write(rrq); err != nil {
			return nil
		}
		resp := make([]byte, 516)
		n, err := conn.Read(resp)
		if err != nil || n < 4 {
			return nil
		}
		opcode := uint16(resp[0])<<8 | uint16(resp[1])
		switch opcode {
		case 3: // DATA
			bn := uint16(resp[2])<<8 | uint16(resp[3])
			return &types.Result{
				Host: host, Port: port, Service: "tftp",
				Banner: "TFTP (block " + itoa(int(bn)) + ")",
				Time:   time.Now(),
			}
		case 5: // ERROR
			ec := uint16(resp[2])<<8 | uint16(resp[3])
			if ec <= 7 {
				return &types.Result{
					Host: host, Port: port, Service: "tftp",
					Banner: "TFTP (err " + itoa(int(ec)) + ")",
					Time:   time.Now(),
				}
			}
		}
		return nil
	})
}

// itoa is a small int→string helper to avoid fmt import. /
// itoa 是避免 fmt 导入的小工具。
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
