// sweep.go — A3 sensitivity-sweep mode: runs bench.Run across a grid
// of one controller axis and prints the curve. Each axis varies a
// single parameter and holds the rest at the base settings; results
// append one NDJSON Meta per grid point to
// <workspace>/bench/sweep_<axis>_<time>.ndjson.
//
// Load-sensitive axes (the AIMD policy knobs) need the LoadDelay
// signal — with plain uniform-latency services the controller never
// sees RTT inflation, so slowstart/aistep/md*/ratchet default
// -loaddelay to 2ms when unset.
//
// / sweep.go —— A3 敏感度扫描模式：沿单轴网格跑 bench.Run 并打印曲
// 线。每轴只动一个参数，其余保持基线设置；结果按网格点逐行追加
// NDJSON Meta 到 <workspace>/bench/sweep_<axis>_<time>.ndjson。
//
// 负载敏感轴（AIMD 策略旋钮）需要 LoadDelay 信号——普通均匀延迟服
// 务下控制器永远看不到 RTT 膨胀，因此 slowstart/aistep/md*/ratchet
// 在未设置时把 -loaddelay 默认为 2ms。
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/bench"
	"github.com/LCUstinian/FG-QiMen/internal/types"
)

// sweepPoint is one grid point: the parameter under test plus the
// frozen base settings. / sweepPoint 是一个网格点：被测参数 + 冻结
// 的基线设置。
type sweepPoint struct {
	label   string
	threads int
	timeout time.Duration
	adjust  time.Duration
	tuning  *types.AIMDTuning
}

// sweepBase carries the shared settings for every point on an axis.
// / sweepBase 携带一条轴上所有网格点共享的设置。
type sweepBase struct {
	runs      int
	topo      bench.Topology
	latency   time.Duration
	jitter    time.Duration
	loaddelay time.Duration
	threads   int
	timeout   time.Duration
}

// SweepAxes lists the sweepable axes for usage text.
// / SweepAxes 列出可扫描轴，供使用说明。
func SweepAxes() []string {
	return []string{"threads", "timeout", "adjust", "slowstart", "aistep",
		"mdstress", "mdcongest", "ratchet"}
}

// loadSensitiveAxes need LoadDelay>0 to produce a controller signal.
// / loadSensitiveAxes 需要 LoadDelay>0 才能产生控制器信号。
var loadSensitiveAxes = map[string]bool{
	"slowstart": true, "aistep": true,
	"mdstress": true, "mdcongest": true, "ratchet": true,
}

// NeedsLoadDelay reports whether the axis requires the load-sensitive
// service signal. / NeedsLoadDelay 报告该轴是否需要负载敏感服务信号。
func NeedsLoadDelay(axis string) bool { return loadSensitiveAxes[axis] }

