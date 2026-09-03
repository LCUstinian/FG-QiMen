// multishort_test.go — tests for the multi-char short flag rewrite
// hook. Verifies the table in cmd/multishort.go is correct and the
// rewrite handles every shape we expect (with/without =, long form
// pass-through, unknown shorts pass-through, position args untouched).
// / multishort_test.go — 多字母短参重写 hook 测试。验证重写表正确
// 且覆盖所有形态（带/不带 =、长形式透传、未知短参透传、位置参数不
// 动）。
package cmd

import (
	"reflect"
	"testing"
)

// TestExpandMultiCharShorts_KnownAliases: each entry in the
// multiCharShorts map rewrites both -X value and -X=value forms.
// / 已知别名的 -X value 和 -X=value 两种形式都重写。
func TestExpandMultiCharShorts_KnownAliases(t *testing.T) {
	cases := []struct {
		in   []string
		want []string
	}{
		// -ot value
		{[]string{"scan", "-ot", "result.txt"}, []string{"scan", "--output-txt", "result.txt"}},
		// -ot=value
		{[]string{"scan", "-ot=result.txt"}, []string{"scan", "--output-txt=result.txt"}},
		// -oj
		{[]string{"scan", "-oj", "r.json"}, []string{"scan", "--output-json", "r.json"}},
		{[]string{"scan", "-oj=r.json"}, []string{"scan", "--output-json=r.json"}},
		// -oc
		{[]string{"scan", "-oc", "r.csv"}, []string{"scan", "--output-csv", "r.csv"}},
		{[]string{"scan", "-oc=r.csv"}, []string{"scan", "--output-csv=r.csv"}},
		// -uf
		{[]string{"scan", "-uf", "users.txt"}, []string{"scan", "--user-file", "users.txt"}},
		{[]string{"scan", "-uf=users.txt"}, []string{"scan", "--user-file=users.txt"}},
		// -pf
		{[]string{"scan", "-pf", "pass.txt"}, []string{"scan", "--pass-file", "pass.txt"}},
		{[]string{"scan", "-pf=pass.txt"}, []string{"scan", "--pass-file=pass.txt"}},
	}
	for _, c := range cases {
		got := expandMultiCharShorts(c.in)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("expandMultiCharShorts(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}

// TestExpandMultiCharShorts_PassThrough: long-form --flags, single-
// char shorts, position args, -- separator — none are touched.
// / 长形式 --flag、单 -x、位置参数、-- 分隔符都不动。
func TestExpandMultiCharShorts_PassThrough(t *testing.T) {
	in := []string{
		"scan",
		"-H", "1.0.0.0/8", // single-char short, untouched
		"-h",                // also untouched (not in our map)
		"--project", "corp", // long-form, untouched
		"--output-txt", "r.txt", // long-form already, untouched
		"-u", "admin", // single-char short, untouched
		"-p", "root", // single-char short, untouched
		"positional",        // position arg, untouched
		"--",                // end-of-flags marker, untouched
		"--after-dash", "x", // after --, untouched
	}
	want := []string{
		"scan",
		"-H", "1.0.0.0/8",
		"-h",
		"--project", "corp",
		"--output-txt", "r.txt",
		"-u", "admin",
		"-p", "root",
		"positional",
		"--",
		"--after-dash", "x",
	}
	got := expandMultiCharShorts(in)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("pass-through:\n  got:  %v\n  want: %v", got, want)
	}
}

// TestExpandMultiCharShorts_UnknownShorts: args starting with - but
// not in our map pass through verbatim, so cobra/pflag can emit its
// own "unknown flag" error later. We never strip the dash.
// / 不在映射里的 -X arg 原样透传，让 cobra/pflag 后面报错。我们不
// 剥短横。
func TestExpandMultiCharShorts_UnknownShorts(t *testing.T) {
	in := []string{"scan", "-xy", "value", "--bad=foo"}
	want := []string{"scan", "-xy", "value", "--bad=foo"}
	got := expandMultiCharShorts(in)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("unknown short:\n  got:  %v\n  want: %v", got, want)
	}
}

// TestExpandMultiCharShorts_Combined: a real-world invocation mixing
// 2-char shorts, 1-char shorts, long form, and value with =.
// / 综合：实际命令行混用 2 字母 + 1 字母 + 长形式 + 带等号。
func TestExpandMultiCharShorts_Combined(t *testing.T) {
	in := []string{
		"scan",
		"-H", "1.0.0.0/8",
		"-u", "admin",
		"-p", "root,toor",
		"-uf", "users.txt",
		"-pf", "passes.txt",
		"-ot", "r.txt",
		"-oj", "r.json",
		"-oc", "r.csv",
	}
	want := []string{
		"scan",
		"-H", "1.0.0.0/8",
		"-u", "admin",
		"-p", "root,toor",
		"--user-file", "users.txt",
		"--pass-file", "passes.txt",
		"--output-txt", "r.txt",
		"--output-json", "r.json",
		"--output-csv", "r.csv",
	}
	got := expandMultiCharShorts(in)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("combined:\n  got:  %v\n  want: %v", got, want)
	}
}

