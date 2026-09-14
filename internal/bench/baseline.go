// baseline.go — local baseline SSOT for the bench judge: records are
// written as NDJSON (one "meta" line + one "sample" line per port) and
// compared structurally. Per the v8 plan the judge never gates: the
// comparator only reports deltas and flags likely regressions.
//
// / baseline.go —— 基准裁判的本机基线 SSOT：记录以 NDJSON 落盘（一
// 行 "meta" + 每端口一行 "sample"），对比为结构化 diff。按 v8 方案
// 裁决，裁判不拦截：对比器只报告差值并标记疑似回归。
package bench

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Baseline regression thresholds — observation markers only, never a
// gate (CI does not block on latency per the v8 plan).
// / 基线回归阈值——仅观测标记，绝不拦截（按 v8 方案 CI 不对时延拦
// 截）。
const (
	// LatencyRegressRatio: current > baseline × this ⇒ flag.
	// / LatencyRegressRatio：current > baseline × 此值 ⇒ 标记。
	LatencyRegressRatio = 1.10

	// IdentRegressDelta: identified ratio drop (percentage points)
	// larger than this ⇒ flag. / IdentRegressDelta：识别率下降（百
	// 分点）超过此值 ⇒ 标记。
	IdentRegressDelta = 0.01
)

// Save writes the result as NDJSON (meta line first, then samples).
// / Save 把结果写成 NDJSON（meta 行在前，样本行在后）。
func Save(res *Result, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	w := bufio.NewWriter(f)
	enc := json.NewEncoder(w)
	if err := enc.Encode(res.Meta); err != nil {
		return err
	}
	for _, s := range res.Samples {
		if err := enc.Encode(s); err != nil {
			return err
		}
	}
	return w.Flush()
}

// LoadBaseline reads back a saved baseline (meta + samples).
// / LoadBaseline 读回已保存的基线（meta + 样本）。
func LoadBaseline(path string) (*Result, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	res := &Result{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	first := true
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if first {
			first = false
			if err := json.Unmarshal([]byte(line), &res.Meta); err != nil {
				return nil, fmt.Errorf("baseline meta: %w", err)
			}
			continue
		}
		var s LatencySample
		if err := json.Unmarshal([]byte(line), &s); err != nil {
			return nil, fmt.Errorf("baseline sample: %w", err)
		}
		res.Samples = append(res.Samples, s)
	}
	if first {
		return nil, fmt.Errorf("baseline %s: empty file", path)
	}
	return res, sc.Err()
}

// Diff is the structural comparison between baseline and current.
// / Diff 是基线与当前结果的结构化对比。
type Diff struct {
	// LatencyP50Ratio / LatencyP95Ratio: current / baseline (1.0 = par).
	// / LatencyP50Ratio / LatencyP95Ratio：current / baseline（1.0 = 持平）。
	LatencyP50Ratio float64
	LatencyP95Ratio float64

	// IdentifiedDelta / ProbeHitDelta: current − baseline (absolute).
	// / IdentifiedDelta / ProbeHitDelta：current − baseline（绝对差）。
	IdentifiedDelta float64
	ProbeHitDelta   float64

	// Regressions lists human-readable findings; empty = no flag.
	// / Regressions 列出人类可读的发现；空 = 无标记。
	Regressions []string
}

