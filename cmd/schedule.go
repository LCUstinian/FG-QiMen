// schedule.go — pre-scan hook that resolves the --at / --in /
// --cron / --tz flags and either prints the next fire time (dry
// run) or blocks until it.
//
// schedule.go — 预扫描钩子，解析 --at / --in / --cron / --tz
// flag，然后要么打下次执行时间（干跑）要么阻塞等待。
//
// Behavior matrix:
//   - No schedule flag set: return immediately, scan runs now.
//   - --schedule-dry-run: print the next fire time and exit.
//   - --at or --in (one-shot): wait until the target time,
//     then return so the scan continues.
//   - --cron without --daemon: wait until the next fire time,
//     then return (one cycle). Useful for "run the next
//     scheduled time and then keep going".
//   - --cron with --daemon: keep cycling. After each scan
//     completes (returns from runScan's core.RunScan call),
//     re-compute the next fire time and wait again. This is
//     the only way to handle "run at 9am every day" — the
//     scan itself takes some time, and we want it to run at
//     the scheduled time, not N minutes late.
//
// 行为矩阵：
//   - 无调度 flag：立即返回，扫描现在跑。
//   - --schedule-dry-run：打下次执行时间后退出。
//   - --at 或 --in（一次性）：等到目标时间，然后返回让扫描继续。
//   - --cron 无 --daemon：等到下次 fire 然后返回（单轮）。适合
//     "跑下次调度时间然后继续"。
//   - --cron 加 --daemon：循环。每次扫描完成后（从 runScan 的
//     core.RunScan 返回），重算下次 fire 重新等待。这是"每天
//     9am 跑"的唯一办法——扫描本身要花点时间，我们想让它在调度
//     时间跑，不是晚 N 分钟。
package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/scheduler"
	"github.com/spf13/cobra"
)

// applySchedule resolves the schedule flags and either prints the
// next fire time (dry-run) or blocks until the target time. It
// is the first thing runScan does (before transport flags,
// workspace open, session build) so a misconfigured schedule
// fails fast without opening any sockets or files. / applySchedule
// 解析调度 flag，要么打下次 fire 时间（干跑）要么阻塞等到目标
// 时间。它是 runScan 第一件事（在 transport flag、工作区打开、
// session 构造之前），让配置错的调度在不打开任何 socket 或文件
// 时快速失败。
func applySchedule(cmd *cobra.Command) error {
	opts := scheduler.Options{
		At:     flagScheduleAt,
		In:     flagScheduleIn,
		Cron:   flagScheduleCron,
		TZ:     flagScheduleTZ,
		Daemon: flagScheduleDaemon,
		DryRun: flagScheduleDryRun,
	}
	in, err := scheduler.Resolve(opts)
	if err != nil {
		return err
	}
	if in.Mode == scheduler.ModeNone {
		// No schedule — scan runs now. / 无调度——扫描立即跑。
		return nil
	}
	in.Output = cmd.ErrOrStderr()
	next := in.NextFire()
	fmt.Fprintf(in.Output,
		"[*] scheduler: mode=%s tz=%s daemon=%v dry-run=%v\n",
		in.Mode, in.TZ, in.Daemon, in.DryRun,
	)
	fmt.Fprintf(in.Output,
		"[*] scheduler: next run at %s\n",
		next.Format(time.RFC3339),
	)
	if in.DryRun {
		// User asked for the time, not the wait. / 用户要时
		// 间，不要等。
		return nil
	}
	if !in.Daemon || in.Mode != scheduler.ModeCron {
		// One-shot: wait once, then return so the scan
		// proceeds. / 一次性：等一次，然后返回让扫描继续。
		return in.Wait(cmd.Context())
	}
	// Daemon: loop forever. After each scan run, re-compute
	// the next fire and wait again. / Daemon：永久循环。每
	// 次扫描后重算下次 fire 重新等。
	for {
		if err := in.Wait(cmd.Context()); err != nil {
			return err
		}
		// Re-set the base to the live clock so the next
		// NextFire computes from now (not from the previous
		// fire time, which is in the past). / 重设 base 为实
		// 时时钟，让下次 NextFire 从现在算（不从上次 fire 时
		// 间算，那个是过去时）。
		in.Base = time.Now()
		next = in.NextFire()
		fmt.Fprintf(in.Output, "[*] scheduler: next run at %s\n", next.Format(time.RFC3339))
	}
	// Unreachable — the loop only exits on ctx cancel.
	// / 不可达——循环只在 ctx 取消时退。
}

// unusedSilencer keeps the import of "os" live so future
// logging that needs stderr-handling primitives doesn't have
// to re-add it. / unusedSilencer 让 os 包的导入保持活跃，
// 未来加需要 stderr-handling 的日志时不用重新加。
var _ = os.Stderr
