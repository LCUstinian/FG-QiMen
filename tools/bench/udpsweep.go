// udpsweep.go — A4 sensitivity-sweep mode over the UDP farm: runs
// bench.RunUDP across a grid of one UDP-pool axis and prints the
// curve. Results append one NDJSON UDPResultMeta per grid point to
// <workspace>/bench/sweep_<axis>_<time>.ndjson.
//
// / udpsweep.go —— A4 UDP farm 敏感度扫描模式：沿单条 UDP 池轴网格
// 跑 bench.RunUDP 并打印曲线。结果按网格点逐行追加 NDJSON
// UDPResultMeta 到 <workspace>/bench/sweep_<axis>_<time>.ndjson。
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/bench"
)

// udpSweepBase carries the shared settings for every point on a UDP
// axis. / udpSweepBase 携带一条 UDP 轴上所有网格点共享的设置。
type udpSweepBase struct {
	runs    int
	timeout time.Duration
	strict  bool
}

// UDPSweepAxes lists the UDP sweepable axes for usage text.
// / UDPSweepAxes 列出 UDP 可扫描轴，供使用说明。
func UDPSweepAxes() []string {
	return []string{"udpthreads", "udptimeout"}
}

// udpSweepAxis returns the grid for one UDP axis.
// / udpSweepAxis 返回一条 UDP 轴的网格。
func udpSweepAxis(axis string, b udpSweepBase) ([]bench.UDPOptions, []string, error) {
	var points []bench.UDPOptions
	var labels []string
	switch axis {
	case "udpthreads":
		// 128/200 were the pre-A4 defaults, 800 is the A4 knee and the
		// shipped default since; the grid keeps all five points so the
		// curve stays comparable across releases. / 128/200 是 A4 前
		// 出厂值，800 是 A4 拐点即现行出厂值；网格保留全部五点，曲线
		// 跨版本可比。
		vals := []int{128, 200, 400, 800, 1600}
		for _, n := range vals {
			points = append(points, bench.UDPOptions{
				Runs: b.runs, Timeout: b.timeout, Strict: b.strict,
				UDPThreads: n, UDPMax: n})
			labels = append(labels, fmt.Sprintf("udpthreads=%d", n))
		}
	case "udptimeout":
		vals := []time.Duration{500 * time.Millisecond, time.Second, 2 * time.Second}
		for _, d := range vals {
			points = append(points, bench.UDPOptions{
				Runs: b.runs, Timeout: d, Strict: b.strict})
			labels = append(labels, fmt.Sprintf("udptimeout=%v", d))
		}
	default:
		return nil, nil, fmt.Errorf("unknown udp sweep axis %q (available: %v)", axis, UDPSweepAxes())
	}
	return points, labels, nil
}

// runUDPSweep executes the grid and prints one curve line per point.
// / runUDPSweep 执行网格并按点打印曲线行。
func runUDPSweep(axis string, b udpSweepBase, outDir string) error {
	points, labels, err := udpSweepAxis(axis, b)
	if err != nil {
		return err
	}
	fmt.Printf("udp sweep axis=%s runs=%d timeout=%v strict=%v points=%d\n",
		axis, b.runs, b.timeout, b.strict, len(points))
	fmt.Println("label               wall/run      wall-min     wall-max   records")

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

	for i, p := range points {
		res, err := bench.RunUDP(p)
		if err != nil {
			return fmt.Errorf("%s: %w", labels[i], err)
		}
		res.Meta.Label = labels[i]
		if err := enc.Encode(&res.Meta); err != nil {
			return err
		}
		fmt.Printf("%-18s %10v %12v %12v %8d\n",
			labels[i],
			res.Meta.WallMean.Round(time.Millisecond),
			res.Meta.WallMin.Round(time.Millisecond),
			res.Meta.WallMax.Round(time.Millisecond),
			res.Meta.UDPResults)
	}
	fmt.Printf("records written: %s\n", recPath)
	return nil
}
