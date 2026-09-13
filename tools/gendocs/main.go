// gendocs regenerates the generated-docs artifacts under docs/ from the
// in-source flag + plugin registries. Run from the repo root:
//
//	go run ./tools/gendocs            # FLAGS + PLUGINS, both languages
//	go run ./tools/gendocs flags      # FLAGS.md + FLAGS.zh-CN.md only
//	go run ./tools/gendocs plugins    # PLUGINS.md + PLUGINS.zh-CN.md only
//
// `just docs-gen` is the documented entry point. Each run writes the
// bilingual pair of its target; the Chinese artifacts require every
// flag to have a translation in internal/docgen/zh.go (validated here
// before anything is written). The artifacts are guarded by
// internal/docgen's TestGeneratedDocsUpToDate: CI goes red the moment
// a flag/plugin change lands without regenerating.
//
// gendocs 从源码内的 flag + 插件 registry 重新生成 docs/ 下的文档产
// 物。在仓库根目录运行。每次运行写出目标的双语对；中文产物要求每个
// flag 都在 internal/docgen/zh.go 配有翻译（写文件前先校验）。产物由
// internal/docgen 的 TestGeneratedDocsUpToDate 守卫：flag/插件变更
// 落地而未重新生成时 CI 变红。
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/LCUstinian/FG-QiMen/internal/docgen"
)

func main() {
	what := "all"
	if len(os.Args) > 1 {
		what = os.Args[1]
	}

	// Fail before writing anything if a flag lacks a Chinese usage
	// translation — the bilingual pair can never ship half-covered.
	// / 有 flag 缺中文 usage 翻译时先于任何写文件失败——双语对永远
	// 不允许半覆盖状态出厂。
	if err := docgen.ValidateFlagDescZh(); err != nil {
		fmt.Fprintf(os.Stderr, "gendocs: %v\n", err)
		os.Exit(1)
	}

	type artifact struct {
		name    string
		content string
	}
	var all []artifact
	if what == "all" || what == "flags" {
		all = append(all,
			artifact{"FLAGS.md", docgen.FlagsMarkdown()},
			artifact{"FLAGS.zh-CN.md", docgen.FlagsMarkdownZh()})
	}
	if what == "all" || what == "plugins" {
		all = append(all,
			artifact{"PLUGINS.md", docgen.PluginsMarkdown()},
			artifact{"PLUGINS.zh-CN.md", docgen.PluginsMarkdownZh()})
	}
	if len(all) == 0 {
		fmt.Fprintf(os.Stderr, "gendocs: unknown target %q (want flags | plugins | all)\n", what)
		os.Exit(2)
	}

	for _, a := range all {
		path := filepath.Join("docs", a.name)
		if err := os.WriteFile(path, []byte(a.content), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "gendocs: write %s: %v\n", path, err)
			os.Exit(1)
		}
		fmt.Println("wrote", path)
	}
}
