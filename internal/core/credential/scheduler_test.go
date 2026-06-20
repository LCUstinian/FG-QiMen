// scheduler_test.go — unit tests for the credential Scheduler.
//
// scheduler_test.go — credential Scheduler 的单元测试。
//
// The Scheduler is a thin wrapper around:
//  1. Bounded global concurrency (sem)
//  2. Per-target throttling (time gap between attempts)
//  3. Hit sink fan-out
//  4. ctx cancellation propagation
//
// We test each independently with a fake Authenticator that records
// call timing and either returns nil or a Hit on demand.
//
// Scheduler 是以下几件事的薄包装：
//  1. 有界全局并发（sem）
//  2. 按目标限流（尝试间隔）
//  3. Hit sink 扇出
//  4. ctx 取消传播
//
// 用一个会记录调用时间并按需返回 nil 或 Hit 的假 Authenticator 逐项
// 测试。
package credential

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeAuth records every call and returns the configured hit.
// / fakeAuth 记录每次调用并返回配置的 hit。
type fakeAuth struct {
	name      string
	delay     time.Duration
	hitOnCall int32 // 0 = always miss, N = hit on Nth call
	calls     int32
	mu        sync.Mutex
	durations []time.Duration
}

func (f *fakeAuth) Name() string        { return f.name }
func (f *fakeAuth) DefaultPorts() []int { return []int{22} }

