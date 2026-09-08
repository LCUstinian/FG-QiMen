// mockplugin_test.go — minimal mockPlugin used by benchmark_test.go and
// benchmark_v04_test.go. The benchmarks don't actually exercise the
// Identify / Credential code paths — they only need Name + Ports for
// the port-index and worker-throughput measurements — so all probe
// methods return nil / empty.
//
// / mockplugin_test.go — 供 benchmark_test.go / benchmark_v04_test.go
// 使用的最小 mockPlugin。基准测试不真的走 Identify / Credential 路径
// ——只需要 Name + Ports 用于端口索引和 worker 吞吐测量——所以所有
// probe 方法返回 nil / empty 即可。

package core

import (
	"context"

	"github.com/LCUstinian/FG-QiMen/internal/plugins"
	"github.com/LCUstinian/FG-QiMen/internal/types"
)

// mockPlugin is a no-op plugins.Plugin used only by benchmarks. It is
// defined here so benchmark_test.go and benchmark_v04_test.go can both
// reference it without a circular test-fixture dependency.
// / mockPlugin 是仅给基准测试用的无操作 plugins.Plugin。在此处
// 定义让 benchmark_test.go 和 benchmark_v04_test.go 都能引用而
// 不产生测试夹具循环依赖。
type mockPlugin struct {
	name  string
	ports []int
}

func (m *mockPlugin) Name() string { return m.name }

func (m *mockPlugin) Ports() []int { return m.ports }

func (m *mockPlugin) Modes() plugins.Mode {
	// Identify + Credential both; benchmarks don't actually invoke
	// them but returning a non-zero Mode avoids short-circuit paths
	// in worker dispatch. / Identify + Credential 都返回；基准
	// 测试不会真调它们，但返回非零 Mode 避免 worker dispatch 短路。
	return plugins.ModeIdentify | plugins.ModeCredential
}

func (m *mockPlugin) Identify(ctx context.Context, host string, port int) *types.Result {
	return nil
}

func (m *mockPlugin) Credential(ctx context.Context, host string, port int, creds []types.Cred) *types.Result {
	return nil
}

// Ensure mockPlugin satisfies plugins.Plugin at compile time.
// / 编译期确保 mockPlugin 满足 plugins.Plugin 接口。
var _ plugins.Plugin = (*mockPlugin)(nil)
