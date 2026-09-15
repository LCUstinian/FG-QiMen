// udpfarm.go — the A4 UDP benchmark: a loopback /28 farm of fake UDP
// services driven through the REAL RunScan pipeline with --udp on,
// measuring the UDP phase's wall clock. Where the TCP bench (bench.go)
// measures identification latency, A4 measures the throughput lever:
// the UDP pool's concurrency cap against a fleet of silent ports.
//
// Topology model (mirrors a firewalled LAN, the worst case for UDP):
//   - every expanded host gets RespPorts responsive fake services
//     (bind on the specific loopback IP; answer any datagram),
//   - every expanded host gets SilentPorts silent ports — modeled by
//     ONE wildcard (0.0.0.0) bind per port, so every host's that port
//     is open-but-idle and each probe pays the full read deadline,
//   - every expanded host gets ClosedPorts unbound ports — the kernel
//     answers ICMP port-unreachable → fast StateClosed.
//
// All candidate ports come from the live UDPHintPorts() set, so the
// farm only ever probes ports the UDP service probe actually targets.
// A port whose bind fails (OS service already owns it) is DROPPED from
// the target list entirely — the probe traffic never touches a port
// whose verdict would be contaminated by the host OS.
//
// / udpfarm.go —— A4 UDP 基准：回环 /28 假 UDP 服务农场，--udp 开启
// 走真实 RunScan 管线，量 UDP 阶段挂钟时间。TCP 基准（bench.go）量
// 识别时延，A4 量吞吐杠杆：UDP 池并发上限对阵静默端口大部队。
//
// 拓扑模型（对应防火墙 LAN——UDP 的最坏情形）：
//   - 每台展开主机有 RespPorts 个响应式假服务（绑定具体回环 IP；任
//     何 datagram 都应答），
//   - 每台主机有 SilentPorts 个静默端口——用每个端口一条 wildcard
//     （0.0.0.0）绑定模拟，于是所有主机的该端口都 open-but-idle，每
//     个探测都要付满读超时，
//   - 每台主机有 ClosedPorts 个未绑定端口——内核回 ICMP
//     port-unreachable → 快速 StateClosed。
//
// 全部候选端口来自实时 UDPHintPorts() 集，farm 只探测 UDP 服务探针
// 真正会打的端口。绑定失败的端口（OS 服务已占用）直接从目标集剔除
// ——探测流量不碰裁决会被宿主 OS 污染的端口。
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
	"github.com/LCUstinian/FG-QiMen/internal/portscan/fingerprint"
	"github.com/LCUstinian/FG-QiMen/internal/session"
	"github.com/LCUstinian/FG-QiMen/internal/types"
	"github.com/LCUstinian/FG-QiMen/internal/workspace"
)

// UDP farm topology defaults. Sized so one round is sweepable (~5s at
// the shipped 200-thread cap, sub-second at high caps) while the
// silent fleet still dominates the wall clock.
// / UDP farm 拓扑默认值。规模取在一轮可扫描（出厂 200 线程上限约
// 5s，高上限亚秒级），同时静默端口大部队仍主导挂钟。
const (
	// UDPFarmCIDR is the loopback farm network. The whole 127/8 is
	// loopback-routable on Windows and Linux, so a nonstandard range
	// avoids colliding with real services on 127.0.0.x.
	// / UDPFarmCIDR 是回环农场网段。Windows 与 Linux 上整个 127/8 都
	// 可回环路由，取非标准段避免与 127.0.0.x 上的真实服务相撞。
	UDPFarmCIDR = "127.66.0.0/28"

	// DefaultUDPFarmSilent/Resp/Closed are the per-host port counts.
	// / UDP farm 的每主机端口数默认值。
	DefaultUDPFarmSilent = 30
	DefaultUDPFarmResp   = 3
	DefaultUDPFarmClosed = 2

	// udpFarmFixedOverhead is a rough expectation of the non-UDP-phase
	// wall time (env probe + alive sweep + fast TCP refusal storm) —
	// documented, not enforced. / 非 UDP 阶段挂钟的粗略预期（环境画像
	// + alive sweep + 快速 TCP 拒绝风暴）——仅说明，不强制。
	udpFarmFixedOverhead = "≈1-2s"
)

