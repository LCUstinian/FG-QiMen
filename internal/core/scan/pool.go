// Package scan: bounded-concurrency worker pool with AIMD sizing.
// Package scan: 有界并发 worker 池，AIMD 自适应调并发。
//
// Pool pulls Items from an Iterator, dispatches them to N workers
// (each calling the Probe), and sends Results to an out channel.
//
// The adaptive controller (borrowed from fscan's AdaptivePool) runs a
// two-phase policy driven by PoolMetrics (metrics.go):
//
//  1. Slow start: from target/4, double per healthy interval until
//     the env-tuned target — no burst of MaxThreads connects at t=0.
//  2. Steady-state AIMD: additive increase (+target/20) while
//     healthy, multiplicative decrease (×0.85 stressed / ×0.5
//     congested) on real overload signals — resource-exhaustion rate
//     and fast/slow EMA RTT trend.
//
// What the controller deliberately does NOT do (the avalanche
// lessons): it never shrinks on a high filtered/closed ratio (that is
// the target network's posture, not scanner overload — shrinking on
// it collapsed concurrency and stretched /24 sweeps into hours), and
// steady-state growth beyond the env-tuned target requires real
// responses: an interval with no open and no refusal carries no
// ground truth, so Good is demoted to OK there.
//
// Pool 从 Iterator 拉 Item，分发给 N 个 worker（每个调 Probe），
// 把 Result 发到 out channel。自适应控制器（借鉴 fscan 的
// AdaptivePool）分两阶段，由 PoolMetrics（metrics.go）驱动：
//
//  1. 慢启动：从 target/4 起步，健康周期内逐周期翻倍至环境调优的
//     target——避免 t=0 直接打出 MaxThreads 量级的连接。
//  2. 稳态 AIMD：健康时加性增（+target/20），真实过载信号上乘性
//     减（有压力 ×0.85 / 拥塞 ×0.5）——信号为资源耗尽率与 fast/slow
//     双 EMA RTT 趋势。
//
// 控制器刻意不做的事（雪崩教训）：绝不因 filtered/closed 比例高而
// 缩容（那是目标网络的姿态，不是扫描器过载——据此缩容曾把并发塌
// 缩、把 /24 扫描拖成数小时）；稳态下要超出环境调优 target 的增长
// 必须有真实响应：无 open 无 refused 的周期不携带网络真值，此时
// Good 降级为 OK。
package scan

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// PoolOptions configures Pool. / PoolOptions 配置 Pool。
type PoolOptions struct {
	// Probe is the technique used to test each (host, port).
	// Probe 是探测每个 (host, port) 的技术。
	Probe Probe

	// Timeout is the per-probe timeout. / Timeout 是每个 probe 的超时。
	Timeout time.Duration

	// MinThreads and MaxThreads bound the concurrency. / MinThreads
	// 和 MaxThreads 限制并发的上下界。
	MinThreads int
	MaxThreads int

	// InitialThreads is the starting concurrency. If zero, defaults
	// to MaxThreads. / InitialThreads 起始并发；零 = MaxThreads。
	InitialThreads int

	// AdjustInterval is how often adaptive sizing re-evaluates.
	// Default 500ms. / AdjustInterval 自适应调并发的评估周期；默认 500ms。
	AdjustInterval time.Duration

	// OnProbeError is invoked when a probe returns a transport-layer
	// error (ctx cancel, conn reset, etc.) that the pool chose not
	// to record or push downstream. nil = silent; non-nil = the
	// caller (typically core/scanner.go) logs via its session.Log.
	// The audit (P3 / F12) flagged the previous silent-discard as
	// hiding misconfigured probes — this hook restores visibility
	// without coupling Pool to the Log interface.
	//
	// OnProbeError 在 probe 返回传输层错误（ctx cancel、conn reset
	// 等）时被调，Pool 选择不记录也不向下游推。nil = 静默；非 nil =
	// 调用方（通常是 core/scanner.go）通过 session.Log 记日志。审计
	// （P3 / F12）把旧的静默丢弃标为隐藏配错的 probe——本 hook 在
	// 不把 Pool 和 Log 接口耦合的前提下恢复可见性。
	OnProbeError func(item Item, err error)

	// Env classifies the target network for the controller's health
	// thresholds (see metrics.go). Zero value maps to the WAN set.
	// Wired from the env profile (core.NetworkEnv) by core/scanner.go.
	// / Env 为控制器的健康阈值刻画目标网络（见 metrics.go）。零值映
	// 射到 WAN 档。由 core/scanner.go 从环境画像（core.NetworkEnv）
	// 映射接线。
	Env Env

	// Adaptive, when non-nil, replaces the fixed Timeout with the
	// RTT-derived mean+4σ value per probe, and is fed RTT samples
	// from every probe that actually reached a host (open / closed).
	// Probes that only waited out a deadline (filtered, or silent
	// UDP with RTT=0) never feed it. Nil = fixed timeout (default).
	// / Adaptive 非 nil 时，把固定 Timeout 替换为按 RTT 推导的
	// mean+4σ 值（每个 probe 各取一次），并由每个真正到达主机的
	// probe（open / closed）喂 RTT 样本。只等满超时的探测（filtered、
	// 静默 UDP 的 RTT=0）绝不喂入。nil = 固定超时（默认）。
	Adaptive *AdaptiveTimeout

	// Tuning overrides the AIMD policy constants (see aimd.go).
	// Nil = shipped defaults; zero fields inside a non-nil struct
	// keep their defaults. Measurement surface for the A3 bench
	// sweep — not an operator surface.
	// / Tuning 覆写 AIMD 策略常量（见 aimd.go）。nil = 出厂默认；
	// 非 nil 结构体内的零值字段保持各自默认。A3 bench 扫描的测量表
	// 面——非操作员表面。
	Tuning *AIMDTuning

	// InflightSink, when non-nil, mirrors the pool's in-flight probe
	// count (+1 on acquire, −1 on release). The TUI v3 spec (§7.3)
	// wires it to types.State.Inflight so the PROGRESS ledger can
	// split ports into done / inflight / deferred. nil = unwired;
	// the pool keeps its own counter regardless, so Pool.Inflight()
	// always works.
	// / InflightSink 非 nil 时镜像池的在飞 probe 数（获取 +1，释放
	// −1）。TUI v3 规格（§7.3）把它接到 types.State.Inflight，让
	// PROGRESS 账本把 ports 分解为 done / inflight / deferred。
	// nil = 未接线；池自身计数器无论如何都在，Pool.Inflight() 恒可用。
	InflightSink *atomic.Int64
}

