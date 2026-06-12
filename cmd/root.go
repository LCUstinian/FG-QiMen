// Package cmd implements the Cobra command tree for fg-qimen.
// Package cmd 实现 fg-qimen 的 Cobra 命令树。
//
// The command tree:
//
//	fg-qimen (root)
//	├── scan        — default scan (ephemeral or -p <project>)
//	├── resume      — resume a project from bbolt state
//	├── projects    — manage project workspaces
//	│   ├── list
//	│   ├── create
//	│   ├── delete
//	│   └── info
//	└── version     — show version
//
// All terminal output (banner, help, log, error) is English-only.
// Comments are bilingual (Chinese + English) for international collaborator
// readability.
//
// 所有终端输出（banner、help、日志、错误）均为纯英文。
// 注释为中英双语，便于国际协作者阅读。
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

	"github.com/LCUstinian/FG-QiMen/common"
	"github.com/LCUstinian/FG-QiMen/core"
	_ "github.com/LCUstinian/FG-QiMen/core/cred/protocols" // register SSH/FTP/MySQL authenticators
	"github.com/LCUstinian/FG-QiMen/tui"
	"github.com/LCUstinian/FG-QiMen/workspace"

	// Register all built-in plugins via their init() funcs.
	// 通过 init() 注册所有内置插件。
	_ "github.com/LCUstinian/FG-QiMen/plugins/adapted"
)

// Global flags, populated by Cobra and consumed by BuildConfig.
// 全局 flag，由 Cobra 填充，由 BuildConfig 消费。
var (
	flagHost         string
	flagHostsFile    string
	flagProject      string
	flagMode         string
	flagResume       bool
	flagNoState      bool
	flagPorts        string
	flagExcludePorts string
	flagAliveOnly    bool
	flagThreads      int
	flagTimeout      time.Duration
	flagUser         []string
	flagPass         []string
	flagUserFile     string
	flagPassFile     string
	flagOutputTXT    string
	flagOutputJSON   string
	flagSilent       bool
	flagNoTUI        bool
	flagNoICMP       bool
	flagVerbose      bool
	flagShutdownTime time.Duration
	flagPlugins      string
)

// rootCmd is the top-level fg-qimen command.
// rootCmd 是 fg-qimen 的顶级命令。
var rootCmd = &cobra.Command{
	Use:   "fg-qimen",
	Short: "FG-QiMen — pipeline scanner with project workspaces",
	Long: `FG-QiMen is a CLI scanner that decouples the port scanner (producer)
from the plugin workers (consumer) via a Go channel pipeline. It supports
three run modes (scan / crack / linked) and two work modes (ephemeral
oneshot or persistent project workspace with bbolt state).

Examples / 用例:
  fg-qimen -H 192.168.1.0/24                          # ephemeral scan
  fg-qimen -p corp -H 10.0.0.0/24 -mode linked       # project mode
  fg-qimen -p corp -H 10.0.0.0/24 -resume            # resume
  fg-qimen projects list                              # list projects`,
	SilenceUsage:  true,
	SilenceErrors: false,
	// Default behavior: run a scan.
	// 默认行为：执行扫描。
	RunE: runScan,
}

