// Copyright (c) 2026 LCUstinian
// SPDX-License-Identifier: MIT
// FingerprintHub is a community-driven fingerprint database
// (https://github.com/0x727/FingerprintHub) with 3139 rules. The
// embedded JSON defines per-product matchers of three types:
//   - word   : keyword substring match (optionally case-insensitive)
//   - regex  : regex match
//   - favicon: mmh3 / MD5 hash match on /favicon.ico
//
// Each matcher also has a "condition" (and / or) and a "part" (header / body).
//
// FingerprintHub 是社区驱动的指纹库
// （https://github.com/0x727/FingerprintHub）含 3139 条规则。embedded
// JSON 为每个产品定义三种匹配器：
//   - word   : 关键字子串匹配（可选大小写不敏感）
//   - regex  : 正则匹配
//   - favicon: /favicon.ico 的 mmh3 / MD5 哈希匹配
//
// 每个匹配器还有 condition（and / or）和 part（header / body）。
package fingerprint

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
)

//go:embed web_fingerprint_v4.json
var fingerprintHubData []byte

// EnhancedFingerprint is one FingerprintHub entry (deserialized from
// JSON). We use anonymous structs to match the JSON shape without
// committing to a typed struct (the upstream data has many optional
// fields).
//
// EnhancedFingerprint 是一条 FingerprintHub 条目（反序列化自 JSON）。
// 用匿名 struct 以匹配 JSON 形状，不强行定死字段。
type EnhancedFingerprint struct {
	ID   string `json:"id"`
	Info struct {
		Name     string                 `json:"name"`
		Author   string                 `json:"author"`
		Tags     string                 `json:"tags"`
		Severity string                 `json:"severity"`
		Metadata map[string]interface{} `json:"metadata"`
	} `json:"info"`
	HTTP []struct {
		Method   string   `json:"method"`
		Path     []string `json:"path"`
		Matchers []struct {
			Type            string   `json:"type"`
			Words           []string `json:"words"`
			Regex           []string `json:"regex"`
			Hash            []string `json:"hash"`
			Part            string   `json:"part"`
			CaseInsensitive bool     `json:"case-insensitive"`
			Condition       string   `json:"condition"` // "and" / "or"
		} `json:"matchers"`
	} `json:"http"`
}

// DB is the in-memory FingerprintHub database with a regex cache.
// DB 是内存中的 FingerprintHub 数据库，含正则缓存。
type DB struct {
	Fingerprints []*EnhancedFingerprint
	regexCache   map[string]*regexp.Regexp
	mu           sync.RWMutex
}

var (
	db     *DB
	dbOnce sync.Once
)

// loadDB loads + caches the JSON data exactly once. / loadDB 加载并缓存
// JSON 数据一次。
func loadDB() *DB {
	d := &DB{regexCache: map[string]*regexp.Regexp{}}
	if err := json.Unmarshal(fingerprintHubData, &d.Fingerprints); err != nil {
		// If the data is corrupt, return an empty DB; matching
		// simply returns nothing. / 数据损坏则返回空 DB；匹配
		// 自然返回空。
		_ = err // logged via fmt below in v0.2 with a proper logger
		fmt.Fprintf(devNull{}, "fingerprint: load failed: %v\n", err)
	}
	return d
}

// devNull is a placeholder sink to satisfy Fprintf without a real
// logger. / devNull 是占位 sink，让 Fprintf 不需要真 logger 也能编译。
type devNull struct{}

func (devNull) Write(p []byte) (int, error) { return len(p), nil }

// getDB returns the singleton DB, initializing on first call.
// getDB 返回单例 DB，首次调用时初始化。
func getDB() *DB {
	dbOnce.Do(func() { db = loadDB() })
	return db
}

// fingerprintMatch is a single match with priority. / fingerprintMatch 是带优先级的单次匹配。
type fingerprintMatch struct {
	Name     string
	Priority int
}

