// Package fingerprint: public MatchAll entry point.
// Package fingerprint：公共 MatchAll 入口。
package fingerprint

import "sort"

// MatchAll runs all matchers (hardcoded rules + FingerprintHub)
// against the given CheckData and returns the merged, deduplicated,
// sorted list of matched names.
//
// MatchAll 跑所有匹配器（硬编码规则 + FingerprintHub）在给定 CheckData
// 上，返回合并、去重、排序后的命中名列表。
func MatchAll(data CheckData) []string {
	// 1) Hardcoded rules. / 硬编码规则。
	hardHits := matchHardcoded(data)

	// 2) FingerprintHub. / FingerprintHub。
	hubHits := matchEnhancedFingerprints(data)

	// Merge + dedup + alphabetical sort. / 合并 + 去重 + 字典序排序。
	all := append(hardHits, hubHits...)
	uniq := make([]string, 0, len(all))
	seen := make(map[string]struct{}, len(all))
	for _, s := range all {
		if _, dup := seen[s]; dup {
			continue
		}
		seen[s] = struct{}{}
		uniq = append(uniq, s)
	}
	sort.Strings(uniq)
	return uniq
}

// matchHardcoded runs the hardcoded RuleDatas list against data and
// returns the matched names.
//
// matchHardcoded 跑硬编码 RuleDatas 列表对 data，返回命中名。
func matchHardcoded(data CheckData) []string {
	var hits []string
	for _, r := range RuleDatas {
		if matchRule(r, data) {
			hits = append(hits, r.Name)
		}
	}
	return hits
}
