// view_test.go — tests for visual helpers (renderBar, sparkline,
// truncate, symFor, severityColor) and golden-file snapshots for
// each region renderer (viewHeader, viewEvents, viewCounters, etc.).
// / view_test.go — 视觉辅助函数测试及每个区域渲染器（viewHeader、
// viewEvents、viewCounters 等）的 golden-file 快照。
package tui

import (
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
