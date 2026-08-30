// plugintest_test.go — sanity test for the Smoke helper itself.
//
// plugintest is a package that other packages' test files import to
// smoke-test their plugins. This file adds a minimal self-test so
// the helper itself is non-zero covered. / Smoke helper 自身的健全性
// 测试。plugintest 是被其他包的测试文件 import 用来冒烟测试其插件
// 的，给它自己加个测试确保 helper 自身被某处覆盖。
package plugintest

import (
	"context"
	"testing"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/plugins"
	"github.com/LCUstinian/FG-QiMen/internal/types"
)

// minimalPlugin is the smallest valid Plugin implementation. It
// returns a non-empty name, one port, Identify mode, and nil for
// every network call. / minimalPlugin 是最小的有效 Plugin 实现。
// 返非空名、1 个端口、Identify 模式，所有网络调用都返 nil。
type minimalPlugin struct{}

func (minimalPlugin) Name() string        { return "minimal" }
func (minimalPlugin) Ports() []int        { return []int{1} }
func (minimalPlugin) Modes() plugins.Mode { return plugins.ModeIdentify }
func (minimalPlugin) Identify(context.Context, string, int) *types.Result {
	return nil
}
func (minimalPlugin) Credential(context.Context, string, int, []types.Cred) *types.Result {
	return nil
}

// dualModePlugin has both Identify and Credential modes enabled
// (used to exercise the Credential path of Smoke). / dualModePlugin
// 同时启用 Identify 和 Credential 模式（用于跑 Smoke 的 Credential
// 路径）。
type dualModePlugin struct{ minimalPlugin }

func (dualModePlugin) Modes() plugins.Mode { return plugins.ModeIdentify | plugins.ModeCredential }

// TestSmoke_MinimalPlugin_Passes runs Smoke against a minimal
// valid plugin and confirms the helper doesn't false-positive. /
// 用最小有效插件跑 Smoke，确认 helper 不误报。
func TestSmoke_MinimalPlugin_Passes(t *testing.T) {
	Smoke(t, minimalPlugin{})
}

// TestSmoke_DualModePlugin_Passes exercises the Credential
// branch of Smoke. / 跑 Smoke 的 Credential 分支。
func TestSmoke_DualModePlugin_Passes(t *testing.T) {
	Smoke(t, dualModePlugin{})
}

// TestClosedPortConst pins the closed-port constant so a future
// change to a "definitely listening" port (which would make
// Smoke return false negatives) is caught by review. / 钉死
// closedPort 常量，防止未来改成"可能有人监听"的端口（会让 Smoke
// 漏报）。
func TestClosedPortConst(t *testing.T) {
	if closedPort != 1 {
		t.Errorf("closedPort = %d, want 1 (tcpmux; reserved-but-unused on all major OSes)", closedPort)
	}
	if shortTimeout != 2*time.Second {
		t.Errorf("shortTimeout = %v, want 2s (caps per-call hang time)", shortTimeout)
	}
}
