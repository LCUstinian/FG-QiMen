// Package core provides pipeline constants for FG-QiMen.
// Package core 提供 FG-QiMen 的管线常量。
package core

import "time"

// Pipeline constants / 管线常量
const (
	// DefaultChannelBuffer is the default size for pipeline channels.
	// 管线 channel 的默认容量。
	DefaultChannelBuffer = 1024

	// DefaultMinThreads is the minimum number of scan threads.
	// 扫描线程最小值。
	DefaultMinThreads = 1

	// DefaultMaxThreads is the maximum number of scan threads.
	// 扫描线程最大值。
	DefaultMaxThreads = 500

	// DefaultPluginWorkers is the default maximum number of plugin workers.
	// 插件 worker 默认上限。
	DefaultPluginWorkers = 16

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
