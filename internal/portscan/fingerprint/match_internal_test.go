// match_internal_test.go — internal tests for the MatchBanner
// confidence contract, using synthetic probes so the ordering logic is
// tested deterministically (no embedded-DB dependency).
// / match_internal_test.go —— 用合成探针确定性测试 MatchBanner 置信度
// 契约的内部测试（不依赖 embedded DB）。
package fingerprint

import (
	"regexp"
	"testing"
)

func mustCompiled(t *testing.T, pattern string) *regexp.Regexp {
	t.Helper()
	re, err := regexp.Compile(patternToGoRegex(pattern))
	if err != nil {
		t.Fatalf("compile %q: %v", pattern, err)
	}
	return re
}

// TestMatchBanner_HardPreferredOverSoft pins the confidence contract:
// a hard match anywhere beats an earlier soft hit; a lone soft hit is
// demoted to nmap's "service?" convention with empty version info.
// / TestMatchBanner_HardPreferredOverSoft 钉死置信度契约：任何位置的
// 硬匹配都优先于更早的 soft 命中；孤立的 soft 命中按 nmap "service?"
// 惯例降级且不带版本信息。
func TestMatchBanner_HardPreferredOverSoft(t *testing.T) {
	v := &VScan{Probes: []Probe{
		{Matchs: &[]Match{{
			Service:         "softsvc",
			IsSoft:          true,
			PatternCompiled: mustCompiled(t, `soft`),
		}}},
		{Matchs: &[]Match{{
			Service:         "hardsvc",
			VersionInfo:     "p/Hard/ v/1.2/",
			PatternCompiled: mustCompiled(t, `hard`),
		}}},
	}}

	// Soft rule is in an EARLIER probe, but the hard match must win.
	// / soft 规则在更早的 probe 里，但硬匹配必须胜出。
	svc, ver, ok := v.MatchBanner([]byte("x-soft-y-hard-z"))
	if !ok || svc != "hardsvc" || ver != "p/Hard/ v/1.2/" {
		t.Errorf("hard-preference: got svc=%q ver=%q ok=%v, want hardsvc with version", svc, ver, ok)
	}

	// Lone soft hit → "service?" with empty version info.
	// / 孤立 soft 命中 → "service?" 且版本信息为空。
	svc, ver, ok = v.MatchBanner([]byte("just-soft-here"))
	if !ok || svc != "softsvc?" || ver != "" {
		t.Errorf("soft demotion: got svc=%q ver=%q ok=%v, want \"softsvc?\" with empty version", svc, ver, ok)
	}

	// Empty banner never matches. / 空 banner 永不匹配。
	if _, _, ok := v.MatchBanner(nil); ok {
		t.Errorf("empty banner must not match")
	}
}