// DefaultPoolOptions returns a PoolOptions with sensible defaults.
// DefaultPoolOptions 返回带合理默认的 PoolOptions。
func DefaultPoolOptions(probe Probe) PoolOptions {
	return PoolOptions{
		Probe:          probe,
		Timeout:        3 * time.Second,
		MinThreads:     10,
		MaxThreads:     500,
		InitialThreads: 200,
		AdjustInterval: 500 * time.Millisecond,
	}
}

// Pool is the worker pool. Construct with NewPool and call Run().
// Pool 是 worker 池。用 NewPool 构造并调用 Run()。
type Pool struct {
	opts PoolOptions

	// currentThreads is the current concurrency level, manipulated
	// by the adaptive controller. / currentThreads 是当前并发级，
	// 由自适应控制器调节。
	currentThreads atomic.Int32

	// tuning is opts.Tuning with defaults resolved — read-only after
	// NewPool. / tuning 是解析默认后的 opts.Tuning——NewPool 后只读。
	tuning AIMDTuning

	// Controller state + shared signals. Controller-only fields
	// (target, inSlowStart, prevSnap) are touched exclusively by the
	// single adaptiveLoop goroutine, so they need no atomics.
	// / 控制器状态 + 共享信号。控制器专属字段（target、inSlowStart、
	// prevSnap）只被唯一的 adaptiveLoop goroutine 触碰，无需 atomic。
	metrics PoolMetrics
	target  int32
	// inSlowStart: doubling phase toward target. / 慢启动：向 target
	// 翻倍阶段。
	inSlowStart bool
	prevSnap    MetricsSnapshot

	// busyTimer is reused across busy-wait iterations to avoid
	// allocating a fresh Timer on every time.After call. / busyTimer
	// 跨 busy-wait 迭代复用，避免每次 time.After 分配 Timer。
	busyTimer *time.Timer

	// inflight is the number of probes currently running (spec §7.3).
	// It lives on the Pool — not as a Run-local — so the in-flight
	// count survives across Run calls (UDP reuses the Scanner pattern
	// with a fresh pool anyway) and Pool.Inflight() can expose it for
	// tests and the optional InflightSink mirror.
	// / inflight 是当前在跑的 probe 数（spec §7.3）。放在 Pool 上而
	// 非 Run 局部——让在飞数跨 Run 调用存续（UDP 用独立 pool 反正无
	// 复用），并让 Pool.Inflight() 能为测试与可选的 InflightSink 镜
	// 像暴露它。
	inflight atomic.Int32
}

