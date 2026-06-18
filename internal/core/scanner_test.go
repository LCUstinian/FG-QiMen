package core

import (
	"context"
	"testing"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/plugins"
	"github.com/LCUstinian/FG-QiMen/internal/session"
	"github.com/LCUstinian/FG-QiMen/internal/types"
)

// TestRunScan_EmptyTargets tests that RunScan handles empty target list gracefully.
// TestRunScan_EmptyTargets 测试 RunScan 正确处理空目标列表。
func TestRunScan_EmptyTargets(t *testing.T) {
	cfg := &types.Config{
		Host:    "",
		Mode:    types.ModeScan,
		Threads: 10,
		Timeout: 3 * time.Second,
	}

	sess, err := session.NewSession(context.Background(), cfg, "")
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}

	count, err := RunScan(context.Background(), sess)
	if err != nil {
		t.Errorf("RunScan returned error: %v", err)
	}
	if count != 0 {
		t.Errorf("RunScan returned count %d, want 0", count)
	}
}

// TestRunScan_ContextCancellation tests that RunScan exits cleanly when context is canceled.
// TestRunScan_ContextCancellation 测试 RunScan 在 context 取消时干净退出。
func TestRunScan_ContextCancellation(t *testing.T) {
	cfg := &types.Config{
		Host:    "192.168.1.1",
		Mode:    types.ModeScan,
		Threads: 10,
		Timeout: 3 * time.Second,
	}

	sess, err := session.NewSession(context.Background(), cfg, "")
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = RunScan(ctx, sess)
	}()

	select {
	case <-done:
		// Success: RunScan exited
	case <-time.After(5 * time.Second):
		t.Error("RunScan did not exit after context cancellation")
	}
}

// TestBuildPortIndex tests that buildPortIndex correctly maps ports to plugins.
// TestBuildPortIndex 测试 buildPortIndex 正确映射端口到插件。
func TestBuildPortIndex(t *testing.T) {
	pluginList := []plugins.Plugin{
		&mockPlugin{name: "ssh", ports: []int{22}},
		&mockPlugin{name: "http", ports: []int{80, 8080}},
		&mockPlugin{name: "mysql", ports: []int{3306}},
	}

	index := buildPortIndex(pluginList)

	// Test port 22 -> ssh
	if len(index[22]) != 1 || index[22][0].Name() != "ssh" {
		t.Errorf("port 22: got %v, want [ssh]", index[22])
	}

	// Test port 80 -> http
	if len(index[80]) != 1 || index[80][0].Name() != "http" {
		t.Errorf("port 80: got %v, want [http]", index[80])
	}

	// Test port 8080 -> http
	if len(index[8080]) != 1 || index[8080][0].Name() != "http" {
		t.Errorf("port 8080: got %v, want [http]", index[8080])
	}

	// Test port 3306 -> mysql
	if len(index[3306]) != 1 || index[3306][0].Name() != "mysql" {
		t.Errorf("port 3306: got %v, want [mysql]", index[3306])
	}

	// Test non-existent port
	if len(index[9999]) != 0 {
		t.Errorf("port 9999: got %v, want []", index[9999])
	}
}

// mockPlugin is a test implementation of plugins.Plugin.
// mockPlugin 是 plugins.Plugin 的测试实现。
type mockPlugin struct {
	name  string
	ports []int
}

func (m *mockPlugin) Name() string { return m.name }
func (m *mockPlugin) Ports() []int { return m.ports }
func (m *mockPlugin) Modes() plugins.Mode {
	return plugins.ModeIdentify | plugins.ModeCredential
}
func (m *mockPlugin) Identify(ctx context.Context, host string, port int) *types.Result {
	return nil
}
func (m *mockPlugin) Credential(ctx context.Context, host string, port int, creds []types.Cred) *types.Result {
	return nil
}
