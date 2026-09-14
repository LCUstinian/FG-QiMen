// bench.go — the A1 benchmark judge: builds a loopback topology of
// fake services / blackholes, runs the real scan pipeline against it,
// and collects the three core metrics (latency P50/P95, identification
// ratio, probe cost/return) plus observed memory. Output is a NDJSON
// record file consumable by the baseline comparator.
//
// Design constraints honored from the v8 plan:
//   - zero edits to the scan core: the judge drives the same
//     core.RunScan entry the CLI uses, with an explicit Config so
//     env-profiler auto-tuning cannot add run-to-run variance;
//   - per-port latency = Result.Time − run start (end-to-end, queue
//     and scheduling included — the operator-visible definition);
//   - everything runs on 127.0.0.1 so the loopback is the only
//     network variable.
//
// / bench.go —— A1 基准裁判：构建回环拓扑（假服务/黑洞），对它跑真
// 实扫描管线，收集三核心指标（时延 P50/P95、识别率、probe 成本回报）
// 及观测内存。输出 NDJSON 记录文件供基线对比器消费。
//
// 遵守 v8 方案的设计约束：
//   - 零修改扫描核心：裁判驱动的就是 CLI 用的 core.RunScan 入口，
//     Config 全显式化，环境画像自动调优无法引入轮间方差；
//   - 单端口时延 = Result.Time − 扫描开始（端到端，含排队与调度
//     ——正是操作者可见的定义）；
//   - 全部跑在 127.0.0.1 上，回环是唯一网络变量。
package bench

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/core"
	"github.com/LCUstinian/FG-QiMen/internal/fakeserver"
	"github.com/LCUstinian/FG-QiMen/internal/output"
	"github.com/LCUstinian/FG-QiMen/internal/session"
	"github.com/LCUstinian/FG-QiMen/internal/types"
	"github.com/LCUstinian/FG-QiMen/internal/workspace"
)

// ServiceKind names a fake service in the topology.
// / ServiceKind 命名拓扑中的一个假服务。
type ServiceKind string

const (
	// ServiceSSH emits an OpenSSH-style greeting on connect: passive
	// banner grab → hard identification. / ServiceSSH 连接即发
	// OpenSSH 风格 banner：被动抓取 → 硬识别。
	ServiceSSH ServiceKind = "ssh"

	// ServiceHTTP answers an HTTP request with an nginx-style
	// response: identified via web path / active probes.
	// / ServiceHTTP 以 nginx 风格响应回答 HTTP 请求：经 web 路径/
	// 主动探针识别。
	ServiceHTTP ServiceKind = "http"

	// ServiceMemcached answers nmap stats probes with STAT lines:
	// silent greeting, so it exercises the TCP active-probe path and
	// the probe economics counters. / ServiceMemcached 以 STAT 行回
	// 答 nmap stats 探针：无 greeting，专门锻炼 TCP 主动探针路径与
	// probe 经济计数器。
	ServiceMemcached ServiceKind = "memcached"
)

// Topology describes one benchmark loopback layout.
// / Topology 描述一个基准回环布局。
type Topology struct {
	// Name labels the topology in results ("single" for A1).
	// / Name 在结果中标注拓扑（A1 为 "single"）。
	Name string

	// Services are started as normal fake servers.
	// / Services 以正常假服务启动。
	Services []ServiceKind

	// Blackholes is the count of silent ports (probe deadline burners).
	// / Blackholes 是静默端口数（烧 probe 超时用）。
	Blackholes int

	// Refusals is the count of closed ports (immediate RST).
	// / Refusals 是关闭端口数（立即 RST）。
	Refusals int
}

// Options controls a benchmark invocation.
// / Options 控制一次基准调用。
type Options struct {
	// Runs is the number of scan rounds over the topology.
	// / Runs 是对拓扑的扫描轮数。
	Runs int

	// ReadDelay/Jitter are injected into every normal fake server
	// (models RTT; zero = honest loopback). / ReadDelay/Jitter 注入
	// 每个正常假服务（模拟 RTT；零 = 诚实回环）。
	ReadDelay time.Duration
	Jitter    time.Duration

	// Timeout is the explicit per-probe timeout (no adaptive tuning —
	// the judge must not let env-profiler add variance).
	// / Timeout 是显式 probe 超时（不做自适应——裁判不允许环境画
	// 像引入方差）。
	Timeout time.Duration

	// Threads is the explicit pool upper bound. / Threads 是显式线
	// 程池上限。
	Threads int

	// WorkDir is the temp workspace root for outputs. Empty = a fresh
	// os.MkdirTemp, removed after the run. / WorkDir 是输出的临时
	// workspace 根。空 = 新建 os.MkdirTemp，跑完即删。
	WorkDir string
}

