// Package scan: lock-free scan metrics + pool health assessment.
// Package scan: 无锁扫描度量 + 池健康评估。
//
// PoolMetrics feeds the AIMD pool controller (pool.go) with three
// signals, mirroring fscan's ScanMetrics:
//
//  1. exhaustion rate — resource-exhaustion errors (EMFILE / socket
//     starvation) over completed probes; the direct "we outgrew the
//     FD budget" signal.
//  2. RTT trend — dual exponential moving average (fast α=1/10,
//     slow α=1/50). fast/slow > 1 means latency is rising: the pool
//     is pushing the network harder than it can absorb.
//  3. throughput composition — opens / refusals / timeouts, so the
//     controller can tell "real responses" from "dead silence".
//
// All counters are atomic; the dual EMA is a lock-free CAS loop so
// workers never contend on a mutex in the hot path.
//
// PoolMetrics 给 pool.go 的 AIMD 控制器喂三类信号（对应 fscan 的
// ScanMetrics）：资源耗尽率（fd 预算不足的直接信号）、RTT 趋势
// （双 EMA，fast/slow > 1 表示延迟在上升）、吞吐构成（区分"真实
// 响应"与"死寂"）。全部 atomic；双 EMA 用 CAS 无锁更新，worker
// 热路径无锁竞争。
package scan

import (
	"runtime"
	"sync/atomic"
	"time"
)

// Env classifies the target network for the pool controller's health
// thresholds. It mirrors core.NetworkEnv (EnvProbe) but lives here to
// avoid a core → scan import cycle; core/scanner.go maps one to the
// other at wiring time.
// / Env 为池控制器的健康阈值刻画目标网络环境。对应 core 包的
// NetworkEnv（环境画像），放在本包以避免 core → scan 循环依赖；
// core/scanner.go 在接线时做映射。
type Env string

const (
	EnvLAN      Env = "LAN"      // sub-20ms median, low loss / 中位 <20ms、低丢包
	EnvWAN      Env = "WAN"      // 20-200ms median / 中位 20-200ms
	EnvInternet Env = "Internet" // >200ms median / 中位 >200ms
	EnvSlow     Env = "Slow"     // ≥50% loss or unusable path / 丢包 ≥50% 或路径不可用
)

// minHealthSamples is the minimum number of completed probes within
// one adjust interval before the controller trusts the deltas. Below
// it the sample is too thin to distinguish congestion from a quiet
// moment. / 一个评估周期内完成探测数低于该值时控制器不采信增量
// ——样本太薄，无法区分拥塞与瞬时安静。
const minHealthSamples = 30

// PoolMetrics is the shared signal store written by pool workers and
// read by the adaptive controller. Zero value is ready to use.
// / PoolMetrics 是由 pool worker 写入、自适应控制器读取的共享信号
// 存储。零值即可用。
type PoolMetrics struct {
	opens     atomic.Int64 // TCP/UDP handshake or response / 握手或有响应
	refused   atomic.Int64 // active RST (fast RTT ground truth) / 主动拒绝
	timeouts  atomic.Int64 // waited out the deadline / 等满超时
	exhausted atomic.Int64 // fd / socket starvation errors / 资源耗尽错误

	// Dual EMA RTT (nanoseconds): fast tracks recent trend, slow is
	// the baseline. ratio > 1 ⇒ latency rising. / 双 EMA RTT（纳秒）：
	// fast 跟踪近期趋势，slow 作基线。比值 > 1 ⇒ 延迟上升。
	rttFastNs  atomic.Int64
	rttSlowNs  atomic.Int64
	rttSamples atomic.Int64
}

// RecordOpen records a successful probe (connection established or a
// UDP response) with its RTT. / 记录一次成功探测（建连或 UDP 响应）
// 及其 RTT。
func (m *PoolMetrics) RecordOpen(rtt time.Duration) {
	m.opens.Add(1)
	m.recordRTT(rtt)
}

// RecordRefused records an active refusal (RST) with its RTT.
// / 记录一次主动拒绝（RST）及其 RTT。
func (m *PoolMetrics) RecordRefused(rtt time.Duration) {
	m.refused.Add(1)
	m.recordRTT(rtt)
}

