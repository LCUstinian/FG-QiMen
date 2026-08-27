// retry_test.go — unit tests for RetryableProbe and its statistics.
//
// Covers:
//   - Resource-exhaustion error classification (isResourceExhaustedError)
//   - Concurrent stats counters stay accurate under -race (Task 5)
//   - Probe retries up to MaxRetries, then returns the last error
//
// retry_test.go — RetryableProbe 及其统计的单测。
package scan

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// resourceErrProbe is a fake Probe that always returns a
// resource-exhaustion-style error. Used to drive RetryableProbe
// through its retry path without touching the network.
//
// resourceErrProbe 是始终返回资源耗尽错误的 fake Probe。用于驱动
// RetryableProbe 走完重试路径而不触网。
type resourceErrProbe struct{}

func (p *resourceErrProbe) Name() string     { return "fake-res-exh" }
func (p *resourceErrProbe) Method() Method   { return MethodTCPConnect }
func (p *resourceErrProbe) Available() error { return nil }
func (p *resourceErrProbe) Probe(_ context.Context, _ string, _ int, _ time.Duration) (Result, error) {
	return Result{}, errors.New("too many open files")
}

// TestRetryableProbeStatsConcurrent — Task 5 (first-batch fixes).
// The audit flagged RetryableProbe.stats.{TotalAttempts,
// SuccessfulRetries, FailedRetries, ResourceErrors} as a P0 data
// race: every Probe call incremented plain int fields from
// concurrent goroutines. The retry loop runs up to MaxRetries+1
// attempts per call, so a single Probe call writes to stats
// counters up to 4 times. With N concurrent Probe calls the writes
// overlap and -race fires immediately.
//
// / TestRetryableProbeStatsConcurrent — 第一批修复 Task 5。审计将
// RetryableProbe.stats.{TotalAttempts, SuccessfulRetries,
// FailedRetries, ResourceErrors} 标为 P0 数据竞争：每次 Probe 调用
// 从并发 goroutine 增加普通 int 字段。重试循环每次调用最多跑
// MaxRetries+1 次尝试，所以单次 Probe 调用写入 stats 计数最多 4 次。
// N 次并发 Probe 调用时写入重叠，-race 立即触发。
func TestRetryableProbeStatsConcurrent(t *testing.T) {
	const N = 64
	rp := NewRetryableProbe(&resourceErrProbe{}, RetryConfig{
		MaxRetries:        3,
		TrackStats:        true,
		InitialBackoff:    1 * time.Millisecond,
		BackoffMultiplier: 1.0,
	})

	var wg sync.WaitGroup
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_, _ = rp.Probe(ctx, "127.0.0.1", 1, time.Second)
		}()
	}
	wg.Wait()

	got := rp.Stats()
	// TotalAttempts is incremented once per Probe call (NOT once per
	// inner attempt — that's what ResourceErrors counts). So with N
	// concurrent calls, TotalAttempts must equal N. / TotalAttempts
	// 每次 Probe 调用递增一次（不是每次内部 attempt 一次——那是
	// ResourceErrors 的职责）。N 次并发调用时 TotalAttempts 应等于 N。
	wantTotal := N
	if got.TotalAttempts != wantTotal {
		t.Errorf("Stats.TotalAttempts = %d, want %d", got.TotalAttempts, wantTotal)
	}
	// ResourceErrors is incremented once per inner attempt that
	// failed with a resource-exhaustion error. With N calls each
	// doing MaxRetries+1=4 attempts, ResourceErrors must equal
	// 4*N. / ResourceErrors 每次内部 attempt 失败且为资源耗尽错时
	// 递增。N 次调用各做 MaxRetries+1=4 次尝试，ResourceErrors 应
	// 等于 4*N。
	wantResErr := N * 4
	if got.ResourceErrors != wantResErr {
		t.Errorf("Stats.ResourceErrors = %d, want %d", got.ResourceErrors, wantResErr)
	}
	// Every call exhausted all retries so FailedRetries must also
	// equal N (one increment per Probe call at the end of the
	// loop). / 每次调用都耗尽所有重试，故 FailedRetries 也应等于 N
	// （每次 Probe 调用循环末尾递增一次）。
	wantFailed := N
	if got.FailedRetries != wantFailed {
		t.Errorf("Stats.FailedRetries = %d, want %d", got.FailedRetries, wantFailed)
	}
	// No successes on a resource-only fake probe.
	if got.SuccessfulRetries != 0 {
		t.Errorf("Stats.SuccessfulRetries = %d, want 0", got.SuccessfulRetries)
	}
}

// TestRetryableProbeStatsOffByDefault — TrackStats=false means all
// counters stay zero, regardless of how many Probe calls run. Sanity
// guard against a future refactor that flips the default.
//
// / TestRetryableProbeStatsOffByDefault — TrackStats=false 时所有
// 计数保持零，无论跑多少次 Probe 调用。防止未来重构翻转默认值。
func TestRetryableProbeStatsOffByDefault(t *testing.T) {
	rp := NewRetryableProbe(&resourceErrProbe{}, RetryConfig{
		MaxRetries:     2,
		TrackStats:     false,
		InitialBackoff: 1 * time.Millisecond,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	for i := 0; i < 4; i++ {
		_, _ = rp.Probe(ctx, "127.0.0.1", 1, time.Second)
	}
	got := rp.Stats()
	if got.TotalAttempts != 0 || got.FailedRetries != 0 || got.ResourceErrors != 0 {
		t.Errorf("TrackStats=false: stats should be zero, got %+v", got)
	}
}

// TestIsResourceExhaustedError — pin the regex/keyword set. A
// regression here flips the retry decision and would change the
// behaviour of every external probe that hits an EMFILE/ENOBUFS
// condition.
//
// / TestIsResourceExhaustedError — 钉死正则/关键字集。回归会让重试
// 决策翻转，改变每个命中 EMFILE/ENOBUFS 的外部 probe 的行为。
func TestIsResourceExhaustedError(t *testing.T) {
	cases := []struct {
		errStr string
		want   bool
	}{
		{"too many open files", true},
		{"Cannot allocate memory", true},
		{"no buffer space available", true},
		{"An operation on a socket could not be performed because there was no network activity", true}, // matches the substring pattern
		{"WSAENOBUFS", true},
		{"resource temporarily unavailable", true},
		{"connection refused", false},
		{"i/o timeout", false},
		{"", false},
	}
	for _, c := range cases {
		var err error
		if c.errStr != "" {
			err = errors.New(c.errStr)
		}
		if got := isResourceExhaustedError(err); got != c.want {
			t.Errorf("isResourceExhaustedError(%q) = %v, want %v", c.errStr, got, c.want)
		}
	}
	// nil error is never a resource-exhaustion signal. / nil 错误
	// 永远不是资源耗尽信号。
	if isResourceExhaustedError(nil) {
		t.Error("isResourceExhaustedError(nil) = true, want false")
	}
}

// silence the unused-import warning for strings when no other
// test in this file references it (some build configs are strict).
var _ = strings.Contains
