// aggregation_test.go — registration-closure guard.
//
// Twice already a finished component never ran in production because
// its registration import was missing from the aggregation chain:
//   - the Stage-0 fingerprint layer was dead code on real scans
//     (scanner built its probe without the BannerReader), and
//   - the entire webtitle plugin (3139-rule FingerprintHub DB,
//     favicon matcher) never registered: adapted/web/http.go imported
//     only the basic http plugin, so webtitle's init() never fired
//     outside unit tests.
//
// Both shipped green — unit tests import the plugin packages
// directly, so their init()s fire in the test process and everything
// passes while the production binary silently lacks the feature.
// These tests close that gap by asserting the AGGREGATION CLOSURE —
// what a binary that imports only the root gets — not individual
// behavior:
//
//	TestAggregationImportsAllSubpackages: every package under
//	  internal/plugins/adapted/ must appear in the transitive
//	  dependency closure of the adapted ROOT package. A new plugin
//	  directory that nobody blank-imported into the chain turns the
//	  closure red here, in CI, before any release.
//
//	TestRootBinaryImportsAdapted: cmd/root.go must blank-import the
//	  adapted root (the single entry point that makes the whole
//	  plugin registry exist in the real binary).
//
// Implementation note: `go test` runs with the package source
// directory as cwd, so `go list ./...` lists the adapted tree and
// `go list -deps .` walks the root's closure; package-path form
// (`go list -deps <module>/cmd`) works from any in-module cwd.
//
// aggregation_test.go — 注册闭包守卫。
//
// 已经两次因为聚合链缺注册 import，做好的组件从未在生产运行：
//   - Stage-0 指纹层在真实扫描上是死代码（scanner 构建 probe 时
//     没接 BannerReader）；
//   - 整个 webtitle 插件（3139 条 FingerprintHub 规则库、favicon
//     匹配器）从未注册：adapted/web/http.go 只 import 了基础 http
//     插件，webtitle 的 init() 在单测之外从未触发。
//
// 两次都带着绿灯发布——单测直接 import 插件包，测试进程里 init()
// 全部触发，一切通过，而生产二进制静默缺功能。本文件把缺口封死：
// 断言的对象是"聚合闭包"（只 import 根包的二进制能拿到什么），而
// 不是单个行为：
//
//	TestAggregationImportsAllSubpackages：internal/plugins/adapted/
//	  下的每个包都必须出现在 adapted 根包的传递依赖闭包中。新加
//	  的插件目录若没人 blank-import 进链路，这里在 CI 上直接变红，
//	  赶在任何 release 之前。
//
//	TestRootBinaryImportsAdapted：cmd/root.go 必须 blank-import
//	  adapted 根包（让整个插件注册表在真实二进制中存在的唯一入口）。
//
// 实现说明：`go test` 以包源码目录为 cwd，故 `go list ./...` 列出
// adapted 树、`go list -deps .` 走根包闭包；包路径形式
// （`go list -deps <module>/cmd`）在模块内任意 cwd 均可解析。
package adapted

import (
	"os/exec"
	"strings"
	"testing"
)

const modulePath = "github.com/LCUstinian/FG-QiMen"

// goList runs go list with the given arguments and returns one
// package path per line. The go toolchain is a hard prerequisite of
// this repo (tests compile it), so a missing binary is a t.Fatalf,
// not a skip. / goList 执行 go list 并按行返回包路径。go 工具链是
// 本仓库的硬前置（测试本身就要编译它），二进制缺失按 Fatal 处理，
// 不跳过。
func goList(t *testing.T, args ...string) []string {
	t.Helper()
	out, err := exec.Command("go", append([]string{"list"}, args...)...).Output()
	if err != nil {
		t.Fatalf("go list %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	var pkgs []string
	for _, line := range strings.Split(string(out), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			pkgs = append(pkgs, line)
		}
	}
	return pkgs
}

// TestAggregationImportsAllSubpackages asserts the root package's
// transitive closure swallows every package in the adapted tree.
// / TestAggregationImportsAllSubpackages 断言根包传递闭包吞下
// adapted 树里的每一个包。
func TestAggregationImportsAllSubpackages(t *testing.T) {
	// All packages under internal/plugins/adapted/ (cwd = adapted
	// root). / adapted 目录下所有包（cwd 即 adapted 根）。
	all := goList(t, "./...")

	// Transitive closure of the ROOT package only — what a binary
	// blank-importing just this package sees. / 仅根包的传递闭包
	//——即只 blank-import 本包的二进制能看到的一切。
	closure := make(map[string]struct{})
	for _, p := range goList(t, "-deps", ".") {
		closure[p] = struct{}{}
	}

	var missing []string
	for _, p := range all {
		if _, ok := closure[p]; !ok {
			missing = append(missing, p)
		}
	}
	if len(missing) > 0 {
		t.Errorf("packages exist under internal/plugins/adapted but are OUTSIDE the aggregation closure — a binary importing only the adapted root will never register them:\n  %s\nAdd a blank import to the owning category package (e.g. internal/plugins/adapted/web/http.go) or the adapted doc.go.",
			strings.Join(missing, "\n  "))
	}
}

// TestRootBinaryImportsAdapted pins cmd/root.go to the adapted root
// import — the other half of the webtitle-class accident (root forgets
// the tree entirely). / TestRootBinaryImportsAdapted 把 cmd/root.go
// 钉在 adapted 根包 import 上——webtitle 类事故的另一半（root 整
// 棵树都忘了 import）。
func TestRootBinaryImportsAdapted(t *testing.T) {
	want := modulePath + "/internal/plugins/adapted"
	for _, p := range goList(t, "-deps", modulePath+"/cmd") {
		if p == want {
			return
		}
	}
	t.Errorf("%s/cmd does not depend on %s — the plugin registry is empty in real binaries; restore the blank import in cmd/root.go",
		modulePath, want)
}
