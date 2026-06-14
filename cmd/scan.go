// scan.go — `fg-qimen scan` subcommand and the implementation of the
// default scan pipeline.
//
// scan.go — `fg-qimen scan` 子命令及默认扫描管线的实现。
//
// scanCmd is also wired as rootCmd.RunE in root.go so that
// `fg-qimen -H 192.168.1.0/24` works without an explicit `scan` token —
// `fg-qimen scan -H 192.168.1.0/24` is the explicit-and-grep-friendly
// alias. resumeCmd in resume.go also delegates to runScan after forcing
// --resume=true.
//
// scanCmd 同时在 root.go 中作为 rootCmd.RunE 注册，使
// `fg-qimen -H 192.168.1.0/24` 无需显式 `scan` token 即可工作；
// `fg-qimen scan -H 192.168.1.0/24` 是显式且便于 grep 的等价写法。
// resume.go 中的 resumeCmd 强制 --resume=true 后同样委托给 runScan。
//
// runScan is intentionally a thin orchestrator — every step is a named
// helper so each concern is independently testable.
//
// runScan 故意保持薄编排器形态：每一步都是具名 helper，便于独立单测。
package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/LCUstinian/FG-QiMen/internal/core"
	"github.com/LCUstinian/FG-QiMen/internal/output"
	"github.com/LCUstinian/FG-QiMen/internal/session"
	"github.com/LCUstinian/FG-QiMen/internal/tui"
	"github.com/LCUstinian/FG-QiMen/internal/types"
	"github.com/LCUstinian/FG-QiMen/internal/ui"
	"github.com/LCUstinian/FG-QiMen/internal/workspace"
)

var scanCmd = &cobra.Command{
	Use:   "scan",
	Short: "Run a scan (default action of fg-qimen)",
	Long: `Run a scan. By default this is ephemeral (oneshot) mode, writing
results to ./result.txt and ./result.json in the current directory.
Pass -p <name> to switch into persistent project mode.`,
	// Reuse the root RunE so flags and behavior are identical.
	// 复用根 RunE，flags 和行为完全一致。
	RunE: runScan,
}

func init() {
	rootCmd.AddCommand(scanCmd)
}

// runScan is the default RunE for rootCmd and the explicit `scan`
// subcommand. It is a thin orchestrator: every step is a named helper.
//
// runScan 是 rootCmd 的默认 RunE，也是显式 `scan` 子命令的处理函数。
// 它是薄编排器：每一步都是具名 helper。
//
// 流程：flag → Config → workspace open → context + signal handler →
// session → resume load → output open → core.RunScan。
func runScan(cmd *cobra.Command, args []string) error {
	cfg, err := buildConfig()
	if err != nil {
		return fmt.Errorf("config error: %w", err)
	}

	// Open workspace (ephemeral or persistent) and ensure cleanup.
	// 打开工作区（即扫即走 / 增量扫描），并确保退出时清理。
	proj, err := openProject(cfg)
	if err != nil {
		return fmt.Errorf("workspace error: %w", err)
	}
	defer func() { _ = proj.Close() }()

	// Graceful shutdown: first SIGINT cancels ctx; second SIGINT or
	// shutdown-timeout triggers os.Exit(1).
	//
	// 优雅退出：第一次 SIGINT 取消 ctx；第二次 SIGINT 或 shutdown 超时
	// 触发 os.Exit(1)。
	ctx, cancel, drainCh := installSignalHandler(cfg.ShutdownTimeout)
	defer cancel()
	defer close(drainCh)

	// Build session, then wire logger + UI + store. cleanup() quits the
	// TUI if it was started, and is safe to call regardless.
	//
	// 构造 session，并装配 logger / UI / store。cleanup() 退出 TUI
	// （如有），无 TUI 时是空操作。
	sess, cleanup, err := buildSession(ctx, cfg, proj, drainCh)
	if err != nil {
		return err
	}
	defer cleanup()
	defer func() { _ = sess.Out.Close() }()

	if err := loadResumeState(sess, cfg); err != nil {
		return err
	}
	if err := openOutputSinks(sess, cfg); err != nil {
		return err
	}

	if _, err := core.RunScan(ctx, sess); err != nil {
		return fmt.Errorf("scan error: %w", err)
	}
	return nil
}

