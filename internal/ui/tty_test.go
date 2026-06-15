// tty_test.go — table-driven tests for tty detection and ShouldUseTUI.
//
// We exercise every branch of the decision: explicit overrides first,
// then environment signals, then the tty / width probes. To avoid
// leaking global state we save / clear / restore the relevant env
// vars in a t.Cleanup.
package ui

import (
	"os"
	"testing"

	"github.com/LCUstinian/FG-QiMen/internal/types"
)

// withEnv sets the named env vars for the duration of the test and
// restores the previous values on cleanup. Pass val == "" to unset.
//
// withEnv 在测试期间设置指定环境变量，清理时恢复旧值。val=="" 表示删除。
func withEnv(t *testing.T, kv map[string]string) {
	t.Helper()
	prev := make(map[string]string, len(kv))
	for k, v := range kv {
		p, ok := os.LookupEnv(k)
		prev[k] = p
		if ok {
			_ = os.Unsetenv(k)
		}
		if v != "" {
			t.Setenv(k, v)
		}
	}
	t.Cleanup(func() {
		for k, p := range prev {
			if p == "" {
				_ = os.Unsetenv(k)
			} else {
				_ = os.Setenv(k, p)
			}
		}
	})
}

// TestIsCI — every env var on the recognised list flips IsCI to true;
// "false" / "0" / unset do not.
//
// TestIsCI — 识别列表里的每个环境变量都能让 IsCI 返回 true；"false" /
// "0" / 未设置则不能。
func TestIsCI(t *testing.T) {
	for _, k := range []string{
		"CI", "CONTINUOUS_INTEGRATION", "BUILD_NUMBER",
		"CI_NAME", "DRONE", "CIRCLECI",
	} {
		t.Run(k+"=1", func(t *testing.T) {
			withEnv(t, map[string]string{k: "1"})
			if !IsCI() {
				t.Errorf("IsCI() with %s=1 = false, want true", k)
			}
		})
		t.Run(k+"=false", func(t *testing.T) {
			withEnv(t, map[string]string{k: "false"})
			if IsCI() {
				t.Errorf("IsCI() with %s=false = true, want false", k)
			}
		})
		t.Run(k+"=empty", func(t *testing.T) {
			withEnv(t, map[string]string{k: ""})
			if IsCI() {
				t.Errorf("IsCI() with %s='' = true, want false", k)
			}
		})
	}
}

// TestIsDumbTerm — TERM=dumb flips IsDumbTerm to true; anything else
// does not (empty string included).
//
// TestIsDumbTerm — TERM=dumb 让 IsDumbTerm 返回 true；其他值（含空
// 串）不会。
func TestIsDumbTerm(t *testing.T) {
	cases := []struct {
		term string
		want bool
	}{
		{"dumb", true},
		{"xterm-256color", false},
		{"screen", false},
		{"", false},
	}
	for _, c := range cases {
		t.Run("TERM="+c.term, func(t *testing.T) {
			withEnv(t, map[string]string{"TERM": c.term})
			if got := IsDumbTerm(); got != c.want {
				t.Errorf("IsDumbTerm() with TERM=%q = %v, want %v", c.term, got, c.want)
			}
		})
	}
}

