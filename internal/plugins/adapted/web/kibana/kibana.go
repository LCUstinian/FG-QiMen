// kibana.go — Kibana Identify plugin. Sends HTTP GET /api/status
// and parses the JSON response. / kibana.go — Kibana 识别插件。
// 发 HTTP GET /api/status 并解析 JSON 响应。
package kibana

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"strings"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/plugins"
	"github.com/LCUstinian/FG-QiMen/internal/types"
)

// Plugin identifies Kibana servers. / Plugin 识别 Kibana 服务。
type Plugin struct{}

// New returns a new kibana plugin. / New 返回一个新的 kibana 插件。
func New() *Plugin { return &Plugin{} }

func init() { plugins.Register(New()) }

// Name implements plugins.Plugin. / Name 实现 plugins.Plugin。
func (p *Plugin) Name() string { return "kibana" }

// Ports returns default Kibana ports. / Ports 返回默认 Kibana 端口。
func (p *Plugin) Ports() []int { return []int{5601} }

// Modes returns Identify only. / Modes 仅返回 Identify。
func (p *Plugin) Modes() plugins.Mode { return plugins.ModeIdentify }

// Credential is a no-op stub. / Credential 空 stub。
func (p *Plugin) Credential(ctx context.Context, host string, port int, creds []types.Cred) *types.Result {
	return nil
}

// Identify does HTTP GET /api/status. Kibana always serves this
// unauthenticated by default. / Identify 发 HTTP GET /api/status。
// Kibana 默认不需认证。
func (p *Plugin) Identify(ctx context.Context, host string, port int) *types.Result {
	return plugins.RawTCPIdentify(ctx, host, port, func(conn net.Conn) *types.Result {
		hostPort := net.JoinHostPort(host, itoa(port))
		req := "GET /api/status HTTP/1.1\r\n" +
			"Host: " + hostPort + "\r\n" +
			"User-Agent: fg-qimen/0.3.1\r\n" +
			"Accept: application/json\r\n" +
			"Connection: close\r\n\r\n"
		_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
		if _, err := conn.Write([]byte(req)); err != nil {
			return nil
		}
		buf := make([]byte, 8192)
		total := 0
		for total < len(buf) {
			n, err := conn.Read(buf[total:])
			total += n
			if err != nil {
				break
			}
		}
		resp := string(buf[:total])
		if !strings.HasPrefix(resp, "HTTP/1.1 200") {
			return nil
		}
		// Find the JSON body. / 找 JSON body。
		idx := strings.Index(resp, "\r\n\r\n")
		if idx < 0 {
			return nil
		}
		body := resp[idx+4:]
		// Lightweight parse — we only need "name" and "version.number".
		// / 轻量解析——只要 "name" 和 "version.number"。
		var status struct {
			Name    string `json:"name"`
			Version struct {
				Number string `json:"number"`
			} `json:"version"`
		}
		if err := json.Unmarshal([]byte(body), &status); err != nil {
			return nil
		}
		if status.Name == "" {
			return nil
		}
		return &types.Result{
			Host:    host,
			Port:    port,
			Service: "kibana",
			Banner:  status.Name + " " + status.Version.Number,
			Time:    time.Now(),
		}
	})
}

// itoa is a small int→string helper to avoid fmt. / itoa 是小
// int→string 辅助。
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [6]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// keep io import alive for future use. / 保留 io 导入。
var _ = io.Discard