// installSignalHandler wires SIGINT/SIGTERM into a graceful-shutdown
// pipeline and returns:
//   - ctx: cancelled on first signal
//   - cancel: explicit cancellation the caller can also trigger
//   - drainCh: closed by the caller when the scan finishes; the goroutine
//     uses it to know that "normal completion" has occurred so it can
//     exit without waiting for a second signal.
//
// The goroutine exits via os.Exit(1) on second signal or drain timeout.
//
// installSignalHandler 把 SIGINT/SIGTERM 接入优雅退出管线，返回：
//   - ctx：收到首次信号时取消
//   - cancel：调用方主动取消
//   - drainCh：调用方在 scan 结束时关闭；goroutine 借此知道"正常完成"
//     而非等待第二次信号。
//
// 收到第二次信号或排空超时时 goroutine 调 os.Exit(1)。
func installSignalHandler(timeout time.Duration) (context.Context, context.CancelFunc, chan struct{}) {
	ctx, cancel := context.WithCancel(context.Background())
	drainCh := make(chan struct{})
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		select {
		case <-sigs:
			// First signal: cancel and start drain.
			// 第一次信号：触发取消并开始排空。
			fmt.Fprintln(os.Stderr, "\n[!] Received interrupt, draining pipeline...")
			cancel()
			select {
			case <-drainCh:
				// Pipeline drained cleanly.
				// 排空完成。
			case <-sigs:
				// Second signal within drain window: hard exit.
				// 排空期间收到第二次信号：强退。
				fmt.Fprintln(os.Stderr, "[!] Second interrupt received, forcing exit")
				os.Exit(1)
			case <-time.After(timeout):
				// Drain timed out: hard exit.
				// 排空超时：强退。
				fmt.Fprintln(os.Stderr, "[!] Drain timed out, forcing exit")
				os.Exit(1)
			}
		case <-drainCh:
			// Normal completion; nothing to do.
			// 正常完成。
		}
	}()
	return ctx, cancel, drainCh
}

// buildSession constructs the Session and wires logger, UI, and store.
// The returned cleanup function quits the TUI if one was started; it
// is safe to call regardless (no-op in plain-text mode).
//
// buildSession 构造 Session 并装配 logger / UI / store。返回的 cleanup
// 函数在启用了 TUI 时调用 prog.Quit()，纯文本模式下是空操作。
func buildSession(ctx context.Context, cfg *types.Config, proj *workspace.Project, drainCh chan struct{}) (*session.Session, func(), error) {
	sess, err := session.NewSession(ctx, cfg, cfg.Project)
	if err != nil {
		return nil, nil, fmt.Errorf("session error: %w", err)
	}

	// Wire logger (silent flag suppresses to file-only; -v adds debug).
	// 装配 logger（silent 抑制控制台；-v 开启 debug）。
	if !cfg.Silent {
		sess.Log = types.NewStderrLogger()
	} else {
		sess.Log = types.DiscardLogger{}
	}

	// Wire UI: TUI mode if stdout is a TTY and -no-tui/-silent are not
	// set. Otherwise, plain text mode (results are still in the file
	// sinks).
	//
	// 装配 UI：stdout 是 TTY 且未传 -no-tui/-silent 时进 TUI 模式；
	// 否则纯文本模式（结果仍在文件汇中输出）。
	useTUI := types.IsTerminalStdout() && !cfg.NoTUI && !cfg.Silent
	if useTUI {
		prog := tui.NewProgram(cfg)
		sess.UI = prog
		go func() {
			if _, err := prog.Run(); err != nil {
				fmt.Fprintln(os.Stderr, "tui error:", err)
			}
		}()
		// Quit the TUI when the scan finishes (normal path: drainCh
		// closes → quit TUI). The cleanup function is the public exit;
		// the watcher goroutine is a fallback for early returns.
		//
		// scan 结束时退出 TUI（正常路径：drainCh 关闭 → 退出 TUI）。
		// cleanup 是公开出口；watcher goroutine 是早退的兜底。
		cleanup := func() { prog.Quit() }
		go func() {
			<-drainCh
			cleanup()
		}()
		return sess, cleanup, nil
	}

	sess.UI = ui.NewTextUI()

	// Wire bbolt store from project (nil in ephemeral mode).
	// 从 project 装配 bbolt store（即扫即走模式下为 nil）。
	sess.Store = proj.AsStore()

	return sess, func() {}, nil
}

