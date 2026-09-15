// degrade_test.go — §6.2 degradation-ladder tests: the ASCII glyph
// table (level 4) and the color ladder (NO_COLOR kill switch + 16-
// color grayscale). ASCII golden frames live in golden_test.go's
// directory under an -ascii suffix.
//
// degrade_test.go — §6.2 降级阶梯测试：ASCII 字形表（第 4 级）与颜色
// 阶梯（NO_COLOR 总闸 + 16 色灰度）。ASCII golden 帧在 golden 目录，
// 以 -ascii 后缀区分。
package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// snapshotGlyphTable captures every glyph var; the returned closure
// restores it. Glyphs are package globals — every test that swaps the
// table must register the restore via t.Cleanup or table mutations
// leak into later cases (golden frames included).
// / snapshotGlyphTable 捕获全部字形 var；返回闭包负责恢复。字形是包
// 级全局——任何换表的测试都必须用 t.Cleanup 注册恢复，否则突变会泄
// 漏进后面的用例（含 golden 帧）。
func snapshotGlyphTable() func() {
	snap := glyphSnapshotFn()
	return snap.restore
}

// TestSetASCIIFallback_TablePurity pins the ASCII symbol-table
// contract: after the swap every glyph is non-empty, pure ASCII
// (bytes < 0x80), exactly one display column, and the ramp strings
// keep their Unicode rune counts (barFracs fraction indexing and
// sparkline level indexing stay valid).
// / TestSetASCIIFallback_TablePurity 钉住 ASCII 符号表契约：换表后
// 每个字形非空、纯 ASCII（字节 < 0x80）、恰好 1 显示列，且坡串保持
// Unicode 的 rune 数（barFracs 八分块索引与 sparkline 层级索引仍有效）。
func TestSetASCIIFallback_TablePurity(t *testing.T) {
	defer snapshotGlyphTable()()
	SetASCIIFallback(true)

	// Multi-glyph collections: each entry is a RAMP of one-column glyphs,
	// so the width-1 law does not apply (their rune counts are pinned
	// separately below).
	// / 多字形集合：每一项是单列字形的坡串，单宽律不适用（rune 数在下
	// 面单独钉住）。
	ramps := map[string]bool{"spinnerFrames": true, "barFracs": true, "glSpark": true}

	for _, name := range glyphNames {
		g := *glyphPointers[name]
		if g == "" {
			t.Errorf("glyph %q is empty after ASCII fallback", name)
			continue
		}
		if !ramps[name] {
			if w := lipgloss.Width(g); w != 1 {
				t.Errorf("glyph %q = %q has width %d, want 1 (width law)", name, g, w)
			}
		}
		for _, r := range g {
			if r >= 0x80 {
				t.Errorf("glyph %q = %q contains non-ASCII rune %q", name, g, r)
			}
		}
	}
	if len(spinnerFrames) != 4 {
		t.Errorf("ASCII spinnerFrames has %d frames, want 4 (mod arithmetic)", len(spinnerFrames))
	}
	if len(barFracs) != barFracsUnicode {
		t.Errorf("ASCII barFracs has %d runes, want %d (fraction indexing)", len([]rune(barFracs)), barFracsUnicode)
	}
	if len([]rune(glSpark)) != glSparkUnicode {
		t.Errorf("ASCII glSpark has %d runes, want %d (level indexing)", len([]rune(glSpark)), glSparkUnicode)
	}
}

// TestSetASCIIFallback_WidthParity pins the degradation purity law
// (spec §6.2): swapping the table changes GLYPHS only — every ASCII
// glyph occupies the same display column as its Unicode counterpart,
// so the lattice width law and golden guards survive degradation.
// / TestSetASCIIFallback_WidthParity 钉住降级纯度律（spec §6.2）：
// 换表只换字形——每个 ASCII 字形与 Unicode 对应物占相同显示列，
// lattice 宽度律与 golden 守卫在降级后依然成立。
func TestSetASCIIFallback_WidthParity(t *testing.T) {
	defer snapshotGlyphTable()()
	uni := map[string]string{}
	for _, name := range glyphNames {
		uni[name] = *glyphPointers[name]
	}
	SetASCIIFallback(true)
	for _, name := range glyphNames {
		if lipgloss.Width(uni[name]) != lipgloss.Width(*glyphPointers[name]) {
			t.Errorf("glyph %q: unicode width %d != ascii width %d",
				name, lipgloss.Width(uni[name]), lipgloss.Width(*glyphPointers[name]))
		}
	}
}

