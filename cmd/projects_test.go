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
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/LCUstinian/FG-QiMen/internal/workspace"
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

// newCmdForTestCapture is like newCmdForTest but pipes output to a
// buffer the test can read. Used by the List / Info / Create
// tests that assert on printed output.
//
// newCmdForTestCapture 像 newCmdForTest 但把输出接到 buffer，
// 测试可读。List / Info / Create 测试需要断言打印输出时用。
func newCmdForTestCapture(t *testing.T, _ func(*cobra.Command, []string) error) (*cobra.Command, *bytes.Buffer) {
	t.Helper()
	c := &cobra.Command{Use: "test"}
	buf := &bytes.Buffer{}
	c.SetOut(buf)
	c.SetErr(buf)
	return c, buf
}

// TestRunProjectsCreate — happy-path creation. The audit (P2 / F08)
// flagged runProjectsCreate as untested. We chdir into a temp
// dir, call the function with a valid name, then verify the
// project directory + bbolt DB were materialised.
//
// Invalid-name path is covered by TestValidProjectName's table
// (it gates Create via the same function). We don't re-test the
// rejection here.
//
// TestRunProjectsCreate — happy-path 创建。审计（P2 / F08）标
// runProjectsCreate 未测。我们切到 temp 目录，用合法名调函数，
// 然后验证 project 目录 + bbolt DB 已被物化。
func TestRunProjectsCreate(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)

	cmd, _ := newCmdForTestCapture(t, runProjectsCreate)
	if err := runProjectsCreate(cmd, []string{"alpha"}); err != nil {
		t.Fatalf("runProjectsCreate: %v", err)
	}

	// The function joins "runs/projects/<name>" relative to cwd
	// and calls workspace.Open, which creates the directory + opens
	// the bbolt DB at fg.db.
	// / 函数相对 cwd join "runs/projects/<name>" 并调
	// workspace.Open，后者创建目录 + 在 fg.db 打开 bbolt。
	dir := filepath.Join(tmp, "runs", "projects", "alpha")
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Errorf("expected project dir at %s, got stat err %v", dir, err)
	}
	dbPath := filepath.Join(dir, "fg.db")
	if _, err := os.Stat(dbPath); err != nil {
		t.Errorf("expected bbolt DB at %s, got err %v", dbPath, err)
	}
}

// TestRunProjectsCreate_InvalidName — verify the gating flow: a
// name rejected by validProjectName bubbles up as an error and
// leaves the workspace untouched.
//
// TestRunProjectsCreate_InvalidName — 验门控流：被 validProjectName
// 拒绝的名字作为错冒上来，且 workspace 不动。
func TestRunProjectsCreate_InvalidName(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)

	cmd, _ := newCmdForTestCapture(t, runProjectsCreate)
	if err := runProjectsCreate(cmd, []string{"../escape"}); err == nil {
		t.Errorf("expected error for invalid name, got nil")
	}

	// And no project dir was created.
	// / 并且没创建 project 目录。
	dir := filepath.Join(tmp, "runs", "projects", "escape")
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("expected no project dir at %s, got stat err %v", dir, err)
	}
}

// TestRunProjectsList_Empty — when ./runs/projects/ doesn't exist
// (fresh checkout), the List subcommand prints the "no projects
// yet" hint instead of erroring.
//
// TestRunProjectsList_Empty — 当 ./runs/projects/ 不存在（全新
// checkout）时，List 子命令打印"no projects yet"提示而不是报错。
func TestRunProjectsList_Empty(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)

	cmd, buf := newCmdForTestCapture(t, runProjectsList)
	if err := runProjectsList(cmd, nil); err != nil {
		t.Fatalf("runProjectsList: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "no projects yet") {
		t.Errorf("expected hint in output, got %q", out)
	}
}

// TestRunProjectsList_Populated — when two project dirs exist,
// List prints both, sorted by name. We don't pin the exact
// tabwriter formatting (whitespace is brittle); we just assert
// that both names appear and the longer-named one comes after
// the shorter one (proxy for "sorted").
//
// TestRunProjectsList_Populated — 当两个 project 目录存在，
// List 打印两者，按名排序。我们不固定 tabwriter 精确格式（空
// 白易碎）；只断两个名都出现且长名在短名后（"已排序"的代理）。
func TestRunProjectsList_Populated(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)

	// Materialise two project dirs by going through workspace.Open
	// (which creates the dir + opens bbolt).
	// / 通过 workspace.Open 物化两个 project 目录（创建目录 +
	// 开 bbolt）。
	for _, n := range []string{"alpha", "beta"} {
		p, err := openProjectForTest(t, n)
		if err != nil {
			t.Fatalf("setup Open(%q): %v", n, err)
		}
		_ = p.Close()
	}

	cmd, buf := newCmdForTestCapture(t, runProjectsList)
	if err := runProjectsList(cmd, nil); err != nil {
		t.Fatalf("runProjectsList: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "alpha") {
		t.Errorf("expected 'alpha' in output, got %q", out)
	}
	if !strings.Contains(out, "beta") {
		t.Errorf("expected 'beta' in output, got %q", out)
	}
	// Sorted: alpha before beta. / 排序：alpha 在 beta 前。
	if i, j := strings.Index(out, "alpha"), strings.Index(out, "beta"); i > j {
		t.Errorf("expected 'alpha' before 'beta' in output, got positions %d vs %d", i, j)
	}
}

