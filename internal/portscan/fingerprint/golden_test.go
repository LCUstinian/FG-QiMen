// golden_test.go — fingerprint quality measurement harness (方向1 步骤1:
// 先测量). Loads testdata/golden.json, feeds every banner through the
// production MatchBanner path (the exact engine wired at
// core/pipeline_workers.go stage 0), and reports identification
// metrics. Strict-tier cases double as regression guards; report-tier
// cases document observed behaviour without failing CI.
//
// Fixture policy: synthetic banners and public-protocol document
// samples ONLY — no banners captured from real internal/production
// networks may be committed (sensitive-data rule).
//
// golden_test.go — 指纹识别质量度量 harness（方向1 步骤1：先测量）。
// 加载 testdata/golden.json，把每条 banner 送入生产 MatchBanner 路径
// （与 core/pipeline_workers.go stage 0 接线的同一引擎），输出识别
// 率指标。strict 档用例兼作回归守卫；report 档用例记录实测行为，
// 不使 CI 变红。
//
// 样本政策：只允许合成 banner 与公开协议文档样例——禁止把真实
// 内网/生产环境抓取的 banner 提交进仓库（敏感数据红线）。
package fingerprint_test

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"testing"

	"github.com/LCUstinian/FG-QiMen/internal/portscan/fingerprint"
)

// maxPassiveBanner mirrors the production passive grab cap
// (core/scan FirstBanner reads at most 256 bytes). The harness
// truncates every fixture the same way so the measurement reflects
// what the scanner actually feeds MatchBanner in the field.
// / maxPassiveBanner 对齐生产被动抓取上限（core/scan FirstBanner 最
// 多读 256 字节）。harness 按同样方式截断样本，使度量反映扫描器在
// 现场真正喂给 MatchBanner 的输入。
const maxPassiveBanner = 256

// goldenCase is one fixture in testdata/golden.json.
// / goldenCase 是 testdata/golden.json 中的一条样本。
type goldenCase struct {
	ID         string `json:"id"`
	Source     string `json:"source"`     // synthetic | rfc-867 | public-protocol
	Banner     string `json:"banner"`     // literal text (must be pure ASCII/UTF-8-safe bytes)
	BannerEsc  string `json:"banner_esc"` // nmap-style escapes (\xff...) decoded via production DecodePattern — REQUIRED for banners with bytes ≥0x80, because JSON \u00XX would UTF-8-encode into two bytes and corrupt the fixture
	Service    string `json:"service"`    // exact expectation, "svc?" included; "" = expect no match
	Product    string `json:"product"`    // "" = don't assert
	Version    string `json:"version"`    // "" = don't assert
	Soft       bool   `json:"soft"`
	NeedsProbe bool   `json:"needs_probe"` // passive path is structurally blind for this service
	Tier       string `json:"tier"`        // strict | report
	Note       string `json:"note"`
}

// fixtureBanner resolves the raw banner bytes for a case: either the
// literal field or the nmap-escape field decoded through the
// production decoder (the same DecodePattern the probe file uses).
// / fixtureBanner 解析样本的原始 banner 字节：字面量字段，或经生产
// 解码器（与探针文件同一 DecodePattern）解码的 nmap 转义字段。
func fixtureBanner(t *testing.T, c goldenCase) []byte {
	t.Helper()
	if c.BannerEsc != "" {
		dec, err := fingerprint.DecodePattern(c.BannerEsc)
		if err != nil {
			t.Fatalf("case %s: DecodePattern(banner_esc): %v", c.ID, err)
		}
		return dec
	}
	return []byte(c.Banner)
}

func loadGolden(t *testing.T) []goldenCase {
	t.Helper()
	raw, err := os.ReadFile("testdata/golden.json")
	if err != nil {
		t.Fatalf("read golden dataset: %v", err)
	}
	var cases []goldenCase
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatalf("parse golden dataset: %v", err)
	}
	if len(cases) == 0 {
		t.Fatal("golden dataset is empty")
	}
	return cases
}

