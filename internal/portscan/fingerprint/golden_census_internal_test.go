// golden_census_internal_test.go — silent-drop census for the embedded
// probe database. The second half of the 方向1 measurement: not only
// "how much does the engine identify", but "how much COULD it identify
// if nothing died at parse time".
//
// Go regexp supports neither PCRE lookaround ((?=, (?!, (?<) nor
// backreferences (\1..\9). Upstream nmap-service-probes rules using
// them fail regexp.Compile inside parseMatchDirective and are silently
// dropped (parser.go only surfaces the deliberate looseRuleBlacklist
// branch; compile failures just `continue`). This census quantifies
// the silent death toll and buckets it by failure cause.
//
// golden_census_internal_test.go — 嵌入探针库的静默丢弃普查。方向1
// 度量的后半：不只量"引擎识别了多少"，还要量"若解析期不死，引擎本
// 能识别多少"。Go regexp 既不支持 PCRE lookaround（(?=、(?!、(?<），
// 也不支持反向引用（\1..\9）；上游规则中含这两类语法的 pattern 会在
// parseMatchDirective 内编译失败并被静默丢弃（parser.go 只有黑名单
// 分支是显式的，编译失败只是 continue）。本测试量化静默死亡数并按
// 失败原因分桶。
package fingerprint

import (
	"strings"
	"testing"
)

func TestGolden_ParseCensus(t *testing.T) {
	lines := strings.Split(embeddedProbes, "\n")

	var total, compileFail, lookaroundFail, repeatFail, escapeFail, otherFail, blacklisted int
	for _, line := range lines {
		var prefix string
		switch {
		case strings.HasPrefix(line, "match "):
			prefix = "match"
		case strings.HasPrefix(line, "softmatch "):
			prefix = "softmatch"
		default:
			continue
		}
		total++
		p := &Probe{}
		m, err := p.parseMatchDirective(line, prefix, prefix == "softmatch")
		if err != nil {
			compileFail++
			// Classify from the SOURCE LINE, not the error text: Go
			// reports lookbehind (?< as "invalid named capture", so
			// error-text matching undercounts the lookaround family.
			// / 按"源码行特征"分类而非报错文本：Go 把 (?< 后顾断言报成
			// "invalid named capture"，按报错文本匹配会漏计 lookaround。
			switch {
			case strings.Contains(line, "(?=") || strings.Contains(line, "(?!") || strings.Contains(line, "(?<"):
				lookaroundFail++ // PCRE lookaround — Go regexp hard limit
			case strings.Contains(err.Error(), "invalid repeat count"):
				repeatFail++ // Go repeat cap is 1000
			case strings.Contains(err.Error(), "invalid escape sequence"):
				escapeFail++ // includes \1..\9 backreferences
			default:
				otherFail++
				if otherFail <= 5 {
					t.Logf("other compile failure: %v", err)
				}
			}
			continue
		}
		if looseRuleBlacklist[m.Service] {
			blacklisted++
		}
	}

	if total == 0 {
		t.Fatal("no match directives found in embedded probes")
	}
	alive := total - compileFail - blacklisted
	t.Logf("CENSUS: file_rules=%d alive=%d (%.1f%%)  compile_failed=%d (lookaround=%d repeat_over_1000=%d bad_escape=%d other=%d)  blacklisted=%d",
		total, alive, 100*float64(alive)/float64(total),
		compileFail, lookaroundFail, repeatFail, escapeFail, otherFail, blacklisted)
}
