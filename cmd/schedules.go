// schedules.go — manage persistent scan schedules.
//
// v0.5: 'fg-qimen schedules add | list | remove'. The
// schedule itself is stored in the project DB (the `schedules`
// bucket, opened lazily by internal/scheduler/store.go).
// / v0.5：'fg-qimen schedules add | list | remove'。调度本
// 身存项目 DB（`schedules` bucket，internal/scheduler/store.go
// 懒打开）。
//
// Use case: an operator wants to register a recurring scan
// against a project without having to set up cron on the
// host itself. They can `schedules add --project corp --cron "0 9 *
// * *"` and the schedule lives in the project DB until
// explicitly removed.
// / 用例：操作员想在不依赖宿主机 cron 的情况下，给项目注册
// 循环扫描。`schedules add --project corp --cron "0 9 * * *"`，调度
// 存项目 DB 直到显式 remove。
package cmd

import (
	"fmt"
	"text/tabwriter"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/scheduler"
	"github.com/LCUstinian/FG-QiMen/internal/workspace"
	"github.com/spf13/cobra"
)

var schedulesCmd = &cobra.Command{
	Use:   "schedules",
	Short: "Manage persistent scan schedules (v0.5+)",
	Long: "Add, list, or remove scan schedules stored in the project's " +
		"bbolt DB. Use the --at / --in / --cron flags on the schedules " +
		"add subcommand the same way you would on the scan command.",
}

var (
	schedulesAddCmd = &cobra.Command{
		Use:   "add <name>",
		Short: "Add a schedule to the project",
		Args:  cobra.ExactArgs(1),
		RunE:  runSchedulesAdd,
	}
	schedulesListCmd = &cobra.Command{
		Use:   "list",
		Short: "List schedules in the project",
		RunE:  runSchedulesList,
	}
	schedulesRemoveCmd = &cobra.Command{
		Use:   "remove <name>",
		Short: "Remove a schedule by name",
		Args:  cobra.ExactArgs(1),
		RunE:  runSchedulesRemove,
	}
)

func init() {
	rootCmd.AddCommand(schedulesCmd)
	schedulesCmd.AddCommand(schedulesAddCmd)
	schedulesCmd.AddCommand(schedulesListCmd)
	schedulesCmd.AddCommand(schedulesRemoveCmd)
}

// runSchedulesAdd persists a new schedule. The schedule flags
// (--at / --in / --cron / --tz / --daemon) are shared with
// runScan. / runSchedulesAdd 持久化新调度。调度 flag
// (--at / --in / --cron / --tz / --daemon) 与 runScan 共享。
func runSchedulesAdd(cmd *cobra.Command, args []string) error {
	name := args[0]
	mode, value := detectScheduleMode()
	if mode == "" {
		return fmt.Errorf("schedules add: one of --at, --in, or --cron is required")
	}
	// Validate the cron expression up front so the user
	// doesn't get a half-saved record. / 预先校验 cron 表达式，
	// 避免半保存。
	if mode == "cron" {
		if _, err := scheduler.ParseCron(flagScheduleCron, loadScheduleTZ()); err != nil {
			return fmt.Errorf("schedules add: %w", err)
		}
	}
	proj, err := workspace.Open(flagProject)
	if err != nil {
		return fmt.Errorf("open project %q: %w", flagProject, err)
	}
	defer func() { _ = proj.Close() }()
	s := scheduler.NewStore(proj.DB)
	rec := scheduler.Record{
		Name:      name,
		Project:   flagProject,
		Mode:      mode,
		Value:     value,
		TZ:        flagScheduleTZ,
		Daemon:    flagScheduleDaemon,
		CreatedAt: time.Now(),
	}
	if err := s.Add(rec); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Added schedule %q (mode=%s) to project %q\n",
		name, mode, flagProject)
	return nil
}

// runSchedulesList prints all schedules in the project. /
// runSchedulesList 打印项目所有调度。
func runSchedulesList(cmd *cobra.Command, args []string) error {
	proj, err := workspace.Open(flagProject)
	if err != nil {
		return fmt.Errorf("open project %q: %w", flagProject, err)
	}
	defer func() { _ = proj.Close() }()
	s := scheduler.NewStore(proj.DB)
	all, err := s.List()
	if err != nil {
		return err
	}
	if len(all) == 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "(no schedules in project %q — run `fg-qimen schedules add --project %s --cron \"...\"` to add one)\n",
			flagProject, flagProject)
		return nil
	}
	tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tMODE\tVALUE\tTZ\tDAEMON\tCREATED")
	for _, r := range all {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%v\t%s\n",
			r.Name, r.Mode, r.Value, r.TZ, r.Daemon,
			r.CreatedAt.Format("2006-01-02 15:04"))
	}
	return tw.Flush()
}

// runSchedulesRemove deletes a schedule by name. Idempotent —
// returns nil whether the schedule existed or not. /
// runSchedulesRemove 按 name 删调度。幂等——无论存不存在都返
// nil。
func runSchedulesRemove(cmd *cobra.Command, args []string) error {
	name := args[0]
	proj, err := workspace.Open(flagProject)
	if err != nil {
		return fmt.Errorf("open project %q: %w", flagProject, err)
	}
	defer func() { _ = proj.Close() }()
	s := scheduler.NewStore(proj.DB)
	if err := s.Remove(name); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Removed schedule %q from project %q (if it existed)\n",
		name, flagProject)
	return nil
}

// detectScheduleMode returns ("at", opts.At), ("in", opts.In),
// ("cron", opts.Cron), or ("", ""). The flag set is
// intentionally shared with the scan command — running
// `fg-qimen schedules add --project corp --cron "0 9 * * *"` reads
// the same --cron flag as `fg-qimen scan --cron "0 9 * * *"`,
// and the resolution / validation in scheduler.Resolve is
// shared too. / detectScheduleMode 返 ("at", opts.At)、
// ("in", opts.In)、("cron", opts.Cron) 或 ("", "")。flag 集
// 有意与 scan 命令共享——`fg-qimen schedules add -p corp
// --cron "0 9 * * *"` 读同 --cron flag 如 `fg-qimen scan
// --cron "0 9 * *"`，解析 / 校验在 scheduler.Resolve 里共
// 享。
func detectScheduleMode() (string, string) {
	if flagScheduleAt != "" {
		return "at", flagScheduleAt
	}
	if flagScheduleIn != "" {
		return "in", flagScheduleIn
	}
	if flagScheduleCron != "" {
		return "cron", flagScheduleCron
	}
	return "", ""
}

// loadScheduleTZ returns the --tz IANA location, or time.Local
// if --tz is empty. The schedules subcommand uses this for
// validation only (the cron parser handles the TZ itself).
// / loadScheduleTZ 返 --tz IANA location，--tz 空时返
// time.Local。schedules 子命令只在验证时用这个（cron 解析
// 器自己处理 TZ）。
func loadScheduleTZ() *time.Location {
	if flagScheduleTZ == "" {
		return time.Local
	}
	loc, err := time.LoadLocation(flagScheduleTZ)
	if err != nil {
		return time.Local
	}
	return loc
}