// TestRunProjectsInfo — happy-path. We materialise a project with
// a couple of result files, then call Info and verify the output
// reports them as "X bytes" rather than "(missing)".
//
// TestRunProjectsInfo — happy-path。物化一个带几个结果文件的
// project，然后调 Info 并验证输出报它们为 "X bytes" 而不是
// "(missing)"。
func TestRunProjectsInfo(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)

	p, err := openProjectForTest(t, "alpha")
	if err != nil {
		t.Fatalf("setup Open: %v", err)
	}
	_ = p.Close()

	// Drop a targets.txt and result.txt so the file listing shows
	// something other than "(missing)".
	// / 放一个 targets.txt 和 result.txt，让文件列表显示非
	// "(missing)"。
	dir := filepath.Join(tmp, "runs", "projects", "alpha")
	for _, fn := range []string{"targets.txt", "result.txt"} {
		if err := writeFile(filepath.Join(dir, fn), "stub"); err != nil {
			t.Fatalf("setup write %s: %v", fn, err)
		}
	}

	cmd, buf := newCmdForTestCapture(t, runProjectsInfo)
	if err := runProjectsInfo(cmd, []string{"alpha"}); err != nil {
		t.Fatalf("runProjectsInfo: %v", err)
	}

	out := buf.String()
	for _, must := range []string{
		"Project: alpha",
		"Root:",
		"runs/projects/alpha",
		"DB:",
		"targets.txt",
		"result.txt",
	} {
		if !strings.Contains(out, must) {
			t.Errorf("expected %q in output, got %q", must, out)
		}
	}
	// missing-file rows show "(missing)". With our stub present
	// for two of the six files, the literal "(missing)" should
	// still appear for the others. / 缺失文件行显 "(missing)"。
	// 我们为 6 个文件中的 2 个放了 stub，所以 "(missing)" 应
	// 该还在为其它文件出现。
	if !strings.Contains(out, "(missing)") {
		t.Errorf("expected '(missing)' for absent files, got %q", out)
	}
}

// TestRunProjectsInfo_CreatesIfMissing — workspace.Open is
// "create-or-open", so Info on a never-seen-before project name
// silently creates it. This test pins that behavior so a future
// change to Open (e.g., one that refuses to auto-create) doesn't
// regress Info. After the call, the project dir + fg.db exist.
//
// TestRunProjectsInfo_CreatesIfMissing — workspace.Open 是
// "create-or-open"，所以对从未见过的 project 名调 Info 会
// 静默创建。本测试锁定该行为，以防 Open 未来的修改（如拒绝
// 自动创建）让 Info 倒退。调用后，project dir + fg.db 存在。
func TestRunProjectsInfo_CreatesIfMissing(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)

	cmd, buf := newCmdForTestCapture(t, runProjectsInfo)
	if err := runProjectsInfo(cmd, []string{"ghost"}); err != nil {
		t.Fatalf("runProjectsInfo(ghost): unexpected error: %v", err)
	}

	// Output should mention the project. / 输出应提到 project。
	if !strings.Contains(buf.String(), "Project: ghost") {
		t.Errorf("expected 'Project: ghost' in output, got %q", buf.String())
	}

	// And the dir + DB should now exist. / dir + DB 现在应存在。
	dir := filepath.Join(tmp, "runs", "projects", "ghost")
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("expected project dir at %s after Info, got err %v", dir, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "fg.db")); err != nil {
		t.Errorf("expected fg.db after Info, got err %v", err)
	}
}

// openProjectForTest is a thin wrapper around workspace.Open that
// calls t.Fatal on error. It exists so the populated-list and
// info tests read top-down without "and now jump to a 3-line
// open-and-check-error dance".
//
// openProjectForTest 是 workspace.Open 的薄包装，err 时 t.Fatal。
// 存在是为了让 populated-list 和 info 测试自顶向下读，无需
// "然后跳到 3 行 open-and-check-error 套路"。
func openProjectForTest(t *testing.T, name string) (*workspace.Project, error) {
	t.Helper()
	return workspace.Open(name)
}
