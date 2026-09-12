// projects subcommand / projects 子命令
//
// Manages project workspaces on disk. Does NOT enter the scan pipeline.
// All output is English.
//
// 管理磁盘上的项目工作区。不会进入扫描管线。所有输出为英文。
package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/LCUstinian/FG-QiMen/internal/workspace"
)

var projectsCmd = &cobra.Command{
	Use:   "projects",
	Short: "Manage project workspaces",
	Long:  "List, create, delete, or inspect project workspaces under ./fgqm_workspace/projects/.",
}

var (
	projectsListCmd = &cobra.Command{
		Use:   "list",
		Short: "List all project workspaces",
		RunE:  runProjectsList,
	}
	projectsCreateCmd = &cobra.Command{
		Use:   "create <name>",
		Short: "Create a new project workspace",
		Args:  cobra.ExactArgs(1),
		RunE:  runProjectsCreate,
	}
	projectsDeleteCmd = &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a project workspace and all its data",
		Args:  cobra.ExactArgs(1),
		RunE:  runProjectsDelete,
	}
	projectsInfoCmd = &cobra.Command{
		Use:   "info <name>",
		Short: "Show project workspace details",
		Args:  cobra.ExactArgs(1),
		RunE:  runProjectsInfo,
	}
	// v0.4 Phase 2.4: portable single-file project dump (.fgq
	// format). export a project to share with a teammate or
	// back up off-box; import to recreate the bbolt project
	// locally. / v0.4 Phase 2.4：可移植单文件项目转储（.fgq 格
	// 式）。导出项目以便与队友共享或异地备份；导入可在本地重建
	// bbolt 项目。
	projectsExportCmd = &cobra.Command{
		Use:   "export <name> <out.fgq>",
		Short: "Export a project workspace to a single .fgq file",
		Args:  cobra.ExactArgs(2),
		RunE:  runProjectsExport,
	}
	projectsImportCmd = &cobra.Command{
		Use:   "import <in.fgq> <name>",
		Short: "Import a project from a .fgq file",
		Args:  cobra.ExactArgs(2),
		RunE:  runProjectsImport,
	}
	// M-5 audit fix: retention for long-lived projects. Without it the
	// seen-hash set (targets bucket) grows unboundedly and -resume's
	// LoadSeenHashes slows linearly. / M-5 审计修复：长期项目的保留
	// 策略。没有它 seen-hash 集合（targets bucket）无限增长，-resume
	// 的 LoadSeenHashes 线性变慢。
	projectsPruneCmd = &cobra.Command{
		Use:   "prune <name> --before <date>",
		Short: "Delete seen-hash entries older than a cutoff date",
		Args:  cobra.ExactArgs(1),
		RunE:  runProjectsPrune,
	}
)

var (
	// pruneBefore is the retention cutoff: RFC3339 ("2026-09-01T00:00:00Z")
	// or a plain date ("2026-09-01", local midnight). Required — a prune
	// with no cutoff would mean "delete everything", and that's what
	// `projects delete` is for.
	// pruneBefore 是保留截止：RFC3339 或纯日期（本地时区零点）。必填
	// ——不带截止的 prune 等于"全删"，那是 `projects delete` 的职责。
	pruneBefore string
	// pruneCompact rewrites the DB file after pruning to actually shrink
	// it on disk. / prune 后重写 DB 文件，真正收缩磁盘占用。
	pruneCompact bool
	// pruneYes confirms the destructive step non-interactively (CI /
	// scripts). / 非交互确认破坏性步骤（CI / 脚本）。
	pruneYes bool
)

func init() {
	rootCmd.AddCommand(projectsCmd)
	projectsCmd.AddCommand(projectsListCmd)
	projectsCmd.AddCommand(projectsCreateCmd)
	projectsCmd.AddCommand(projectsDeleteCmd)
	projectsCmd.AddCommand(projectsInfoCmd)
	projectsCmd.AddCommand(projectsExportCmd)
	projectsCmd.AddCommand(projectsImportCmd)
	projectsPruneCmd.Flags().StringVar(&pruneBefore, "before", "", "cutoff date: RFC3339 or YYYY-MM-DD (entries seen before this are deleted)")
	projectsPruneCmd.Flags().BoolVar(&pruneCompact, "compact", false, "rewrite fgqm.db after pruning to reclaim disk space")
	projectsPruneCmd.Flags().BoolVar(&pruneYes, "yes", false, "skip the interactive confirmation (for scripts)")
	_ = projectsPruneCmd.MarkFlagRequired("before")
	projectsCmd.AddCommand(projectsPruneCmd)
}

