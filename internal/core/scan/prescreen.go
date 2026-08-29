// Package scan: network segment pre-screening for large-scale scans.
// Package scan: 大规模扫描的网段预筛机制。
//
// Inspired by fscan's network pre-screening (port_scan.go L756-879), this
// module skips empty network segments by probing gateways (.1/.254) with
// rotating ports (22/80/443/3389) before full port scans.
//
// 借鉴 fscan 的网段预筛（port_scan.go L756-879），本模块通过探测网关
// （.1/.254）+ 轮换端口（22/80/443/3389）跳过空网段。
package scan

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
)

// PrescreenThreshold is the minimum number of hosts to trigger pre-screening.
// PrescreenThreshold 是触发预筛的最小主机数量。
const PrescreenThreshold = 256

// PrescreenOptions configures pre-screening behavior.
// PrescreenOptions 配置预筛行为。
type PrescreenOptions struct {
	// Enabled controls whether pre-screening is active.
	// Enabled 控制是否启用预筛。
	Enabled bool

	// Threshold is the minimum host count to trigger pre-screening.
	// Threshold 是触发预筛的最小主机数量。
	Threshold int

	// ProbePorts are the ports to probe on gateways (.1/.254).
	// ProbePorts 是在网关（.1/.254）上探测的端口。
	ProbePorts []int

	// Timeout is the per-gateway probe timeout.
	// Timeout 是每个网关探测超时。
	Timeout time.Duration

	// Concurrency is the number of concurrent gateway probes.
	// Concurrency 是并发网关探测数量。
	Concurrency int
}

// DefaultPrescreenOptions returns sensible defaults.
// DefaultPrescreenOptions 返回合理默认值。
func DefaultPrescreenOptions() PrescreenOptions {
	return PrescreenOptions{
		Enabled:     true,
		Threshold:   PrescreenThreshold,
		ProbePorts:  []int{22, 80, 443, 3389}, // SSH, HTTP, HTTPS, RDP
		Timeout:     2 * time.Second,
		Concurrency: 50,
	}
}

// Prescreener filters network segments by probing gateways.
// Prescreener 通过探测网关过滤网段。
type Prescreener struct {
	opts  PrescreenOptions
	probe Probe
}

// NewPrescreener creates a new Prescreener.
// NewPrescreener 创建新的 Prescreener。
func NewPrescreener(opts PrescreenOptions, probe Probe) *Prescreener {
	if probe == nil {
		probe = NewTCPConnectProbe()
	}
	return &Prescreener{
		opts:  opts,
		probe: probe,
	}
}

// ShouldPrescreen returns true if pre-screening should be applied.
// ShouldPrescreen 在应该应用预筛时返回 true。
func (p *Prescreener) ShouldPrescreen(hostCount int) bool {
	return p.opts.Enabled && hostCount >= p.opts.Threshold
}

// FilterHosts filters the host list by probing network gateways.
// FilterHosts 通过探测网关过滤主机列表。
//
// Returns hosts that are in responsive network segments. For hosts in
// segments where both .1 and .254 gateways are unreachable on all probe
// ports, they are excluded.
//
// 返回在响应网段中的主机。对于 .1 和 .254 网关在所有探测端口都不可达
// 的网段，排除其中的主机。
func (p *Prescreener) FilterHosts(ctx context.Context, hosts []string) []string {
	if !p.ShouldPrescreen(len(hosts)) {
		return hosts
	}

	// Group hosts by /24 network segment / 按 /24 网段分组主机
	segments := make(map[string][]string)
	for _, host := range hosts {
		ip := net.ParseIP(host)
		if ip == nil {
			// Not an IP, pass through / 非 IP，直接通过
			segments[""] = append(segments[""], host)
			continue
		}
		ip4 := ip.To4()
		if ip4 == nil {
			// IPv6, pass through (no pre-screening) / IPv6，直接通过（不预筛）
			segments[""] = append(segments[""], host)
			continue
		}
		// Extract /24 network (e.g., "192.168.1") / 提取 /24 网络（如 "192.168.1"）
		parts := strings.Split(host, ".")
		if len(parts) == 4 {
			network := strings.Join(parts[:3], ".")
			segments[network] = append(segments[network], host)
		} else {
			segments[""] = append(segments[""], host)
		}
	}

	// Probe gateways for each segment / 探测每个网段的网关
	liveSegments := p.probSegments(ctx, segments)

	// Rebuild host list with only live segments / 用活跃网段重建主机列表
	var result []string
	for network, segmentHosts := range segments {
		if network == "" {
			// Pass-through hosts / 直通主机
			result = append(result, segmentHosts...)
		} else if liveSegments[network] {
			result = append(result, segmentHosts...)
		}
		// else: skip dead segment / 否则：跳过死网段
	}

	return result
}

