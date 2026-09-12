// scan_lifecycle.go — session assembly and hard-exit plumbing for the
// scan pipeline, split out of scan.go (audit L-7) so the orchestrator
// file stays a single screen of named steps.
//
// scan_lifecycle.go — 扫描管线的 session 装配与硬退出设施，从
// scan.go 拆出（审计 L-7），让编排器文件保持在一屏具名步骤内。
package cmd

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/output"
	"github.com/LCUstinian/FG-QiMen/internal/session"
	"github.com/LCUstinian/FG-QiMen/internal/tui"
	"github.com/LCUstinian/FG-QiMen/internal/types"
	"github.com/LCUstinian/FG-QiMen/internal/ui"
	"github.com/LCUstinian/FG-QiMen/internal/workspace"
)

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

	// cleanup: idempotent. First call schedules the TUI force-quit
	// backstop and blocks until the Run goroutine returns; subsequent
	// calls are no-ops (close of an already-closed channel panics, so
	// guard).
	//
	// The force-quit is deliberately deferred past tui.LingerBudget:
	// the normal completion path relies on the doneMsg → linger →
	// self-quit chain inside the model to show the DONE chip and the
	// final summary. An immediate p.Quit() here races that chain and
	// truncates the linger — the exact symptom seen in the /24 live
	// smoke test. The AfterFunc backstop covers paths where Done()
	// never fired (early-error) or the model is wedged; calling
	// p.Quit() on an already-exited program is a safe no-op.
	//
	// cleanup：幂等。首次调用安排 TUI 强制退出兜底并阻塞到 Run
	// goroutine 返回；后续调用空操作（对已关闭 channel 再 close 会
	// panic，所以守卫）。
	//
	// 强制退出刻意推迟到 tui.LingerBudget 之后：正常完成路径依赖
	// model 内部的 doneMsg → linger → 自退链来展示 DONE 芯片与最终
	// 摘要。此处立即 p.Quit() 会与该链竞争并截断 linger——正是 /24
	// 实机冒烟测试看到的症状。AfterFunc 兜底覆盖 Done() 从未触发的
	// 路径（早错）或模型卡死的场景；对已退出的 program 调 p.Quit()
	// 是安全的空操作。
	var cleanedUp bool
	var cleanupMu sync.Mutex
	cleanup := func() {
		cleanupMu.Lock()
		defer cleanupMu.Unlock()
		if cleanedUp {
			return
		}
		cleanedUp = true
		time.AfterFunc(tui.LingerBudget, p.Quit)
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
			"Delete the corrupt fgqm.db to silence this warning.", err)
		return nil
	}
	for _, h := range hashes {
		sess.State.MarkSeen(h)
	}
	sess.Log.Info("[*] resume: loaded %d seen hashes from bbolt", len(hashes))
	return nil
}

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
