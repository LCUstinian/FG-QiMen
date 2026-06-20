// aws.go — AWS EC2 Instance Metadata Service (IMDS) Identify
// plugin. Phase 1.8 of the audit roadmap.
//
// / aws.go — AWS EC2 Instance Metadata Service（IMDS）识别
// 插件。审计路线图 Phase 1.8。
//
// IMDSv1: HTTP GET http://169.254.169.254/latest/meta-data/ returns
// a list of metadata paths (no token). We do NOT read sensitive
// sub-paths (HARD). / IMDSv1：HTTP GET
// http://169.254.169.254/latest/meta-data/ 返 metadata 路径
// 列表（无 token）。我们**不**读敏感子路径（HARD）。
package aws

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/plugins"
	"github.com/LCUstinian/FG-QiMen/internal/types"
)

const imdsHost = "169.254.169.254"

// Plugin identifies AWS IMDS endpoints. / Plugin 识别 AWS IMDS
// 端点。
type Plugin struct{}

// New returns a new aws plugin. / New 返回一个新的 aws 插件。
func New() *Plugin { return &Plugin{} }

func init() { plugins.Register(New()) }

// Name implements plugins.Plugin. / Name 实现 plugins.Plugin。
func (p *Plugin) Name() string { return "aws-imds" }

// Ports returns the IMDS port (HTTP on 80). / Ports 返回 IMDS
// 端口（HTTP on 80）。
func (p *Plugin) Ports() []int { return []int{80} }

// Modes returns Identify only. / Modes 仅返回 Identify。
func (p *Plugin) Modes() plugins.Mode { return plugins.ModeIdentify }

// Credential is a no-op stub. / Credential 空 stub。
func (p *Plugin) Credential(ctx context.Context, host string, port int, creds []types.Cred) *types.Result {
	return nil
}

// Identify checks if the target host is the AWS IMDS endpoint
// (or a proxy that returns IMDS-style paths). / Identify 检
// 查 target host 是否是 AWS IMDS 端点。
func (p *Plugin) Identify(ctx context.Context, host string, port int) *types.Result {
	if host != imdsHost {
		return nil
	}
	addr := net.JoinHostPort(host, itoa(port))
	url := fmt.Sprintf("http://%s/latest/meta-data/", addr)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("User-Agent", "fg-qimen/0.3.1")
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil
	}
	buf := make([]byte, 4096)
	n, _ := resp.Body.Read(buf)
	body := string(buf[:n])
	if !strings.Contains(body, "instance-id") {
		return nil
	}
	lines := strings.Split(body, "\n")
	return &types.Result{
		Host:    host,
		Port:    port,
		Service: "aws-imds",
		Banner:  fmt.Sprintf("AWS IMDSv1 (paths=%d)", len(lines)),
		Time:    time.Now(),
	}
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