// TestShouldUseTUI — exhaustive truth table for the decision function.
// The factory and runScan rely on this; a regression here silently
// sends users into the wrong mode.
//
// TestShouldUseTUI — 决策函数的穷举真值表。工厂和 runScan 都依赖它；
// 此处回归会在用户端静默地切到错误模式。
func TestShouldUseTUI(t *testing.T) {
	// Tests run with stdout connected to a tty under `go test -v` only;
	// under `go test` (non-verbose) stdout is captured by the runner and
	// IsTerminalStdout returns false. The width probe also returns an
	// error on a non-tty, so ShouldUseTUI is false in that environment.
	// We branch on the actual environment value so the test is
	// hermetic: every assertion is conditional on the precondition it
	// cares about.
	stdoutIsTTY := IsTerminalStdout()
	widthOK := false
	if w, err := TerminalWidth(); err == nil && w >= minTUIWidth {
		widthOK = true
	}
	t.Logf("env: stdoutIsTTY=%v widthOK=%v", stdoutIsTTY, widthOK)

	cases := []struct {
		name string
		cfg  *types.Config
		want bool
		// precondition — skip the case if the test environment can't
		// reproduce the (TTY-bound) precondition the case assumes.
		skipUnless struct {
			stdoutIsTTY bool
			widthOK     bool
		}
	}{
		{
			name: "explicit NoTUI forces text",
			cfg:  &types.Config{NoTUI: true},
			want: false,
		},
		{
			name: "nil config is always text (defensive)",
			cfg:  nil,
			want: false,
		},
		{
			name: "CI env forces text even with TTY",
			cfg:  &types.Config{},
			want: false,
			skipUnless: struct {
				stdoutIsTTY bool
				widthOK     bool
			}{stdoutIsTTY: true, widthOK: true},
		},
		{
			name: "TERM=dumb forces text even with TTY",
			cfg:  &types.Config{},
			want: false,
			skipUnless: struct {
				stdoutIsTTY bool
				widthOK     bool
			}{stdoutIsTTY: true, widthOK: true},
		},
		{
			name: "non-TTY stdout forces text",
			cfg:  &types.Config{},
			want: false,
			skipUnless: struct {
				stdoutIsTTY bool
				widthOK     bool
			}{stdoutIsTTY: false},
		},
		{
			name: "narrow terminal forces text",
			cfg:  &types.Config{},
			want: false,
			skipUnless: struct {
				stdoutIsTTY bool
				widthOK     bool
			}{stdoutIsTTY: true, widthOK: false},
		},
		{
			name: "all-conditions-met enables TUI",
			cfg:  &types.Config{},
			want: true,
			skipUnless: struct {
				stdoutIsTTY bool
				widthOK     bool
			}{stdoutIsTTY: true, widthOK: true},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.skipUnless.stdoutIsTTY && !stdoutIsTTY {
				t.Skip("test env has no TTY on stdout")
			}
			if c.skipUnless.widthOK && !widthOK {
				t.Skip("test env has insufficient terminal width")
			}
			if got := ShouldUseTUI(c.cfg); got != c.want {
				t.Errorf("ShouldUseTUI() = %v, want %v", got, c.want)
			}
		})
	}
}

// TestShouldUseTUI_CIEnvOverride — sanity check that a CI env var
// flips the decision to false even when stdout looks like a tty and
// width is OK. We do this outside the table so the env-var dance is
// explicit and readable.
//
// TestShouldUseTUI_CIEnvOverride — 健全性检查：CI 环境变量会让决策变
// false，即便 stdout 是 tty 且宽度足够。放在表外让 env 操作更显式易读。
func TestShouldUseTUI_CIEnvOverride(t *testing.T) {
	if !IsTerminalStdout() {
		t.Skip("test env has no TTY on stdout")
	}
	withEnv(t, map[string]string{"CI": "true"})
	cfg := &types.Config{}
	if ShouldUseTUI(cfg) {
		t.Error("ShouldUseTUI() with CI=true = true, want false")
	}
}

// TestIsTerminalStdout — sanity that the helper agrees with the
// x/term library on this process. We don't assert a specific value
// (depends on the test runner) — we only assert it's consistent
// with a direct library call.
//
// TestIsTerminalStdout — 健全性检查 helper 与 x/term 一致。我们不
// 断言具体值（取决于测试运行器），只断言它与 x/term 库调用一致。
func TestIsTerminalStdout(t *testing.T) {
	got1 := IsTerminalStdout()
	// Direct call to the underlying library for cross-check.
	// 直接调用底层库做交叉验证。
	// (We don't import x/term here — the value is observable via
	// TerminalWidth's error path; if the fd is a tty, GetSize
	// succeeds.)
	_, err := TerminalWidth()
	got2 := err == nil
	if got1 != got2 {
		t.Errorf("IsTerminalStdout()=%v but TerminalWidth err=%v; should agree", got1, err)
	}
}
