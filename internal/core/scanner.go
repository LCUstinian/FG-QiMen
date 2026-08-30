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
	"github.com/LCUstinian/FG-QiMen/internal/store"
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

	// v0.4 Phase 2.1: dispatch by mode. The previous code
	// unconditionally ran alive + scan + plugins for every mode
	// and used wantIdentify/wantCredential to gate the plugin
	// stage only. In ModeCrack the user already has a target:port
	// list (either from --project + bbolt or from --hosts-file +
	// --ports); running alive + a fresh port scan was pure
	// overhead. The ModeCrack pipeline skips both stages and
	// goes straight to plugin Identify + Credential on the
	// supplied pairs. / v0.4 Phase 2.1：按 mode 派发。旧代码
	// 无条件跑 alive + scan + plugins，只在 plugin 层用
	// wantIdentify/wantCredential 挡。ModeCrack 下用户已经
	// 有 target:port 列表（--project + bbolt 或
	// --hosts-file + --ports），跑 alive + 端口扫描是纯浪费。
	// ModeCrack 流水线跳过这两步，直接对给定的 target:port 跑
	// plugin Identify + Credential。
	switch cfg.Mode {
	case types.ModeScan, types.ModeLinked:
		return runFullPipeline(ctx, sess)
	case types.ModeCrack:
		return runCrackPipeline(ctx, sess)
	default:
		return 0, fmt.Errorf("unknown mode: %q", cfg.Mode)
	}
}

