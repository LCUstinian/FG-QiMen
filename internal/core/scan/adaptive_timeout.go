// Package scan: RTT-sampled adaptive probe timeout.
// Package scan: 基于 RTT 采样的自适应探测超时。
//
// AdaptiveTimeout refines the fixed pool timeout DURING the scan:
// every probe that actually reached a host (open handshake, refused
// RST) records its RTT; the recommended timeout becomes
// mean(RTT) + 4*stddev(RTT), clamped to [max(500ms, base/5), base].
// The base never grows beyond the operator's (possibly env-tuned)
// timeout, and an operator-explicit --timeout disables the mechanism
// at the wiring site (core/scanner.go), mirroring the ApplyEnvTuning
// convention.
//
// Sampling rule: StateOpen and StateClosed results are valid samples
// (both mean the host answered). Filtered results are NOT — their
// "RTT" is the full timeout wait, which would poison the ring buffer
// and freeze the timeout at its ceiling. The pool additionally
// guards on Result.RTT > 0 because silent UDP probes report
// open|filtered with a degenerate full-wait RTT.
//
// AdaptiveTimeout 在扫描过程中精修固定池超时：每个真正到达主机的
// probe（握手成功、被拒 RST）记录其 RTT；推荐超时变为
// mean(RTT) + 4*stddev(RTT)，clamp 到 [max(500ms, base/5), base]。
// base 永不超过操作员的（可能已被环境画像调优过的）超时；操作员
// 显式 --timeout 时在接线点（core/scanner.go）整体禁用本机制，与
// ApplyEnvTuning 的约定一致。
//
// 采样规则：StateOpen 与 StateClosed 是有效样本（都代表主机应答
// 了）。Filtered 不是——它的"RTT"是等满超时，会毒化环形缓冲并把
// 超时冻在上限。池侧另加 Result.RTT > 0 防线，因为静默 UDP 探测
// 以 open|filtered 语义返回一个退化的"等满"RTT。
//
// Algorithm borrowed from fscan (core/adaptive_timeout.go): 64-sample
// ring buffer, warmup of 10 samples, cached recomputation. The
// floor = max(500ms, base/5) guards against tight σ on a fast LAN
// understating tail latency under a 200-worker backlog (fscan issue
// #503 rationale).
// / 算法借鉴 fscan（core/adaptive_timeout.go）：64 样本环形缓冲、
// 10 样本冷启动、带脏标记的缓存重算。下限 max(500ms, base/5) 防
// 快速局域网上 σ 过小、低估 200 worker 背压下的尾延迟（fscan
// issue #503 的理由）。
package scan

import (
	"math"
	"sync"
	"time"
)

const (
	// adaptiveSamples is the ring buffer capacity.
	// / adaptiveSamples 是环形缓冲容量。
	adaptiveSamples = 64

	// adaptiveWarmup is the sample count before the computed timeout
	// is trusted; below it, the base timeout is returned as-is.
	// / adaptiveWarmup 是信任计算超时前需要的样本数；不足时原样
	// 返回 base 超时。
	adaptiveWarmup = 10

	// adaptiveMinFloor is the absolute lower bound of the computed
	// timeout. / adaptiveMinFloor 是计算超时的绝对下限。
	adaptiveMinFloor = 500 * time.Millisecond
)

// AdaptiveTimeout computes a per-probe timeout from recorded RTT
// samples. Safe for concurrent use.
// / AdaptiveTimeout 按记录的 RTT 样本计算每个 probe 的超时。并发安全。
type AdaptiveTimeout struct {
	mu      sync.Mutex
	samples [adaptiveSamples]float64 // RTT in ms / RTT（毫秒）
	pos     int                      // next write slot / 下一个写入槽位
	count   int                      // total recorded / 累计记录数
	base    time.Duration            // operator/env ceiling / 操作员/环境上限
	floor   time.Duration            // computed lower bound / 计算下限
	cached  time.Duration
	dirty   bool
}

// NewAdaptiveTimeout returns an AdaptiveTimeout clamped to [floor, base].
// The floor is max(500ms, base/5). Base values ≤ 0 fall back to the
// 3s pool default.
// / NewAdaptiveTimeout 返回 clamp 到 [下限, base] 的 AdaptiveTimeout。
// 下限为 max(500ms, base/5)。base ≤ 0 时回退到 3s 池默认值。
func NewAdaptiveTimeout(base time.Duration) *AdaptiveTimeout {
	if base <= 0 {
		base = 3 * time.Second
	}
	floor := base / 5
	if floor < adaptiveMinFloor {
		floor = adaptiveMinFloor
	}
	if floor > base {
		floor = base
	}
	return &AdaptiveTimeout{
		base:  base,
		floor: floor,
	}
}

// Record adds one RTT sample (a probe that actually reached the host).
// Non-positive RTTs are ignored — callers may report 0 for "no
// response".
// / Record 添加一个 RTT 样本（真正到达主机的探测）。非正 RTT 被忽略
// ——调用方可能用 0 表示"无响应"。
func (a *AdaptiveTimeout) Record(rtt time.Duration) {
	if rtt <= 0 {
		return
	}
	a.mu.Lock()
	a.samples[a.pos] = float64(rtt) / float64(time.Millisecond)
	a.pos = (a.pos + 1) % adaptiveSamples
	if a.count < adaptiveSamples {
		a.count++
	}
	a.dirty = true
	a.mu.Unlock()
}

// Timeout returns the currently recommended per-probe timeout. With
// fewer than warmup samples it returns the base timeout. The
// mean+4σ computation runs outside the lock on a local copy.
// / Timeout 返回当前推荐的单 probe 超时。样本少于冷启动数时返回
// base 超时。mean+4σ 计算在锁外对本地副本执行。
func (a *AdaptiveTimeout) Timeout() time.Duration {
	a.mu.Lock()
	if a.count < adaptiveWarmup {
		a.mu.Unlock()
		return a.base
	}
	if !a.dirty {
		cached := a.cached
		a.mu.Unlock()
		return cached
	}
	n := a.count
	local := make([]float64, n)
	// Unroll the ring in chronological order. / 按时间顺序展开环形缓冲。
	start := (a.pos - n + adaptiveSamples) % adaptiveSamples
	for i := 0; i < n; i++ {
		local[i] = a.samples[(start+i)%adaptiveSamples]
	}
	a.mu.Unlock()

	var sum float64
	for _, s := range local {
		sum += s
	}
	mean := sum / float64(n)
	var sqSum float64
	for _, s := range local {
		d := s - mean
		sqSum += d * d
	}
	stddev := math.Sqrt(sqSum / float64(n))

	to := time.Duration(mean+4*stddev) * time.Millisecond
	if to < a.floor {
		to = a.floor
	}
	if to > a.base {
		to = a.base
	}

	a.mu.Lock()
	a.cached = to
	a.dirty = false
	a.mu.Unlock()
	return to
}
