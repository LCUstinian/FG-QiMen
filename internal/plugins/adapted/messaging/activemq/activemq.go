// activemq.go — Apache ActiveMQ Identify plugin. Reads the
// OpenWire protocol magic + version header. / activemq.go —
// Apache ActiveMQ 识别插件。读 OpenWire 协议 magic + version 头。
//
// Wire format (OpenWire v1):
//   - 4 bytes: magic ("\x00\x00\x00\x00" little-endian = 0)
//   - 1 byte:  minor version (1 = OpenWire v1, 2 = v2, 3 = v3, ...)
//   - 1 byte:  major version
//   - remaining: frame payload
//
// We just need the magic + version to identify.
// / 我们只需要 magic + version 来识别。
package activemq

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/plugins"
	"github.com/LCUstinian/FG-QiMen/internal/types"
)

// Plugin identifies ActiveMQ brokers. / Plugin 识别 ActiveMQ broker。
type Plugin struct{}

// New returns a new activemq plugin. / New 返回一个新的 activemq 插件。
func New() *Plugin { return &Plugin{} }

func init() { plugins.Register(New()) }

// Name implements plugins.Plugin. / Name 实现 plugins.Plugin。
func (p *Plugin) Name() string { return "activemq" }

// Ports returns default ActiveMQ OpenWire port. / Ports 返回默认
// ActiveMQ OpenWire 端口。
func (p *Plugin) Ports() []int { return []int{61616} }

// Modes returns Identify only. / Modes 仅返回 Identify。
func (p *Plugin) Modes() plugins.Mode { return plugins.ModeIdentify }

// Credential is a no-op stub. / Credential 空 stub。
func (p *Plugin) Credential(ctx context.Context, host string, port int, creds []types.Cred) *types.Result {
	return nil
}

// Identify sends a minimal OpenWire magic and reads the version.
// / Identify 发最小 OpenWire magic 并读版本。
func (p *Plugin) Identify(ctx context.Context, host string, port int) *types.Result {
	return plugins.RawTCPIdentify(ctx, host, port, func(conn net.Conn) *types.Result {
		// Many ActiveMQ brokers send the version on connect
		// (the WireFormatInfo frame). We just read 4 bytes —
		// if the first 4 are 0x00000000 (little-endian magic),
		// it's ActiveMQ. / 多数 ActiveMQ broker 在 connect 时
		// 发版本（WireFormatInfo 帧）。只读 4 字节——前 4 字节
		// 是 0x00000000（小端 magic）就是 ActiveMQ。
		_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		hdr := make([]byte, 4)
		n, err := conn.Read(hdr)
		if err != nil || n < 4 {
			return nil
		}
		// OpenWire magic is 0 (4 little-endian zero bytes).
		// / OpenWire magic 是 0（4 字节小端零）。
		// We accept either 0x00000000 (v1) or 0x01000000 (some
		// forks) as OpenWire magic. / 接受 0x00000000 (v1) 或
		// 0x01000000 (部分 fork) 作为 OpenWire magic。
		if hdr[0] == 0 && hdr[1] == 0 && hdr[2] == 0 && (hdr[3] == 0 || hdr[3] == 1) {
			// Read version bytes. / 读版本字节。
			ver := make([]byte, 2)
			_, _ = conn.Read(ver)
			return &types.Result{
				Host:    host,
				Port:    port,
				Service: "activemq",
				Banner:  fmt.Sprintf("ActiveMQ (OpenWire v%d.%d)", ver[1], ver[0]),
				Time:    time.Now(),
			}
		}
		return nil
	})
}