// runFullPipeline is the original alive → scan → identify →
// optional credential pipeline, used for ModeScan and ModeLinked.
// / runFullPipeline 是原 alive → scan → identify → 可选 credential
// 流水线，给 ModeScan 和 ModeLinked 用。
func runFullPipeline(ctx context.Context, sess *session.Session) (int, error) {
	cfg := sess.Config

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

	// Wire the bbolt batched writer when persistence is enabled and
	// the operator hasn't explicitly disabled it. The BatchWriter
	// goroutine flushes every DefaultBatchInterval (200ms) or
	// DefaultBatchSize (32) ops, whichever comes first — amortising
	// the per-write fsync. / 当启用持久化且操作员未显式禁用时，
	// 接入 bbolt 批量写。BatchWriter goroutine 按 DefaultBatchInterval
	// （200ms）或 DefaultBatchSize（32）刷盘，摊销每次写的 fsync。
	if sess.Store != nil && !cfg.NoBatch {
		bw := store.NewBatchWriter(sess.Store, store.DefaultBatchSize, store.DefaultBatchInterval)
		sess.BatchWriter = bw
		defer bw.Stop()
		go bw.Run(ctx)
	}

	if cfg.AliveOnly {
		sess.UI.Done(summaryString(sess))
		return 0, nil
	}

	// Channel sizes / 通道容量
	items := make(chan types.ScanItem, DefaultChannelBuffer)
	results := make(chan *types.Result, DefaultChannelBuffer)

	var wg sync.WaitGroup

	// Stage 1: port scan (core/scan). / 阶段 1：端口扫描。

	// Task 3 (first-batch fixes): resolve the effective port list
	// (include + exclude) ONCE here, before any goroutine starts,
	// instead of inside the worker goroutine. Previously:
	//   - ParsePorts was called from inside the goroutine and its
	//     error was only logged, so a bad --ports value silently
	//     ran a 0-port scan ("no findings").
	//   - ExcludePorts was stored in cfg but never read — the
	//     `-exclude-ports` flag had no effect on the scan.
	// ResolvePorts closes both gaps and propagates errors up to
	// RunScan so a config typo aborts before any worker starts.
	//
	// 第一批修复 Task 3：在任何 goroutine 启动前一次性解析有效端口列
	// 表（include + exclude），而非放在 worker goroutine 里。旧行为：
	//   - ParsePorts 在 goroutine 里调用，error 只 log，坏的 --ports
	//     静默跑 0 端口扫描（"无发现"）。
	//   - ExcludePorts 存进 cfg 但从不读取——`-exclude-ports` flag 对
	//     扫描完全无效。
	// ResolvePorts 同时修补两处缺口，并把错误向上传到 RunScan，让配
	// 置拼错在任何 worker 启动前中止。
	ports, err := cfg.ResolvePorts()
	if err != nil {
		return 0, fmt.Errorf("resolve ports: %w", err)
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		// P1#1 + C1 audit fix: single defer close(items) at the
		// bottom (line ~128) covers all return paths. The earlier
		// duplicate defer close(items) here would double-close and
		// panic on every scan. Removed in the v0.2 audit.
		//
		// P1#1 + C1 审计修法：底部（约 128 行）唯一的 defer close(items)
		// 覆盖所有返回路径。此处早先的重复 defer close(items) 会在每
		// 次扫描时 double-close 并 panic。v0.2 审计删除。
		scanRes := make(chan scan.Result, DefaultChannelBuffer)
		sc := scan.NewScanner(scan.ScanOptions{
			Probe:      scan.NewTCPConnectProbe(),
			Timeout:    cfg.Timeout,
			Threads:    cfg.Threads,
			MinThreads: DefaultMinThreads,
			MaxThreads: DefaultMaxThreads,
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
		workerCount = DefaultPluginWorkers
	}
	maxWorkers := cfg.MaxPluginWorkers
	if maxWorkers <= 0 {
		maxWorkers = DefaultPluginWorkers
	}
	if workerCount > maxWorkers {
		workerCount = maxWorkers
	}

	// Phase 2.8 (audit roadmap): build the creds slice once instead
	// of in every worker goroutine. N workers × N cred pairs used
	// to be N Cartesian products. / Phase 2.8（审计路线图）：凭据 slice
	// 只构建一次，不再每个 worker goroutine 各算一次。N worker × N
	// 凭据对以前是 N 次笛卡尔积。
	//
	// Task 2 (first-batch fixes): a loader error (unreadable user/pass
	// file, exceeded MaxUsers / MaxPasses / MaxCredPairs) must abort
	// RunScan before any worker goroutine starts — running a port scan
	// with an empty cred slice silently yields zero auth attempts, which
	// looks like a "scan found no vulnerabilities" instead of a config
	// typo. / 第一批修复 Task 2：loader 错误（不可读 user/pass 文件、
	// 超过 MaxUsers / MaxPasses / MaxCredPairs）必须在任何 worker
	// goroutine 启动前中止 RunScan——空 cred slice 跑端口扫描会静默
	// 得零次认证，看起来像"扫描未发现漏洞"而非配置拼错。
	//
	// v0.4: load creds lazily — only when the current mode actually
	// needs them. ModeScan runs Identify only and never consults
	// creds, so loading + holding a cred slice wastes memory and
	// file I/O. The loader's error semantics (abort before workers
	// start) are preserved because the worker pool is only spawned
	// below this point. / v0.4：懒加载凭据——仅当当前模式实际要用
	// 时才加载。ModeScan 只跑 Identify，从不查 creds，加载+持有
	// cred slice 浪费内存和文件 I/O。loader 的错误语义（worker 启
	// 动前中止）仍保留，因为 worker 池在此后才生成。
	var creds []types.Cred
	if wantCredential(cfg.Mode) {
		var err error
		creds, err = loadCreds(sess)
		if err != nil {
			return 0, fmt.Errorf("load credentials: %w", err)
		}
	}
	var workersWG sync.WaitGroup
	for i := 0; i < workerCount; i++ {
		workersWG.Add(1)
		go func() {
			defer workersWG.Done()
			runPluginWorker(ctx, sess, creds, items, results)
		}()
	}
	// P2-2 (audit): wg.Wait() below must cover the worker pool so a
	// future drift in the producer's defer-close(items) cannot leave
	// wg.Wait() hanging on a still-active worker pool. The closer
	// goroutine is registered with the outer wg, so workersWG.Wait()
	// → close(results) is part of wg.Wait()'s completion. / P2-2
	// （审计）：下方 wg.Wait() 必须覆盖 worker 池，否则 producer 的
	// defer-close(items) 漂移会让 wg.Wait() 在仍活跃的 worker 池上
	// 挂起。close goroutine 注册到外层 wg，workersWG.Wait() →
	// close(results) 是 wg.Wait() 完成的一部分。
	wg.Add(1)
	go func() {
		defer wg.Done()
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

// runCrackPipeline runs only the plugin Identify + Credential
// stages on a pre-known target:port list. No alive probe, no
// port scan. The list comes from one of:
//   - --hosts-file + --ports (CLI flags, ephemeral)
//   - --project + bbolt-seen-set (persistent, repeated crack)
//
// Skipping alive+scan saves ~1 TCP connect per target per port
// in typical use (a 256-host /24 × 6-port crack skips 1536
// redundant connects per the old code). / runCrackPipeline 仅
// 在预先已知的 target:port 列表上跑 plugin Identify + Credential。
// 不跑 alive 探活、不跑端口扫描。列表来源：
//   - --hosts-file + --ports（CLI flag，即扫即走）
//   - --project + bbolt seen-set（持久化，重复 crack）
//
// 跳过 alive + scan 在典型场景下省 ~1 TCP connect/host/port
// （256-host /24 × 6-port 的 crack 比旧代码少 1536 次冗余连接）。
func runCrackPipeline(ctx context.Context, sess *session.Session) (int, error) {
	cfg := sess.Config

	// Wire the bbolt batched writer (same as full pipeline — crack
	// mode hits are exactly the thing worth persisting). / 接
	// bbolt 批量写（与 full pipeline 同——crack 模式命中就是
	// 值得持久化的东西）。
	if sess.Store != nil && !cfg.NoBatch {
		bw := store.NewBatchWriter(sess.Store, store.DefaultBatchSize, store.DefaultBatchInterval)
		sess.BatchWriter = bw
		defer bw.Stop()
		go bw.Run(ctx)
	}

	// Resolve the target:port list. / 解析 target:port 列表。
	targets, err := types.ExpandTargets(cfg.Host, cfg.HostsFile)
	if err != nil {
		return 0, fmt.Errorf("expand targets: %w", err)
	}
	ports, err := cfg.ResolvePorts()
	if err != nil {
		return 0, fmt.Errorf("resolve ports: %w", err)
	}
	if len(targets) == 0 || len(ports) == 0 {
		sess.Log.Info("crack mode: empty targets or ports; nothing to crack")
		sess.UI.Done(summaryString(sess))
		return 0, nil
	}
	// The CrossIterator produces the Cartesian product host × port.
	// We feed it into the same `items` channel the full pipeline
	// uses so the plugin worker + result sink code path is shared.
	// / CrossIterator 生成 host × port 笛卡尔积。喂到与 full
	// pipeline 相同的 `items` 通道，共享 plugin worker + result
	// sink 代码路径。
	items := make(chan types.ScanItem, DefaultChannelBuffer)
	results := make(chan *types.Result, DefaultChannelBuffer)

	var wg sync.WaitGroup

	// Crack mode: NO port scan, NO alive probe. We feed the
	// items channel directly from the iterator. / Crack 模式：
	// 不跑端口扫描、不跑 alive 探活。直接从迭代器喂 items 通道。
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(items)
		it := scan.NewCrossIterator(targetAddrs(targets), ports)
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			item, ok := it.Next()
			if !ok {
				return
			}
			sess.State.Counters.Ports.Add(1)
			select {
			case items <- types.ScanItem{Host: item.Host, Port: item.Port}:
			case <-ctx.Done():
				return
			}
		}
	}()

	// Stage 2: plugin worker pool — same as full pipeline.
	// / 阶段 2：plugin worker 池——同 full pipeline。
	workerCount := cfg.Threads
	if workerCount <= 0 {
		workerCount = DefaultPluginWorkers
	}
	maxWorkers := cfg.MaxPluginWorkers
	if maxWorkers <= 0 {
		maxWorkers = DefaultPluginWorkers
	}
	if workerCount > maxWorkers {
		workerCount = maxWorkers
	}

	// Crack mode ALWAYS needs creds (it's the whole point of
	// the mode). / Crack 模式总是需要 creds（这是模式的意义）。
	creds, err := loadCreds(sess)
	if err != nil {
		return 0, fmt.Errorf("load credentials: %w", err)
	}
	var workersWG sync.WaitGroup
	for i := 0; i < workerCount; i++ {
		workersWG.Add(1)
		go func() {
			defer workersWG.Done()
			runPluginWorker(ctx, sess, creds, items, results)
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		workersWG.Wait()
		close(results)
	}()

	// Stage 3: result sink — same as full pipeline.
	// / 阶段 3：结果汇——同 full pipeline。
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
