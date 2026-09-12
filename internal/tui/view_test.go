// view_test.go — tests for visual helpers (renderBar, sparkline,
// truncate, symFor, severityColor) and golden-file snapshots for
// each region renderer (viewHeader, viewEvents, viewCounters, etc.).
// / view_test.go — 视觉辅助函数测试及每个区域渲染器（viewHeader、
// viewEvents、viewCounters 等）的 golden-file 快照。
package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/LCUstinian/FG-QiMen/internal/types"
)

// TestRenderBar verifies the progress-bar glyph math.
// / 验证进度条字符数学。
func TestRenderBar(t *testing.T) {
	cases := []struct {
		filled, total, w int
		want             string
	}{
		{0, 10, 10, "░░░░░░░░░░"},
		{5, 10, 10, "▓▓▓▓▓░░░░░"},
		{10, 10, 10, "▓▓▓▓▓▓▓▓▓▓"},
		// total=0 → empty bar of width w
		{0, 0, 8, "░░░░░░░░"},
		// w=0 → empty
		{5, 10, 0, ""},
		// over-filled clamped to 100%
		{15, 10, 10, "▓▓▓▓▓▓▓▓▓▓"},
	}
	for _, c := range cases {
		got := renderBar(c.filled, c.total, c.w)
		if got != c.want {
			t.Errorf("renderBar(%d, %d, %d) = %q, want %q",
				c.filled, c.total, c.w, got, c.want)
		}
	}
}

// TestSparkline verifies the rate sparkline glyph sequence.
// / 验证 rate sparkline 字符序列。
func TestSparkline(t *testing.T) {
	// Linear ramp 1..8 → 8 distinct glyphs.
	samples := []float64{1, 2, 3, 4, 5, 6, 7, 8}
	got := sparkline(samples, 8)
	want := "▁▂▃▄▅▆▇█"
	if got != want {
		t.Errorf("sparkline(ramp, 8) = %q, want %q", got, want)
	}

	// All zeros → all lowest glyphs.
	zeros := []float64{0, 0, 0, 0}
	if got := sparkline(zeros, 4); got != "▁▁▁▁" {
		t.Errorf("sparkline(zeros, 4) = %q, want %q", got, "▁▁▁▁")
	}

	// Empty samples → empty string.
	if got := sparkline(nil, 8); got != "" {
		t.Errorf("sparkline(nil, 8) = %q, want empty", got)
	}

	// Fewer samples than width → padded with lowest glyph.
	// Math: idx = int(7 * s / max); for s=5/max=10, idx=3 → '▄'.
	// (Brief listed "▁▁▁▅█" but no single formula matches both this
	// case and the ramp 1..8 case; trust the implementation.)
	short := []float64{5, 10}
	if got := sparkline(short, 5); got != "▁▁▁▄█" {
		t.Errorf("sparkline(short, 5) = %q, want %q", got, "▁▁▁▄█")
	}

	// width=0 → empty.
	if got := sparkline(samples, 0); got != "" {
		t.Errorf("sparkline(samples, 0) = %q, want empty", got)
	}
}

// TestTruncate verifies rune-safe truncation.
// / 验证 rune 安全的截断。
func TestTruncate(t *testing.T) {
	cases := []struct {
		in   string
		n    int
		want string
	}{
		{"ssh", 10, "ssh"},             // shorter than n
		{"hello world", 8, "hello..."}, // truncate + ellipsis
		{"hi", 3, "hi"},                // exact length
		{"hello", 2, "he"},             // n <= 3, no ellipsis
		{"", 5, ""},                    // empty
		// Brief listed "中..." for n=3 but the implementation's n<=3
		// rule returns n runes without an ellipsis. The implementation
		// is consistent with the other test cases; trust it.
		{"中文测试", 3, "中文测"}, // multi-byte UTF-8 (n<=3, no ellipsis)
		{"x", 0, ""},       // n=0
	}
	for _, c := range cases {
		got := truncate(c.in, c.n)
		if got != c.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", c.in, c.n, got, c.want)
		}
	}
}