// Inflight returns the number of probes currently in flight.
// / Inflight 返回当前在飞的 probe 数。
func (p *Pool) Inflight() int32 { return p.inflight.Load() }

// NewPool constructs a Pool. InitialThreads is the AIMD target: the
// pool is born at max(floor, target/4) and doubles up healthy
// intervals until it reaches target, then switches to steady-state
// AIMD bounded by MaxThreads. / NewPool 构造一个 Pool。
// InitialThreads 是 AIMD 的 target：池以 max(floor, target/4) 出生，
// 健康周期内逐周期翻倍至 target，随后切换为 MaxThreads 约束下的
// 稳态 AIMD。
func NewPool(opts PoolOptions) *Pool {
	if opts.Probe == nil {
		opts.Probe = NewTCPConnectProbe()
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 3 * time.Second
	}
	if opts.MinThreads <= 0 {
		opts.MinThreads = 1
	}
	if opts.MaxThreads < opts.MinThreads {
		opts.MaxThreads = opts.MinThreads
	}
	if opts.InitialThreads <= 0 || opts.InitialThreads > opts.MaxThreads {
		opts.InitialThreads = opts.MaxThreads
	}
	if opts.AdjustInterval <= 0 {
		opts.AdjustInterval = 500 * time.Millisecond
	}
	p := &Pool{
		opts: opts,
		// Stop the timer immediately so it doesn't fire on the
		// first Reset. / 立即停止 timer，避免首次 Reset 前就触发。
		busyTimer: time.NewTimer(0),
		tuning:    opts.Tuning.resolved(),
	}
	if !p.busyTimer.Stop() {
		<-p.busyTimer.C
	}
	p.target = int32(opts.InitialThreads)
	start := p.slowStartStart()
	p.currentThreads.Store(start)
	p.inSlowStart = start < p.target
	return p
}

// slowStartStart computes the birth concurrency: target/SlowStartDiv,
// floored by the pool floor. / slowStartStart 计算出生并发：
// target/SlowStartDiv，以池下限兜底。
func (p *Pool) slowStartStart() int32 {
	start := p.target / int32(p.tuning.SlowStartDiv)
	if floor := p.floorThreads(); start < floor {
		start = floor
	}
	if start > p.target {
		start = p.target
	}
	return start
}

// floorThreads is the lowest concurrency the controller will ever
// set: max(MinThreads, MaxThreads/20). The MinThreads half honors the
// operator/env floor (DefaultMinThreads=50 came from the avalanche
// lesson); the MaxThreads/20 half is fscan's proportional bottom.
// / floorThreads 是控制器允许的最低并发：max(MinThreads,
// MaxThreads/20)。前者尊重操作员/环境下限（DefaultMinThreads=50 源
// 自雪崩教训）；后者是 fscan 的比例下限。
func (p *Pool) floorThreads() int32 {
	floor := int32(p.opts.MinThreads)
	if prop := int32(p.opts.MaxThreads / 20); prop > floor {
		floor = prop
	}
	if floor < 1 {
		floor = 1
	}
	return floor
}

