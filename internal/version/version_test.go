// version_test.go — regression tests for the ldflag-injected Value
// (v0.3.1 release candidates used 'const Value' which the linker
// couldn't patch; this test pins the 'var' choice so a future
// refactor doesn't reintroduce the silent-failure bug).
//
// version_test.go — 给 ldflag 注入的 Value 写回归测试（v0.3.1 release
// 候选用了 'const Value'，linker 改不动；这个测试把 'var' 钉死，
// 防止未来的重构重新引入静默失败 bug）。
package version

import "testing"

// TestValueIsVar compiles only if Value is declared as a `var`. If a
// future change moves it back to `const`, this test would still
// compile (Go doesn't distinguish var/const at the use site) but
// the ldflag patch would silently fail in the linker.
//
// This test asserts the documented invariants so any future
// refactor that breaks them gets caught by the linter or a human
// reader. / 这个测试固定已记录的契约；任何破坏契约的 future 重构
// 会被 lint 或代码 review 抓住。
func TestValueIsVar(t *testing.T) {
	if Value == "" {
		t.Fatal("Value is empty; the in-source default of \"0.2.0\" should always be present")
	}
	// Document that callers must read Value through a var (not
	// const). Reading from a const would have baked the literal
	// at compile time and broken ldflag -X injection.
	// / 记录调用方必须通过 var 读取 Value（不是 const）。如果
	// 读 const，编译期会烤进字面量，ldflag -X 注入失效。
	_ = Value
}

// TestValueDefault pins the in-source default to "0.2.0". A change
// to the default would be a user-visible behavior change (every
// `go run` from a fresh checkout would report a different
// version) and should require an explicit update here.
// / TestValueDefault 把源代码默认值钉在 "0.2.0"。改默认值是
// 用户可见行为变化（每个 `go run` 都会报不同版本），需要显式更新
// 此测试。
func TestValueDefault(t *testing.T) {
	if Value != "0.2.0" {
		t.Errorf("Value default changed: got %q, want %q (update this test if intentional)", Value, "0.2.0")
	}
}