// RecordTimeout records a probe that waited out its full deadline.
// / 记录一次等满超时的探测。
func (m *PoolMetrics) RecordTimeout() { m.timeouts.Add(1) }

// RecordExhausted records a resource-exhaustion error (EMFILE /
// socket starvation). / 记录一次资源耗尽错误（EMFILE / socket 耗尽）。
func (m *PoolMetrics) RecordExhausted() { m.exhausted.Add(1) }

// recordRTT feeds both EMAs. RTT ≤ 0 is skipped: silent probes (full
// deadline waits, silent UDP) report zero RTT and must never poison
// the trend — same invariant as AdaptiveTimeout's sample gate.
// / recordRTT 喂双 EMA。RTT ≤ 0 直接跳过：静默探测（等满超时、
// 静默 UDP）报零 RTT，绝不能毒化趋势——与 AdaptiveTimeout 的采样
// 门禁同一不变量。
func (m *PoolMetrics) recordRTT(rtt time.Duration) {
	ns := int64(rtt)
	if ns <= 0 {
		return
	}
	m.rttSamples.Add(1)
	// fast: α = 1/10 — new = old + (sample-old)/10
	updateEMA(&m.rttFastNs, ns, 10)
	// slow: α = 1/50 — baseline / 基线
	updateEMA(&m.rttSlowNs, ns, 50)
}

// updateEMA CAS-updates an EMA stored in an atomic.Int64. The zero
// value means "no sample yet" and is seeded with the first sample.
// / updateEMA 用 CAS 更新存在 atomic.Int64 里的 EMA。零值表示"尚无
// 样本"，用首个样本播种。
func updateEMA(target *atomic.Int64, sample int64, divisor int64) {
	for {
		old := target.Load()
		if old == 0 {
			if target.CompareAndSwap(0, sample) {
				return
			}
			runtime.Gosched()
			continue
		}
		next := old + (sample-old)/divisor
		if target.CompareAndSwap(old, next) {
			return
		}
		runtime.Gosched()
	}
}

// MetricsSnapshot is a point-in-time copy of the counters. The
// controller diffs consecutive snapshots to get per-interval rates.
// / MetricsSnapshot 是计数器的时点快照。控制器对相邻快照做差得到
// 周期内速率。
type MetricsSnapshot struct {
	Opens     int64
	Refused   int64
	Timeouts  int64
	Exhausted int64
	RTTFastNs int64
	RTTSlowNs int64
}

// Total returns the number of completed probes (all outcomes).
// / Total 返回已完成探测数（所有结局）。
func (s MetricsSnapshot) Total() int64 {
	return s.Opens + s.Refused + s.Timeouts + s.Exhausted
}

// Snapshot returns the current counter values.
// / Snapshot 返回当前计数器值。
func (m *PoolMetrics) Snapshot() MetricsSnapshot {
	return MetricsSnapshot{
		Opens:     m.opens.Load(),
		Refused:   m.refused.Load(),
		Timeouts:  m.timeouts.Load(),
		Exhausted: m.exhausted.Load(),
		RTTFastNs: m.rttFastNs.Load(),
		RTTSlowNs: m.rttSlowNs.Load(),
	}
}

// RTTRatio returns fast-EMA / slow-EMA. Values > 1 mean latency is
// rising relative to the baseline (congestion signal). Below
// rttMinSamples the estimate is noise, so it reports a neutral 1.0.
// / RTTRatio 返回 fast EMA / slow EMA。>1 表示延迟相对基线在上升
// （拥塞信号）。样本数低于 rttMinSamples 时估计值是噪声，返回中性
// 的 1.0。
func (m *PoolMetrics) RTTRatio() float64 {
	const rttMinSamples = 20
	if m.rttSamples.Load() < rttMinSamples {
		return 1.0
	}
	slow := m.rttSlowNs.Load()
	if slow <= 0 {
		return 1.0
	}
	return float64(m.rttFastNs.Load()) / float64(slow)
}

// HealthSignal is the controller's verdict for one adjust interval.
// / HealthSignal 是控制器对一个评估周期的判定。
type HealthSignal int

const (
	HealthUnknown   HealthSignal = iota // too few samples / 样本不足
	HealthGood                          // clean; may grow / 干净，可以提速
	HealthOK                            // acceptable; hold / 尚可，维持
	HealthStressed                      // mild pressure; ease off / 有压力，轻微降速
	HealthCongested                     // overload; back off hard / 明确过载，大幅退避
)