// Execute is the entry point invoked by main.go.
// Execute 是 main.go 调用的入口。
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	// Persistent flags are inherited by subcommands.
	// 持久化 flag 会被子命令继承。
	pf := rootCmd.PersistentFlags()

	pf.StringVarP(&flagHost, "host", "H", "",
		"target IP / CIDR / range / comma-list (e.g. 192.168.1.0/24)")
	pf.StringVarP(&flagHostsFile, "hosts-file", "f", "",
		"load targets from a file (one per line)")
	pf.StringVarP(&flagProject, "project", "p", "",
		"project name (empty = ephemeral oneshot mode)")
	pf.StringVar(&flagMode, "mode", "scan",
		"run mode: scan | crack | linked")
	pf.BoolVarP(&flagResume, "resume", "", false,
		"resume from bbolt seen-set (project mode only)")
	pf.BoolVarP(&flagNoState, "no-state", "", false,
		"disable bbolt, use in-memory dedup only")
	pf.StringVar(&flagPorts, "ports", "22,80,3306,3389,6379,8080",
		"comma-separated port list to scan")
	pf.StringVar(&flagExcludePorts, "exclude-ports", "",
		"comma-separated ports to exclude")
	pf.BoolVarP(&flagAliveOnly, "alive-only", "a", false,
		"only run host discovery; skip port scan and plugins")
	pf.IntVarP(&flagThreads, "threads", "t", 200,
		"concurrent worker count")
	pf.DurationVarP(&flagTimeout, "timeout", "", 3*time.Second,
		"per-operation timeout (e.g. 3s, 500ms)")
	pf.StringSliceVarP(&flagUser, "user", "u", nil,
		"credential testing usernames (repeatable)")
	pf.StringSliceVarP(&flagPass, "pass", "P", nil,
		"credential testing passwords (repeatable)")
	pf.StringVar(&flagUserFile, "user-file", "",
		"usernames dictionary file")
	pf.StringVar(&flagPassFile, "pass-file", "",
		"passwords dictionary file")
	pf.StringVarP(&flagOutputTXT, "output-txt", "o", "",
		"path to TXT result file (default: <project>/result.txt or ./result.txt)")
	pf.StringVarP(&flagOutputJSON, "output-json", "j", "",
		"path to NDJSON result file (default: <project>/result.json or ./result.json)")
	pf.BoolVar(&flagSilent, "silent", false,
		"suppress info log to console; file output still works")
	pf.BoolVar(&flagNoTUI, "no-tui", false,
		"force plain-text mode even when stdout is a TTY")
	pf.BoolVar(&flagNoICMP, "no-icmp", false,
		"skip ICMP probe, use TCP-ping fallback only")
	pf.BoolVarP(&flagVerbose, "verbose", "v", false,
		"verbose debug logging")
	pf.DurationVar(&flagShutdownTime, "shutdown-timeout", 5*time.Second,
		"graceful shutdown drain timeout")
	pf.StringVar(&flagPlugins, "plugins", "",
		"comma-separated plugin names to enable (default: all)")

	// Subcommands are registered from their own files via init().
	// 子命令由各自文件的 init() 注册。
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
	sess, err := common.NewSession(ctx, cfg, cfg.Project)
	if err != nil {
		return fmt.Errorf("session error: %w", err)
	}
	defer func() { _ = sess.Out.Close() }()

	// Wire logger (silent flag suppresses to file-only; -v adds debug).
	// 装配 logger（silent 抑制控制台；-v 开启 debug）。
	if !cfg.Silent {
		sess.Log = common.NewStderrLogger()
	} else {
		sess.Log = common.DiscardLogger{}
	}

	// Wire UI: TUI mode if stdout is a TTY and -no-tui/-silent are not set.
	// Otherwise, plain text mode (NopUI; results are still in the file sinks).
	// 装配 UI：stdout 是 TTY 且未传 -no-tui/-silent 时进 TUI 模式；
	// 否则纯文本模式（结果仍在文件汇中输出）。
	useTUI := common.IsTerminalStdout() && !cfg.NoTUI && !cfg.Silent
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
		sess.UI = common.NewTextUI()
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
	out, err := common.OpenOutput(common.OutputConfig{
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
func buildConfig() (*common.Config, error) {
	cfg := &common.Config{
		Host:            flagHost,
		HostsFile:       flagHostsFile,
		Project:         flagProject,
		Mode:            common.RunMode(flagMode),
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
func openProject(cfg *common.Config) (*workspace.Project, error) {
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
func resolveOutputPath(cfg *common.Config, flagValue, defaultName string) string {
	if flagValue != "" {
		return flagValue
	}
	if cfg.Project != "" {
		return filepath.Join("runs", "projects", cfg.Project, defaultName)
	}
	return filepath.Join("runs", "default", defaultName)
}
