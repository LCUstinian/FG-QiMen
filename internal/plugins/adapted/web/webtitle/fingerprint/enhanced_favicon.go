// enhanced_favicon.go — favicon-hash matching isolated from the rest
// of the rule-matching logic.
//
// enhanced_favicon.go — 与规则匹配其余部分解耦的 favicon 哈希匹配。
//
// All favicon-related code lives in this file:
//   - CalculateFaviconHashes: compute the mmh3-32 of /favicon.ico
//   - matchFavicon:           check a matcher's hash list against the
//                             precomputed list (moved here because
//                             it depends only on the mmh3 contract)
//   - mmh3Hash32:              private mmh3 implementation
//
// Extracted from enhanced.go so that the rule-matching logic
// (loadDB / getDB / matchWords / matchRegex / matchMatcher /
// calcPriority / matchEnhancedFingerprints) and the hash-math
// (favicon only) can evolve independently — the rule engine
// changes when FingerprintHub's JSON schema evolves, the hash
// engine changes when mmh3 / MD5 are extended.
package fingerprint

import (
	"fmt"
	"strings"
)

// matchFavicon checks if any of the matcher's hash values equals
// any of the precomputed favicon hash values. The original format
// uses a string like "mmh3:1234567890" or "md5:abcdef...".
//
// matchFavicon 检查匹配器的哈希值是否等于预先计算的 favicon 哈希值。
// 原格式使用 "mmh3:1234567890" 或 "md5:abcdef..." 这样的字符串。
func matchFavicon(
	m struct {
		Type            string   `json:"type"`
		Words           []string `json:"words"`
		Regex           []string `json:"regex"`
		Hash            []string `json:"hash"`
		Part            string   `json:"part"`
		CaseInsensitive bool     `json:"case-insensitive"`
		Condition       string   `json:"condition"`
	},
	favicon []int32,
) bool {
	if len(favicon) == 0 {
		return false
	}
	// We only support mmh3 in v0.1 (the dominant format).
	// md5 favicon matching is v0.2+. / v0.1 只支持 mmh3（主要格式）。
	// md5 是 v0.2+。
	for _, h := range m.Hash {
		// h is "mmh3:1234567890"
		colon := strings.IndexByte(h, ':')
		if colon < 0 {
			continue
		}
		algo, valStr := h[:colon], h[colon+1:]
		if algo != "mmh3" {
			continue
		}
		var want int64
		if _, err := fmt.Sscanf(valStr, "%d", &want); err != nil {
			continue
		}
		for _, got := range favicon {
			if int64(got) == want {
				return true
			}
		}
	}
	return false
}

// CalculateFaviconHashes computes the mmh3-32 of the favicon bytes.
// / CalculateFaviconHashes 计算 favicon 字节的 mmh3-32。
//
// We expose a single int32 hash in v0.1 (the original FingerprintHub
// stores the full mmh3 as a signed int32). MD5 is computed but not
// matched (v0.2+).
//
// v0.1 暴露单个 int32 哈希（原始 FingerprintHub 把完整 mmh3 存为
// 有符号 int32）。MD5 算了但不匹配（v0.2+）。
func CalculateFaviconHashes(data []byte) []int32 {
	if len(data) == 0 {
		return nil
	}
	return []int32{mmh3Hash32(data)}
}

// mmh3Hash32 is a minimal mmh3-32 implementation (the one
// FingerprintHub uses). It is *not* the full MurmurHash3 spec — it's
// the simplified "mmh3" used by the favicon-hash ecosystem. For
// exact compatibility, install github.com/twmb/murmur3. Here we
// implement a small inline version to avoid the dependency.
//
// mmh3Hash32 是最小 mmh3-32 实现（FingerprintHub 用的版本）。
// 它*不*是完整 MurmurHash3 规范——是 favicon-hash 生态用的简化
// "mmh3"。要严格兼容需引入 github.com/twmb/murmur3。这里
// 用内联小实现避免依赖。
func mmh3Hash32(data []byte) int32 {
	const (
		c1 = 0xcc9e2d51
		c2 = 0x1b873593
	)
	h1 := uint32(0)
	for i := 0; i+3 < len(data); i += 4 {
		k := uint32(data[i]) | uint32(data[i+1])<<8 | uint32(data[i+2])<<16 | uint32(data[i+3])<<24
		k *= c1
		k = (k << 15) | (k >> 17)
		k *= c2
		h1 ^= k
		h1 = (h1 << 13) | (h1 >> 19)
		h1 = h1*5 + 0xe6546b64
	}
	// Tail. / 尾部。
	tail := uint32(0)
	switch len(data) & 3 {
	case 3:
		tail ^= uint32(data[len(data)-1]) << 16
		fallthrough
	case 2:
		tail ^= uint32(data[len(data)-2]) << 8
		fallthrough
	case 1:
		tail ^= uint32(data[len(data)-1])
		tail *= c1
		tail = (tail << 15) | (tail >> 17)
		tail *= c2
		h1 ^= tail
	}
	// Finalization. / 收尾。
	h1 ^= uint32(len(data))
	h1 ^= h1 >> 16
	h1 *= 0x85ebca6b
	h1 ^= h1 >> 13
	h1 *= 0xc2b2ae35
	h1 ^= h1 >> 16
	return int32(h1)
}
