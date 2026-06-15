// Package core orchestrates the scan pipeline.
// Package core 编排扫描管线。
//
// Flow:
//
//	hostiter → alive (core/alive) → portscan (core/scan) →
//	  → [plugin workers: Identify] → output
//	  → [cred scheduler: Credential] → creds.txt
//
// All stages are context-aware. New in v0.1: each stage lives in its
// own focused subpackage (core/alive, core/scan, core/cred) with a
// clean interface and unit tests; scanner.go just glues them together.
//
// 所有阶段都基于 context。v0.1 新设计：每个阶段独立成包（core/alive、
// core/scan、core/cred），接口清晰、有单测；scanner.go 只做装配。
package core

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/core/alive"
	"github.com/LCUstinian/FG-QiMen/internal/core/scan"
	"github.com/LCUstinian/FG-QiMen/internal/session"
	"github.com/LCUstinian/FG-QiMen/internal/types"
)

// RunScan is the main entry point for a single scan invocation.
// RunScan 是单次扫描的主入口。
//
// It wires up the pipeline based on cfg.Mode:
//   - ModeScan / ModeLinked: full pipeline (alive → scan → identify → optional cred)
//   - ModeCrack: skip port scan, run credential tests against known ports
//
// 它根据 cfg.Mode 装配管线。
func RunScan(ctx context.Context, sess *session.Session) (int, error) {
	cfg := sess.Config
	if cfg == nil {
		return 0, fmt.Errorf("nil config")
	}

	sess.UI.Banner(cfg)

	// Expand targets. / 展开目标。
	targets, err := types.ExpandTargets(cfg.Host, cfg.HostsFile)
	if err != nil {
		return 0, fmt.Errorf("expand targets: %w", err)
	}
	if len(targets) == 0 {
		sess.Log.Info("no targets provided; nothing to scan")
		return 0, nil
	}

	// Stage 0: alive (core/alive). / 阶段 0：存活发现。
	aliveOpts := alive.DefaultOptions()
	if cfg.Timeout > 0 {
		aliveOpts.Timeout = cfg.Timeout
	}
	aliveDiscovery := alive.New(aliveOpts)
	aliveRes, _ := aliveDiscovery.Run(ctx, targetAddrs(targets))
	sess.State.Counters.Alive.Store(int64(len(aliveRes.Hits)))
	if len(aliveRes.Hits) > 0 && len(aliveRes.Hits) < len(targets) {
		sess.Log.Info("[*] alive: %d/%d hosts responded", len(aliveRes.Hits), len(targets))
	}
	if cfg.AliveOnly {
		sess.UI.Done(summaryString(sess))
		return 0, nil
	}

	// Channel sizes / 通道容量
	const itemsBuf = 1024

	items := make(chan types.ScanItem, itemsBuf)
	results := make(chan *types.Result, itemsBuf)

	var wg sync.WaitGroup

	// Stage 1: port scan (core/scan). / 阶段 1：端口扫描。
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(items)
		ports, _ := cfg.ParsePorts()
		scanRes := make(chan scan.Result, itemsBuf)
		sc := scan.NewScanner(scan.ScanOptions{
			Probe:      scan.NewTCPConnectProbe(),
			Timeout:    cfg.Timeout,
			Threads:    cfg.Threads,
			MinThreads: 1,
			MaxThreads: 500,
			// P3 / F12 audit fix: surface probe errors (ctx cancel,
			// conn reset, etc.) to the session log instead of
			// silently dropping them. The pool worker records the
			// error here; we don't push a zero-value Result to the
			// output channel.
			//
			// P3 / F12 审计修法：把 probe 错误（ctx cancel、conn
			// reset 等）暴露到 session log，而不是静默丢弃。Pool
			// worker 在此记录；不向输出 channel 推零值 Result。
			OnProbeError: func(_ scan.Item, err error) {
				sess.Log.Warn("scan probe error: %v", err)
			},
		})
		// Run scan in a goroutine; consume results in this one and
		// translate to plugin ScanItems.
		// scan 跑在子 goroutine；本 goroutine 消费并转为 plugin ScanItem。
		//
		// P1#1: defer close(items) ensures the plugin worker pool
		// (stage 2) gets `!ok` on its `for { case item, ok := <-in }`
		// loop and exits on every return path — ctx cancel, scanDone
		// (normal completion), and scanRes close. Previously the
		// producer returned on ctx.Done() / scanDone / !ok without
		// closing items, leaving 16 workers blocked on the never-
		// closed channel; wg.Wait() in this function then hung
		// indefinitely on every normal scan completion, blocking the
		// deferred sess.UI.Done(summary) from ever running.
		//
		// P1#1：defer close(items) 保证 plugin worker 池（阶段 2）能
		// 在 for { case item, ok := <-in } 循环里收到 !ok 并退出——
		// 覆盖 ctx cancel、scanDone（正常完成）、scanRes 关闭三条返回
		// 路径。旧版在 ctx.Done() / scanDone / !ok 路径直接返回而
		// 不 close(items)，导致 16 个 worker 永远阻塞在未关闭通道上；
		// 本函数的 wg.Wait() 也在每次正常完成时无限挂起，阻塞
		// 延迟的 sess.UI.Done(summary) 永远跑不到。
		scanDone := make(chan struct{})
		defer close(items) // see P1#1 above / 见上方 P1#1
		go func() {
			_ = sc.Run(ctx, scan.NewCrossIterator(targetAddrs(targets), ports), scanRes)
			close(scanDone)
		}()
		for {
			select {
			case <-ctx.Done():
				return
			case <-scanDone:
				return
			case r, ok := <-scanRes:
				if !ok {
					return
				}
				if r.State != scan.StateOpen {
					continue
				}
				sess.State.Counters.Ports.Add(1)
				select {
				case items <- types.ScanItem{
					Host:   r.Host,
					Port:   r.Port,
					Banner: r.Banner,
				}:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	// Stage 2: plugin worker pool. / 阶段 2：plugin worker 池。
	workerCount := cfg.Threads
	if workerCount <= 0 {
		workerCount = 200
	}
	if workerCount > 16 {
		workerCount = 16
	}

	var workersWG sync.WaitGroup
	for i := 0; i < workerCount; i++ {
		workersWG.Add(1)
		go func() {
			defer workersWG.Done()
			runPluginWorker(ctx, sess, items, results)
		}()
	}
	go func() {
		workersWG.Wait()
		close(results)
	}()

	// Stage 3: result sink. / 阶段 3：结果汇。
	wg.Add(1)
	go func() {
		defer wg.Done()
		runResultSink(ctx, sess, results)
	}()

	// Periodic stats pusher. / 周期性 stats 推送。
	go pushStats(ctx, sess, 1*time.Second)

	wg.Wait()
	sess.UI.Done(summaryString(sess))
	return 0, nil
}

// targetAddrs extracts the address strings from a []types.Target.
// targetAddrs 从 []types.Target 提取地址字符串。
func targetAddrs(targets []types.Target) []string {
	out := make([]string, len(targets))
	for i, t := range targets {
		out[i] = t.Addr
	}
	return out
}

// summaryString builds a one-line summary printed at end of scan.
// summaryString 构建扫描结束时打印的单行摘要。
func summaryString(sess *session.Session) string {
	c := sess.State.Snapshot()
	return fmt.Sprintf(
		"[*] Done. alive=%d ports=%d results=%d creds=%d errors=%d",
		c.Alive, c.Ports, c.Results, c.Creds, c.Errors)
}

// (P2 dead-code purge: PluginsAll removed in v0.2 audit. Callers
// outside core should import internal/plugins directly and use
// plugins.All().)
// （P2 死代码清理：v0.2 审计删了 PluginsAll。core 外的调用者应直接
// 导入 internal/plugins，用 plugins.All()。）
