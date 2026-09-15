// golden_test.go — TUI v3 golden frame matrix (spec §11.1). Each
// frame renders a deterministic Model through the PRODUCTION View()
// and is pinned byte-for-byte against testdata/golden/<name>.txt.
//
// golden_test.go — TUI v3 golden 帧矩阵（spec §11.1）。每帧用确定性
// 的 Model 走生产 View() 渲染，与 testdata/golden/<name>.txt 逐字节
// 对钉。
//
// Regenerate after an INTENTIONAL visual change:
//
//	go test ./internal/tui/ -run TestGolden -update
//
// Guards run on every frame (spec §11.1):
//   - frame line count == terminal height (reconciliation contract);
//   - no line wider than the terminal (lattice width law);
//   - no trailing newline (a phantom element makes bubbletea clip the
//     top row);
//   - frame purity: no CJK characters, no raw ANSI escapes and no
//     control bytes in the stored golden (颜色 profile 在 go test 下
//     不是 TTY → Ascii；为防 CLICOLOR_FORCE 之类环境污染，入库前剥
//     掉 ANSI，golden 恒为纯文本)。
//
// Matrix status: 25 frames (spec §11.1) — 3 breakpoints × run-idle /
// run-scanning / run-done / paused / browse / storm / merged / help
// plus the narrow overlay variant. TestGolden_MatrixCompleteness pins
// the count so the matrix cannot silently shrink.
package tui

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/LCUstinian/FG-QiMen/internal/types"
)

var goldenUpdate = flag.Bool("update", false, "regenerate golden frames in testdata/golden")

// goldenDir is where the baselines live, relative to the package dir.
// / goldenDir 是基线目录（相对包目录）。
const goldenDir = "testdata/golden"

// ansiRe matches ANSI escape sequences for stripping before storage.
// / ansiRe 匹配 ANSI 转义序列，入库前剥离。
var ansiRe = regexp.MustCompile(`\x1b\[[0-9;?]*[a-zA-Z]`)

// goldenFrame is one matrix cell. / goldenFrame 是矩阵的一格。
type goldenFrame struct {
	name   string
	width  int
	height int
	build  func() Model
}

// goldenBase is the fixed wall-clock all frame events share.
// / goldenBase 是所有帧事件共享的固定墙钟。
var goldenBase = time.Date(2026, 9, 14, 14, 23, 0, 0, time.UTC)

// goldenScanningModel builds the populated deterministic model shared
// by the run-scanning / paused / done / overlay frames.
// / goldenScanningModel 构造 run-scanning / paused / done / overlay
// 帧共用的确定性已填充 model。
func goldenScanningModel(w, h int) Model {
	m := Model{
		width: w, height: h,
		mode: "scan", project: "demo",
		runState: runScanning, uiMode: modeRun,
	}
	m.counters = types.CountersView{
		Stage: int64(types.StageIdentify), AliveProbed: 18, Ports: 142,
		Results: 23, Creds: 2, Errors: 7,
	}
	m.rateHits, m.ratePorts = 28.5, 142.0
	m.elapsed = "12s"
	m.eta = "~30s"
	for i := 1; i <= 12; i++ {
		m.recordRate(float64(i))
	}
	kinds := []string{"hit", "miss", "hit", "cred_success", "warn", "miss"}
	svcs := []string{"ssh", "http", "redis", "https", "smb", "mysql"}
	ports := []int{22, 80, 6379, 443, 445, 3306}
	for i := 0; i < 10; i++ {
		m.pushEvent(eventEntry{
			Host:    fmt.Sprintf("10.0.0.%d", i+1),
			Port:    ports[i%len(ports)],
			Service: svcs[i%len(svcs)],
			Kind:    kinds[i%len(kinds)],
			At:      goldenBase.Add(time.Duration(i) * time.Second),
		})
	}
	m.topPlugins = [][2]string{
		{"http-title", "9"},
		{"ssh-banner", "6"},
		{"redis", "3"},
	}
	return m
}

// goldenIdleModel builds the just-launched model: IDLE chip, no data.
// / goldenIdleModel 构造刚启动的 model：IDLE 芯片、无数据。
func goldenIdleModel(w, h int) Model {
	return Model{
		width: w, height: h,
		mode: "scan", project: "demo",
		runState: runIdle, uiMode: modeRun,
	}
}

// goldenDoneModel builds the finished model: DONE chip, StageDone.
// / goldenDoneModel 构造完成态 model：DONE 芯片、StageDone。
func goldenDoneModel(w, h int) Model {
	m := goldenScanningModel(w, h)
	m.runState = runDone
	m.lingerLeft = 10
	m.counters.Stage = int64(types.StageDone)
	m.eta = "" // no denominator at done / 完成后无分母
	return m
}

