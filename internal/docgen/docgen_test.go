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

// readRepoDoc reads a repository markdown file with CRLF normalized
// to LF. On windows-latest CI the runner checks out with autocrlf=true,
// so every tracked .md lands on disk as CRLF while the generated
// artifacts are LF — a byte comparison would fail only on that one
// platform (macOS/ubuntu checkout LF). Normalizing here keeps the
// guard about CONTENT drift, not about git's eol settings.
// / readRepoDoc 读取仓库 markdown 并把 CRLF 归一化为 LF。windows-latest
// CI 以 autocrlf=true 检出，所有受跟踪的 .md 落盘为 CRLF，而生成产物
// 是 LF——逐字节比较会在且仅会在那个平台失败（macOS/ubuntu 检出为
// LF）。在此归一化让守卫关注内容漂移，而非 git 的 eol 配置。
func readRepoDoc(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s unreadable: %v", path, err)
	}
	return strings.ReplaceAll(string(raw), "\r\n", "\n")
}

func TestGeneratedDocsUpToDate(t *testing.T) {
	artifacts := map[string]string{
		"FLAGS.md":         FlagsMarkdown(),
		"FLAGS.zh-CN.md":   FlagsMarkdownZh(),
		"PLUGINS.md":       PluginsMarkdown(),
		"PLUGINS.zh-CN.md": PluginsMarkdownZh(),
	}
	for name, want := range artifacts {
		path := filepath.Join("..", "..", "docs", name)
		got := strings.TrimSpace(readRepoDoc(t, path))
		if got != strings.TrimSpace(want) {
			t.Fatalf("%s is stale relative to the live registries — run `just docs-gen` and commit the regenerated file", path)
		}
	}
}

// TestREADMEStatsUpToDate pins the stats strip between the
// gendocs:stats markers of both root READMEs to the live plugin /
// authenticator / probe / fingerprint registries. A plugin added
// without `just docs-gen` turns CI red here, exactly like the docs/
// artifacts above.
// / TestREADMEStatsUpToDate 把两份根 README 中 gendocs:stats 标记之间
// 的统计条钉死在活体插件 / authenticator / 探测库 / 指纹 registry
// 上。新增插件没跑 `just docs-gen` 时，CI 在这里变红，与上面的
// docs/ 产物同一机制。
func TestREADMEStatsUpToDate(t *testing.T) {
	readmes := map[string]string{
		"README.md":       READMEStats(),
		"README.zh-CN.md": READMEStatsZh(),
	}
	for name, want := range readmes {
		path := filepath.Join("..", "..", name)
		got, err := StatsBlock(readRepoDoc(t, path))
		if err != nil {
			t.Fatalf("%s: %v — add the gendocs:stats marker pair and run `just docs-gen`", path, err)
		}
		if got != want {
			t.Fatalf("%s stats strip is stale relative to the live registries — run `just docs-gen` and commit the regenerated README", path)
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
