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
// demoted to nmap's "service?" convention with no product/version.
// / TestMatchBanner_HardPreferredOverSoft 钉死置信度契约：任何位置的
// 硬匹配都优先于更早的 soft 命中；孤立的 soft 命中按 nmap "service?"
// 惯例降级且不带产品/版本。
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
	bm, ok := v.MatchBanner([]byte("x-soft-y-hard-z"))
	if !ok || bm.Service != "hardsvc" || bm.Product != "Hard" || bm.Version != "1.2" || bm.Soft {
		t.Errorf("hard-preference: got %+v ok=%v, want hardsvc Hard/1.2 not-soft", bm, ok)
	}

	// Lone soft hit → "service?" with no product/version.
	// / 孤立 soft 命中 → "service?" 且产品/版本为空。
	bm, ok = v.MatchBanner([]byte("just-soft-here"))
	if !ok || bm.Service != "softsvc?" || bm.Product != "" || bm.Version != "" || !bm.Soft {
		t.Errorf("soft demotion: got %+v ok=%v, want \"softsvc?\" empty identity Soft=true", bm, ok)
	}

	// Empty banner never matches. / 空 banner 永不匹配。
	if _, ok := v.MatchBanner(nil); ok {
		t.Errorf("empty banner must not match")
	}
}

// TestParseVersionInfo pins the p/v segment parser: whitespace-skipping,
// space-bearing values, backslash-escaped delimiters, missing p/v, and
// $N substitution (in-range and out-of-range). Multi-directive tails
// (i/o/d/cpe) must be ignored.
// / TestParseVersionInfo 钉死 p/v 分段解析器：跳空白、带空格的值、
// 反斜杠转义分隔符、缺失 p/v、$N 替换（范围内与越界）。多指令尾段
// （i/o/d/cpe）必须忽略。
func TestParseVersionInfo(t *testing.T) {
	cases := []struct {
		name        string
		vi          string
		subs        []string
		wantProduct string
		wantVersion string
	}{
		{
			name:        "typical full template",
			vi:          " p/OpenSSH/ v/1:8.9p1 Debian-3ubuntu0.1/ i/protocol 2.0/ o/Linux/ cpe:/a:openbsd:openssh:8.9p1/ cpe:/o:linux:linux_kernel/",
			subs:        []string{"2.0", "8.9p1"},
			wantProduct: "OpenSSH",
			wantVersion: "1:8.9p1 Debian-3ubuntu0.1",
		},
		{
			name:        "$N substitution",
			vi:          " p/Nginx/ v/$1/",
			subs:        []string{"1.25.3"},
			wantProduct: "Nginx",
			wantVersion: "1.25.3",
		},
		{
			name:        "out-of-range $N expands to empty",
			vi:          " p/X/ v/$5/",
			subs:        []string{"1"},
			wantProduct: "X",
			wantVersion: "",
		},
		{
			name:        "$N with no subs expands to empty",
			vi:          " p/X/ v/$1/",
			subs:        nil,
			wantProduct: "X",
			wantVersion: "",
		},
		{
			name:        "escaped delimiter inside value",
			vi:          ` p/Weird\/Name/ v/2.0/`,
			subs:        nil,
			wantProduct: "Weird/Name",
			wantVersion: "2.0",
		},
		{
			name:        "missing v segment",
			vi:          " p/OnlyP/",
			subs:        nil,
			wantProduct: "OnlyP",
			wantVersion: "",
		},
		{
			name:        "missing p segment",
			vi:          " v/9.9/",
			subs:        nil,
			wantProduct: "",
			wantVersion: "9.9",
		},
		{
			name:        "empty input",
			vi:          "",
			subs:        nil,
			wantProduct: "",
			wantVersion: "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			product, version := parseVersionInfo(c.vi, c.subs)
			if product != c.wantProduct || version != c.wantVersion {
				t.Errorf("parseVersionInfo(%q) = (%q, %q), want (%q, %q)",
					c.vi, product, version, c.wantProduct, c.wantVersion)
			}
		})
	}
}