// UDPOptions controls a UDP farm benchmark invocation.
// / UDPOptions 控制一次 UDP farm 基准调用。
type UDPOptions struct {
	// Runs is the number of scan rounds. / 扫描轮数。
	Runs int

	// Timeout is the explicit per-probe timeout — it caps both the TCP
	// phase and the UDP read budget (udpBase = min(Timeout, 2s)).
	// / 显式 probe 超时——同时限制 TCP 阶段与 UDP 读预算
	//（udpBase = min(Timeout, 2s)）。
	Timeout time.Duration

	// Threads is the TCP pool cap (the TCP phase is all-refusals here;
	// kept explicit for determinism). / TCP 池上限（TCP 阶段全是拒绝；
	// 为确定性保持显式）。
	Threads int

	// UDPThreads / UDPMax override the UDP pool sizing (zero = shipped
	// defaults). This is the A4 sweep axis. / UDP 池尺寸覆写（零 =
	// 出厂默认）。A4 扫描轴。
	UDPThreads int
	UDPMax     int

	// Strict enables --udp-strict semantics (silent → filtered →
	// dropped). / 启用 --udp-strict 语义（静默 → filtered → 丢弃）。
	Strict bool

	// SilentPorts/RespPorts/ClosedPorts size the farm. Zero = default.
	// / farm 的端口规模。零 = 默认。
	SilentPorts int
	RespPorts   int
	ClosedPorts int

	// WorkDir is the temp workspace root. Empty = fresh temp dir,
	// removed after. / 临时 workspace 根。空 = 新建临时目录，跑完即删。
	WorkDir string
}

// UDPResult is one UDP farm benchmark invocation's summary.
// / UDPResult 是一次 UDP farm 基准的汇总。
type UDPResult struct {
	Meta UDPResultMeta `json:"meta"`
}

// UDPResultMeta records the invocation parameters and the headline
// metric (wall mean). NDJSON-record shaped so Save() can store it.
// / UDPResultMeta 记录调用参数与核心指标（挂钟均值）。NDJSON 记录
// 形状，供 Save() 落盘。
type UDPResultMeta struct {
	Type       string        `json:"type"` // always "udp-meta" / 恒为 "udp-meta"
	Date       string        `json:"date"`
	Topology   string        `json:"topology"`
	Runs       int           `json:"runs"`
	Timeout    time.Duration `json:"timeout_ns"`
	Strict     bool          `json:"strict"`
	UDPThreads int           `json:"udp_threads"`
	UDPMax     int           `json:"udp_max_threads"`
	Hosts      int           `json:"hosts"`
	Ports      int           `json:"udp_ports"`
	GoVersion  string        `json:"go_version"`
	OS         string        `json:"os"`

	// Headline metrics / 核心指标
	WallMean   time.Duration `json:"wall_mean_ns"`
	WallMin    time.Duration `json:"wall_min_ns"`
	WallMax    time.Duration `json:"wall_max_ns"`
	UDPResults int           `json:"udp_results"` // open|filtered records / open|filtered 记录数

	// A4 sweep identity / A4 扫描身份
	Label string `json:"label,omitempty"`
}

// UDPFarmPlan records which port played which role in the last built
// farm — the truth table the result sanity check is measured against.
// / UDPFarmPlan 记录上次构建的农场里各端口扮演的角色——结果健全性
// 检查的对照真值表。
type UDPFarmPlan struct {
	Hosts           string `json:"hosts"`
	ResponsivePorts []int  `json:"responsive_ports"`
	SilentPorts     []int  `json:"silent_ports"`
	ClosedPorts     []int  `json:"closed_ports"`
}

