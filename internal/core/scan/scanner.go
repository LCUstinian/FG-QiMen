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
	Threads    int // AIMD target concurrency; the pool slow-starts below it and AIMD-adjusts around it / AIMD 目标并发；池在其下方慢启动，并在其附近 AIMD 调整
	MinThreads int
	MaxThreads int
	// Adaptive, when non-nil, enables the RTT-sampled per-probe
	// timeout (see AdaptiveTimeout). The wiring site (core/scanner.go)
	// disables it when the operator set --timeout explicitly.
	// / Adaptive 非 nil 时启用 RTT 采样的逐 probe 超时（见
	// AdaptiveTimeout）。操作员显式设置 --timeout 时由接线点
	// （core/scanner.go）禁用。
	Adaptive *AdaptiveTimeout
	// Env classifies the target network for the pool's AIMD health
	// thresholds (see metrics.go). Zero value maps to the WAN set.
	// / Env 为池的 AIMD 健康阈值刻画目标网络（见 metrics.go）。零值
	// 映射到 WAN 档。
	Env Env
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
	if opts.Adaptive != nil {
		pOpts.Adaptive = opts.Adaptive
	}
	if opts.Env != "" {
		pOpts.Env = opts.Env
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
