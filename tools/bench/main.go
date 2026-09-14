// main.go — tools/bench: the operator-facing entry of the A1 judge.
// Runs the loopback benchmark, prints the summary, optionally saves a
// new local baseline and/or diffs against the existing one.
//
// Usage (via justfile or directly):
//
//	go run ./tools/bench -runs 5
//	go run ./tools/bench -runs 5 -save        # refresh local baseline
//	go run ./tools/bench -latency 50ms        # injected-RTT round
//
// The judge reports regressions but never exits non-zero on latency —
// the v8 plan keeps CI free of timing gates.
//
// / main.go —— tools/bench：A1 裁判的面向操作者入口。跑回环基准、
// 打印汇总，可选地保存新本机基线并/或与现有基线 diff。
//
// 经 justfile 或直接调用：
//
//	go run ./tools/bench -runs 5
//	go run ./tools/bench -runs 5 -save        # 刷新本机基线
//	go run ./tools/bench -latency 50ms        # 注入 RTT 轮
//
// 裁判报告回归但对时延不退出非零——v8 方案保持 CI 无时延门禁。
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/bench"
	"github.com/LCUstinian/FG-QiMen/internal/workspace"
)

func main() {
	var (
		runs     = flag.Int("runs", 3, "scan rounds over the topology / 对拓扑的扫描轮数")
		topoName = flag.String("topo", "single", fmt.Sprintf("topology name: %v / 拓扑名", bench.TopologyNames()))
		latency  = flag.Duration("latency", 0, "injected response delay per fake service / 每个假服务注入的响应延迟")
		jitter   = flag.Duration("jitter", 0, "±jitter around -latency / -latency 的 ±扰动")
		timeout  = flag.Duration("timeout", 2*time.Second, "explicit per-probe timeout / 显式 probe 超时")
		threads  = flag.Int("threads", 64, "explicit thread pool cap / 显式线程池上限")
		save     = flag.Bool("save", false, "save the result as the local baseline / 把结果存为本机基线")
		baseline = flag.String("baseline", "", "baseline path to compare against (default: <workspace>/bench/baseline.ndjson) / 对比用基线路径")
		out      = flag.String("out", "", "write the NDJSON record to this path / 结果记录落盘路径")
	)
	flag.Parse()

	topo, ok := bench.TopologyByName(*topoName)
	if !ok {
		fmt.Fprintf(os.Stderr, "unknown topology %q (available: %v)\n", *topoName, bench.TopologyNames())
		os.Exit(2)
	}

	// Resolve the baseline path BEFORE Run: Run points the workspace
	// root at its temp dir, and the baseline must live in the
	// operator's real workspace (the local SSOT).
	// / 在 Run 之前解析基线路径：Run 会把 workspace 根指到临时目录，
	// 而基线必须放在操作者真实 workspace（本机 SSOT）里。
	basePath := *baseline
	if basePath == "" {
		basePath = filepath.Join(workspace.Root(), "bench", "baseline.ndjson")
	}

	res, err := bench.Run(topo, bench.Options{
		Runs:      *runs,
		ReadDelay: *latency,
		Jitter:    *jitter,
		Timeout:   *timeout,
		Threads:   *threads,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "bench: %v\n", err)
		os.Exit(1)
	}

	fmt.Print(bench.FormatMeta(&res.Meta))
	if len(res.Samples) > 0 {
		fmt.Println("per-port samples:")
		for _, s := range res.Samples {
			fmt.Printf("  run%d port=%-5d %-10s %8v  product=%q conf=%s\n",
				s.Run, s.Port, s.Service, s.Latency, s.Product, s.Confidence)
		}
	}

	if *out != "" {
		if err := bench.Save(res, *out); err != nil {
			fmt.Fprintf(os.Stderr, "bench: save record: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("record written: %s\n", *out)
	}

	base, err := bench.LoadBaseline(basePath)
	switch {
	case err == nil:
		diff := bench.Compare(&base.Meta, &res.Meta)
		fmt.Println("--- baseline diff ---")
		fmt.Print(bench.FormatDiff(diff, &base.Meta, &res.Meta))
	case os.IsNotExist(err):
		if !*save {
			fmt.Printf("no baseline at %s (first run — use -save to record one)\n", basePath)
		}
	default:
		fmt.Fprintf(os.Stderr, "bench: load baseline: %v\n", err)
		os.Exit(1)
	}

	if *save {
		if err := bench.Save(res, basePath); err != nil {
			fmt.Fprintf(os.Stderr, "bench: save baseline: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("baseline saved: %s\n", basePath)
	}
}
