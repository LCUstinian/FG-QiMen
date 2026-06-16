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
	var lastResult Result
	var lastErr error
	backoff := r.config.InitialBackoff

	for attempt := 0; attempt <= r.config.MaxRetries; attempt++ {
		// First attempt or retry / 首次尝试或重试
		lastResult, lastErr = r.inner.Probe(ctx, host, port, timeout)

		// Success or non-retryable error / 成功或不可重试错误
		if lastErr == nil || !isResourceExhaustedError(lastErr) {
			return lastResult, lastErr
		}

		// Last attempt failed, return / 最后一次尝试失败，返回
		if attempt == r.config.MaxRetries {
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
		"too many open files",           // Linux EMFILE
		"cannot allocate memory",        // Linux ENOMEM
		"no buffer space available",     // BSD ENOBUFS
		"an operation on a socket could not be performed", // Windows socket exhaustion
		"wsaenobufs",                    // Windows no buffer space
		"out of socket descriptors",     // Generic
		"resource temporarily unavailable", // EAGAIN/EWOULDBLOCK
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
	TotalAttempts    int
	SuccessfulRetries int
	FailedRetries    int
	ResourceErrors   int
}

// ProbeWithRetryStats is like RetryableProbe but also tracks statistics.
// ProbeWithRetryStats 类似 RetryableProbe 但同时追踪统计信息。
type ProbeWithRetryStats struct {
	*RetryableProbe
	stats RetryStats
}

// NewProbeWithRetryStats creates a new ProbeWithRetryStats.
// NewProbeWithRetryStats 创建新的 ProbeWithRetryStats。
func NewProbeWithRetryStats(inner Probe, config RetryConfig) *ProbeWithRetryStats {
	return &ProbeWithRetryStats{
		RetryableProbe: NewRetryableProbe(inner, config),
	}
}

// Probe implements Probe with statistics tracking.
// Probe 实现带统计追踪的 Probe。
func (p *ProbeWithRetryStats) Probe(ctx context.Context, host string, port int, timeout time.Duration) (Result, error) {
	p.stats.TotalAttempts++

	var lastResult Result
	var lastErr error
	backoff := p.config.InitialBackoff

	for attempt := 0; attempt <= p.config.MaxRetries; attempt++ {
		lastResult, lastErr = p.inner.Probe(ctx, host, port, timeout)

		if lastErr == nil {
			if attempt > 0 {
				p.stats.SuccessfulRetries++
			}
			return lastResult, lastErr
		}

		if isResourceExhaustedError(lastErr) {
			p.stats.ResourceErrors++
		}

		if !isResourceExhaustedError(lastErr) {
			return lastResult, lastErr
		}

		if attempt == p.config.MaxRetries {
			p.stats.FailedRetries++
			return lastResult, lastErr
		}

		select {
		case <-ctx.Done():
			return lastResult, ctx.Err()
		case <-time.After(backoff):
		}

		backoff = time.Duration(float64(backoff) * p.config.BackoffMultiplier)
	}

	return lastResult, lastErr
}

// Stats returns the current retry statistics.
// Stats 返回当前重试统计信息。
func (p *ProbeWithRetryStats) Stats() RetryStats {
	return p.stats
}
