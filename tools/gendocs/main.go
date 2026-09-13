// gendocs regenerates the generated-docs artifacts from the in-source
// flag + plugin + fingerprint registries. Run from the repo root:
//
//	go run ./tools/gendocs            # FLAGS + PLUGINS + README strips
//	go run ./tools/gendocs flags      # FLAGS.md + FLAGS.zh-CN.md only
//	go run ./tools/gendocs plugins    # PLUGINS.md + PLUGINS.zh-CN.md only
//	go run ./tools/gendocs readme     # README(.zh-CN).md stats strips only
//
// `just docs-gen` is the documented entry point. Each run writes the
// bilingual pair of its target; the Chinese artifacts require every
// flag to have a translation in internal/docgen/zh.go (validated here
// before anything is written). The artifacts are guarded by
// internal/docgen's TestGeneratedDocsUpToDate and
// TestREADMEStatsUpToDate: CI goes red the moment a flag/plugin/
// fingerprint change lands without regenerating.
//
// gendocs 从源码内的 flag + 插件 + 指纹 registry 重新生成文档产物。
// 在仓库根目录运行。每次运行写出目标的双语对；中文产物要求每个
// flag 都在 internal/docgen/zh.go 配有翻译（写文件前先校验）。产物由
// internal/docgen 的 TestGeneratedDocsUpToDate 与 TestREADMEStatsUpToDate
// 守卫：flag/插件/指纹变更落地而未重新生成时 CI 变红。
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
	if len(all) == 0 && what != "readme" {
		fmt.Fprintf(os.Stderr, "gendocs: unknown target %q (want flags | plugins | readme | all)\n", what)
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

	// The README stats strips are surgical rewrites between the
	// gendocs:stats markers, never whole-file overwrites — the READMEs
	// are hand-written L1 prose everywhere else and must not be touched.
	// / README 统计条是 gendocs:stats 标记之间的外科手术式替换，绝不
	// 整文件重写——README 其余部分是手写的 L1 常青文本，必须原样保留。
	if what == "all" || what == "readme" {
		strips := []struct{ name, block string }{
			{"README.md", docgen.READMEStats()},
			{"README.zh-CN.md", docgen.READMEStatsZh()},
		}
		for _, s := range strips {
			raw, err := os.ReadFile(s.name)
			if err != nil {
				fmt.Fprintf(os.Stderr, "gendocs: read %s: %v (must run from the repo root)\n", s.name, err)
				os.Exit(1)
			}
			out, err := docgen.ApplyStatsBlock(string(raw), s.block)
			if err != nil {
				fmt.Fprintf(os.Stderr, "gendocs: %s: %v\n", s.name, err)
				os.Exit(1)
			}
			if err := os.WriteFile(s.name, []byte(out), 0o644); err != nil {
				fmt.Fprintf(os.Stderr, "gendocs: write %s: %v\n", s.name, err)
				os.Exit(1)
			}
			fmt.Println("wrote", s.name)
		}
	}
}
