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
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/core/alive"
	"github.com/LCUstinian/FG-QiMen/internal/core/scan"
	"github.com/LCUstinian/FG-QiMen/internal/session"
	"github.com/LCUstinian/FG-QiMen/internal/store"
	"github.com/LCUstinian/FG-QiMen/internal/types"
)

// aliveProbesFn returns the alive.Options to use for the discovery
// phase. Tests override this package-level var to inject custom probes
// for deterministic timing; production code leaves it pointing at
// alive.DefaultOptions.
//
// / aliveProbesFn 返回存活发现阶段的 Options。测试覆盖这个包级
// 变量以注入自定义 probe 来获得确定性时序；生产代码保留它指向
// alive.DefaultOptions。
var aliveProbesFn = alive.DefaultOptions

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

	// Periodic UI stats ticker. Started HERE (before mode dispatch
	// + alive discovery), not deeper in the pipeline, so the UI
	// ticks elapsed time during the alive sweep too — otherwise
	// the screen sits idle for the entire ICMP/TCP/system-ping
	// round and looks like nothing is happening ("did it start?").
	// The ticker is fire-and-forget; it auto-exits when ctx fires.
	// / 周期性 UI stats 滴答。在这里启动（mode 派发 + alive 探测之
	// 前），而不是在流水线深处，让 UI 在 alive sweep 期间也走
	// elapsed——否则屏幕在整个 ICMP/TCP/system-ping 回合内空着，看
	// 上去像没动静（"它开始了吗？"）。ticker fire-and-forget；
	// ctx 触发时自动退出。
	go pushStats(ctx, sess, 1*time.Second)

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

	// TUI Spec A (Task 3): record total host count so the TUI can
	// show "alive 2/4" progress instead of just "alive 2". Stored
	// once at scan start so it stays correct if targets is later
	// mutated. / TUI Spec A（Task 3）：记录总主机数，让 TUI 显示
	// "alive 2/4" 而非仅 "alive 2"。在扫描开始时存一次，避免后
	// 续 targets 被改时出错。
	sess.State.TotalHosts.Store(int64(len(targets)))

	// Stage 0: alive (core/alive). / 阶段 0：存活发现。
	// TUI Spec A (Task 3): publish Stage transition so the TUI can
	// render the current phase label. / TUI Spec A（Task 3）：发布
	// Stage 转换，让 TUI 渲染当前阶段标签。
	sess.State.Stage.Store(types.StageAlive)
	aliveOpts := aliveProbesFn()
	if cfg.Timeout > 0 {
		aliveOpts.Timeout = cfg.Timeout
	}
	aliveDiscovery := alive.New(aliveOpts)
	// Surface what we're about to do — alive sweep can take
	// seconds-to-minutes on big targets, and a silent "no counters
	// moving" screen reads as "did anything start?". / 把接下来做
	// 的事情打出来——alive sweep 在大目标上可能要几秒到几分钟，
	// 屏幕静默"计数器不动"看上去像"是不是没启动？"
	sess.Log.Info("[*] alive: probing %d host(s) (timeout %s, threads %d)",
		len(targets), aliveOpts.Timeout, aliveOpts.Threads)
	// v0.5.2: poll Discovery.Progress() into Counters.AliveProbed
	// during the alive sweep so the TUI counter advances instead of
	// sitting at 0 for the whole phase (looked like a hang to
	// operators). The goroutine exits when alive.Run returns.
	//
	// v0.5.2：alive 阶段期间 poll Discovery.Progress() 写入
	// Counters.AliveProbed，让 TUI 计数器随扫描推进而递增而非整个
	// 阶段都停在 0（操作员以为挂了）。alive.Run 返回后 goroutine 退出。
	alivePollCtx, alivePollCancel := context.WithCancel(ctx)
	defer alivePollCancel()
	go func() {
		tick := time.NewTicker(100 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-alivePollCtx.Done():
				return
			case <-tick.C:
				sess.State.Counters.AliveProbed.Store(aliveDiscovery.Progress())
			}
		}
	}()
	aliveRes, _ := aliveDiscovery.Run(ctx, targetAddrs(targets))
	alivePollCancel() // stop the poller before storing final value
	sess.State.Counters.AliveProbed.Store(aliveDiscovery.Progress())
	sess.State.Counters.Alive.Store(int64(len(aliveRes.Hits)))
	if len(aliveRes.Hits) > 0 && len(aliveRes.Hits) < len(targets) {
		sess.Log.Info("[*] alive: %d/%d hosts responded", len(aliveRes.Hits), len(targets))
	}

	// Persist the discovery-stage alive list before any port scan.
	// Previously the alive sink was only fed by WriteResult (open-port
	// results), so on firewalled networks — where most alive hosts
	// yield no open port — fgqm_alive stayed empty even though every
	// host in this map had responded. Sorted for stable output.
	// / 在端口扫描之前持久化发现阶段的存活名单。此前 alive sink 只由
	// WriteResult（open 端口结果）喂数据，在防火墙网络——多数存活主机
	// 没有开放端口——fgqm_alive 始终为空，尽管这里的每台主机都已经响
	// 应。排序保证输出稳定。
	if sess.Out != nil && len(aliveRes.Hits) > 0 {
		aliveHosts := make([]string, 0, len(aliveRes.Hits))
		for h := range aliveRes.Hits {
			aliveHosts = append(aliveHosts, h)
		}
		sort.Strings(aliveHosts)
		for _, h := range aliveHosts {
			hit := aliveRes.Hits[h]
			sess.Out.WriteAliveDiscovery(h, string(hit.Method), hit.Time)
		}
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

	// TUI Spec A (Task 3): publish Stage transition out of alive
	// into port-scan. / TUI Spec A（Task 3）：发布 Stage 从 alive
	// 切到 port-scan。
	sess.State.Stage.Store(types.StagePortScan)

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
	// TUI Spec A (Task 3): record the maximum number of port-scan
	// probes that will be attempted (len(targets) * len(ports)).
	// The channel-based producer/consumer doesn't have a static
	// portScanItems slice, so we use the upper bound — the TUI
	// uses this for "ports X/Y" progress display. / TUI Spec A
	//（Task 3）：记录端口扫描的最大探测数（targets × ports）。基于
	// 通道的 producer/consumer 没有静态 portScanItems slice，所以
	// 取上界——TUI 用来显示 "ports X/Y" 进度。
	sess.State.TotalPorts.Store(int64(len(targets)) * int64(len(ports)))

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
	// TUI Spec A (Task 3): publish Stage transition into IDENTIFY.
	// Set here (just before the worker pool is spawned) so the TUI
	// shows the IDENTIFY label for the duration of plugin dispatch.
	// In ModeScan (no creds) the stage stays at IDENTIFY until done.
	// / TUI Spec A（Task 3）：发布 Stage 到 IDENTIFY。在 worker 池
	// 启动前设置，让 TUI 在 plugin 分发期间显示 IDENTIFY 标签。
	// ModeScan（无 creds）阶段停留在 IDENTIFY 直到结束。
	sess.State.Stage.Store(types.StageIdentify)
	if wantCredential(cfg.Mode) {
		// In modes that exercise cred dispatch (ModeLinked, ModeCrack)
		// the worker pool interleaves identify + cred per item, so
		// the cred "stage" is overlapped with identify in time.
		// Transitioning Stage here at the pool boundary is the
		// closest stable boundary we have. / 在跑凭据派发的 mode
		//（ModeLinked、ModeCrack）里，worker 池在每个 item 上交
		// 错 identify + cred，凭据"阶段"在时间上与 identify 重叠。
		// 在池边界转换 Stage 是我们能找到的最稳的边界。
		sess.State.Stage.Store(types.StageCred)
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

	// (Periodic stats pusher is started in RunScan, BEFORE this
	// function, so the UI ticks during alive discovery too. Don't
	// start a second one here — that would 2x the tick rate.
	// / 周期性 stats 推送器在 RunScan 里、本函数之前已启动，让
	// UI 在 alive 探测期间也走滴答。这里别再起一个，否则 tick
	// 率翻倍。)

	wg.Wait()
	// TUI Spec A (Task 3): publish Stage transition into DONE.
	// Set after wg.Wait() returns so the TUI label flips at the
	// moment all worker goroutines have exited. / TUI Spec A
	//（Task 3）：发布 Stage 到 DONE。在 wg.Wait() 返回后设置，
	// 让 TUI 标签在所有 worker goroutine 都退出后切换。
	sess.State.Stage.Store(types.StageDone)
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
	// TUI Spec A (Task 3): crack mode skips alive+scan but still
	// has a host and port count for the TUI. / TUI Spec A（Task 3）：
	// crack 模式跳过 alive+scan 但仍有 host 与 port 数给 TUI。
	sess.State.TotalHosts.Store(int64(len(targets)))
	sess.State.TotalPorts.Store(int64(len(targets)) * int64(len(ports)))
	// TUI Spec A (Task 3): crack mode jumps straight to IDENTIFY
	// (no StageAlive, no StagePortScan — those stages are skipped).
	// / TUI Spec A（Task 3）：crack 模式直接进入 IDENTIFY（跳过
	// StageAlive、StagePortScan——这两阶段被跳过）。
	sess.State.Stage.Store(types.StageIdentify)
	sess.State.Stage.Store(types.StageCred)
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

	// (Periodic stats pusher is started in RunScan, BEFORE this
	// function, so the UI ticks during alive discovery too. Don't
	// start a second one here — that would 2x the tick rate.
	// / 周期性 stats 推送器在 RunScan 里、本函数之前已启动，让
	// UI 在 alive 探测期间也走滴答。这里别再起一个，否则 tick
	// 率翻倍。)

	wg.Wait()
	// TUI Spec A (Task 3): crack pipeline terminates at StageDone
	// exactly like full pipeline. / TUI Spec A（Task 3）：crack 流
	// 水与 full pipeline 一样在 StageDone 终止。
	sess.State.Stage.Store(types.StageDone)
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

// normalisePluginName lowercases and strips trailing /x or -x
// segments unconditionally (not version-only) so the same plugin
// doesn't show up as 3 rows ("ssh", "SSH", "ssh/2.0"). Called at
// the scanner.go dispatch write site, not in TUI render.
// / normalisePluginName 小写化并去掉尾部 /x 或 -x 段（无条件，不
// 限于版本），让同一个 plugin 不以 3 行显示。在 scanner.go dispatch
// 写入点调用，TUI 渲染不调。
func normalisePluginName(name string) string {
	n := strings.ToLower(strings.TrimSpace(name))
	// Strip trailing "/x" or "-x" segments unconditionally. Both
	// numeric tails (versions like "ssh/2.0", "postgres-15") and
	// non-numeric tails (like "ssh/non-version") get collapsed so
	// the TUI's plugin-breakdown table stays compact. The decision
	// to strip non-numeric tails too was accepted in the Task 3
	// review: the trade-off is "lose one / non-version suffix" vs
	// "let 'ssh' / 'ssh-lite' show as two rows in the top-plugins
	// panel", and the panel's grouping is what users actually
	// wanted. / 去掉尾部 "/x" 或 "-x" 段（无条件）。数字尾（版
	// 本号如 "ssh/2.0"、"postgres-15"）和非数字尾（"ssh/non-
	// version"）都折掉，让 TUI plugin 细分表保持紧凑。Task 3 评审
	// 接受了也剥非数字尾的决定：取舍是"丢一个 / non-version 后缀"
	// vs "让 'ssh' / 'ssh-lite' 在 top-plugins 面板里出现两行"，面
	// 板分组才是用户真正想要的。
	for _, sep := range []string{"/", "-"} {
		if i := strings.Index(n, sep); i >= 0 {
			n = n[:i]
		}
	}
	return n
}

// maxSyncMapEntries caps PluginHits and ErrorCategories at 256 to
// avoid leaking garbage strings from misbehaving plugins. Real max
// in FG-QiMen is ~50 plugins, so the cap is defensive only.
// / maxSyncMapEntries 把 PluginHits 和 ErrorCategories 上限设为
// 256，避免行为不端的 plugin 泄漏垃圾字符串。真实上限约 50。
const maxSyncMapEntries = 256

// bumpSyncMap increments a *atomic.Int64 stored under key in a
// sync.Map. Creates the entry if absent; silently drops the
// increment when the cap is reached. / bumpSyncMap 递增 sync.Map
// 中 key 下存储的 *atomic.Int64。如不存在则创建；达上限时静默
// 丢弃。
func bumpSyncMap(m *sync.Map, key string) {
	if v, ok := m.Load(key); ok {
		if c, ok := v.(*atomic.Int64); ok {
			c.Add(1)
		}
		return
	}
	// Cap check: count current entries before adding new key.
	// / 上限检查：加新 key 前先数当前条目。
	count := 0
	m.Range(func(_, _ any) bool {
		count++
		return true
	})
	if count >= maxSyncMapEntries {
		return // cap reached — drop the increment silently
	}
	v, _ := m.LoadOrStore(key, &atomic.Int64{})
	if c, ok := v.(*atomic.Int64); ok {
		c.Add(1)
	}
}
