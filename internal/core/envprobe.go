// envprobe.go — pre-scan network environment profiling (borrowed from
// fscan's network_profiler.go + env_profiler.go).
//
// Before the pipeline starts, we sample a few targets with cheap TCP
// connects and classify the network (LAN / WAN / Internet / Slow). The
// profile then auto-tunes --timeout and --threads — but ONLY values the
// operator did not set explicitly (fscan's isExplicit pattern; a
// hand-tuned flag always wins).
//
// Key insight borrowed from fscan: a connection REFUSED is a valid RTT
// sample (the peer is reachable — the RST round-trip measures the path).
// Only timeouts / unreachable count as loss. This lets us profile even
// when every sampled port is closed.
//
// envprobe.go — 扫描前网络环境画像（借鉴 fscan 的 network_profiler.go
// 与 env_profiler.go）。
//
// 管线启动前，抽样少量目标做廉价 TCP 连接，把网络分类（LAN / WAN /
// Internet / Slow），画像随后自动调优 --timeout 与 --threads——但只调
// 操作员未显式指定的值（fscan 的 isExplicit 模式；手工调过的 flag 永远
// 优先）。
//
// 从 fscan 借来的关键洞察：connection REFUSED 是有效 RTT 样本（对端
// 可达——RST 往返即路径时延），只有超时/不可达才算丢包。因此即便抽样
// 端口全部关闭也能完成画像。
package core

import (
	"context"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/core/scan"
	"github.com/LCUstinian/FG-QiMen/internal/types"
)

// EnvProbeMinHosts is the target count below which profiling is
// skipped: small scans finish quickly regardless, and the ~1-3s
// sampling budget is better spent scanning. / EnvProbeMinHosts 是画像
// 的最小目标数：小扫描本来就快，~1-3s 的采样预算不如直接扫。
const EnvProbeMinHosts = 64

// envProbeConcurrency bounds the sampling dialers. / envProbeConcurrency
// 限制采样并发。
const envProbeConcurrency = 50

// envProbePorts are the fixed sampling ports (gateway/service ports
// with high refused-probability, mirroring fscan's probePorts).
// envProbePorts 是固定采样端口（高拒绝率的网关/服务端口，对齐 fscan
// 的 probePorts）。
var envProbePorts = []int{80, 443, 22, 445}

// NetworkEnv classifies the sampled path quality. / NetworkEnv 对抽样
// 的路径质量分类。
type NetworkEnv string

const (
	EnvLAN      NetworkEnv = "LAN"      // sub-20ms median, low loss / 中位 <20ms、低丢包
	EnvWAN      NetworkEnv = "WAN"      // 20-200ms median / 中位 20-200ms
	EnvInternet NetworkEnv = "Internet" // >200ms median / 中位 >200ms
	EnvSlow     NetworkEnv = "Slow"     // ≥50% loss or unusable path / 丢包 ≥50% 或路径不可用
)

// NetworkProfile is the sampling result. / NetworkProfile 是采样结果。
type NetworkProfile struct {
	Env       NetworkEnv
	MedianRTT time.Duration
	StddevRTT time.Duration
	LossRate  float64 // [0,1] / [0,1]
	Samples   int     // valid RTT samples / 有效 RTT 样本数
	Attempts  int     // total probes issued / 发出的探测总数
}

