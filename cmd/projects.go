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

	"github.com/spf13/cobra"

	"github.com/LCUstinian/FG-QiMen/internal/workspace"
)

var projectsCmd = &cobra.Command{
	Use:   "projects",
	Short: "Manage project workspaces",
	Long:  "List, create, delete, or inspect project workspaces under ./runs/projects/.",
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
)

func init() {
	rootCmd.AddCommand(projectsCmd)
	projectsCmd.AddCommand(projectsListCmd)
	projectsCmd.AddCommand(projectsCreateCmd)
	projectsCmd.AddCommand(projectsDeleteCmd)
	projectsCmd.AddCommand(projectsInfoCmd)
	projectsCmd.AddCommand(projectsExportCmd)
	projectsCmd.AddCommand(projectsImportCmd)
}

// runProjectsList lists all projects under ./runs/projects/.
// runProjectsList 列出 ./runs/projects/ 下的所有项目。
func runProjectsList(cmd *cobra.Command, args []string) error {
	out := cmd.OutOrStdout()
	entries, err := os.ReadDir(filepath.Join("runs", "projects"))
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
	fmt.Fprintf(cmd.OutOrStdout(), "[+] project created: runs/projects/%s\n", name)
	return nil
}

// runProjectsDelete removes a project workspace.
// runProjectsDelete 删除一个项目工作区。
func runProjectsDelete(cmd *cobra.Command, args []string) error {
	name := args[0]
	dir := filepath.Join("runs", "projects", name)
	if _, err := os.Stat(dir); err != nil {
		return fmt.Errorf("project %q does not exist", name)
	}
	// Hard delete (with confirmation prompt in interactive mode would be ideal,
	// but for v0.1 simplicity we just remove).
	// 硬删除（交互模式加确认更安全，但 v0.1 先简化直接删）。
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "[-] project deleted: runs/projects/%s\n", name)
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
	fmt.Fprintf(out, "Root:    runs/projects/%s\n", name)
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
		fpath := filepath.Join("runs", "projects", name, fname)
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
