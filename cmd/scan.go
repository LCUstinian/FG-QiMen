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
	"time"

	"github.com/spf13/cobra"

	"github.com/LCUstinian/FG-QiMen/internal/core"
	"github.com/LCUstinian/FG-QiMen/internal/core/credential/auth/network"
	"github.com/LCUstinian/FG-QiMen/internal/output"
	"github.com/LCUstinian/FG-QiMen/internal/plugins/adapted/web/webtitle/fingerprint"
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
results to ./runs/default/<YYYY-MM-DD>/fgqm_result.txt and the corresponding
.json in the current directory. Pass --project <name> to switch into
persistent project mode.`,
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
//
// closeOutputForHardExit synchronously flushes + closes the multi-
// format result sinks. Called from preHardExit because os.Exit(1)
// bypasses the deferred sess.Out.Close() in runScan, and the
// in-memory bufio buffers (default 4 KB per sink) plus the SARIF
// document buffer would otherwise be lost. / 在硬退出路径上同步
// flush + close 多格式结果 sink。从 preHardExit 调用，因为
// os.Exit(1) 会绕过 runScan 里 defer 的 sess.Out.Close()，否则
// 内存里的 bufio 缓冲（默认每 sink 4 KB）加上 SARIF 文档缓冲都
// 会丢。
//
// Safe with nil (no-op). Idempotent with Output.Close — the
// existing implementation documents safe-call on a partially-
// initialized Output, and the second call after a successful
// first would no-op the closers (each writes through a
// bufio.Writer that's set to nil post-Close). / 对 nil 安全（空
// 操作）。与 Output.Close 幂等——实现里文档明对部分初始化的
// Output 安全，第二次调用时 closer 会跳过（每个底层 writer
// Close 后会被置 nil）。
//
// We discard the returned error because the next thing that
// happens is os.Exit(1) — there's no operator-readable surface
// to surface the error to, and the alternative (a stderr line
// before exit) would race with the TUI teardown. / 丢弃返错因为
// 接下来就是 os.Exit(1)——没有操作员能读的输出面，stderr 打一行
// 又会和 TUI 拆除抢。
func closeOutputForHardExit(o *output.Output) {
	if o == nil {
		return
	}
	_ = o.Close()
}

func buildSession(ctx context.Context, cfg *types.Config, proj *workspace.Project, drainCh chan struct{}, prog **tui.Program, runDone *chan struct{}) (*session.Session, func(), error) {
	sess, err := session.NewSession(ctx, cfg, cfg.Project)
	if err != nil {
		return nil, nil, fmt.Errorf("session error: %w", err)
	}

	// Wire logger (silent flag suppresses to file-only; -v adds debug).
	//
	// 装配 logger（silent 抑制控制台；-v 开启 debug）。
	//
	// In TUI mode we ALWAYS use the discard logger, regardless of
	// cfg.Silent: the dashboard is the sole event surface and any
	// log line written to stderr will smear across the alt screen
	// and visually duplicate information already shown by the
	// dashboard (status bar counters, LIVE EVENTS column). The
	// TUI user opts into "no log" by running fg-qimen without
	// flags, and into "see logs" by passing --no-tui.
	// TUI 模式下**始终**用 discard logger，与 cfg.Silent 无关：
	// dashboard 是唯一事件面，任何写到 stderr 的日志都会糊在
	// alt screen 上，与 dashboard 已展示的信息（状态条计数、
	// LIVE EVENTS 列）视觉重复。TUI 用户用"不传 flag"表示
	// "不要日志"，用 --no-tui 表示"我要看日志"。
	//
	// The TUI is unaffected by Silent — the dashboard is the live event
	// surface, the logger is the secondary channel; both can be quiet
	// or noisy independently.
	//
	// TUI 不受 Silent 影响——dashboard 是实时事件展示，logger 是次要
	// 通道；两者可以独立地安静或嘈杂。
	if cfg.NoTUI && !cfg.Silent {
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
	//
	// Task 4 (first-batch fixes): when cfg.NoState is true, leave
	// sess.Store = nil regardless of project mode. The earlier
	// path unconditionally called proj.AsStore(), which is a no-op
	// when proj.DB is nil (openPersistent returned DB=nil for the
	// noState branch) but the explicit cfg.NoState check makes
	// intent visible and prevents a future refactor from
	// re-introducing the bbolt open.
	//
	// 第一批修复 Task 4：当 cfg.NoState 为 true 时，无论项目模式
	// sess.Store 一律保持 nil。旧路径无条件调 proj.AsStore()，对
	// proj.DB 为 nil（openPersistent 为 noState 分支返回 DB=nil）
	// 时是空操作，但显式 cfg.NoState 检查让意图可见，并防止未来
	// 重构再次引入 bbolt 打开。
	//
	// Encryption: if cfg.ProjectKey is non-empty, hand it to
	// AsStoreWithPassphrase which runs it through Argon2id (v0.4+) and
	// uses the resulting key to encrypt the JSON payload at rest. New
	// writes use magic 0x03 (Argon2id-derived); old 0x01/0x02 values
	// (SHA-256 KDF) remain readable on the same store. Empty key →
	// plaintext (v0.2.x on-disk format, backward compatible).
	//
	// 加密：若 cfg.ProjectKey 非空，交给 AsStoreWithPassphrase，内部用
	// Argon2id（v0.4+）派生 key 加密 JSON 负载。新写入用 magic 0x03
	// （Argon2id 派生）；旧的 0x01/0x02 值（SHA-256 KDF）同一 store
	// 仍可读。空 key → 明文（v0.2.x 磁盘格式，向后兼容）。
	if cfg.NoState {
		sess.Store = nil
	} else if cfg.ProjectKey != "" && proj.DB != nil {
		sess.Store = proj.AsStoreWithPassphrase(cfg.ProjectKey)
	} else {
		sess.Store = proj.AsStore()
	}

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
// loadResumeState rehydrates the in-memory seen-set from the bbolt
// store. When -resume is set but the bbolt file is corrupt /
// unreadable, we degrade to a warning + fresh run rather than
// aborting the scan — the operator can re-scan from scratch and
// the corrupt DB can be deleted manually. P4.9 (audit roadmap).
//
// loadResumeState 从 bbolt store 重水化内存中的 seen-set。当
// -resume 设置但 bbolt 文件损坏/不可读时，降级为 warning + 重新
// 跑扫描（操作员可从头重扫并手动删损坏 DB）。P4.9（审计路线图）。
func loadResumeState(sess *session.Session, cfg *types.Config) error {
	if !cfg.Resume || sess.Store == nil {
		return nil
	}
	hashes, err := sess.Store.LoadSeenHashes()
	if err != nil {
		sess.Log.Warn("resume: bbolt read failed (%v); continuing with empty seen-set. "+
			"Delete the corrupt fg.db to silence this warning.", err)
		return nil
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
	// Capture the local-time once so all sinks for this run land
	// in the same daily bucket (a scan that crosses midnight
	// doesn't split its results across two folders). / 一次性
	// 抓本地时间，让本次 run 的所有 sink 都进同一日桶（跨午夜
	// 的扫描不会把结果拆到两个目录）。
	now := time.Now()
	// resolveOutputPath may reject user-supplied paths that
	// escape the cwd (Stage 18 / P1#18 / F-05 fix). Fail fast
	// here so we don't half-open some sinks before discovering
	// the rest.
	//
	// resolveOutputPath 可能拒绝跳出 cwd 的用户路径（Stage 18 /
	// P1#18 / F-05 修法）。这里快速失败，避免开了部分 sink 之后
	// 才暴露别的。
	resultTXT, err := resolveOutputPath(cfg, flagOutputTXT, "fgqm_result.txt", now)
	if err != nil {
		return fmt.Errorf("output path: %w", err)
	}
	resultJSON, err := resolveOutputPath(cfg, flagOutputJSON, "fgqm_result.json", now)
	if err != nil {
		return fmt.Errorf("output path: %w", err)
	}
	credsPath, err := resolveOutputPath(cfg, "", "fgqm_creds.txt", now)
	if err != nil {
		return fmt.Errorf("output path: %w", err)
	}
	rdpJSON, err := resolveOutputPath(cfg, "", "fgqm_rdp.json", now)
	if err != nil {
		return fmt.Errorf("output path: %w", err)
	}
	rdpTXT, err := resolveOutputPath(cfg, "", "fgqm_rdp.txt", now)
	if err != nil {
		return fmt.Errorf("output path: %w", err)
	}
	// Alive-host list (one IP per line, deduped). Always on by
	// default — operators pipe it directly into nmap / masscan /
	// curl loops, and there's no harm in writing it (an empty
	// scan produces an empty file). / 存活主机列表（每行一个 IP，
	// 去重）。默认始终开启——操作员直接管道给 nmap / masscan /
	// curl 循环，写个空文件没坏处。
	alivePath, err := resolveOutputPath(cfg, "", "fgqm_alive.txt", now)
	if err != nil {
		return fmt.Errorf("output path: %w", err)
	}
	// CSV is opt-in: only resolve when --output-csv is set. An empty
	// path in OpenOutput disables the sink, so the simplest pattern
	// is to pass the empty string when the flag was not provided.
	//
	// CSV 是 opt-in：只有 --output-csv 设置时才解析。OpenOutput 中
	// 空路径即禁用 sink，所以最简模式是 flag 未提供时传空串。
	var resultCSV string
	if cfg.OutputCSV != "" {
		resultCSV, err = resolveOutputPath(cfg, flagOutputCSV, "fgqm_result.csv", now)
		if err != nil {
			return fmt.Errorf("output path: %w", err)
		}
	}
	// SARIF is opt-in (v0.4): GitHub Code Scanning ingests it natively.
	// / SARIF 是 opt-in（v0.4）：GitHub Code Scanning 原生摄取。
	var resultSARIF string
	if flagOutputSARIF != "" {
		resultSARIF, err = resolveOutputPath(cfg, flagOutputSARIF, "fgqm_result.sarif", now)
		if err != nil {
			return fmt.Errorf("output path: %w", err)
		}
	}
	out, err := output.OpenOutput(output.OutputConfig{
		ResultTXTPath:   resultTXT,
		ResultJSONPath:  resultJSON,
		ResultCSVPath:   resultCSV,
		ResultSARIFPath: resultSARIF,
		CredsPath:       credsPath,
		RotateMaxBytes:  flagOutputRotateBytes,
		RotateMaxFiles:  flagOutputRotateFiles,
		RDPJSONPath:     rdpJSON,
		RDPTXTPath:      rdpTXT,
		ResultAlivePath: alivePath,
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
// buildConfig 把全局 flag 值汇总成 Config 结构。
func buildConfig() (*types.Config, error) {
	cfg := &types.Config{
		Host:             flagHost,
		HostsFile:        flagHostsFile,
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
		Verbose:          flagVerbose,
		ShowCleartext:    flagShowCleartext,
		InsecureTLS:      flagInsecureTLS,
		InsecureSSH:      flagInsecureSSH,
		KnownHostsFile:   flagKnownHosts,
		ShutdownTimeout:  flagShutdownTime,
		Plugins:          flagPlugins,
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// openProject opens a project workspace (ephemeral or persistent).
// Task 4 (first-batch fixes): honours cfg.NoState by passing it
// through to workspace.OpenWithOptions, so a `--no-state` invocation
// on a named project skips the bbolt open and the runs/projects/<name>/
// directory creation entirely. Without this, `--no-state` was dead
// code: the flag was wired through cfg.NoState but the production
// path unconditionally called proj.AsStore(), which forced a bbolt
// open in workspace.Open.
//
// openProject 打开项目工作区（即扫即走 / 增量扫描）。
// 第一批修复 Task 4：通过 workspace.OpenWithOptions 兑现 cfg.NoState，
// 让对命名项目的 `--no-state` 调用完全跳过 bbolt 打开和
// runs/projects/<name>/ 目录创建。否则 `--no-state` 是死代码：flag
// 通过 cfg.NoState 传递，但生产路径无条件调 proj.AsStore()，迫使
// workspace.Open 打开 bbolt。
func openProject(cfg *types.Config) (*workspace.Project, error) {
	return workspace.OpenWithOptions(cfg.Project, workspace.OpenOptions{NoState: cfg.NoState})
}

// resolveOutputPath resolves a possibly-empty output path to a default
// inside the project root (project mode) or the ./runs/default/
// directory (ephemeral mode), bucketed by the local-date `now`
// (YYYY-MM-DD) and stamped on the filename with HH-MM-SS so
// multiple runs on the same day don't overwrite each other.
// User-supplied paths via -o / -j bypass the bucketing AND the
// stamp (operators who pass an explicit path want their exact
// path, not an auto-decorated one).
//
// resolveOutputPath 把可能为空的输出路径解析为默认值，按 `now`
// 的本地日期（YYYY-MM-DD）分桶，文件名再加 HH-MM-SS 时间戳：
//   - 项目模式：./runs/projects/<name>/<YYYY-MM-DD>/<file>_<HH-MM-SS>
//   - 即扫即走：./runs/default/<YYYY-MM-DD>/<file>_<HH-MM-SS>
//   - 显式 -o / -j：原样返回（不分桶 + 不加时间戳）
//
// Why bucket by date + stamp by time: an operator who runs
// fg-qimen every day against the same project would otherwise
// see one day's results clobber the previous day's (date bucket
// fixes that), AND two runs on the same day would clobber each
// other (timestamp suffix fixes that). Bucketing by local-date
// gives the operator a per-day audit trail in the same project
// root, the HH-MM-SS stamp gives per-run isolation within a day,
// while fg.db (the persistent state / dedup DB) stays at the
// project root and is shared across all runs. / 为什么按日分桶
// + 文件加时间戳：操作员每天对同一项目跑 fg-qimen 时，结果文件
// 会互相覆盖（日桶解决这个），同一天多次跑也会互相覆盖（时
// 间戳后缀解决这个）。按本地日分桶给同项目根保留每日审计轨迹，
// HH-MM-SS 给同日内每次 run 隔离，fg.db 保持在项目根跨所有
// run 共享。
func resolveOutputPath(cfg *types.Config, flagValue, defaultName string, now time.Time) (string, error) {
	if flagValue != "" {
		return safeOutputPath(flagValue)
	}
	day := dailyRunSubdir(now)
	stamped := stampFileName(defaultName, now)
	if cfg.Project != "" {
		return filepath.Join("runs", "projects", cfg.Project, day, stamped), nil
	}
	return filepath.Join("runs", "default", day, stamped), nil
}

// dailyRunSubdir formats `t` as the YYYY-MM-DD bucket name used
// under each project / ephemeral root. The format is intentionally
// fixed-width ISO so directory listings sort chronologically. The
// caller is expected to pass a local-time `t` (we don't pin a TZ
// here — operators reason in local time and the daily bucket name
// should match their calendar day).
//
// dailyRunSubdir 把 `t` 格式化为项目 / 即扫即走根下用的 YYYY-MM-DD
// 桶名。格式固定 ISO 让目录列表按时间排序。调用方传本地时间 `t`
// （不固定 TZ——操作员按本地时区想，日桶名应匹配他们日历日）。
func dailyRunSubdir(t time.Time) string {
	return t.Format("2006-01-02")
}

// stampFileName inserts the run timestamp between the base and
// the extension of `name`. The timestamp is local HH-MM-SS,
// dash-separated (Windows doesn't allow `:` in filenames, and
// the dashes match the YYYY-MM-DD style for visual consistency).
// "fgqm_result.txt" at 14:30:22 → "fgqm_result_14-30-22.txt".
// No extension → suffix appended raw. / stampFileName 在文件名
// 基与扩展名之间插入运行时间戳。本地 HH-MM-SS，连字符分隔
// （Windows 不允许文件名带冒号，连字符与 YYYY-MM-DD 风格一致）。
// 无扩展名则直接追加。
func stampFileName(name string, t time.Time) string {
	ts := t.Format("15-04-05")
	ext := filepath.Ext(name)
	base := strings.TrimSuffix(name, ext)
	return base + "_" + ts + ext
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