// Compare diffs current against baseline. Topology or injection
// mismatches are reported as regressions because the numbers are not
// comparable in that case. / Compare 将当前结果与基线对比。拓扑或注
// 入参数不一致时直接报告为回归——因为数字不可比。
func Compare(baseline, current *Meta) *Diff {
	d := &Diff{}
	d.LatencyP50Ratio = current.LatencyP50.Seconds() / baseline.LatencyP50.Seconds()
	d.LatencyP95Ratio = current.LatencyP95.Seconds() / baseline.LatencyP95.Seconds()
	d.IdentifiedDelta = current.IdentifiedRatio - baseline.IdentifiedRatio
	d.ProbeHitDelta = current.ProbeHitRate - baseline.ProbeHitRate

	if baseline.Topology != current.Topology {
		d.Regressions = append(d.Regressions,
			fmt.Sprintf("topology mismatch: baseline=%s current=%s (numbers not comparable)",
				baseline.Topology, current.Topology))
		return d
	}
	if baseline.ReadDelay != current.ReadDelay {
		d.Regressions = append(d.Regressions,
			fmt.Sprintf("read_delay mismatch: baseline=%v current=%v (numbers not comparable)",
				baseline.ReadDelay, current.ReadDelay))
	}

	if !sameSign(current.LatencyP50, baseline.LatencyP50) ||
		current.LatencyP50.Seconds() > baseline.LatencyP50.Seconds()*LatencyRegressRatio {
		d.Regressions = append(d.Regressions,
			fmt.Sprintf("latency P50: %.0fms → %.0fms (×%.2f)",
				baseline.LatencyP50.Seconds()*1000, current.LatencyP50.Seconds()*1000, d.LatencyP50Ratio))
	}
	if sameSign(current.LatencyP95, baseline.LatencyP95) &&
		current.LatencyP95.Seconds() > baseline.LatencyP95.Seconds()*LatencyRegressRatio {
		d.Regressions = append(d.Regressions,
			fmt.Sprintf("latency P95: %.0fms → %.0fms (×%.2f)",
				baseline.LatencyP95.Seconds()*1000, current.LatencyP95.Seconds()*1000, d.LatencyP95Ratio))
	}
	if current.IdentifiedRatio < baseline.IdentifiedRatio-IdentRegressDelta {
		d.Regressions = append(d.Regressions,
			fmt.Sprintf("identified ratio: %.1f%% → %.1f%%",
				baseline.IdentifiedRatio*100, current.IdentifiedRatio*100))
	}
	if current.ProbeHitRate < baseline.ProbeHitRate-IdentRegressDelta {
		d.Regressions = append(d.Regressions,
			fmt.Sprintf("probe hit rate: %.1f%% → %.1f%%",
				baseline.ProbeHitRate*100, current.ProbeHitRate*100))
	}
	return d
}

// sameSign guards the ratio math against zero baselines (a zero
// baseline means the metric was never produced, not "infinitely fast").
// / sameSign 防御零基线的比率运算（零基线意味着指标从未产出，而非
// "无限快"）。
func sameSign(a, b time.Duration) bool {
	return a > 0 && b > 0
}

// FormatDiff renders the diff as an operator-readable block.
// / FormatDiff 把 diff 渲染为操作者可读文本块。
func FormatDiff(d *Diff, b, c *Meta) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "baseline %s (%s) vs current %s\n", b.Date, b.Topology, c.Date)
	fmt.Fprintf(&sb, "latency P50  %8.0fms → %8.0fms  (×%.2f)\n",
		b.LatencyP50.Seconds()*1000, c.LatencyP50.Seconds()*1000, d.LatencyP50Ratio)
	fmt.Fprintf(&sb, "latency P95  %8.0fms → %8.0fms  (×%.2f)\n",
		b.LatencyP95.Seconds()*1000, c.LatencyP95.Seconds()*1000, d.LatencyP95Ratio)
	fmt.Fprintf(&sb, "identified   %8.1f%% → %8.1f%%  (%+.1fpp)\n",
		b.IdentifiedRatio*100, c.IdentifiedRatio*100, d.IdentifiedDelta*100)
	fmt.Fprintf(&sb, "probe hits   %8.1f%% → %8.1f%%  (%+.1fpp)\n",
		b.ProbeHitRate*100, c.ProbeHitRate*100, d.ProbeHitDelta*100)
	fmt.Fprintf(&sb, "wall/run     %8.0fms → %8.0fms\n",
		b.WallMean.Seconds()*1000, c.WallMean.Seconds()*1000)
	if len(d.Regressions) > 0 {
		sb.WriteString("REGRESSIONS:\n")
		for _, r := range d.Regressions {
			fmt.Fprintf(&sb, "  ! %s\n", r)
		}
	} else {
		sb.WriteString("no regressions flagged\n")
	}
	return sb.String()
}

// FormatMeta renders a standalone run summary.
// / FormatMeta 渲染单次运行的汇总。
func FormatMeta(m *Meta) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "topology=%s runs=%d read_delay=%v\n", m.Topology, m.Runs, m.ReadDelay)
	fmt.Fprintf(&sb, "latency P50=%v P95=%v\n", m.LatencyP50, m.LatencyP95)
	fmt.Fprintf(&sb, "identified=%.1f%% (hard=%d soft=%d none=%d)\n",
		m.IdentifiedRatio*100, m.IdentHard, m.IdentSoft, m.IdentNone)
	fmt.Fprintf(&sb, "probe economics: ports=%d hits=%d (%.1f%%)\n",
		m.ProbePorts, m.ProbeHits, m.ProbeHitRate*100)
	fmt.Fprintf(&sb, "wall/run=%v peak_rss=%.0fMB go=%s\n", m.WallMean, m.PeakRSSMB, m.GoVersion)
	return sb.String()
}
