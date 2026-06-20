// azure.go — Azure Instance Metadata Service Identify plugin.
// Phase 1.8 of the audit roadmap.
//
// / azure.go — Azure Instance Metadata Service 识别插件。审计
// 路线图 Phase 1.8。
//
// Azure IMDS: HTTP GET http://169.254.169.254/metadata/instance
// with header "Metadata: true" returns JSON {compute: {...},
// network: {...}}. / Azure IMDS：HTTP GET
// http://169.254.169.254/metadata/instance 带 "Metadata: true"
// 头返 JSON {compute: {...}, network: {...}}。
//
// HARD-rule compliance: we send the GET and check the response
// status. We do NOT read sensitive fields like access tokens or
// user data. / HARD 规则合规：我们发 GET 并检查响应状态。我
// 们**不**读敏感字段如 access token 或 user data。
package azure

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/plugins"
	"github.com/LCUstinian/FG-QiMen/internal/types"
)

// imdsHost is the well-known Azure IMDS link-local IP. / imdsHost
// 是著名的 Azure IMDS link-local IP。
const imdsHost = "169.254.169.254"

// Plugin identifies Azure IMDS endpoints. / Plugin 识别 Azure
// IMDS 端点。
type Plugin struct{}

// New returns a new azure plugin. / New 返回一个新的 azure 插件。
func New() *Plugin { return &Plugin{} }

func init() { plugins.Register(New()) }

// Name implements plugins.Plugin. / Name 实现 plugins.Plugin。
func (p *Plugin) Name() string { return "azure-imds" }

// Ports returns the IMDS port (HTTP on 80). / Ports 返回 IMDS
// 端口（HTTP on 80）。
func (p *Plugin) Ports() []int { return []int{80} }

// Modes returns Identify only. / Modes 仅返回 Identify。
func (p *Plugin) Modes() plugins.Mode { return plugins.ModeIdentify }

// Credential is a no-op stub. / Credential 空 stub。
func (p *Plugin) Credential(ctx context.Context, host string, port int, creds []types.Cred) *types.Result {
	return nil
}

// Identify checks if the target host is the Azure IMDS endpoint
// (or a proxy). / Identify 检查 target host 是否是 Azure IMDS
// 端点（或代理）。
func (p *Plugin) Identify(ctx context.Context, host string, port int) *types.Result {
	if host != imdsHost {
		return nil
	}
	return probeIMDS(ctx, host, port)
}

// probeIMDS sends a GET to /metadata/instance with the
// "Metadata: true" header. / probeIMDS 发 GET 到 /metadata/
// instance 带 "Metadata: true" 头。
func probeIMDS(ctx context.Context, host string, port int) *types.Result {
	addr := net.JoinHostPort(host, itoa(port))
	url := fmt.Sprintf("http://%s/metadata/instance?api-version=2021-02-01", addr)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("User-Agent", "fg-qimen/0.3.1")
	req.Header.Set("Metadata", "true")
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
	// Verify it's IMDS JSON: must have "compute" + "vmSize" or
	// similar. / 验证是 IMDS JSON：必须含 "compute" + "vmSize"
	// 等。
	var inst struct {
		Compute struct {
			VMSize              string `json:"vmSize"`
			OSProfile           struct {
				ComputerName string `json:"computerName"`
			} `json:"osProfile"`
			Location string `json:"location"`
		} `json:"compute"`
	}
	if err := json.Unmarshal([]byte(body), &inst); err != nil {
		return nil
	}
	if inst.Compute.VMSize == "" {
		return nil
	}
	// HARD: do not include ComputerName (could be sensitive).
	// / HARD：不包含 ComputerName（可能敏感）。
	return &types.Result{
		Host:    host,
		Port:    port,
		Service: "azure-imds",
		Banner:  fmt.Sprintf("Azure IMDS (vmSize=%s, location=%s)",
			inst.Compute.VMSize, inst.Compute.Location),
		Time: time.Now(),
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
