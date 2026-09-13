// probe_golden_test.go — fingerprint quality measurement for the TCP
// ACTIVE-probe path (the counterpart of golden_test.go's passive
// harness). Loads testdata/golden_probes.json, feeds each probe
// RESPONSE through the same production MatchBanner the Stage-0 probe
// fallback uses, and reports per-class metrics:
//
//	hard / soft  — the probe's response produced an identity claim
//	miss         — the response stayed silent to the rule set
//	wrong        — a rule OTHER than expected fired (false positive)
//
// The FALSE-POSITIVE GUARD cases are the load-bearing rows: probe
// responses are matched against the WHOLE rule set (not just the
// sending probe's rules), so well-known "service scolded us" banners
// (SSH Protocol mismatch, SMTP 554) must stay silent — a hard claim
// from either would poison every silent port 22/25 on the network.
//
// Fixture policy: identical to golden.json — synthetic/public-protocol
// samples only, no real-network captures.
//
// probe_golden_test.go — TCP 主动探针路径的指纹质量度量（golden_test.go
// 被动 harness 的对应物）。加载 testdata/golden_probes.json，把每条探
// 针响应送入 Stage-0 探针兜底所用的同一生产 MatchBanner，输出分类度量：
//
//	hard / soft  — 探针响应产出了身份断言
//	miss         — 响应对规则集沉默
//	wrong        — 命中了期望之外的规则（误报）
//
// FALSE-POSITIVE GUARD 用例是承重行：探针响应对全规则集匹配（而非仅
// 发送探针的规则），因此著名的"服务训斥我们"响应（SSH Protocol
// mismatch、SMTP 554）必须保持沉默——任何一方给出硬断言都会毒化全网
// 的沉默端口 22/25。
//
// 样本政策与 golden.json 一致——只允许合成/公开协议样本，禁真实抓包。
package fingerprint_test

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/LCUstinian/FG-QiMen/internal/portscan/fingerprint"
)

// probeCase is one fixture in testdata/golden_probes.json.
// / probeCase 是 testdata/golden_probes.json 中的一条样本。
type probeCase struct {
	ID       string `json:"id"`
	Source   string `json:"source"`
	ViaProbe string `json:"via_probe"` // the probe whose payload elicited this response / 引出该响应的探针
	Banner   string `json:"banner"`
	Service  string `json:"service"` // "" = expect silence (false-positive guard)
	Product  string `json:"product"`
	Version  string `json:"version"`
	Soft     bool   `json:"soft"`
	Tier     string `json:"tier"`
	Note     string `json:"note"`
}

func loadProbeGolden(t *testing.T) []probeCase {
	t.Helper()
	raw, err := os.ReadFile("testdata/golden_probes.json")
	if err != nil {
		t.Fatalf("read probe golden dataset: %v", err)
	}
	var cases []probeCase
	if err := json.Unmarshal(raw, &cases); err != nil {
		t.Fatalf("parse probe golden dataset: %v", err)
	}
	if len(cases) == 0 {
		t.Fatal("probe golden dataset is empty")
	}
	return cases
}

func TestGolden_ProbeMetrics(t *testing.T) {
	v := fingerprint.NewVScan()

	// via_probe must name a real TCP probe — a typo here silently
	// turns the fixture into documentation fiction.
	// / via_probe 必须是真实存在的 TCP 探针——这里的 typo 会让样本
	// 沦为纸面虚构。
	known := make(map[string]bool, len(v.Probes))
	for _, p := range v.Probes {
		known[p.Name] = true
	}

	cases := loadProbeGolden(t)

	var hard, soft, miss, wrong int
	for _, c := range cases {
		if !known[c.ViaProbe] {
			t.Errorf("case %s: via_probe %q is not a TCP probe in the database", c.ID, c.ViaProbe)
			continue
		}

		got, ok := v.MatchBanner([]byte(c.Banner))
		var outcome, detail string
		switch {
		case !ok:
			miss++
			outcome = "MISS"
			if c.Service == "" {
				detail = "expected-silence confirmed"
			} else {
				detail = fmt.Sprintf("want %q", c.Service)
			}
		case c.Service == "":
			wrong++
			outcome = "WRONG"
			detail = fmt.Sprintf("unwanted hit svc=%q product=%q version=%q soft=%v",
				got.Service, got.Product, got.Version, got.Soft)
		case got.Service != c.Service:
			wrong++
			outcome = "WRONG"
			detail = fmt.Sprintf("svc=%q want=%q", got.Service, c.Service)
		case got.Soft:
			soft++
			outcome = "SOFT"
			detail = fmt.Sprintf("svc=%q (soft demotion)", got.Service)
		default:
			hard++
			outcome = "HARD"
			detail = fmt.Sprintf("svc=%q product=%q version=%q", got.Service, got.Product, got.Version)
		}

		// Strict failures: a false positive, a hard expectation that
		// missed, or a per-field deviation. A MISS on a service="" guard
		// case is the CONFIRMED outcome, not a failure.
		// / strict 失败：误报、期望命中却 MISS、或逐字段偏差。service=""
		// 守卫用例上的 MISS 是"确认结果"，不是失败。
		deviates := ok && c.Service != "" &&
			(c.Soft != got.Soft ||
				(c.Product != "" && got.Product != c.Product) ||
				(c.Version != "" && got.Version != c.Version))
		strictFail := c.Tier == "strict" &&
			(outcome == "WRONG" || (outcome == "MISS" && c.Service != "") || deviates)

		msg := fmt.Sprintf("case %-32s via=%-12s tier=%-6s outcome=%-5s %s",
			c.ID, c.ViaProbe, c.Tier, outcome, detail)
		if strictFail {
			t.Errorf("%s", msg)
		} else {
			t.Log(msg)
		}
	}

	ident := hard + soft
	t.Logf("PROBE-SUMMARY: total=%d hard=%d soft=%d miss=%d wrong=%d ident_rate=%.1f%% false_pos=%d",
		len(cases), hard, soft, miss, wrong, 100*float64(ident)/float64(len(cases)), wrong)
}
