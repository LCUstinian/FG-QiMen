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
	// service-probe phase's adaptive pool. The A4 loopback sweep
	// (BENCH-A4.md) showed the throughput knee at 800: 200 → 8.7s,
	// 800 → 4.6s (-47%), 1600 → 4.4s (-3% only) with stable records
	// and zero probe errors even at 2× the knee — so 800 ships as
	// both the AIMD initial target and the hard cap (slow start still
	// ramps target/4 → 2× → knee). Resource-exhausted dials surface
	// to the AIMD controller, so FD starvation shrinks the pool
	// instead of silently dropping probes.
	// / DefaultUDPThreads / DefaultUDPMaxThreads 限制 UDP 服务探测阶
	// 段的自适应池。A4 回环扫描（BENCH-A4.md）显示吞吐拐点在 800：
	// 200 → 8.7s，800 → 4.6s（-47%），1600 → 4.4s（仅 -3%），记录数
	// 恒定、2× 拐点处仍零探测错误——故 800 同时出厂为 AIMD 初始
	// target 与硬上限（慢启动仍按 target/4 → 2× → 拐点爬坡）。资源
	// 耗尽的 dial 会浮出给 AIMD 控制器，FD 饥饿触发缩容而不是悄悄
	// 丢探测。
	DefaultUDPThreads    = 800
	DefaultUDPMaxThreads = 800

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

// v0.9 (需求B) enumeration constants / v0.9（需求B）枚举常量
const (
	// DefaultEnumMaxDepth bounds the read-only evidence walk: 1 = top
	// level only, 2 = one subdirectory level deep. Deep trees are
	// deliberately not traversed — enumeration is evidence collection,
	// not a full mirror.
	// / 只读取证遍历的深度上限：1 = 仅顶层，2 = 深入一层子目录。深层
	// 目录树刻意不遍历——枚举是取证，不是完整镜像。
	DefaultEnumMaxDepth = 2

	// DefaultEnumMaxEntries caps TOTAL captured entries per host
	// (across all shares / the whole FTP tree) so a huge NAS cannot
	// balloon the evidence file or stall a plugin worker.
	// / 每 host 捕获条目总数上限（跨全部共享 / 整棵 FTP 树），防巨型
	// NAS 撑爆证据文件或拖住 plugin worker。
	DefaultEnumMaxEntries = 200

	// DefaultEnumHostTimeout bounds ONE host's whole enumeration
	// (session + walk), independent of the TCP probe timeout — a
	// directory walk legitimately takes longer than a connect probe.
	// / 单 host 整次枚举（会话 + 遍历）的时间上限，独立于 TCP 探测超
	// 时——目录遍历合法地比连接探测慢。
	DefaultEnumHostTimeout = 30 * time.Second
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