// probeSegments probes gateways (.1/.254) for each network segment.
// probeSegments 探测每个网段的网关（.1/.254）。
//
// Returns a map of network -> isLive. A segment is live if at least one
// gateway responds on at least one probe port.
//
// 返回 network -> isLive 的映射。如果至少一个网关在至少一个探测端口响应，
// 网段即为活跃。
//
// Task 6 (first-batch fixes): ctx cancellation now exits the
// dispatcher loop promptly. The previous implementation's
// `break` exited only the inner `select`, not the surrounding `for`,
// so each iteration after ctx fired would still try to acquire the
// semaphore (`sem <- struct{}{}`) and block there forever once the
// semaphore was full. The for loop kept dispatching goroutines
// that immediately saturated the semaphore and never released it.
// `wg.Wait()` then waited for the in-flight probes' full per-
// gateway timeout. The fix combines the ctx check and the
// semaphore acquire in one `select` and uses a labelled `break` to
// exit the for loop on either signal.
//
// 第一批修复 Task 6：ctx 取消现在让 dispatcher 循环立即退出。旧实
// 现的 `break` 只跳出内层 `select`，不跳外层 `for`，所以 ctx 触发
// 后的每次迭代仍尝试获取 semaphore（`sem <- struct{}{}`），一旦
// semaphore 满就永远阻塞在那里。for 循环继续派发立即占满 semaphore
// 且永不释放的 goroutine。`wg.Wait()` 接着等所有在飞探测走完完整
// per-gateway 超时。修法把 ctx 检查和 semaphore 获取合到一个
// `select` 里，用带 label 的 `break` 跳出 for 循环。
func (p *Prescreener) probSegments(ctx context.Context, segments map[string][]string) map[string]bool {
	liveSegments := make(map[string]bool)
	var mu sync.Mutex

	// Collect all gateway probe tasks / 收集所有网关探测任务
	type task struct {
		network string
		gateway string
	}
	tasks := make([]task, 0, 2*len(segments))
	for network := range segments {
		if network == "" {
			continue
		}
		// Probe .1 and .254 gateways / 探测 .1 和 .254 网关
		tasks = append(tasks, task{network, network + ".1"})
		tasks = append(tasks, task{network, network + ".254"})
	}

	// Worker pool to probe gateways concurrently / Worker 池并发探测网关
	sem := make(chan struct{}, p.opts.Concurrency)
	var wg sync.WaitGroup

dispatch:
	for _, t := range tasks {
		select {
		case <-ctx.Done():
			// Caller cancelled; stop dispatching and let
			// wg.Wait() reap the in-flight probes below. Their
			// inner Probe calls observe the same ctx and return
			// early. / 调用方取消；停止派发，让 wg.Wait() 收尾
			// 在飞的探测。其内层 Probe 调用观察到同一 ctx 并提
			// 前返回。
			break dispatch
		case sem <- struct{}{}:
			// Only dispatch after a successful acquire; one
			// acquire → one release via the goroutine's defer.
			// / 仅在成功获取后才派发；一次获取对应 goroutine
			// 中 defer 的一次释放。
		}
		wg.Add(1)

		go func(t task) {
			defer wg.Done()
			defer func() { <-sem }()

			// Try all probe ports on this gateway / 在此网关尝试所有探测端口
			for _, port := range p.opts.ProbePorts {
				res, err := p.probe.Probe(ctx, t.gateway, port, p.opts.Timeout)
				if err == nil && res.State == StateOpen {
					// Gateway responded, mark segment as live / 网关响应，标记网段为活跃
					mu.Lock()
					liveSegments[t.network] = true
					mu.Unlock()
					return // No need to try other ports / 无需尝试其他端口
				}
			}
		}(t)
	}

	wg.Wait()
	return liveSegments
}

// PrescreenSummary holds pre-screening statistics.
// PrescreenSummary 存放预筛统计信息。
type PrescreenSummary struct {
	TotalHosts     int
	FilteredHosts  int
	LiveSegments   int
	DeadSegments   int
	SkippedHosts   int
	PrescreenTime  time.Duration
	WasPrescreened bool
}

// String returns a human-readable summary.
// String 返回可读的摘要。
func (s PrescreenSummary) String() string {
	if !s.WasPrescreened {
		return "pre-screening: disabled"
	}
	return fmt.Sprintf(
		"pre-screening: %d hosts → %d hosts (skipped %d in %d dead segments, kept %d live segments) in %s",
		s.TotalHosts, s.FilteredHosts, s.SkippedHosts, s.DeadSegments, s.LiveSegments, s.PrescreenTime,
	)
}
