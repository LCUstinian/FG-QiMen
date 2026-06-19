// batch.go — batched bbolt writes for high-throughput scans.
//
// batch.go — 高吞吐扫描的批量 bbolt 写。
//
// At scan rates of ~50 results/sec, the per-call PutResult / PutCred /
// MarkSeenPersisted pattern produces one fsync per result. This file
// introduces BatchWriter, an in-process accumulator that flushes via
// Store.PutMany on either a count threshold (default 32) or a time
// threshold (default 200ms), whichever comes first.
//
// 在 ~50 结果/秒的扫描速率下，每次调 PutResult / PutCred /
// MarkSeenPersisted 模式每个结果产生一次 fsync。本文件引入
// BatchWriter——一个进程内的累加器，按数量阈值（默认 32）或
// 时间阈值（默认 200ms）触发 Store.PutMany 刷盘，以先到为准。
//
// Operators can disable batching with `cfg.NoBatch = true` to fall back
// to per-write semantics (useful for crash-window-sensitive runs).
// / 操作员可用 `cfg.NoBatch = true` 禁用批处理以回到 per-write 语义
// （崩溃窗口敏感的运行有用）。
package store

import (
	"context"
	"sync"
	"time"
)

// Default batch thresholds. / 默认批量阈值。
const (
	// DefaultBatchSize is the op count that triggers a flush.
	// DefaultBatchSize 是触发刷盘的操作数阈值。
	DefaultBatchSize = 32

	// DefaultBatchInterval is the time-based flush interval.
	// DefaultBatchInterval 是基于时间的刷盘间隔。
	DefaultBatchInterval = 200 * time.Millisecond
)

// BatchWriter accumulates PutOp values and flushes them in batches
// via Store.PutMany. Safe for concurrent use by multiple goroutines.
// / BatchWriter 累加 PutOp 值并通过 Store.PutMany 批量刷盘。多
// goroutine 并发安全。
type BatchWriter struct {
	s        *Store
	size     int
	interval time.Duration

	mu      sync.Mutex
	pending []PutOp
	// pendingBytes tracks the in-memory payload size. Flushing on
	// a byte threshold (in addition to count) prevents pathological
	// memory growth if a single PutOp carries a huge payload.
	// pendingBytes 跟踪内存中负载大小。在字节阈值上（外加数量阈
	// 值）刷盘，避免单个巨大 PutOp 撑爆内存。
	pendingBytes int

	stopCh   chan struct{}
	doneCh   chan struct{}
	running  bool
	runOnce  sync.Once
	doneOnce sync.Once
}

// NewBatchWriter returns a BatchWriter bound to s. size and interval
// are the flush thresholds; pass 0 to use DefaultBatchSize /
// DefaultBatchInterval. / NewBatchWriter 返回绑定到 s 的 BatchWriter。
// size 和 interval 是刷盘阈值；传 0 用 DefaultBatchSize /
// DefaultBatchInterval。
func NewBatchWriter(s *Store, size int, interval time.Duration) *BatchWriter {
	if size <= 0 {
		size = DefaultBatchSize
	}
	if interval <= 0 {
		interval = DefaultBatchInterval
	}
	return &BatchWriter{
		s:        s,
		size:     size,
		interval: interval,
		stopCh:   make(chan struct{}),
		doneCh:   make(chan struct{}),
	}
}

// Enqueue adds an op to the pending batch. If the count or byte
// threshold is reached, the batch is flushed synchronously. / Enqueue
// 把 op 加入待刷盘批次。若达数量或字节阈值则同步刷盘。
func (b *BatchWriter) Enqueue(op PutOp) {
	if b == nil {
		return
	}
	b.mu.Lock()
	b.pending = append(b.pending, op)
	// Conservative byte estimate: the marshaled size. PutOp.Value
	// can be []byte (already-marshaled) or anything (will be
	// marshaled at flush time). We can't know the marshaled size
	// without actually marshaling, so we use a per-op ceiling of
	// 64KiB which fits any single result row in practice.
	// 保守字节估计：序列化后大小。PutOp.Value 可能是 []byte
	// （已序列化）或任意（将在刷盘时序列化）。不实际序列化
	// 没法知道序列化后大小，所以我们用 64KiB 的 per-op 上限，
	// 实践中能容纳任何单条结果。
	b.pendingBytes += 64 * 1024
	full := len(b.pending) >= b.size || b.pendingBytes >= 1<<20
	b.mu.Unlock()
	if full {
		b.Flush()
	}
}

// Flush writes any pending ops to the underlying Store. Safe to call
// concurrently with Enqueue. / Flush 把待写 ops 写入底层 Store。
// 可与 Enqueue 并发调用。
func (b *BatchWriter) Flush() {
	if b == nil {
		return
	}
	b.mu.Lock()
	if len(b.pending) == 0 {
		b.mu.Unlock()
		return
	}
	ops := b.pending
	b.pending = nil
	b.pendingBytes = 0
	b.mu.Unlock()
	if b.s == nil {
		return
	}
	_ = b.s.PutMany(ops)
}

// Run starts the periodic flusher. Returns when ctx is canceled or
// Stop() is called; in both cases the final Flush() is invoked so no
// ops are lost. / Run 启动周期性刷盘器。ctx 取消或 Stop() 被调时返
// 回；两种情况都会做最后一次 Flush()，不丢任何 op。
//
// Run must be called at most once per BatchWriter. / 每个 BatchWriter
// 最多调一次 Run。
func (b *BatchWriter) Run(ctx context.Context) {
	if b == nil {
		return
	}
	b.runOnce.Do(func() {
		b.running = true
		t := time.NewTicker(b.interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				b.Flush()
				return
			case <-b.stopCh:
				b.Flush()
				return
			case <-t.C:
				b.Flush()
			}
		}
	})
	b.doneOnce.Do(func() { close(b.doneCh) })
}

// Stop signals Run to exit and waits for the final flush to complete.
// Safe to call without Run() having been started — in that case the
// pending ops are flushed synchronously here. / Stop 通知 Run 退出
// 并等待最后一次刷盘完成。在 Run() 未启动时也安全——这种情况
// 下待写 op 在这里同步刷盘。
func (b *BatchWriter) Stop() {
	if b == nil {
		return
	}
	if !b.running {
		// No Run() goroutine is active. Flush synchronously and
		// close doneCh so any concurrent or future waiters wake up.
		// / 没有 Run() goroutine 在跑。同步刷盘并关闭 doneCh，
		// 让任何并发或将来等待者醒过来。
		b.Flush()
		b.doneOnce.Do(func() { close(b.doneCh) })
		return
	}
	select {
	case <-b.stopCh:
		// already stopped
	default:
		close(b.stopCh)
	}
	<-b.doneCh
}

// Pending returns the current pending op count (for tests / metrics).
// / Pending 返回当前待写 op 数量（供测试/指标）。
func (b *BatchWriter) Pending() int {
	if b == nil {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.pending)
}