// goldenBrowseModel builds the T2 browse state: the viewport is
// anchored mid-ring (event ID 6) while newer events hide behind the
// ↓N lag chip. / goldenBrowseModel 构造 T2 browse 态：视口锚在 ring
// 中部（事件 ID 6），更新的事件藏在 ↓N 芯片之后。
func goldenBrowseModel(w, h int) Model {
	m := goldenScanningModel(w, h)
	m.browse = true
	m.browseID = 6
	m.browseLag = 4
	return m
}

// goldenStormModel builds the T2 storm state: the EVENTS cell swaps
// to the critical-only summary view. The sidecar is populated via
// pushCritical (the base model's pushEvent path never fills it — only
// appendBatch does): 2 rows survive, 9 evicted via critTotal=11.
// / goldenStormModel 构造 T2 风暴态：EVENTS 格换成 critical-only 摘
// 要视图。侧车用 pushCritical 显式填充（基础 model 的 pushEvent 路
// 径不填侧车——只有 appendBatch 会填）：2 行存活，critTotal=11 模
// 拟 9 条被淘汰。
func goldenStormModel(w, h int) Model {
	m := goldenScanningModel(w, h)
	m.storm = true
	m.stormRate = 620
	m.pushCritical(eventEntry{Host: "10.0.0.4", Port: 443, Service: "https",
		Kind: "cred_success", Text: "svc-backup:***len=9",
		At: goldenBase.Add(3 * time.Second)})
	m.pushCritical(eventEntry{Host: "10.0.0.5", Port: 445, Service: "smb",
		Kind: "warn", Text: "null session enabled",
		At: goldenBase.Add(4 * time.Second)})
	m.critTotal = 11 // 2 in the sidecar + 9 evicted / 侧车 2 条 + 淘汰 9 条
	return m
}

// goldenMergedModel builds the T2 merged state: two same-source runs
// appended after the 10 base events — run A (×5, folded, IDs 11-15)
// and run B (×3, Enter-expanded via expandedRun=16, IDs 16-18) — so
// both the folded ×N row and the replay path are pinned.
// / goldenMergedModel 构造 T2 merged 态：在 10 条基础事件后追加两个
// 同源 run——run A（×5，折叠，ID 11-15）和 run B（×3，经
// expandedRun=16 Enter 展开，ID 16-18）——同时钉住折叠 ×N 行与重放
// 路径。
func goldenMergedModel(w, h int) Model {
	m := goldenScanningModel(w, h)
	t0 := goldenBase.Add(10 * time.Second)
	for i := 0; i < 5; i++ { // run A: ×5 folded
		m.pushEvent(eventEntry{Host: "10.0.0.99", Port: 8443, Service: "https",
			Kind: "hit", At: t0.Add(time.Duration(i) * 200 * time.Millisecond)})
	}
	t1 := goldenBase.Add(11 * time.Second)
	for i := 0; i < 3; i++ { // run B: ×3, expanded
		m.pushEvent(eventEntry{Host: "10.0.0.98", Port: 9200, Service: "elastic",
			Kind: "hit", At: t1.Add(time.Duration(i) * 200 * time.Millisecond)})
	}
	m.expandedRun = 16
	return m
}

// goldenMatrix returns the 25-frame matrix across the three
// breakpoints. / goldenMatrix 返回三个断点下的 25 帧矩阵。
func goldenMatrix() []goldenFrame {
	sizes := map[string][2]int{
		"narrow": {60, 24},
		"medium": {100, 30},
		"wide":   {120, 40},
	}
	states := []struct {
		name  string
		build func(w, h int) Model
	}{
		{"run-idle", goldenIdleModel},
		{"run-scanning", goldenScanningModel},
		{"run-done", goldenDoneModel},
		{"paused", func(w, h int) Model {
			m := goldenScanningModel(w, h)
			m.uiMode = modePaused
			return m
		}},
		{"browse", goldenBrowseModel},
		{"storm", goldenStormModel},
		{"merged", goldenMergedModel},
		{"help", func(w, h int) Model {
			m := goldenScanningModel(w, h)
			m.uiMode = modeHelp
			return m
		}},
	}
	var frames []goldenFrame
	for bp, sz := range sizes {
		for _, st := range states {
			f := st
			frames = append(frames, goldenFrame{
				name:   bp + "-" + f.name,
				width:  sz[0],
				height: sz[1],
				build:  func() Model { return f.build(sz[0], sz[1]) },
			})
		}
	}
	// Narrow 'L' overlay variant: LIVE EVENTS forced on in narrow.
	// / narrow 'L' overlay 变体：narrow 下强制显示 LIVE EVENTS。
	narrow := sizes["narrow"]
	frames = append(frames, goldenFrame{
		name:   "narrow-run-scanning-overlay",
		width:  narrow[0],
		height: narrow[1],
		build: func() Model {
			m := goldenScanningModel(narrow[0], narrow[1])
			m.showLiveOverlay = true
			return m
		},
	})
	return frames
}

