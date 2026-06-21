// Package plugintest provides shared test helpers for plugin smoke tests.
//
// Package plugintest 为插件冒烟测试提供共享测试助手。
//
// A.6 of the audit roadmap: the adapted/ subtree historically had 0%
// statement coverage for most plugins (network I/O is hard to test
// without a fake server per protocol). This package lets every plugin
// get to a non-zero coverage baseline with a single 10-line test file
// that calls Smoke(t, New()).
//
// A.6 审计路线图项：adapted/ 子树历史上多数插件 0% 覆盖率（无 fake
// server 难测网络 I/O）。本包让每个插件用 10 行测试文件调用
// Smoke(t, New()) 即可达到非零覆盖率基线。
//
// Smoke is deliberately minimal — it only checks the interface
// contract and that Identify/Credential on a closed port return nil
// (no false-positive, no panic). It does NOT exercise the protocol
// logic; for plugins that need real coverage (e.g. ntp, rdp), see the
// per-plugin fake-server tests in dns_test.go / ntp_test.go / rdp_test.go.
//
// Smoke 故而保持极简——只检查接口契约以及 Identify/Credential 在
// 关闭端口上返回 nil（无误报、无 panic）。它不验证协议逻辑；需
// 真实覆盖的插件（如 ntp、rdp）参见 dns_test.go / ntp_test.go /
// rdp_test.go 中的 per-plugin fake-server 测试。
package plugintest

import (
	"context"
	"testing"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/plugins"
	"github.com/LCUstinian/FG-QiMen/internal/types"
)

// closedPort is the target for "nothing is listening here" checks.
// Port 1 (tcpmux) is reserved-but-unused on virtually every system, so
// 127.0.0.1:1 is a safe "definitely closed" address for the duration
// of the test. / closedPort 是"没人在听"检查的目标。端口 1 (tcpmux)
// 在几乎所有系统上是保留但未使用的，所以 127.0.0.1:1 在测试期间是
// 安全的"确定关闭"地址。
const closedPort = 1

// shortTimeout caps each Identify/Credential call so a broken plugin
// that hangs on connect (e.g. blocking DNS lookup) fails fast instead
// of stalling the test runner. / shortTimeout 给每次 Identify/Credential
// 调用设上限,避免坏插件在 connect 时挂起（如 DNS lookup 阻塞）拖死
// 测试运行器。
const shortTimeout = 2 * time.Second

// Smoke runs a minimal sanity test on a Plugin implementation:
//   - Name() returns a non-empty string
//   - Ports() returns a non-empty slice
//   - Modes() is non-zero (at least one of Identify / Credential)
//   - Identify() on a closed port returns nil within shortTimeout
//   - Credential() (if implemented) on a closed port returns nil
//     within shortTimeout
//
// Smoke 对 Plugin 实现跑最小健全性测试：
//   - Name() 返回非空字符串
//   - Ports() 返回非空 slice
//   - Modes() 非零（至少 Identify / Credential 之一）
//   - Identify() 在关闭端口上 shortTimeout 内返回 nil
//   - Credential()（若实现）在关闭端口上 shortTimeout 内返回 nil
func Smoke(t *testing.T, p plugins.Plugin) {
	t.Helper()
	if p == nil {
		t.Fatal("plugin is nil")
	}
	if name := p.Name(); name == "" {
		t.Errorf("Name() returned empty string")
	}
	if ports := p.Ports(); len(ports) == 0 {
		t.Errorf("Ports() returned empty slice")
	}
	if modes := p.Modes(); modes == 0 {
		t.Errorf("Modes() returned 0 — plugin declares no capability")
	}

	// Identify path — always run. / Identify 路径——总跑。
	tctx, cancel := context.WithTimeout(context.Background(), shortTimeout)
	defer cancel()
	if got := p.Identify(tctx, "127.0.0.1", closedPort); got != nil {
		t.Errorf("Identify on closed port returned %+v, want nil", got)
	}

	// Credential path — only run if the plugin declares it. Identify-
	// only plugins (e.g. mysql, jenkins) have a no-op Credential that
	// returns nil, but we don't bother calling it: that path is
	// trivial and adds no signal beyond the nil-check we'd already
	// need to suppress.
	// Credential 路径——只在插件声明时才跑。Identify-only 插件（如
	// mysql、jenkins）的 Credential 是 no-op 返回 nil,我们不调用:
	// 该路径很琐碎,除了 nil-check 不会增加信号。
	if p.Modes()&plugins.ModeCredential != 0 {
		tctx, cancel := context.WithTimeout(context.Background(), shortTimeout)
		defer cancel()
		if got := p.Credential(tctx, "127.0.0.1", closedPort, []types.Cred{
			{User: "u", Pass: "p"},
		}); got != nil {
			t.Errorf("Credential on closed port returned %+v, want nil", got)
		}
	}
}