// LatencySample is one open port's end-to-end identification latency.
// / LatencySample 是一个开放端口的端到端识别时延。
type LatencySample struct {
	Run        int           `json:"run"`
	Port       int           `json:"port"`
	Service    string        `json:"service"`
	Latency    time.Duration `json:"latency_ns"`
	Product    string        `json:"product,omitempty"`
	Confidence string        `json:"confidence,omitempty"`
}

// Meta is the summary record of one benchmark invocation — the unit
// the baseline comparator diffs. / Meta 是一次基准调用的汇总记录
// ——基线对比器的 diff 单元。
type Meta struct {
	Type            string        `json:"type"` // always "meta"
	Date            string        `json:"date"`
	Topology        string        `json:"topology"`
	Runs            int           `json:"runs"`
	ReadDelay       time.Duration `json:"read_delay_ns"`
	GoVersion       string        `json:"go_version"`
	GOMAXPROCS      int           `json:"gomaxprocs"`
	OS              string        `json:"os"`
	LatencyP50      time.Duration `json:"latency_p50_ns"`
	LatencyP95      time.Duration `json:"latency_p95_ns"`
	IdentifiedRatio float64       `json:"identified_ratio"` // (hard+soft)/(hard+soft+none)
	ProbeHitRate    float64       `json:"probe_hit_rate"`   // hits/probes
	ProbePorts      int64         `json:"probe_ports"`
	ProbeHits       int64         `json:"probe_hits"`
	IdentHard       int64         `json:"ident_hard"`
	IdentSoft       int64         `json:"ident_soft"`
	IdentNone       int64         `json:"ident_none"`
	WallMean        time.Duration `json:"wall_mean_ns"` // per-run wall clock
	PeakRSSMB       float64       `json:"peak_rss_mb"`
}

// Result bundles the summary with per-port samples for inspection.
// / Result 把汇总与逐端口样本打包供检查。
type Result struct {
	Meta    Meta
	Samples []LatencySample
}

// Run executes Runs rounds of the topology and aggregates metrics.
// / Run 执行 Runs 轮拓扑扫描并汇总指标。
func Run(topo Topology, opts Options) (*Result, error) {
	if opts.Runs <= 0 {
		opts.Runs = 1
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 2 * time.Second
	}
	if opts.Threads <= 0 {
		opts.Threads = 64
	}

	workDir := opts.WorkDir
	removeTemp := false
	if workDir == "" {
		var err error
		workDir, err = os.MkdirTemp("", "fgqm-bench-")
		if err != nil {
			return nil, fmt.Errorf("bench: temp workspace: %w", err)
		}
		removeTemp = true
	}
	defer func() {
		if removeTemp {
			_ = os.RemoveAll(workDir)
		}
	}()
	workspace.SetRoot(workDir)

	res := &Result{}
	var allLatencies []time.Duration
	var wallSum time.Duration

	for run := 1; run <= opts.Runs; run++ {
		meta, samples, wall, err := runOnce(topo, opts, run, workDir)
		if err != nil {
			return nil, err
		}
		allLatencies = append(allLatencies, sampleLatencies(samples)...)
		wallSum += wall
		if run == opts.Runs {
			// Counters from the final round stand in for the whole
			// set (each round is an independent Session; the
			// topology is identical so the ratios are stable).
			// / 末轮计数器代表全集（每轮独立 Session；拓扑相同，
			// 比率稳定）。
			res.Meta.IdentHard = meta.IdentHard
			res.Meta.IdentSoft = meta.IdentSoft
			res.Meta.IdentNone = meta.IdentNone
			res.Meta.ProbePorts = meta.ProbePorts
			res.Meta.ProbeHits = meta.ProbeHits
		}
		res.Samples = append(res.Samples, samples...)
	}

	res.Meta.Type = "meta"
	res.Meta.Date = time.Now().Format(time.RFC3339)
	res.Meta.Topology = topo.Name
	res.Meta.Runs = opts.Runs
	res.Meta.ReadDelay = opts.ReadDelay
	res.Meta.GoVersion = runtime.Version()
	res.Meta.GOMAXPROCS = runtime.GOMAXPROCS(0)
	res.Meta.OS = runtime.GOOS
	res.Meta.LatencyP50 = percentile(allLatencies, 50)
	res.Meta.LatencyP95 = percentile(allLatencies, 95)
	res.Meta.IdentifiedRatio = ratio(float64(res.Meta.IdentHard+res.Meta.IdentSoft),
		float64(res.Meta.IdentHard+res.Meta.IdentSoft+res.Meta.IdentNone))
	res.Meta.ProbeHitRate = ratio(float64(res.Meta.ProbeHits), float64(res.Meta.ProbePorts))
	res.Meta.WallMean = wallSum / time.Duration(opts.Runs)
	res.Meta.PeakRSSMB = peakRSSMB()
	return res, nil
}