// ProbeNetwork samples up to sampleCount hosts (evenly spaced over the
// target list) against envProbePorts and returns the profile. A probe
// budget of ~3s is enforced by the caller's ctx if needed.
//
// ProbeNetwork 对最多 sampleCount 台主机（目标列表等距抽样）按
// envProbePorts 采样并返回画像。必要时调用方可用 ctx 限制 ~3s 预算。
func ProbeNetwork(ctx context.Context, hosts []string, probe scan.Probe, timeout time.Duration) NetworkProfile {
	if len(hosts) == 0 {
		return NetworkProfile{Env: EnvSlow, LossRate: 1}
	}

	// Evenly spaced sampling (fscan: index * step). Dedup so a tiny
	// list doesn't re-probe the same host. / 等距抽样（fscan：
	// index * step）。去重避免小列表重复探测同一主机。
	sampleCount := 10
	seen := map[string]struct{}{}
	var samples []string
	if len(hosts) <= sampleCount {
		samples = hosts
	} else {
		step := float64(len(hosts)) / float64(sampleCount)
		for i := 0; i < sampleCount; i++ {
			h := hosts[int(float64(i)*step)]
			if _, dup := seen[h]; !dup {
				seen[h] = struct{}{}
				samples = append(samples, h)
			}
		}
	}

	type task struct {
		host string
		port int
	}
	var tasks []task
	for _, h := range samples {
		for _, p := range envProbePorts {
			tasks = append(tasks, task{h, p})
		}
	}

	var (
		mu       sync.Mutex
		rtts     []float64 // milliseconds / 毫秒
		attempts int
	)
	sem := make(chan struct{}, envProbeConcurrency)
	var wg sync.WaitGroup

dispatch:
	for _, t := range tasks {
		select {
		case <-ctx.Done():
			break dispatch
		case sem <- struct{}{}:
		}
		wg.Add(1)
		go func(t task) {
			defer wg.Done()
			defer func() { <-sem }()

			start := time.Now()
			res, err := probe.Probe(ctx, t.host, t.port, timeout)
			elapsed := time.Since(start)

			mu.Lock()
			defer mu.Unlock()
			attempts++
			// Valid RTT: open or refused (peer reachable). Loss:
			// filtered / transport error. / 有效 RTT：open 或 refused
			//（对端可达）。丢包：filtered / 传输错误。
			if err == nil && (res.State == scan.StateOpen || res.State == scan.StateClosed) {
				rtts = append(rtts, float64(elapsed.Microseconds())/1000.0)
			}
		}(t)
	}
	wg.Wait()

	if attempts == 0 {
		return NetworkProfile{Env: EnvSlow, LossRate: 1}
	}
	p := NetworkProfile{Attempts: attempts, Samples: len(rtts), LossRate: 1 - float64(len(rtts))/float64(attempts)}
	if len(rtts) == 0 {
		p.Env = EnvSlow
		return p
	}
	sort.Float64s(rtts)
	median := rtts[len(rtts)/2]
	p.MedianRTT = time.Duration(median * float64(time.Millisecond))

	// Population stddev around the median (fscan uses 4σ margins; a
	// stddev against the median is outlier-robust). / 中位数附近的总体
	// 标准差（fscan 用 4σ 余量；对中位数求标准差更抗离群）。
	var sqSum float64
	for _, r := range rtts {
		d := r - median
		sqSum += d * d
	}
	p.StddevRTT = time.Duration(math.Sqrt(sqSum/float64(len(rtts))) * float64(time.Millisecond))

	switch {
	case p.LossRate >= 0.5:
		p.Env = EnvSlow
	case p.MedianRTT > 200*time.Millisecond:
		p.Env = EnvInternet
	case p.MedianRTT > 20*time.Millisecond:
		p.Env = EnvWAN
	default:
		p.Env = EnvLAN
	}
	return p
}

// ApplyEnvTuning adjusts cfg.Threads / cfg.Timeout from the profile,
// never touching operator-explicit values. Returns a human-readable
// summary of what (if anything) changed.
//
// ApplyEnvTuning 按画像调整 cfg.Threads / cfg.Timeout，绝不覆盖操作
// 员显式设置的值。返回变更（若有）的可读摘要。
func ApplyEnvTuning(cfg *types.Config, p NetworkProfile) string {
	if p.Env == "" {
		return ""
	}
	summary := fmt.Sprintf("env=%s median_rtt=%s loss=%.0f%% samples=%d/%d",
		p.Env, p.MedianRTT.Round(time.Millisecond), p.LossRate*100, p.Samples, p.Attempts)

	// Timeout: median + 4σ, floored by median*3+200ms (protects against
	// a tight σ understating a jittery path), clamped to [1s, 10s].
	// Skipped when loss ≥ 50%: surviving samples can't be trusted to
	// represent a lossy path (a low median would shorten the timeout
	// exactly when the path needs more patience).
	// / 超时：median + 4σ，下限 median*3+200ms（防止 σ 过小低估抖动
	// 路径），clamp 到 [1s, 10s]。丢包 ≥50% 时跳过：幸存样本不足以代
	// 表一条高丢包路径（低中位数会在路径最需要耐心时反而缩短超时）。
	if !cfg.TimeoutExplicit && p.Samples > 0 && p.LossRate < 0.5 {
		computed := p.MedianRTT + 4*p.StddevRTT
		floor := 3*p.MedianRTT + 200*time.Millisecond
		if computed < floor {
			computed = floor
		}
		if computed < 1*time.Second {
			computed = 1 * time.Second
		}
		if computed > 10*time.Second {
			computed = 10 * time.Second
		}
		if computed != cfg.Timeout {
			summary += fmt.Sprintf(", timeout %s → %s", cfg.Timeout, computed)
			cfg.Timeout = computed
			// PortTimeout/WebTimeout stay 0 ("same as Timeout") — the
			// downstream default already follows the tuned value.
			// / PortTimeout/WebTimeout 保持 0（"同 Timeout"）——下游默
			// 认值本就跟随调优后的主超时。
		}
	}

	// Threads: environment factor, further compressed by loss, clamped
	// to [DefaultMinThreads, DefaultMaxThreads]. / 线程：环境系数再按
	// 丢包压缩，clamp 到 [DefaultMinThreads, DefaultMaxThreads]。
	if !cfg.ThreadsExplicit {
		factor := 1.0
		switch p.Env {
		case EnvLAN:
			factor = 1.5
		case EnvWAN:
			factor = 1.0
		case EnvInternet:
			factor = 0.4
		case EnvSlow:
			factor = 0.15
		}
		if p.LossRate > 0.1 {
			factor *= 1 - p.LossRate
		}
		tuned := int(float64(cfg.Threads) * factor)
		if tuned < DefaultMinThreads {
			tuned = DefaultMinThreads
		}
		if tuned > DefaultMaxThreads {
			tuned = DefaultMaxThreads
		}
		if tuned != cfg.Threads {
			summary += fmt.Sprintf(", threads %d → %d", cfg.Threads, tuned)
			cfg.Threads = tuned
		}
	}
	return summary
}
