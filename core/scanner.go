// Package core orchestrates the scan pipeline.
// Package core 编排扫描管线。
//
// Flow:
//
//	hostiter → alive (core/alive) → portscan (core/scan) →
//	  → [plugin workers: Identify] → output
//	  → [cred scheduler: Credential] → creds.txt
//
// All stages are context-aware. New in v0.1: each stage lives in its
// own focused subpackage (core/alive, core/scan, core/cred) with a
// clean interface and unit tests; scanner.go just glues them together.
//
// 所有阶段都基于 context。v0.1 新设计：每个阶段独立成包（core/alive、
// core/scan、core/cred），接口清晰、有单测；scanner.go 只做装配。
package core

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/LCUstinian/FG-QiMen/common"
	"github.com/LCUstinian/FG-QiMen/core/alive"
	"github.com/LCUstinian/FG-QiMen/core/scan"
	"github.com/LCUstinian/FG-QiMen/plugins"
)

// RunScan is the main entry point for a single scan invocation.
// RunScan 是单次扫描的主入口。
//
// It wires up the pipeline based on cfg.Mode:
//   - ModeScan / ModeLinked: full pipeline (alive → scan → identify → optional cred)
//   - ModeCrack: skip port scan, run credential tests against known ports
//
// 它根据 cfg.Mode 装配管线。
func RunScan(ctx context.Context, sess *common.Session) (int, error) {
	cfg := sess.Config
	if cfg == nil {
		return 0, fmt.Errorf("nil config")
	}

	sess.UI.Banner(cfg)

	// Expand targets. / 展开目标。
	targets, err := common.ExpandTargets(cfg.Host, cfg.HostsFile)
	if err != nil {
		return 0, fmt.Errorf("expand targets: %w", err)
	}
	if len(targets) == 0 {
		sess.Log.Info("no targets provided; nothing to scan")
		return 0, nil
	}

	// Stage 0: alive (core/alive). / 阶段 0：存活发现。
	aliveOpts := alive.DefaultOptions()
	if cfg.Timeout > 0 {
		aliveOpts.Timeout = cfg.Timeout
	}
	aliveDiscovery := alive.New(aliveOpts)
	aliveRes, _ := aliveDiscovery.Run(ctx, targetAddrs(targets))
	sess.State.Counters.Alive.Store(int64(len(aliveRes.Hits)))
	if len(aliveRes.Hits) > 0 && len(aliveRes.Hits) < len(targets) {
		sess.Log.Info("[*] alive: %d/%d hosts responded", len(aliveRes.Hits), len(targets))
	}
	if cfg.AliveOnly {
		sess.UI.Done(summaryString(sess))
		return 0, nil
	}

	// Channel sizes / 通道容量
	const itemsBuf = 1024

	items := make(chan common.ScanItem, itemsBuf)
	results := make(chan *common.Result, itemsBuf)

	var wg sync.WaitGroup

	// Stage 1: port scan (core/scan). / 阶段 1：端口扫描。
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(items)
		ports, _ := cfg.ParsePorts()
		scanRes := make(chan scan.Result, itemsBuf)
		sc := scan.NewScanner(scan.ScanOptions{
			Probe:      scan.NewTCPConnectProbe(),
			Timeout:    cfg.Timeout,
			Threads:    cfg.Threads,
			MinThreads: 1,
			MaxThreads: 500,
		})
		// Run scan in a goroutine; consume results in this one and
		// translate to plugin ScanItems.
		// scan 跑在子 goroutine；本 goroutine 消费并转为 plugin ScanItem。
		scanDone := make(chan struct{})
		go func() {
			_ = sc.Run(ctx, scan.NewCrossIterator(targetAddrs(targets), ports), scanRes)
			close(scanDone)
		}()
		for {
			select {
			case <-ctx.Done():
				return
			case <-scanDone:
				return
			case r, ok := <-scanRes:
				if !ok {
					return
				}
				if r.State != scan.StateOpen {
					continue
				}
				sess.State.Counters.Ports.Add(1)
				select {
				case items <- common.ScanItem{
					Host:   r.Host,
					Port:   r.Port,
					Banner: r.Banner,
				}:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	// Stage 2: plugin worker pool. / 阶段 2：plugin worker 池。
	workerCount := cfg.Threads
	if workerCount <= 0 {
		workerCount = 200
	}
	if workerCount > 16 {
		workerCount = 16
	}

	var workersWG sync.WaitGroup
	for i := 0; i < workerCount; i++ {
		workersWG.Add(1)
		go func() {
			defer workersWG.Done()
			runPluginWorker(ctx, sess, items, results)
		}()
	}
	go func() {
		workersWG.Wait()
		close(results)
	}()

	// Stage 3: result sink. / 阶段 3：结果汇。
	wg.Add(1)
	go func() {
		defer wg.Done()
		runResultSink(ctx, sess, results)
	}()

	// Periodic stats pusher. / 周期性 stats 推送。
	go pushStats(ctx, sess, 1*time.Second)

	wg.Wait()
	sess.UI.Done(summaryString(sess))
	return 0, nil
}

// targetAddrs extracts the address strings from a []common.Target.
// targetAddrs 从 []common.Target 提取地址字符串。
func targetAddrs(targets []common.Target) []string {
	out := make([]string, len(targets))
	for i, t := range targets {
		out[i] = t.Addr
	}
	return out
}

// summaryString builds a one-line summary printed at end of scan.
// summaryString 构建扫描结束时打印的单行摘要。
func summaryString(sess *common.Session) string {
	c := sess.State.Snapshot()
	return fmt.Sprintf(
		"[*] Done. alive=%d ports=%d results=%d creds=%d errors=%d",
		c.Alive, c.Ports, c.Results, c.Creds, c.Errors)
}

// PluginsAll is a re-export of plugins.All for callers outside core.
// PluginsAll 给 core 外的调用者重导出 plugins.All。
func PluginsAll() []plugins.Plugin { return plugins.All() }
