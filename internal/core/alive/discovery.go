// Package alive: orchestrator.
// Package alive: 调度器。
//
// Discovery runs an ordered list of Probes against a target list
// and returns the first Hit (first-match strategy) for each host.
//
// Discovery 按顺序对目标列表跑 Probes 列表，返回每个主机的首个 Hit
// （first-match 策略）。
package alive

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

// Options configures the Discovery orchestrator.
// Options 配置 Discovery 调度器。
type Options struct {
	// Probes is the ordered list of probes to try. The first one that
	// returns a Hit is recorded. / Probes 是按顺序尝试的 probe 列表；
	// 第一个返回 Hit 的被记录。
	Probes []Probe

	// Timeout is the per-probe timeout (applied to each probe.Probe call).
	// Timeout 是每个 probe 的超时（作用于每次 probe.Probe 调用）。
	Timeout time.Duration

	// Threads is the maximum number of concurrent (host × probe) workers.
	// Threads 是并发 (host × probe) worker 的最大数。
	Threads int

	// FirstOnly: if true, stop probing a host as soon as one probe hits.
	// Always true in v0.1. / FirstOnly：若为 true，任一 probe 命中即停止
	// 探测该主机。v0.1 始终为 true。
	FirstOnly bool
}

// DefaultOptions returns sensible defaults: every probe registered
// via RegisterAlwaysOnProbe (ICMP + TCP-ping + system-ping from the
// in-tree probes_init.go), followed by every probe registered via
// RegisterLANProbe (typically ARP + NetBIOS, when the
// internal/discovery package is blank-imported). 3s timeout, 200
// threads, first-only. Probes that fail Available() are silently
// skipped at runtime.
//
// DefaultOptions 返回合理默认：所有通过 RegisterAlwaysOnProbe 注册
// 的 probe（in-tree probes_init.go 注册的 ICMP + TCP-ping +
// system-ping），再追加所有通过 RegisterLANProbe 注册的 probe（通常是
// ARP + NetBIOS，仅在 blank-import internal/discovery 时存在）。
// 3s 超时，200 线程，first-only。Available() 失败的 probe 在运行时
// 被静默跳过。
func DefaultOptions() Options {
	probes := append(RegisteredAlwaysOnProbes(), RegisteredLANProbes()...)
	return Options{
		Probes:    probes,
		Timeout:   3 * time.Second,
		Threads:   200,
		FirstOnly: true,
	}
}

// Discovery is the orchestrator. Construct with New() and call Run().
// Discovery 是调度器。用 New() 构造并调用 Run()。
type Discovery struct {
	opts Options
}

// New constructs a Discovery with the given options.
// New 用给定 options 构造一个 Discovery。
func New(opts Options) *Discovery {
	if opts.Timeout <= 0 {
		opts.Timeout = 3 * time.Second
	}
	if opts.Threads <= 0 {
		opts.Threads = 200
	}
	if !opts.FirstOnly {
		opts.FirstOnly = true
	}
	return &Discovery{opts: opts}
}

// AvailableProbes returns the subset of configured probes whose
// Available() returns nil, in the configured order.
//
// AvailableProbes 返回已配置且 Available() 返回 nil 的 probe 子集。
func (d *Discovery) AvailableProbes() []Probe {
	var out []Probe
	for _, p := range d.opts.Probes {
		if err := p.Available(); err == nil {
			out = append(out, p)
		}
	}
	return out
}

// RunResult summarizes the outcome of a Run() call. / RunResult 汇总 Run() 的结果。
type RunResult struct {
	// Hits maps host → first Hit. / Hits 映射 host → 首个 Hit。
	//
	// C3 audit fix: Hits is protected by hitsMu because multiple worker
	// goroutines write to it concurrently. The previous unsynchronized
	// map writes triggered `fatal error: concurrent map writes` under
	// the default 200-thread host discovery.
	//
	// C3 审计修法：Hits 由 hitsMu 保护，因为多个 worker goroutine 并发
	// 写入。原先未同步的 map 写入在默认 200 线程主机存活探测下会触发
	// `fatal error: concurrent map writes`。
	Hits   map[string]Hit
	hitsMu sync.Mutex
	// Tried counts how many probes were attempted (across all hosts).
	// Tried 计数所有尝试过的 probe 总数。
	Tried atomic.Int64
	// Unreachable lists hosts that no probe could confirm as alive.
	// Unreachable 列出所有 probe 都没能确认存活的主机。
	Unreachable []string
}

// SetHit records a hit for host under the mutex. / SetHit 在互斥锁保护下记录 host 的 hit。
func (r *RunResult) SetHit(host string, hit Hit) {
	r.hitsMu.Lock()
	r.Hits[host] = hit
	r.hitsMu.Unlock()
}

// Run probes all hosts concurrently and returns the RunResult.
// Honors ctx for cancellation.
//
// Run 并发探测所有主机并返回 RunResult。遵循 ctx 取消。
func (d *Discovery) Run(ctx context.Context, hosts []string) (*RunResult, error) {
	probes := d.AvailableProbes()
	if len(probes) == 0 {
		return nil, errors.New("alive: no probes available in this environment")
	}

	result := &RunResult{Hits: make(map[string]Hit, len(hosts))}

	// Per-host work item. / 单个主机的 work item。
	type work struct {
		host string
	}
	queue := make(chan work, len(hosts))
	for _, h := range hosts {
		queue <- work{host: h}
	}
	close(queue)

	threads := d.opts.Threads
	if threads > len(hosts) {
		threads = len(hosts)
	}
	if threads < 1 {
		threads = 1
	}
	sem := make(chan struct{}, threads)
	var wg sync.WaitGroup
	var unreachableMu sync.Mutex
	var unreachable []string

	for w := range queue {
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			return result, ctx.Err()
		}
		wg.Add(1)
		go func(host string) {
			defer wg.Done()
			defer func() { <-sem }()

			hit, err := d.probeOne(ctx, host, probes)
			result.Tried.Add(1)
			if err == nil {
				result.SetHit(host, hit) // C3: mutex-protected write / 互斥锁保护写入
				return
			}
			// ErrUnreachable is a clean miss; real errors are also treated
			// as unreachable for the host but logged via the wrapped err.
			// ErrUnreachable 是干净的 miss；其他错误同样视为不可达。
			unreachableMu.Lock()
			unreachable = append(unreachable, host)
			unreachableMu.Unlock()
		}(w.host)
	}
	wg.Wait()
	result.Unreachable = unreachable
	return result, nil
}

// probeOne runs the probe chain for a single host and returns the
// first Hit (or ErrUnreachable if all miss).
//
// probeOne 对单个主机跑 probe 链，返回首个 Hit（或 ErrUnreachable 如果
// 全部 miss）。
func (d *Discovery) probeOne(ctx context.Context, host string, probes []Probe) (Hit, error) {
	for _, p := range probes {
		if ctx.Err() != nil {
			return Hit{}, ctx.Err()
		}
		hit, err := p.Probe(ctx, host, d.opts.Timeout)
		if err == nil {
			return hit, nil
		}
		if errors.Is(err, ErrUnreachable) {
			continue // try the next probe
		}
		// A non-ErrUnreachable error is a real failure of this probe;
		// we continue to the next probe rather than aborting the host.
		// 非 ErrUnreachable 错误视为此 probe 真实失败；继续下一个 probe
		// 而不是放弃该主机。
		continue
	}
	return Hit{}, ErrUnreachable
}
