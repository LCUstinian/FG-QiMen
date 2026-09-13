// docgen_test.go — guard test pinning the generated docs to the live
// registries.
//
// docgen_test.go — 把生成的文档钉死在活体 registry 上的守卫测试。
//
// The same failure mode this prevents bit the project once already:
// hand-written flag tables drifted three ways at once (28 vs 45 vs 58
// flags across three docs). With this test, a flag or plugin change
// that doesn't regenerate the artifacts turns CI red with an explicit
// remediation hint — the same "implemented but never wired" guard
// philosophy as the adapted-plugin aggregation tests, applied to docs.
//
// / 本测试防止的失效模式已在项目里发生过一次：手写 flag 表同时漂移
// 出三个口径（三份文档里 28/45/58 各说各话）。有了这个测试，改了
// flag/插件却没重新生成产物，CI 会带着明确的修复提示变红——与
// adapted 插件聚合守卫测试同一哲学，应用到文档。
package docgen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGeneratedDocsUpToDate(t *testing.T) {
	artifacts := map[string]string{
		"FLAGS.md":         FlagsMarkdown(),
		"FLAGS.zh-CN.md":   FlagsMarkdownZh(),
		"PLUGINS.md":       PluginsMarkdown(),
		"PLUGINS.zh-CN.md": PluginsMarkdownZh(),
	}
	for name, want := range artifacts {
		path := filepath.Join("..", "..", "docs", name)
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s unreadable: %v — run `just docs-gen`", path, err)
		}
		if strings.TrimSpace(string(got)) != strings.TrimSpace(want) {
			t.Fatalf("%s is stale relative to the live registries — run `just docs-gen` and commit the regenerated file", path)
		}
	}
}

// TestFlagDescZhCoverage forces every registered flag to carry a
// Chinese usage translation. Without it, a new flag would silently
// render English in FLAGS.zh-CN.md; with it, CI names the offender.
// / TestFlagDescZhCoverage 强制每个已注册 flag 都带中文 usage 翻译。
// 没有它，新 flag 会在 FLAGS.zh-CN.md 里静默渲染英文；有了它，CI
// 直接点名肇事者。
func TestFlagDescZhCoverage(t *testing.T) {
	if err := ValidateFlagDescZh(); err != nil {
		t.Fatal(err)
	}
}

func TestFlagsMarkdownGroupOrderStable(t *testing.T) {
	out := FlagsMarkdown()
	last := -1
	for _, g := range flagGroupOrder {
		idx := strings.Index(out, "## "+g+"\n")
		if idx < 0 {
			continue // group legitimately absent from the registry
		}
		if idx < last {
			t.Errorf("group %q rendered out of declared order", g)
		}
		last = idx
	}
	if strings.Contains(out, "## Ungrouped") {
		t.Errorf("Ungrouped section is non-empty: every flag must carry a group annotation (see cmd/flags_test.go)")
	}
}

func TestPluginsMarkdownSortedByName(t *testing.T) {
	out := PluginsMarkdown()
	// The header block ends at the table; every `| \`name\` |` row must
	// be in ascending order.
	// / 头部块之后是表格；每个 `| \`name\` |` 行必须升序。
	var names []string
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(line, "| `") {
			continue
		}
		end := strings.Index(line, "` |")
		if end > 3 {
			names = append(names, line[3:end])
		}
	}
	for i := 1; i < len(names); i++ {
		if names[i-1] >= names[i] {
			t.Errorf("plugin rows not strictly ascending at %q vs %q", names[i-1], names[i])
		}
	}
}