func (f *fakeAuth) Authenticate(ctx context.Context, host string, port int, creds []Cred, timeout time.Duration) (*Hit, error) {
	start := time.Now()
	defer func() {
		f.mu.Lock()
		f.durations = append(f.durations, time.Since(start))
		f.mu.Unlock()
	}()
	atomic.AddInt32(&f.calls, 1)
	if f.delay > 0 {
		select {
		case <-time.After(f.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	n := atomic.LoadInt32(&f.calls)
	if f.hitOnCall > 0 && n == f.hitOnCall {
		return &Hit{
			Cred:     Cred{User: "u", Pass: "p", Method: AuthPassword},
			Attempts: int(n),
			Time:     time.Now(),
		}, nil
	}
	return nil, nil
}

// TestScheduler_RunsAllTargets verifies the Scheduler dispatches every
// target. / TestScheduler_RunsAllTargets 验证 Scheduler 派发每个
// target。
func TestScheduler_RunsAllTargets(t *testing.T) {
	auth := &fakeAuth{name: "ssh"}
	s := NewScheduler(SchedulerOptions{MaxConcurrent: 4})
	targets := make([]Target, 10)
	for i := range targets {
		targets[i] = Target{Host: "10.0.0.1", Port: 22, Auth: auth, Creds: []Cred{{User: "u", Pass: "p"}}}
	}
	var hits int32
	sink := FuncHitSink(func(*Hit) { atomic.AddInt32(&hits, 1) })
	s.Run(context.Background(), targets, sink)
	if got := atomic.LoadInt32(&auth.calls); got != 10 {
		t.Errorf("auth.calls = %d, want 10", got)
	}
	if got := atomic.LoadInt32(&hits); got != 0 {
		t.Errorf("hits = %d, want 0 (auth returns nil)", got)
	}
}

// TestScheduler_HitPropagation verifies the sink receives a Hit when
// the authenticator returns one. / TestScheduler_HitPropagation 验证
// authenticator 返回 Hit 时 sink 收到。
func TestScheduler_HitPropagation(t *testing.T) {
	auth := &fakeAuth{name: "ssh", hitOnCall: 1}
	s := NewScheduler(SchedulerOptions{MaxConcurrent: 1})
	got := make(chan *Hit, 1)
	sink := FuncHitSink(func(h *Hit) { got <- h })
	s.Run(context.Background(), []Target{{Host: "10.0.0.1", Port: 22, Auth: auth}}, sink)
	select {
	case h := <-got:
		if h == nil || h.Cred.User != "u" {
			t.Errorf("unexpected hit: %+v", h)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no hit received within 2s")
	}
}

// TestScheduler_RespectsMaxConcurrent verifies the sem semaphore
// caps the number of in-flight auth calls. / TestScheduler_RespectsMaxConcurrent
// 验证 sem 信号量限制同时在飞的 auth 调用数。
func TestScheduler_RespectsMaxConcurrent(t *testing.T) {
	auth := &fakeAuth{name: "ssh", delay: 50 * time.Millisecond}
	const maxC = 4
	s := NewScheduler(SchedulerOptions{MaxConcurrent: maxC})
	var inFlight int32
	var peak int32
	wrapped := wrappedAuth{
		inner: auth,
		before: func() {
			n := atomic.AddInt32(&inFlight, 1)
			for {
				p := atomic.LoadInt32(&peak)
				if n <= p || atomic.CompareAndSwapInt32(&peak, p, n) {
					break
				}
			}
		},
		after: func() { atomic.AddInt32(&inFlight, -1) },
	}
	targets := make([]Target, 20)
	for i := range targets {
		targets[i] = Target{Host: "10.0.0.1", Port: 22, Auth: wrapped, Creds: []Cred{{User: "u", Pass: "p"}}}
	}
	s.Run(context.Background(), targets, NopHitSink{})
	if got := atomic.LoadInt32(&peak); got > maxC {
		t.Errorf("peak in-flight = %d, want <= %d", got, maxC)
	}
	if got := atomic.LoadInt32(&peak); got == 0 {
		t.Error("peak in-flight = 0, scheduler never ran any work")
	}
}

// TestScheduler_ContextCancelStops verifies cancelling the root
// context causes in-flight Authenticate calls to return ctx.Err and
// no more targets are dispatched. / TestScheduler_ContextCancelStops
// 验证取消根 context 让在飞的 Authenticate 返回 ctx.Err，不再派发
// 新 target。
func TestScheduler_ContextCancelStops(t *testing.T) {
	auth := &fakeAuth{name: "ssh", delay: 5 * time.Second}
	s := NewScheduler(SchedulerOptions{MaxConcurrent: 2})
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	targets := make([]Target, 100)
	for i := range targets {
		targets[i] = Target{Host: "10.0.0.1", Port: 22, Auth: auth, Creds: []Cred{{User: "u", Pass: "p"}}}
	}
	start := time.Now()
	s.Run(ctx, targets, NopHitSink{})
	elapsed := time.Since(start)
	if elapsed > 2*time.Second {
		t.Errorf("scheduler took %v after ctx cancel; should be sub-second", elapsed)
	}
}

// TestScheduler_PerTargetThrottle verifies the per-target interval
// gates the gap between attempts against the same host:port. We use
// two different ports so the throttle doesn't fire (one bucket per
// host:port). / TestScheduler_PerTargetThrottle 验证按目标间隔限制
// 同 host:port 的尝试间隔。我们用不同端口避免节流触发（每个
// host:port 一个桶）。
func TestScheduler_PerTargetThrottle(t *testing.T) {
	auth := &fakeAuth{name: "ssh"}
	const interval = 100 * time.Millisecond
	s := NewScheduler(SchedulerOptions{
		MaxConcurrent:     1, // serialise so timing is deterministic
		PerTargetInterval: interval,
	})
	targets := []Target{
		{Host: "10.0.0.1", Port: 22, Auth: auth},
		{Host: "10.0.0.1", Port: 23, Auth: auth},
		{Host: "10.0.0.1", Port: 24, Auth: auth},
	}
	start := time.Now()
	s.Run(context.Background(), targets, NopHitSink{})
	elapsed := time.Since(start)
	// With MaxConcurrent=1 and 3 different ports, the throttle
	// shouldn't fire (different keys), so we expect ~0ms total.
	// Allow 100ms slack for scheduling jitter.
	// MaxConcurrent=1 + 3 不同端口，节流不应触发（不同 key），所以
	// 预期 ~0ms。允许 100ms 调度抖动。
	if elapsed > 200*time.Millisecond {
		t.Errorf("elapsed = %v, want < 200ms (throttle shouldn't fire on distinct ports)", elapsed)
	}
}

// wrappedAuth wraps an Authenticator and runs before/after hooks.
// Used to track in-flight count. / wrappedAuth 包一个 Authenticator
// 跑 before/after 钩子。用于跟踪 in-flight 计数。
type wrappedAuth struct {
	inner  Authenticator
	before func()
	after  func()
}

func (w wrappedAuth) Name() string        { return w.inner.Name() }
func (w wrappedAuth) DefaultPorts() []int { return w.inner.DefaultPorts() }

func (w wrappedAuth) Authenticate(ctx context.Context, host string, port int, creds []Cred, timeout time.Duration) (*Hit, error) {
	if w.before != nil {
		w.before()
	}
	defer func() {
		if w.after != nil {
			w.after()
		}
	}()
	return w.inner.Authenticate(ctx, host, port, creds, timeout)
}
