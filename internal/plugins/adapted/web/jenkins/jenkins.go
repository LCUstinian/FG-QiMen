// jenkins.go — Jenkins Identify plugin. Sends HTTP GET /api/json
// and checks the X-Jenkins response header. Phase 1.5 of the
// audit roadmap. / jenkins.go — Jenkins 识别插件。发 HTTP GET
// /api/json 并检查 X-Jenkins 响应头。审计路线图 Phase 1.5。
package jenkins

import (
	"context"
	"net"
	"strings"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/plugins"
	"github.com/LCUstinian/FG-QiMen/internal/types"
)

const probePath = "/api/json"

// Plugin identifies Jenkins servers. / Plugin 识别 Jenkins 服务。
type Plugin struct{}

// New returns a new jenkins plugin. / New 返回一个新的 jenkins 插件。
func New() *Plugin { return &Plugin{} }

func init() { plugins.Register(New()) }

// Name implements plugins.Plugin. / Name 实现 plugins.Plugin。
func (p *Plugin) Name() string { return "jenkins" }

// Ports returns default Jenkins ports. / Ports 返回默认 Jenkins 端口。
func (p *Plugin) Ports() []int { return []int{8080, 8443, 50000} }

// Modes returns Identify only. / Modes 仅返回 Identify。
func (p *Plugin) Modes() plugins.Mode { return plugins.ModeIdentify }

// Credential is a no-op stub. / Credential 空 stub。
func (p *Plugin) Credential(ctx context.Context, host string, port int, creds []types.Cred) *types.Result {
	return nil
}

// Identify opens a raw TCP conn, sends an HTTP GET /api/json, and
// checks for the X-Jenkins response header. Phase 1.5.
// / Identify 开 TCP 原始 conn，发 HTTP GET /api/json，检查
// X-Jenkins 响应头。Phase 1.5。
func (p *Plugin) Identify(ctx context.Context, host string, port int) *types.Result {
	return plugins.RawTCPIdentify(ctx, host, port, func(conn net.Conn) *types.Result {
		scheme := "http"
		if port == 443 || port == 8443 {
			scheme = "https"
		}
		// We don't speak TLS here (would need tls.Config). Most
		// Jenkins on 8080 / 50000 are plain HTTP. / 不做 TLS
		// 握手（需要 tls.Config）。多数 Jenkins on 8080 /
		// 50000 是 plain HTTP。
		_ = scheme
		hostPort := net.JoinHostPort(host, itoa(port))
		req := "GET " + probePath + " HTTP/1.1\r\n" +
			"Host: " + hostPort + "\r\n" +
			"User-Agent: fg-qimen/0.3.1\r\n" +
			"Accept: */*\r\n" +
			"Connection: close\r\n\r\n"
		_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
		if _, err := conn.Write([]byte(req)); err != nil {
			return nil
		}
		// Read at most 4 KiB of response. / 最多读 4 KiB 响应。
		buf := make([]byte, 4096)
		total := 0
		for total < len(buf) {
			n, err := conn.Read(buf[total:])
			total += n
			if err != nil {
				break
			}
		}
		resp := string(buf[:total])
		// Look for X-Jenkins header (case-insensitive). / 找
		// X-Jenkins 头（大小写不敏感）。
		ver := extractHeader(resp, "X-Jenkins")
		if ver == "" {
			return nil
		}
		name := "Jenkins"
		if strings.Contains(strings.ToLower(resp), "hudson") {
			name = "Jenkins (Hudson fork)"
		}
		return &types.Result{
			Host:    host,
			Port:    port,
			Service: "jenkins",
			Banner:  name + " " + ver,
			Time:    time.Now(),
		}
	})
}

// extractHeader returns the value of header name from a raw HTTP
// response, or "" if not found. / extractHeader 从原始 HTTP 响应
// 返 header name 的值；未找到返 ""。
func extractHeader(resp, name string) string {
	// Case-insensitive prefix match. The response is a single
	// raw string; we look for "\n<name>:" or start-of-string
	// "<name>:". / 大小写不敏感前缀匹配。响应是单个原始字
	// 符串；找 "\n<name>:" 或开头 "<name>:"。
	lower := strings.ToLower(resp)
	lowName := strings.ToLower(name)
	for i := 0; i < len(lower)-len(lowName)-1; i++ {
		if (i == 0 || lower[i-1] == '\n') && lower[i:i+len(lowName)] == lowName && lower[i+len(lowName)] == ':' {
			// Read until \r or \n. / 读到 \r 或 \n。
			j := i + len(lowName) + 1
			for j < len(lower) && lower[j] != '\n' && lower[j] != '\r' {
				j++
			}
			return strings.TrimSpace(resp[i+len(name)+1 : j])
		}
	}
	return ""
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