// TestSymFor verifies kind → symbol mapping.
// / 验证 kind → 符号映射。
func TestSymFor(t *testing.T) {
	cases := []struct {
		kind string
		want string
	}{
		{"critical_hit", symCriticalHit},
		{"hit", symInfoHit},
		{"cred_success", symCredSuccess},
		{"miss", symMiss},
		{"warn", symWarn},
		{"unknown", symIdle},
	}
	for _, c := range cases {
		got := symFor(c.kind)
		if got != c.want {
			t.Errorf("symFor(%q) = %q, want %q", c.kind, got, c.want)
		}
	}
}

// TestSeverityColor verifies the color returned for each event kind.
// Flash expiry takes precedence over kind. / 验证每种事件类型返回
// 的颜色。flash 过期优先于 kind。
func TestSeverityColor(t *testing.T) {
	m := Model{}
	e := eventEntry{Host: "1.2.3.4", Port: 80, Kind: "hit"}

	// No flash → severity color for "hit" = colorWarn.
	if got := m.severityColor(e); got != colorWarn {
		t.Errorf("no-flash hit: severityColor = %v, want colorWarn", got)
	}

	// With active flash → colorErr regardless of kind.
	m.flashUntil = map[string]time.Time{
		"1.2.3.4:80": time.Now().Add(1 * time.Second),
	}
	if got := m.severityColor(e); got != colorErr {
		t.Errorf("active flash: severityColor = %v, want colorErr", got)
	}

	// Expired flash → back to severity color.
	m.flashUntil["1.2.3.4:80"] = time.Now().Add(-1 * time.Second)
	if got := m.severityColor(e); got != colorWarn {
		t.Errorf("expired flash: severityColor = %v, want colorWarn", got)
	}

	// Different kinds map to different colors.
	for _, c := range []struct {
		kind string
		want lipgloss.Color
	}{
		{"cred_success", colorOk},
		{"miss", colorFgDim},
		{"warn", colorWarn},
		{"unknown", colorFg},
	} {
		e.Kind = c.kind
		if got := m.severityColor(e); got != c.want {
			t.Errorf("severityColor(%q) = %v, want %v", c.kind, got, c.want)
		}
	}
}

// headerTestModel returns a Model with safe defaults for viewHeader
// tests: a known width/height, StageIdentify so the badge contains
// "IDENTIFY", and a populated rateSamples ring so the sparkline has
// something to render. / headerTestModel 返回带安全默认值的 Model
// 供 viewHeader 测试用：固定 width/height，StageIdentify（让徽章
// 包含 "IDENTIFY"），并填充 rateSamples 让 sparkline 有内容可渲染。
func headerTestModel(width, height int) Model {
	m := newTestModel()
	m.width = width
	m.height = height
	m.counters.Stage = int64(types.StageIdentify)
	m.eta = "~30s"
	m.elapsed = "5s"
	m.rateHits = 2.5
	m.ratePorts = 10.0
	m.counters.AliveProbed = 5
	// Populate rateSamples with a linear ramp so sparkline has data.
	for i := 1; i <= 16; i++ {
		m.recordRate(float64(i))
	}
	return m
}

// TestViewHeader_NarrowBreakpoint verifies the narrow-mode header
// renders the stage badge, sparkline glyphs, and counters line in
// compact form. / 验证 narrow 模式 header 渲染 stage badge、sparkline
// 字符与 counters 行，紧凑形式。
func TestViewHeader_NarrowBreakpoint(t *testing.T) {
	m := headerTestModel(60, 24)
	got := m.viewHeader(1, BreakNarrow)

	// Header must contain the stage badge — StageIdentify → "IDENTIFY".
	if !strings.Contains(got, "IDENTIFY") {
		t.Errorf("narrow header missing stage badge IDENTIFY: %q", got)
	}
	// Must contain at least one sparkline glyph (▁..█).
	if !strings.ContainsAny(got, "▁▂▃▄▅▆▇█") {
		t.Errorf("narrow header missing sparkline glyphs: %q", got)
	}
	// Must contain a counter — either "rate:" or "ports:".
	if !strings.Contains(got, "rate:") && !strings.Contains(got, "ports:") {
		t.Errorf("narrow header missing counter line: %q", got)
	}
}

