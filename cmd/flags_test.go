// flags_test.go — verify that all registered flags have a group annotation.
//
// flags_test.go — 验证所有注册的 flag 都有分组标注。
//
// We don't unit-test the help output (cobra's templates are not stable
// across versions), but we DO test that every persistent flag is
// reachable AND has a group annotation. This guards against the
// "added a flag but forgot to annotate it" bug that would silently
// break the help-text categorization.
//
// 我们不单元测试 help 输出（cobra 模板跨版本不稳定），但测试每个
// 持久化 flag 都能找到且有 group 标注。防止"加了 flag 却忘了标注"
// 的 bug——那种 bug 会静默破坏 help 文本分类。
package cmd

import (
	"testing"

	"github.com/spf13/pflag"
)

// expectedFlagGroups maps flag long-name → expected group label.
// A failure here means either (a) a new flag was added without an
// annotate() call, or (b) a flag was removed but the map still
// references it.
//
// expectedFlagGroups 把 flag 长名映射到预期分组标签。
// 此处失败意味着：(a) 加了 flag 但没调 annotate()，或 (b) 删了 flag
// 但 map 仍引用它。
var expectedFlagGroups = map[string]string{
	// Target
	"host":       "Target",
	"hosts-file": "Target",
	// Workspace
	"project":     "Workspace",
	"project-key": "Workspace",
	"mode":        "Workspace",
	"resume":      "Workspace",
	"no-state":    "Workspace",
	// Ports
	"ports":         "Ports",
	"exclude-ports": "Ports",
	"alive-only":    "Ports",
	// Network
	"proxy":           "Network",
	"socks5":          "Network",
	"iface":           "Network",
	"port-timeout":    "Network",
	"web-timeout":     "Network",
	"web-fingerprint": "Network",
	// Concurrency
	"threads":          "Concurrency",
	"timeout":          "Concurrency",
	"shutdown-timeout": "Concurrency",
	"max-workers":      "Concurrency",
	// Credentials
	"user":               "Credentials",
	"pass":               "Credentials",
	"user-file":          "Credentials",
	"pass-file":          "Credentials",
	"http-form-url":      "Credentials",
	"http-form-fields":   "Credentials",
	"http-form-success":  "Credentials",
	"http-form-failure":  "Credentials",
	"http-form-redirect": "Credentials",
	// Output
	"output-txt":   "Output",
	"output-json":  "Output",
	"output-csv":   "Output",
	"output-sarif": "Output",
	// Behavior
	"silent":   "Behavior",
	"no-tui":   "Behavior",
	"no-batch": "Behavior",
	"no-icmp":  "Behavior",
	"verbose":  "Behavior",
	"plugins":  "Behavior",
	// Safety
	"show-creds":   "Safety",
	"insecure-tls": "Safety",
	"insecure-ssh": "Safety",
	"known-hosts":  "Safety",
}

func TestAllFlagsHaveGroupAnnotation(t *testing.T) {
	// Snapshot the persistent flag set by invoking registerGlobalFlags
	// against a fresh FlagSet (registerGlobalFlags is idempotent
	// at the type level — it assigns to package vars, not flagset
	// state, so calling it twice is safe and we only end up with
	// one copy of each flag in *rootCmd.PersistentFlags()).
	//
	// 触发 registerGlobalFlags 并把持久化 flag 集快照出来。
	// registerGlobalFlags 在类型层面幂等——它赋给包级变量而非
	// flagset 状态,调两次也安全,*rootCmd.PersistentFlags() 里
	// 每个 flag 只一份。
	if rootCmd == nil {
		t.Fatal("rootCmd not initialized")
	}
	if rootCmd.PersistentFlags() == nil {
		t.Fatal("rootCmd.PersistentFlags() is nil")
	}

	// Iterate everything we expect; fail loudly on missing annotations.
	// 遍历所有预期项；对缺失标注大声失败。
	seen := map[string]bool{}
	rootCmd.PersistentFlags().VisitAll(func(f *pflag.Flag) {
		seen[f.Name] = true
		want, ok := expectedFlagGroups[f.Name]
		if !ok {
			// A new flag was added but expectedFlagGroups wasn't
			// updated. This is the most common regression.
			//
			// 加了 flag 但 expectedFlagGroups 未更新。
			// 这是最常见的回归。
			t.Errorf("flag --%s is not in expectedFlagGroups; add it (and call annotate())", f.Name)
			return
		}
		got := groupOf(f)
		if got != want {
			t.Errorf("flag --%s has group %q, want %q", f.Name, got, want)
		}
	})

	// Reverse check: anything in expectedFlagGroups that doesn't
	// exist as a real flag is a dead entry (someone removed a flag
	// but didn't clean up the map).
	//
	// 反向检查：expectedFlagGroups 中存在但不存在的 flag 是死
	// 条目（有人删了 flag 但没清理 map）。
	for name := range expectedFlagGroups {
		if !seen[name] {
			t.Errorf("expectedFlagGroups references --%s but no such flag is registered", name)
		}
	}
}

func TestAnnotateHelper_IdempotentAndOverwrites(t *testing.T) {
	// annotate() should not panic on repeated calls; the latest group
	// wins. We test this against a fresh FlagSet so we don't pollute
	// rootCmd.
	//
	// annotate() 重复调用不应 panic；最后分组胜出。测试用新 FlagSet
	// 不污染 rootCmd。
	pf := pflag.NewFlagSet("test", pflag.ContinueOnError)
	var s string
	pf.StringVar(&s, "x", "", "")
	annotate(pf, []string{"x"}, "First")
	if got := groupOf(pf.Lookup("x")); got != "First" {
		t.Fatalf("after first annotate: got %q, want First", got)
	}
	annotate(pf, []string{"x"}, "Second")
	if got := groupOf(pf.Lookup("x")); got != "Second" {
		t.Fatalf("after second annotate: got %q, want Second (overwrite)", got)
	}
}

func TestAnnotateHelper_MissingFlagIsNoOp(t *testing.T) {
	// annotate() with an unknown flag name must NOT panic — it's
	// called against a fully-populated FlagSet in production, but
	// future refactors might rename a flag and a stale name in the
	// annotate() list should silently do nothing rather than crash.
	//
	// annotate() 对未知 flag 名必须 NOT panic——生产中它对完整
	// FlagSet 调用,但未来重构可能改名,annotate() 列表中的过期
	// 名字应静默 no-op 而非崩溃。
	pf := pflag.NewFlagSet("test", pflag.ContinueOnError)
	annotate(pf, []string{"nonexistent"}, "Anything")
	// No assertion needed — passing without panic is the test.
}

// groupOf returns the "group" annotation for a flag, or "" if none.
// groupOf 返回 flag 的 "group" 标注，无则返回 ""。
func groupOf(f *pflag.Flag) string {
	if f == nil || f.Annotations == nil {
		return ""
	}
	g, ok := f.Annotations["group"]
	if !ok || len(g) == 0 {
		return ""
	}
	return g[0]
}
