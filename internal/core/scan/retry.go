// Package scan: retry logic for port scanning.
// Package scan: 端口扫描重试逻辑。
//
// Inspired by fscan's retry mechanism (port_scan.go L340-368), this module
// retries TCP connect attempts only for resource-exhausted errors (too many
// open files, out of sockets), with exponential backoff.
//
// 借鉴 fscan 的重试机制（port_scan.go L340-368），本模块仅对资源耗尽
// 错误（文件描述符不足、socket 耗尽）重试 TCP 连接，采用指数退避。
package scan

import (
	"context"
	"strings"
	"time"
)

// RetryConfig configures retry behavior.
// RetryConfig 配置重试行为。
type RetryConfig struct {
	// MaxRetries is the maximum number of retry attempts.
	// MaxRetries 是最大重试次数。
	MaxRetries int

	// TrackStats enables per-probe retry statistics (TotalAttempts /
	// SuccessfulRetries / ResourceErrors / FailedRetries). Off by
	// default; the counters cost ~1 atomic add per call when on.
	// / TrackStats 启用 per-probe 重试统计（TotalAttempts /
	// SuccessfulRetries / ResourceErrors / FailedRetries）。默认关
	// 闭；开启时每条调用 ~1 次 atomic add 开销。
	TrackStats bool

	// InitialBackoff is the initial backoff duration.
	// InitialBackoff 是初始退避时长。
	InitialBackoff time.Duration

	// BackoffMultiplier is the backoff multiplier for each retry.
	// BackoffMultiplier 是每次重试的退避倍数。
	BackoffMultiplier float64
}

// DefaultRetryConfig returns sensible defaults.
// DefaultRetryConfig 返回合理默认值。
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxRetries:        3,
		InitialBackoff:    200 * time.Millisecond,
		BackoffMultiplier: 2.0, // 200ms → 400ms → 800ms
	}
}

// RetryableProbe wraps a Probe with retry logic.
// RetryableProbe 用重试逻辑包装 Probe。
type RetryableProbe struct {
	inner  Probe
	config RetryConfig
	// stats is updated by every Probe call. Pass config.TrackStats
	// to enable; otherwise the counters stay at zero. The
	// ProbeWithRetryStats wrapper from v0.2.x is gone — the same
	// struct now does both jobs. P4.1 (audit roadmap).
	// / stats 每次 Probe 调用更新。传 config.TrackStats 启用；否则计
	// 数器保持零。v0.2.x 的 ProbeWithRetryStats 包装已移除——同
	// 一结构现在做两件事。P4.1（审计路线图）。
	stats RetryStats
}

// NewRetryableProbe creates a new RetryableProbe.
// NewRetryableProbe 创建新的 RetryableProbe。
func NewRetryableProbe(inner Probe, config RetryConfig) *RetryableProbe {
	return &RetryableProbe{
		inner:  inner,
		config: config,
	}
}

// Name implements Probe. / Name 实现 Probe。
func (r *RetryableProbe) Name() string {
	return r.inner.Name() + "-retry"
}

// Method implements Probe. / Method 实现 Probe。
func (r *RetryableProbe) Method() Method {
	return r.inner.Method()
}

// Available implements Probe. / Available 实现 Probe。
func (r *RetryableProbe) Available() error {
	return r.inner.Available()
}

// Probe implements Probe with retry logic.
// Probe 实现带重试逻辑的 Probe。
func (r *RetryableProbe) Probe(ctx context.Context, host string, port int, timeout time.Duration) (Result, error) {
	if r.config.TrackStats {
		r.stats.TotalAttempts++
	}
	var lastResult Result
	var lastErr error
	backoff := r.config.InitialBackoff

	for attempt := 0; attempt <= r.config.MaxRetries; attempt++ {
		// First attempt or retry / 首次尝试或重试
		lastResult, lastErr = r.inner.Probe(ctx, host, port, timeout)

		// Success or non-retryable error / 成功或不可重试错误
		if lastErr == nil || !isResourceExhaustedError(lastErr) {
			if r.config.TrackStats && lastErr == nil && attempt > 0 {
				r.stats.SuccessfulRetries++
			}
			return lastResult, lastErr
		}

		if r.config.TrackStats && isResourceExhaustedError(lastErr) {
			r.stats.ResourceErrors++
		}

		// Last attempt failed, return / 最后一次尝试失败，返回
		if attempt == r.config.MaxRetries {
			if r.config.TrackStats {
				r.stats.FailedRetries++
			}
			return lastResult, lastErr
		}

		// Wait before retry (with context cancellation check) / 重试前等待（检查 context 取消）
		select {
		case <-ctx.Done():
			return lastResult, ctx.Err()
		case <-time.After(backoff):
			// Continue to next attempt / 继续下次尝试
		}

		// Exponential backoff / 指数退避
		backoff = time.Duration(float64(backoff) * r.config.BackoffMultiplier)
	}

	return lastResult, lastErr
}

// Stats returns the current retry statistics. Only meaningful when
// config.TrackStats is true; otherwise counters stay at zero.
// Stats 返回当前重试统计信息。仅在 config.TrackStats 为 true 时有
// 意义；否则计数器保持零。
func (r *RetryableProbe) Stats() RetryStats {
	return r.stats
}

// isResourceExhaustedError checks if the error is a resource exhaustion error.
// isResourceExhaustedError 检查错误是否为资源耗尽错误。
//
// Inspired by fscan's isResourceExhaustedError (port_scan.go L370-384), this
// function identifies errors that indicate the system is out of resources
// (file descriptors, sockets) and should be retried.
//
// 借鉴 fscan 的 isResourceExhaustedError（port_scan.go L370-384），本函数
// 识别表明系统资源不足（文件描述符、socket）且应重试的错误。
func isResourceExhaustedError(err error) bool {
	if err == nil {
		return false
	}

	errStr := strings.ToLower(err.Error())

	// Resource exhaustion patterns from fscan / fscan 的资源耗尽模式
	patterns := []string{
		"too many open files",                             // Linux EMFILE
		"cannot allocate memory",                          // Linux ENOMEM
		"no buffer space available",                       // BSD ENOBUFS
		"an operation on a socket could not be performed", // Windows socket exhaustion
		"wsaenobufs",                                      // Windows no buffer space
		"out of socket descriptors",                       // Generic
		"resource temporarily unavailable",                // EAGAIN/EWOULDBLOCK
	}

	for _, pattern := range patterns {
		if strings.Contains(errStr, pattern) {
			return true
		}
	}

	return false
}

// RetryStats tracks retry statistics.
// RetryStats 追踪重试统计信息。
type RetryStats struct {
	TotalAttempts     int
	SuccessfulRetries int
	FailedRetries     int
	ResourceErrors    int
}

// ProbeWithRetryStats removed in v0.3.1 (P4.1 of the audit roadmap):
// the 90% duplicate of RetryableProbe is now expressed as
// RetryableProbe + RetryConfig{TrackStats: true} + RetryableProbe.Stats().
// / ProbeWithRetryStats 在 v0.3.1 移除（P4.1 审计路线图）：与
// RetryableProbe 重复 90% 的代码现表示为 RetryableProbe +
// RetryConfig{TrackStats: true} + RetryableProbe.Stats()。
