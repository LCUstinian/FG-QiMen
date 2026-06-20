// fuzz_test.go — fuzz targets for the host expansion / port parsing
// pipeline. Run via `go test -fuzz=FuzzExpandHosts -fuzztime=30s`.
//
// fuzz_test.go — host 展开 / 端口解析管线的模糊目标。通过
// `go test -fuzz=FuzzExpandHosts -fuzztime=30s` 运行。
package types

import "testing"

// FuzzParsePorts drives Config.ParsePorts with arbitrary input.
// The function MUST NOT panic on any input; any error (parse
// failure, out-of-range port) is acceptable. / FuzzParsePorts 用
// 任意输入驱动 Config.ParsePorts。函数对任何输入都不能 panic；任
// 何错误（解析失败、超范围端口）都可接受。
func FuzzParsePorts(f *testing.F) {
	f.Add("80,443,8080")
	f.Add("22-25")
	f.Add("invalid")
	f.Add("")
	f.Add("99999")
	f.Add("80,80,80,80")
	f.Add("common")
	f.Add("web,db,1-100")

	f.Fuzz(func(t *testing.T, spec string) {
		if len(spec) > 200 {
			spec = spec[:200]
		}
		c := &Config{Ports: spec}
		_, _ = c.ParsePorts()
	})
}

// FuzzExpandTargets drives ExpandTargets with arbitrary input. The
// function MUST NOT panic on any input (even garbage); any error
// is acceptable. / FuzzExpandTargets 用任意输入驱动 ExpandTargets。
// 函数对任何输入都不能 panic（即使是垃圾）；任何错误都可接受。
func FuzzExpandTargets(f *testing.F) {
	// Seed corpus: a few representative inputs the v0.2 audit ran
	// into. / 种子语料：v0.2 审计碰到的代表性输入。
	f.Add("192.168.1.0/24")
	f.Add("10.0.0.1")
	f.Add("10.0.0.1-10.0.0.5")
	f.Add("10.0.0.1,10.0.0.2,10.0.0.3")
	f.Add("")
	f.Add("not-a-target")
	f.Add("::1")
	f.Add("0.0.0.0/0")

	f.Fuzz(func(t *testing.T, spec string) {
		// Cap the spec to avoid the MaxTargets error path dominating
		// the corpus. / 限制 spec 长度，避免 MaxTargets 错误路径占据
		// 语料。
		if len(spec) > 200 {
			spec = spec[:200]
		}
		// We don't check the output, just that ExpandTargets doesn't
		// panic and returns within a sane time. / 不检查输出，只看
		// ExpandTargets 不 panic 且在合理时间内返回。
		_, _ = ExpandTargets(spec, "")
	})
}