// resetTimer is a small wrapper that drains a stopped timer's
// channel and resets it to fire after d. Used by the busy-wait loop
// to avoid the per-iteration time.After allocation. / resetTimer 是
// 排空已停 timer channel 并重置为 d 后触发的薄包装。busy-wait 循环
// 用它避免每次迭代 time.After 分配。
func resetTimer(t *time.Timer, d time.Duration) {
	if !t.Stop() {
		select {
		case <-t.C:
		default:
		}
	}
	t.Reset(d)
}

// Run consumes items from iter, dispatches to workers, and pushes
// results to out. Returns when iter is exhausted or ctx is canceled.
// The output channel is NOT closed by Run — caller (Scanner) closes
// it after all workers have returned.
//
// Run 消费 iter 的 item，分发给 worker，把 result 推给 out。iter 耗尽
// 或 ctx 取消时返回。输出 channel 不由 Run 关闭——调用方（Scanner）
// 在所有 worker 返回后关闭。
func (p *Pool) Run(ctx context.Context, iter Iterator, out chan<- Result) error {
	// Adaptive controller / 自适应控制器
	stopAdj := make(chan struct{})
	adjDone := make(chan struct{})
	go func() {
		defer close(adjDone)
		p.adaptiveLoop(ctx, stopAdj)
	}()

	// C4 audit fix: the previous implementation closed and recreated
	// the semaphore on every resize. Workers captured `sem` by closure
	// reference, so after `sem = newSem` they released to the NEW
	// semaphore while holding a slot on the OLD one. This caused:
	//   1. Deadlock when newSem was empty (worker release blocked forever)
	//   2. Concurrency exceeding MaxThreads (old in-flight + new acquires)
	// The fix uses a single max-capacity semaphore (MaxThreads) plus an
	// atomic counter `inflight` that workers check against `currentThreads`.
	// Resize only changes `currentThreads`; no semaphore recreation.
	//
	// C4 审计修法：旧实现在每次 resize 时 close 并重建信号量。worker 通过
	// 闭包引用 `sem`，`sem = newSem` 后它们向新信号量释放，却持有旧信号
	// 量的 slot。这导致：
	//   1. newSem 为空时死锁（worker 释放永久阻塞）
	//   2. 并发超过 MaxThreads（旧 in-flight + 新 acquire）
	// 修法：用单一最大容量信号量（MaxThreads）+ atomic 计数器 `inflight`，
	// worker 检查 `inflight` 与 `currentThreads` 的关系。resize 只改
	// `currentThreads`，不重建信号量。
	sem := make(chan struct{}, p.opts.MaxThreads)
	var wg sync.WaitGroup

	for {
		select {
		case <-ctx.Done():
			close(stopAdj)
			wg.Wait()
			<-adjDone
			return ctx.Err()
		default:
		}
		item, ok := iter.Next()
		if !ok {
			break
		}
		// Acquire a slot, but honor ctx cancellation. We also wait
		// until inflight < currentThreads so the adaptive controller's
		// shrinks actually take effect.
		// 取一个 slot，但尊重 ctx 取消。同时等待 inflight < currentThreads，
		// 让自适应控制器的缩容真正生效。
		//
		// P3-7 (audit): the previous backoff was a fixed 1ms
		// busy-wait. Under saturation (inflight >= currentThreads
		// for many consecutive iterations) the producer goroutine
		// pegs CPU at ~1 kHz of Timer resets. Replaced with capped
		// exponential backoff starting at 1ms and doubling up to
		// 50ms; CPU usage drops ~50× under saturation while still
		// waking promptly when a slot opens. / P3-7（审计）：旧版是
		// 固定 1ms busy-wait。饱和时（inflight >= currentThreads
		// 连续多次）生产者 goroutine 以 ~1 kHz Timer 重置把 CPU
		// 占满。改为 1ms 起步、倍增到 50ms 上限的指数退避；饱和
		// 时 CPU 占用降 ~50×，slot 释放后仍能及时醒来。
		const (
			backoffMin = 1 * time.Millisecond
			backoffMax = 50 * time.Millisecond
		)
		backoff := backoffMin
		for {
			if ctx.Err() != nil {
				close(stopAdj)
				wg.Wait()
				<-adjDone
				return ctx.Err()
			}
			cur := p.currentThreads.Load()
			if p.inflight.Load() < cur {
				// Try to acquire the semaphore without blocking forever;
				// fall back to ctx-aware select on failure.
				// 尝试非阻塞获取信号量；失败则走 ctx 感知 select。
				select {
				case sem <- struct{}{}:
					goto acquired
				default:
				}
			}
			// Phase 2.5 (audit roadmap): use a reused Timer instead of
			// time.After, which would allocate a new Timer per
			// busy-wait iteration. / Phase 2.5（审计路线图）：用复用
			// 的 Timer 替代 time.After，避免每次 busy-wait 分配新 Timer。
			resetTimer(p.busyTimer, backoff)
			select {
			case <-ctx.Done():
				p.busyTimer.Stop()
				close(stopAdj)
				wg.Wait()
				<-adjDone
				return ctx.Err()
			case <-p.busyTimer.C:
				// Capped exponential backoff. Reset to backoffMin
				// after a successful acquire (handled at `acquired`
				// below via the loop restart). / 上限封顶的指数退避。
				// 获取成功后在 `acquired` 处重启循环重置为 backoffMin。
				backoff *= 2
				if backoff > backoffMax {
					backoff = backoffMax
				}
			}
		}
	acquired:
		// Note: backoff is not reset here. The inner for-loop
		// re-initialises via `backoff *= 2; if backoff > backoffMax {
		// backoff = backoffMax }` from `backoffMin` on the next
		// iteration, and the value of `backoff` is not read after
		// this label. / 注：此处不重置 backoff。内层 for 循环在
		// 下一次迭代通过 `backoff *= 2; if backoff > backoffMax {
		// backoff = backoffMax }` 从 backoffMin 重新开始；本标签
		// 之后 `backoff` 不会被读取。
		p.inflight.Add(1)
		if p.opts.InflightSink != nil {
			p.opts.InflightSink.Add(1)
		}
		wg.Add(1)
		go func(item Item) {
			defer wg.Done()
			defer func() {
				p.inflight.Add(-1)
				if p.opts.InflightSink != nil {
					p.opts.InflightSink.Add(-1)
				}
				<-sem
			}()
			defer func() {
				// M7 audit fix: recover from panics in probes so a
				// single buggy protocol probe doesn't crash the whole
				// scan. / M7 审计修法：恢复 probe 中的 panic，避免单个
				// 有 bug 的协议 probe 拖垮整个扫描。
				if r := recover(); r != nil {
					if p.opts.OnProbeError != nil {
						p.opts.OnProbeError(item, fmt.Errorf("probe panic: %v", r))
					}
				}
			}()
			// Per-probe timeout: adaptive (mean+4σ of real-host RTT
			// samples) when wired, otherwise the fixed pool timeout.
			// / 每个 probe 的超时：接线了自适应（真实主机 RTT 样本的
			// mean+4σ）就用自适应，否则用固定池超时。
			timeout := p.opts.Timeout
			if p.opts.Adaptive != nil {
				timeout = p.opts.Adaptive.Timeout()
			}
			res, err := p.opts.Probe.Probe(ctx, item.Host, item.Port, timeout)
			if err != nil {
				// (P3 / F12 in the v0.2 audit) the previous code
				// discarded the probe error with `_`, which let
				// ctx-cancel / connection-reset pollute the
				// adaptive window and emit a zero-value Result
				// downstream. Treat a transport-layer error as
				// "neither open nor filtered" so the window
				// doesn't skew, and don't push a meaningless res
				// to the output channel.
				//
				// （v0.2 审计 P3 / F12）旧代码用 `_` 丢 probe 错误，
				// 让 ctx-cancel / 连接重置污染自适应窗口，并把零值
				// Result 推到下游。把传输层错误视作"非 open 也非
				// filtered"，避免窗口偏移，且不向输出 channel 推
				// 无意义 res。
				//
				// Resource-exhaustion errors double as the AIMD
				// congestion signal: RetryableProbe absorbs the
				// transient ones with backoff, so what surfaces here
				// is persistent FD/socket starvation — exactly when
				// the controller must shrink.
				// / 资源耗尽错误同时充当 AIMD 的拥塞信号：
				// RetryableProbe 已用退避吸收瞬时耗尽，能浮到这里
				// 的是持续性 FD/socket 饥饿——正是控制器必须缩容的
				// 时刻。
				if isResourceExhaustedError(err) {
					p.metrics.RecordExhausted()
				}
				if p.opts.OnProbeError != nil {
					p.opts.OnProbeError(item, err)
				}
				return
			}
			// Classify for the AIMD controller. Only open and closed
			// carry an RTT (ground truth); filtered/timeouts dilute
			// the exhaustion rate's denominator but never feed the
			// RTT EMAs (recordRTT skips RTT ≤ 0).
			// / 为 AIMD 控制器分类。只有 open 和 closed 携带 RTT
			// （网络真值）；filtered/timeout 只稀释耗尽率的分母，
			// 绝不喂 RTT EMA（recordRTT 跳过 RTT ≤ 0）。
			switch res.State {
			case StateOpen:
				p.metrics.RecordOpen(res.RTT)
			case StateClosed:
				p.metrics.RecordRefused(res.RTT)
			default:
				p.metrics.RecordTimeout()
			}
			// Feed the adaptive timeout only with samples that
			// actually reached a host: open (handshake / response)
			// and closed (refused RST) both prove a round trip.
			// Filtered results waited out the full deadline — their
			// "RTT" is the timeout itself and would poison the ring;
			// RTT ≤ 0 covers probes that report "no response"
			// (silent UDP) with a zero RTT.
			// / 只把真正到达主机的样本喂给自适应超时：open（握手/
			// 响应）与 closed（拒绝 RST）都证明了一个往返。filtered
			// 是等满超时——它的"RTT"就是超时本身，会毒化采样环；
			// RTT ≤ 0 覆盖以零 RTT 报告"无响应"的探测（静默 UDP）。
			if p.opts.Adaptive != nil && res.RTT > 0 &&
				(res.State == StateOpen || res.State == StateClosed) {
				p.opts.Adaptive.Record(res.RTT)
			}
			select {
			case out <- res:
			case <-ctx.Done():
			}
		}(item)
	}

	// Wait for in-flight + close adaptive loop. / 等 in-flight + 关自适应循环。
	close(stopAdj)
	wg.Wait()
	<-adjDone
	return nil
}

