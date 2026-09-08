// render.go — view-layer helpers for the v0.5.2 info-density TUI
// (Task 4 of TUI v2 Spec A). The main Model + View stays in tui.go
// (which grows ~50 lines) but the per-render math — rate EWMA,
// top-N extraction, ETA projection, fixed-width bar chart — lives
// here so tui.go stays readable.
//
// render.go — v0.5.2 信息密度 TUI（Task 4 of TUI v2 Spec A）的视
// 图层辅助函数。Model + View 主体仍在 tui.go（它增长 ~50 行），
// 但每次渲染的数学——EWMA 速率、top-N 抽取、ETA 估算、固定宽度柱
// 图——在这里，让 tui.go 保持可读。
package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/types"
)

// rateTracker keeps an EWMA-smoothed per-second rate of hits and
// ports. EWMA α=0.5 gives faster decay than pure mean so a brief
// burst still shows up quickly.
//
// rateTracker 维持 hits 和 ports 的 EWMA 平滑每秒速率。α=0.5
// 衰减比纯均值快，瞬时突增仍能迅速体现。
type rateTracker struct {
	lastHits  int64
	lastPorts int64
	lastAt    time.Time
	emaHits   float64
	emaPorts  float64
}

// update samples the next (hits, ports) snapshot, computes the
// instantaneous rate over the gap since the last call, and folds
// it into the running EMA. Negative deltas (counter resets, rare
// race) are clamped to zero so the EWMA never goes negative.
//
// update 采样下一次 (hits, ports) 快照，计算相对上次调用的瞬时速
// 率，并折入运行中的 EMA。负 delta（计数器回滚、罕见的赛跑）被
// 钳为 0，让 EWMA 永不变成负值。
func (r *rateTracker) update(now time.Time, hits, ports int64) (rateHits, ratePorts float64) {
	if !r.lastAt.IsZero() {
		dt := now.Sub(r.lastAt).Seconds()
		if dt > 0 {
			dH := float64(hits - r.lastHits)
			dP := float64(ports - r.lastPorts)
			if dH < 0 {
				dH = 0
			}
			if dP < 0 {
				dP = 0
			}
			instHits := dH / dt
			instPorts := dP / dt
			const alpha = 0.5
			r.emaHits = alpha*instHits + (1-alpha)*r.emaHits
			r.emaPorts = alpha*instPorts + (1-alpha)*r.emaPorts
		}
	}
	r.lastHits = hits
	r.lastPorts = ports
	r.lastAt = now
	return r.emaHits, r.emaPorts
}

// topN returns the top n (name, countString) pairs from m sorted by
// count desc. Returns nil when m is empty so View() can render
// placeholders.
//
// topN 从 m 取计数前 n 名，返回排序好的 [name, countString] 对。
// m 为空时返回 nil 以便 View() 渲染占位符。
func topN(m map[string]int64, n int) [][2]string {
	type kv struct {
		k string
		v int64
	}
	var all []kv
	for k, v := range m {
		if v <= 0 {
			continue
		}
		all = append(all, kv{k, v})
	}
	if len(all) == 0 {
		return nil
	}
	sort.Slice(all, func(i, j int) bool { return all[i].v > all[j].v })
	out := make([][2]string, 0, n)
	for i := 0; i < len(all) && i < n; i++ {
		out = append(out, [2]string{all[i].k, fmt.Sprintf("%d", all[i].v)})
	}
	return out
}

// computeETA returns "ETA ~Ns" string per-stage. Returns "" if
// inputs are insufficient.
//
// computeETA 按阶段返回 ETA 字符串。输入不足时返回 ""。
//
// Per-stage formula:
//   - StageAlive:      probed / totalHosts   (alive sweep progress)
//   - StagePortScan:   ports / totalPorts    (port enumeration)
//   - StageIdentify:   ports / totalPorts    (same envelope; identify
//                                             also drives port counter)
//   - StageCred / StageDone: empty (no rate projection)
//
// totalHosts / totalPorts are passed separately because
// CountersView doesn't carry them (they live on State directly).
//
// totalHosts / totalPorts 单独传入，因为 CountersView 不携带它们
// （它们直接放在 State 上）。
//
// 按阶段公式：
//   - StageAlive：     probed / totalHosts
//   - StagePortScan：  ports / totalPorts
//   - StageIdentify：  ports / totalPorts
//   - StageCred / StageDone：空（无速率预测）
func computeETA(start, now time.Time, stage int32, view types.CountersView, totalHosts, totalPorts int64) string {
	_ = start // reserved for future total-runtime ETA; per-stage uses elapsed since start
	elapsed := now.Sub(start).Seconds()
	if elapsed <= 0 {
		return ""
	}
	switch int(stage) {
	case int(types.StageAlive):
		if view.AliveProbed > 0 && totalHosts > 0 &&
			view.AliveProbed < totalHosts {
			rem := elapsed * float64(totalHosts-view.AliveProbed) / float64(view.AliveProbed)
			return fmt.Sprintf("~%ds", int(rem))
		}
	case int(types.StagePortScan), int(types.StageIdentify):
		if view.Ports > 0 && totalPorts > 0 &&
			view.Ports < totalPorts {
			rem := elapsed * float64(totalPorts-view.Ports) / float64(view.Ports)
			return fmt.Sprintf("~%ds", int(rem))
		}
	}
	return ""
}

// bar renders a fixed-width horizontal bar made of filled + empty
// Unicode blocks. ratio is clamped to [0, 1] so callers don't have
// to validate. The block characters are intentionally distinct so
// even a 6-char bar reads as a bar (vs. a row of equal-width
// digits).
//
// bar 渲染由填色 + 空心 Unicode block 组成的固定宽度水平条。
// ratio 钳到 [0, 1]，调用方不用校验。block 字符刻意选用有强对比
// 的两种，让 6 字符的 bar 也读作 bar（而非一排等宽数字）。
func bar(ratio float64, w int) string {
	if ratio < 0 {
		ratio = 0
	}
	if ratio > 1 {
		ratio = 1
	}
	filled := int(float64(w) * ratio)
	return strings.Repeat("█", filled) + strings.Repeat("░", w-filled)
}
