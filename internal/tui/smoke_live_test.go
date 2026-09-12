//go:build smoke

// smoke_live_test.go — live-TUI smoke probe. NOT compiled without
// `-tags smoke`, so normal builds, `go test ./...`, vet and CI never
// touch it. It exists to execute the manual /24 smoke test from
// docs/verification/v0.7.0-tui-v2-bc/verification.md in a way that is
// executable and leaves evidence.
//
// smoke_live_test.go —— 实机 TUI 冒烟探针。没有 `-tags smoke` 时完全
// 不参与编译，正常构建、`go test ./...`、vet 和 CI 均不受影响。它的
// 作用是把 verification.md 里的人工 /24 冒烟测试变成可执行、可留证
// 的形式。
//
// How it works / 原理:
//  1. The PRODUCTION tui.Program is constructed with
//     tui.NewProgramWithOptions(cfg, tea.WithOutput(buffer)) —
//     bubbletea v1.3.10 renders to any writer (standard_renderer.flush
//     has no TTY guard), so no terminal is needed. CLICOLOR_FORCE=1 in
//     the parent shell forces the lipgloss colour profile so severity
//     colours survive the pipe.
//     用 tui.NewProgramWithOptions 构造生产级 tui.Program，渲染流捕
//     获到内存——bubbletea 对任意 writer 都渲染，无需终端。父 shell
//     里 CLICOLOR_FORCE=1 强制颜色 profile，让 severity 颜色在管道
//     下依然可见。
//  2. The real scan pipeline (core.RunScan over a real /24) drives the
//     program through its production ui.UI implementation — throttle,
//     state capture, done-once all exercised for real.
//     真实扫描管线（core.RunScan 扫真实 /24）通过生产 ui.UI 实现驱
//     动 program——节流、state 捕获、done-once 全部真实生效。
//  3. A scripted operator timeline injects real key events (e/E/?/esc/
//     p/r/L) and resizes across the three breakpoints via Program.Send,
//     snapshotting the capture stream at each step. Assertions run on
//     the per-step deltas of the raw ANSI stream.
//     脚本化的操作员时间线经 Program.Send 注入真实按键（e/E/?/esc/
//     p/r/L）并在三个断点间改变窗口尺寸，每步对捕获流做快照；断言
//     基于每步的增量。
//
// Usage / 用法:
//
//	$env:FGQI_SMOKE_CIDR  = '192.168.204.0/24'
//	$env:CLICOLOR_FORCE   = '1'
//	$env:TERM             = 'xterm-256color'
//	go test -tags smoke -run TestSmokeLiveTUIOnTarget -v -timeout 15m ./internal/tui/
package tui_test

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/LCUstinian/FG-QiMen/internal/core"
	"github.com/LCUstinian/FG-QiMen/internal/session"
	"github.com/LCUstinian/FG-QiMen/internal/tui"
	"github.com/LCUstinian/FG-QiMen/internal/types"
)

// lockBuffer is an io.Writer the renderer writes into; the test reads
// deltas concurrently. / lockBuffer 是渲染器写入的 io.Writer；测试并
// 发地读取增量。
type lockBuffer struct {
	mu sync.Mutex
	b  []byte
}

func (w *lockBuffer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.b = append(w.b, p...)
	return len(p), nil
}

func (w *lockBuffer) snapshot() []byte {
	w.mu.Lock()
	defer w.mu.Unlock()
	out := make([]byte, len(w.b))
	copy(out, w.b)
	return out
}

type smokeCp struct {
	label  string
	prev   int
	off    int
	failed bool // timeline aborted before reaching this step / 时间线提前中止
}

var (
	ipv4PortRe = regexp.MustCompile(`(?:\d{1,3}\.){3}\d{1,3}:\d{1,5}`)
	tsRowRe    = regexp.MustCompile(`\[\d{2}:\d{2}:\d{2}\]`)
	sgrRe      = regexp.MustCompile(`\x1b\[[0-9;]*m`)
)

func keyPress(r rune) tea.Msg  { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}} }
func escKey() tea.Msg          { return tea.KeyMsg{Type: tea.KeyEscape} }
func resizeMsg(w, h int) tea.Msg { return tea.WindowSizeMsg{Width: w, Height: h} }

func stripANSI(s string) string { return sgrRe.ReplaceAllString(s, "") }