// adaptiveLoop periodically drives the controller at AdjustInterval.
// Exits when stop is closed.
//
// adaptiveLoop 按 AdjustInterval 周期性驱动控制器。stop 关闭时退出。
//
// P3-3 (audit): the previous implementation drove the loop off a
// single ticker at AdjustInterval (default 500ms). When Pool.Run
// closes stopAdj on the happy path, the goroutine sat blocked on
// `<-t.C` for up to 500ms before noticing the close. Now a much
// shorter wake-up ticker (50ms) drives the loop iteration; the
// actual adjust() call is still throttled to AdjustInterval via a
// timestamp guard, so the cost is unchanged but join latency drops
// to ~50ms. / P3-3（审计）：旧实现用单一 ticker（默认 500ms）驱动
// 循环。Pool.Run 在 happy path 关闭 stopAdj 时，goroutine 在
// `<-t.C` 上阻塞最多 500ms 才看到关闭。现在用更短的唤醒 ticker
// （50ms）驱动循环；adjust() 仍按 AdjustInterval 节流，成本不变
// 但 join 延迟降至 ~50ms。
func (p *Pool) adaptiveLoop(ctx context.Context, stop chan struct{}) {
	const wakeInterval = 50 * time.Millisecond
	wakeT := time.NewTicker(wakeInterval)
	defer wakeT.Stop()
	var lastAdjust time.Time
	for {
		select {
		case <-stop:
			return
		case <-ctx.Done():
			return
		case <-wakeT.C:
			if time.Since(lastAdjust) >= p.opts.AdjustInterval {
				p.adjust()
				lastAdjust = time.Now()
			}
		}
	}
}