// TestSetASCIIFallback_Idempotent verifies the swap is safe to call
// twice (production wiring + test harness both call it).
// / TestSetASCIIFallback_Idempotent 验证换表可重复调用（生产接线与
// 测试夹具都会调）。
func TestSetASCIIFallback_Idempotent(t *testing.T) {
	defer snapshotGlyphTable()()
	SetASCIIFallback(true)
	first := *glyphPointers["boxH"]
	SetASCIIFallback(true)
	if *glyphPointers["boxH"] != first {
		t.Error("second SetASCIIFallback(true) call changed the table")
	}
}

// TestApplyColorLadder drives the color half of the §6.2 ladder:
// NO_COLOR dims every signal/focus token, a 16-color ANSI profile
// remaps signal to grayscale (warn keeps the light-gray token), and
// TrueColor leaves the palette untouched.
// / TestApplyColorLadder 驱动 §6.2 阶梯的颜色半边：NO_COLOR 把一切
// 信号/焦点令牌降为 dim，16 色 ANSI profile 把信号域映射为灰度
// （warn 保留浅灰令牌），TrueColor 不动调色板。
func TestApplyColorLadder(t *testing.T) {
	// Token snapshot so every subtest starts from the shipped palette.
	// / 令牌快照，让每个子测试都从出厂调色板出发。
	type tokens struct{ accent, ok, err, warn, zone, idle lipgloss.Color }
	shipped := tokens{cAccent, cOk, cErr, cWarn, cZone, cIdle}
	restore := func() {
		cAccent, cOk, cErr = shipped.accent, shipped.ok, shipped.err
		cWarn, cZone, cIdle = shipped.warn, shipped.zone, shipped.idle
	}

	t.Run("NO_COLOR dims everything", func(t *testing.T) {
		t.Setenv("NO_COLOR", "1")
		defer restore()
		applyColorLadder(termenv.TrueColor)
		for name, c := range map[string]lipgloss.Color{
			"cAccent": cAccent, "cOk": cOk, "cErr": cErr,
			"cWarn": cWarn, "cZone": cZone, "cIdle": cIdle,
		} {
			if c != cDim {
				t.Errorf("%s = %v, want cDim under NO_COLOR", name, c)
			}
		}
	})

	t.Run("ANSI16 grayscale", func(t *testing.T) {
		defer restore()
		applyColorLadder(termenv.ANSI)
		for name, c := range map[string]lipgloss.Color{
			"cAccent": cAccent, "cOk": cOk, "cErr": cErr, "cZone": cZone,
		} {
			if c != cText {
				t.Errorf("%s = %v, want cText under ANSI16 grayscale", name, c)
			}
		}
		if cWarn != cDim {
			t.Errorf("cWarn = %v, want cDim (light gray) under ANSI16 grayscale", cWarn)
		}
		if cIdle != shipped.idle {
			t.Errorf("cIdle = %v, want unchanged (IDLE chip keeps its identity)", cIdle)
		}
	})

	t.Run("TrueColor untouched", func(t *testing.T) {
		defer restore()
		applyColorLadder(termenv.TrueColor)
		if cWarn != shipped.warn || cAccent != shipped.accent {
			t.Error("TrueColor profile must not remap tokens")
		}
	})
}

// TestASCII_FramePurity renders the run-scanning frame under the ASCII
// table and asserts the output contains no non-ASCII rune at all —
// the whole point of the fallback.
// / TestASCII_FramePurity 在 ASCII 表下渲染 run-scanning 帧，断言输
// 出完全不含非 ASCII rune——这才是回退的意义。
func TestASCII_FramePurity(t *testing.T) {
	defer snapshotGlyphTable()()
	SetASCIIFallback(true)

	m := goldenScanningModel(120, 30)
	view := ansiRe.ReplaceAllString(m.View(), "")
	for _, r := range view {
		if r >= 0x80 {
			t.Fatalf("ASCII frame contains non-ASCII rune %q", r)
		}
	}
	if strings.Contains(view, "░") || strings.Contains(view, "█") {
		t.Error("ASCII frame still contains Unicode bar glyphs")
	}
}