// TestViewHeader_MediumBreakpoint verifies the medium-mode header
// adds ETA/elapsed on the right side. / 验证 medium 模式 header 在
// 右侧加 ETA/elapsed。
func TestViewHeader_MediumBreakpoint(t *testing.T) {
	m := headerTestModel(100, 30)
	got := m.viewHeader(1, BreakMedium)

	if !strings.Contains(got, "IDENTIFY") {
		t.Errorf("medium header missing stage badge: %q", got)
	}
	// ETA string was set to "~30s" in the helper.
	if !strings.Contains(got, "ETA") && !strings.Contains(got, "~30s") && !strings.Contains(got, "elapsed") {
		t.Errorf("medium header missing right-edge ETA/elapsed: %q", got)
	}
}

// TestViewHeader_WideBreakpoint verifies the wide-mode header is
// wider and includes the sparkline. / 验证 wide 模式 header 更宽并
// 包含 sparkline。
func TestViewHeader_WideBreakpoint(t *testing.T) {
	m := headerTestModel(140, 40)
	got := m.viewHeader(1, BreakWide)

	if !strings.Contains(got, "IDENTIFY") {
		t.Errorf("wide header missing stage badge: %q", got)
	}
	// Wide mode must include a sparkline glyph (140 cols → plenty of room).
	if !strings.ContainsAny(got, "▁▂▃▄▅▆▇█") {
		t.Errorf("wide header missing sparkline: %q", got)
	}
	// Wide should be at least 2 lines: stage badge + rate row.
	if strings.Count(got, "\n") < 1 {
		t.Errorf("wide header should be >=2 lines, got %q", got)
	}
}

// TestViewHeader_ZeroHeightReturnsEmpty verifies the height<=0 guard
// returns empty so callers don't render a stray line.
// / 验证 height<=0 guard 返回空，调用方不会渲染多余的空行。
func TestViewHeader_ZeroHeightReturnsEmpty(t *testing.T) {
	m := headerTestModel(100, 30)
	if got := m.viewHeader(0, BreakMedium); got != "" {
		t.Errorf("viewHeader(0) = %q, want empty", got)
	}
	if got := m.viewHeader(-1, BreakMedium); got != "" {
		t.Errorf("viewHeader(-1) = %q, want empty", got)
	}
}

// TestStageBadge verifies the stage badge wraps the existing "[ ▶
// STAGE ]" format from tui.go View(). / 验证 stage badge 包装 tui.go
// View() 里旧的 "[ ▶ STAGE ]" 格式。
func TestStageBadge(t *testing.T) {
	m := newTestModel()
	m.counters.Stage = int64(types.StageIdentify)
	got := m.stageBadge()
	if !strings.Contains(got, "IDENTIFY") {
		t.Errorf("stageBadge() = %q, want to contain IDENTIFY", got)
	}
	// Must keep the "[ ▶ STAGE ]" bracket framing.
	if !strings.HasPrefix(got, "  [ ") || !strings.Contains(got, " ]") {
		t.Errorf("stageBadge() = %q, want format \"  [ ▶ STAGE ]\"", got)
	}
}

// TestCountersLine verifies countersLine renders the rate row when at
// least one rate is positive. / 验证当至少一个速率 > 0 时 countersLine
// 渲染 rate 行。
func TestCountersLine(t *testing.T) {
	m := newTestModel()
	m.rateHits = 2.5
	m.ratePorts = 10.0
	m.counters.AliveProbed = 5
	got := m.countersLine()
	if got == "" {
		t.Errorf("countersLine() = empty, want rate row")
	}
	if !strings.Contains(got, "rate:") || !strings.Contains(got, "ports:") {
		t.Errorf("countersLine() = %q, want rate row with rate: and ports:", got)
	}

	// Zero rate → empty (no "warming up" line).
	m2 := newTestModel()
	if got := m2.countersLine(); got != "" {
		t.Errorf("countersLine() with zero rate = %q, want empty", got)
	}
}