// runOnce boots a fresh topology, scans it, and harvests results.
// / runOnce 启动全新拓扑、扫描并收割结果。
func runOnce(topo Topology, opts Options, run int, workDir string) (roundMeta, []LatencySample, time.Duration, error) {
	servers := make([]*fakeserver.Server, 0, len(topo.Services)+topo.Blackholes)
	servicePorts := make([]int, 0, len(topo.Services))
	portService := map[int]ServiceKind{}
	defer func() {
		for _, s := range servers {
			_ = s.Close()
		}
	}()

	srvOpts := fakeserver.Options{
		Mode:      fakeserver.ModeNormal,
		ReadDelay: opts.ReadDelay,
		Jitter:    opts.Jitter,
	}
	for _, kind := range topo.Services {
		srv, err := fakeserver.NewTCP(srvOpts, serviceHandler(kind))
		if err != nil {
			return roundMeta{}, nil, 0, fmt.Errorf("bench: %s server: %w", kind, err)
		}
		servers = append(servers, srv)
		servicePorts = append(servicePorts, srv.Port())
		portService[srv.Port()] = kind
	}

	var blackholePorts, refusalPorts []int
	for i := 0; i < topo.Blackholes; i++ {
		srv, err := fakeserver.NewTCP(fakeserver.Options{Mode: fakeserver.ModeBlackhole}, nil)
		if err != nil {
			return roundMeta{}, nil, 0, fmt.Errorf("bench: blackhole: %w", err)
		}
		servers = append(servers, srv)
		blackholePorts = append(blackholePorts, srv.Port())
	}
	for i := 0; i < topo.Refusals; i++ {
		// Closed ports: bind briefly, note the port, release — a
		// later RST is near-certain (TOCTOU odds negligible for a
		// benchmark). / 关闭端口：短暂绑定记下端口号即释放——随后
		// RST 几乎必然（基准场景 TOCTOU 概率可忽略）。
		port, err := grabAndRelease()
		if err != nil {
			return roundMeta{}, nil, 0, fmt.Errorf("bench: refusal port: %w", err)
		}
		refusalPorts = append(refusalPorts, port)
	}

	allPorts := append(append([]int{}, servicePorts...), blackholePorts...)
	allPorts = append(allPorts, refusalPorts...)

	cfg := &types.Config{
		Mode:            types.ModeScan,
		Host:            "127.0.0.1",
		Ports:           joinPorts(allPorts),
		ExpandScope:     "off",
		NoState:         true,
		NoICMP:          true,
		NoSubnetProbe:   true,
		Threads:         opts.Threads,
		ThreadsExplicit: true,
		Timeout:         opts.Timeout,
		TimeoutExplicit: true,
		PortTimeout:     opts.Timeout,
		WebTimeout:      opts.Timeout,
		ShutdownTimeout: 5 * time.Second,
	}
	if err := cfg.Validate(); err != nil {
		return roundMeta{}, nil, 0, fmt.Errorf("bench: config: %w", err)
	}

	ctx := context.Background()
	sess, err := session.NewSession(ctx, cfg, "")
	if err != nil {
		return roundMeta{}, nil, 0, fmt.Errorf("bench: session: %w", err)
	}

	resultPath := filepath.Join(workDir, fmt.Sprintf("bench_run%d.ndjson", run))
	out, err := output.OpenOutput(output.OutputConfig{ResultJSONPath: resultPath})
	if err != nil {
		return roundMeta{}, nil, 0, fmt.Errorf("bench: output: %w", err)
	}
	sess.Out = out

	start := time.Now()
	exitCode, err := core.RunScan(ctx, sess)
	wall := time.Since(start)
	closeErr := out.Close()
	if err != nil {
		return roundMeta{}, nil, 0, fmt.Errorf("bench: scan: %w (exit %d)", err, exitCode)
	}
	if closeErr != nil {
		return roundMeta{}, nil, 0, fmt.Errorf("bench: output close: %w", closeErr)
	}

	cv := sess.State.Snapshot()
	rm := roundMeta{
		IdentHard:  cv.IdentHard,
		IdentSoft:  cv.IdentSoft,
		IdentNone:  cv.IdentNone,
		ProbePorts: cv.ProbePorts,
		ProbeHits:  cv.ProbeHits,
	}

	samples, err := harvestSamples(resultPath, run, start, portService)
	if err != nil {
		return roundMeta{}, nil, 0, fmt.Errorf("bench: harvest: %w", err)
	}
	return rm, samples, wall, nil
}

