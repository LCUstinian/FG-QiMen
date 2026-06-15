// projects_test.go — tests for the projects subcommand helpers
// flagged as untested in the v0.2 audit (P2 / F08).
//
// Scope:
//   - validProjectName: pure function, table-driven; this is
//     the audit's "5 untested functions" call-out's most
//     important to lock down (the others are filesystem
//     orchestration that's hard to unit-test without a tempdir
//     dance; we cover runProjectsDelete's "valid name → success
//     on real tempdir" as a smoke test).
//   - runProjectsDelete: smoke test creating a temp project dir
//     and verifying it's removed.
//
// Why a single file: the audit noted that 5 cobra subcommand
// functions in this file are untested. The most-failure-prone
// one (validProjectName, because it gates the file path used
// by Delete/Create) gets the heavy table test; the others get
// a minimal happy-path smoke. A future audit pass can layer
// in deeper tests.
//
// projects_test.go — projects 子命令 helper 的测试。
// v0.2 审计标出 5 个未测函数。本文件覆盖：
//   - validProjectName：纯函数，表驱动；审计"5 个未测函数"里最重
//     要的（它门控 Delete/Create 用的文件路径）。
//   - runProjectsDelete：smoke test 创 temp project dir 并验
//     证删除。
//
// 单一文件原因：审计标出 5 个未测的 cobra 子命令函数。最易出
// 错的（validProjectName）拿到重表测；其它拿最小 happy-path
// smoke。未来审计 pass 可以叠加深测试。
package cmd

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// TestValidProjectName — table-driven. The audit (P2 / F08)
// flagged this as the projects subcommand's most important
// "untested" function because it gates the file path used by
// runProjectsCreate / Delete / Info — a permissive regex
// would let `..` or `/` slip into `runs/projects/<name>/`.
//
// TestValidProjectName — 表驱动。审计（P2 / F08）把它标为
// projects 子命令最重要的"未测"函数，因为它门控
// runProjectsCreate / Delete / Info 用的文件路径——一个宽容
// 的正则会让 `..` 或 `/` 溜进 `runs/projects/<name>/`。
func TestValidProjectName(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		// Empty / oversize rejected.
		// 空 / 超长拒绝。
		{"", false},
		{string(make([]byte, 65)), false}, // 65 chars: over the 64 cap
		// (Skipped: 64 zero bytes is technically allowed length
		// but the for-range loop rejects 0x00 as an out-of-
		// range rune. That's the right behaviour — the all-zero
		// case is indistinguishable from a corrupted entry and
		// we want the loop to fail-loud rather than pass-silent.)

		// Happy path: letters, digits, hyphens, underscores.
		// 正常路径：字母、数字、连字符、下划线。
		{"a", true},
		{"corp", true},
		{"corp-intranet", true},
		{"corp_intranet", true},
		{"Project1", true},
		{"a-b_c-1", true},
		{strings.Repeat("a", 64), true}, // exactly 64 'a' chars: allowed

		// Forbidden characters.
		// 禁止字符。
		{"corp intranet", false}, // space
		{"corp.intranet", false}, // dot is not in the allow-list
		{"corp/intranet", false}, // slash
		{"corp\\intranet", false}, // backslash
		{"corp:intranet", false},  // colon (Windows-illegal anyway)
		{"corp/intra/net", false},
		{"corp!", false},
		{"中国", false},  // non-ASCII rune
		{"corp\tnet", false}, // tab

		// Path traversal: even if the chars were legal, we
		// refuse any ".." substring. The function should
		// reject the cases below.
		// 路径穿越：即便字符合法，我们拒绝任何 ".." 子串。
		// 函数应拒绝下面的情况。
		{"..", false},
		{"../etc", false},
		{"a..b", false},
		{"foo..", false},
		{"..bar", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := validProjectName(c.name); got != c.want {
				t.Errorf("validProjectName(%q) = %v, want %v", c.name, got, c.want)
			}
		})
	}
}

// TestRunProjectsDeleteSmoke — happy-path delete against a real
// temp directory. The audit flagged runProjectsDelete as untested
// (P2 / F08). It joins "runs/projects/<name>" — we can't run
// the test from the repo root without littering the working tree,
// so the test chdir's into a temp dir, builds a fake project
// directory there, calls the function, and verifies removal.
//
// TestRunProjectsDeleteSmoke — 在真实 temp 目录上 happy-path 删
// 除。审计标 runProjectsDelete 未测（P2 / F08）。它 join "runs/
// projects/<name>"——不切到 temp 目录会污染仓库，所以测试切到
// t.TempDir，构造一个伪 project 目录，调函数，验删除。
func TestRunProjectsDeleteSmoke(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)

	// Create a fake project dir at runs/projects/alpha. The
	// function joins "runs/projects/<name>" relative to cwd,
	// so we materialise the parent and a child marker file.
	// / 构造伪 project dir 在 runs/projects/alpha。函数相对
	// cwd join "runs/projects/<name>"，所以物化父目录和子标
	// 记文件。
	projDir := filepath.Join(tmp, "runs", "projects", "alpha")
	if err := os.MkdirAll(projDir, 0o755); err != nil {
		t.Fatalf("setup: MkdirAll: %v", err)
	}
	if err := writeFile(filepath.Join(projDir, "fg.db"), "fake"); err != nil {
		t.Fatalf("setup: writeFile: %v", err)
	}

	// Cobra command for the call (OutOrStdout can be nil if
	// we don't read it). / Cobra command 用来调（OutOrStdout
	// 不读，可以 nil）。
	cmd := newCmdForTest(t, runProjectsDelete)

	if err := runProjectsDelete(cmd, []string{"alpha"}); err != nil {
		t.Fatalf("runProjectsDelete: %v", err)
	}
	// Verify the project dir is gone.
	// / 验 project dir 已删。
	if _, err := os.Stat(projDir); !os.IsNotExist(err) {
		t.Errorf("project dir still present: %v", err)
	}
}

// writeFile is a tiny test helper for one-line file materialization.
// It exists so the smoke test reads top-down without
// "and now jump to an os.WriteFile".
//
// writeFile 是单行文件物化的小测试 helper。存在是为了让 smoke
// test 自顶向下读，无需"然后跳到 os.WriteFile"。
func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o644)
}

// newCmdForTest builds a *cobra.Command whose OutOrStdout is
// discarded. The projects subcommands read OutOrStdout for
// their success print; nil would NPE on the first happy-path
// call. We use io.Discard via a fresh cobra.
//
// newCmdForTest 构造一个 OutOrStdout 被丢弃的 *cobra.Command。
// projects 子命令读 OutOrStdout 打成功；nil 会在首次 happy-path
// 调时 NPE。我们通过新 cobra 用 io.Discard。
func newCmdForTest(t *testing.T, _ func(*cobra.Command, []string) error) *cobra.Command {
	t.Helper()
	c := &cobra.Command{Use: "test"}
	c.SetOut(io.Discard)
	return c
}