// envThresholds holds the health cut-offs for one network class.
// Inner-networks get tighter RTT expectations (a 2× RTT swing on a
// LAN is alarming; on the Internet it is a Tuesday); lossy/slow paths
// get the widest tolerance.
// / envThresholds 是一类网络的健康判据。内网 RTT 预期更紧（LAN 上
// RTT 翻倍是警报；公网上这是日常）；高丢包/慢路径容忍度最宽。
type envThresholds struct {
	congestExhaust float64
	stressExhaust  float64
	congestRTT     float64
	stressRTT      float64
	goodRTT        float64
}

// thresholdsFor returns the cut-offs for an Env. Unknown values
// (zero-value Env when profiling did not run) map to the WAN set —
// the middle of the road. / thresholdsFor 返回一类 Env 的判据。未知
// 值（画像未跑时的零值 Env）映射到 WAN 档——居中策略。
func thresholdsFor(e Env) envThresholds {
	switch e {
	case EnvLAN:
		return envThresholds{congestExhaust: 0.08, stressExhaust: 0.03,
			congestRTT: 1.8, stressRTT: 1.4, goodRTT: 1.15}
	case EnvInternet, EnvSlow:
		return envThresholds{congestExhaust: 0.25, stressExhaust: 0.10,
			congestRTT: 3.5, stressRTT: 2.5, goodRTT: 1.5}
	default: // WAN / unknown / WAN 或未知
		return envThresholds{congestExhaust: 0.15, stressExhaust: 0.05,
			congestRTT: 2.5, stressRTT: 1.8, goodRTT: 1.3}
	}
}

// assessHealth diffs snap against prev (which it replaces), then
// classifies the interval. Exhaustion rate takes priority over the
// RTT trend: a starving scanner distorts everything downstream.
//
// The no-response guard: an interval with zero opens AND zero
// refusals carries no ground truth about the network — silence can
// mean "idle-but-firewalled" just as easily as "healthy". Growing on
// it is how the old open-ratio heuristic misread hardened segments,
// so Good is demoted to OK when the interval was pure silence.
// Timeouts still count toward Total (they dilute, never drive, the
// exhaustion rate) — mirroring the avalanche lesson: a high filtered
// ratio is the network's posture, not scanner overload.
//
// / assessHealth 对 snap 与 prev 做差（并替换 prev），然后对区间分
// 类。耗尽率优先于 RTT 趋势：饿着的扫描器会扭曲一切下游信号。
// 无响应守则：一个既无 open 也无 refused 的区间不携带任何网络真
// 值——静默既可能是"空闲但被防火墙挡"，也可能是"健康"。在纯静默
// 上扩容正是旧 open 比例启发式误读加固网段的原因，因此该情形下
// Good 降级为 OK。timeout 仍计入 Total（只会稀释、不会驱动耗尽率
// ）——呼应雪崩教训：filtered 比例高是网络姿态，不是扫描器过载。
func (m *PoolMetrics) assessHealth(prev *MetricsSnapshot, e Env) (HealthSignal, MetricsSnapshot) {
	snap := m.Snapshot()
	deltaTotal := snap.Total() - prev.Total()
	deltaExhausted := snap.Exhausted - prev.Exhausted
	deltaOpens := snap.Opens - prev.Opens
	deltaRefused := snap.Refused - prev.Refused
	*prev = snap

	if deltaTotal < minHealthSamples {
		return HealthUnknown, snap
	}

	exhaustRate := float64(deltaExhausted) / float64(deltaTotal)
	ratio := m.RTTRatio()
	th := thresholdsFor(e)

	health := HealthOK
	switch {
	case exhaustRate > th.congestExhaust:
		health = HealthCongested
	case ratio > th.congestRTT:
		health = HealthCongested
	case exhaustRate > th.stressExhaust:
		health = HealthStressed
	case ratio > th.stressRTT:
		health = HealthStressed
	case exhaustRate < 0.01 && ratio < th.goodRTT:
		health = HealthGood
	}

	// Silence guard / 静默守则
	if health == HealthGood && deltaOpens == 0 && deltaRefused == 0 {
		health = HealthOK
	}
	return health, snap
}