// TestGoldenFrames pins every implemented frame byte-for-byte and runs
// the §11.1 guards. / 逐字节钉住每个已实现帧并执行 §11.1 守卫。
func TestGoldenFrames(t *testing.T) {
	frames := goldenMatrix()
	for _, f := range frames {
		f := f
		t.Run(f.name, func(t *testing.T) {
			m := f.build()
			view := ansiRe.ReplaceAllString(m.View(), "")

			// Guard 1: exactly one line per terminal row.
			// / 守卫 1：帧行数恰好等于终端行数。
			lines := strings.Split(view, "\n")
			if len(lines) != f.height {
				t.Errorf("frame has %d lines, want exactly %d (height reconciliation contract)",
					len(lines), f.height)
			}
			// Guard 2: lattice width law. / 守卫 2：帧宽度律。
			for i, ln := range lines {
				if w := lipgloss.Width(ln); w > f.width {
					t.Errorf("line %d width %d > terminal %d: %q", i+1, w, f.width, ln)
				}
			}
			// Guard 3: no trailing newline. / 守卫 3：无尾随换行。
			if strings.HasSuffix(view, "\n") {
				t.Errorf("frame has a trailing newline (bubbletea would clip the top row)")
			}
			// Guard 4: frame purity — no CJK, no control bytes.
			// / 守卫 4：帧纯度——无 CJK、无控制字节。
			for _, r := range view {
				if r >= 0x4E00 && r <= 0x9FFF || r >= 0xFF00 && r <= 0xFFEF {
					t.Errorf("frame contains a CJK character %q (frame purity law)", r)
					break
				}
				if r < ' ' && r != '\n' {
					t.Errorf("frame contains a control byte %q (frame purity law)", r)
					break
				}
			}

			path := filepath.Join(goldenDir, f.name+".txt")
			if *goldenUpdate {
				if err := os.MkdirAll(goldenDir, 0o755); err != nil {
					t.Fatalf("mkdir %s: %v", goldenDir, err)
				}
				if err := os.WriteFile(path, []byte(view), 0o644); err != nil {
					t.Fatalf("write %s: %v", path, err)
				}
				return
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("golden missing (run `go test ./internal/tui/ -run TestGolden -update`): %v", err)
			}
			if view != string(want) {
				wantLines := strings.Split(string(want), "\n")
				for i := 0; i < len(lines) || i < len(wantLines); i++ {
					var gotLn, wantLn string
					if i < len(lines) {
						gotLn = lines[i]
					}
					if i < len(wantLines) {
						wantLn = wantLines[i]
					}
					if gotLn != wantLn {
						t.Fatalf("golden mismatch at line %d:\n  got:  %q\n  want: %q",
							i+1, gotLn, wantLn)
					}
				}
				t.Fatalf("golden mismatch (line counts differ: got %d, want %d)",
					len(lines), len(wantLines))
			}
		})
	}
}

// TestGolden_MatrixCompleteness pins the full 25-frame matrix from
// spec §11.1: 3 breakpoints × 8 states plus the narrow overlay —
// dropping a state or a breakpoint is a spec change, not a refactor.
// / 钉住 spec §11.1 的完整 25 帧矩阵：3 断点 × 8 状态 + narrow
// overlay——删状态或删断点属于规格变更，不是重构。
func TestGolden_MatrixCompleteness(t *testing.T) {
	implemented := map[string]bool{}
	for _, f := range goldenMatrix() {
		implemented[f.name] = true
	}
	const wantImplemented = 25
	if len(implemented) != wantImplemented {
		t.Errorf("golden matrix has %d implemented frames, want %d", len(implemented), wantImplemented)
	}
	for _, bp := range []string{"narrow", "medium", "wide"} {
		for _, st := range []string{"run-idle", "run-scanning", "run-done",
			"paused", "browse", "storm", "merged", "help"} {
			name := bp + "-" + st
			if !implemented[name] {
				t.Errorf("frame %q missing from goldenMatrix", name)
			}
		}
	}
	if !implemented["narrow-run-scanning-overlay"] {
		t.Errorf("frame %q missing from goldenMatrix", "narrow-run-scanning-overlay")
	}
}