// TestExpandMultiCharShorts_KnownLimitations documents the two
// edge cases where the rewrite mis-fires. These are by design —
// fixing them would require knowing which positions expect flag
// values (which means re-implementing cobra's flag spec parser,
// not worth the complexity). The workarounds are documented in
// the CHANGELOG.
//
// TestExpandMultiCharShorts_KnownLimitations 钉住两个 hook 会误
// 改的边界场景。修复需要知道哪些位置期望 flag 值（意味着重做
// cobra 的 flag spec 解析，不值）。workaround 在 CHANGELOG 里
// 说明。
func TestExpandMultiCharShorts_KnownLimitations(t *testing.T) {
	// Edge A (REGRESSION test for the flag-value heuristic):
	// a flag's value happens to start with a registered
	// multi-short. Example: --pass "-ot" (literal password
	// "-ot"). The hook used to rewrite -ot → --output-txt,
	// breaking the value; the heuristic now detects that the
	// previous arg is flag-shaped (--pass) and skips the
	// rewrite. / 边界A：flag 的值以注册的 multi-short 开头
	// （例：--pass "-ot"）。hook 之前会重写 -ot → --output-txt
	// 破坏值；现在启发式看到上一个是 --pass flag 形态就跳过
	// 重写。
	t.Run("flag value starts with multi-short (now handled)", func(t *testing.T) {
		in := []string{"scan", "--pass", "-ot"}
		want := []string{"scan", "--pass", "-ot"}
		got := expandMultiCharShorts(in)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("edge A regression:\n  got:  %v\n  want: %v", got, want)
		}
	})

	// Edge A with -p short: --pass short takes a value, so "-ot"
	// after it must be the password literal, not a separate flag.
	t.Run("value after -p short", func(t *testing.T) {
		in := []string{"scan", "-p", "-ot"}
		want := []string{"scan", "-p", "-ot"}
		got := expandMultiCharShorts(in)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("-p short value:\n  got:  %v\n  want: %v", got, want)
		}
	})

	// Edge B: positional argument after `--` separator. The hook
	// sees `--` as flag-shaped, so the next arg is treated as a
	// value (not rewritten). This is the correct semantic
	// whether the user passes a literal file name or a flag-
	// shaped positional.
	t.Run("positional after --", func(t *testing.T) {
		in := []string{"scan", "--", "-uf.bak", "-ot-extra"}
		want := []string{"scan", "--", "-uf.bak", "-ot-extra"}
		got := expandMultiCharShorts(in)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("after --:\n  got:  %v\n  want: %v", got, want)
		}
	})

	// Sanity: a legitimate -ot after a non-flag arg still
	// rewrites. This proves the heuristic doesn't break the
	// common case.
	t.Run("legitimate -ot after non-flag arg", func(t *testing.T) {
		in := []string{"scan", "-H", "1.0.0.0/8", "-ot", "r.txt"}
		want := []string{"scan", "-H", "1.0.0.0/8", "--output-txt", "r.txt"}
		got := expandMultiCharShorts(in)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("legitimate:\n  got:  %v\n  want: %v", got, want)
		}
	})
}