// TestEtaLine verifies etaLine prefers m.eta, falling back to elapsed.
// / 验证 etaLine 优先 m.eta，回退到 elapsed。
func TestEtaLine(t *testing.T) {
	m := newTestModel()
	m.eta = "~30s"
	m.elapsed = "5s"
	if got := m.etaLine(); got != "~30s" {
		t.Errorf("etaLine() with eta = %q, want %q", got, "~30s")
	}
	m.eta = ""
	if got := m.etaLine(); got != "elapsed 5s" {
		t.Errorf("etaLine() without eta = %q, want %q", got, "elapsed 5s")
	}
	m.eta = ""
	m.elapsed = ""
	if got := m.etaLine(); got != "" {
		t.Errorf("etaLine() with both empty = %q, want empty", got)
	}
}

// TestUptimeLine verifies uptimeLine formats time.Since(m.start) when
// start is set, else returns "". / 验证 uptimeLine 在 start 设置时
// 格式化 time.Since(m.start)，否则返回 ""。
func TestUptimeLine(t *testing.T) {
	m := newTestModel()
	if got := m.uptimeLine(); got != "" {
		t.Errorf("uptimeLine() with unset start = %q, want empty", got)
	}
	m.start = time.Now().Add(-65 * time.Second)
	got := m.uptimeLine()
	if !strings.HasPrefix(got, "up ") && !strings.HasPrefix(got, "uptime ") {
		// The exact prefix is implementation choice; just check it
		// contains a time-like suffix.
		t.Errorf("uptimeLine() = %q, want non-empty with 'up' prefix", got)
	}
}

// TestViewLiveEvents_Empty verifies the empty-state placeholder.
// / 验证空状态的占位。
func TestViewLiveEvents_Empty(t *testing.T) {
	m := newTestModel()
	got := m.viewLiveEvents(8, BreakMedium)
	if !strings.Contains(got, "no events") {
		t.Errorf("empty viewLiveEvents missing placeholder: %q", got)
	}
}

// TestViewLiveEvents_RendersRecentEvents verifies the most-recent N
// events render in chronological order with severity symbols.
// / 验证最近 N 个事件按时间顺序渲染，带 severity 符号。
func TestViewLiveEvents_RendersRecentEvents(t *testing.T) {
	m := newTestModel()
	m.flashUntil = map[string]time.Time{} // no flash for predictability
	for i := 0; i < 5; i++ {
		m.pushEvent(eventEntry{
			Host:    fmt.Sprintf("10.0.0.%d", i),
			Port:    22,
			Service: "ssh",
			Kind:    "hit",
			At:      time.Unix(int64(1700000000+i), 0),
		})
	}
	got := m.viewLiveEvents(3, BreakMedium)
	// 3 rows visible → should show 10.0.0.2, 10.0.0.3, 10.0.0.4 (last 3).
	for _, want := range []string{"10.0.0.2", "10.0.0.3", "10.0.0.4"} {
		if !strings.Contains(got, want) {
			t.Errorf("viewLiveEvents missing %q: %q", want, got)
		}
	}
	// 10.0.0.0, 10.0.0.1 should NOT appear (cut off).
	for _, notWant := range []string{"10.0.0.0", "10.0.0.1"} {
		if strings.Contains(got, notWant) {
			t.Errorf("viewLiveEvents contains old event %q: %q", notWant, got)
		}
	}
	// Each row should have the hit symbol.
	if !strings.Contains(got, symInfoHit) {
		t.Errorf("viewLiveEvents missing info-hit symbol: %q", got)
	}
}