// RunUDP executes Runs rounds of the UDP farm and aggregates the wall
// clock. / RunUDP 执行 Runs 轮 UDP farm 并聚合挂钟。
func RunUDP(opts UDPOptions) (*UDPResult, error) {
	if opts.Runs <= 0 {
		opts.Runs = 1
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 2 * time.Second
	}
	if opts.Threads <= 0 {
		opts.Threads = 64
	}
	if opts.SilentPorts <= 0 {
		opts.SilentPorts = DefaultUDPFarmSilent
	}
	if opts.RespPorts <= 0 {
		opts.RespPorts = DefaultUDPFarmResp
	}
	if opts.ClosedPorts <= 0 {
		opts.ClosedPorts = DefaultUDPFarmClosed
	}

	workDir := opts.WorkDir
	removeTemp := false
	if workDir == "" {
		var err error
		workDir, err = os.MkdirTemp("", "fgqm-bench-udp-")
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

	plan, err := buildUDPFarmPlan(opts)
	if err != nil {
		return nil, err
	}
	if len(plan.SilentPorts) == 0 {
		return nil, fmt.Errorf("bench: udp farm: no bindable silent ports (hint set too small or OS owns them all)")
	}
	// A degenerate run without responsive ports benchmarks nothing
	// useful and confuses the accounting invariants, so fail fast.
	// The usual cause: macOS routes only 127.0.0.1 to lo0 — the rest
	// of 127/8 needs manual aliases (root) — and unprivileged binds
	// of the classic <1024 responders fail there too.
	// / 没有响应式端口的退化跑法测不出任何有用信息，还会扰乱记账不
	// 变式，因此快速失败。常见原因：macOS 只把 127.0.0.1 路由到
	// lo0——127/8 其余地址需要手工别名（root）——且经典 <1024 应答
	// 端口在非特权下同样绑不上。
	if len(plan.ResponsivePorts) == 0 {
		return nil, fmt.Errorf("bench: udp farm: no bindable responsive ports — 127/8 multi-address loopback unavailable (macOS routes only 127.0.0.1 to lo0 without manual aliases)")
	}

	// Expand the farm hosts ONCE so the plan's Hosts and the actual
	// target set agree even if ExpandTargets trims network/broadcast.
	// / 只展开一次农场主机，即使 ExpandTargets 裁掉网络/广播地址，
	// 计划的 Hosts 与实际目标集也保持一致。
	targets, err := types.ExpandTargets(plan.Hosts, "")
	if err != nil {
		return nil, fmt.Errorf("bench: expand farm hosts: %w", err)
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("bench: expand farm hosts: no hosts from %q", plan.Hosts)
	}

	allPorts := append(append([]int{}, plan.ResponsivePorts...), plan.SilentPorts...)
	allPorts = append(allPorts, plan.ClosedPorts...)
	sort.Ints(allPorts)

	res := &UDPResult{Meta: UDPResultMeta{
		Type:       "udp-meta",
		Date:       time.Now().Format(time.RFC3339),
		Topology:   "udp-farm",
		Runs:       opts.Runs,
		Timeout:    opts.Timeout,
		Strict:     opts.Strict,
		UDPThreads: opts.UDPThreads,
		UDPMax:     opts.UDPMax,
		Hosts:      len(targets),
		Ports:      len(allPorts),
		GoVersion:  runtime.Version(),
		OS:         runtime.GOOS,
	}}

	var walls []time.Duration
	for run := 1; run <= opts.Runs; run++ {
		wall, udpCount, err := runUDPOnce(plan, targets, allPorts, opts, run, workDir)
		if err != nil {
			return nil, err
		}
		walls = append(walls, wall)
		res.Meta.UDPResults = udpCount
	}
	res.Meta.WallMean = meanDuration(walls)
	res.Meta.WallMin, res.Meta.WallMax = minMaxDuration(walls)
	return res, nil
}

// buildUDPFarmPlan picks the farm's ports from the live UDP hint set:
// responsive first (they are the interesting few), then the silent
// bulk, then the closed handful. A port whose required bind fails
// (OS-owned) is skipped entirely. / buildUDPFarmPlan 从实时 UDP 提示
// 集选农场端口：先响应式（少数关键），再静默主力，再少数 closed。
// 所需绑定失败的端口（OS 占用）整体跳过。
func buildUDPFarmPlan(opts UDPOptions) (UDPFarmPlan, error) {
	hints := fingerprint.NewVScan().UDPHintPorts()
	ports := make([]int, 0, len(hints))
	for p := range hints {
		ports = append(ports, p)
	}
	sort.Ints(ports)

	plan := UDPFarmPlan{Hosts: UDPFarmCIDR}
	// Preferred responsive ports: the classic responders first so the
	// farm's open ports look like a real LAN's handful of UDP services.
	// / 响应式优先端口：经典应答者排前，让农场的开放端口像真实 LAN
	// 的少数 UDP 服务。
	prefResp := []int{53, 123, 161, 137, 69, 514, 111, 500}

	take := func(want int, candidates []int, bind func(int) error, bucket *[]int) {
		for _, p := range candidates {
			if len(*bucket) >= want {
				return
			}
			if err := bind(p); err == nil {
				*bucket = append(*bucket, p)
			}
		}
	}

	respOrder := append([]int{}, prefResp...)
	for _, p := range ports {
		respOrder = append(respOrder, p)
	}
	take(opts.RespPorts, respOrder, func(p int) error {
		l, err := fakeserver.ListenUDP(farmServiceHost, p, farmUDPResponder)
		if err != nil {
			return err
		}
		// Hold the bind only long enough to claim the port: the
		// responsive listeners are re-bound fresh in every run Once
		// the plan is fixed. / 绑定只需占住端口：计划确定后每轮重新
		// 绑定响应式监听。
		return l.Close()
	}, &plan.ResponsivePorts)

	silentOrder := make([]int, 0, len(ports))
	for _, p := range ports {
		if containsInt(plan.ResponsivePorts, p) {
			continue
		}
		silentOrder = append(silentOrder, p)
	}
	take(opts.SilentPorts, silentOrder, func(p int) error {
		l, err := fakeserver.ListenUDP("0.0.0.0", p, func([]byte, *net.UDPAddr) []byte { return nil })
		if err != nil {
			return err
		}
		return l.Close()
	}, &plan.SilentPorts)

	closedOrder := make([]int, 0, len(ports))
	for _, p := range ports {
		if containsInt(plan.ResponsivePorts, p) || containsInt(plan.SilentPorts, p) {
			continue
		}
		closedOrder = append(closedOrder, p)
	}
	// Closed candidates must be VERIFIABLY free: bind-and-release, so
	// an OS-owned port never enters the closed bucket and pollutes the
	// fast path with real replies. / closed 候选必须可验证地空闲：绑定
	// 后释放，OS 占用的端口绝不进 closed 桶污染快速路径。
	take(opts.ClosedPorts, closedOrder, func(p int) error {
		l, err := fakeserver.ListenUDP("0.0.0.0", p, func([]byte, *net.UDPAddr) []byte { return nil })
		if err != nil {
			return err
		}
		return l.Close()
	}, &plan.ClosedPorts)

	if len(plan.SilentPorts) < opts.SilentPorts {
		return plan, fmt.Errorf("bench: udp farm: only %d/%d silent ports bindable",
			len(plan.SilentPorts), opts.SilentPorts)
	}
	return plan, nil
}

// farmServiceHost is the loopback IP responsive services bind on. The
// /28's first usable host address. / farmServiceHost 是响应式服务绑
// 定的回环 IP——/28 的首个可用主机地址。
const farmServiceHost = "127.66.0.1"

// farmUDPResponder answers any datagram with a fixed payload shaped
// like a plausible service reply. Identity fidelity is NOT the A4
// metric (wall time is); the response just needs to arrive fast.
// / farmUDPResponder 用固定 payload 回答任何 datagram，形似可信的服
// 务应答。身份保真不是 A4 指标（挂钟才是）；只需快速到达。
func farmUDPResponder([]byte, *net.UDPAddr) []byte {
	return []byte("FG-QiMen udp-farm responder\n")
}

// runUDPOnce boots a fresh farm, scans it with --udp on, measures the
// wall clock, and counts UDP-phase records. / runUDPOnce 启动全新农
// 场、--udp 开启扫描、量挂钟并统计 UDP 阶段记录数。
func runUDPOnce(plan UDPFarmPlan, targets []types.Target, allPorts []int, opts UDPOptions, run int, workDir string) (time.Duration, int, error) {
	// Fresh listeners per run: responsive on every expanded host (so
	// the farm is homogeneous), silent wildcard re-binds, closed
	// ports stay unbound. / 每轮全新监听：响应式绑到每台展开主机（农
	// 场同质化），静默 wildcard 重绑，closed 保持不绑。
	listeners := make([]*fakeserver.UDPListener, 0,
		len(targets)*len(plan.ResponsivePorts)+len(plan.SilentPorts))
	defer func() {
		for _, l := range listeners {
			_ = l.Close()
		}
	}()
	for _, t := range targets {
		for _, p := range plan.ResponsivePorts {
			l, err := fakeserver.ListenUDP(t.Addr, p, farmUDPResponder)
			if err != nil {
				return 0, 0, fmt.Errorf("bench: responsive %s:%d: %w", t.Addr, p, err)
			}
			listeners = append(listeners, l)
		}
	}
	for _, p := range plan.SilentPorts {
		l, err := fakeserver.ListenUDP("0.0.0.0", p, func([]byte, *net.UDPAddr) []byte { return nil })
		if err != nil {
			return 0, 0, fmt.Errorf("bench: silent 0.0.0.0:%d: %w", p, err)
		}
		listeners = append(listeners, l)
	}

	cfg := &types.Config{
		Mode:            types.ModeScan,
		Host:            plan.Hosts,
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
		UDP:             true,
		UDPStrict:       opts.Strict,
		UDPThreads:      opts.UDPThreads,
		UDPMaxThreads:   opts.UDPMax,
	}
	if err := cfg.Validate(); err != nil {
		return 0, 0, fmt.Errorf("bench: config: %w", err)
	}

	ctx := context.Background()
	sess, err := session.NewSession(ctx, cfg, "")
	if err != nil {
		return 0, 0, fmt.Errorf("bench: session: %w", err)
	}

	resultPath := filepath.Join(workDir, fmt.Sprintf("udp_run%d.ndjson", run))
	out, err := output.OpenOutput(output.OutputConfig{ResultJSONPath: resultPath})
	if err != nil {
		return 0, 0, fmt.Errorf("bench: output: %w", err)
	}
	sess.Out = out

	start := time.Now()
	exitCode, err := core.RunScan(ctx, sess)
	wall := time.Since(start)
	closeErr := out.Close()
	if err != nil {
		return 0, 0, fmt.Errorf("bench: scan: %w (exit %d)", err, exitCode)
	}
	if closeErr != nil {
		return 0, 0, fmt.Errorf("bench: output close: %w", closeErr)
	}

	udpCount, err := countUDPRecords(resultPath, plan)
	if err != nil {
		return 0, 0, fmt.Errorf("bench: harvest: %w", err)
	}
	return wall, udpCount, nil
}

// countUDPRecords reads the run's NDJSON and counts records whose port
// belongs to the farm. Every (host, farm-port) emits exactly one UDP
// record, so the UDP phase contributes hosts×ports records; ports the
// host OS itself serves on TCP (e.g. TcpSs on 135 under Windows) add a
// handful of extra TCP-phase records with the same empty shape — the
// count is a drift indicator, and the sweep's stable 504 (=490+14) is
// the expected Windows signature.
// / countUDPRecords 读取该轮 NDJSON 并统计端口属于 farm 的记录。每
// 个 (主机, farm 端口) 恰好产出一条 UDP 记录，因此 UDP 阶段贡献
// hosts×ports 条；宿主 OS 自己在 TCP 上服务的端口（如 Windows 的
// TcpSs 占 135）会追加少量同形状的 TCP 阶段记录——该计数是漂移指
// 示器，扫描里稳定的 504（=490+14）就是 Windows 特征值。
func countUDPRecords(path string, plan UDPFarmPlan) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	farmPorts := map[int]struct{}{}
	for _, p := range plan.ResponsivePorts {
		farmPorts[p] = struct{}{}
	}
	for _, p := range plan.SilentPorts {
		farmPorts[p] = struct{}{}
	}
	for _, p := range plan.ClosedPorts {
		farmPorts[p] = struct{}{}
	}

	count := 0
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var r types.Result
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			return 0, fmt.Errorf("decode result line: %w", err)
		}
		if _, ok := farmPorts[r.Port]; ok {
			count++
		}
	}
	return count, sc.Err()
}