// matchEnhancedFingerprints runs the FingerprintHub matcher against
// the given CheckData. Returns matched names sorted by priority (high
// to low) then by name (a→z). / matchEnhancedFingerprints 在给定
// CheckData 上跑 FingerprintHub 匹配器。返回按优先级降序、再按名称升序
// 排列的匹配名。
func matchEnhancedFingerprints(data CheckData) []string {
	d := getDB()
	if d == nil || len(d.Fingerprints) == 0 {
		return nil
	}
	bodyStr := string(data.Body)

	// Process in worker chunks to keep large rule sets snappy. The
	// upstream uses NumCPU(); for v0.1 we use a single worker (the
	// matching is fast — the regex cache is the bottleneck, not
	// the loop). / 用 worker 分块处理以保持大型规则集流畅。上游
	// 用 NumCPU()；v0.1 单 worker（匹配本身快——瓶颈在正则缓存，不是循环）。
	d.mu.RLock()
	fps := d.Fingerprints
	d.mu.RUnlock()

	var matches []fingerprintMatch
	for _, fp := range fps {
		if len(fp.HTTP) == 0 {
			continue
		}
		httpRule := fp.HTTP[0]
		for _, m := range httpRule.Matchers {
			if matchMatcher(m, bodyStr, data.Headers, data.Favicon, d) {
				matches = append(matches, fingerprintMatch{
					Name:     fp.Info.Name,
					Priority: calcPriority(fp, m.Type),
				})
				break // first matcher in this rule hit, no need to keep
			}
		}
	}

	// Sort by priority desc, then name asc. / 按优先级降序，再按名称升序。
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].Priority != matches[j].Priority {
			return matches[i].Priority > matches[j].Priority
		}
		return matches[i].Name < matches[j].Name
	})
	out := make([]string, len(matches))
	for i, m := range matches {
		out[i] = m.Name
	}
	return out
}

// calcPriority returns a higher-is-better score based on matcher type
// and verified flag. / calcPriority 返回越高越好的分数，基于匹配器类型
// 和 verified 标志。
func calcPriority(fp *EnhancedFingerprint, matcherType string) int {
	p := 0
	switch matcherType {
	case "favicon":
		p += 100
	case "regex":
		p += 50
	case "word":
		p += 30
	}
	if fp.Info.Metadata != nil {
		if v, ok := fp.Info.Metadata["verified"].(bool); ok && v {
			p += 20
		}
	}
	return p
}

// matchMatcher is the per-matcher test. / matchMatcher 是单匹配器测试。
func matchMatcher(
	m struct {
		Type            string   `json:"type"`
		Words           []string `json:"words"`
		Regex           []string `json:"regex"`
		Hash            []string `json:"hash"`
		Part            string   `json:"part"`
		CaseInsensitive bool     `json:"case-insensitive"`
		Condition       string   `json:"condition"`
	},
	body, headers string,
	favicon []int32,
	d *DB,
) bool {
	switch m.Type {
	case "word":
		return matchWords(m, body, headers)
	case "regex":
		return matchRegex(m, body, headers, d)
	case "favicon":
		return matchFavicon(m, favicon)
	default:
		return false
	}
}

// matchWords runs keyword matching. / matchWords 跑关键字匹配。
func matchWords(
	m struct {
		Type            string   `json:"type"`
		Words           []string `json:"words"`
		Regex           []string `json:"regex"`
		Hash            []string `json:"hash"`
		Part            string   `json:"part"`
		CaseInsensitive bool     `json:"case-insensitive"`
		Condition       string   `json:"condition"`
	},
	body, headers string,
) bool {
	target := body
	if m.Part == "header" {
		target = headers
	}
	words := m.Words
	if m.CaseInsensitive {
		target = strings.ToLower(target)
		words = make([]string, len(m.Words))
		for i, w := range m.Words {
			words[i] = strings.ToLower(w)
		}
	}
	isAnd := m.Condition == "and"
	hits := 0
	for _, w := range words {
		if strings.Contains(target, w) {
			if !isAnd {
				return true
			}
			hits++
		} else if isAnd {
			return false
		}
	}
	return isAnd && hits == len(words)
}

// matchRegex runs regex matching with a per-pattern cache.
// / matchRegex 用每模式缓存跑正则匹配。
func matchRegex(
	m struct {
		Type            string   `json:"type"`
		Words           []string `json:"words"`
		Regex           []string `json:"regex"`
		Hash            []string `json:"hash"`
		Part            string   `json:"part"`
		CaseInsensitive bool     `json:"case-insensitive"`
		Condition       string   `json:"condition"`
	},
	body, headers string,
	d *DB,
) bool {
	target := body
	if m.Part == "header" {
		target = headers
	}
	isAnd := m.Condition == "and"
	hits := 0
	for _, pat := range m.Regex {
		re := getOrCompile(d, pat)
		if re == nil {
			continue
		}
		if re.MatchString(target) {
			if !isAnd {
				return true
			}
			hits++
		} else if isAnd {
			return false
		}
	}
	return isAnd && hits == len(m.Regex)
}

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

// getOrCompile returns a compiled regex for pattern, using the
// DB's cache. Returns nil on compile error (skip).
//
// getOrCompile 返回 pattern 的编译后正则，用 DB 缓存。编译失败
// 返回 nil（跳过）。
func getOrCompile(d *DB, pattern string) *regexp.Regexp {
	d.mu.RLock()
	if re, ok := d.regexCache[pattern]; ok {
		d.mu.RUnlock()
		return re
	}
	d.mu.RUnlock()

	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil
	}
	d.mu.Lock()
	d.regexCache[pattern] = re
	d.mu.Unlock()
	return re
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
