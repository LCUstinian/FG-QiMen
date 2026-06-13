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
	Long:  "List, create, delete, or inspect project workspaces under ./projects/.",
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
)

func init() {
	rootCmd.AddCommand(projectsCmd)
	projectsCmd.AddCommand(projectsListCmd)
	projectsCmd.AddCommand(projectsCreateCmd)
	projectsCmd.AddCommand(projectsDeleteCmd)
	projectsCmd.AddCommand(projectsInfoCmd)
}

// runProjectsList lists all projects under ./projects/.
// runProjectsList 列出 ./projects/ 下的所有项目。
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
	for _, fname := range []string{"targets.txt", "result.txt", "result.json", "creds.txt", "rdp.json", "rdp.txt"} {
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
