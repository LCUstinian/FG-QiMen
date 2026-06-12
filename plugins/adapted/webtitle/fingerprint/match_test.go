// match_test.go — unit tests for the fingerprint package.
// match_test.go — fingerprint 包的单元测试。
package fingerprint_test

import (
	"testing"

	"github.com/LCUstinian/FG-QiMen/plugins/adapted/webtitle/fingerprint"
)

// TestMatchAll_Hardcoded verifies that hardcoded rules fire when the
// expected marker is present in the body or headers. / TestMatchAll_Hardcoded
// 验证硬编码规则在 body 或 headers 包含预期标记时触发。
func TestMatchAll_Hardcoded(t *testing.T) {
	// "十年磨一剑" (the Chinese slogan in ThinkPHP) is a reliable
	// marker that appears on most ThinkPHP error pages. / "十年磨
	// 一剑"（ThinkPHP 的中文标语）是大多数 ThinkPHP 错误页都有的
	// 可靠标记。
	data := fingerprint.CheckData{
		Body: []byte("十年磨一剑 - ThinkPHP 5.1"),
	}
	hits := fingerprint.MatchAll(data)
	if !contains(hits, "ThinkPHP") {
		t.Errorf("expected ThinkPHP hit, got %v", hits)
	}
}

// TestMatchAll_Header verifies header-based rules. / TestMatchAll_Header
// 验证 header 类的规则。
func TestMatchAll_Header(t *testing.T) {
	data := fingerprint.CheckData{
		Body: []byte(""),
		Headers: "Server: nginx/1.21\n" +
			"X-Powered-By: PHP/7.4\n",
	}
	hits := fingerprint.MatchAll(data)
	if !contains(hits, "Nginx") {
		t.Errorf("expected Nginx hit, got %v", hits)
	}
	if !contains(hits, "PHP") {
		t.Errorf("expected PHP hit, got %v", hits)
	}
}

// TestMatchAll_Miss verifies a clean body / headers produce no hits.
// / TestMatchAll_Miss 验证干净 body / headers 不产生命中。
func TestMatchAll_Miss(t *testing.T) {
	data := fingerprint.CheckData{
		Body:    []byte("just a plain text page"),
		Headers: "Server: custom/1.0\n",
	}
	hits := fingerprint.MatchAll(data)
	// We don't assert empty (FingerprintHub has 3139 rules, any might match);
	// we only assert that the obviously-wrong names are absent. / 我们不
	// 断言空（FingerprintHub 有 3139 条规则，任何一条都可能匹配）；只
	// 断言显然不对的名字不在。
	if contains(hits, "PHP") || contains(hits, "Nginx") {
		t.Errorf("unexpected hits on a clean page: %v", hits)
	}
}

// TestMatchAll_EnhancedFingerprint verifies the FingerprintHub JSON
// is loaded and can match a known rule. The JSON has a rule that
// matches "CloudFlare" by a specific header. / TestMatchAll_EnhancedFingerprint
// 验证 FingerprintHub JSON 加载并能匹配已知规则。JSON 里有按特定
// header 匹配 "CloudFlare" 的规则。
func TestMatchAll_EnhancedFingerprint(t *testing.T) {
	// CloudFlare typically sets "cf-ray" or "cf-cache-status" headers.
	// We use "cf-cache-status: HIT" which appears in many rules.
	// / CloudFlare 通常设 "cf-ray" 或 "cf-cache-status" 头。我们用
	// "cf-cache-status: HIT"，这在很多规则里出现。
	data := fingerprint.CheckData{
		Headers: "Server: cloudflare\ncf-cache-status: HIT\n",
	}
	hits := fingerprint.MatchAll(data)
	if !contains(hits, "CloudFlare") {
		t.Errorf("expected CloudFlare hit from FingerprintHub, got %v", hits)
	}
}

// TestCalculateFaviconHashes verifies the favicon hashing function
// returns a stable non-zero int32. / TestCalculateFaviconHashes 验证
// favicon 哈希函数返回稳定的非零 int32。
func TestCalculateFaviconHashes(t *testing.T) {
	data := []byte("GIF89a\x01\x00\x01\x00\x80\x00\x00\xff\xff\xff")
	h := fingerprint.CalculateFaviconHashes(data)
	if len(h) != 1 {
		t.Fatalf("expected 1 hash, got %d", len(h))
	}
	if h[0] == 0 {
		t.Errorf("expected non-zero hash")
	}
	// Same input should give the same hash. / 同输入应给同哈希。
	h2 := fingerprint.CalculateFaviconHashes(data)
	if h[0] != h2[0] {
		t.Errorf("non-deterministic: %d vs %d", h[0], h2[0])
	}
}

// contains is a tiny helper. / contains 是个小工具。
func contains(slice []string, s string) bool {
	for _, x := range slice {
		if x == s {
			return true
		}
	}
	return false
}
