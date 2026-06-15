// cmd/signal.go — graceful-shutdown signal handler. Split out from
// scan.go (the v0.2 audit's cmd-scan-god-file finding) so the
// signal-state machine lives on its own and can be tested
// independently of session / output / config wiring.
//
// Behaviour:
//   - First SIGINT/SIGTERM: cancel ctx, start drain timer.
//   - drainCh closes within timeout → exit 0 (caller's defer chain).
//   - Second SIGINT within drain window → os.Exit(1) after
//     invoking preHardExit.
//   - Drain timeout → os.Exit(1) after invoking preHardExit.
//   - Normal completion (drainCh closes first) → silent exit.
//
// preHardExit runs synchronously before os.Exit(1) so the TUI can
// release its altscreen. See cmd/scan.go for the prog/runDone
// wiring that the closure dereferences.
//
// cmd/signal.go — 优雅退出 signal handler。从 scan.go 拆出（v0.2 审
// 计的 cmd-scan-god-file finding），让信号状态机独立，便于独立测
// 试，不依赖 session / output / config 装配。
//
// 行为：
//   - 首次 SIGINT/SIGTERM：取消 ctx，启动 drain 计时。
//   - drainCh 在 timeout 内关闭 → exit 0（调用方的 defer 链）。
//   - drain 期间收到第二次 SIGINT → 调用 preHardExit 后 os.Exit(1)。
//   - Drain 超时 → 调用 preHardExit 后 os.Exit(1)。
//   - 正常完成（drainCh 先关闭）→ 静默退出。
//
// preHardExit 在 os.Exit(1) 前同步调，让 TUI 释放 altscreen。prog /
// runDone 闭包引用见 cmd/scan.go。
package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

// installSignalHandler wires SIGINT/SIGTERM into a graceful-shutdown
// pipeline. See package doc above for behaviour.
//
// installSignalHandler 把 SIGINT/SIGTERM 接入优雅退出管线。
// 行为见包级文档。
func installSignalHandler(timeout time.Duration, preHardExit func()) (context.Context, context.CancelFunc, chan struct{}) {
	ctx, cancel := context.WithCancel(context.Background())
	drainCh := make(chan struct{})
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	// Release the signal handler slot when the goroutine exits so a
	// long-lived test harness doesn't leak the underlying signal fd.
	// 在 goroutine 退出时释放 signal handler 槽位，避免长时间运行的
	// 测试 harness 泄漏底层的 signal fd。
	defer signal.Stop(sigs)

	// Guard the hard-exit path so it runs at most once even if both
	// the second-signal and drain-timeout cases somehow race (they
	// shouldn't, but the cost of a sync.Once is negligible here).
	// 守卫硬退出路径：即便第二次信号和 drain 超时两个 case 出现竞争
	// （理论上不会），至多执行一次。sync.Once 的开销可忽略。
	var hardExitOnce sync.Once
	hardExit := func(reason string) {
		hardExitOnce.Do(func() {
			fmt.Fprintln(os.Stderr, reason)
			if preHardExit != nil {
				preHardExit()
			}
			os.Exit(1)
		})
	}

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
				hardExit("[!] Second interrupt received, forcing exit")
			case <-time.After(timeout):
				// Drain timed out: hard exit.
				// 排空超时：强退。
				hardExit("[!] Drain timed out, forcing exit")
			}
		case <-drainCh:
			// Normal completion; nothing to do.
			// 正常完成。
		}
	}()
	return ctx, cancel, drainCh
}
