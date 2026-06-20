// Package fingerprint: public MatchAll entry point.
// Package fingerprint：公共 MatchAll 入口。
package fingerprint

import "sort"

// MatchAll runs all matchers (hardcoded rules + FingerprintHub +
// custom user-supplied rules) against the given CheckData and
// returns the merged, deduplicated, sorted list of matched names.
//
// MatchAll 跑所有匹配器（硬编码规则 + FingerprintHub + 用户自定义
// 规则）在给定 CheckData 上，返回合并、去重、排序后的命中名列表。
//
// Phase D (audit roadmap): added the custom-ruleset path so operators
// can supply their own JSON rules (native FG format or EHole
// format) via LoadCustomRuleset at process start.
func MatchAll(data CheckData) []string {
	// 1) Hardcoded rules. / 硬编码规则。
	hardHits := matchHardcoded(data)

	// 2) FingerprintHub. / FingerprintHub。
	hubHits := matchEnhancedFingerprints(data)

	// 3) Custom user-supplied rules (Phase D). / 用户自定义规则。
	customHits := matchCustomRules(data)

	// Merge + dedup + alphabetical sort. / 合并 + 去重 + 字典序排序。
	all := append(hardHits, hubHits...)
	all = append(all, customHits...)
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

// matchCustomRules runs the user-supplied custom rules against data.
// Returns the deduplicated list of matched rule names. /
// matchCustomRules 跑用户提供的自定义规则在 data 上。返回去重后的
// 命中规则名列表。
func matchCustomRules(data CheckData) []string {
	entries := CustomEntries()
	if len(entries) == 0 {
		return nil
	}
	bodyStr := string(data.Body)
	crs := CustomRules()
	hits := make([]string, 0, len(entries))
	for i, e := range entries {
		// Empty matchers list = no-op. / 空 matcher 列表 = no-op。
		if len(e.Matchers) == 0 {
			continue
		}
		// Each entry's matchers are AND-evaluated: ALL must hit
		// for the rule to count. (Phase D v1: AND-only; OR
		// support is a future enhancement.) / 每条 entry 的
		// matcher 是 AND 评估：全部命中才算。（Phase D v1：
		// 仅 AND；OR 支持是未来增强。）
		allHit := true
		for _, m := range e.Matchers {
			if !matchMatcher(m, bodyStr, data.Headers, data.Favicon, getDB()) {
				allHit = false
				break
			}
		}
		if allHit && i < len(crs) && crs[i].Info.Name != "" {
			hits = append(hits, crs[i].Info.Name)
		}
	}
	return hits
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