// adjust runs one controller step: assess health from the metric
// deltas of the last interval, then apply the phase policy. It is
// only ever called from the adaptiveLoop goroutine.
//
// adjust 执行一步控制器：用上个周期的度量增量评估健康，再应用阶段
// 策略。只会被 adaptiveLoop goroutine 调用。
func (p *Pool) adjust() {
	health, _ := p.metrics.assessHealth(&p.prevSnap, p.opts.Env)
	if health == HealthUnknown {
		return
	}
	// Sustained RTT inflation lowers the AIMD target — the ceiling
	// for additive growth follows the network down instead of
	// fighting it. / RTT 持续膨胀时压低 AIMD target——加性增的天花
	// 板跟着网络下滑，而不是与网络对抗。
	p.maybeReduceTarget()

	cur := p.currentThreads.Load()
	var newSize int32
	if p.inSlowStart {
		newSize = p.adjustSlowStart(health, cur)
	} else {
		newSize = p.adjustAIMD(health, cur)
	}
	if floor := p.floorThreads(); newSize < floor {
		newSize = floor
	}
	if newSize > int32(p.opts.MaxThreads) {
		newSize = int32(p.opts.MaxThreads)
	}
	if newSize != cur {
		p.currentThreads.Store(newSize)
	}
}

// adjustSlowStart doubles per healthy interval until target; a
// stressed/congested interval exits the phase immediately with the
// congestion decrease. / adjustSlowStart 健康周期内逐周期翻倍至
// target；出现压力/拥塞立即退出该阶段并按拥塞因子缩减。
func (p *Pool) adjustSlowStart(health HealthSignal, cur int32) int32 {
	switch health {
	case HealthCongested, HealthStressed:
		p.inSlowStart = false
		return int32(float64(cur) * p.tuning.MDCongest)
	default:
		newSize := cur * 2
		if newSize >= p.target {
			newSize = p.target
			p.inSlowStart = false
		}
		return newSize
	}
}

