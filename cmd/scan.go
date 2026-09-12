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
// helper so each concern is independently testable. The helpers live in
// sibling files: session/TUI/hard-exit in scan_lifecycle.go, output
// path & sink wiring in scan_outputs.go (split in audit L-7).
//
// runScan 故意保持薄编排器形态：每一步都是具名 helper，便于独立单测。
// helper 分布在同级文件：session/TUI/硬退出在 scan_lifecycle.go，
// 输出路径与 sink 装配在 scan_outputs.go（审计 L-7 拆分）。
package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/LCUstinian/FG-QiMen/internal/core"
	"github.com/LCUstinian/FG-QiMen/internal/core/credential/auth/network"
	"github.com/LCUstinian/FG-QiMen/internal/output"
	"github.com/LCUstinian/FG-QiMen/internal/plugins/adapted/web/webtitle/fingerprint"
	"github.com/LCUstinian/FG-QiMen/internal/transport"
	"github.com/LCUstinian/FG-QiMen/internal/tui"
	"github.com/LCUstinian/FG-QiMen/internal/types"
	"github.com/LCUstinian/FG-QiMen/internal/workspace"
)

var scanCmd = &cobra.Command{
	Use:   "scan [target]",
	Short: "Run a scan (default action of fg-qimen)",
	Long: `Run a scan. By default this is ephemeral (oneshot) mode, writing
results to ./fgqm_workspace/default/<YYYY-MM-DD>/fgqm_result.txt and the corresponding
.json in the current directory. Pass --project <name> to switch into
persistent project mode.

The target may be given as a positional argument (fg-qimen scan
192.168.1.0/24) or via --host/-H; --host wins when both are set.`,
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
	// Positional target support: `fg-qimen scan 192.168.1.0/24` is
	// the muscle-memory invocation for every pentest tool; requiring
	// --host forced a flag on the most common argument. --host wins
	// on conflict so scripted invocations keep priority. More than
	// one positional is a usage error (the host spec syntax already
	// covers lists via commas).
	// / 位置参数目标支持：`fg-qimen scan 192.168.1.0/24` 是渗透测试
	// 工具的肌肉记忆式调用；强制 --host 给最常见参数添了负担。冲突
	// 时 --host 优先，脚本化调用保持既有语义。多个位置参数是用法错
	// 误（逗号列表语法已覆盖多目标场景）。
	if len(args) > 0 {
		if len(args) > 1 {
			return fmt.Errorf("expected at most 1 positional target, got %d (use commas for multiple hosts: --host \"a,b\")", len(args))
		}
		if flagHost == "" {
			flagHost = args[0]
		}
	}

	// Wire the workspace override before any workspace I/O — this is
	// the earliest hook in runScan, so Open / resolveOutputPath /
	// List all see the effective root. / 在任何工作区 I/O 之前接线工
	// 作区覆盖——这是 runScan 里最早的钩子，Open / resolveOutputPath
	// / List 都能看到生效根。
	workspace.SetRoot(flagWorkspace)

	cfg, err := buildConfig(cmd.Flags())
	if err != nil {
		return fmt.Errorf("config error: %w", err)
	}

	// v0.5: scheduled scan. Parse the schedule (--at / --in /
	// --cron / --tz) and either dry-run, wait, or run as a
	// daemon before any network I/O. / v0.5：定时扫描。在任何网
	// 络 I/O 前解析 --at / --in / --cron / --tz，然后干跑、等
	// 待、或以 daemon 模式跑。
	if err := applySchedule(cmd); err != nil {
		return err
	}

	// Apply transport-layer security flags BEFORE any TLS/SSH probe
	// is constructed. The transport package exposes atomic flags that
	// the auth / plugin TLS sites read at probe-build time; setting
	// them here (before buildSession, before core.RunScan) means no
	// probe can observe a partial / default state.
	//
	// 在任何 TLS/SSH 探测构造前应用传输层安全 flag。transport 包暴露
	// atomic 标志，auth / plugin 的 TLS 站点在 probe 构造时读取；在
	// 这里（buildSession、core.RunScan 之前）设置意味着任何 probe
	// 都不会观察到部分 / 默认状态。
	applyTransport(cfg)
	applyHTTPForm()

	// Initialize global proxy manager BEFORE any network operations.
	// 在任何网络操作前初始化全局代理管理器。
	if err := initProxyManager(cfg); err != nil {
		return fmt.Errorf("proxy initialization error: %w", err)
	}

	// Open workspace (ephemeral or persistent) and ensure cleanup.
	// 打开工作区（即扫即走 / 增量扫描），并确保退出时清理。
	proj, err := openProject(cfg)
	if err != nil {
		return fmt.Errorf("workspace error: %w", err)
	}
	defer func() { _ = proj.Close() }()

	// Per-run log file: open AFTER applySchedule (which may wait for
	// the scheduled moment) so the HH-MM-SS stamp reflects the actual
	// scan start, and captured ONCE so the log file and every result
	// sink land in the same daily bucket. Opened before buildSession
	// so even the earliest log lines (resume warnings) land in the
	// file. Failure degrades to the pre-file behaviour with a stderr
	// warning — a log sink must never abort a scan.
	//
	// 本次 run 的日志文件：在 applySchedule（可能等待定时时刻）之后
	// 打开，让 HH-MM-SS 时间戳反映真实扫描起点；只捕获一次，让日志
	// 文件与所有结果 sink 落进同一日桶。在 buildSession 之前打开，
	// 最早的日志行（resume 警告）也能进文件。打开失败降级为无文件
	// 的原行为并 stderr 警告——日志 sink 绝不能让扫描中止。
	runNow := time.Now()
	logF, _, logErr := openRunLogFile(cfg, runNow)
	if logErr != nil {
		fmt.Fprintf(os.Stderr, "warning: log file: %v (continuing without file log)\n", logErr)
	} else {
		defer func() { _ = logF.Close() }()
	}

	// preHardExit is a lazy closure: it dereferences prog/runDone at
	// call time, not at creation time. The signal goroutine can only
	// reach preHardExit via a second SIGINT or drain timeout — both
	// take long enough that buildSession (and therefore the prog /
	// runDone assignment) is guaranteed to have completed first. The
	// nil-checks defend against the impossible "instant double SIGINT"
	// case at zero cost.
	//
	// preHardExit 是惰性闭包：调用时才解引用 prog / runDone。信号
	// goroutine 只能通过第二次 SIGINT 或 drain 超时到达 preHardExit
	// —— 两者都足够慢，buildSession（从而 prog / runDone 的赋值）必
	// 已完成。nil 检查以零开销防御理论上的"瞬时双 SIGINT"。
	var (
		prog    *tui.Program
		runDone chan struct{}
		// sessOut is set after openOutputSinks (further down).
		// The signal goroutine can only reach preHardExit via a
		// second SIGINT or drain timeout — both take long enough
		// that the assignment is guaranteed to have happened first.
		// / sessOut 在下面的 openOutputSinks 之后赋值。信号 goroutine
		// 只能经第二次 SIGINT 或 drain 超时到达 preHardExit——两者
		// 都足够慢，赋值必先完成。
		sessOut *output.Output
	)
	preHardExit := func() {
		// Flush + close all result sinks BEFORE we touch the TUI.
		// os.Exit(1) below skips the deferred sess.Out.Close() in
		// runScan, so without this explicit close the last
		// bufio-buffered writes (default 4 KB per sink) and the
		// SARIF in-memory buffer would never reach disk. We use
		// Close (not just Flush) so SARIF — which is single-doc
		// and only emits at Close time — also lands on disk.
		//
		// 在动 TUI 前 flush + close 所有结果 sink。下面 os.Exit(1)
		// 会跳过 runScan 里 defer 的 sess.Out.Close()，少了这个
		// 显式 close 的话，最后每 sink 的 bufio 缓冲（默认 4 KB）
		// 和 SARIF 内存缓冲都不会落盘。这里用 Close（而不是单
		// Flush）是为了让 SARIF——单文档，只在 Close 时输出——
		// 也写到磁盘。
		//
		// Idempotency: Output.Close is documented as safe on a
		// partially-initialized Output; calling it here even when
		// the deferred Close will eventually run too (it won't,
		// but defensively) is fine. We discard the error because
		// os.Exit(1) is the next thing that happens anyway and
		// there's no operator-readable surface to log to.
		//
		// 幂等性：Output.Close 文档明对部分初始化的 Output 安全；
		// 即便这里调了后面 defer 也会跑（实际上不会，但防御性写）
		// 也没问题。丢弃错误是因为接下来就是 os.Exit(1)，没有
		// 操作员能读的输出面来记录。
		closeOutputForHardExit(sessOut)
		if prog != nil && runDone != nil {
			prog.Quit()
			<-runDone
		}
	}

	// Graceful shutdown: first SIGINT cancels ctx; second SIGINT or
	// shutdown-timeout triggers os.Exit(1). preHardExit is invoked
	// synchronously so the TUI can release its altscreen / cursor
	// before the process dies.
	//
	// 优雅退出：第一次 SIGINT 取消 ctx；第二次 SIGINT 或 shutdown 超时
	// 触发 os.Exit(1)。preHardExit 同步调用，让 TUI 在进程死前释放
	// alt screen / cursor。
	ctx, cancel, drainCh := installSignalHandler(cfg.ShutdownTimeout, preHardExit)
	defer cancel()
	defer close(drainCh)

	// Build session with the signal-handler-owned ctx. buildSession
	// wires logger, store, and UI; in TUI mode it also assigns prog
	// and runDone so preHardExit can do its job.
	//
	// 用 signal handler 拥有的 ctx 构造 session。buildSession 装配
	// logger / store / UI；TUI 模式下还会赋值 prog 和 runDone 让
	// preHardExit 能完成清理。
	sess, cleanup, err := buildSession(ctx, cfg, proj, drainCh, logF, &prog, &runDone)
	if err != nil {
		return err
	}
	defer cleanup()
	defer func() { _ = sess.Out.Close() }()

	if err := loadResumeState(sess, cfg); err != nil {
		return err
	}
	if err := openOutputSinks(sess, cfg, runNow); err != nil {
		return err
	}
	// Hand the result sink back to preHardExit so the hard-exit
	// path (2nd SIGINT or drain timeout) can synchronously flush
	// + close it before os.Exit(1) — otherwise the deferred
	// sess.Out.Close() never runs and the last buffered writes
	// (4 KB per sink) plus the SARIF document buffer are lost.
	// The signal goroutine can only fire hardExit after at least
	// one signal round-trip or a multi-second drain timeout, both
	// of which guarantee this assignment has happened.
	//
	// 把结果汇回传给 preHardExit，让硬退出路径（第二次 SIGINT
	// 或 drain 超时）能在 os.Exit(1) 前同步 flush + close——
	// 否则 defer 的 sess.Out.Close() 不会跑，最后的缓冲写入
	//（每 sink 4 KB）和 SARIF 文档缓冲都会丢。信号 goroutine
	// 至少要经过一次信号往返或多秒的 drain 超时才能触发
	// hardExit，两种情况都保证此赋值已发生。
	sessOut = sess.Out

	// Phase D (audit roadmap): load optional user-supplied
	// web-fingerprint ruleset. Loaded AFTER session init so the
	// logger is wired. / Phase D（审计路线图）：加载可选的
	// 用户 web 指纹规则集。在 session 初始化后加载，让 logger
	// 已挂上。
	if flagWebFingerprint != "" {
		if added, err := fingerprint.LoadCustomRuleset(flagWebFingerprint); err != nil {
			sess.Log.Warn("web-fingerprint ruleset %q: %v (continuing with built-in rules only)", flagWebFingerprint, err)
		} else {
			sess.Log.Info("[*] web-fingerprint: loaded %d custom rule(s) from %s", added, flagWebFingerprint)
		}
	}

	if _, err := core.RunScan(ctx, sess); err != nil {
		return fmt.Errorf("scan error: %w", err)
	}
	return nil
}

