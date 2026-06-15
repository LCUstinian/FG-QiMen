// Package scan: bounded-concurrency worker pool with adaptive sizing.
// Package scan: 有界并发 worker 池，支持自适应调并发。
//
// Pool pulls Items from an Iterator, dispatches them to N workers
// (each calling the Probe), and sends Results to an out channel.
//
// Pool 从 Iterator 拉 Item，分发给 N 个 worker（每个调 Probe），
// 把 Result 发到 out channel。
//
// Adaptive sizing: when the recent error rate is high (e.g. many
// filtered results due to timeouts), the pool shrinks concurrency to
// reduce pressure; when error rate is low, it grows back. This is a
// simple sliding-window heuristic, not full AIMD.
//
// 自适应调并发：当近期错误率高时（很多 filtered 结果，多为超时），
// 降低并发以减压；错误率低时再加回来。这是简单的滑动窗口启发式，
// 不是完整 AIMD。
package scan

import (
	"context"
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

	// FilteredShrinkRatio: if more than this fraction of recent
	// probes return filtered, shrink concurrency by 25%.
	// FilteredShrinkRatio：若超过这个比例的近期 probe 返回 filtered，
	// 并发降 25%。
	FilteredShrinkRatio float64

	// OpenGrowRatio: if more than this fraction of recent probes
	// are open, grow concurrency by 25% (until MaxThreads).
	// OpenGrowRatio：若超过这个比例的近期 probe 是 open，
	// 并发升 25%（到 MaxThreads 为止）。
	OpenGrowRatio float64
}

// DefaultPoolOptions returns a PoolOptions with sensible defaults.
// DefaultPoolOptions 返回带合理默认的 PoolOptions。
func DefaultPoolOptions(probe Probe) PoolOptions {
	return PoolOptions{
		Probe:               probe,
		Timeout:             3 * time.Second,
		MinThreads:          10,
		MaxThreads:          500,
		InitialThreads:      200,
		AdjustInterval:      500 * time.Millisecond,
		FilteredShrinkRatio: 0.5,
		OpenGrowRatio:       0.1,
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

	// sliding-window counters
	windowMu  sync.Mutex
	window    []windowSample
	windowMax int
}

type windowSample struct {
	open     bool
	filtered bool
}

// NewPool constructs a Pool. / NewPool 构造一个 Pool。
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
	if opts.FilteredShrinkRatio <= 0 {
		opts.FilteredShrinkRatio = 0.5
	}
	if opts.OpenGrowRatio <= 0 {
		opts.OpenGrowRatio = 0.1
	}
	p := &Pool{
		opts:      opts,
		windowMax: 256,
	}
	p.currentThreads.Store(int32(opts.InitialThreads))
	return p
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

	// Semaphore-based worker pool. / 基于信号量的 worker 池。
	threads := int(p.currentThreads.Load())
	sem := make(chan struct{}, threads)
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
		// Acquire a slot, but honor ctx cancellation.
		// 取一个 slot，但尊重 ctx 取消。
		select {
		case <-ctx.Done():
			close(stopAdj)
			wg.Wait()
			<-adjDone
			return ctx.Err()
		case sem <- struct{}{}:
		}
		wg.Add(1)
		// Read current concurrency each time so the adaptive loop
		// can grow the pool. / 每次读当前并发数，让自适应循环能扩池。
		current := int(p.currentThreads.Load())
		if cap(sem) != current {
			// Resize semaphore if needed. / 按需调信号量容量。
			newSem := make(chan struct{}, current)
			close(sem)
			sem = newSem
		}
		go func(item Item) {
			defer wg.Done()
			defer func() { <-sem }()
			res, err := p.opts.Probe.Probe(ctx, item.Host, item.Port, p.opts.Timeout)
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
				if p.opts.OnProbeError != nil {
					p.opts.OnProbeError(item, err)
				}
				return
			}
			p.record(res)
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

// record adds a result to the sliding window.
// record 把一个结果加入滑动窗口。
func (p *Pool) record(r Result) {
	p.windowMu.Lock()
	defer p.windowMu.Unlock()
	if len(p.window) >= p.windowMax {
		// Drop the oldest sample. / 丢弃最早的样本。
		p.window = p.window[1:]
	}
	p.window = append(p.window, windowSample{
		open:     r.State == StateOpen,
		filtered: r.State == StateFiltered,
	})
}

// adaptiveLoop periodically inspects the sliding window and adjusts
// concurrency. Exits when stop is closed.
//
// adaptiveLoop 周期性检查滑动窗口并调并发。stop 关闭时退出。
func (p *Pool) adaptiveLoop(ctx context.Context, stop chan struct{}) {
	t := time.NewTicker(p.opts.AdjustInterval)
	defer t.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ctx.Done():
			return
		case <-t.C:
			p.adjust()
		}
	}
}

// adjust inspects the current window and grows/shrinks the pool
// according to the ratios in opts. / adjust 检查当前窗口，按 opts 中的
// 比例扩缩并发。
func (p *Pool) adjust() {
	p.windowMu.Lock()
	w := p.window
	p.windowMu.Unlock()
	if len(w) < 16 {
		return // not enough samples yet
	}
	var openN, filtN int
	for _, s := range w {
		if s.open {
			openN++
		} else if s.filtered {
			filtN++
		}
	}
	total := len(w)
	filtRatio := float64(filtN) / float64(total)
	openRatio := float64(openN) / float64(total)

	cur := p.currentThreads.Load()
	if filtRatio > p.opts.FilteredShrinkRatio {
		// Shrink by 25%. / 降 25%。
		newThreads := int32(float64(cur) * 0.75)
		if newThreads < int32(p.opts.MinThreads) {
			newThreads = int32(p.opts.MinThreads)
		}
		if newThreads != cur {
			p.currentThreads.Store(newThreads)
		}
	} else if openRatio > p.opts.OpenGrowRatio {
		// Grow by 25%. / 升 25%。
		newThreads := int32(float64(cur) * 1.25)
		if newThreads > int32(p.opts.MaxThreads) {
			newThreads = int32(p.opts.MaxThreads)
		}
		if newThreads != cur {
			p.currentThreads.Store(newThreads)
		}
	}
}