// loadResumeState loads the persisted seen-set from bbolt into the
// in-memory State so the pipeline skips previously-processed triples.
// No-op when -resume is not set or in ephemeral mode.
//
// loadResumeState 把 bbolt 持久化的 seen-set 加载到内存 State，让
// pipeline 跳过已处理项。未设 -resume 或即扫即走模式下空操作。
func loadResumeState(sess *session.Session, cfg *types.Config) error {
	if !cfg.Resume || sess.Store == nil {
		return nil
	}
	hashes, err := sess.Store.LoadSeenHashes()
	if err != nil {
		return fmt.Errorf("load seen set: %w", err)
	}
	for _, h := range hashes {
		sess.State.MarkSeen(h)
	}
	sess.Log.Info("[*] resume: loaded %d seen hashes from bbolt", len(hashes))
	return nil
}

// openOutputSinks opens the multi-format result sink and attaches it
// to sess. Defaults are project-relative for project mode, or current
// directory for ephemeral.
//
// openOutputSinks 打开多格式结果汇并挂到 sess。默认在项目目录下
// （项目模式）或当前目录（即扫即走）。
func openOutputSinks(sess *session.Session, cfg *types.Config) error {
	out, err := output.OpenOutput(output.OutputConfig{
		ResultTXTPath:  resolveOutputPath(cfg, flagOutputTXT, "result.txt"),
		ResultJSONPath: resolveOutputPath(cfg, flagOutputJSON, "result.json"),
		CredsPath:      resolveOutputPath(cfg, "", "creds.txt"),
		RDPJSONPath:    resolveOutputPath(cfg, "", "rdp.json"),
		RDPTXTPath:     resolveOutputPath(cfg, "", "rdp.txt"),
	})
	if err != nil {
		return fmt.Errorf("output error: %w", err)
	}
	sess.Out = out
	return nil
}

// buildConfig collects the global flag values into a Config struct.
// buildConfig 把全局 flag 值汇总成 Config 结构。
func buildConfig() (*types.Config, error) {
	cfg := &types.Config{
		Host:            flagHost,
		HostsFile:       flagHostsFile,
		Project:         flagProject,
		Mode:            types.RunMode(flagMode),
		Resume:          flagResume,
		NoState:         flagNoState,
		Ports:           flagPorts,
		ExcludePorts:    flagExcludePorts,
		AliveOnly:       flagAliveOnly,
		Threads:         flagThreads,
		Timeout:         flagTimeout,
		Users:           flagUser,
		Passes:          flagPass,
		UserFile:        flagUserFile,
		PassFile:        flagPassFile,
		OutputTXT:       flagOutputTXT,
		OutputJSON:      flagOutputJSON,
		Silent:          flagSilent,
		NoTUI:           flagNoTUI,
		NoICMP:          flagNoICMP,
		Verbose:         flagVerbose,
		ShutdownTimeout: flagShutdownTime,
		Plugins:         flagPlugins,
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// openProject opens a project workspace (ephemeral or persistent).
// openProject 打开项目工作区（即扫即走 / 增量扫描）。
func openProject(cfg *types.Config) (*workspace.Project, error) {
	return workspace.Open(cfg.Project)
}

// resolveOutputPath resolves a possibly-empty output path to a default
// inside the project root (project mode) or the ./runs/default/
// directory (ephemeral mode). User-supplied paths via -o / -j are
// returned as-is.
//
// resolveOutputPath 把可能为空的输出路径解析为默认值：
//   - 项目模式：./runs/projects/<name>/<file>
//   - 即扫即走：./runs/default/<file>
//   - 显式 -o / -j：原样返回
func resolveOutputPath(cfg *types.Config, flagValue, defaultName string) string {
	if flagValue != "" {
		return flagValue
	}
	if cfg.Project != "" {
		return filepath.Join("runs", "projects", cfg.Project, defaultName)
	}
	return filepath.Join("runs", "default", defaultName)
}