// resolveProjectKey returns the encryption passphrase for the project
// DB. Priority: --project-key flag > FG_QIMEN_PROJECT_KEY env > "".
// Empty return means "no encryption" (v0.2.x plaintext on-disk format).
//
// resolveProjectKey 返回项目 DB 的加密 passphrase。优先级：
// --project-key flag > FG_QIMEN_PROJECT_KEY env > ""。
// 返回空表示"不加密"（v0.2.x 明文磁盘格式）。
func resolveProjectKey() string {
	if flagProjectKey != "" {
		return flagProjectKey
	}
	return os.Getenv("FG_QIMEN_PROJECT_KEY")
}

// buildConfig collects the global flag values into a Config struct.
// pf (the executed command's merged flag set) supplies the explicit-
// tracking info; nil is tolerated (explicit = false, e.g. in tests).
//
// buildConfig 把全局 flag 值汇总成 Config 结构。pf（执行命令的合并
// flag 集）提供显式追踪信息；允许 nil（显式 = false，如测试中）。
func buildConfig(pf *pflag.FlagSet) (*types.Config, error) {
	// Explicit-tracking (fscan's isExplicit pattern): the core env
	// profiler only auto-tunes values the operator did NOT set on the
	// CLI. pflag.Changed is the authoritative source — flag defaults
	// (e.g. --threads 200) do not count as explicit. The flag set is
	// passed in (not rootCmd.PersistentFlags()) because referencing
	// rootCmd here creates an initialization cycle through runScan.
	// / 显式追踪（fscan 的 isExplicit 模式）：核心环境画像只自动调
	// 优操作员未在 CLI 显式设置的值。pflag.Changed 是权威来源——flag
	// 默认值（如 --threads 200）不算显式。FlagSet 由参数传入（而非
	// 直接引用 rootCmd.PersistentFlags()），因为这里引用 rootCmd 会
	// 经由 runScan 形成初始化环。
	var threadsExplicit, timeoutExplicit bool
	if pf != nil {
		threadsExplicit = pf.Changed("threads")
		timeoutExplicit = pf.Changed("timeout")
	}
	cfg := &types.Config{
		Host:             flagHost,
		HostsFile:        flagHostsFile,
		ExcludeHosts:     flagExcludeHosts,
		ExcludeHostsFile: flagExcludeHostsFile,
		Project:          flagProject,
		ProjectKey:       resolveProjectKey(),
		Mode:             types.RunMode(flagMode),
		Resume:           flagResume,
		NoState:          flagNoState,
		Ports:            flagPorts,
		ExcludePorts:     flagExcludePorts,
		Proxy:            flagProxy,
		Socks5:           flagSocks5,
		Iface:            flagIface,
		PortTimeout:      flagPortTimeout,
		WebTimeout:       flagWebTimeout,
		AliveOnly:        flagAliveOnly,
		Threads:          flagThreads,
		Timeout:          flagTimeout,
		MaxPluginWorkers: flagMaxPluginWorkers,
		Users:            flagUser,
		Passes:           flagPass,
		UserFile:         flagUserFile,
		PassFile:         flagPassFile,
		OutputTXT:        flagOutputTXT,
		OutputJSON:       flagOutputJSON,
		OutputCSV:        flagOutputCSV,
		Silent:           flagSilent,
		NoTUI:            flagNoTUI,
		NoICMP:           flagNoICMP,
		NoBatch:          flagNoBatch,
		NoSubnetProbe:    flagNoPrescreen,
		Verbose:          flagVerbose,
		ShowCleartext:    flagShowCleartext,
		InsecureTLS:      flagInsecureTLS,
		InsecureSSH:      flagInsecureSSH,
		KnownHostsFile:   flagKnownHosts,
		ShutdownTimeout:  flagShutdownTime,
		Plugins:          flagPlugins,

		ThreadsExplicit: threadsExplicit,
		TimeoutExplicit: timeoutExplicit,
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// openProject opens a project workspace (ephemeral or persistent).
// Task 4 (first-batch fixes): honours cfg.NoState by passing it
// through to workspace.OpenWithOptions, so a `--no-state` invocation
// on a named project skips the bbolt open and the fgqm_workspace/projects/<name>/
// directory creation entirely. Without this, `--no-state` was dead
// code: the flag was wired through cfg.NoState but the production
// path unconditionally called proj.AsStore(), which forced a bbolt
// open in workspace.Open.
//
// openProject 打开项目工作区（即扫即走 / 增量扫描）。
// 第一批修复 Task 4：通过 workspace.OpenWithOptions 兑现 cfg.NoState，
// 让对命名项目的 `--no-state` 调用完全跳过 bbolt 打开和
// fgqm_workspace/projects/<name>/ 目录创建。否则 `--no-state` 是
// 死代码：flag 通过 cfg.NoState 传递，但生产路径无条件调 proj.AsStore()，
// 迫使 workspace.Open 打开 bbolt。
func openProject(cfg *types.Config) (*workspace.Project, error) {
	return workspace.OpenWithOptions(cfg.Project, workspace.OpenOptions{NoState: cfg.NoState})
}

// applyTransport copies the cmd-line transport security flags into
// the process-wide atomic flags in internal/transport. Called once
// at scan start (before any probe is built); subsequent calls in the
// same process re-set the flags (idempotent; the values are still
// authoritative for the rest of the run).
//
// applyTransport 把 cmd 行的 transport 安全 flag 拷到 internal/transport
// 的进程级 atomic flag 上。扫描启动时调一次（任何 probe 构造前）；同一
// 进程内多次调用会重新设 flag（幂等；值对后续运行仍然有效）。
func applyTransport(cfg *types.Config) {
	if cfg == nil {
		return
	}
	transport.InsecureTLS.Store(cfg.InsecureTLS)
	transport.InsecureSSH.Store(cfg.InsecureSSH)
	if cfg.KnownHostsFile != "" {
		path := cfg.KnownHostsFile
		transport.KnownHostsFile.Store(&path)
	}
}

// applyHTTPForm copies the cmd-line http-form-* flags into the
// package-level vars in core/credential/auth/network. The
// HTTPFormAuthenticator reads these on every attempt. / applyHTTPForm
// 把 cmd 行的 http-form-* flag 拷到 core/credential/auth/network 的
// 包级变量。HTTPFormAuthenticator 每次 attempt 读取这些。
func applyHTTPForm() {
	network.HTTPFormURL = flagHTTPFormURL
	network.HTTPFormFields = flagHTTPFormFields
	network.HTTPFormSuccess = flagHTTPFormSuccess
	network.HTTPFormFailure = flagHTTPFormFailure
	network.HTTPFormRedirect = flagHTTPFormRedir
}
