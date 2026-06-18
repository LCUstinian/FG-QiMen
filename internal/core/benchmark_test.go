package core

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/plugins"
	"github.com/LCUstinian/FG-QiMen/internal/session"
	"github.com/LCUstinian/FG-QiMen/internal/types"
)

// BenchmarkPluginWorker benchmarks the plugin worker throughput.
// BenchmarkPluginWorker 基准测试 plugin worker 吞吐量。
func BenchmarkPluginWorker(b *testing.B) {
	cfg := &types.Config{
		Host:    "192.168.1.1",
		Mode:    types.ModeScan,
		Threads: 16,
		Timeout: 3 * time.Second,
	}

	sess, err := session.NewSession(context.Background(), cfg, "")
	if err != nil {
		b.Fatalf("NewSession failed: %v", err)
	}

	in := make(chan types.ScanItem, 1024)
	out := make(chan *types.Result, 1024)

	// Pre-fill input channel
	for i := 0; i < 1000; i++ {
		in <- types.ScanItem{
			Host:   "192.168.1.1",
			Port:   22,
			Banner: "SSH-2.0-OpenSSH_8.9",
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		go runPluginWorker(context.Background(), sess, in, out)
	}
}

// BenchmarkResultSink benchmarks the result sink write speed.
// BenchmarkResultSink 基准测试 result sink 写入速度。
func BenchmarkResultSink(b *testing.B) {
	cfg := &types.Config{
		Host:    "192.168.1.1",
		Mode:    types.ModeScan,
		Threads: 16,
		Timeout: 3 * time.Second,
	}

	sess, err := session.NewSession(context.Background(), cfg, "")
	if err != nil {
		b.Fatalf("NewSession failed: %v", err)
	}

	in := make(chan *types.Result, 1024)

	// Pre-fill input channel
	for i := 0; i < 1000; i++ {
		in <- &types.Result{
			Host:    "192.168.1.1",
			Port:    22,
			Service: "ssh",
			Banner:  "SSH-2.0-OpenSSH_8.9",
			Time:    time.Now(),
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		go runResultSink(context.Background(), sess, in)
	}
}

// BenchmarkMatchesPort benchmarks the port matching performance.
// BenchmarkMatchesPort 基准测试端口匹配性能。
func BenchmarkMatchesPort(b *testing.B) {
	plugins := []plugins.Plugin{
		&mockPlugin{name: "ssh", ports: []int{22}},
		&mockPlugin{name: "http", ports: []int{80, 8080}},
		&mockPlugin{name: "mysql", ports: []int{3306}},
		&mockPlugin{name: "postgres", ports: []int{5432}},
		&mockPlugin{name: "redis", ports: []int{6379}},
	}

	index := buildPortIndex(plugins)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = index[22]
	}
}

// BenchmarkBuildPortIndex benchmarks the port index construction.
// BenchmarkBuildPortIndex 基准测试端口索引构建。
func BenchmarkBuildPortIndex(b *testing.B) {
	pluginList := make([]plugins.Plugin, 30)
	for i := 0; i < 30; i++ {
		pluginList[i] = &mockPlugin{
			name:  fmt.Sprintf("plugin%d", i),
			ports: []int{1000 + i, 2000 + i, 3000 + i},
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = buildPortIndex(pluginList)
	}
}
