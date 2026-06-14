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

// runScan is the default RunE for rootCmd and the explicit `scan` subcommand.
// runScan 是 rootCmd 的默认 RunE，也是显式 `scan` 子命令的处理函数。
//
// It builds the Config from flags, opens the workspace (ephemeral or
// persistent), wires up the SIGINT-driven graceful shutdown context, and
// dispatches to core.RunScan.
//
// 流程：flag → Config → workspace open → context + signal handler → core.RunScan。
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

	// Wire up graceful shutdown: first SIGINT triggers cancel + drain,
	// second SIGINT (or shutdown-timeout) hard-exits.
	//
	// 优雅退出：第一次 SIGINT 触发 cancel + 排空，第二次 SIGINT（或超时）强退。
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	done := make(chan struct{})
	go func() {
		select {
		case <-sigs:
			// First signal: cancel and start drain.
			// 第一次信号：触发取消并开始排空。
			fmt.Fprintln(os.Stderr, "\n[!] Received interrupt, draining pipeline...")
			cancel()
			select {
			case <-done:
				// Pipeline drained cleanly.
				// 排空完成。
			case <-sigs:
				// Second signal within drain window: hard exit.
				// 排空期间收到第二次信号：强退。
				fmt.Fprintln(os.Stderr, "[!] Second interrupt received, forcing exit")
				os.Exit(1)
			case <-time.After(cfg.ShutdownTimeout):
				// Drain timed out: hard exit.
				// 排空超时：强退。
				fmt.Fprintln(os.Stderr, "[!] Drain timed out, forcing exit")
				os.Exit(1)
			}
		case <-done:
			// Normal completion; nothing to do.
		}
	}()
	defer close(done)

	// Build session from cfg + project + state.
	// 从 cfg + project + state 构建 session。
	sess, err := session.NewSession(ctx, cfg, cfg.Project)
	if err != nil {
		return fmt.Errorf("session error: %w", err)
	}
	defer func() { _ = sess.Out.Close() }()

	// Wire logger (silent flag suppresses to file-only; -v adds debug).
	// 装配 logger（silent 抑制控制台；-v 开启 debug）。
	if !cfg.Silent {
		sess.Log = types.NewStderrLogger()
	} else {
		sess.Log = types.DiscardLogger{}
	}

	// Wire UI: TUI mode if stdout is a TTY and -no-tui/-silent are not set.
	// Otherwise, plain text mode (NopUI; results are still in the file sinks).
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
		defer prog.Quit()
	} else {
		sess.UI = ui.NewTextUI()
	}

	// Wire bbolt store from project (nil in ephemeral mode).
	// 从 project 装配 bbolt store（即扫即走模式下为 nil）。
	sess.Store = proj.AsStore()

	// On --resume, load the persisted seen-set into the in-memory State
	// so the pipeline skips previously-processed (host, port, plugin) triples.
	// --resume 时从 bbolt 加载已见 hash 到内存 State，让 pipeline 跳过已处理项。
	if cfg.Resume && sess.Store != nil {
		hashes, err := sess.Store.LoadSeenHashes()
		if err != nil {
			return fmt.Errorf("load seen set: %w", err)
		}
		for _, h := range hashes {
			sess.State.MarkSeen(h)
		}
		sess.Log.Info("[*] resume: loaded %d seen hashes from bbolt", len(hashes))
	}

	// Open output files. Defaults are project-relative for project mode,
	// or current directory for ephemeral.
	// 打开输出文件。默认在项目目录下（项目模式）或当前目录（即扫即走）。
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

	if _, err := core.RunScan(ctx, sess); err != nil {
		return fmt.Errorf("scan error: %w", err)
	}
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
