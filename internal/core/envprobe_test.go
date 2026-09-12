// envprobe_test.go — tests for the network environment profiler.
// envprobe_test.go — 网络环境画像的测试。
package core

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/core/scan"
	"github.com/LCUstinian/FG-QiMen/internal/types"
)

// delayProbe returns Open after a fixed delay, or StateFiltered with an
// error when filtered=true (simulating a firewalled path = loss).
//
// delayProbe 固定延迟后返回 Open；filtered=true 时返回
// StateFiltered+error（模拟防火墙路径 = 丢包）。
type delayProbe struct {
	delay   time.Duration
	filter  bool
	mu      sync.Mutex
	hosts   map[string]int
	attempt int
}

func (p *delayProbe) Name() string        { return "delay" }
func (p *delayProbe) Method() scan.Method { return scan.MethodTCPConnect }
func (p *delayProbe) Available() error    { return nil }

func (p *delayProbe) Probe(_ context.Context, host string, _ int, _ time.Duration) (scan.Result, error) {
	p.mu.Lock()
	if p.hosts == nil {
		p.hosts = map[string]int{}
	}
	p.hosts[host]++
	p.attempt++
	p.mu.Unlock()
	if p.delay > 0 {
		time.Sleep(p.delay)
	}
	if p.filter {
		return scan.Result{State: scan.StateFiltered}, fmt.Errorf("timeout")
	}
	return scan.Result{State: scan.StateClosed}, nil
}

func hundredHosts() []string {
	hosts := make([]string, 0, 100)
	for i := 1; i <= 100; i++ {
		hosts = append(hosts, fmt.Sprintf("10.90.%d.%d", i/254, i%254+1))
	}
	return hosts
}

func TestProbeNetworkClassifiesLAN(t *testing.T) {
	probe := &delayProbe{} // instant refused = valid RTT / 立即拒绝 = 有效 RTT
	p := ProbeNetwork(context.Background(), hundredHosts(), probe, time.Second)
	if p.Env != EnvLAN {
		t.Errorf("Env = %s, want LAN (median=%s)", p.Env, p.MedianRTT)
	}
	if p.LossRate != 0 {
		t.Errorf("LossRate = %v, want 0", p.LossRate)
	}
	if p.Samples != p.Attempts || p.Attempts == 0 {
		t.Errorf("Samples=%d Attempts=%d, want equal and > 0", p.Samples, p.Attempts)
	}
}

func TestProbeNetworkClassifiesSlowAllLoss(t *testing.T) {
	probe := &delayProbe{filter: true}
	p := ProbeNetwork(context.Background(), hundredHosts(), probe, time.Second)
	if p.Env != EnvSlow {
		t.Errorf("Env = %s, want Slow", p.Env)
	}
	if p.Samples != 0 || p.LossRate != 1 {
		t.Errorf("Samples=%d LossRate=%v, want 0/1", p.Samples, p.LossRate)
	}
}

func TestProbeNetworkEvenSampling(t *testing.T) {
	probe := &delayProbe{}
	ProbeNetwork(context.Background(), hundredHosts(), probe, time.Second)
	probe.mu.Lock()
	unique, attempts := len(probe.hosts), probe.attempt
	probe.mu.Unlock()
	if unique > 10 {
		t.Errorf("sampled %d unique hosts, want ≤ 10", unique)
	}
	if attempts != 4*unique {
		t.Errorf("attempts=%d, want 4×%d (envProbePorts)", attempts, unique)
	}
}

func TestApplyEnvTuningRespectsExplicitFlags(t *testing.T) {
	cfg := &types.Config{Threads: 200, Timeout: 3 * time.Second, ThreadsExplicit: true, TimeoutExplicit: true}
	p := NetworkProfile{Env: EnvLAN, MedianRTT: 1 * time.Millisecond, StddevRTT: 500 * time.Microsecond, Samples: 40, Attempts: 40, LossRate: 0}
	summary := ApplyEnvTuning(cfg, p)
	if cfg.Timeout != 3*time.Second || cfg.Threads != 200 {
		t.Errorf("explicit flags overridden: timeout=%s threads=%d", cfg.Timeout, cfg.Threads)
	}
	if summary == "" {
		t.Error("summary should still report the profile")
	}
}

func TestApplyEnvTuningLAN(t *testing.T) {
	cfg := &types.Config{Threads: 200, Timeout: 3 * time.Second}
	p := NetworkProfile{Env: EnvLAN, MedianRTT: 1 * time.Millisecond, StddevRTT: 500 * time.Microsecond, Samples: 40, Attempts: 40}
	ApplyEnvTuning(cfg, p)
	if cfg.Timeout != 1*time.Second {
		t.Errorf("LAN timeout = %s, want 1s (clamped floor)", cfg.Timeout)
	}
	if cfg.Threads != 300 {
		t.Errorf("LAN threads = %d, want 300 (200×1.5)", cfg.Threads)
	}
}

func TestApplyEnvTuningSlowClampsToMinThreads(t *testing.T) {
	cfg := &types.Config{Threads: 200, Timeout: 3 * time.Second}
	p := NetworkProfile{Env: EnvSlow, LossRate: 0.8, Samples: 8, Attempts: 40}
	ApplyEnvTuning(cfg, p)
	if cfg.Timeout != 3*time.Second {
		t.Errorf("Slow/no-sample timeout = %s, want unchanged 3s", cfg.Timeout)
	}
	if cfg.Threads != DefaultMinThreads {
		t.Errorf("Slow threads = %d, want floor %d", cfg.Threads, DefaultMinThreads)
	}
}

func TestApplyEnvTuningClampsTimeoutTo10s(t *testing.T) {
	cfg := &types.Config{Threads: 200, Timeout: 3 * time.Second}
	// median 4s → floor 3×4s+200ms=12.2s → clamped to 10s.
	// / median 4s → 下限 3×4s+200ms=12.2s → clamp 到 10s。
	p := NetworkProfile{Env: EnvInternet, MedianRTT: 4 * time.Second, StddevRTT: 0, Samples: 10, Attempts: 40, LossRate: 0}
	ApplyEnvTuning(cfg, p)
	if cfg.Timeout != 10*time.Second {
		t.Errorf("Internet timeout = %s, want clamped 10s", cfg.Timeout)
	}
}
