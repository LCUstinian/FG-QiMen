// weblogic.go — Oracle WebLogic Identify plugin. Sends HTTP
// GET /console and checks the HTML for the WebLogic server
// banner. / weblogic.go — Oracle WebLogic 识别插件。发 HTTP
// GET /console 并检查 HTML 中的 WebLogic server banner。
//
// WebLogic 7001 default console responds with an HTML page
// containing a "WebLogic Server" footer / version. We don't
// authenticate; we just identify. / WebLogic 7001 默认 console
// 返 HTML 页，含 "WebLogic Server" 页脚 / 版本。我们不认证，
// 只识别。
package weblogic

import (
	"context"
	"net"
	"regexp"
	"strings"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/plugins"
	"github.com/LCUstinian/FG-QiMen/internal/types"
)

// Plugin identifies WebLogic servers. / Plugin 识别 WebLogic 服务。
type Plugin struct{}

// New returns a new weblogic plugin. / New 返回一个新的 weblogic 插件。
func New() *Plugin { return &Plugin{} }

func init() { plugins.Register(New()) }

// Name implements plugins.Plugin. / Name 实现 plugins.Plugin。
func (p *Plugin) Name() string { return "weblogic" }

// Ports returns default WebLogic ports. / Ports 返回默认 WebLogic 端口。
func (p *Plugin) Ports() []int { return []int{7001, 7002, 8443} }

// Modes returns Identify only. / Modes 仅返回 Identify。
func (p *Plugin) Modes() plugins.Mode { return plugins.ModeIdentify }

// Credential is a no-op stub. / Credential 空 stub。
func (p *Plugin) Credential(ctx context.Context, host string, port int, creds []types.Cred) *types.Result {
	return nil
}

// versionRe matches the "WebLogic Server Version: 12.2.1.4.0"
// / "WebLogic Server Temporary Patch: 12345678" lines that
// WebLogic emits in HTML footers. / versionRe 匹配 WebLogic 在
// HTML 页脚里写的 "WebLogic Server Version: 12.2.1.4.0" 等行。
var versionRe = regexp.MustCompile(`WebLogic\s+Server(?:\s+Version)?[:\s]+([\d.]+)`)

// Identify sends HTTP GET /console. The page contains the
// "WebLogic Server" footer. / Identify 发 HTTP GET /console。
// 页含 "WebLogic Server" 页脚。
func (p *Plugin) Identify(ctx context.Context, host string, port int) *types.Result {
	return plugins.RawTCPIdentify(ctx, host, port, func(conn net.Conn) *types.Result {
		hostPort := net.JoinHostPort(host, itoa(port))
		req := "GET /console HTTP/1.1\r\n" +
			"Host: " + hostPort + "\r\n" +
			"User-Agent: fg-qimen/0.3.1\r\n" +
			"Accept: */*\r\n" +
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
		// Need a 200/302 response with a WebLogic marker. / 需
		// 200/302 响应且有 WebLogic 标志。
		if !(strings.HasPrefix(resp, "HTTP/1.1 200") ||
			strings.HasPrefix(resp, "HTTP/1.1 302")) {
			return nil
		}
		// Body parsing. / 解析 body。
		idx := strings.Index(resp, "\r\n\r\n")
		if idx < 0 {
			return nil
		}
		body := resp[idx+4:]
		if !strings.Contains(body, "WebLogic") {
			return nil
		}
		ver := "unknown"
		if m := versionRe.FindStringSubmatch(body); m != nil {
			ver = m[1]
		}
		return &types.Result{
			Host:    host,
			Port:    port,
			Service: "weblogic",
			Banner:  "Oracle WebLogic Server " + ver,
			Time:    time.Now(),
		}
	})
}

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
