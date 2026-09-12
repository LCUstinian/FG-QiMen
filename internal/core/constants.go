// Package core provides pipeline constants for FG-QiMen.
// Package core 提供 FG-QiMen 的管线常量。
package core

import "time"

// Pipeline constants / 管线常量
const (
	// DefaultChannelBuffer is the default size for pipeline channels.
	// 管线 channel 的默认容量。
	DefaultChannelBuffer = 1024

	// DefaultMinThreads is the minimum number of scan threads. Kept
	// at 50 (not 1): even with the shrink heuristic removed, a floor
	// of 1 would let a mis-tuned caller turn a /24 sweep serial.
	// 扫描线程最小值。保持 50（不是 1）：即使缩容启发式已移除，
	// 下限为 1 仍可能让配置不当的调用方把 /24 扫描变成串行。
	DefaultMinThreads = 50

	// DefaultMaxThreads is the maximum number of scan threads.
	// 扫描线程最大值。
	DefaultMaxThreads = 500

	// DefaultPluginWorkers is the default maximum number of plugin workers.
	// 插件 worker 默认上限。
	DefaultPluginWorkers = 16

	// DefaultUDPThreads / DefaultUDPMaxThreads bound the UDP
	// service-probe phase's adaptive pool. UDP "silence → open"
	// results would push a freely-growing pool upward while every
	// probe still waits out its full read deadline, so the pool is
	// capped lower than the TCP pool (500). The UDP phase runs AFTER
	// the TCP scan inside the same Stage-1 goroutine, so this also
	// bounds its wall-clock interference.
	// / DefaultUDPThreads / DefaultUDPMaxThreads 限制 UDP 服务探测阶
	// 段的自适应池。UDP"静默 → open"的结果会让自由增长的池在每个
	// probe 还在等满读超时时继续上推，因此上限比 TCP 池（500）低。
	// UDP 阶段在 Stage-1 goroutine 内、TCP 扫描之后串行跑，这也限
	// 制了它对总时长的干扰。
	DefaultUDPThreads    = 128
	DefaultUDPMaxThreads = 200

	// DefaultUDPProbeTimeout caps the UDP phase's per-probe timeout
	// regardless of --timeout: a slow-WAN-tuned 5s TCP timeout would
	// double every silent UDP port's cost.
	// / DefaultUDPProbeTimeout 限制 UDP 阶段的单 probe 超时，不受
	// --timeout 影响：慢 WAN 调优出的 5s TCP 超时会让每个静默 UDP
	// 端口的成本翻倍。
	DefaultUDPProbeTimeout = 2 * time.Second

	// DefaultStatsInterval is the interval for periodic stats push.
	// 周期性 stats 推送间隔。
	DefaultStatsInterval = 1 * time.Second

	// BannerMaxLength is the maximum length of banner to display.
	// banner 显示最大长度。
	BannerMaxLength = 80
)

// Scan pool constants / 扫描池常量
const (
	// DefaultScanTimeout is the default timeout for port scan.
	// 端口扫描默认超时。
	DefaultScanTimeout = 3 * time.Second

	// DefaultAdjustInterval is the interval for adaptive thread adjustment.
	// 自适应线程调整间隔。
	DefaultAdjustInterval = 500 * time.Millisecond

	// DefaultBackoffWait is the wait time on error backoff.
	// 错误退避等待时间。
	DefaultBackoffWait = 1 * time.Millisecond

	// DefaultMaxSamples is the maximum number of samples for adaptive pool.
	// 自适应池最大样本数。
	DefaultMaxSamples = 256

	// DefaultMinSamples is the minimum samples before adjustment.
	// 调整前最小样本数。
	DefaultMinSamples = 16

	// DefaultScaleDown is the scale-down factor (0.75 = 25% reduction).
	// 缩容因子（0.75 = 减少 25%）。
	DefaultScaleDown = 0.75

	// DefaultScaleUp is the scale-up factor (1.25 = 25% increase).
	// 扩容因子（1.25 = 增加 25%）。
	DefaultScaleUp = 1.25
)
