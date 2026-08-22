// benchmark_v04_test.go — v0.4 quantitative benchmark suite.
//
// This file collects the user-visible throughput benchmarks whose
// results the README Performance section quotes. Run with:
//
//   go test -bench=. -benchmem -benchtime=3s ./internal/core/
//
// Targets (commodity Linux 2026 hardware, Go 1.26):
//   - BenchmarkPortScanClosedPort (TCP connect to 127.0.0.1:1)
//   - BenchmarkExpandTargetsCIDR   (target expansion throughput)
//   - BenchmarkHashKey             (seen-set dedup key derivation)
//   - BenchmarkBuildPortIndex      (port → plugin lookup index)
//   - BenchmarkPortFingerprintMatch (banner → service classification)
//   - BenchmarkRunScanPipelineStub  (full pipeline stub throughput)
//
// baseline_v04.txt in this directory captures the recorded numbers
// for each commit. / 本目录下 baseline_v04.txt 记录每次 commit 的
// 数据。
//
// benchmark_v04_test.go — v0.4 量化基准测试套件。
package core

import (
	"context"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/core/scan"
	"github.com/LCUstinian/FG-QiMen/internal/plugins"
	"github.com/LCUstinian/FG-QiMen/internal/types"
)

// BenchmarkPortScanClosedPort measures the connect-only path of the
// port scanner. Uses 127.0.0.1 on a high closed port (64535) so the
// OS returns ECONNREFUSED quickly. Note: Windows Defender / SmartScreen
// can interfere with loopback TCP latency on Windows hosts —
// benchmark numbers are most accurate on Linux CI.
// / BenchmarkPortScanClosedPort 测量端口扫描器的仅连接路径。用
// 127.0.0.1 的高位关闭端口 (64535) 让 OS 快速返回 ECONNREFUSED。
// 注：Windows Defender / SmartScreen 可能干扰 loopback TCP 延迟
// ——基准数字在 Linux CI 上最准确。
func BenchmarkPortScanClosedPort(b *testing.B) {
	host := "127.0.0.1:64535" // closed port on loopback
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			c, err := net.DialTimeout("tcp", host, 100*time.Millisecond)
			if err == nil {
				_ = c.Close()
			}
		}
	})
	// Reported as: <b.N> iterations / elapsed → ports/sec.
	// / 报告：<b.N> 次迭代 / 时长 → ports/sec。
}

// BenchmarkExpandTargetsCIDR measures ExpandTargetsStream for a
// mid-sized CIDR (/20 = 4,096 hosts). / BenchmarkExpandTargetsCIDR
// 测量中型 CIDR（/20 = 4,096 hosts）的 ExpandTargetsStream 吞吐。
func BenchmarkExpandTargetsCIDR(b *testing.B) {
	spec := "10.0.0.0/20"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		it, err := types.ExpandTargetsStream(spec, "")
		if err != nil {
			b.Fatalf("ExpandTargetsStream: %v", err)
		}
		count := 0
		for t, ok := it.Next(); ok; t, ok = it.Next() {
			count++
			_ = t
		}
		if count != 4096 {
			b.Fatalf("count = %d, want 4096", count)
		}
	}
}

// BenchmarkHashKey measures dedup key derivation (SHA-1 16-byte
// truncated). Hot path on every Result. / BenchmarkHashKey 测量
// 去重键 derivation（SHA-1 截 16 字节）。每个 Result 都走热路径。
func BenchmarkHashKey(b *testing.B) {
	parts := []string{"10.0.0.5", "22", "ssh", "identify"}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = types.HashKey(parts...)
	}
}

// BenchmarkBuildPortIndex measures port → plugin lookup index
// construction. / BenchmarkBuildPortIndex 测量端口 → 插件查找索
// 引构建。
func BenchmarkBuildPortIndex2(b *testing.B) {
	pluginList := make([]plugins.Plugin, 0, 42)
	for _, name := range []string{
		"ssh", "http", "redis", "mongodb", "postgresql", "mssql", "smb",
		"smtp", "snmp", "ldap", "memcached", "elasticsearch", "rdp",
		"vnc", "telnet", "oracle", "winrm", "pop3", "imap", "socks5",
		"rsync", "docker", "rabbitmq", "modbus", "ipmi", "bacnet",
		"ntp", "tftp", "dns", "nfs",
	} {
		pluginList = append(pluginList, &mockPlugin{name: name, ports: []int{1, 22, 80, 443, 3306, 8080}})
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = buildPortIndex(pluginList)
	}
}

// BenchmarkCrossIterator measures the Cartesian product iterator
// through the scan package. / BenchmarkCrossIterator 测量 scan 包
// 的笛卡尔积迭代器。
func BenchmarkCrossIterator(b *testing.B) {
	hosts := make([]string, 1000)
	for i := range hosts {
		hosts[i] = "10.0.0." + strconv.Itoa(i%255)
	}
	ports := []int{22, 80, 443, 8080, 3306}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		it := scan.NewCrossIterator(hosts, ports)
		count := 0
		for {
			_, ok := it.Next()
			if !ok {
				break
			}
			count++
		}
		if count != 5000 {
			b.Fatalf("count = %d, want 5000", count)
		}
	}
}

// mockPluginBV04 is a tiny plugins.Plugin for the benchmark suite.
// (Renamed to avoid collision with scanner_test.go's mockPlugin.)
// / mockPluginBV04 是基准测试套件用的小型 plugins.Plugin。
// (重命名以避免与 scanner_test.go 的 mockPlugin 冲突。)
type mockPluginBV04 struct {
	name  string
	ports []int
}

func (m *mockPluginBV04) Name() string               { return m.name }
func (m *mockPluginBV04) Ports() []int              { return m.ports }
func (m *mockPluginBV04) Modes() plugins.Mode       { return plugins.ModeIdentify }
func (m *mockPluginBV04) Identify(ctx context.Context, host string, port int) *types.Result {
	return nil
}

// suppress unused warning for testing.TB / mockPlugin. / 抑制 unused。
var _ = time.Now