// runProjectsList lists all projects under ./fgqm_workspace/projects/.
// runProjectsList 列出 ./fgqm_workspace/projects/ 下的所有项目。
func runProjectsList(cmd *cobra.Command, args []string) error {
	out := cmd.OutOrStdout()
	entries, err := os.ReadDir(filepath.Join("fgqm_workspace", "projects"))
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Fprintln(out, "(no projects yet — run `fg-qimen projects create <name>` to create one)")
			return nil
		}
		return err
	}
	if len(entries) == 0 {
		fmt.Fprintln(out, "(no projects yet)")
		return nil
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	tw := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tSTATE")
	for _, n := range names {
		proj, err := workspace.Open(n)
		if err != nil {
			fmt.Fprintf(tw, "%s\t<open error: %v>\n", n, err)
			continue
		}
		stats, _ := proj.Stats()
		state := "ok"
		if stats == "" {
			state = "ok"
		}
		fmt.Fprintf(tw, "%s\t%s\n", n, state)
		_ = proj.Close()
	}
	return tw.Flush()
}

// runProjectsCreate creates a new project workspace.
// runProjectsCreate 创建一个新的项目工作区。
func runProjectsCreate(cmd *cobra.Command, args []string) error {
	name := args[0]
	if !validProjectName(name) {
		return fmt.Errorf("invalid project name %q (allowed: letters, digits, dash, underscore)", name)
	}
	proj, err := workspace.Open(name)
	if err != nil {
		return err
	}
	defer proj.Close()
	fmt.Fprintf(cmd.OutOrStdout(), "[+] project created: fgqm_workspace/projects/%s\n", name)
	return nil
}

// runProjectsDelete removes a project workspace.
// runProjectsDelete 删除一个项目工作区。
func runProjectsDelete(cmd *cobra.Command, args []string) error {
	name := args[0]
	dir := filepath.Join("fgqm_workspace", "projects", name)
	if _, err := os.Stat(dir); err != nil {
		return fmt.Errorf("project %q does not exist", name)
	}
	// Hard delete (with confirmation prompt in interactive mode would be ideal,
	// but for v0.1 simplicity we just remove).
	// 硬删除（交互模式加确认更安全，但 v0.1 先简化直接删）。
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "[-] project deleted: fgqm_workspace/projects/%s\n", name)
	return nil
}

// runProjectsInfo shows details about a project workspace.
// runProjectsInfo 显示项目工作区详情。
func runProjectsInfo(cmd *cobra.Command, args []string) error {
	name := args[0]
	proj, err := workspace.Open(name)
	if err != nil {
		return err
	}
	defer proj.Close()

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Project: %s\n", name)
	fmt.Fprintf(out, "Root:    fgqm_workspace/projects/%s\n", name)
	fmt.Fprintf(out, "DB:      %s\n", proj.DBPath)
	stats, _ := proj.Stats()
	if stats != "" {
		fmt.Fprintln(out, "Stats:")
		fmt.Fprintln(out, stats)
	}
	// List output files / 列出输出文件
	fmt.Fprintln(out, "Files:")
	// targets.txt is unprefixed because it's a hand-editable target
	// list (operators expect to read/write it directly). Result /
	// creds / RDP files all carry the fgqm_ prefix so they stand
	// out in mixed directories and grep-friendly. / targets.txt 不
	// 加前缀因为它是手编目标列表（操作员预期直接读写）。结果 /
	// 凭据 / RDP 文件都带 fgqm_ 前缀，混合目录里显眼，便于 grep。
	for _, fname := range []string{"targets.txt", "fgqm_result.txt", "fgqm_result.json", "fgqm_creds.txt", "fgqm_rdp.json", "fgqm_rdp.txt"} {
		fpath := filepath.Join("fgqm_workspace", "projects", name, fname)
		if info, err := os.Stat(fpath); err == nil {
			fmt.Fprintf(out, "  %-15s  %d bytes\n", fname, info.Size())
		} else {
			fmt.Fprintf(out, "  %-15s  (missing)\n", fname)
		}
	}
	return nil
}

// validProjectName returns true if name is safe to use as a directory name.
// validProjectName 当 name 可安全用作目录名时返回 true。
func validProjectName(name string) bool {
	if name == "" || len(name) > 64 {
		return false
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '_':
		default:
			return false
		}
	}
	return !strings.Contains(name, "..")
}

