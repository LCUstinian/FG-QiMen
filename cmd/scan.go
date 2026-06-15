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
	"path/filepath"
	"strings"
	"sync"

	"github.com/spf13/cobra"

	"github.com/LCUstinian/FG-QiMen/internal/core"
	"github.com/LCUstinian/FG-QiMen/internal/output"
	"github.com/LCUstinian/FG-QiMen/internal/session"
	"github.com/LCUstinian/FG-QiMen/internal/transport"
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

	// Open workspace (ephemeral or persistent) and ensure cleanup.
	// 打开工作区（即扫即走 / 增量扫描），并确保退出时清理。
	proj, err := openProject(cfg)
	if err != nil {
		return fmt.Errorf("workspace error: %w", err)
	}
	defer func() { _ = proj.Close() }()

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
	)
	preHardExit := func() {
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
	sess, cleanup, err := buildSession(ctx, cfg, proj, drainCh, &prog, &runDone)
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

// buildSession constructs the Session and wires logger, UI, and store.
// The returned cleanup function quits the TUI if one was started; it
// is safe to call regardless (no-op in plain-text mode) and is
// idempotent — calling it twice (e.g. once via defer and once via the
// signal-handler preHardExit) is harmless.
//
// In TUI mode buildSession also writes the *tui.Program and the
// bubbletea-Run-done channel back through the prog / runDone out
// parameters so runScan's preHardExit closure can do its job. The
// TUI Run goroutine + drainCh watcher are also started here.
//
// buildSession 构造 Session 并装配 logger / UI / store。返回的 cleanup
// 函数在启用了 TUI 时调用 prog.Quit()，纯文本模式下是空操作。多次
// 调用（defer + signal-handler preHardExit）是幂等的。
//
// TUI 模式下 buildSession 还会通过 prog / runDone 出参回写 *tui.Program
// 和 bubbletea-Run-done channel，让 runScan 的 preHardExit 闭包能正常
// 工作。TUI Run goroutine 和 drainCh watcher 也在这里启动。
func buildSession(ctx context.Context, cfg *types.Config, proj *workspace.Project, drainCh chan struct{}, prog **tui.Program, runDone *chan struct{}) (*session.Session, func(), error) {
	sess, err := session.NewSession(ctx, cfg, cfg.Project)
	if err != nil {
		return nil, nil, fmt.Errorf("session error: %w", err)
	}

	// Wire logger (silent flag suppresses to file-only; -v adds debug).
	// The TUI is unaffected by Silent — the dashboard is the live event
	// surface, the logger is the secondary channel; both can be quiet
	// or noisy independently.
	//
	// 装配 logger（silent 抑制控制台；-v 开启 debug）。TUI 不受 Silent
	// 影响——dashboard 是实时事件展示，logger 是次要通道；两者可以独
	// 立地安静或嘈杂。
	if !cfg.Silent {
		sess.Log = types.NewStderrLogger()
	} else {
		sess.Log = types.DiscardLogger{}
	}

	// Wire bbolt store from project (nil in ephemeral mode). Done
	// BEFORE the UI choice so the TUI path also gets persistence
	// wired — a previous version of this code set Store only on the
	// text-UI branch, which silently broke -resume in TUI mode.
	//
	// 从 project 装配 bbolt store（即扫即走模式下为 nil）。放在 UI
	// 选择之前，让 TUI 路径也获得持久化——旧版只在 text-UI 分支赋值
	// Store，导致 -resume 在 TUI 模式下静默失效。
	sess.Store = proj.AsStore()

	// UI selection: consult ui.ShouldUseTUI (which centralises the
	// tty / CI / dumb-term / width logic) and act on the result.
	//
	// UI 选择：调用 ui.ShouldUseTUI（集中了 tty / CI / dumb-term /
	// 宽度判断），按结果分支。
	if !ui.ShouldUseTUI(cfg) {
		sess.UI = ui.NewTextUI(cfg)
		return sess, func() {}, nil
	}

	// TUI path. / TUI 路径。
	p := tui.NewProgram(cfg)
	sess.UI = p

	// Hand the program pointer and the Run-done channel back to
	// runScan so its preHardExit closure can release the altscreen
	// synchronously on hard exit.
	//
	// 把 program 指针和 Run-done channel 回传给 runScan，让其
	// preHardExit 闭包在硬退出时同步释放 altscreen。
	*prog = p
	*runDone = make(chan struct{})

	// Start the bubbletea Run loop. The Run goroutine's lifetime is
	// the TUI's lifetime: closing runDone signals the TUI is fully
	// torn down (altscreen restored, goroutine exited).
	//
	// 启动 bubbletea Run 循环。Run goroutine 的生命期就是 TUI 的
	// 生命期：关闭 runDone 意味着 TUI 完整拆除（altscreen 还原、goroutine
	// 退出）。
	go func() {
		defer close(*runDone)
		if _, err := p.Run(); err != nil {
			fmt.Fprintln(os.Stderr, "tui error:", err)
		}
	}()

	// cleanup: idempotent. First call quits the TUI and blocks until
	// the Run goroutine returns; subsequent calls are no-ops (close
	// of an already-closed channel panics, so guard).
	//
	// cleanup：幂等。首次调用退出 TUI 并阻塞到 Run goroutine 返回；
	// 后续调用空操作（对已关闭的 channel 再 close 会 panic，所以守
	// 卫）。
	var cleanedUp bool
	var cleanupMu sync.Mutex
	cleanup := func() {
		cleanupMu.Lock()
		defer cleanupMu.Unlock()
		if cleanedUp {
			return
		}
		cleanedUp = true
		p.Quit()
		<-*runDone
	}

	// Watcher: if drainCh closes (normal scan completion path),
	// trigger cleanup so the TUI exits promptly. core.RunScan also
	// calls sess.UI.Done() which sends tea.Quit — that path is the
	// primary one; this watcher covers the rare early-return before
	// Done is reached.
	//
	// Watcher：drainCh 关闭（正常扫描完成路径）时触发 cleanup 让 TUI
	// 立即退出。core.RunScan 也会调 sess.UI.Done() 发 tea.Quit——
	// 那条路径是主路径；本 watcher 覆盖 Done 之前的罕见早退场景。
	go func() {
		<-drainCh
		cleanup()
	}()

	return sess, cleanup, nil
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
	// resolveOutputPath may reject user-supplied paths that
	// escape the cwd (Stage 18 / P1#18 / F-05 fix). Fail fast
	// here so we don't half-open some sinks before discovering
	// the rest.
	//
	// resolveOutputPath 可能拒绝跳出 cwd 的用户路径（Stage 18 /
	// P1#18 / F-05 修法）。这里快速失败，避免开了部分 sink 之后
	// 才暴露别的。
	resultTXT, err := resolveOutputPath(cfg, flagOutputTXT, "result.txt")
	if err != nil {
		return fmt.Errorf("output path: %w", err)
	}
	resultJSON, err := resolveOutputPath(cfg, flagOutputJSON, "result.json")
	if err != nil {
		return fmt.Errorf("output path: %w", err)
	}
	credsPath, err := resolveOutputPath(cfg, "", "creds.txt")
	if err != nil {
		return fmt.Errorf("output path: %w", err)
	}
	rdpJSON, err := resolveOutputPath(cfg, "", "rdp.json")
	if err != nil {
		return fmt.Errorf("output path: %w", err)
	}
	rdpTXT, err := resolveOutputPath(cfg, "", "rdp.txt")
	if err != nil {
		return fmt.Errorf("output path: %w", err)
	}
	out, err := output.OpenOutput(output.OutputConfig{
		ResultTXTPath:  resultTXT,
		ResultJSONPath: resultJSON,
		CredsPath:      credsPath,
		RDPJSONPath:    rdpJSON,
		RDPTXTPath:     rdpTXT,
		// P0#2: result.txt gets the redaction gate; creds.txt is
		// always cleartext (operator's working file).
		// P0#2：result.txt 加 redact 门；creds.txt 始终是明文（操作员
		// 工作文件）。
		ShowCleartext: cfg.ShowCleartext,
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
		ShowCleartext:   flagShowCleartext,
		InsecureTLS:     flagInsecureTLS,
		InsecureSSH:     flagInsecureSSH,
		KnownHostsFile:  flagKnownHosts,
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
func resolveOutputPath(cfg *types.Config, flagValue, defaultName string) (string, error) {
	if flagValue != "" {
		return safeOutputPath(flagValue)
	}
	if cfg.Project != "" {
		return filepath.Join("runs", "projects", cfg.Project, defaultName), nil
	}
	return filepath.Join("runs", "default", defaultName), nil
}

// safeOutputPath sanitizes a user-supplied output path. The
// default behaviour is to confine writes to the current
// working directory; the operator can opt out via env var.
//
// safeOutputPath 安全化用户给的输出路径。默认行为把写入范围
// 限制在当前工作目录；操作员可经环境变量 opt-out。
func safeOutputPath(p string) (string, error) {
	clean := filepath.Clean(p)
	// Make the path absolute relative to cwd. / 把路径解析成相对
	// cwd 的绝对路径。
	abs, err := filepath.Abs(clean)
	if err != nil {
		return "", fmt.Errorf("output path %q: %w", p, err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("output path %q: getwd: %w", p, err)
	}
	// Containment check: abs must be cwd or under cwd. We use a
	// trailing separator on cwd so /foo/bar doesn't match
	// /foo/barbaz.
	//
	// 包含检查：abs 必须是 cwd 或在 cwd 之下。我们给 cwd 加尾
	// 部分隔符以防 /foo/bar 误匹配 /foo/barbaz。
	cwdWithSep := cwd
	if !strings.HasSuffix(cwdWithSep, string(os.PathSeparator)) {
		cwdWithSep += string(os.PathSeparator)
	}
	if abs != cwd && !strings.HasPrefix(abs, cwdWithSep) {
		// Opt-out: an operator who really needs to write to
		// /var/log or similar can set the env var. The
		// rationale for env-not-flag: the use case is sysadmin
		// overrides, not operator-button-clicks.
		//
		// Opt-out：操作员真要写 /var/log 等可设环境变量。选环
		// 境而非 flag 的理由：这是 sysadmin 覆写，不是操作员
		// 点按钮。
		if os.Getenv("FG_QIMEN_ALLOW_EXTERNAL_OUTPUT") == "1" {
			return abs, nil
		}
		return "", fmt.Errorf(
			"output path %q resolves to %q which is outside the current working directory %q; "+
				"set FG_QIMEN_ALLOW_EXTERNAL_OUTPUT=1 to override",
			p, abs, cwd)
	}
	return abs, nil
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