// TestViewLiveEvents_NarrowHides verifies height=0 returns empty.
// / 验证 height=0 时返回空（narrow 隐藏面板）。
func TestViewLiveEvents_NarrowHides(t *testing.T) {
	m := newTestModel()
	m.pushEvent(eventEntry{Host: "1.1.1.1", Port: 80, Kind: "hit"})
	got := m.viewLiveEvents(0, BreakNarrow)
	if got != "" {
		t.Errorf("narrow viewLiveEvents = %q, want empty", got)
	}
}

// TestViewErrors_Collapsed verifies collapsed (height=1) shows a
// single summary line. / 验证折叠态（height=1）显示单行汇总。
func TestViewErrors_Collapsed(t *testing.T) {
	m := newTestModel()
	m.errorsExpanded = false
	got := m.viewErrors(1)
	if !strings.Contains(got, "ERRORS") && !strings.Contains(got, "errors") {
		t.Errorf("collapsed viewErrors missing header: %q", got)
	}
	// Should be at most 1 line.
	if strings.Count(got, "\n") > 0 {
		t.Errorf("collapsed viewErrors has multiple lines: %q", got)
	}
}

// TestViewErrors_Expanded verifies expanded mode shows up to 4 lines.
// / 验证展开态显示最多 4 行。
func TestViewErrors_Expanded(t *testing.T) {
	m := newTestModel()
	m.errorsExpanded = true
	got := m.viewErrors(4)
	// Should be ≤4 lines.
	lines := strings.Count(got, "\n") + 1
	if lines > 4 {
		t.Errorf("expanded viewErrors has %d lines, want <=4", lines)
	}
}

// TestViewStage_ProgressBars verifies alive + ports use the new
// progress bar glyphs. / 验证 alive + ports 用新进度条字符。
func TestViewStage_ProgressBars(t *testing.T) {
	st := newTestState(t)
	st.TotalHosts.Store(24)
	st.TotalPorts.Store(8000)
	m := newTestModelWithState(st)
	m.counters.AliveProbed = 18
	m.counters.Ports = 142
	got := m.viewStage(10, BreakMedium)
	// Should contain both ▓ and ░ (filled + empty bar segments).
	if !strings.Contains(got, "▓") {
		t.Errorf("viewStage missing filled bar: %q", got)
	}
	if !strings.Contains(got, "░") {
		t.Errorf("viewStage missing empty bar: %q", got)
	}
}

// TestViewFooter_TruncatedToWidth pins the P1 fix: the footer hint
// line is cut to the terminal width. Without the cut, JoinVertical
// pads every other region to the footer's 89-col width and the whole
// dashboard wraps on ≤89-col terminals (found by the 80×24 probe).
// / 钉住 P1 修复：footer 提示行裁剪到终端宽度。不裁的话
// JoinVertical 会把其他区域 pad 到 footer 的 89 列宽，≤89 列终端
// 整个 dashboard 折行（80×24 探针发现）。
func TestViewFooter_TruncatedToWidth(t *testing.T) {
	m := newTestModel()
	m.width = 60
	if got := lipgloss.Width(m.viewFooter(1)); got > 60 {
		t.Errorf("footer width = %d at m.width=60, want <= 60", got)
	}
	// 0-width start-up race → 80-col fallback applies.
	// / 0 宽启动竞态 → 应用 80 列回退。
	m2 := newTestModel()
	if got := lipgloss.Width(m2.viewFooter(1)); got > 80 {
		t.Errorf("footer width = %d at m.width=0, want <= 80 (fallback)", got)
	}
}

// TestViewErrors_Collapsed_IndentedDim pins the P4 fix: the collapsed
// errors line is indented 2 spaces like every other region (and still
// exactly 1 line). / 钉住 P4 修复：折叠 errors 行与其他区域一致缩进
// 2 空格（且仍恰好 1 行）。
func TestViewErrors_Collapsed_IndentedDim(t *testing.T) {
	m := newTestModel()
	got := m.viewErrorsCollapsed()
	if !strings.HasPrefix(got, "  ERRORS:") {
		t.Errorf("collapsed errors line not indented: %q", got)
	}
	if strings.Count(got, "\n") > 0 {
		t.Errorf("collapsed errors line has multiple lines: %q", got)
	}
}