// sweepAxis returns the grid for one axis. / sweepAxis 返回一条轴的
// 网格。
func sweepAxis(axis string, b sweepBase) ([]sweepPoint, error) {
	// tune builds a partial AIMDTuning — zero fields keep defaults.
	// / tune 构造部分 AIMDTuning——零值字段保持默认。
	tune := func(f func(*types.AIMDTuning)) *types.AIMDTuning {
		t := &types.AIMDTuning{}
		f(t)
		return t
	}
	switch axis {
	case "threads":
		vals := []int{16, 32, 64, 128, 256, 512}
		out := make([]sweepPoint, 0, len(vals))
		for _, n := range vals {
			out = append(out, sweepPoint{
				label: fmt.Sprintf("threads=%d", n), threads: n, timeout: b.timeout})
		}
		return out, nil
	case "timeout":
		vals := []time.Duration{500 * time.Millisecond, time.Second, 2 * time.Second, 3 * time.Second}
		out := make([]sweepPoint, 0, len(vals))
		for _, d := range vals {
			out = append(out, sweepPoint{
				label: fmt.Sprintf("timeout=%v", d), threads: b.threads, timeout: d})
		}
		return out, nil
	case "adjust":
		vals := []time.Duration{100 * time.Millisecond, 250 * time.Millisecond, 500 * time.Millisecond, time.Second}
		out := make([]sweepPoint, 0, len(vals))
		for _, d := range vals {
			out = append(out, sweepPoint{
				label: fmt.Sprintf("adjust=%v", d), threads: b.threads, timeout: b.timeout, adjust: d})
		}
		return out, nil
	case "slowstart":
		vals := []int{2, 4, 8, 16}
		out := make([]sweepPoint, 0, len(vals))
		for _, n := range vals {
			out = append(out, sweepPoint{
				label: fmt.Sprintf("slowstart=/%d", n), threads: b.threads, timeout: b.timeout,
				tuning: tune(func(t *types.AIMDTuning) { t.SlowStartDiv = n })})
		}
		return out, nil
	case "aistep":
		vals := []int{5, 10, 20, 40}
		out := make([]sweepPoint, 0, len(vals))
		for _, n := range vals {
			out = append(out, sweepPoint{
				label: fmt.Sprintf("aistep=/%d", n), threads: b.threads, timeout: b.timeout,
				tuning: tune(func(t *types.AIMDTuning) { t.AIStepDiv = n })})
		}
		return out, nil
	case "mdstress":
		vals := []float64{0.7, 0.85, 0.95}
		out := make([]sweepPoint, 0, len(vals))
		for _, v := range vals {
			out = append(out, sweepPoint{
				label: fmt.Sprintf("mdstress=%.2f", v), threads: b.threads, timeout: b.timeout,
				tuning: tune(func(t *types.AIMDTuning) { t.MDStress = v })})
		}
		return out, nil
	case "mdcongest":
		vals := []float64{0.3, 0.5, 0.7}
		out := make([]sweepPoint, 0, len(vals))
		for _, v := range vals {
			out = append(out, sweepPoint{
				label: fmt.Sprintf("mdcongest=%.2f", v), threads: b.threads, timeout: b.timeout,
				tuning: tune(func(t *types.AIMDTuning) { t.MDCongest = v })})
		}
		return out, nil
	case "ratchet":
		// 1e12 = effectively off — the ratio can never reach it.
		// / 1e12 = 实际关闭——比值永远到不了。
		vals := []struct {
			label string
			v     float64
		}{{"1.5", 1.5}, {"3.0", 3.0}, {"6.0", 6.0}, {"off", 1e12}}
		out := make([]sweepPoint, 0, len(vals))
		for _, e := range vals {
			out = append(out, sweepPoint{
				label: "ratchet=" + e.label, threads: b.threads, timeout: b.timeout,
				tuning: tune(func(t *types.AIMDTuning) { t.RatchetRatio = e.v })})
		}
		return out, nil
	default:
		return nil, fmt.Errorf("unknown sweep axis %q (available: %v)", axis, SweepAxes())
	}
}

// runSweep executes the grid and prints one curve line per point.
// / runSweep 执行网格并按点打印曲线行。
func runSweep(axis string, b sweepBase, outDir string) error {
	points, err := sweepAxis(axis, b)
	if err != nil {
		return err
	}
	fmt.Printf("sweep axis=%s topo=%s runs=%d loaddelay=%v base(threads=%d timeout=%v) points=%d\n",
		axis, b.topo.Name, b.runs, b.loaddelay, b.threads, b.timeout, len(points))
	fmt.Println("label               wall/run      P50      P95   ident  probe-hit")

	outDir = filepath.Join(outDir, "bench")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	recPath := filepath.Join(outDir, fmt.Sprintf("sweep_%s_%s.ndjson",
		axis, time.Now().Format("15-04-05")))
	rec, err := os.Create(recPath)
	if err != nil {
		return err
	}
	defer rec.Close()
	enc := json.NewEncoder(rec)

	for _, p := range points {
		res, err := bench.Run(b.topo, bench.Options{
			Runs:           b.runs,
			ReadDelay:      b.latency,
			Jitter:         b.jitter,
			LoadDelay:      b.loaddelay,
			Timeout:        p.timeout,
			Threads:        p.threads,
			AdjustInterval: p.adjust,
			Tuning:         p.tuning,
		})
		if err != nil {
			return fmt.Errorf("%s: %w", p.label, err)
		}
		res.Meta.Label = p.label
		if err := enc.Encode(&res.Meta); err != nil {
			return err
		}
		fmt.Printf("%-18s %8v %8v %8v  %5.1f%%  %5.1f%%\n",
			p.label,
			res.Meta.WallMean.Round(time.Millisecond),
			res.Meta.LatencyP50.Round(time.Millisecond),
			res.Meta.LatencyP95.Round(time.Millisecond),
			100*res.Meta.IdentifiedRatio,
			100*res.Meta.ProbeHitRate)
	}
	fmt.Printf("records written: %s\n", recPath)
	return nil
}
