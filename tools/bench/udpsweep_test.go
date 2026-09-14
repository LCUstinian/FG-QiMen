// udpsweep_test.go — tests for the A4 UDP sweep mode. The axis-grid
// builder is pure; the sweep runner is exercised end-to-end against
// an injected RunUDP so the loop, record writing, and curve printing
// stay honest without binding the farm's loopback ports.
//
// / udpsweep_test.go —— A4 UDP sweep 模式的测试。轴网格构建器是纯函
// 数；sweep 执行器对着注入的 RunUDP 端到端跑一遍，让循环、记录落盘
// 与曲线打印保持诚实，且不绑定 farm 的回环端口。
package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/bench"
)

// TestUDPSweepAxes lists both UDP axes. / TestUDPSweepAxes 列出两条
// UDP 轴。
func TestUDPSweepAxes(t *testing.T) {
	axes := UDPSweepAxes()
	if len(axes) != 2 || axes[0] != "udpthreads" || axes[1] != "udptimeout" {
		t.Errorf("UDPSweepAxes() = %v, want [udpthreads udptimeout]", axes)
	}
}

// TestUDPSweepAxisGrids: the udpthreads grid keeps all five points
// (128/200/400/800/1600) for cross-release comparability; the
// udptimeout grid spans 500ms/1s/2s; unknown axes error.
// / TestUDPSweepAxisGrids：udpthreads 网格保留全部五点
// （128/200/400/800/1600）以跨版本可比；udptimeout 网格跨
// 500ms/1s/2s；未知轴报错。
func TestUDPSweepAxisGrids(t *testing.T) {
	b := udpSweepBase{runs: 1, timeout: 2 * time.Second}

	points, labels, err := udpSweepAxis("udpthreads", b)
	if err != nil {
		t.Fatalf("udpthreads axis: %v", err)
	}
	if len(points) != 5 || len(labels) != 5 {
		t.Fatalf("udpthreads grid = %d points, want 5", len(points))
	}
	wantSizes := []int{128, 200, 400, 800, 1600}
	for i, p := range points {
		if p.UDPThreads != wantSizes[i] || p.UDPMax != wantSizes[i] {
			t.Errorf("point %d: threads/max = %d/%d, want %d/%d",
				i, p.UDPThreads, p.UDPMax, wantSizes[i], wantSizes[i])
		}
		if p.Runs != 1 || p.Timeout != 2*time.Second {
			t.Errorf("point %d: runs/timeout = %d/%v, want 1/2s", i, p.Runs, p.Timeout)
		}
	}

	points, labels, err = udpSweepAxis("udptimeout", b)
	if err != nil {
		t.Fatalf("udptimeout axis: %v", err)
	}
	if len(points) != 3 || len(labels) != 3 {
		t.Fatalf("udptimeout grid = %d points, want 3", len(points))
	}
	wantTimeouts := []time.Duration{500 * time.Millisecond, time.Second, 2 * time.Second}
	for i, p := range points {
		if p.Timeout != wantTimeouts[i] {
			t.Errorf("point %d: timeout = %v, want %v", i, p.Timeout, wantTimeouts[i])
		}
	}

	if _, _, err := udpSweepAxis("bogus", b); err == nil {
		t.Error("unknown axis must error")
	}
}

// TestRunUDPSweepErrorPropagates: a failing grid point aborts the
// sweep with the labeled error. / TestRunUDPSweepErrorPropagates：网
// 格点失败时 sweep 以带标签的错误中止。
func TestRunUDPSweepErrorPropagates(t *testing.T) {
	orig := runUDPFunc
	runUDPFunc = func(bench.UDPOptions) (*bench.UDPResult, error) {
		return nil, errors.New("boom")
	}
	t.Cleanup(func() { runUDPFunc = orig })

	if err := runUDPSweep("udpthreads", udpSweepBase{runs: 1}, t.TempDir()); err == nil {
		t.Error("failing grid point must abort the sweep")
	} else if !strings.Contains(err.Error(), "udpthreads=128") {
		t.Errorf("error missing label: %v", err)
	}
	if err := runUDPSweep("bogus", udpSweepBase{runs: 1}, t.TempDir()); err == nil {
		t.Error("unknown axis must error")
	}
}

// RunUDP (the real one is exercised by internal/bench's farm tests —
// go test runs packages in parallel, so two real farms would fight
// over the same loopback ports) and asserts the NDJSON records land
// with one line per grid point.
// / TestRunUDPSweepEndToEnd 以注入的假 RunUDP 驱动网格（真身由
// internal/bench 的 farm 测试覆盖——go test 跨包并行，两个真实 farm
// 会抢同一批回环端口），断言 NDJSON 记录按网格点逐行落盘。
func TestRunUDPSweepEndToEnd(t *testing.T) {
	orig := runUDPFunc
	runUDPFunc = func(opts bench.UDPOptions) (*bench.UDPResult, error) {
		return &bench.UDPResult{Meta: bench.UDPResultMeta{
			Type: "udp-meta", Topology: "udp-farm", Runs: opts.Runs,
			UDPThreads: opts.UDPThreads, UDPMax: opts.UDPMax,
			Hosts: 14, Ports: 35, WallMean: time.Second,
		}}, nil
	}
	t.Cleanup(func() { runUDPFunc = orig })

	outDir := t.TempDir()
	b := udpSweepBase{runs: 1, timeout: 500 * time.Millisecond}
	if err := runUDPSweep("udptimeout", b, outDir); err != nil {
		t.Fatalf("runUDPSweep: %v", err)
	}
	matches, err := filepath.Glob(filepath.Join(outDir, "bench", "sweep_udptimeout_*.ndjson"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("sweep records = %v (err %v), want exactly 1 file", matches, err)
	}
	data, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatalf("read records: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 3 {
		t.Errorf("records file has %d lines, want 3 (one per grid point)", len(lines))
	}
	var meta bench.UDPResultMeta
	for i, ln := range lines {
		if err := json.Unmarshal([]byte(ln), &meta); err != nil {
			t.Fatalf("line %d: %v", i, err)
		}
		if !strings.Contains(meta.Label, "udptimeout=") {
			t.Errorf("line %d label = %q, want udptimeout=*", i, meta.Label)
		}
	}
}