// TestGolden_Metrics measures the passive-identification quality of the
// production engine against the golden dataset and prints a per-case
// table plus aggregate rates. Outcomes: HARD (authoritative), SOFT
// ("svc?" hint), MISS (no rule), WRONG (a service OTHER than expected
// answered — a false positive against the fixture).
// / TestGolden_Metrics 用 golden 数据集度量生产引擎的被动识别质量，
// 输出逐用例表格与汇总率。结果分类：HARD（权威命中）、SOFT（"svc?"
// 提示）、MISS（无规则命中）、WRONG（命中了期望之外的服务——对样本
// 而言即误报）。
func TestGolden_Metrics(t *testing.T) {
	v := fingerprint.NewVScan()

	tcpRules := 0
	for _, p := range v.Probes {
		if p.Matchs != nil {
			tcpRules += len(*p.Matchs)
		}
	}
	t.Logf("engine: tcp_probes=%d tcp_match_rules=%d", len(v.Probes), tcpRules)

	cases := loadGolden(t)

	var hard, soft, miss, wrong, needsProbe int
	for _, c := range cases {
		banner := fixtureBanner(t, c)
		if len(banner) > maxPassiveBanner {
			banner = banner[:maxPassiveBanner]
		}
		if c.NeedsProbe {
			needsProbe++
			if c.Banner != "" {
				if _, ok := v.MatchBanner(banner); ok {
					wrong++
					t.Errorf("case %s: needs_probe case unexpectedly matched", c.ID)
				}
			}
			continue
		}

		got, ok := v.MatchBanner(banner)
		outcome, detail := classify(c, got, ok)

		switch outcome {
		case "HARD":
			hard++
		case "SOFT":
			soft++
		case "MISS":
			miss++
		case "WRONG":
			wrong++
		}

		msg := fmt.Sprintf("case %-28s tier=%-6s outcome=%-5s %s", c.ID, c.Tier, outcome, detail)
		if c.Tier == "strict" && (outcome == "WRONG" || outcome == "MISS" || deviation(c, got, ok)) {
			t.Errorf("%s", msg)
		} else {
			t.Log(msg)
		}
	}

	measurable := len(cases) - needsProbe
	ident := hard + soft
	t.Logf("SUMMARY: total=%d needs_probe=%d measurable=%d hard=%d soft=%d miss=%d wrong=%d",
		len(cases), needsProbe, measurable, hard, soft, miss, wrong)
	if measurable > 0 {
		t.Logf("SUMMARY: hard_rate=%.1f%% ident_rate=%.1f%% false_pos=%d",
			100*float64(hard)/float64(measurable),
			100*float64(ident)/float64(measurable),
			wrong)
	}
}

// classify maps the engine outcome against the fixture expectation.
// / classify 把引擎结果对照样本期望做分类。
func classify(c goldenCase, got fingerprint.BannerMatch, ok bool) (outcome, detail string) {
	switch {
	case !ok:
		if c.Service == "" {
			return "MISS", "expected-miss confirmed"
		}
		return "MISS", fmt.Sprintf("want %q", c.Service)
	case c.Service == "":
		// Expected a miss but something matched — false positive.
		// / 期望未命中却有命中——误报。
		return "WRONG", fmt.Sprintf("unwanted hit svc=%q product=%q version=%q soft=%v",
			got.Service, got.Product, got.Version, got.Soft)
	case got.Service != c.Service:
		return "WRONG", fmt.Sprintf("svc=%q want=%q", got.Service, c.Service)
	case got.Soft:
		return "SOFT", fmt.Sprintf("svc=%q (soft demotion)", got.Service)
	default:
		return "HARD", fmt.Sprintf("svc=%q product=%q version=%q", got.Service, got.Product, got.Version)
	}
}

