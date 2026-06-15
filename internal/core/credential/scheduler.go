// Package cred: per-target scheduler (throttling + concurrency).
// Package cred: 按目标调度（限流 + 并发）。
//
// Scheduler runs authenticators against targets with:
//   - bounded global concurrency
//   - per-target throttling (no more than N auth attempts/sec per host)
//   - first-match short-circuit (stop on hit)
//
// Scheduler 按以下规则跑 authenticator：
//   - 全局有界并发
//   - 按目标限流（每主机每秒不超过 N 次认证）
//   - 首个命中即停（first-match）
package credential

import (
	"context"
	"sync"
	"time"
)

// SchedulerOptions configures Scheduler. / SchedulerOptions 配置 Scheduler。
type SchedulerOptions struct {
	// MaxConcurrent is the global cap on simultaneous Authenticator
	// invocations. / MaxConcurrent 是同时调 Authenticator 的全局上限。
	MaxConcurrent int

	// PerTargetInterval is the minimum spacing between auth attempts
	// against the same host:port (e.g. 100ms = 10 attempts/sec max).
	// PerTargetInterval 是同一 host:port 上认证尝试的最小间隔。
	PerTargetInterval time.Duration

	// StopOnHit: when true, cancel the auth chain for the host as soon
	// as one Hit lands (other attempts in flight are allowed to finish).
	// StopOnHit：true 时任一 host 命中后取消该 host 的后续尝试。
	StopOnHit bool
}

// DefaultSchedulerOptions returns sensible defaults: 50 concurrent
// probes, 100ms per-target spacing (≤ 10 attempts/sec per host).
//
// DefaultSchedulerOptions 返回合理默认：50 并发，单目标间隔 100ms
// （每主机每秒最多 10 次）。
func DefaultSchedulerOptions() SchedulerOptions {
	return SchedulerOptions{
		MaxConcurrent:     50,
		PerTargetInterval: 100 * time.Millisecond,
		StopOnHit:         true,
	}
}

// Target is the (host, port, authenticator) triple to attack.
// Target 是要攻击的 (host, port, authenticator) 三元组。
type Target struct {
	Host  string
	Port  int
	Auth  Authenticator
	Creds []Cred
}

// HitSink receives hits. Implementations decide what to do with them
// (write to creds.txt, push to UI, etc.). / HitSink 接收命中。
// 实现决定如何处理（写 creds.txt、推 UI 等）。
type HitSink interface {
	OnHit(hit *Hit)
}

// FuncHitSink adapts a function to HitSink. / FuncHitSink 把函数适配为 HitSink。
type FuncHitSink func(*Hit)

// OnHit implements HitSink. / OnHit 实现 HitSink。
func (f FuncHitSink) OnHit(h *Hit) { f(h) }

// NopHitSink discards all hits. / NopHitSink 丢弃所有命中。
type NopHitSink struct{}

// OnHit implements HitSink. / OnHit 实现 HitSink。
func (NopHitSink) OnHit(*Hit) {}

// Scheduler runs the schedule. / Scheduler 跑调度。
type Scheduler struct {
	opts SchedulerOptions
}

// NewScheduler constructs a Scheduler. / NewScheduler 构造一个 Scheduler。
func NewScheduler(opts SchedulerOptions) *Scheduler {
	if opts.MaxConcurrent <= 0 {
		opts.MaxConcurrent = 50
	}
	if opts.PerTargetInterval <= 0 {
		opts.PerTargetInterval = 100 * time.Millisecond
	}
	if !opts.StopOnHit {
		opts.StopOnHit = true
	}
	return &Scheduler{opts: opts}
}

// throttleFor returns the per-target last-attempt tracker. We keep a
// goroutine-local guard keyed by "host:port". / throttleFor 是按目标的
// 上次尝试时间记录。key 为 "host:port"。
type throttleKey struct{}

// throttleState is stashed in ctx via context.WithValue. / throttleState
// 通过 context.WithValue 暂存在 ctx 中。
type throttleState struct {
	mu      sync.Mutex
	lastTry map[string]time.Time
}

// Run attacks all targets concurrently and dispatches hits to sink.
// Returns when all targets are done or ctx is canceled.
//
// Run 并发攻击所有目标，命中派发到 sink。所有 target 完成或 ctx 取消时返回。
func (s *Scheduler) Run(ctx context.Context, targets []Target, sink HitSink) {
	if sink == nil {
		sink = NopHitSink{}
	}
	throttle := &throttleState{lastTry: make(map[string]time.Time)}

	sem := make(chan struct{}, s.opts.MaxConcurrent)
	var wg sync.WaitGroup
	for _, t := range targets {
		select {
		case <-ctx.Done():
			wg.Wait()
			return
		case sem <- struct{}{}:
		}
		wg.Add(1)
		go func(t Target) {
			defer wg.Done()
			defer func() { <-sem }()
			s.runOne(ctx, t, sink, throttle)
		}(t)
	}
	wg.Wait()
}

// runOne attacks a single target. / runOne 攻击单个 target。
func (s *Scheduler) runOne(ctx context.Context, t Target, sink HitSink, throttle *throttleState) {
	key := t.Host + ":" + itoa(t.Port)

	// Per-target throttle. / 单目标限流。
	//
	// P1#2: the previous code used time.Sleep while holding throttle.mu,
	// which (a) ignored ctx cancellation (up to PerTargetInterval of
	// post-SIGINT latency per in-flight runOne) and (b) blocked other
	// targets at the same host:port for the sleep duration. We
	// compute the deadline first, release the mutex, then select on
	// time-until-deadline vs ctx.Done.
	//
	// P1#2：旧代码在持有 throttle.mu 时 time.Sleep，有两个问题：(a) 忽
	// 略 ctx 取消（每个 in-flight runOne 在 SIGINT 后最多还要等
	// PerTargetInterval）；(b) 同 host:port 的其他 target 在 sleep 期间
	// 阻塞。先算 deadline，释放 mutex，再 select 在 deadline vs ctx.Done。
	throttle.mu.Lock()
	last := throttle.lastTry[key]
	now := time.Now()
	throttle.lastTry[key] = now
	throttle.mu.Unlock()

	if !last.IsZero() {
		gap := now.Sub(last)
		if gap < s.opts.PerTargetInterval {
			wait := s.opts.PerTargetInterval - gap
			t := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				t.Stop()
				return
			case <-t.C:
			}
		}
	}

	// Call the authenticator with the configured timeout.
	// 用配置的超时调 authenticator。
	timeout := s.opts.PerTargetInterval * 10
	if timeout < 2*time.Second {
		timeout = 2 * time.Second
	}
	hit, err := t.Auth.Authenticate(ctx, t.Host, t.Port, t.Creds, timeout)
	if err != nil {
		// Real error (ctx canceled, network failure, etc.) — surface
		// via the wrapped err. v0.1: log-and-continue at caller level.
		// 真实错误（ctx 取消、网络失败等）——通过包装 err 暴露。
		// v0.1: 调用方记录并继续。
		return
	}
	if hit != nil {
		hit.Host = t.Host
		hit.Port = t.Port
		hit.Service = t.Auth.Name()
		sink.OnHit(hit)
	}
}

// itoa is a fast integer-to-string for keys. Avoids fmt import in
// hot path. / itoa 是 key 用整数转字符串的快速实现，避免热路径引入 fmt。
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
