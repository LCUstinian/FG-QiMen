// perf.go — observed memory for the bench judge. Uses the Go runtime's
// Sys figure as an RSS proxy: for relative comparisons (baseline vs
// current on the same machine) the approximation is sufficient, and it
// keeps the judge free of per-OS syscall surface.
// / perf.go —— 基准裁判的观测内存。用 Go runtime 的 Sys 值作 RSS 代
// 理：对同一台机器上的相对对比（基线 vs 当前）近似足够，且让裁判不
// 带 per-OS syscall 面。
package bench

import "runtime"

func peakRSSMB() float64 {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	return float64(ms.Sys) / (1024 * 1024)
}
