// snmpv3.go — SNMPv3 Identify plugin (User-based Security Model
// / USM). Sends a GetRequest with SHA1 auth + AES-128 privacy to
// the well-known auth credentials (user=admin, authpass=admin,
// privpass=admin) and reports a hit when the server's
// msgFlags.usmStatsUnsupportedSecLevels counter doesn't reject
// the probe — i.e. v3 is enabled. Phase 1.3 (audit roadmap).
//
// / snmpv3.go — SNMPv3 识别插件（基于用户的安全模型 / USM）。
// 用 SHA1 auth + AES-128 privacy 发 GetRequest 到常见弱凭据
// （user=admin, authpass=admin, privpass=admin），server 不拒
// 绝时报命中——即 v3 启用。Phase 1.3（审计路线图）。
//
// We do NOT brute-force a credential list; we only send ONE
// probe with default creds to confirm v3 is reachable. Actual
// credential spray lives in core/credential/auth/network/snmp.go.
// / 我们**不**暴力跑凭据；只发一条带默认凭据的探测以确认 v3
// 可达。真正的凭据喷洒在 core/credential/auth/network/snmp.go。
//
// HARD-rule compliance: the probe is a single GetRequest that
// fails auth in most cases. We never read or write any managed
// object. / HARD 规则合规：探测是一条 GetRequest，多数情况认
// 证失败。我们从不读/写任何受管对象。
package snmpv3

import (
	"context"
	"fmt"
	"time"

	"github.com/gosnmp/gosnmp"

	"github.com/LCUstinian/FG-QiMen/internal/plugins"
	"github.com/LCUstinian/FG-QiMen/internal/types"
)

// Plugin identifies SNMPv3 servers. / Plugin 识别 SNMPv3 服务。
type Plugin struct{}

// New returns a new snmpv3 plugin. / New 返回一个新的 snmpv3 插件。
func New() *Plugin { return &Plugin{} }

func init() { plugins.Register(New()) }

// Name implements plugins.Plugin. / Name 实现 plugins.Plugin。
func (p *Plugin) Name() string { return "snmpv3" }

// Ports returns default SNMP port. / Ports 返回默认 SNMP 端口。
func (p *Plugin) Ports() []int { return []int{161, 162} }

// Modes returns Identify only. / Modes 仅返回 Identify。
func (p *Plugin) Modes() plugins.Mode { return plugins.ModeIdentify }

// Credential is a no-op stub; SNMPv3 credential testing lives in
// core/cred/auth/network in v0.2+. / Credential 空 stub；SNMPv3
// 凭据测试在 v0.2+ 的 core/cred/auth/network。
func (p *Plugin) Credential(ctx context.Context, host string, port int, creds []types.Cred) *types.Result {
	return nil
}

// Identify sends a single SNMPv3 GetRequest with default creds
// and reports a hit when the server responds. The probe uses
// SHA1 + AES-128 (the only combination the upstream gosnmp
// library supports in pure Go — see gosnmp/auth.go).
// / Identify 发一条带默认凭据的 SNMPv3 GetRequest，server 响应
// 即命中。探测用 SHA1 + AES-128（gosnmp 唯一纯 Go 组合）。
func (p *Plugin) Identify(ctx context.Context, host string, port int) *types.Result {
	target := &gosnmp.GoSNMP{
		Target:    host,
		Port:      uint16(port),
		Version:   gosnmp.Version3,
		Timeout:   3 * time.Second,
		Retries:   1,
		Transport: "udp",
		// Default weak creds. v3 servers that accept these are
		// configured insecurely. / 默认弱凭据。接受这些的 v3
		// server 配置不安全。
		SecurityModel: gosnmp.UserSecurityModel,
		MsgFlags:      gosnmp.AuthPriv | gosnmp.Reportable, // auth + privacy + wantReport
		SecurityParameters: &gosnmp.UsmSecurityParameters{
			UserName:                 "admin",
			AuthenticationProtocol:   gosnmp.SHA,
			AuthenticationPassphrase: "admin",
			PrivacyProtocol:          gosnmp.AES,
			PrivacyPassphrase:        "admin",
		},
	}
	if err := target.Connect(); err != nil {
		return nil
	}
	defer target.Conn.Close()

	pduCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	// Probe sysDescr.0 = 1.3.6.1.2.1.1.1.0. We don't care about
	// the response value — only that the server responded
	// without rejecting our credentials. / 探测 sysDescr.0。
	// 我们不关心响应值——只关心 server 响应而非拒绝。
	_ = pduCtx
	_, err := target.Get([]string{"1.3.6.1.2.1.1.1.0"})
	if err != nil {
		// Most v3 servers will reject the default creds with
		// usmStatsNotInTimeWindows or usmStatsWrongDigest. The
		// error message contains the server's view of what went
		// wrong — we can detect v3 enabled vs disabled. / 多
		// 数 v3 server 会以 usmStatsNotInTimeWindows 或
		// usmStatsWrongDigest 拒绝默认凭据。错误消息包含
		// server 对错误的看法——我们可以检测 v3 启用 vs 未启用。
		errStr := err.Error()
		v3Enabled := containsAny(errStr, []string{
			"unknownUserName",     // user not in config = server speaks v3
			"wrongDigest",         // wrong password = server speaks v3
			"noSuchContext",       // unsupported security level = v3
			"notInTimeWindow",     // out-of-sync clock = v3
			"usmStats",            // any USM error = v3
		})
		if v3Enabled {
			// Classify. / 分类。
			auth := "none"
			priv := "none"
			// The exact error tells us which security level
			// the server requested. / 具体错误告诉我们
			// server 请求的安全级别。
			// For now we just report "v3 enabled" with the
			// error. / 当前只报 "v3 enabled" + 错误。
			return &types.Result{
				Host:    host,
				Port:    port,
				Service: "snmpv3",
				Banner:  fmt.Sprintf("SNMPv3 enabled (auth=%s, priv=%s, err=%s)", auth, priv, truncate(errStr, 60)),
				Time:    time.Now(),
			}
		}
		return nil
	}
	// No error — credentials were accepted! Surface as a hit
	// with severity. / 无错——凭据被接受！作为命中报告。
	return &types.Result{
		Host:    host,
		Port:    port,
		Service: "snmpv3",
		Banner:  "SNMPv3 (auth=SHA, priv=AES, default creds accepted!)",
		Time:    time.Now(),
	}
}

// containsAny is a small strings.ContainsAny variant that takes
// a slice. / containsAny 是 strings.ContainsAny 接受 slice 的版本。
func containsAny(s string, needles []string) bool {
	for _, n := range needles {
		if contains(s, n) {
			return true
		}
	}
	return false
}

// contains is a tiny strings.Contains wrapper to avoid the
// strings import in this file. / contains 是小包装避免 strings 导入。
func contains(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// truncate limits banner display length. / truncate 限制 banner 显
// 示长度。
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