// adjustAIMD is the steady-state policy: +5% of target while healthy,
// hold on OK, ×0.85 on stress, ×0.5 on congestion.
// / adjustAIMD 是稳态策略：健康时 +target 的 5%，OK 时维持，有压力
// ×0.85，拥塞 ×0.5。
func (p *Pool) adjustAIMD(health HealthSignal, cur int32) int32 {
	switch health {
	case HealthCongested:
		return int32(float64(cur) * p.tuning.MDCongest)
	case HealthStressed:
		return int32(float64(cur) * p.tuning.MDStress)
	case HealthGood:
		inc := p.target / int32(p.tuning.AIStepDiv)
		if inc < 1 {
			inc = 1
		}
		return cur + inc
	default: // HealthOK
		return cur
	}
}

// maybeReduceTarget lowers the AIMD target by 10% when the fast/slow
// RTT ratio sustains above Tuning.RatchetRatio — latency is
// escalating faster than any single halving would suggest. One-way
// ratchet (never re-raised within a scan) keeps the controller from
// oscillating on a flapping path. Floor: max(floorThreads,
// MaxThreads/5).
// / maybeReduceTarget 在 fast/slow RTT 比持续高于 Tuning.RatchetRatio
// 时把 AIMD target 压低 10%——延迟恶化速度比单次减半所暗示的更快。
// 单向棘轮（一次扫描内不再回升）避免控制器在抖动路径上震荡。下限：
// max(floorThreads, MaxThreads/5)。
func (p *Pool) maybeReduceTarget() {
	if p.metrics.RTTRatio() <= p.tuning.RatchetRatio {
		return
	}
	minTarget := int32(p.opts.MaxThreads / 5)
	if floor := p.floorThreads(); minTarget < floor {
		minTarget = floor
	}
	newTarget := int32(float64(p.target) * 0.9)
	if newTarget < minTarget {
		newTarget = minTarget
	}
	if newTarget < p.target {
		p.target = newTarget
	}
}