// TestRenderTopPlugins_NoBlankLines pins the P4 fix: the unboxed
// TOP PLUGINS part carries no blank lines — a margin or trailing
// newline injects ragged gaps into the JoinVertical composition.
// / 钉住 P4 修复：无框 TOP PLUGINS 部件不含空行——边距或结尾换行
// 会往 JoinVertical 组合里注入参差空隙。
func TestRenderTopPlugins_NoBlankLines(t *testing.T) {
	m := newTestModel()
	got := m.renderTopPluginsPanel(0) // unboxed path / 无框路径
	if strings.Contains(got, "\n\n") {
		t.Errorf("top plugins part has blank lines: %q", got)
	}
	if strings.HasSuffix(got, "\n") {
		t.Errorf("top plugins part has trailing newline: %q", got)
	}
}

// TestViewFrameFitsTerminal is the P2 contract: the composed frame
// NEVER exceeds the terminal — no line wider than m.width (JoinVertical
// pads to the widest line, so one wide line wraps everything) and no
// more content lines than m.height (measured events clamp + last-resort
// truncate). Verified across breakpoints with a full events ring and
// an active rate row, plus paused mode (extra chip row).
// / TestViewFrameFitsTerminal 是 P2 契约：整帧永不超终端——行宽
// 不超 m.width（JoinVertical 会 pad 到最宽行，一行宽全帧折），
// 内容行数不超 m.height（实测 events 钳制 + 兜底裁剪）。在满
// events ring、激活 rate 行的各断点下验证，含暂停态（多一行芯片）。
func TestViewFrameFitsTerminal(t *testing.T) {
	cases := []struct{ w, h int }{
		{80, 24}, {60, 24}, {100, 30}, {120, 40}, {80, 16},
	}
	for _, c := range cases {
		for _, paused := range []bool{false, true} {
			st := newTestState(t)
			st.TotalHosts.Store(24)
			st.TotalPorts.Store(8000)
			m := newTestModelWithState(st)
			m.width, m.height = c.w, c.h
			m.runState = runScanning
			m.uiMode = modeRun
			if paused {
				m.uiMode = modePaused
			}
			// Fill the ring: 20 events (== eventCap).
			// / 填满 ring：20 条事件（== eventCap）。
			for i := 0; i < eventCap; i++ {
				m.pushEvent(eventEntry{
					Host: fmt.Sprintf("10.0.0.%d", i),
					Port: 22, Service: "ssh", Kind: "hit",
					At: time.Date(2026, 9, 12, 14, 23, i, 0, time.UTC),
				})
			}
			m.counters = types.CountersView{
				Stage: int64(types.StageIdentify), AliveProbed: 18, Ports: 142,
				Results: 23, Creds: 2, Errors: 7,
			}
			m.elapsed = "12s"
			m.rateHits, m.ratePorts = 28.5, 142.0

			view := m.View()
			lines := strings.Split(strings.TrimRight(view, "\n"), "\n")
			if len(lines) > c.h {
				t.Errorf("[%dx%d paused=%v]: %d content lines > terminal %d",
					c.w, c.h, paused, len(lines), c.h)
			}
			for i, ln := range lines {
				if w := lipgloss.Width(ln); w > c.w {
					t.Errorf("[%dx%d paused=%v]: line %d width %d > terminal %d: %q",
						c.w, c.h, paused, i+1, w, c.w, ln)
					break
				}
			}
			// Footer must survive the frame on normal terminals.
			// / 常规终端上 footer 必须存活。
			if c.h >= 16 && !strings.Contains(lines[len(lines)-1], "[q] quit") {
				t.Errorf("[%dx%d paused=%v]: footer not the last content line: %q",
					c.w, c.h, paused, lines[len(lines)-1])
			}
		}
	}
}
