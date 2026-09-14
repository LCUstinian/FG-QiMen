// udpfarm_test.go — tests for the A4 UDP farm benchmark. The small
// topology test drives the REAL RunScan pipeline over loopback (the
// same path the sweep uses), so it needs real sockets and a few
// seconds; the rest are pure checks. CI coverage depends on this
// package (udpfarm.go is bench code that must not silently rot).
//
// / udpfarm_test.go —— A4 UDP farm 基准的测试。小拓扑测试在回环上驱
// 动真实 RunScan 管线（与 sweep 同路径），需要真实 socket 和几秒时
// 间；其余是纯检查。CI 覆盖率依赖本包（udpfarm.go 是 bench 代码，
// 不允许悄悄烂掉）。
package bench

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/types"
)

// TestRunUDPFarmSmall runs the smallest farm the plan builder can
// build and checks the headline invariants: every (host, farm-port)
// emits exactly one UDP record; the OS may leak a handful of
// same-shape TCP records for ports it serves itself (Windows TcpSs on
// TCP/135 — see docs/BENCH-A4.md F4), bounded by Hosts.
//
// / TestRunUDPFarmSmall 跑计划构建器能搭出的最小 farm，检查核心不变
// 式：每个（主机, farm 端口）恰好产出一条 UDP 记录；OS 会为它自己
// 服务的端口泄漏少量同 shape 的 TCP 记录（Windows TcpSs 占 TCP/135
// ——见 docs/BENCH-A4.md F4），上界为 Hosts。
func TestRunUDPFarmSmall(t *testing.T) {
	if testing.Short() {
		t.Skip("farm scan drives the real pipeline; skipped in -short")
	}
	res, err := RunUDP(UDPOptions{
		Runs:        1,
		Timeout:     500 * time.Millisecond,
		Threads:     64,
		SilentPorts: 4,
		RespPorts:   2,
		ClosedPorts: 1,
		// Exercise the Config.UDPThreads/UDPMaxThreads wiring branch
		// in core's UDP-pool setup. / 覆盖 core UDP 池装配里
		// Config.UDPThreads/UDPMaxThreads 的覆写分支。
		UDPThreads: 32,
		UDPMax:     64,
		// Fixed workspace root: covers the keep-workspace branch (the
		// temp-dir cleanup branch is covered by the zero-WorkDir sweep
		// test). / 固定 workspace 根：覆盖保留 workspace 分支（临时目
		// 录清理分支由零 WorkDir 的 sweep 测试覆盖）。
		WorkDir: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("RunUDP: %v", err)
	}
	m := res.Meta
	if m.Type != "udp-meta" {
		t.Errorf("meta type = %q, want udp-meta", m.Type)
	}
	if m.Topology != "udp-farm" {
		t.Errorf("topology = %q, want udp-farm", m.Topology)
	}
	if m.Hosts != 14 { // 127.66.0.0/28 minus network+broadcast
		t.Errorf("hosts = %d, want 14", m.Hosts)
	}
	if m.Ports != 2+4+1 {
		t.Errorf("ports = %d, want 7", m.Ports)
	}
	if m.WallMean <= 0 || m.WallMin <= 0 || m.WallMax < m.WallMin {
		t.Errorf("wall stats bogus: mean=%v min=%v max=%v", m.WallMean, m.WallMin, m.WallMax)
	}
	// The record invariant (see F4): Hosts×Ports exact, plus an
	// OS-leak allowance of at most one extra record per host.
	// / 记录不变式（见 F4）：Hosts×Ports 精确，OS 泄漏容限每主机至
	// 多一条。
	if m.UDPResults < m.Hosts*m.Ports || m.UDPResults > m.Hosts*(m.Ports+1) {
		t.Errorf("records = %d, want in [%d, %d]", m.UDPResults, m.Hosts*m.Ports, m.Hosts*(m.Ports+1))
	}
}

// TestBuildUDPFarmPlanBuckets checks the plan's bucket discipline:
// sizes honored, buckets disjoint. / TestBuildUDPFarmPlanBuckets 检查
// 计划的桶纪律：数量到位、桶间不相交。
func TestBuildUDPFarmPlanBuckets(t *testing.T) {
	plan, err := buildUDPFarmPlan(UDPOptions{RespPorts: 3, SilentPorts: 8, ClosedPorts: 2})
	if err != nil {
		t.Fatalf("buildUDPFarmPlan: %v", err)
	}
	if len(plan.ResponsivePorts) != 3 || len(plan.SilentPorts) != 8 || len(plan.ClosedPorts) != 2 {
		t.Fatalf("bucket sizes = %d/%d/%d, want 3/8/2",
			len(plan.ResponsivePorts), len(plan.SilentPorts), len(plan.ClosedPorts))
	}
	seen := map[int]string{}
	for _, p := range plan.ResponsivePorts {
		seen[p] = "resp"
	}
	for _, p := range plan.SilentPorts {
		if role := seen[p]; role != "" {
			t.Errorf("port %d in both %s and silent", p, role)
		}
		seen[p] = "silent"
	}
	for _, p := range plan.ClosedPorts {
		if role := seen[p]; role != "" {
			t.Errorf("port %d in both %s and closed", p, role)
		}
	}
}

// TestBuildUDPFarmPlanUnbindable: demanding more silent ports than
// the hint set can satisfy must fail loudly, not shrink the farm.
// / TestBuildUDPFarmPlanUnbindable：要的静默端口超过 hint 集能给的
// 数量必须响亮失败，而不是悄悄缩水。
func TestBuildUDPFarmPlanUnbindable(t *testing.T) {
	_, err := buildUDPFarmPlan(UDPOptions{RespPorts: 1, SilentPorts: 100000, ClosedPorts: 1})
	if err == nil {
		t.Fatal("expected error for an unsatisfiable silent-port request")
	}
}

// TestFormatUDPAndSaveUDP: the operator summary carries the headline
// fields; SaveUDP round-trips through NDJSON. / TestFormatUDPAndSaveU
// DP：操作者汇总携带核心字段；SaveUDP 经 NDJSON 往返一致。
func TestFormatUDPAndSaveUDP(t *testing.T) {
	r := &UDPResult{Meta: UDPResultMeta{
		Type: "udp-meta", Topology: "udp-farm", Runs: 3,
		UDPThreads: 800, UDPMax: 800, Hosts: 14, Ports: 35,
		WallMean: 4600 * time.Millisecond, WallMin: 4500 * time.Millisecond,
		WallMax: 4700 * time.Millisecond, UDPResults: 504,
	}}
	out := FormatUDP(r)
	for _, want := range []string{"udp-farm", "threads=800", "504"} {
		if !strings.Contains(out, want) {
			t.Errorf("FormatUDP missing %q in:\n%s", want, out)
		}
	}

	path := filepath.Join(t.TempDir(), "rec.ndjson")
	if err := SaveUDP(r, path); err != nil {
		t.Fatalf("SaveUDP: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	var got UDPResult
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Meta.WallMean != r.Meta.WallMean || got.Meta.UDPResults != r.Meta.UDPResults {
		t.Errorf("roundtrip mismatch: %+v vs %+v", got.Meta, r.Meta)
	}

	// A file blocking the record's parent dir makes MkdirAll fail —
	// SaveUDP must surface it. / 用一个文件挡住记录的父目录让
	// MkdirAll 失败——SaveUDP 必须浮出。
	blocker := filepath.Join(t.TempDir(), "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatalf("write blocker: %v", err)
	}
	if err := SaveUDP(r, filepath.Join(blocker, "sub", "rec.ndjson")); err == nil {
		t.Error("SaveUDP under a file path must error")
	}
}

// TestFarmAggregates: tiny aggregate helpers keep their contracts.
// / TestFarmAggregates：小聚合助手维持各自契约。
func TestFarmAggregates(t *testing.T) {
	if got := meanDuration(nil); got != 0 {
		t.Errorf("meanDuration(nil) = %v, want 0", got)
	}
	vals := []time.Duration{3 * time.Second, time.Second, 2 * time.Second}
	if got := meanDuration(vals); got != 2*time.Second {
		t.Errorf("meanDuration = %v, want 2s", got)
	}
	lo, hi := minMaxDuration(vals)
	if lo != time.Second || hi != 3*time.Second {
		t.Errorf("minMaxDuration = (%v, %v), want (1s, 3s)", lo, hi)
	}
	if lo, hi := minMaxDuration([]time.Duration{time.Second}); lo != time.Second || hi != time.Second {
		t.Errorf("minMaxDuration(single) = (%v, %v), want (1s, 1s)", lo, hi)
	}
	if lo, hi := minMaxDuration(nil); lo != 0 || hi != 0 {
		t.Errorf("minMaxDuration(nil) = (%v, %v), want (0, 0)", lo, hi)
	}
	if !containsInt([]int{53, 123}, 123) || containsInt([]int{53, 123}, 80) {
		t.Error("containsInt contract broken")
	}
}

// TestCountUDPRecords: counts every farm-port record (closed bucket
// included — the F4 accounting fix), skips non-farm ports and blank
// lines, and fails loudly on a malformed line or a missing file.
// / TestCountUDPRecords：统计全部 farm 端口记录（closed 桶在内——F4
// 记账修复），跳过非 farm 端口与空行，坏行/缺文件响亮失败。
func TestCountUDPRecords(t *testing.T) {
	plan := UDPFarmPlan{
		Hosts:           "127.66.0.0/28",
		ResponsivePorts: []int{53},
		SilentPorts:     []int{9000},
		ClosedPorts:     []int{135},
	}
	line := func(port int) string {
		b, err := json.Marshal(types.Result{Host: "127.66.0.1", Port: port})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		return string(b)
	}
	path := filepath.Join(t.TempDir(), "run.ndjson")
	content := strings.Join([]string{
		line(53),   // responsive → counted / 响应式 → 计入
		line(9000), // silent → counted / 静默 → 计入
		line(135),  // closed → counted (F4 fix) / closed → 计入（F4 修复）
		line(445),  // not a farm port → skipped / 非 farm 端口 → 跳过
		"",         // blank → skipped / 空行 → 跳过
	}, "\n")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	n, err := countUDPRecords(path, plan)
	if err != nil {
		t.Fatalf("countUDPRecords: %v", err)
	}
	if n != 3 {
		t.Errorf("count = %d, want 3", n)
	}

	// Malformed line fails loudly. / 坏行响亮失败。
	bad := filepath.Join(t.TempDir(), "bad.ndjson")
	if err := os.WriteFile(bad, []byte("{not json\n"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	if _, err := countUDPRecords(bad, plan); err == nil {
		t.Error("malformed line must error")
	}

	// Missing file fails loudly. / 缺文件响亮失败。
	if _, err := countUDPRecords(filepath.Join(t.TempDir(), "absent.ndjson"), plan); err == nil {
		t.Error("missing file must error")
	}
}