// runProjectsExport writes the project's bbolt state to a
// single .fgq file. See internal/workspace/export.go for
// the file format. / runProjectsExport 把项目的 bbolt 状态
// 写到单 .fgq 文件。文件格式见 internal/workspace/export.go。
func runProjectsExport(cmd *cobra.Command, args []string) error {
	name := args[0]
	outPath := args[1]
	proj, err := workspace.Open(name)
	if err != nil {
		return fmt.Errorf("open project %q: %w", name, err)
	}
	defer func() { _ = proj.Close() }()
	if err := proj.Export(outPath); err != nil {
		return fmt.Errorf("export %q → %s: %w", name, outPath, err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Exported project %q to %s\n", name, outPath)
	return nil
}

// runProjectsImport recreates a project from a .fgq file. The
// project name is taken from the second positional argument
// (it can differ from the original project name in the file).
// / runProjectsImport 从 .fgq 文件重建项目。项目名取第二
// 个位置参数（可以与文件里记录的原项目名不同）。
func runProjectsImport(cmd *cobra.Command, args []string) error {
	inPath := args[0]
	name := args[1]
	// Refuse to silently overwrite an existing project. The
	// user can `delete` first if they want to replace. / 拒绝
	// 静默覆盖已有项目。如要替换，用户可以先 delete。
	dir := filepath.Join(workspace.ProjectsRoot(), name)
	if _, err := os.Stat(dir); err == nil {
		return fmt.Errorf("project %q already exists at %s; delete it first if you want to replace", name, dir)
	}
	if err := workspace.Import(inPath, name); err != nil {
		return fmt.Errorf("import %s → %q: %w", inPath, name, err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Imported %s as project %q\n", inPath, name)
	return nil
}

// parsePruneCutoff accepts either a plain date ("2026-09-01", local
// midnight) or RFC3339. The plain-date form is the common operator
// input, so it comes first. / parsePruneCutoff 接受纯日期（本地时
// 区零点）或 RFC3339。纯日期是操作员的常用输入，所以先试。
func parsePruneCutoff(s string) (time.Time, error) {
	if t, err := time.ParseInLocation("2006-01-02", s, time.Local); err == nil {
		return t, nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("--before must be YYYY-MM-DD or RFC3339 (e.g. 2026-09-01), got %q", s)
	}
	return t, nil
}

// runProjectsPrune deletes seen-hash entries older than --before.
// Destructive: it previews the count, then requires either --yes or
// an interactive terminal confirmation before touching anything.
// Results / creds buckets are never touched — they hold the
// operator's findings, not resume state.
//
// runProjectsPrune 删除早于 --before 的 seen-hash 条目。破坏性：
// 先预览条数，然后要求 --yes 或交互终端确认才动手。results /
// creds bucket 永不触碰——那是操作员的发现记录，不是 resume 状态。
func runProjectsPrune(cmd *cobra.Command, args []string) error {
	name := args[0]
	cutoff, err := parsePruneCutoff(pruneBefore)
	if err != nil {
		return err
	}
	proj, err := workspace.Open(name)
	if err != nil {
		return fmt.Errorf("open project %q: %w", name, err)
	}
	defer func() { _ = proj.Close() }()

	// Prune works on plaintext keys only — the seen-hash timestamps in
	// the targets bucket are never encrypted — so no project key is
	// needed here. / prune 只操作明文 key——targets bucket 的
	// seen-hash 时间戳从不加密——因此不需要项目密钥。
	st := proj.AsStore()
	if st == nil {
		return fmt.Errorf("project %q has no persistent state (created with --no-state?)", name)
	}
	out := cmd.OutOrStdout()
	n, err := st.CountSeenBefore(cutoff)
	if err != nil {
		return fmt.Errorf("count prunable entries: %w", err)
	}
	fmt.Fprintf(out, "Project %q: %d seen-hash entries older than %s\n", name, n, cutoff.Format("2006-01-02 15:04:05"))
	if n == 0 {
		fmt.Fprintln(out, "nothing to prune")
		return nil
	}
	// Confirmation gate: scripts must pass --yes; interactive callers
	// get a y/N prompt. A non-interactive stdin without --yes is a
	// likely mistake (CI without the flag), so refuse rather than
	// guess. / 确认门：脚本必须传 --yes；交互调用方拿到 y/N 提示。
	// 非交互 stdin 且无 --yes 多半是失误（CI 忘了 flag），拒绝而非
	// 猜测。
	if !pruneYes {
		if !stdinIsTerminal() {
			return fmt.Errorf("refusing to prune non-interactively without --yes (add --yes to confirm)")
		}
		fmt.Fprint(out, "delete these entries? [y/N] ")
		var answer string
		if _, err := fmt.Scanln(&answer); err != nil {
			answer = ""
		}
		answer = strings.ToLower(strings.TrimSpace(answer))
		if answer != "y" && answer != "yes" {
			fmt.Fprintln(out, "aborted")
			return nil
		}
	}
	deleted, err := st.PruneSeen(cutoff)
	if err != nil {
		return fmt.Errorf("prune: %w", err)
	}
	fmt.Fprintf(out, "[+] pruned %d seen-hash entries (results and creds untouched)\n", deleted)

	if !pruneCompact {
		return nil
	}
	dbPath := proj.DBPath
	sizeBefore := fileSize(dbPath)
	if err := proj.Compact(); err != nil {
		return fmt.Errorf("compact: %w", err)
	}
	fmt.Fprintf(out, "[+] compacted %s: %s → %s\n", dbPath, humanBytes(sizeBefore), humanBytes(fileSize(dbPath)))
	return nil
}

// stdinIsTerminal reports whether stdin is an interactive terminal.
// It is a variable so tests can stub the terminal check without
// wrestling with real OS handles. / stdinIsTerminal 报告 stdin 是否
// 为交互终端。定义为变量以便测试替换终端检测，不必纠缠真实 OS 句柄。
var stdinIsTerminal = func() bool {
	info, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// fileSize returns the size in bytes of path, or 0 when it cannot be
// stat'ed (the caller only uses it for a before/after printout).
// / fileSize 返回 path 的字节大小，stat 失败返回 0（调用方只用它
// 打印前后对比）。
func fileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}

// humanBytes formats a byte count with binary units. / humanBytes
// 用二进制单位格式化字节数。
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