// FormatUDP prints the operator-facing summary. / FormatUDP 打印面向
// 操作者的汇总。
func FormatUDP(r *UDPResult) string {
	m := r.Meta
	var b strings.Builder
	fmt.Fprintf(&b, "udp-farm: hosts=%d ports=%d (resp/silent/closed per host, silent wildcard-bound)\n",
		m.Hosts, m.Ports)
	fmt.Fprintf(&b, "  udp pool: threads=%d max=%d strict=%v timeout=%s\n",
		m.UDPThreads, m.UDPMax, m.Strict, m.Timeout)
	fmt.Fprintf(&b, "  wall mean=%s min=%s max=%s (per run, n=%d; non-UDP phases %s)\n",
		m.WallMean, m.WallMin, m.WallMax, m.Runs, udpFarmFixedOverhead)
	fmt.Fprintf(&b, "  udp-phase records: %d\n", m.UDPResults)
	return b.String()
}

// SaveUDP writes the record NDJSON. / SaveUDP 写记录 NDJSON。
func SaveUDP(r *UDPResult, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(r)
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

func meanDuration(vals []time.Duration) time.Duration {
	if len(vals) == 0 {
		return 0
	}
	var sum time.Duration
	for _, v := range vals {
		sum += v
	}
	return sum / time.Duration(len(vals))
}

func minMaxDuration(vals []time.Duration) (time.Duration, time.Duration) {
	if len(vals) == 0 {
		return 0, 0
	}
	lo, hi := vals[0], vals[0]
	for _, v := range vals[1:] {
		if v < lo {
			lo = v
		}
		if v > hi {
			hi = v
		}
	}
	return lo, hi
}

func containsInt(haystack []int, needle int) bool {
	for _, v := range haystack {
		if v == needle {
			return true
		}
	}
	return false
}