func truncStr(s string, n int) string {
	s = strings.ReplaceAll(s, "\x1b", "ESC")
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func TestSmokeLiveTUIOnTarget(t *testing.T) {
	cidr := os.Getenv("FGQI_SMOKE_CIDR")
	if cidr == "" {
		t.Skip("FGQI_SMOKE_CIDR not set — live smoke probe disabled")
	}
	maxScan := 150 * time.Second
	if v := os.Getenv("FGQI_SMOKE_MAX"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			maxScan = d
		}
	}

	// Subnet prefix for "did real /24 targets show up" matching.
	// / 子网前缀，用于"真实 /24 目标是否出现"的匹配。
	_, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		t.Fatalf("bad FGQI_SMOKE_CIDR %q: %v", cidr, err)
	}
	ip := ipnet.IP.To4()
	if ip == nil {
		t.Fatalf("only IPv4 /24 supported, got %q", cidr)
	}
	prefix := fmt.Sprintf("%d.%d.%d.", ip[0], ip[1], ip[2])

	cfg := &types.Config{
		Host:            cidr,
		Mode:            types.ModeScan,
		NoState:         true,
		Threads:         100,
		Timeout:         2 * time.Second,
		PortTimeout:     800 * time.Millisecond,
		WebTimeout:      3 * time.Second,
		ShutdownTimeout: 5 * time.Second,
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("config invalid: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sess, err := session.NewSession(ctx, cfg, cfg.Project)
	if err != nil {
		t.Fatalf("session: %v", err)
	}

	// Production program with captured output. / 生产级 program，输出捕获。
	buf := &lockBuffer{}
	prog := tui.NewProgramWithOptions(cfg,
		tea.WithInput(nil),
		tea.WithOutput(buf),
	)
	runDone := make(chan struct{})
	go func() {
		defer close(runDone)
		if _, err := prog.Run(); err != nil {
			t.Errorf("tea.Run error: %v", err)
		}
	}()

	// Real production UI surface drives the program. / 生产 UI 表面驱动 program。
	sess.UI = prog

	// Checkpoints. / 快照。
	var mu sync.Mutex
	var marks []smokeCp
	lastOff := 0
	mark := func(label string) {
		buf.mu.Lock()
		off := len(buf.b)
		buf.mu.Unlock()
		mu.Lock()
		marks = append(marks, smokeCp{label: label, prev: lastOff, off: off})
		lastOff = off
		mu.Unlock()
	}
	alive := func() bool {
		select {
		case <-runDone:
			return false
		default:
			return true
		}
	}

	// Boot: tell the model its window, like a real terminal would.
	// / 启动：像真实终端一样告知窗口尺寸。
	prog.Send(resizeMsg(120, 30))
	time.Sleep(500 * time.Millisecond)
	mark("boot")

	// Operator timeline. / 操作员时间线。
	finishedEarly := make(chan struct{})
	go func() {
		defer close(finishedEarly)
		sleepMark := func(d time.Duration, label string) bool {
			time.Sleep(d)
			if !alive() {
				mu.Lock()
				marks = append(marks, smokeCp{label: label, prev: lastOff, off: lastOff, failed: true})
				mu.Unlock()
				return false
			}
			mark(label)
			return true
		}
		send := func(msg tea.Msg) {
			if alive() {
				prog.Send(msg)
			}
		}
		if !sleepMark(15*time.Second, "wide-running") {
			return
		}
		send(keyPress('e')) // expand errors / 展开错误面板
		if !sleepMark(1200*time.Millisecond, "errors-expanded") {
			return
		}
		send(keyPress('E')) // collapse errors / 折叠错误面板
		if !sleepMark(1200*time.Millisecond, "errors-collapsed") {
			return
		}
		send(keyPress('?')) // help overlay / 帮助浮层
		if !sleepMark(800*time.Millisecond, "help") {
			return
		}
		send(escKey()) // close help / 关闭帮助
		if !sleepMark(500*time.Millisecond, "help-closed") {
			return
		}
		send(keyPress('p')) // pause display / 暂停显示
		if !sleepMark(800*time.Millisecond, "paused") {
			return
		}
		send(keyPress('r')) // resume / 恢复
		if !sleepMark(800*time.Millisecond, "resumed") {
			return
		}
		send(resizeMsg(90, 28)) // medium breakpoint / 中断点
		if !sleepMark(800*time.Millisecond, "medium") {
			return
		}
		send(resizeMsg(76, 24)) // narrow breakpoint / 窄断点
		if !sleepMark(800*time.Millisecond, "narrow") {
			return
		}
		send(keyPress('L')) // live overlay on / 实时事件 overlay 开
		if !sleepMark(800*time.Millisecond, "narrow-overlay") {
			return
		}
		send(keyPress('L')) // overlay off / overlay 关
		if !sleepMark(500*time.Millisecond, "narrow-overlay-off") {
			return
		}
	}()

	// Run the real scan. / 跑真实扫描。
	type scanRes struct{ err error }
	scanDone := make(chan scanRes, 1)
	go func() {
		_, err := core.RunScan(ctx, sess)
		scanDone <- scanRes{err: err}
	}()

	var scanErr error
	// cancelled distinguishes "budget elapsed → we cancelled ctx" from
	// a natural finish. RunScan returns nil on ctx cancel (clean
	// early-error shutdown), so scanErr alone cannot tell the two
	// apart.
	// cancelled 区分"预算耗尽 → 我们取消 ctx"与自然完成。RunScan 在
	// ctx 取消时返回 nil（干净的早错关停），单看 scanErr 分不出两者。
	cancelled := false
	select {
	case r := <-scanDone:
		scanErr = r.err
		t.Logf("scan finished: %v", scanErr)
	case <-time.After(maxScan):
		cancelled = true
		t.Logf("scan budget %s elapsed — cancelling ctx (early-error path)", maxScan)
		cancel()
		select {
		case r := <-scanDone:
			scanErr = r.err
		case <-time.After(30 * time.Second):
			t.Error("RunScan did not return within 30s of ctx cancel")
		}
	}
	<-finishedEarly

	// Teardown: quit TUI if still alive, wait for full teardown.
	// / 拆除：若 TUI 仍存活则退出，等待完整拆除。
	select {
	case <-runDone:
	default:
		prog.Quit()
		select {
		case <-runDone:
		case <-time.After(20 * time.Second):
			t.Error("tea.Run did not exit within 20s of Quit")
		}
	}
	mark("tail")

	// Dump captures for human review. / 导出捕获供人工复核。
	stamp := time.Now().Format("20060102-150405")
	rawPath := filepath.Join(os.TempDir(), fmt.Sprintf("fgqi-smoke-%s.log", stamp))
	if err := os.WriteFile(rawPath, buf.snapshot(), 0o600); err != nil {
		t.Logf("capture dump failed: %v", err)
	}
	plainPath := filepath.Join(os.TempDir(), fmt.Sprintf("fgqi-smoke-%s.txt", stamp))
	if err := os.WriteFile(plainPath, []byte(stripANSI(string(buf.snapshot()))), 0o600); err != nil {
		t.Logf("plain dump failed: %v", err)
	}
	t.Logf("captures: %s | %s", rawPath, plainPath)

	// ─── Assertions on per-step deltas / 逐步增量断言 ───
	mu.Lock()
	defer mu.Unlock()
	get := func(label string) string {
		for _, c := range marks {
			if c.label == label {
				s := buf.snapshot()
				if c.prev > len(s) || c.off > len(s) {
					return ""
				}
				return string(s[c.prev:c.off])
			}
		}
		return ""
	}
	have := func(label string) bool {
		for _, c := range marks {
			if c.label == label && !c.failed {
				return true
			}
		}
		return false
	}

	// 1. Status chip flips IDLE → SCANNING on real pipeline life.
	// / 状态芯片 IDLE → SCANNING。
	if !have("wide-running") {
		t.Fatalf("wide-running checkpoint missing — TUI exited before the timeline (run err above?)")
	}
	wide := get("wide-running")
	if !strings.Contains(wide, "SCANNING") {
		t.Errorf("wide-running delta: SCANNING chip not rendered")
	}
	// 2. Colour profile active (CLICOLOR_FORCE honoured). / 颜色 profile 生效。
	codes := map[string]bool{}
	for _, m := range sgrRe.FindAllString(wide, -1) {
		codes[m] = true
	}
	t.Logf("distinct SGR codes in wide-running delta: %d", len(codes))
	if len(codes) < 3 {
		t.Errorf("colour profile inactive: only %d distinct SGR codes (set CLICOLOR_FORCE=1)", len(codes))
	}
	// 3. Sparkline sampling: ring fills within seconds even at zero
	// rate (baseline ▁ row). / sparkline 采样：即使速率为 0，ring 数
	// 秒内也会填充（基线 ▁ 行）。
	if !strings.ContainsAny(wide, "▁▂▃▄▅▆▇█") {
		t.Errorf("sparkline glyphs absent from wide-running delta")
	}
	// 4. LIVE EVENTS fed by real /24 data — or the placeholder on a
	// silent net. A /24 where every TCP SYN is dropped (common for
	// isolated VMware NAT segments) legitimately renders "(no events
	// yet)"; that still proves the region is wired. Only a delta with
	// neither real rows nor the placeholder is a bug.
	// / LIVE EVENTS 有真实 /24 数据——静默网段则渲染占位符。所有 TCP
	// SYN 被丢弃的 /24（隔离 VMware NAT 段很常见）合法地显示
	// "(no events yet)"；这仍证明区域接线正常。既无真实行也无占位
	// 符的增量才算缺陷。
	hits := 0
	for _, ev := range ipv4PortRe.FindAllString(wide, -1) {
		if strings.HasPrefix(ev, prefix) {
			hits++
		}
	}
	t.Logf("event rows with %s* host:port in wide-running delta: %d", prefix, hits)
	if hits == 0 {
		if strings.Contains(wide, "(no events yet)") {
			t.Logf("silent /24 (no open TCP ports seen) — placeholder rendered, live-events region alive")
		} else {
			t.Errorf("no live events for subnet %s* AND no placeholder — live-events region may be broken", prefix)
		}
	}

	// 5. 'e' expands errors: bar rows, or "(no errors)" on a silent net.
	// / 'e' 展开：bar 行或 "(no errors)"。
	if have("errors-expanded") {
		exp := get("errors-expanded")
		if !strings.ContainsAny(exp, "▓░") && !strings.Contains(exp, "(no errors)") {
			t.Errorf("errors-expanded delta has neither bars nor placeholder: %q", truncStr(exp, 300))
		} else if strings.ContainsAny(exp, "▓░") {
			t.Logf("expanded errors: severity bars rendered ✓")
		}
	} else {
		t.Logf("errors-expanded skipped (TUI exited before this step)")
	}

	// 6. 'E' collapses: single "ERRORS:" summary line redrawn.
	// / 'E' 折叠：重绘单行 "ERRORS:"。
	if have("errors-collapsed") {
		if col := get("errors-collapsed"); !strings.Contains(col, "ERRORS:") {
			t.Errorf("errors-collapsed delta missing ERRORS: summary: %q", truncStr(col, 300))
		}
	} else {
		t.Logf("errors-collapsed skipped")
	}

	// 7. '?' help overlay with key descriptions. / '?' 帮助浮层。
	if have("help") {
		if h := get("help"); !strings.Contains(h, "toggle the errors panel") {
			t.Errorf("help overlay content missing: %q", truncStr(h, 300))
		} else {
			t.Logf("help overlay rendered ✓")
		}
	} else {
		t.Logf("help skipped")
	}

	// 8. 'p' pause chip. / 'p' 暂停芯片。
	if have("paused") {
		if p := get("paused"); !strings.Contains(p, "[PAUSED]") {
			t.Errorf("paused delta missing [PAUSED] chip: %q", truncStr(p, 300))
		} else {
			t.Logf("pause chip rendered ✓")
		}
	} else {
		t.Logf("paused skipped")
	}

	// 9. Narrow + 'L': last-5 overlay reveals timestamped event rows —
	// or the placeholder on a silent net (timestamps need events).
	// / 窄断点 + 'L'：最近 5 条 overlay 显示带时间戳的事件行——静默
	// 网段则显示占位符（时间戳需要事件存在）。
	if have("narrow-overlay") {
		if o := get("narrow-overlay"); tsRowRe.MatchString(o) {
			t.Logf("narrow overlay event rows revealed ✓")
		} else if strings.Contains(stripANSI(o), "(no events yet)") {
			t.Logf("narrow overlay placeholder on silent net — timestamped rows N/A without events")
		} else {
			t.Errorf("narrow-overlay delta has neither event rows nor placeholder: %q", truncStr(o, 300))
		}
	} else {
		t.Logf("narrow-overlay skipped")
	}

	// 10. Termination: when the scan finished NATURALLY the DONE chip
	// must appear somewhere in the capture. On the cancel path the
	// doneMsg races prog.Quit() in the teardown and may legitimately
	// lose; record it as a note, not an error.
	// / 终态：扫描自然完成时 DONE 芯片必须出现在捕获的某处。取消路
	// 径中 doneMsg 与拆除时的 prog.Quit() 竞争，可能合法丢失；记
	// note 不记 error。
	full := string(buf.snapshot())
	if !cancelled {
		if strings.Contains(full, "DONE") {
			t.Logf("DONE chip in capture ✓")
		} else {
			t.Errorf("scan finished naturally but no DONE chip anywhere in capture")
		}
	} else {
		t.Logf("scan was cancelled (budget) — DONE chip check relaxed (doneMsg races Quit)")
	}

	// 11. Cred colour surface: N/A unless a credential hit occurred on
	// this network (covered by unit tests otherwise). / 凭据颜色：本
	// 网无凭据命中则为 N/A（单元测试已覆盖）。
	if strings.Contains(full, "★") || strings.Contains(full, "✦") {
		t.Logf("credential-hit rendering observed in capture")
	} else {
		t.Logf("no credential hits on this network — cred_success colour is unit-test-covered (N/A live)")
	}
}