// deviation reports whether a hit deviates from the per-field
// expectations (product/version/soft flag) beyond the service name.
// / deviation 判断命中在 service 之外的逐字段期望（product/version/
// soft 标志）上是否有偏差。
func deviation(c goldenCase, got fingerprint.BannerMatch, ok bool) bool {
	if !ok {
		return false // MISS handled by classify/tier switch
	}
	if c.Soft != got.Soft {
		return true
	}
	if c.Product != "" && got.Product != c.Product {
		return true
	}
	if c.Version != "" && got.Version != c.Version {
		return true
	}
	return false
}

// TestGolden_GarbageSweep is the systematic false-positive guard:
// deterministic pseudo-random garbage (the class that once fired
// jrpgt on 2972/3000 banners before the escape-fidelity fix) must
// produce ZERO hard matches. Soft hits are reported, not asserted —
// loose protocol hints firing on noise is tolerated by design; hard
// product/version claims on noise never are.
// / TestGolden_GarbageSweep 是系统化误报守卫：确定性伪随机垃圾字节
// （转义保真修复前曾在 3000 条噪声上命中 jrpgt 2972 次的那一类）必须
// 零硬匹配。soft 命中只记录不断言——宽松协议提示偶尔咬中噪声是设计
// 内的容忍；但对噪声给出硬 product/version 断言绝不可容忍。
func TestGolden_GarbageSweep(t *testing.T) {
	v := fingerprint.NewVScan()
	rng := rand.New(rand.NewSource(20260913)) // fixed seed: fully deterministic / 固定种子：完全确定性

	const sweep = 256 // aligned with the passive grab cap / 对齐被动抓取上限

	hardHits, softHits := 0, 0
	for i := 0; i < sweep; i++ {
		var banner []byte
		switch i % 3 {
		case 0: // raw uniform bytes / 均匀原始字节
			banner = make([]byte, 1+rng.Intn(maxPassiveBanner))
			for j := range banner {
				banner[j] = byte(rng.Intn(256))
			}
		case 1: // printable ASCII noise / 可打印 ASCII 噪声
			banner = make([]byte, 8+rng.Intn(maxPassiveBanner-8))
			for j := range banner {
				banner[j] = byte(0x20 + rng.Intn(0x5f))
			}
		default: // word soup with CRLF, stresses text rules / 带换行的词组噪声，压测文本规则
			banner = []byte(wordSoup(rng))
		}

		got, ok := v.MatchBanner(banner)
		if !ok {
			continue
		}
		if got.Soft {
			softHits++
			t.Logf("soft hit on garbage: svc=%q", got.Service)
			continue
		}
		hardHits++
		t.Errorf("HARD match on garbage banner: svc=%q product=%q version=%q (i=%d)",
			got.Service, got.Product, got.Version, i)
	}

	t.Logf("GARBAGE-SWEEP: banners=%d hard=%d soft=%d hard_rate=%.2f%%",
		sweep, hardHits, softHits, 100*float64(hardHits)/float64(sweep))
}

// wordSoup builds deterministic text garbage: random uppercase tokens
// joined by spaces/newlines — invalid as any protocol, valid as noise.
// / wordSoup 构造确定性文本垃圾：随机大写词元以空格/换行连接——对任
// 何协议都非法，作为噪声完全合法。
func wordSoup(rng *rand.Rand) string {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	out := make([]byte, 0, 64+rng.Intn(192))
	for len(out) < cap(out)-2 {
		wordLen := 1 + rng.Intn(12)
		for k := 0; k < wordLen && len(out) < cap(out)-2; k++ {
			out = append(out, alphabet[rng.Intn(len(alphabet))])
		}
		switch rng.Intn(4) {
		case 0:
			out = append(out, '\r', '\n')
		case 1:
			out = append(out, ' ')
		case 2:
			out = append(out, ':', ' ')
		default:
			out = append(out, '-')
		}
	}
	return string(out)
}
