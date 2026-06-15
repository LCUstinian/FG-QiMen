// Package scan: top-level Scanner orchestrator.
// Package scan: 顶层 Scanner 调度器。
//
// Scanner wires an Iterator → Probe → Pool → out channel and returns
// when the iterator is exhausted (or ctx is canceled). The output
// channel is closed when Scanner returns.
//
// Scanner 装配 Iterator → Probe → Pool → out channel，迭代器耗尽（或
// ctx 取消）时返回。Scanner 返回时关闭输出 channel。
package scan

import (
	"context"
	"time"
)

// ScanOptions configures Scanner. / ScanOptions 配置 Scanner。
type ScanOptions struct {
	Probe      Probe
	Timeout    time.Duration
	Threads    int // initial threads; pool adapts up/down
	MinThreads int
	MaxThreads int
	// OnProbeError forwards the pool's per-probe error signal to
	// the caller. Same contract as PoolOptions.OnProbeError.
	//
	// OnProbeError 把 pool 的每个 probe 错误信号转发给调用方。契约
	// 同 PoolOptions.OnProbeError。
	OnProbeError func(item Item, err error)
}

// Scanner is the orchestrator. / Scanner 是调度器。
type Scanner struct {
	pool *Pool
}

// NewScanner constructs a Scanner. / NewScanner 构造一个 Scanner。
func NewScanner(opts ScanOptions) *Scanner {
	pOpts := DefaultPoolOptions(opts.Probe)
	if opts.Timeout > 0 {
		pOpts.Timeout = opts.Timeout
	}
	if opts.Threads > 0 {
		pOpts.InitialThreads = opts.Threads
	}
	if opts.MinThreads > 0 {
		pOpts.MinThreads = opts.MinThreads
	}
	if opts.MaxThreads > 0 {
		pOpts.MaxThreads = opts.MaxThreads
	}
	if opts.OnProbeError != nil {
		pOpts.OnProbeError = opts.OnProbeError
	}
	return &Scanner{pool: NewPool(pOpts)}
}

// Run scans the items produced by iter, sending each Result to out.
// The out channel is closed when Run returns. Honors ctx.
//
// Run 扫描 iter 产出的 item，每个 Result 发送到 out。Run 返回时关闭
// out channel。遵循 ctx。
func (s *Scanner) Run(ctx context.Context, iter Iterator, out chan<- Result) error {
	err := s.pool.Run(ctx, iter, out)
	close(out)
	return err
}
