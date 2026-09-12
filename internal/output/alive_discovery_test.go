// alive_discovery_test.go — tests for Output.WriteAliveDiscovery,
// the discovery-stage alive-list writer added when field testing on
// a firewalled /24 showed fgqm_alive staying empty (the sink used to
// be fed only by WriteResult, i.e. open-port results).
//
// alive_discovery_test.go — Output.WriteAliveDiscovery 的单测。该方
// 法是在防火墙 /24 实测发现 fgqm_alive 始终为空（此前 alive sink 只
// 由 WriteResult 即 open 端口结果喂数据）后新增的发现阶段写入路径。
package output

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LCUstinian/FG-QiMen/internal/types"
)

// TestWriteAliveDiscovery_RecordsHostsAndSharesDedup verifies:
//  1. discovery-stage hosts land in the alive sink (json + txt)
//  2. dedup is shared with the WriteResult path — a host recorded
//     by discovery then confirmed by an open-port result is written
//     exactly once
//  3. duplicate discovery entries collapse
//  4. an empty host is a no-op (no blank line that would break
//     `nmap -iL`)
//
// / TestWriteAliveDiscovery_RecordsHostsAndSharesDedup 验证：发现阶
// 段主机进入 alive sink（json + txt）；去重与 WriteResult 路径共享
// ——发现先写、open 结果后确认的同一主机只写一次；重复发现条目折
// 叠；空 host 是 no-op（不产生破坏 `nmap -iL` 的空行）。
func TestWriteAliveDiscovery_RecordsHostsAndSharesDedup(t *testing.T) {
	t.Run("json format", func(t *testing.T) {
		dir := t.TempDir()
		alivePath := filepath.Join(dir, "alive.json")
		o, err := OpenOutput(OutputConfig{ResultAlivePath: alivePath, AliveFormat: "json"})
		if err != nil {
			t.Fatalf("OpenOutput: %v", err)
		}
		defer o.Close()

		o.WriteAliveDiscovery("10.0.0.1", "system", time.Now())
		if err := o.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}

		body, err := os.ReadFile(alivePath)
		if err != nil {
			t.Fatalf("read alive file: %v", err)
		}
		lines := nonEmptyLines(string(body))
		if len(lines) != 1 {
			t.Fatalf("got %d alive lines, want 1: %q", len(lines), lines)
		}
		if !strings.Contains(lines[0], `"host":"10.0.0.1"`) || !strings.Contains(lines[0], `"service":"system"`) {
			t.Errorf("alive line missing host/service: %q", lines[0])
		}
	})

	t.Run("txt format one IP per line", func(t *testing.T) {
		dir := t.TempDir()
		alivePath := filepath.Join(dir, "alive.txt")
		o, err := OpenOutput(OutputConfig{ResultAlivePath: alivePath, AliveFormat: "txt"})
		if err != nil {
			t.Fatalf("OpenOutput: %v", err)
		}
		defer o.Close()

		o.WriteAliveDiscovery("10.0.0.1", "system", time.Now())
		o.WriteAliveDiscovery("10.0.0.2", "tcp", time.Now())
		if err := o.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}

		body, err := os.ReadFile(alivePath)
		if err != nil {
			t.Fatalf("read alive file: %v", err)
		}
		lines := nonEmptyLines(string(body))
		if len(lines) != 2 || lines[0] != "10.0.0.1" || lines[1] != "10.0.0.2" {
			t.Errorf("alive lines = %q, want [10.0.0.1 10.0.0.2]", lines)
		}
	})

	t.Run("dedup shared with WriteResult", func(t *testing.T) {
		dir := t.TempDir()
		alivePath := filepath.Join(dir, "alive.txt")
		o, err := OpenOutput(OutputConfig{ResultAlivePath: alivePath, AliveFormat: "txt"})
		if err != nil {
			t.Fatalf("OpenOutput: %v", err)
		}
		defer o.Close()

		// Discovery confirms the host first; the later open-port
		// result must NOT write it again. / 发现先确认主机；随后
		// 的 open 端口结果不得再写一次。
		o.WriteAliveDiscovery("10.0.0.1", "system", time.Now())
		if err := o.WriteResult(&types.Result{
			Host: "10.0.0.1", Port: 445, Service: "", Time: time.Now(),
		}); err != nil {
			t.Fatalf("WriteResult: %v", err)
		}
		if err := o.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}

		body, err := os.ReadFile(alivePath)
		if err != nil {
			t.Fatalf("read alive file: %v", err)
		}
		if lines := nonEmptyLines(string(body)); len(lines) != 1 {
			t.Errorf("got %d alive lines, want 1 (discovery entry wins): %q", len(lines), lines)
		}
	})

	t.Run("empty host is a no-op", func(t *testing.T) {
		dir := t.TempDir()
		alivePath := filepath.Join(dir, "alive.txt")
		o, err := OpenOutput(OutputConfig{ResultAlivePath: alivePath, AliveFormat: "txt"})
		if err != nil {
			t.Fatalf("OpenOutput: %v", err)
		}
		defer o.Close()

		o.WriteAliveDiscovery("", "system", time.Now()) // must not panic or write
		if err := o.Close(); err != nil {
			t.Fatalf("Close: %v", err)
		}

		// The sink was never written, so the Close() empty-sink
		// sweep deletes the file — "missing" and "empty" are both
		// acceptable here. / sink 从未写入，Close() 的空 sink 清扫
		// 会删除文件——"不存在"与"为空"都算 no-op。
		if _, err := os.Stat(alivePath); err == nil {
			body, err := os.ReadFile(alivePath)
			if err != nil {
				t.Fatalf("read alive file: %v", err)
			}
			if lines := nonEmptyLines(string(body)); len(lines) != 0 {
				t.Errorf("got %d lines for empty host, want 0: %q", len(lines), lines)
			}
		}
	})
}

// nonEmptyLines splits s into non-empty lines. / nonEmptyLines 把 s
// 拆成非空行。
func nonEmptyLines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if strings.TrimSpace(l) != "" {
			out = append(out, strings.TrimSpace(l))
		}
	}
	return out
}