// roundMeta carries the per-run counters of interest.
// / roundMeta 携带单轮关心的计数器。
type roundMeta struct {
	IdentHard, IdentSoft, IdentNone int64
	ProbePorts, ProbeHits           int64
}

// serviceHandler returns a minimal protocol responder for the kind.
// Banners mirror golden/RFC/public-protocol samples — no real product
// is impersonated beyond a single identifying line.
// / serviceHandler 返回该类别的最小协议应答器。banner 对齐 golden/
// RFC/公开协议样本——除单行身份行外不冒充真实产品。
func serviceHandler(kind ServiceKind) func(net.Conn) {
	return func(c net.Conn) {
		switch kind {
		case ServiceSSH:
			// Passive greeting: server speaks first (hard match).
			// / 被动 greeting：服务端先说话（硬匹配）。
			_, _ = c.Write([]byte("SSH-2.0-OpenSSH_9.2p1 Debian-2+deb12u1\r\n"))
		case ServiceHTTP:
			// Read the request head, then answer with an nginx-style
			// response carrying a Server line. / 读取请求头后以带
			// Server 行的 nginx 风格响应回答。
			buf := make([]byte, 2048)
			_, _ = c.Read(buf)
			_, _ = c.Write([]byte("HTTP/1.1 200 OK\r\nServer: nginx/1.24.0\r\nContent-Length: 0\r\nConnection: close\r\n\r\n"))
		case ServiceMemcached:
			// No greeting. Answer any payload with STAT lines — the
			// upstream softmatch sample. / 无 greeting。任何 payload
			// 以 STAT 行回答——上游 softmatch 样本。
			buf := make([]byte, 2048)
			_, _ = c.Read(buf)
			_, _ = c.Write([]byte("STAT pid 1\r\nSTAT version 1.6.22\r\nEND\r\n"))
		}
	}
}

// grabAndRelease binds 127.0.0.1:0, records the port, and closes.
// / grabAndRelease 绑定 127.0.0.1:0，记下端口后关闭。
func grabAndRelease() (int, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer ln.Close()
	if ta, ok := ln.Addr().(*net.TCPAddr); ok {
		return ta.Port, nil
	}
	return 0, fmt.Errorf("not a TCP address: %v", ln.Addr())
}

// harvestSamples reads the run's result NDJSON and converts open-port
// records into latency samples. / harvestSamples 读取该轮结果 NDJSON，
// 把开放端口记录转成时延样本。
func harvestSamples(path string, run int, start time.Time, portService map[int]ServiceKind) ([]LatencySample, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var samples []LatencySample
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var r types.Result
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			return nil, fmt.Errorf("decode result line: %w", err)
		}
		kind, ok := portService[r.Port]
		if !ok {
			continue // not a fake-service port (defensive) / 非假服务端口（防御）
		}
		samples = append(samples, LatencySample{
			Run:        run,
			Port:       r.Port,
			Service:    string(kind),
			Latency:    r.Time.Sub(start),
			Product:    r.Product,
			Confidence: r.Confidence,
		})
	}
	return samples, sc.Err()
}

func sampleLatencies(samples []LatencySample) []time.Duration {
	out := make([]time.Duration, 0, len(samples))
	for _, s := range samples {
		out = append(out, s.Latency)
	}
	return out
}

// percentile returns the p-th percentile (0–100) of the input.
// Nearest-rank on a sorted copy. / percentile 返回输入的 p 百分位数
// （0–100）。对排序副本取最近秩。
func percentile(vals []time.Duration, p int) time.Duration {
	if len(vals) == 0 {
		return 0
	}
	sorted := append([]time.Duration(nil), vals...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	idx := (len(sorted)*p + 99) / 100
	if idx < 1 {
		idx = 1
	}
	if idx > len(sorted) {
		idx = len(sorted)
	}
	return sorted[idx-1]
}

func ratio(num, den float64) float64 {
	if den == 0 {
		return 0
	}
	return num / den
}

func joinPorts(ports []int) string {
	parts := make([]string, 0, len(ports))
	for _, p := range ports {
		parts = append(parts, fmt.Sprintf("%d", p))
	}
	return strings.Join(parts, ",")
}
